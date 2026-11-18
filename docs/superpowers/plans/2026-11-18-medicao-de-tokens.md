# Medição de tokens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Medir quanto cada saída de ferramenta e cada injeção de hook ocupa no contexto e quanto custa em releituras, e mostrar isso no Analytics.

**Architecture:** Um leitor de transcript novo (`internal/usage/context.go`) extrai saídas, turnos e compactações; roda junto da ingestão de uso já existente com offset próprio (`transcript_offsets.tool_offset`), o que também cobre o histórico. A store calcula releituras e peso em SQL; dois endpoints novos alimentam uma seção nova do Analytics.

**Tech Stack:** Go 1.x (net/http, database/sql + SQLite, migrations embutidas), React + TypeScript + TanStack Query + Vitest.

**Spec:** docs/superpowers/specs/2026-11-18-medicao-de-tokens-design.md

## Global Constraints

- Nunca guardar conteúdo de saída nem argumentos de comando: só tool, key, bytes, tokens, turn, at, is_error.
- Tokens estimados = ceil(bytes de texto / 4); cada bloco `image` = 1600.
- Releituras = turnos do mesmo (session_id, path) com turn > o.turn e turn < primeira compactação depois de o.turn. Peso = tokens × releituras.
- Projeto de uma sessão = `COALESCE(c.project_id, s.project_id)` (card da sessão, senão a sessão); família expandida por `FamilyIDs` salvo `exact=1`, como em `/api/usage/totals`.
- Intervalo de datas `[from, to)`, aceitando RFC3339 ou AAAA-MM-DD (use `parseDay`).
- Textos de tela em português, curtos.
- Migração nova: `internal/store/migrations/014_context.sql`.
- Leitura de transcript nunca derruba o daemon (o recover de `ingestAsync` já cobre; a passada do histórico precisa do mesmo).

## Review Focus

- Mensagem de assistant dividida em várias linhas com o mesmo `message.id`, inclusive atravessando duas leituras incrementais: deve continuar sendo um só turno (Task 1, teste `TestParseContext_SplitMessageAcrossReads`).
- Compactação no fim de uma leitura e o turno seguinte só na próxima: o turno seguinte sai com `after_compact` (Task 1 e Task 2 via `pending_compact`).
- Comando Bash com segredo nos argumentos (`curl -H "Authorization: x"`, `export TOKEN=abc && psql ...`): a chave não contém nada além de programa/subcomando (Task 1, `TestBashKey`).
- Transcript truncado (arquivo menor que o tool_offset): linhas antigas daquele path apagadas e releitura do começo, sem turnos duplicados (Task 2).
- Sessão sem nenhum turno ou saída antes do primeiro turno (turn = -1): releituras contam todos os turnos até a compactação; API não quebra com listas vazias (Task 3 e 4).

---

### Task 1: Leitor de contexto do transcript

**Files:**
- Create: `internal/usage/context.go`
- Test: `internal/usage/context_test.go` (fixtures inline, escritas em `t.TempDir()`)

**Interfaces:**
- Produces:
  ```go
  type ToolOutput struct {
      ItemID  string    // tool_use_id ou uuid da linha (hook)
      Tool    string    // nome da ferramenta, ou "hook"
      Key     string
      Bytes   int64
      Tokens  int64
      Turn    int       // índice do último turno visto antes; -1 se nenhum
      At      time.Time
      IsError bool
  }
  type ContextTurn struct {
      Turn         int
      MessageID    string
      At           time.Time
      Tokens       int64 // input + cache_read + cache_write
      AfterCompact bool
  }
  // ContextState é o estado que atravessa leituras incrementais do mesmo arquivo.
  type ContextState struct {
      LastTurn       int    // -1 se nenhum
      LastMessageID  string
      PendingCompact bool
  }
  // ParseContext lê de offset até a última linha completa (em fluxo, linhas de
  // qualquer tamanho). Offset maior que o arquivo devolve reset=true e lê do
  // começo com estado zerado ({LastTurn:-1}). Turnos devolvidos podem repetir
  // o último turno do estado (mesmo message.id): o chamador faz upsert.
  func ParseContext(path string, offset int64, st ContextState) (outs []ToolOutput, turns []ContextTurn, next int64, newState ContextState, reset bool, err error)
  func BashKey(command string) string
  func EstimateTokens(textBytes int64, images int) int64
  ```

