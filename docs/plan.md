# NobreAI Cockpit — Piloto: plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar o piloto do NobreAI Cockpit: daemon `nobred`, CLI `nobre` e interface web, com o adaptador do Claude Code capturando sessões por hooks e um handoff que funciona sem perguntas.

**Architecture:** Um daemon Go local (`nobred`) guarda tudo em SQLite e expõe API REST + SSE em `127.0.0.1:7420`, servindo também a UI React embutida (`go:embed`). O CLI `nobre` é usado pelos humanos, pelos agentes (via skill) e pelos hooks (`nobre hook claude <Evento>`), sempre falando com o daemon por HTTP. Cada provider é um adaptador Go; o piloto implementa só o do Claude Code.

**Tech Stack:** Go 1.26 (stdlib `net/http`, `modernc.org/sqlite` sem cgo, `github.com/spf13/cobra`), React 18 + TypeScript + Vite, `react-router-dom`, `@tanstack/react-query`, Vitest + Testing Library. Fontes Geist/Geist Mono.

**Spec:** `/home/nobre/workspace/nobretech/docs/superpowers/specs/2026-11-05-nobreai-cockpit-design.md` (telas de referência: https://claude.ai/artifact/VZs84mPU4RS1rWCPQg2K8L)

## Escopo do piloto

**Dentro:** card completo (seções 6.0–6.4 do spec), board com colunas automáticas, sessões/tokens/custo, regra de handoff, prompts salvos (9.1), limites com aviso e pausa, setup automático do Claude Code, handoff pela UI no modo "nova janela do terminal", as seis telas.

**Fora (plano 2):** adaptadores Codex e Kiro (na máquina: `codex` não está instalado; `kiro` é a IDE 1.0.437, sem `kiro-cli`), modos tmux e segundo plano (tmux não está instalado), ação "bloquear novas sessões", MCP, times. Por isso o handoff do piloto é testado Claude Code → Claude Code (sessão nova); a interface `Adapter` é desenhada para os outros dois entrarem sem mudar o núcleo.

## Global Constraints

- Repositório novo em `/home/nobre/workspace/nobretech/nobreai-cockpit`, módulo Go `github.com/nobretech/nobreai-cockpit`.
- Nomes: produto **NobreAI Cockpit**; binários `nobre` e `nobred`; chave de card `NAC-<n>` (sequência global iniciando em 1).
- Somente Linux na v1 (leitura de `/proc`). Verificado em 2026-11-05 nesta máquina: de dentro do Bash do Claude Code a cadeia é `zsh → claude → zsh → terminator`, então o `claude` é visível como ancestral (mesmo namespace de PID). `NOBRE_AGENT_PID` continua como override.
- Daemon escuta só em `127.0.0.1:7420` (env `NOBRE_ADDR` sobrescreve). Banco em `$XDG_DATA_HOME/nobreai-cockpit/cockpit.db` (padrão `~/.local/share/...`).
- Toda linha de dados de domínio tem `workspace_id` e `owner_id` (valores fixos `"local"` e `"me"` no piloto).
- Colunas do board, nesta ordem: `backlog`, `em_andamento`, `pausado`, `revisao`, `concluido` (rótulos: Backlog, Em andamento, Pausado / Handoff, Revisão, Concluído).
- Hooks **nunca** podem quebrar o agente: se o daemon estiver fora, `nobre hook` sai com código 0 e sem saída, em até 2 s.
- Custo em USD a partir da tabela de preços editável; modelo sem preço → custo "sem preço" (nunca zero). Exibição opcional em BRL com cotação manual.
- Nenhum preço de modelo é inventado: a tabela começa vazia.
- Toda a UI e as mensagens do CLI em português do Brasil.
- UI segue os tokens do design: fundo `#0E1014`, superfície `#15181E`, borda `#232730`, texto `#E8EAEE`, texto secundário `#A0A7B4`; cor por agente Claude `#3987E5`, Codex `#D95926`, Kiro `#199E70` (sempre com o nome ao lado); status com ícone + texto (`#0CA30C` ok, `#FAB219` aviso, `#FF8A80` erro).

## Review Focus

1. **Daemon fora do ar durante uma sessão** → `nobre hook` sai 0 em ≤2 s, sem stdout; o agente segue normalmente. (Task 9)
2. **`~/.claude/settings.json` com hooks de outros plugins ou JSON inválido** → instalação preserva hooks alheios, faz backup antes, e com JSON inválido aborta sem escrever. (Task 10)
3. **Hook `Stop` em loop** → com `stop_hook_active: true` o daemon nunca bloqueia de novo; lembrete de "sessão sem card" sai no máximo uma vez por sessão. (Task 8)
4. **Transcript lido duas vezes / com linhas repetidas do mesmo `message.id`** → tokens não são contados em dobro. (Task 5)
5. **Transcript grande no `Stop`/`SessionEnd`** → o hook responde dentro do timeout e o fim de sessão é gravado mesmo se a requisição for cancelada. (Task 8)
6. **Processo do agente morre sem `SessionEnd`** → card sai de "Em andamento" após a carência (15 min), e a UI mostra "sem sessão viva" nesse meio-tempo. (Task 11)

---

## Estrutura de arquivos

```
nobreai-cockpit/
  go.mod
  cmd/nobre/main.go                 # CLI (cobra)
  cmd/nobred/main.go                # daemon
  internal/domain/                  # tipos e regras puras
    types.go  column.go  readiness.go
  internal/store/                   # SQLite
    store.go  migrations/001_init.sql
    cards.go  content.go  sessions.go  usage.go  prompts.go  settings.go  projects.go
  internal/proc/ancestry.go         # PID do agente via /proc
  internal/usage/transcript.go  cost.go
  internal/prompts/render.go  seeds.go
  internal/resume/package.go
  internal/adapter/adapter.go       # interface
  internal/adapter/claude/          # hooks, install, skill, transcript
    hooks.go  install.go  skill.md  detect.go
  internal/board/reconciler.go  limits.go
  internal/launch/launch.go
  internal/api/                     # HTTP + SSE
    server.go  hub.go  cards.go  me.go  hooks.go  config.go  overview.go  usage.go  prompts.go
  internal/client/client.go         # cliente HTTP do CLI
  web/                              # React + Vite
    embed.go  index.html  src/...
  docs/spec.md  docs/plan.md        # cópias do spec e deste plano
```

---

### Task 1: Repositório, domínio e banco

**Files:**
- Create: `go.mod`, `cmd/nobred/main.go` (stub), `internal/domain/types.go`, `internal/domain/column.go`, `internal/store/store.go`, `internal/store/migrations/001_init.sql`, `docs/spec.md`, `docs/plan.md`, `.gitignore`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces:
  - `domain.Column` (string) com constantes `Backlog="backlog"`, `EmAndamento="em_andamento"`, `Pausado="pausado"`, `Revisao="revisao"`, `Concluido="concluido"`; `domain.Columns []Column` na ordem.
  - Structs em `domain`: `Card{ID int64; Key, Title, Goal, DoneWhen, WhereStopped, HowToTest, Branch string; WhereStoppedAt *time.Time; ProjectID int64; Column Column; CreatedAt, UpdatedAt time.Time}`, `Session{ID, Agent, ProviderSessionID, Cwd, Branch, GitHead, Model, TranscriptPath, EndReason string; CardID *int64; ProjectID *int64; PID int; StartedAt, LastEventAt time.Time; EndedAt *time.Time; FullCapture bool}`, `Event{ID int64; SessionID string; CardID *int64; Kind string; Data json.RawMessage; At time.Time}`, `Decision{ID, CardID int64; What, Why, SessionID string; At time.Time}`, `EssentialFile{ID, CardID int64; Path, Lines, Why string}`, `Todo{ID, CardID int64; Text string; Done bool; SessionID string}`, `Question{ID, CardID int64; Text, Answer, SessionID string}`, `UsageRow{SessionID, MessageID, Model string; Input, Output, CacheWrite, CacheRead int64; At time.Time}`, `Price{Model string; InputPerM, OutputPerM, CacheWritePerM, CacheReadPerM float64}`, `Limit{Agent string; DailyUSD, CardUSD float64; WarnPct int; Action string}` (`Action` ∈ `"pause"|"warn"`), `Prompt{ID int64; Name, Slug, Tag, Description, Body string; Agents []string; ProjectID *int64; IsDefaultHandoff bool; Uses int; LastUsedAt *time.Time}`, `Project{ID int64; Name, Path string; Capture bool}`.
  - Kinds de evento (constantes `domain.Ev*`): `session_start`, `session_end`, `prompt`, `file_read`, `file_edit`, `command`, `note`, `decision`, `essential_file`, `todo`, `question`, `heartbeat`.
  - `store.Open(path string) (*Store, error)` — cria diretório, abre SQLite com `_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`, aplica migrações embutidas; `(*Store).Close() error`; `store.DefaultPath() string`.

- [ ] **Step 1: Inicializar o repositório**

```bash
mkdir -p /home/nobre/workspace/nobretech/nobreai-cockpit && cd $_ && git init
go mod init github.com/nobretech/nobreai-cockpit
mkdir -p docs && cp ../docs/superpowers/specs/2026-11-05-nobreai-cockpit-design.md docs/spec.md && cp ../docs/superpowers/plans/2026-11-05-nobreai-cockpit-piloto.md docs/plan.md
```
`.gitignore`: `web/node_modules/`, `web/dist/`, `/nobre`, `/nobred`.

- [ ] **Step 2: Escrever o teste que falha**

`TestOpenMigratesAndIsIdempotent`: `Open(t.TempDir()+"/x/cockpit.db")` sem erro; `Close`; reabrir sem erro; tabela `cards` existe (`SELECT count(*) FROM cards` = 0); `PRAGMA foreign_keys` = 1.

- [ ] **Step 3: Rodar e ver falhar** — `go test ./internal/store/ -run TestOpen -v` → FAIL (pacote não compila).

- [ ] **Step 4: Implementar tipos, `Open` e `001_init.sql`**

Tabelas (todas com `workspace_id TEXT NOT NULL DEFAULT 'local'`, `owner_id TEXT NOT NULL DEFAULT 'me'`): `projects(id, name, path UNIQUE, capture)`, `cards(id, key UNIQUE, project_id, title, goal, done_when, where_stopped, where_stopped_at, how_to_test, branch, col, created_at, updated_at)`, `sessions(id PK TEXT, agent, provider_session_id, card_id NULL, project_id NULL, pid, cwd, branch, git_head, model, transcript_path, transcript_offset INTEGER DEFAULT 0, full_capture, started_at, last_event_at, ended_at NULL, end_reason, reminded INTEGER DEFAULT 0)`, `events(id, session_id, card_id NULL, kind, data JSON, at)`, `decisions`, `essential_files(UNIQUE(card_id,path))`, `todos`, `questions`, `usage(session_id, message_id, model, input, output, cache_write, cache_read, at, PRIMARY KEY(session_id, message_id))`, `prices(model PK, ...)`, `limits(agent PK, daily_usd, card_usd, warn_pct DEFAULT 80, action DEFAULT 'warn')`, `prompts(id, name, slug UNIQUE, tag, description, body, agents JSON, project_id NULL, is_default_handoff, uses, last_used_at)`, `settings(key PK, value JSON)`. Datas em texto RFC3339 UTC. Migrações por `PRAGMA user_version`.

- [ ] **Step 5: Rodar e ver passar** — mesmo comando → PASS.

- [ ] **Step 6: Commit** — `git add -A && git commit -m "feat: domínio e banco SQLite do NobreAI Cockpit"`

---

### Task 2: Cards e transições do board

**Files:**
- Create: `internal/store/cards.go`, `internal/store/projects.go`, `internal/domain/column.go` (adicionar regras)
- Test: `internal/store/cards_test.go`, `internal/domain/column_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `(*Store).CreateProject(name, path string) (domain.Project, error)`; `ListProjects() ([]domain.Project, error)`; `UpdateProject(p domain.Project) error`; `DeleteProject(id int64) error`; `ProjectForPath(cwd string) (*domain.Project, error)` — o projeto cujo `path` é o prefixo mais longo de `cwd` (limite de diretório: `/a/b` não casa `/a/bc`).
  - `(*Store).CreateCard(c domain.Card) (domain.Card, error)` — atribui `Key` = `NAC-<id>`, `Column` padrão `backlog`.
  - `GetCard(key string) (domain.Card, error)` (erro `store.ErrNotFound`), `ListCards(f CardFilter) ([]domain.Card, error)` com `CardFilter{ProjectID int64; Agent string}` (Agent filtra cards com alguma sessão daquele agente), `UpdateCard(c domain.Card) error`.
  - `domain.NextColumn(cur Column, ev Transition) Column` com `Transition` ∈ `SessionAttached`, `SessionEnded`, `AgentDone`, `HumanApproved`; `(*Store).MoveCard(key string, to Column) error` (movimento manual, sem regra).

- [ ] **Step 1: Testes que falham**

`TestNextColumn` (tabela): `backlog+SessionAttached→em_andamento`; `pausado+SessionAttached→em_andamento`; `em_andamento+SessionEnded→pausado`; `revisao+SessionEnded→revisao`; `concluido+SessionEnded→concluido`; `em_andamento+AgentDone→revisao`; `revisao+HumanApproved→concluido`; `concluido+SessionAttached→em_andamento`.
`TestCreateCardKeys`: dois cards → `NAC-1`, `NAC-2`; `GetCard("NAC-9")` → `ErrNotFound`.
`TestProjectForPath`: projetos `/w/api` e `/w/api/sub`; `/w/api/sub/x` → `sub`; `/w/apix` → nil.

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/... -run 'TestNextColumn|TestCreateCardKeys|TestProjectForPath' -v` → FAIL.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: cards, projetos e transições do board`

---

### Task 3: Conteúdo do card e regra de "pronto para handoff"

**Files:**
- Create: `internal/store/content.go`, `internal/domain/readiness.go`, `internal/store/settings.go`
- Test: `internal/domain/readiness_test.go`, `internal/store/content_test.go`

**Interfaces:**
- Consumes: Task 2.
- Produces:
  - Store: `SetWhereStopped(cardID int64, text string, at time.Time) error`; `AddDecision(d) (domain.Decision, error)`; `UpsertEssentialFile(f) error` (mesma path atualiza `why`/`lines`); `AddTodo(t) (domain.Todo, error)`; `SetTodoDone(id int64, done bool) error`; `AddQuestion(q)`; `AnswerQuestion(id int64, answer string) error`; `CardContent(cardID int64) (domain.Content, error)` com `domain.Content{Decisions []Decision; Files []EssentialFile; Todos []Todo; Questions []Question}` (definido em `internal/domain/types.go` para evitar ciclo de import); `LastEventAt(cardID int64, kind string) (*time.Time, error)`.
  - Settings: `(*Store).GetSettings() (domain.Settings, error)` / `PutSettings(domain.Settings) error`, com `domain.Settings{Rules map[string]bool; WhereStoppedTTLMin int; GraceMin int; ReminderEditThreshold int; LaunchMode string; TerminalCmd []string; Currency string; BRLRate float64; IncompleteAction string}`. Padrões: regras `goal`, `where_stopped`, `essential_file`, `todo_open`, `how_to_test` ligadas e `no_open_questions` desligada; TTL 15; carência 15; lembrete 3; `LaunchMode "terminal"`; `TerminalCmd ["x-terminal-emulator","-e"]`; `Currency "USD"`; `IncompleteAction "ask_agent"` (outra opção `"mark"`).
  - `domain.Readiness(c Card, ct Content, live bool, lastEdit *time.Time, s Settings, now time.Time) ReadinessResult` (todos os tipos em `domain`) com `ReadinessResult{Items []ReadinessItem; Met, Total int; Ready bool}` e `ReadinessItem{Rule, Label string; OK bool}`. Labels exatos: `goal` "Objetivo e “pronto quando” preenchidos"; `where_stopped` "“Onde parei” atualizado"; `essential_file` "Pelo menos 1 arquivo essencial com motivo"; `todo_open` "Lista “Falta” não está vazia"; `how_to_test` "“Como testar” preenchido"; `no_open_questions` "Nenhuma pergunta em aberto sem resposta". Só regras ligadas entram em `Items`/`Total`.

- [ ] **Step 1: Testes que falham** (`TestReadiness`, tabela)
  - card vazio → `Met=0, Total=5, Ready=false`.
  - `where_stopped` com sessão viva: `WhereStoppedAt = now-10min` → OK; `now-20min` → não OK (TTL 15).
  - `where_stopped` sem sessão viva: `WhereStoppedAt=t0`, `lastEdit=t0+1min` → não OK; `lastEdit=t0-1min` → OK; `WhereStoppedAt=nil` → não OK.
  - `essential_file` com `Why=""` não conta.
  - `todo_open`: só todos com `Done=false` contam.
  - tudo preenchido → `Ready=true, Met=5`.
  - `no_open_questions` ligada com pergunta sem resposta → item não OK.
  `TestUpsertEssentialFile`: mesma path duas vezes → 1 linha com o `why` novo.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/... -run 'TestReadiness|TestUpsertEssentialFile' -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: conteúdo do card e regra de pronto para handoff`

---

### Task 4: Sessões, eventos e PID do agente

**Files:**
- Create: `internal/store/sessions.go`, `internal/proc/ancestry.go`
- Test: `internal/store/sessions_test.go`, `internal/proc/ancestry_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - `proc.FindAgentAncestor(root string, start int, names []string) (int, error)` — sobe por `<root>/<pid>/stat` (campo ppid) a partir de `start`, devolve o primeiro PID cujo `<root>/<pid>/comm` está em `names`; `root` é `/proc` em produção (parametrizado para teste); `proc.Alive(root string, pid int) bool`.
  - Store: `StartSession(s domain.Session) error` (idempotente por `ID`); `GetSession(id string) (domain.Session, error)`; `SessionByPID(pid int) (*domain.Session, error)` (só sessões não encerradas); `TouchSession(id string, at time.Time) error`; `EndSession(id, reason string, at time.Time) error`; `LiveSessions() ([]domain.Session, error)`; `SessionsForCard(cardID int64) ([]domain.Session, error)`; `AddEvent(e domain.Event) error`; `RecentEvents(limit int) ([]domain.Event, error)`; `MarkReminded(id string) error`.
  - `(*Store).AttachSession(sessionID, cardKey string, force bool) error` — liga sessão ao card, aplica `NextColumn(SessionAttached)`, e copia para o card os eventos já capturados da sessão (`UPDATE events SET card_id`). Se outra sessão **viva** já é dona do card: `ErrCardBusy`, a menos que `force` (aí a outra é desligada do card).
  - Ao `EndSession` de sessão com card: aplica `NextColumn(SessionEnded)`.
  - ID de sessão = `<agent>:<provider_session_id>`.

- [ ] **Step 1: Testes que falham**
  `TestFindAgentAncestor`: árvore falsa em `t.TempDir()` (`100 comm=claude`, `200 ppid=100 comm=bash`, `300 ppid=200 comm=nobre`) → `FindAgentAncestor(root,300,["claude"])` = 100; sem match → erro.
  `TestAttachSessionOneOwner`: sessão A viva ligada a NAC-1 → card `em_andamento`; ligar B → `ErrCardBusy`; com `force` → ok e A sem card.
  `TestAttachCopiesEvents`: 2 eventos `file_edit` antes do attach → `card_id` preenchido depois.
  `TestEndSessionMovesCard`: `EndSession` → card `pausado`; card em `revisao` continua `revisao`.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/... -run 'TestFindAgentAncestor|TestAttach|TestEndSession' -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: sessões, eventos e identificação do processo do agente`

---

### Task 5: Uso de tokens e custo

**Files:**
- Create: `internal/usage/transcript.go`, `internal/usage/cost.go`, `internal/store/usage.go`, `internal/usage/testdata/main.jsonl`, `internal/usage/testdata/main/subagents/agent-x.jsonl`
- Test: `internal/usage/transcript_test.go`, `internal/usage/cost_test.go`, `internal/store/usage_test.go`

**Interfaces:**
- Consumes: Task 4.
- Produces:
  - `usage.ParseClaudeTranscript(path string, offset int64) (rows []domain.UsageRow, newOffset int64, err error)` — lê do `offset` até a última linha completa; considera linhas com `type=="assistant"` e `message.usage`; `MessageID = message.id`; `Model = message.model`; `Input=input_tokens`, `Output=output_tokens`, `CacheWrite=cache_creation_input_tokens`, `CacheRead=cache_read_input_tokens`; `At=timestamp`. Linhas repetidas do mesmo `message.id` viram 1 linha (a última vence).
  - `usage.SubagentTranscripts(mainPath string) []string` — `<mainPath sem .jsonl>/subagents/agent-*.jsonl`.
  - `usage.Cost(r domain.UsageRow, p *domain.Price) (usd float64, priced bool)` — `(in*InputPerM + out*OutputPerM + cw*CacheWritePerM + cr*CacheReadPerM)/1e6`; `p==nil` → `(0,false)`.
  - Store: `UpsertUsage(rows []domain.UsageRow) error` (chave `(session_id,message_id)`); `SetTranscriptOffset(sessionID string, off int64) error`; `ListPrices()`, `PutPrice(p)`, `DeletePrice(model)`; `ListLimits()`, `PutLimit(l)`.
  - `(*Store).Totals(f TotalsFilter) ([]Total, error)` com `TotalsFilter{From, To time.Time; GroupBy string}` (`"agent"|"card"|"session"|"day_agent"|"model"`) e `Total{Key, Agent, Day string; Sessions int; Input, Output, CacheWrite, CacheRead int64; CostUSD float64; Unpriced bool}`. `Unpriced=true` se alguma linha do grupo não tem preço.

- [ ] **Step 1: Testes que falham**
  `TestParseDedupesMessageID`: fixture com 3 linhas do `msg_1` (mesmo usage) + 1 do `msg_2` → 2 rows; `newOffset` = tamanho do arquivo.
  `TestParseIncremental`: parse até o fim, anexar linha `msg_3`, parse do offset → só `msg_3`.
  `TestParseIgnoresPartialLastLine`: última linha sem `\n` → não lida e offset para antes dela.
  `TestCost`: price `{3,15,3.75,0.3}` e row `{1_000_000,100_000,0,2_000_000}` → `3 + 1.5 + 0 + 0.6 = 5.10`; `nil` → `priced=false`.
  `TestUpsertUsageIdempotent`: inserir as mesmas rows 2× → contagem igual.
  `TestTotalsUnpriced`: modelo sem preço → `Unpriced=true`, `CostUSD` só dos com preço.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/usage/ ./internal/store/ -run 'TestParse|TestCost|TestUpsertUsage|TestTotals' -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: leitura de tokens dos transcripts e cálculo de custo`

---

### Task 6: Prompts salvos e pacote de retomada

**Files:**
- Create: `internal/prompts/render.go`, `internal/prompts/seeds.go`, `internal/store/prompts.go`, `internal/resume/package.go`
- Test: `internal/prompts/render_test.go`, `internal/resume/package_test.go`

**Interfaces:**
- Consumes: Tasks 3–5.
- Produces:
  - `prompts.Vars` = `map[string]string`; variáveis conhecidas `card.id`, `card.titulo`, `card.onde_parei`, `card.pacote`, `card.arquivos`, `card.como_testar`, `git.branch`, `handoff.faltando`.
  - `prompts.Render(body string, v Vars) (string, error)` — substitui `{{ nome }}` (espaços opcionais); nome fora da lista → `*UnknownVarsError{Names []string}` (sem renderizar parcialmente).
  - `prompts.Seeds() []domain.Prompt` — os 5 do design, com os corpos exatos da tela Prompts: `retomar-card` (Retomada, `IsDefaultHandoff=true`), `revisar-pr` (Revisão), `testes-borda` (Testes), `especificar` (Spec), `fechar-handoff` (Retomada). Inseridos pela migração de dados no primeiro `Open` se `prompts` está vazia.
  - Store: `ListPrompts()`, `GetPrompt(slug)`, `CreatePrompt(p)`, `UpdatePrompt(p)`, `DeletePrompt(slug)`, `DefaultHandoffPrompt()`, `MarkPromptUsed(slug, at)`; ao marcar um como `IsDefaultHandoff`, os outros perdem a marca.
  - `resume.Build(c domain.Card, ct domain.Content, sessions []domain.Session, totals []store.Total, diffStat string) string` — markdown com, nesta ordem: `# <KEY> · <Título>`, linha `repo · branch · N sessões`, `## Onde parei`, `## Objetivo` (com "Pronto quando"), `## Falta` (checkboxes abertos), `## Feito`, `## Decisões` (`- <what> — porquê: <why>`), `## Arquivos essenciais` (`- <path>[:<lines>]: <why>`), `## Perguntas em aberto`, `## Como testar`, `## Desde a última sessão` (diffStat ou "sem mudanças registradas"). Seções vazias são omitidas, exceto "Onde parei" que mostra "(não registrado)".
  - `resume.Vars(c, ct, pkg, readiness domain.ReadinessResult) prompts.Vars` — `card.arquivos` = paths separados por vírgula; `handoff.faltando` = labels dos itens não OK separados por "; ".

- [ ] **Step 1: Testes que falham**
  `TestRenderKnownVars`: `"Card {{card.id}} em {{ git.branch }}"` → `"Card NAC-42 em feat/oauth2"`.
  `TestRenderUnknownVars`: `"{{card.foo}} {{x}}"` → erro com `Names = ["card.foo","x"]`.
  `TestSeedsRenderWithAllVars`: cada seed renderiza sem erro com todas as variáveis preenchidas.
  `TestBuildPackageOrderAndOmissions`: card com onde parei, 1 decisão, 1 arquivo, sem perguntas → contém `## Onde parei` antes de `## Decisões`; não contém `## Perguntas em aberto`.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/prompts/ ./internal/resume/ -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: prompts salvos com variáveis e pacote de retomada`

---

### Task 7: Servidor HTTP, SSE e endpoints de dados

**Files:**
- Create: `internal/api/server.go`, `internal/api/hub.go`, `internal/api/cards.go`, `internal/api/me.go`, `internal/api/config.go`, `internal/api/usage.go`, `internal/api/prompts.go`, `cmd/nobred/main.go`, `internal/adapter/adapter.go` (só a interface e os tipos `HookCall`, `HookReply`, `Deps`, `Detection`, `Check`, `InstallReport`), `internal/launch/launch.go` (só `Launcher` e `Spec`), `internal/gitinfo/gitinfo.go` (implementação real de `GitInfo` executando `git`)
- Test: `internal/gitinfo/gitinfo_test.go` (repo temporário: `Branch`, `Clean` antes/depois de um arquivo novo)
- Test: `internal/api/cards_test.go`, `internal/api/me_test.go`, `internal/api/hub_test.go`

**Interfaces:**
- Consumes: Tasks 1–6.
- Produces:
  - `api.New(st *store.Store, deps Deps) http.Handler` com `Deps{Now func() time.Time; ProcRoot string; Adapters map[string]adapter.Adapter; Launcher launch.Launcher; Git GitInfo}` (`GitInfo` = interface `Branch(dir) string; Head(dir) string; DiffStat(dir, since string) string; Clean(dir) bool; Checkout(dir, branch string) error`). As interfaces `adapter.Adapter` e `launch.Launcher` são definidas nesta task (assinaturas nas Tasks 8 e 12); as implementações chegam lá, e `nil` é aceito até então.
  - `api.Hub` com `Publish(kind string)` e `GET /api/stream` (SSE, evento `changed` com `data: {"kind":"cards"|"sessions"|"usage"|"config"|"prompts"}`), heartbeat a cada 25 s.
  - Rotas (JSON; erros `{"error": "<mensagem em pt-BR>"}`):
    - `GET /api/health` → `{"ok":true,"version":"0.1.0"}`
    - `GET /api/cards?project=&agent=` · `POST /api/cards` · `GET /api/cards/{key}` → `CardDetail{Card; Content; Sessions []SessionWithUsage; Readiness; Totals; Live *Session}` · `PATCH /api/cards/{key}` (title, goal, done_when, how_to_test, where_stopped, column)
    - `POST /api/cards/{key}/decisions|files|todos|questions` · `PATCH /api/todos/{id}` · `PATCH /api/questions/{id}`
    - `GET /api/cards/{key}/resume` → `{"markdown": "..."}`
    - Rotas do agente, resolvidas pelo header `X-Nobre-Agent-PID` → sessão viva via `SessionByPID` (404 `"nenhuma sessão de agente encontrada para este processo"`): `POST /api/me/start {title, goal, done_when}` (cria card no projeto da sessão + attach), `POST /api/me/resume {key, force}` (attach + devolve markdown), `POST /api/me/note|decide|file|todo|todo-done|ask|test|done`, `GET /api/me/status`.
    - `GET/PUT /api/settings` · `GET/PUT /api/limits` · `GET/PUT/DELETE /api/prices` · `GET/POST/PATCH/DELETE /api/projects` · `GET/POST/PATCH/DELETE /api/prompts` · `POST /api/prompts/{slug}/render {card}` → `{"text"}` ou 422 com `unknown`.
    - `GET /api/usage/totals?from=&to=&group=` · `GET /api/sessions?limit=`.
  - Toda escrita chama `Hub.Publish` com o kind certo.
  - `cmd/nobred`: lê `NOBRE_ADDR` (padrão `127.0.0.1:7420`), abre `store.DefaultPath()`, sobe o servidor, encerra limpo em SIGINT/SIGTERM.

- [ ] **Step 1: Testes que falham** (`httptest`)
  `TestCreateAndGetCard`: POST → 201 com `key:"NAC-1"`; GET → `readiness.total == 5`.
  `TestMeRequiresKnownPID`: `POST /api/me/note` sem sessão → 404 com a mensagem exata.
  `TestMeNoteUpdatesWhereStopped`: sessão com `PID=4242` ligada a NAC-1; header `X-Nobre-Agent-PID: 4242`; `note {"text":"x"}` → card com `where_stopped="x"` e evento `note`.
  `TestMeDoneMovesToRevisao`.
  `TestPromptRenderUnknown`: corpo com `{{x}}` → 422 `{"unknown":["x"]}`.
  `TestHubPublishesOnWrite`: cliente SSE recebe `changed` com `kind:"cards"` após POST.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/api/ -v`.
- [ ] **Step 3: Implementar** (roteamento com `http.ServeMux` padrão do Go 1.22+, `GET /api/cards/{key}`).
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: API HTTP e stream de eventos do nobred`

---

### Task 8: Adaptador Claude Code — eventos dos hooks

**Files:**
- Create: `internal/adapter/claude/hooks.go`, `internal/api/hooks.go`, `internal/adapter/claude/testdata/*.json`
- Test: `internal/adapter/claude/hooks_test.go`

**Interfaces:**
- Consumes: Tasks 4–7.
- Produces:
  - (Definidos na Task 7, implementados aqui.) `adapter.Adapter` interface: `Name() string`; `ProcessNames() []string`; `HandleHook(ctx context.Context, h HookCall) (HookReply, error)`; `Detect() Detection`; `Install() (InstallReport, error)`; `LaunchCommand(initialPrompt string) []string`. `HookCall{Event string; Payload json.RawMessage; AgentPID int; Env map[string]string}`; `HookReply{Stdout string; Exit int}`.
  - `claude.New(st *store.Store, deps adapter.Deps) *Adapter`, onde `adapter.Deps{Now; Git; Publish func(kind string)}`. `ProcessNames()` = `["claude"]`. `LaunchCommand(p)` = `["claude", p]`.
  - `POST /api/hooks/{agent}` com corpo `HookCall` → `HookReply`.
  - Comportamento por evento (payload do Claude Code: `session_id`, `transcript_path`, `cwd`, `hook_event_name`, mais os campos de cada evento):
    - **Qualquer evento**: se a sessão não existe, cria (`ID claude:<session_id>`, `PID=AgentPID`, branch/head via Git, `ProjectID` e `FullCapture` = projeto encontrado com `capture=true`); `TouchSession`. Sem `FullCapture`, só registra a sessão e o uso (nada de prompts, arquivos, contexto, bloqueios).
    - **SessionStart**: se `Env["NOBRE_RESUME"]` tem uma chave → `AttachSession(force=true)` e devolve o pacote. Stdout = JSON `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext": <texto>}}` onde o texto é: instruções curtas do Cockpit (quando criar card, comandos `nobre`), a lista de cards `pausado` do mesmo projeto e branch (`NAC-n · título · onde parei`), e, se houve attach, o pacote de retomada.
    - **UserPromptSubmit**: evento `prompt` com `{"text": prompt}`.
    - **PostToolUse**: `Read` → `file_read`; `Edit|Write|MultiEdit|NotebookEdit` → `file_edit` com `{"path": tool_input.file_path (ou notebook_path)}`; `Bash` → `command` com `{"command", "exit_code"?}`.
    - **PreToolUse**: se o limite do agente (diário) ou do card for atingido e `Action=="pause"` → stdout `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"NobreAI Cockpit: limite de custo atingido (<detalhe>). Registre “Onde parei” com nobre note e encerre."}}`, exceto quando o comando Bash começa com `nobre ` (o agente precisa conseguir registrar o handoff).
    - **Stop**: lê o transcript (Task 5) e grava uso. Se `stop_hook_active` é true → nada. Se a sessão tem card, `IncompleteAction=="ask_agent"` e **há `file_edit` desta sessão depois de `WhereStoppedAt`** (ou `WhereStoppedAt` nulo com algum `file_edit`) → `{"decision":"block","reason": <prompt "fechar-handoff" renderizado>}`. O `Stop` dispara a cada resposta, por isso os demais itens da regra (como testar, arquivos, falta) só aparecem na UI e no `SessionEnd`; nunca bloqueiam turnos. Se a sessão não tem card, `FullCapture`, editou ≥ `ReminderEditThreshold` arquivos distintos e ainda não foi lembrada → `MarkReminded` e `{"decision":"block","reason":"NobreAI Cockpit: esta sessão já editou <n> arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start \"<título>\"; se não, só continue."}`.
    - **SessionEnd**: `EndSession(reason)` primeiro, depois lê o transcript.
  - Leitura do transcript também processa `usage.SubagentTranscripts`.
  - Todo trabalho de banco do handler usa um contexto desligado da requisição (`context.WithoutCancel`). No `Stop`, a decisão de bloquear é calculada e devolvida antes; a leitura do transcript roda em goroutine depois da resposta (serializada por sessão).

- [ ] **Step 1: Testes que falham** (fixtures em `testdata/` com payloads reais de cada evento)
  `TestSessionStartInjectsPausedCards`: projeto `/w/api`, card `pausado` no branch `main` → `additionalContext` contém `NAC-1`.
  `TestSessionStartResumeEnv`: `Env{"NOBRE_RESUME":"NAC-1"}` → sessão ligada, card `em_andamento`, contexto contém `## Onde parei`.
  `TestPostToolUseEditRecordsFile`; `TestUnregisteredProjectRecordsOnlyUsage` (nenhum evento `file_edit`, sessão existe).
  `TestStopBlocksWhenEditsAfterWhereStopped`; `TestStopDoesNotBlockWhenOnlyHowToTestMissing` (onde parei depois do último edit, só falta "como testar" → sem bloqueio); `TestStopNeverBlocksWhenStopHookActive`; `TestSessionEndSurvivesCancelledRequest` (contexto cancelado logo após o envio → sessão encerrada); `TestStopReminderOnlyOnce` (2 Stops → só o primeiro bloqueia).
  `TestPreToolUseDeniesOverLimitButAllowsNobre`.
  `TestStopReadsUsage`: transcript fixture → `Totals(group=session)` com tokens > 0.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/adapter/... -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: adaptador do Claude Code para hooks`

---

### Task 9: CLI `nobre`

**Files:**
- Create: `cmd/nobre/main.go`, `cmd/nobre/agent.go`, `cmd/nobre/hook.go`, `cmd/nobre/admin.go`, `internal/client/client.go`
- Test: `cmd/nobre/hook_test.go`, `internal/client/client_test.go`

**Interfaces:**
- Consumes: Tasks 4, 7, 8.
- Produces:
  - `client.New(addr string) *Client` com timeout 2 s; métodos espelhando as rotas da Task 7; envia `X-Nobre-Agent-PID` com o resultado de `proc.FindAgentAncestor("/proc", os.Getppid(), <ProcessNames de todos os adaptadores>)` (ou `NOBRE_AGENT_PID` se definido).
  - Comandos (texto de ajuda em pt-BR):
    - Agente: `nobre start "<título>" [--goal --done-when]`, `nobre resume <KEY> [--force]` (imprime o pacote), `nobre note "<onde parei>"`, `nobre decide "<decisão>" --why "<porquê>"`, `nobre file <path> --why "<motivo>" [--lines L12-96]`, `nobre todo add "<texto>"`, `nobre todo done <id>`, `nobre ask "<pergunta>"`, `nobre test "<como testar>"`, `nobre done`, `nobre status`.
    - Hooks: `nobre hook <agente> <Evento>` — lê stdin, envia `HookCall` com `Env` = `NOBRE_RESUME` se existir; imprime `Stdout`, sai com `Exit`. **Qualquer erro de rede/timeout → exit 0 sem saída.**
    - Admin: `nobre project add [path] [--name]`, `nobre project list`, `nobre prompt list`, `nobre prompt use <slug> [--card KEY]` (imprime o texto renderizado e marca uso), `nobre prompt save --name <n> --tag <t>` (corpo pelo stdin), `nobre ui` (abre `http://127.0.0.1:7420` com `xdg-open`).
  - Sem card ligado, comandos de agente que exigem card respondem: `Esta sessão ainda não tem card. Use nobre start "<título>" ou nobre resume <KEY>.` e saem com código 1.

- [ ] **Step 1: Testes que falham**
  `TestHookFailOpenWhenDaemonDown`: `NOBRE_ADDR=127.0.0.1:1` → exit 0, stdout vazio, duração < 2,5 s.
  `TestHookPassesStdoutAndExit`: servidor falso devolve `{"stdout":"X","exit":0}` → stdout `X`.
  `TestClientSendsAgentPID`: com `NOBRE_AGENT_PID=77` o header chega como `77`.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./cmd/nobre/ ./internal/client/ -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: CLI nobre para agentes, hooks e administração`

---

### Task 10: Setup automático do Claude Code e serviço do daemon

**Files:**
- Create: `internal/adapter/claude/install.go`, `internal/adapter/claude/detect.go`, `internal/adapter/claude/skill.md`, `cmd/nobre/setup.go`
- Modify: `internal/api/config.go` (rotas de agentes)
- Test: `internal/adapter/claude/install_test.go`

**Interfaces:**
- Consumes: Tasks 8–9.
- Produces:
  - `claude.InstallHooks(settingsPath, nobreBin string) (InstallReport, error)` — eventos `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SessionEnd`, cada um com `{"matcher":"*"?, "hooks":[{"type":"command","command":"<nobreBin> hook claude <Evento>","timeout":10}]}` (matcher só em Pre/PostToolUse). Mescla preservando entradas existentes; idempotente (reconhece as nossas pelo prefixo `<nobreBin> hook claude`); grava backup `settings.json.bak-<YYYYMMDDHHMMSS>` antes de escrever; JSON inválido → erro `"~/.claude/settings.json não é um JSON válido; nada foi alterado"` sem escrever; arquivo ausente → cria.
  - `claude.InstallSkill(dir string) error` — escreve `<dir>/nobre-cockpit/SKILL.md` a partir de `skill.md` embutido (frontmatter `name: nobre-cockpit`, `description` dizendo quando usar: tarefas de várias etapas, mudanças em vários arquivos, quando o usuário pedir um card ou citar uma chave `NAC-`). Corpo: quando criar card (critério do spec 6.0), os comandos `nobre` com um exemplo cada, a regra de manter "Onde parei" atualizado e marcar arquivos essenciais com motivo, e que não se deve perguntar ao usuário o que já está no card.
  - `(*Adapter).Detect() adapter.Detection` com `Detection{Found bool; Path, Version string; Checks []Check}` e `Check{Key, Label, Detail string; OK bool}`: `cli` ("CLI encontrado"), `hooks` ("Hooks instalados", detalhe com eventos faltantes), `skill` ("Skill do Cockpit instalada"), `events` ("Recebendo eventos", detalhe "último há …" a partir de `LastEventAt`).
  - Rotas: `GET /api/agents` → `[]{agent, detection}`; `POST /api/agents/{agent}/install` → `InstallReport`.
  - `nobre setup` — instala hooks e skill do Claude, grava `~/.config/systemd/user/nobred.service` (`ExecStart=<caminho absoluto do nobred>`, `Restart=on-failure`) e roda `systemctl --user daemon-reload && systemctl --user enable --now nobred`; registra o diretório atual como projeto se for repositório git e o usuário confirmar (`[s/N]`). Imprime um checklist com ✓/! por item.

- [ ] **Step 1: Testes que falham**
  `TestInstallPreservesForeignHooks`: settings com hook de outro plugin em `Stop` → depois da instalação os dois existem.
  `TestInstallIdempotent`: instalar 2× → cada evento com exatamente uma entrada nossa.
  `TestInstallInvalidJSONAborts`: arquivo `{` → erro com a mensagem exata, conteúdo intacto, sem backup novo.
  `TestInstallCreatesBackup`.
  `TestDetectReportsMissingHooks`: settings sem `Stop` → check `hooks` não OK com "Stop" no detalhe.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/adapter/claude/ -run 'TestInstall|TestDetect' -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: setup automático do Claude Code e serviço systemd`

---

### Task 11: Reconciliação do board, limites e visão geral

**Files:**
- Create: `internal/board/reconciler.go`, `internal/board/limits.go`, `internal/api/overview.go`
- Test: `internal/board/reconciler_test.go`, `internal/board/limits_test.go`, `internal/api/overview_test.go`

**Interfaces:**
- Consumes: Tasks 3–5, 7.
- Produces:
  - `board.Reconcile(st *store.Store, procRoot string, now time.Time, s domain.Settings) (changed bool, err error)` — para cada sessão viva cujo PID não existe mais: marca `pid_dead_at` (na 1ª vez) e, passada a carência `GraceMin` desde então, `EndSession(reason "processo encerrado")` (que move o card). Roda a cada 30 s no `nobred`; publica `cards`/`sessions` quando muda.
  - `board.LimitStatus(st, agent string, cardID *int64, now time.Time) LimitState` com `LimitState{AgentSpentUSD, AgentDailyUSD, CardSpentUSD, CardUSD float64; Pct int; Warn, Over bool; Action string}`; "hoje" = dia local; limite 0 = sem limite.
  - `GET /api/overview` → `Overview{Active []ActiveSession; Today TodayTotals; Alerts []Alert; NoCard []SessionWithUsage; Events []Event}` com `ActiveSession{Session; Card *Card; LastActivity string; SessionUsage, CardTotals Total; Readiness *ReadinessResult}` e `Alert{Kind, CardKey, Agent, Text string}`. Tipos de alerta e textos:
    - `stale`: "<KEY> está em “Em andamento” sem sessão viva há <n> min."
    - `limit_warn`: "<Agente> usou <pct>% do limite diário (US$ <gasto> de US$ <limite>)."
    - `incomplete`: "<KEY> foi pausado com handoff incompleto." (card `pausado` com `Ready=false`)
    - `unpriced`: "Há uso de modelos sem preço configurado: <modelos>."

- [ ] **Step 1: Testes que falham**
  `TestReconcileWaitsGrace`: PID morto, `now` = morte + 10 min → card ainda `em_andamento` e alerta `stale`; + 16 min → `pausado`.
  `TestReconcileKeepsLive`: PID vivo (proc falso) → nada muda.
  `TestLimitStatus`: limite diário 10, gasto 8,20 → `Pct=82, Warn=true, Over=false`; 10,00 → `Over=true`.
  `TestOverviewAlerts`: monta cenário com um de cada tipo → 4 alertas com os textos exatos.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/board/ ./internal/api/ -run 'TestReconcile|TestLimit|TestOverview' -v`.
- [ ] **Step 3: Implementar** (`pid_dead_at` como coluna nova numa migração `002`).
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: reconciliação do board, limites e visão geral`

---

### Task 12: "Continuar com…" pela interface

**Files:**
- Create: `internal/launch/launch.go`
- Modify: `internal/api/cards.go` (rota continue), `cmd/nobred/main.go` (injeta launcher real)
- Test: `internal/launch/launch_test.go`, `internal/api/continue_test.go`

**Interfaces:**
- Consumes: Tasks 6–8, 11.
- Produces:
  - `launch.Launcher` interface `Start(argv []string) error` (definida na Task 7 com `Spec{Argv []string}`); `launch.Exec{}` usa `exec.Command` com `Setsid` e solta o processo.
  - `launch.TerminalArgv(terminalCmd []string, dir string, env map[string]string, agentArgv []string) []string` — `terminalCmd + ["cd <dir> && <K>=<V> … exec <agentArgv>"]`, tudo com shell-quote numa string só. Diretório e variáveis vão **dentro** do comando porque o terminator (e o gnome-terminal) reaproveitam uma instância já aberta via DBus e descartam o `cwd`/env do processo que chamou.
  - `POST /api/cards/{key}/continue {agent, mode, prompt_slug}`: `mode` vazio = `Settings.LaunchMode`; só `"terminal"` no piloto (outros → 400 "modo ainda não disponível no piloto"). Passos: projeto do card; se branch atual ≠ branch do card: árvore limpa → `Checkout`, suja → 409 "O repositório tem mudanças não salvas; faça commit ou stash antes de continuar."; prompt inicial = `prompt_slug` ou o padrão de handoff, renderizado com `resume.Vars`; `NOBRE_RESUME=<KEY>` e o diretório do projeto embutidos via `TerminalArgv`; se o card tem sessão viva → 409 "<KEY> está com uma sessão viva de <agente>. Encerre-a ou use forçar." a menos que `force:true`; registra uso do prompt.

- [ ] **Step 1: Testes que falham**
  `TestTerminalArgv`: `(["x-terminal-emulator","-e"], "/w/api", {"NOBRE_RESUME":"NAC-1"}, ["claude","Continue o NAC-1: 'x'"])` → `["x-terminal-emulator","-e","cd '/w/api' && NOBRE_RESUME='NAC-1' exec 'claude' 'Continue o NAC-1: '\\''x'\\'''"]`.
  `TestContinueRefusesDirtyTree`, `TestContinueRefusesLiveSession`, `TestContinueStartsWithEnv` (o argv recebido pelo launcher falso contém `cd '<path do projeto>'` e `NOBRE_RESUME='NAC-1'`).
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/launch/ ./internal/api/ -run 'TestTerminalArgv|TestContinue' -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: continuar card em nova janela do terminal`

---

### Task 13: Base da UI web e embed no daemon

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/embed.go`, `web/src/main.tsx`, `web/src/styles/tokens.css`, `web/src/app/Layout.tsx`, `web/src/app/routes.tsx`, `web/src/api/client.ts`, `web/src/api/types.ts`, `web/src/api/useStream.ts`, `web/src/ui/` (AgentSwatch, StatusBadge, ReadinessPips, Money, Tokens)
- Modify: `internal/api/server.go` (servir `web.Dist` com fallback SPA para `index.html`)
- Test: `web/src/ui/ui.test.tsx`, `internal/api/static_test.go`

**Interfaces:**
- Consumes: Task 7.
- Produces:
  - `web.Dist` (`embed.FS` de `dist/`; commitar `web/dist/.gitkeep` e ajustar o `.gitignore` para `web/dist/*` + `!web/dist/.gitkeep`, para o `go:embed` compilar num clone limpo); `Makefile` com `make web` (`pnpm -C web install && pnpm -C web build`) e `make build` (web + `go build ./cmd/...`).
  - Rotas da SPA: `/` (Sala de controle), `/board`, `/cards/:key`, `/prompts`, `/analytics`, `/config`. Barra lateral com a ordem e os ícones do design; rodapé "nobred ativo · 127.0.0.1:7420 · v0.1.0" a partir de `/api/health` (vermelho "nobred fora do ar" se falhar).
  - `api/types.ts` espelhando os JSON da Task 7/11 (nomes de campo em snake_case como a API).
  - `useStream()` — abre `EventSource('/api/stream')` e invalida queries do react-query por `kind`.
  - Componentes: `AgentSwatch({agent})` (quadrado 10px na cor do agente + nome), `StatusBadge({column, live})`, `ReadinessPips({met,total})` com `aria-label` "<met>/<total> para handoff", `Money({usd, unpriced})` ("US$ 4,82"; com cotação em BRL quando `Currency=="BRL"`; `unpriced` → "sem preço"), `Tokens({n})` ("412 k", "1,08 M").
- [ ] **Step 1: Testes que falham**
  `ui.test.tsx`: `Money` com 4.82 → "US$ 4,82"; `Money` unpriced → "sem preço"; `Tokens` 1_082_000 → "1,08 M"; `ReadinessPips` 4/5 → `aria-label` "4/5 para handoff".
  `TestServesSPAFallback`: `GET /board` → 200 com `<div id="root">`.
- [ ] **Step 2: Rodar e ver falhar** — `pnpm -C web test --run` e `go test ./internal/api/ -run TestServesSPA -v`.
- [ ] **Step 3: Implementar** (tokens do design em `tokens.css` como variáveis CSS; Geist via Google Fonts).
- [ ] **Step 4: Rodar e ver passar** + `make build` sem erro.
- [ ] **Step 5: Commit** — `feat: base da interface web embutida no nobred`

---

### Task 14: Telas Sala de controle e Board

**Files:**
- Create: `web/src/pages/ControlRoom.tsx`, `web/src/pages/Board.tsx`, `web/src/pages/board.ts`
- Test: `web/src/pages/board.test.ts`, `web/src/pages/ControlRoom.test.tsx`

**Interfaces:**
- Consumes: Tasks 11, 13.
- Produces:
  - Sala de controle, seguindo o artboard 1: quatro indicadores (agentes ativos, cards em andamento com o aviso "<n> sem sessão viva", custo hoje "de US$ <soma dos limites diários>", tokens hoje com entrada/saída/cache), "Precisa de atenção" com os alertas da Task 11 e suas ações (abrir card, mover para Pausado, ajustar limite), "Agentes rodando agora", "Sessões sem card" com "Criar card desta sessão" (prompt de título → `POST /api/cards` + attach via `POST /api/cards/{key}/attach {session_id}` — rota adicionada aqui), "Custo hoje × limite diário" com marcador em 80%, "Eventos dos hooks".
  - Board, seguindo o artboard 2: cinco colunas com a frase de regra de cada uma, filtro por agente, card com chave, "ao vivo"/"sem sessão há <n> min", título, onde parei, agente, nº de sessões, custo, `ReadinessPips`. Arrastar entre colunas → `PATCH column`.
  - `board.groupByColumn(cards: CardSummary[]): Record<Column, CardSummary[]>` (ordem fixa, colunas vazias presentes).
- [ ] **Step 1: Testes que falham** — `groupByColumn` devolve as 5 chaves na ordem mesmo sem cards; `ControlRoom` renderiza o texto de um alerta `stale` vindo de um mock da API.
- [ ] **Step 2: Rodar e ver falhar** — `pnpm -C web test --run`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: telas sala de controle e board`

---

### Task 15: Tela do card (documento de handoff)

**Files:**
- Create: `web/src/pages/CardPage.tsx`, `web/src/pages/card/*.tsx` (WhereStopped, Goal, Todos, Decisions, Files, Sessions, Questions, ContinuePanel, CliPanel, ReadinessPanel, LimitPanel, ResumePreview)
- Test: `web/src/pages/CardPage.test.tsx`

**Interfaces:**
- Consumes: Tasks 7, 12, 13.
- Produces: tela do artboard 3, com todos os campos editáveis pelo humano (onde parei, objetivo, pronto quando, como testar, todos, responder pergunta); tabela de sessões com entrada/saída/cache/custo por sessão e total do card; "Arquivos tocados" (eventos `file_edit`/`file_read`) recolhido por padrão; painel "Continuar com outro agente" (agentes detectados; no piloto só Claude Code habilitado, os outros com "em breve"), "Prompt inicial" (prompts disponíveis, padrão marcado), "Abrir em" (padrão da plataforma; tmux e segundo plano desabilitados com "em breve"), botão "Iniciar <agente> neste card" → `POST continue`, exibindo erros 409 em linha; painel "Pelo terminal" com `nobre resume <KEY>` e botão copiar; "Pronto para handoff?" com itens da regra; "Aprovar e concluir" quando em `revisao`.
- [ ] **Step 1: Teste que falha** — com mock de `CardDetail`, mostra "Onde parei", "4 de 5" e, ao receber 409 do continue, mostra a mensagem do servidor.
- [ ] **Step 2: Rodar e ver falhar.**
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: tela do card com handoff`

---

### Task 16: Telas Analytics, Configuração e Prompts

**Files:**
- Create: `web/src/pages/Analytics.tsx`, `web/src/pages/analytics/DailyChart.tsx`, `web/src/pages/Config.tsx`, `web/src/pages/Prompts.tsx`, `web/src/pages/prompts/highlight.ts`
- Test: `web/src/pages/prompts/highlight.test.ts`, `web/src/pages/analytics/DailyChart.test.tsx`

**Interfaces:**
- Consumes: Tasks 5, 6, 10, 11, 13.
- Produces:
  - Analytics (artboard 4): período Hoje/7 dias/30 dias, indicadores, gráfico de barras empilhadas custo/dia/agente (um eixo só, legenda, tooltip por dia, botão "Ver como tabela"), tabela por agente, cards mais caros, sessões recentes com tokens e custo. O indicador "Retomadas sem perguntas" fica fora do piloto (depende de classificar perguntas; anotar no plano 2).
  - Configuração (artboard 5): agentes detectados com checklist e "Instalar o que falta" (`POST install`), limites (diário, por card, aviso %, ação `pause|warn`), criação de cards (as 4 opções; no piloto mapeiam para `ReminderEditThreshold` e a injeção de cards pausados), regra de handoff (as 6 regras, TTL, ação ao terminar), "Abrir o agente em" (só terminal habilitado), projetos com "Capturar sessões", preços por modelo (modelos vistos no uso sem preço aparecem com destaque) e moeda/cotação.
  - Prompts (artboard 6): lista com filtro por tag, editor (nome, slug, texto, variáveis clicáveis, agentes, padrão no handoff, só neste projeto), prévia renderizada com um card escolhido (variável desconhecida em destaque de erro via resposta 422), comando `nobre prompt use <slug> --card <KEY>`, e "Prompts que você digitou nas sessões" (eventos `prompt`) com "Salvar como prompt".
  - `highlight.splitVars(body: string): {text: string; isVar: boolean}[]`.
- [ ] **Step 1: Testes que falham** — `splitVars("a {{card.id}} b")` → 3 partes com a do meio `isVar`; `DailyChart` com 2 dias renderiza 2 grupos com `aria-label` contendo o total.
- [ ] **Step 2: Rodar e ver falhar.**
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: telas de analytics, configuração e prompts`

---

### Task 17: Teste de ponta a ponta do handoff e checklist do piloto

**Files:**
- Create: `internal/e2e/handoff_test.go`, `internal/e2e/testdata/` (payloads e transcripts de duas sessões), `docs/piloto-checklist.md`

**Interfaces:**
- Consumes: tudo.
- Produces:
  - `TestHandoffEndToEnd` (servidor real em `httptest`, `/proc` falso): sessão A (PID 100) inicia num projeto registrado → `nobre start` (via client com `NOBRE_AGENT_PID=100`) → edits, `note`, `decide`, `file`, `todo add`, `test` → `Stop` com transcript → `SessionEnd` → card `pausado`, `Ready=true`, custo da sessão A com preço configurado. Sessão B (PID 200) com `Env NOBRE_RESUME=NAC-1` → `SessionStart` devolve contexto com o "Onde parei" e as decisões de A → card `em_andamento` com 2 sessões e totais somados.
  - `docs/piloto-checklist.md`: roteiro manual com o Claude Code real: `make build`, `nobre setup`, abrir sessão num repo registrado, pedir uma tarefa de várias etapas e ver o card nascer, encerrar, clicar "Continuar com Claude Code" na UI, e confirmar os três critérios do spec (sem perguntas; tokens e US$ por sessão; colunas corretas — inclusive matando o processo e esperando a carência).
- [ ] **Step 1: Escrever o teste.**
- [ ] **Step 2: Rodar** — `go test ./internal/e2e/ -v` → corrigir o que falhar nas tasks de origem.
- [ ] **Step 3: Rodar a suíte inteira** — `go test ./... && pnpm -C web test --run && make build` → tudo verde.
- [ ] **Step 4: Commit** — `test: handoff de ponta a ponta e checklist do piloto`

---

### Task 18: Dados de demonstração (`nobred --seed-demo`)

Adicionada em 2026-11-07, quando o usuário pediu para fazer primeiro "as bases e o frontend". Roda depois da Task 11 e antes da Task 13, para que as telas possam ser conferidas sem hooks.

**Files:**
- Create: `internal/demo/seed.go`
- Modify: `cmd/nobred/main.go` (flags `--db <path>` e `--seed-demo`)
- Test: `internal/demo/seed_test.go`

**Interfaces:**
- Consumes: Tasks 1–7, 11.
- Produces:
  - `demo.Seed(st *store.Store, now time.Time, livePID int) error` — falha com `"o banco não está vazio; use outro --db"` se já existir algum card. Carrega os dados ilustrativos do design (https://claude.ai/artifact/VZs84mPU4RS1rWCPQg2K8L): projeto `nobretech/portal-api` (path `/tmp/nobreai-demo/portal-api`); os 11 cards do board nas colunas do artboard 2, com títulos e "onde parei" iguais aos do design; o card "Migrar autenticação para OAuth2" completo como no artboard 3 (objetivo, pronto quando, 6 todos com 3 feitos, as 3 decisões, os 3 arquivos essenciais, 1 pergunta em aberto, 3 sessões Claude/Codex/Claude com os tokens de entrada/saída/cache do design e 14 eventos de arquivos tocados); as sessões ao vivo de Claude Code, Codex e Kiro (Sala de controle) com `PID = livePID`, para a reconciliação não encerrá-las; uma sessão sem card; limites diários 15/10/8; os 4 prompts capturados da tela Prompts; preços para os modelos usados, gravados com o nome do modelo exatamente como aparece no uso e marcados na setting `demo: true`.
  - `nobred --seed-demo` sem `--db` usa `$XDG_DATA_HOME/nobreai-cockpit/cockpit-demo.db` (nunca o banco real), roda `Seed` com `livePID = os.Getpid()` e sobe o servidor normalmente. Com a setting `demo: true`, a UI mostra uma faixa "Dados de demonstração" no topo (Task 13).

- [ ] **Step 1: Testes que falham**
  `TestSeedRefusesNonEmpty`: um card criado antes → erro com a mensagem exata.
  `TestSeedHeroCard`: depois do seed, o card "Migrar autenticação para OAuth2" tem 3 sessões, `Readiness.Met == 4` com as regras padrão e totais de entrada/saída/cache `820000/121000/141000`.
  `TestSeedLiveSessionsSurviveReconcile`: `board.Reconcile` com `/proc` real e `livePID=os.Getpid()` → nenhuma sessão encerrada.
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/demo/ -v`.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: dados de demonstração para conferir as telas`
