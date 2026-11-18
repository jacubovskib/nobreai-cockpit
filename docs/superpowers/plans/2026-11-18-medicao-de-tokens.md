# Medição de tokens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Medir quanto cada saída de ferramenta e cada injeção de hook ocupa no contexto e quanto custa em releituras, e mostrar isso no Analytics.

**Architecture:** Um leitor de transcript novo (`internal/usage/context.go`) extrai saídas, turnos e compactações; roda junto da ingestão de uso já existente com offset próprio (`transcript_offsets.tool_offset`), o que também cobre o histórico. A store calcula releituras e peso em SQL; dois endpoints novos alimentam uma seção nova do Analytics.

**Tech Stack:** Go 1.x (net/http, database/sql + SQLite, migrations embutidas), React + TypeScript + TanStack Query + Vitest.

**Spec:** docs/superpowers/specs/2026-11-18-medicao-de-tokens-design.md

## Global Constraints

- Nunca guardar conteúdo de saída nem argumentos de comando: só tool, key, bytes, tokens, turn, at, is_error.
- Tokens estimados = ceil(bytes de texto / 4); cada bloco `image` = 1600.
- Turno = message.id distinto de assistant com usage e com algum token (0 token não conta). Segmento = compactações (`compact_boundary`) já vistas no arquivo.
- Releituras = MAX(turn) dos turnos do mesmo (session_id, path, segment) − o.turn, mínimo 0 (0 se o segmento não tem turno). Peso = (tokens + in_tokens) × releituras.
- Hook: tamanho = `hookSpecificOutput.additionalContext`; stdout em texto puro só conta em SessionStart e UserPromptSubmit.
- Projeto de uma sessão = `COALESCE(c.project_id, s.project_id)` (card da sessão, senão a sessão); família expandida por `FamilyIDs` salvo `exact=1`, como em `/api/usage/totals`.
- Intervalo de datas `[from, to)`, aceitando RFC3339 ou AAAA-MM-DD (use `parseDay`).
- Textos de tela em português, curtos.
- Migração nova: `internal/store/migrations/014_context.sql`.
- Leitura de transcript nunca derruba o daemon (o recover de `ingestAsync` já cobre; a passada do histórico precisa do mesmo).

## Review Focus