- [ ] **Step 1: Write the failing tests**

  - `TestParseContext_Basic`: transcript com assistant (id m1, usage input 10/cache_read 100/cache_write 5, bloco tool_use id t1 name Bash input.command "git status -s"), user com tool_result t1 content string de 400 bytes, assistant m2 (usage 1/200/0). Espera: 1 output {ItemID t1, Tool "Bash", Key "git status", Bytes 400, Tokens 100, Turn 0}; turns [{0,m1,115},{1,m2,201}].
  - `TestParseContext_ListContentAndImage`: tool_result content lista [text 8 bytes, image, tool_reference] → Bytes 8, Tokens 2+1600.
  - `TestParseContext_SplitMessageAcrossReads`: m1 em duas linhas (uma com usage antiga, outra com usage final); lê até a primeira linha, guarda next/state, lê o resto: só um turno índice 0 com os tokens da última linha.
  - `TestParseContext_Compaction`: m1, system compact_boundary, m2 → m2.AfterCompact true; compact_boundary como última linha de uma leitura → newState.PendingCompact true e, na leitura seguinte, m2.AfterCompact true.
  - `TestParseContext_Hooks`: attachment hook_success {hookEvent SessionStart, command "/x/nobre hook claude SessionStart", stdout `{"hookSpecificOutput":{"additionalContext":"<40 bytes>"}}`} → {Tool "hook", Key "SessionStart · cockpit", Bytes 40}; stdout texto puro de outro command → Key "SessionStart · outros", Bytes len(stdout); stdout vazio → nenhuma saída; hook_blocking_error {hookEvent Stop, blockingError "<n bytes>"} → Key "Stop · bloqueio".
  - `TestParseContext_Keys`: Read file_path "/a/b.go" → "Read .go"; "/a/Makefile" → "Read sem extensão"; Agent/Task subagent_type "Explore" → "Agent Explore", vazio → "Agent geral"; Skill skill "napkin" → "Skill napkin"; Grep → "Grep"; tool_result de tool_use_id desconhecido → Tool "?" Key "?".
  - `TestParseContext_Truncated`: offset > tamanho → reset true, lê do começo.
  - `TestParseContext_PartialLastLine`: última linha sem `\n` não é lida e next aponta para o fim da última linha completa.
  - `TestBashKey` (tabela): `git status -s`→`git status`; `cd /x && go test ./...`→`go test`; `FOO=1 BAR=2 pnpm test --run`→`pnpm test`; `cat secret.txt`→`cat`; `/usr/bin/python3 -I x.py`→`python3`; `curl -H "Authorization: Bearer abc" https://x`→`curl`; `export TOKEN=abc && psql -c 'select'`→`psql`; `git --no-pager log`→`git`; `ls | grep x`→`ls`; `""`→`outro`; `$(weird)`→`outro`; `make update`→`make update`; `nobre note "x"`→`nobre note`.

- [ ] **Step 2: Run tests to verify they fail**

  Run: `go test ./internal/usage/ -run 'TestParseContext|TestBashKey'`
  Expected: FAIL (símbolos indefinidos).

- [ ] **Step 3: Implement `internal/usage/context.go`**

  - Leitura com `bufio.Reader.ReadBytes('\n')` a partir do offset; linha sem `\n` final para a leitura sem avançar `next`.
  - `json.Unmarshal` numa struct leve (`content` como `json.RawMessage`, decodificado como string ou lista).
  - Mapa local tool_use_id → (name, key) preenchido pelos blocos tool_use; o key é calculado no tool_use (o input não sai daí).
  - Turno: assistant com usage e message.id ≠ state.LastMessageID abre turno novo (LastTurn+1); mesmo id reemite o turno com os tokens novos. Output aponta para `state.LastTurn` no momento em que aparece.
  - `at`: timestamp RFC3339Nano da linha; inválido → mtime do arquivo.
  - BashKey: corta no primeiro `|`, `;`, `&&`, `||` depois de pular segmentos iniciais `cd …` e `export …`; descarta tokens iniciais `VAR=valor`; programa = basename do primeiro token, validado por `^[A-Za-z0-9._+-]{1,32}$` (senão `outro`); subcomando só para a lista do spec e só se casar `^[a-z][a-z0-9-]{0,24}$`.

- [ ] **Step 4: Run tests to verify they pass**

  Run: `go test ./internal/usage/`
  Expected: PASS.

- [ ] **Step 5: Commit**

  `git add internal/usage/context.go internal/usage/context_test.go && git commit -m "feat(usage): leitor de saidas, turnos e compactacoes do transcript"`

---

### Task 2: Store e ingestão (inclui o histórico)

**Files:**
- Create: `internal/store/migrations/014_context.sql`, `internal/store/context.go`, `internal/store/context_test.go`
- Modify: `internal/adapter/claude/ingest.go`, `cmd/nobred/main.go` (chamar a passada do histórico)
- Test: `internal/adapter/claude/context_ingest_test.go`

**Interfaces:**
- Consumes: `usage.ParseContext`, `usage.ContextState`, `usage.ToolOutput`, `usage.ContextTurn` (Task 1).
- Produces:
  ```go
  // store
  func (s *Store) ContextCursor(sessionID, path string) (off int64, st usage.ContextState, err error)
  // SaveContext grava saídas (upsert por session_id,item_id), turnos (upsert por
  // session_id,path,turn) e o cursor, numa transação. reset=true apaga antes as
  // linhas de (session_id, path).
  func (s *Store) SaveContext(sessionID, path string, reset bool, outs []usage.ToolOutput, turns []usage.ContextTurn, next int64, st usage.ContextState) error
  func (s *Store) ClaudeTranscripts() (map[string]string, error) // session id → transcript_path (agent='claude', path não vazio)
  // adapter
  func (a *Adapter) BackfillContext() // passada do histórico, síncrona; o main chama em goroutine
  ```

- [ ] **Step 1: Migração `014_context.sql`**

  ```sql
  ALTER TABLE transcript_offsets ADD COLUMN tool_offset INTEGER NOT NULL DEFAULT 0;
  ALTER TABLE transcript_offsets ADD COLUMN pending_compact INTEGER NOT NULL DEFAULT 0;
  CREATE TABLE tool_outputs (
    session_id TEXT NOT NULL REFERENCES sessions(id),
    path TEXT NOT NULL,
    item_id TEXT NOT NULL,
    tool TEXT NOT NULL,
    key TEXT NOT NULL,
    bytes INTEGER NOT NULL,
    tokens INTEGER NOT NULL,
    turn INTEGER NOT NULL,
    at TEXT NOT NULL,
    is_error INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, item_id)
  );
  CREATE INDEX idx_tool_outputs_at ON tool_outputs(at);
  CREATE INDEX idx_tool_outputs_path ON tool_outputs(session_id, path, turn);
  CREATE TABLE context_turns (
    session_id TEXT NOT NULL REFERENCES sessions(id),
    path TEXT NOT NULL,
    turn INTEGER NOT NULL,
    message_id TEXT NOT NULL,
    at TEXT NOT NULL,
    tokens INTEGER NOT NULL,
    after_compact INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, path, turn)
  );
  ```
  O estado `LastTurn`/`LastMessageID` vem de `context_turns` (MAX(turn) do path); `PendingCompact` de `transcript_offsets.pending_compact`.

- [ ] **Step 2: Failing store tests (`internal/store/context_test.go`)**

  - `TestContextCursor_Empty`: sem linha → off 0, `ContextState{LastTurn:-1}`.
  - `TestSaveContext_RoundTrip`: grava 2 turnos + 1 output, cursor devolve off, LastTurn 1, LastMessageID do turno 1, PendingCompact gravado. Linha de `transcript_offsets` já existente (offset de uso 999) mantém `offset` 999.
  - `TestSaveContext_UpsertTurn`: gravar de novo o turno 1 com tokens novos substitui.
  - `TestSaveContext_Reset`: reset=true apaga turnos/saídas antigos do path (não os de outro path da mesma sessão).
  - `TestClaudeTranscripts`: só sessões agent claude com transcript_path.

- [ ] **Step 3: Implement `internal/store/context.go`** e rode `go test ./internal/store/` → PASS.

- [ ] **Step 4: Failing adapter test (`context_ingest_test.go`)**

  - `TestReadTranscripts_Context`: com o env de teste existente (`newEnv`), transcript principal + um de subagente em `<main sem .jsonl>/subagents/agent-a.jsonl`; `readTranscripts` grava saídas e turnos dos dois paths; segunda chamada sem mudança não duplica; ao acrescentar linhas, só o novo é lido.
  - `TestBackfillContext`: sessão já com `transcript_offsets.offset` no fim do arquivo (uso já lido) e `tool_offset` 0 → `BackfillContext()` grava as saídas; o uso não muda.
  - `TestReadTranscripts_ContextTruncated`: tool_offset maior que o arquivo → linhas antigas do path somem e o arquivo é relido.