- Mensagem de assistant dividida em várias linhas com o mesmo `message.id`, inclusive atravessando duas leituras incrementais: deve continuar sendo um só turno (Task 1, teste `TestParseContext_SplitMessageAcrossReads`).
- Injeção de hook logo depois de uma compactação: conta os turnos do segmento novo, não zero (Task 1 atribui o segmento; Task 3 `TestContextRereads_AfterCompaction`).
- Compactação no fim de uma leitura e o turno seguinte só na próxima: o segmento atravessa a leitura via `ContextState.Segment`/`transcript_offsets.segment` (Task 1 e Task 2).
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
      Bytes    int64     // bytes de texto da saída
      Tokens   int64     // estimativa da saída
      InTokens int64     // estimativa do input do tool_use (0 para hook)
      Turn     int       // índice do último turno visto antes; -1 se nenhum
      Segment  int       // compactações vistas antes da saída
      At       time.Time
      IsError  bool
  }
  type ContextTurn struct {
      Turn      int
      Segment   int
      MessageID string
      At        time.Time
      Tokens    int64 // input + cache_read + cache_write
  }
  // ContextState é o estado que atravessa leituras incrementais do mesmo arquivo.
  type ContextState struct {
      LastTurn      int    // -1 se nenhum
      LastMessageID string
      Segment       int
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

  - `TestParseContext_Basic`: transcript com assistant (id m1, usage input 10/cache_read 100/cache_write 5, bloco tool_use id t1 name Bash input {"command":"git status -s"}), user com tool_result t1 content string de 400 bytes, assistant m2 (usage 1/200/0). Espera: 1 output {ItemID t1, Tool "Bash", Key "git status", Bytes 400, Tokens 100, InTokens = EstimateTokens(len do JSON cru do input, 0), Turn 0, Segment 0}; turns [{0,seg 0,m1,115},{1,seg 0,m2,201}].
  - `TestParseContext_ZeroTokenMessage`: assistant m0 com usage toda 0 entre m1 e m2 não abre turno (turnos 0 e 1 continuam m1 e m2).
  - `TestParseContext_ListContentAndImage`: tool_result content lista [text 8 bytes, image, tool_reference] → Bytes 8, Tokens 2+1600.
  - `TestParseContext_SplitMessageAcrossReads`: m1 em duas linhas (uma com usage antiga, outra com usage final); lê até a primeira linha, guarda next/state, lê o resto: só um turno índice 0 com os tokens da última linha.
  - `TestParseContext_Compaction`: m1, system compact_boundary, attachment hook_success SessionStart, m2 → m1 segmento 0; a saída do hook {Turn 0, Segment 1}; m2 {Turn 1, Segment 1}. compact_boundary como última linha de uma leitura → newState.Segment 1 e, na leitura seguinte, m2.Segment 1.
  - `TestParseContext_Hooks`: attachment hook_success {hookEvent SessionStart, command "/x/nobre hook claude SessionStart", stdout `{"hookSpecificOutput":{"additionalContext":"<40 bytes>"}}`} → {Tool "hook", Key "SessionStart · cockpit", Bytes 40, InTokens 0}; stdout texto puro de outro command em SessionStart → Key "SessionStart · outros", Bytes len(stdout); stdout texto puro em Stop ou PostToolUse → nenhuma saída; stdout vazio → nenhuma saída; hook_blocking_error {hookEvent Stop, blockingError "<n bytes>"} → Key "Stop · bloqueio".
  - `TestParseContext_Keys`: Read file_path "/a/b.go" → "Read .go"; "/a/Makefile" → "Read sem extensão"; Agent/Task subagent_type "Explore" → "Agent Explore", vazio → "Agent geral"; Skill skill "napkin" → "Skill napkin"; Grep → "Grep"; tool_result de tool_use_id desconhecido → Tool "?" Key "?".
  - `TestParseContext_Truncated`: offset > tamanho → reset true, lê do começo.
  - `TestParseContext_PartialLastLine`: última linha sem `\n` não é lida e next aponta para o fim da última linha completa.
  - `TestBashKey` (tabela): `git status -s`→`git status`; `cd /x && go test ./...`→`go test`; `FOO=1 BAR=2 pnpm test --run`→`pnpm test`; `cat secret.txt`→`cat`; `/usr/bin/python3 -I x.py`→`python3`; `curl -H "Authorization: Bearer abc" https://x`→`curl`; `export TOKEN=abc && psql -c 'select'`→`psql`; `git --no-pager log`→`git`; `ls | grep x`→`ls`; `""`→`outro`; `$(weird)`→`outro`; `make update`→`make update`; `nobre note "x"`→`nobre note`; `sudo systemctl restart x`→`systemctl restart`; `env A=1 go build`→`go build`; `time go test ./...`→`go test`; `timeout 30 pnpm test`→`pnpm test`; `rtk git status`→`git status`; `nohup nice make update`→`make update`.

- [ ] **Step 2: Run tests to verify they fail**

  Run: `go test ./internal/usage/ -run 'TestParseContext|TestBashKey'`
  Expected: FAIL (símbolos indefinidos).

- [ ] **Step 3: Implement `internal/usage/context.go`**

  - Leitura com `bufio.Reader.ReadBytes('\n')` a partir do offset; linha sem `\n` final para a leitura sem avançar `next`.
  - `json.Unmarshal` numa struct leve (`content` como `json.RawMessage`, decodificado como string ou lista).
  - Mapa local tool_use_id → (name, key) preenchido pelos blocos tool_use; o key é calculado no tool_use (o input não sai daí).
  - Turno: assistant com usage de soma > 0 e message.id ≠ state.LastMessageID abre turno novo (LastTurn+1, Segment atual); mesmo id reemite o turno com os tokens novos. Output aponta para `state.LastTurn` e `state.Segment` no momento em que aparece. `compact_boundary` incrementa `state.Segment`.
  - InTokens: `EstimateTokens(len(input cru), 0)` do tool_use, guardado no mapa tool_use_id junto do nome e da chave.
  - `at`: timestamp RFC3339Nano da linha; inválido → mtime do arquivo.
  - BashKey: corta no primeiro `|`, `;`, `&&`, `||` depois de pular segmentos iniciais `cd …` e `export …`; descarta tokens iniciais `VAR=valor`; programa = basename do primeiro token, validado por `^[A-Za-z0-9._+-]{1,32}$` (senão `outro`); subcomando só para a lista do spec e só se casar `^[a-z][a-z0-9-]{0,24}$`. Invólucros pulados antes do programa: `sudo`, `env` (e as atribuições depois dele), `time`, `timeout <n>`, `nice`, `nohup`, `rtk`.

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
  // Mescla de saídas (tool_use e tool_result podem chegar em leituras diferentes):
  // - linha nova: tool = COALESCE(NULLIF(tool,''),'?'), key idem;
  // - conflito: tool/key só mudam se o novo não for vazio; in_tokens só se o novo > 0;
  //   bytes/tokens/is_error só se o novo tiver Result=true; turn/segment/at ficam os da primeira gravação.
  func (s *Store) SaveContext(sessionID, path string, reset bool, outs []usage.ToolOutput, turns []usage.ContextTurn, next int64, st usage.ContextState) error
  func (s *Store) ClaudeTranscripts() (map[string]string, error) // session id → transcript_path (agent='claude', path não vazio)
  // adapter
  func (a *Adapter) BackfillContext(ctx context.Context) // passada do histórico, síncrona; para entre sessões quando ctx termina
  func (a *Adapter) StartBackfill(ctx context.Context)   // a.wg.Add(1) e roda BackfillContext numa goroutine (Wait espera)
  ```

- [ ] **Step 1: Migração `014_context.sql`**

  ```sql
  ALTER TABLE transcript_offsets ADD COLUMN tool_offset INTEGER NOT NULL DEFAULT 0;
  ALTER TABLE transcript_offsets ADD COLUMN segment INTEGER NOT NULL DEFAULT 0;
  CREATE TABLE tool_outputs (
    session_id TEXT NOT NULL REFERENCES sessions(id),
    path TEXT NOT NULL,
    item_id TEXT NOT NULL,
    tool TEXT NOT NULL,
    key TEXT NOT NULL,
    bytes INTEGER NOT NULL,
    tokens INTEGER NOT NULL,
    in_tokens INTEGER NOT NULL DEFAULT 0,
    turn INTEGER NOT NULL,
    segment INTEGER NOT NULL DEFAULT 0,
    at TEXT NOT NULL,
    is_error INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, item_id)
  );
  CREATE INDEX idx_tool_outputs_at ON tool_outputs(at);
  CREATE INDEX idx_tool_outputs_path ON tool_outputs(session_id, path, segment);
  CREATE TABLE context_turns (
    session_id TEXT NOT NULL REFERENCES sessions(id),
    path TEXT NOT NULL,
    turn INTEGER NOT NULL,
    segment INTEGER NOT NULL DEFAULT 0,
    message_id TEXT NOT NULL,
    at TEXT NOT NULL,
    tokens INTEGER NOT NULL,
    PRIMARY KEY (session_id, path, turn)
  );
  CREATE INDEX idx_context_turns_at ON context_turns(at);
  ```
  O estado `LastTurn`/`LastMessageID` vem de `context_turns` (MAX(turn) do path); `Segment` de `transcript_offsets.segment`.

- [ ] **Step 2: Failing store tests (`internal/store/context_test.go`)**

  - `TestContextCursor_Empty`: sem linha → off 0, `ContextState{LastTurn:-1}`.
  - `TestSaveContext_RoundTrip`: grava 2 turnos + 1 output, cursor devolve off, LastTurn 1, LastMessageID do turno 1, Segment gravado. Linha de `transcript_offsets` já existente (offset de uso 999) mantém `offset` 999.
  - `TestSaveContext_UpsertTurn`: gravar de novo o turno 1 com tokens novos substitui.
  - `TestSaveContext_KeepCompleted`: registro completo (Result true, bytes 400) seguido de um só de uso (Result false, bytes 0) do mesmo item → bytes/tokens continuam 400/100.
  - `TestSaveContext_MergeUseAndResult`: grava {item t1, Tool Bash, Key "git status", InTokens 5, Result false, Turn 2}; depois {item t1, Tool "", Key "", Bytes 400, Tokens 100, Result true, Turn 3} → linha única Bash/"git status"/in 5/tokens 100/turn 2. Resultado sozinho de item novo → tool "?" key "?".
  - `TestSaveContext_Reset`: reset=true apaga turnos/saídas antigos do path (não os de outro path da mesma sessão).
  - `TestClaudeTranscripts`: só sessões agent claude com transcript_path.

- [ ] **Step 3: Implement `internal/store/context.go`** e rode `go test ./internal/store/` → PASS.

- [ ] **Step 4: Failing adapter test (`context_ingest_test.go`)**

  - `TestReadTranscripts_Context`: com o env de teste existente (`newEnv`), transcript principal + um de subagente em `<main sem .jsonl>/subagents/agent-a.jsonl`; `readTranscripts` grava saídas e turnos dos dois paths; segunda chamada sem mudança não duplica; ao acrescentar linhas, só o novo é lido.
  - `TestBackfillContext`: sessão já com `transcript_offsets.offset` no fim do arquivo (uso já lido) e `tool_offset` 0 → `BackfillContext(ctx)` grava as saídas; o uso não muda. Com ctx já cancelado, não lê nenhuma sessão.
  - `TestReadTranscripts_ContextTruncated`: tool_offset maior que o arquivo → linhas antigas do path somem e o arquivo é relido.

- [ ] **Step 5: Implement**

  - Em `readTranscripts`, para cada path, depois do uso: `ContextCursor` → `ParseContext` (arquivo inexistente: ignora) → `SaveContext` quando `next != off || reset || len(outs)+len(turns) > 0`. Uso continua idêntico.
  - `BackfillContext(ctx)`: para cada sessão de `ClaudeTranscripts()` (antes de cada uma, sai se `ctx.Err() != nil`), pega o mutex da sessão (`a.ingest.LoadOrStore`), roda `readTranscripts`, solta; com recover por sessão (log e segue); log com quantidade de sessões e duração. `readTranscripts` já publica "usage" quando há uso novo; publique também quando houver contexto novo.
  - `cmd/nobred/main.go`: depois de criar `ctx` (signal.NotifyContext), `claudeAdapter.StartBackfill(ctx)`. O `defer claudeAdapter.Wait()` já existente espera a passada, que para entre sessões com o ctx cancelado.

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
      InTokens  int64  `json:"in_tokens"`
      AvgTokens int64  `json:"avg_tokens"` // (tokens + in_tokens) / count
      Weight    int64  `json:"weight"`
  }
  type ContextProject struct {
      ProjectID int64  `json:"project_id"`
      Weight    int64  `json:"weight"`
      TopKey    string `json:"top_key"` // chave de maior peso no projeto
  }
  type ContextTotals struct {
      ContextTotal int64 `json:"context_total"` // Σ context_turns.tokens com at no filtro (todos os paths)
      WeightTotal  int64 `json:"weight_total"`  // Σ pesos de todas as saídas no filtro
  }
  type ContextSession struct {
      SessionID   string `json:"session_id"`
      CardKey     string `json:"card_key"`
      CardTitle   string `json:"card_title"`
      StartedAt   string `json:"started_at"`
      PeakContext int64  `json:"peak_context"` // maior tokens de turno do transcript principal
      Compactions int64  `json:"compactions"`  // MAX(segment) do principal (turnos e saídas)
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
      AfterCompact bool   `json:"after_compact"` // segmento maior que o do turno anterior
  }
  func (s *Store) ContextTopKeys(f ContextFilter, limit int) ([]ContextKey, error)       // ordem: weight desc, tokens desc
  func (s *Store) ContextTopSessions(f ContextFilter, limit int) ([]ContextSession, error)
  func (s *Store) ContextByProject(f ContextFilter) ([]ContextProject, error)           // ordem: weight desc; sem projeto fica de fora
  func (s *Store) ContextTotals(f ContextFilter) (ContextTotals, error)
  func (s *Store) ContextSessionDetail(sessionID string, limit int) (turns []ContextTurnPoint, top []ContextOutput, err error) // turns só do principal, em ordem; erro ErrNotFound se a sessão não existe
  ```
  Releituras de uma saída `o` (máximo por segmento, não COUNT correlacionado):
  ```sql
  MAX(0, COALESCE((SELECT MAX(t.turn) FROM context_turns t
                   WHERE t.session_id=o.session_id AND t.path=o.path AND t.segment=o.segment), o.turn) - o.turn)
  ```
  (ou um CTE `seg_max(session_id, path, segment, max_turn)` juntado às saídas).