- [ ] **Step 5: Implement**

  - Em `readTranscripts`, para cada path, depois do uso: `ContextCursor` → `ParseContext` (arquivo inexistente: ignora) → `SaveContext` quando `next != off || reset || len(outs)+len(turns) > 0`. Uso continua idêntico.
  - `BackfillContext`: para cada sessão de `ClaudeTranscripts()`, pega o mutex da sessão (`a.ingest.LoadOrStore`), roda `readTranscripts`, solta; com recover por sessão (log e segue); um `a.publish("usage")` no fim se gravou algo; log com quantidade de sessões e duração.
  - `cmd/nobred/main.go`: depois de criar o adapter, `go claudeAdapter.BackfillContext()` (fora do caminho de start do servidor).

- [ ] **Step 6: Run** `go test ./internal/store/ ./internal/adapter/claude/ ./internal/usage/` → PASS; `go vet ./...` limpo.

- [ ] **Step 7: Commit** `feat: grava saidas e turnos do transcript e mede o historico`

---

### Task 3: Consultas de peso na store

**Files:**
- Modify: `internal/store/context.go`
- Test: `internal/store/context_query_test.go`

**Interfaces:**
- Consumes: tabelas da Task 2.
- Produces:
  ```go
  type ContextFilter struct {
      From, To   time.Time   // zero = sem limite; [From, To) sobre tool_outputs.at
      ProjectIDs []int64     // vazio = todos; COALESCE(c.project_id, s.project_id) IN (...)
  }
  type ContextKey struct {
      Tool      string `json:"tool"`
      Key       string `json:"key"`
      Count     int64  `json:"count"`
      Tokens    int64  `json:"tokens"`
      AvgTokens int64  `json:"avg_tokens"`
      Weight    int64  `json:"weight"`
  }
  type ContextSession struct {
      SessionID   string `json:"session_id"`
      CardKey     string `json:"card_key"`
      CardTitle   string `json:"card_title"`
      StartedAt   string `json:"started_at"`
      PeakContext int64  `json:"peak_context"` // maior tokens de turno do transcript principal
      Compactions int64  `json:"compactions"`  // turnos after_compact do principal
      Weight      int64  `json:"weight"`       // soma dos pesos das saídas no filtro
  }
  type ContextOutput struct {
      Tool     string `json:"tool"`
      Key      string `json:"key"`
      Tokens   int64  `json:"tokens"`
      Rereads  int64  `json:"rereads"`
      Weight   int64  `json:"weight"`
      At       string `json:"at"`
      Subagent bool   `json:"subagent"` // path ≠ sessions.transcript_path
  }
  type ContextTurnPoint struct {
      Turn         int    `json:"turn"`
      At           string `json:"at"`
      Tokens       int64  `json:"tokens"`
      AfterCompact bool   `json:"after_compact"`
  }
  func (s *Store) ContextTopKeys(f ContextFilter, limit int) ([]ContextKey, error)       // ordem: weight desc, tokens desc
  func (s *Store) ContextTopSessions(f ContextFilter, limit int) ([]ContextSession, error)
  func (s *Store) ContextSessionDetail(sessionID string, limit int) (turns []ContextTurnPoint, top []ContextOutput, err error) // turns só do principal, em ordem; erro ErrNotFound se a sessão não existe
  ```
  Releituras de uma saída `o` (SQL, subconsulta correlacionada):
  ```sql
  (SELECT COUNT(*) FROM context_turns t WHERE t.session_id=o.session_id AND t.path=o.path AND t.turn > o.turn
     AND t.turn < COALESCE((SELECT MIN(k.turn) FROM context_turns k WHERE k.session_id=o.session_id AND k.path=o.path
                            AND k.after_compact=1 AND k.turn > o.turn), 1<<62))
  ```