- [ ] **Step 1: Failing tests**

  - `TestContextRereads`: path com turnos 0..3 no segmento 0 e 4..5 no segmento 1; output {turn 1, seg 0} → 2; output {turn 4, seg 1} → 1; output {turn -1, seg 0} → 4; output {turn 5, seg 1} → 0.
  - `TestContextRereads_AfterCompaction`: turnos 0..1 seg 0; output de hook {turn 1, seg 1}; turnos 2..4 seg 1 → releituras 3; peso = (tokens + in_tokens) × 3.
  - `TestContextByProjectAndTotals`: dois projetos, peso e top_key por projeto; ContextTotal soma os turnos no intervalo; WeightTotal soma todas as saídas (não só o limite).
  - `TestContextTopKeys`: duas chaves, ordena por weight; AvgTokens = Tokens/Count; filtro From/To exclui saída fora; filtro ProjectIDs pelo card da sessão e, sem card, pela sessão.
  - `TestContextTopSessions`: PeakContext e Compactions só do principal (turnos e segmentos de subagente ignorados), weight soma no filtro, card key/título quando houver.
  - `TestContextSessionDetail`: turns do principal em ordem, AfterCompact true no primeiro turno de cada segmento > 0; top limitado e com Subagent true para saída de subagente; sessão inexistente → `ErrNotFound` (use o erro que a store já usa para "não encontrado").
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
  - `GET /api/context/top?from=&to=&project=&exact=1` → `{"keys": ContextKey[], "sessions": ContextSession[], "projects": ContextProject[], "context_total": n, "weight_total": n}` (keys até 30, sessions até 10).
  - `GET /api/context/sessions/{id}` → `{"turns": ContextTurnPoint[], "top": ContextOutput[]}` (top até 10).