- [ ] **Step 1: Failing tests**

  - `TestContextRereads`: path com turnos 0..5, after_compact no 4; output turn 1 → releituras 2 (turnos 2,3); output turn 4 → 1 (turno 5); output turn -1 → 4 (turnos 0..3).
  - `TestContextTopKeys`: duas chaves, ordena por weight; AvgTokens = Tokens/Count; filtro From/To exclui saída fora; filtro ProjectIDs pelo card da sessão e, sem card, pela sessão.
  - `TestContextTopSessions`: PeakContext e Compactions só do principal (turnos de subagente ignorados), weight soma no filtro, card key/título quando houver.
  - `TestContextSessionDetail`: turns do principal em ordem; top limitado e com Subagent true para saída de subagente; sessão inexistente → `ErrNotFound` (use o erro que a store já usa para "não encontrado").
  - `TestContextEmpty`: sem dados → slices vazias não nulas.

- [ ] **Step 2: Run** `go test ./internal/store/ -run Context` → FAIL; implemente; → PASS.

- [ ] **Step 3: Commit** `feat(store): ranking de contexto com releituras e peso`

---

### Task 4: API

**Files:**
- Create: `internal/api/context.go`, `internal/api/context_test.go`
- Modify: `internal/api/server.go` (registrar `contextRoutes`)

**Interfaces:**
- Consumes: Task 3; `parseDay`, `writeJSON`, `writeError`, `writeStoreError`, `s.st.FamilyIDs` (já existentes).
- Produces:
  - `GET /api/context/top?from=&to=&project=&exact=1` → `{"keys": ContextKey[], "sessions": ContextSession[]}` (keys até 30, sessions até 10).
  - `GET /api/context/sessions/{id}` → `{"turns": ContextTurnPoint[], "top": ContextOutput[]}` (top até 10).

- [ ] **Step 1: Failing tests** (com `newTestAPI`):
  - `TestContextTop`: dados semeados via store; resposta com keys ordenadas; `project` de pai expande a família; `exact=1` não expande; `from` inválido → 400; `project` não numérico → 400.
  - `TestContextSession`: 200 com turns/top; id inexistente → 404.
  - `TestContextTop_Empty`: `{"keys":[],"sessions":[]}`.
- [ ] **Step 2: Implement** seguindo `usageTotals` para from/to/project/exact.
- [ ] **Step 3: Run** `go test ./internal/api/` → PASS.
- [ ] **Step 4: Commit** `feat(api): endpoints de contexto`

---

### Task 5: Tela no Analytics

**Files:**
- Create: `web/src/pages/analytics/ContextSection.tsx`, `web/src/pages/analytics/ContextCurve.tsx`, `web/src/pages/analytics/ContextSection.test.tsx`
- Modify: `web/src/api/types.ts`, `web/src/api/queries.ts`, `web/src/pages/Analytics.tsx` (renderizar `<ContextSection from to projectId exact />` no fim)

**Interfaces:**
- Consumes: endpoints da Task 4.
- Produces: `useContextTop(from, to, project, exact)` (queryKey `['context','top',…]`) e `useContextSession(id | null)` (desabilitada com null) em `queries.ts`; tipos `ContextKey`, `ContextSession`, `ContextOutput`, `ContextTurnPoint`, `ContextTop`, `ContextDetail` em `types.ts`. A invalidação por SSE "usage" já existente deve alcançar `['context']` — confira onde `['usage']` é invalidado e inclua `['context']` no mesmo lugar.

- [ ] **Step 1: Failing tests** (`ContextSection.test.tsx`, mock de fetch como nos testes de Analytics):
  - Renderiza o título "O que enche o contexto" e uma linha por chave com Chamadas, Média, Total e Peso formatados por `tokens()`.
  - Linha de hook do Cockpit (`SessionStart · cockpit`) aparece como as outras.
  - "Sessões que mais pesaram" lista as sessões (card key quando houver, senão "sem card"); clicar numa sessão busca `/api/context/sessions/<id>` e mostra a curva (`role="img"` com `aria-label` "Contexto por turno") e a lista "Saídas mais pesadas" com "subagente" marcado.
  - Sem dados: texto "Nenhuma saída medida no período."
- [ ] **Step 2: Implement**
  - Tabela no mesmo estilo da tabela de cards do Analytics (`th`, `td`, `num`, `panel`, `h2`).
  - `ContextCurve`: SVG responsivo (viewBox), polilinha dos tokens por turno, linha vertical tracejada em cada `after_compact`, rótulo do pico; cores pelos tokens CSS existentes (`var(--accent)`, `var(--text-2)`).
- [ ] **Step 3: Run** `cd web && pnpm test --run && pnpm build` → PASS.
- [ ] **Step 4: Commit** `feat(web): o que enche o contexto no Analytics`