- [ ] **Step 1: Failing tests** (com `newTestAPI`):
  - `TestContextTop`: dados semeados via store; resposta com keys ordenadas; `project` de pai expande a família; `exact=1` não expande; `from` inválido → 400; `project` não numérico → 400.
  - `TestContextSession`: 200 com turns/top; id inexistente → 404.
  - `TestContextTop_Empty`: `{"keys":[],"sessions":[],"projects":[],"context_total":0,"weight_total":0}`.
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
  - Renderiza o título "O que enche o contexto" e uma linha por chave com Chamadas, Entrada, Saída, Média e Peso formatados por `tokens()`; última linha "Resto (base e conversa)" com context_total − weight_total (nunca negativo).
  - Com o filtro em Todos (`projectId` null), tabela "Por projeto" com nome do projeto (de `useProjects`), peso e chave que mais pesou; com projeto escolhido ela não aparece.
  - Linha de hook do Cockpit (`SessionStart · cockpit`) aparece como as outras.
  - "Sessões que mais pesaram" lista as sessões (card key quando houver, senão "sem card"); clicar numa sessão busca `/api/context/sessions/<id>` e mostra a curva (`role="img"` com `aria-label` "Contexto por turno") e a lista "Saídas mais pesadas" com "subagente" marcado.
  - Sem dados: texto "Nenhuma saída medida no período."
- [ ] **Step 2: Implement**
  - Tabela no mesmo estilo da tabela de cards do Analytics (`th`, `td`, `num`, `panel`, `h2`).
  - `ContextCurve`: SVG responsivo (viewBox), polilinha dos tokens por turno, linha vertical tracejada em cada `after_compact`, rótulo do pico; cores pelos tokens CSS existentes (`var(--accent)`, `var(--text-2)`).
- [ ] **Step 3: Run** `cd web && pnpm test --run && pnpm build` → PASS.
- [ ] **Step 4: Commit** `feat(web): o que enche o contexto no Analytics`
