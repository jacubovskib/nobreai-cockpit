# Visão por projeto — Parte 2 (plano no card e cronograma): plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O agente registra o plano e as tarefas de uma entrega no card (`nobre plan`, `nobre task`), o núcleo cobra no fim do turno quando um plano foi escrito sem registro ou quando as tarefas ficaram para trás depois de um commit, e o Cockpit mostra o plano no card, os blocos de progresso na página Projetos e no cronograma, com linha de contexto e estado.

**Architecture:** O plano mora no card: `cards.plan_path` e a tabela `plan_tasks` (migração 021). O plano entra em `domain.Content`, e daí chega sozinho à prontidão, ao pacote de retomada, ao `nobre status`, ao `CardDetail` e ao `CardSummary`. Quem cobra é o núcleo agente-neutro (`internal/agentcore`, `turnEnd`), nunca um driver. Ele olha os eventos `file_edit` e `command` que a sessão já grava, então não há gancho novo no `ToolAfter`. A web ganha uma seção Plano no card, um componente de blocos, a linha de contexto no cronograma e os padrões de arquivo de plano em Configuração.

**Tech Stack:** Go 1.26 (SQLite, `net/http`), React + React Router + TanStack Query + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-10-visao-por-projeto-design.md` (seções 5 e 6, e o cronograma da seção 4). Card NAC-5. A Parte 1 está em `docs/plan-visao-projeto.md`.

## Desvios da spec (para o usuário conferir)

1. **A cobrança fica no núcleo, não no adaptador do Claude.** Desde o NAC-42 as regras de hook vivem em `internal/agentcore`, e Claude e Kiro são drivers. A spec, anterior a isso, cita `internal/adapter/claude/hooks.go`.
2. **A detecção vem dos eventos que já existem, não de um gancho novo no `PostToolUse`.** No fim do turno, o núcleo compara os caminhos de `file_edit` da sessão com os padrões. O efeito é o mesmo de "o daemon guarda que esta sessão escreveu um plano", sem estado novo.
3. **Os padrões iniciais vão além de `docs/plan*.md`.** São `docs/plan*.md`, `docs/superpowers/plans/*.md` e `.kiro/specs/*/tasks.md`. O segundo é onde o fluxo atual deste repositório grava os planos. No Kiro, `tasks.md` é o plano de implementação, e `requirements.md` e `design.md` são a spec. Por isso a regra "specs ficam de fora" continua valendo.
4. **Um plano escrito sem card dispara o lembrete "sem card" mesmo abaixo do limite de arquivos.** Um plano já indica trabalho com histórico. Quando o lembrete "sem card" já saiu antes de o plano ser escrito, vem depois um único lembrete curto, só do plano.
5. **A skill vale para os dois agentes.** O texto novo vai em `internal/adapter/claude/skill.md` e em `internal/adapter/kiro/steering.md`.
6. **A linha de contexto de uma entrega concluída é "concluída dd/mm"**, e não "merge dd/mm". O Cockpit não sabe quando houve merge.
7. **"O prompt de handoff lista as tarefas em aberto" é entendido como o prompt padrão de retomada** (`retomar-card`, que traz `{{card.pacote}}`). O `fechar-handoff`, que o fim de turno usa, não lista as tarefas. O lembrete de `nobre task` cobre esse caso.

## Global Constraints

- Valem as constraints de `docs/plan.md`. Toda a UI e as mensagens do CLI em pt-BR.
- Nunca tocar o `~/.claude` real, o `systemctl` nem o `nobred` instalado (porta 7420). Testes em `t.TempDir()` e `httptest`.
- Datas no SQL são comparadas com `julianday(col)`, nunca como texto.
- Migração nova: `internal/store/migrations/021_plan_tasks.sql`. A última na `main` é a 020. Antes de criar, confira de novo com `git worktree list` e `ls internal/store/migrations` na `main`.
- Regras de hook só em `internal/agentcore`. Os adaptadores (`internal/adapter/claude`, `internal/adapter/kiro`) não ganham regra nova. Toda regra nova de fim de turno também é exercitada pela bateria `internal/agentcore/contracttest`.
- Os lembretes novos são só de fim de turno. O reconciliador (fim de sessão sem hook) não muda. `nobre memorias` não traz regras deste projeto hoje.
- Lembretes nunca prendem o agente em loop. A continuação de um turno segurado (`StopActive`) nunca é segurada, e cada lembrete sai no máximo uma vez por condição.
- Estados de tarefa gravados: `a_fazer`, `em_andamento`, `em_revisao`, `concluida`. Rótulos: "a fazer", "em andamento", "em revisão", "concluída". No CLI: `a-fazer`, `andamento`, `revisao`, `concluida` (os valores gravados também valem).
- O Cockpit não lê nem interpreta o markdown do plano nem o `progress.md`. Quem informa as tarefas é o agente.
- Padrões de plano: casados com `path.Match` sobre o caminho **relativo ao projeto da sessão**, com barras. Sem `**`. Caminho absoluto nunca casa: fora do projeto da sessão (por exemplo, a raiz do monorepo vista de um sub-projeto), não há cobrança. Lista vazia desliga a cobrança de plano.
- Cor nunca é a única informação: os blocos de progresso sempre trazem o texto "N/M" ao lado e um `title` em cada bloco.
- Commits terminam com a linha `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **`nobre plan` de novo com uma tarefa inserida no meio**: as tarefas de mesmo título mantêm o estado e a data, a nova entra como `a_fazer` e a numeração segue a nova ordem (teste na Task 1).
2. **Agente que nunca registra o plano**: o lembrete sai uma vez por sessão, e a continuação (`StopActive`) e os fins de turno seguintes não seguram de novo (teste na Task 3).
3. **Plano escrito com "Onde parei" em dia, ou com `IncompleteAction = "mark"`**: o turno é segurado só com a linha do plano. A regra do handoff não abafa a do plano, nem o contrário (teste na Task 3).
4. **Commit escrito de outras formas** (`rtk git commit -m x`, `git -C /w/api commit -am x`, `cd x && git commit`) conta como commit. `git log --grep commit` e um commit com `exit_code` 1 não contam (testes na Task 3).
5. **Card com todas as tarefas concluídas, ainda em Em andamento**: não há lembrete de `nobre task`, e o cronograma mostra "tarefas concluídas" (testes nas Tasks 3 e 4).

---

### Task 1: Domínio e store — plano, tarefas e padrões

**Files:**
- Create: `internal/store/migrations/021_plan_tasks.sql`, `internal/domain/plan.go`, `internal/domain/plan_test.go`, `internal/store/plan.go`, `internal/store/plan_test.go`
- Modify: `internal/domain/readiness.go` (`Content`, `Settings`, `DefaultSettings`, regra `todo_open`), `internal/store/content.go` (`CardContent`), `internal/resume/package.go` (`BuildIn`), `internal/api/config.go` (validação do PUT)
- Test: `internal/domain/readiness_test.go`, `internal/resume/package_test.go`, `internal/api/config_test.go`

**Interfaces:**
- Produces:
  - Migração 021: `ALTER TABLE cards ADD COLUMN plan_path TEXT NOT NULL DEFAULT ''`; `ALTER TABLE cards ADD COLUMN plan_updated_at TEXT`; tabela `plan_tasks(id INTEGER PRIMARY KEY AUTOINCREMENT, workspace_id TEXT NOT NULL DEFAULT 'local', owner_id TEXT NOT NULL DEFAULT 'me', card_id INTEGER NOT NULL REFERENCES cards(id), n INTEGER NOT NULL, title TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'a_fazer', session_id TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, UNIQUE(card_id, n))`; em `sessions`, `plan_reminded INTEGER NOT NULL DEFAULT 0` e `task_reminded_for TEXT NOT NULL DEFAULT ''`. `plan_path` **não** é único: o mesmo arquivo pode estar em vários cards.
  - `type TaskStatus string` com `TaskTodo = "a_fazer"`, `TaskDoing = "em_andamento"`, `TaskReview = "em_revisao"`, `TaskDone = "concluida"`; `func ParseTaskStatus(s string) (TaskStatus, bool)`; `func (s TaskStatus) Label() string`.
  - `type PlanTask struct { N int \`json:"n"\`; Title string \`json:"title"\`; Status TaskStatus \`json:"status"\`; SessionID string \`json:"session_id"\`; UpdatedAt time.Time \`json:"updated_at"\` }`
  - `type Plan struct { Path string \`json:"path"\`; Tasks []PlanTask \`json:"tasks"\` }` e `func (p *Plan) Open() int` (tarefas não concluídas; 0 com `p` nil).
  - `Content` ganha `Plan *Plan \`json:"plan"\``: nil quando o card não tem tarefa (o card "tem plano" se e só se `Plan != nil`).
  - `Settings` ganha `PlanPatterns []string \`json:"plan_patterns"\``. O padrão é `[]string{"docs/plan*.md", "docs/superpowers/plans/*.md", ".kiro/specs/*/tasks.md"}`.
  - `func MatchPlan(patterns []string, rel string) bool` (`internal/domain/plan.go`).
  - `func (s *Store) SetPlan(cardID int64, path string, titles []string, sessionID string, at time.Time) (domain.Plan, error)`
  - `func (s *Store) SetTaskStatus(cardID int64, n int, st domain.TaskStatus, sessionID string, at time.Time) (domain.PlanTask, error)`: `ErrNotFound` se a tarefa `n` não existe.
  - `func (s *Store) LastPlanChange(cardID int64) (*time.Time, error)`: o maior entre `plan_updated_at` e os `updated_at` das tarefas; nil sem plano.

- [ ] **Step 1: Write the failing tests**
  - `TestParseTaskStatus` (tabela): `"andamento"→TaskDoing`, `"em_andamento"→TaskDoing`, `"a-fazer"→TaskTodo`, `"revisao"→TaskReview`, `"concluida"→TaskDone`, `"feito"→(_, false)`, `""→(_, false)`. `TaskReview.Label() == "em revisão"`.
  - `TestMatchPlan`: com o padrão de `DefaultSettings`, casam `docs/plan-visao.md`, `docs/plan.md`, `docs/superpowers/plans/2026-11-24-x.md` e `.kiro/specs/login/tasks.md`; não casam `docs/superpowers/specs/x-design.md`, `.kiro/specs/login/design.md`, `/w/api/docs/plan.md` (absoluto) nem `web/docs/plan.md`. Com `nil`, nada casa.
  - `TestSetPlanReplacesByTitle` em `internal/store/plan_test.go`: `SetPlan(c, "docs/p.md", ["A","B","C"], "s1", t0)`; `SetTaskStatus(c, 2, TaskDone, "s1", t0+1h)`; `SetPlan(c, "docs/p.md", ["A","X","B"], "s2", t0+2h)`. O resultado é `[{1,A,a_fazer,t0}, {2,X,a_fazer,t0+2h}, {3,B,concluida,t0+1h}]` e "C" some. `LastPlanChange == t0+2h`.
  - `TestSetPlanDuplicateTitles`: `["A","A"]` sobre `["A"]` concluída dá `[{1,A,concluida},{2,A,a_fazer}]` (cada tarefa antiga é usada uma vez só, na ordem).
  - `TestSetTaskStatusUnknown`: a tarefa 9 de um plano de 3 devolve `ErrNotFound`.
  - `TestCardContentPlan`: um card sem tarefas tem `ct.Plan == nil`; depois de `SetPlan`, tem `ct.Plan.Path == "docs/p.md"` e as tarefas em ordem de `n`.
  - `TestReadinessOpenTaskSatisfiesTodo` em `readiness_test.go`: sem `Todos` e com uma tarefa `a_fazer`, o item `todo_open` fica OK; com todas as tarefas `concluida`, fica pendente.
  - `TestBuildInPlan` em `package_test.go`: com o plano `docs/p.md` (1 concluída, 1 em andamento, 1 a fazer), o pacote tem exatamente a seção abaixo, logo depois de "Feito". Sem plano, não há seção "Plano".
    ```
    ## Plano
    docs/p.md · 1 de 3 concluídas
    - 2. Núcleo — em andamento
    - 3. Web — a fazer
    ```
    É por aqui que o prompt de handoff padrão (`retomar-card`, com `{{card.pacote}}`) passa a listar as tarefas em aberto: não há variável de prompt nova.
  - Em `config_test.go`: o PUT com `plan_patterns: ["docs/[plan.md"]` devolve 400 "padrão de plano inválido: docs/[plan.md". Com `["/abs/*.md"]`, também 400. Com `[]`, grava, e o GET devolve `[]`, não o padrão. Um PUT sem a chave `plan_patterns`, ou com `null`, mantém a lista anterior.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ ./internal/store/ ./internal/resume/ ./internal/api/ -run 'TaskStatus|MatchPlan|SetPlan|SetTaskStatus|CardContentPlan|OpenTask|BuildInPlan|PlanPattern'`
Expected: FAIL (símbolos indefinidos)

- [ ] **Step 3: Implement** a migração, `plan.go` no domínio e no store, o carregamento em `CardContent`, a regra `todo_open` (OK com um `Todo` aberto **ou** `ct.Plan.Open() > 0`, sem mudar o rótulo) e a seção "Plano" do pacote.
  - `SetPlan` roda numa transação. Para cada título novo, na ordem, ele reaproveita a primeira tarefa antiga de mesmo título ainda não usada, com estado, `session_id` e `updated_at`. Se não houver, cria a tarefa como `a_fazer` com `updated_at = at`. As antigas que sobram são apagadas. `n` é renumerado de 1 em diante, e o comando grava `plan_path` e `plan_updated_at = at`.
  - Os títulos chegam sem espaços nas pontas e nunca vazios. Título vazio ou lista vazia → erro `store.ErrInvalid` (crie-o se não existir, e mapeie para 400 em `writeStoreError`).
  - No PUT de configurações (`putSettings`), `plan_patterns` segue o padrão das outras fatias. Chave ausente ou `null` mantém o valor atual, e só um `[]` explícito desliga. Clone a fatia em `old`, como `ContextWarnPcts`, para o diff do log de configuração não perder a troca.
  - Validação no PUT de configurações: cada padrão sem espaços nas pontas, não vazio, não absoluto, e com `path.Match(p, "")` sem `path.ErrBadPattern`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/domain/ ./internal/store/ ./internal/resume/ ./internal/api/`
Expected: PASS

- [ ] **Step 5: Commit** — `feat(store): plano e tarefas no card (NAC-5)`

### Task 2: API, CLI e skill — `nobre plan` e `nobre task`

**Files:**
- Modify: `internal/agentcore/start.go` (`commandsGuidance`: depois de `nobre todo add "<texto>";`, acrescente `nobre plan <arquivo> --task "<tarefa>" ... ao terminar um plano; nobre task <n> <estado> a cada tarefa;`; ajuste os testes que comparam o texto exato), `internal/api/me.go` (rotas e handlers), `internal/api/detail.go` (`CardSummary`, `cardSummary`), `internal/domain/column.go` (eventos), `internal/client/client.go` (se a forma de `Me` pedir), `cmd/nobre/agent.go`, `cmd/nobre/main.go` (`usage`, `run`, `helpFor`), `internal/agentcore/nobrecmd.go` (`handoffSubcommands`), `internal/adapter/claude/skill.md`, `internal/adapter/kiro/steering.md`
- Test: `internal/api/me_test.go`, `cmd/nobre/agent_test.go`, `internal/agentcore/nobrecmd_test.go`

**Interfaces:**
- Consumes: `store.SetPlan`, `store.SetTaskStatus`, `domain.ParseTaskStatus`, `Content.Plan` (Task 1).
- Produces:
  - `POST /api/me/plan` com `{"path": string, "tasks": []string}` → 200 com `domain.Plan`. Sem tarefas → 400 "informe ao menos uma tarefa". O handler grava o evento `domain.EvPlan = "plan"` com `{"path", "tasks": n}` e publica `cards`.
  - `POST /api/me/task` com `{"n": int, "status": string}` → 200 com `domain.PlanTask`. Estado desconhecido → 400 "estado inválido: use a-fazer, andamento, revisao ou concluida". Tarefa inexistente → 404 "tarefa <n> não existe no plano do card". O handler grava o evento `domain.EvTask = "task"` com `{"n", "status"}`.
  - `CardSummary` ganha `Plan *domain.Plan \`json:"plan"\`` (o mesmo `ct.Plan` que `cardSummary` já carrega).
  - CLI: `nobre plan <arquivo> --task "<título>" [--task ...]` imprime "Plano registrado: N tarefas."; `nobre task <n> <estado>` imprime "Tarefa N: <rótulo>.". `nobre status` lista o plano como o pacote de retomada faz.
  - `handoffSubcommands` inclui `"plan"` e `"task"`: a pausa por limite deixa passar os dois.

- [ ] **Step 1: Write the failing tests**
  - `TestMePlanAndTask` em `me_test.go`: numa sessão ligada a um card, `POST /api/me/plan {"path":"docs/p.md","tasks":["A","B"]}` → 200. `POST /api/me/task {"n":1,"status":"andamento"}` → 200 com `status == "em_andamento"`. `GET /api/cards` traz `plan.tasks[0].status == "em_andamento"`. Há um evento `plan` e um evento `task` no card. `{"n":9,...}` → 404 com a mensagem acima. `{"status":"feito"}` → 400. `{"tasks":[]}` → 400. Numa sessão sem card, a resposta segue a de `nobre todo` sem card (a mesma de `withSessionCard`).
  - `TestCLIPlan` em `agent_test.go` (siga o padrão dos testes de `todo`/`decide` com servidor falso): `nobre plan docs/p.md --task A --task "B c"` manda `{"path":"docs/p.md","tasks":["A","B c"]}` e imprime "Plano registrado: 2 tarefas.". `nobre plan docs/p.md` sem `--task` sai com o erro de uso, sem chamar o daemon. `nobre task 2 revisao` manda `{"n":2,"status":"revisao"}`. `nobre task x revisao` dá erro de uso.
  - Em `nobrecmd_test.go`: `plainNobreCommand("nobre task 2 concluida")` e `plainNobreCommand(\`nobre plan docs/p.md --task "A"\`)` são `true`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/api/ ./cmd/nobre/ ./internal/agentcore/ -run 'MePlan|CLIPlan|PlainNobre'`
Expected: FAIL

- [ ] **Step 3: Implement** handlers, eventos, `CardSummary.Plan`, comandos e textos de uso. `--task` repetível com um `flag.Value` de lista em `cmd/nobre/agent.go`. O `ParseTaskStatus` fica no servidor, e o CLI só repassa o texto.
- [ ] **Step 4: Update the agent guide** em `skill.md` e `steering.md`, com o mesmo texto nos dois (só mude o que já é diferente entre eles). Na lista "Comandos", logo depois de `nobre todo`:
  - `` `nobre plan docs/plan-oauth.md --task "callback" --task "refresh token"`: liga o plano ao card e registra as tarefas em ordem; rode de novo quando o plano mudar (as tarefas de mesmo título mantêm o estado). ``
  - `` `nobre task 2 andamento`: muda o estado da tarefa 2 (`a-fazer`, `andamento`, `revisao`, `concluida`). ``

  Em "Como manter o card útil", acrescente: "Ao terminar de escrever um plano de implementação, registre as tarefas com `nobre plan` (a spec não é plano). A cada tarefa iniciada, mandada para revisão ou concluída, rode `nobre task`. A tarefa é um passo do plano; `nobre todo` é uma pendência solta, fora do plano."
- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/api/ ./cmd/nobre/ ./internal/agentcore/ ./internal/adapter/...`
Expected: PASS (inclusive testes que comparem a skill instalada com o arquivo).

- [ ] **Step 6: Commit** — `feat(cli): nobre plan e nobre task (NAC-5)`

### Task 3: Núcleo — cobrança de plano e de tarefas no fim do turno

**Files:**
- Modify: `internal/agentcore/stop.go` (`turnEnd`), `internal/store/sessions.go`, `internal/domain/plan.go` (`IsGitCommit`), `internal/agentcore/contracttest/contract.go`
- Test: `internal/agentcore/stop_test.go`, `internal/domain/plan_test.go`, `internal/store/plan_test.go`

**Interfaces:**
- Consumes: `domain.MatchPlan`, `Settings.PlanPatterns`, `store.LastPlanChange`, `Content.Plan` (Task 1).
- Produces:
  - `func IsGitCommit(cmd string) bool` (`internal/domain/plan.go`). Separa o comando em `&&`, `||`, `;` e `|`, e procura em cada parte o token `git`. Depois dele, pula as opções (`-C` e `-c` levam o argumento seguinte) e o próximo token tem de ser `commit`.
  - `func (s *Store) SessionEditedPaths(sessionID string) ([]string, error)`: os caminhos distintos de `file_edit`, na ordem da primeira edição.
  - `func (s *Store) SessionCommittedAfter(sessionID string, after time.Time) (bool, error)`: há um evento `command` da sessão depois de `after`, com `IsGitCommit` e `exit_code` ausente ou 0. Uma linha de comando que também roda `nobre task` ou `nobre plan` (por exemplo, `git commit -m x && nobre task 2 concluida`) **não** conta. A tarefa muda no meio do comando, e o evento só é gravado no fim, com horário posterior.
  - `func (s *Store) MarkPlanReminded(id string) error`, `func (s *Store) SessionPlanReminded(id string) (bool, error)`, `func (s *Store) TaskRemindedFor(id string) (string, error)`, `func (s *Store) SetTaskRemindedFor(id, v string) error`.
- Textos exatos (`<p>` = o primeiro caminho editado que casa com um padrão; `<KEY>` = a chave do card):
  - Plano sem registro: `NobreAI Cockpit: você escreveu um plano (<p>) e o card <KEY> ainda não tem as tarefas. Registre com nobre plan <p> --task "<tarefa 1>" --task "<tarefa 2>" ...`
  - Tarefas para trás: `NobreAI Cockpit: houve commit desde a última atualização das tarefas do card <KEY>. Atualize com nobre task <n> andamento|revisao|concluida.`
  - Sem card, linha acrescentada ao lembrete de hoje (depois de `\n`): `Inclui um plano (<p>): crie o card com nobre start "<título>" e registre as tarefas com nobre plan.`
  - Sem card, depois que o lembrete de hoje já saiu: `NobreAI Cockpit: esta sessão escreveu um plano (<p>) sem card. Crie o card com nobre start "<título>" e registre as tarefas com nobre plan.`
  - O lembrete de hoje passa a dizer "1 arquivo" quando `n == 1` ("%d arquivos" nos outros casos, como hoje).

Regras de `turnEnd` (o começo continua igual: `StopActive` ou sem captura completa → nada):
- **Com card**, junte com `"\n\n"`, nesta ordem, as partes que valerem, e segure o turno se houver alguma:
  1. O prompt de handoff, nas mesmas condições de hoje (`IncompleteAction == "ask_agent"`, regra `where_stopped` ligada e edição depois do "Onde parei"). Essas condições não desligam as partes 2 e 3.
  2. Plano sem registro: há padrões, a sessão editou um caminho que casa, `ct.Plan == nil` e a sessão ainda não foi lembrada disso. A parte marca `MarkPlanReminded`.
  3. Tarefas para trás: `ct.Plan.Open() > 0`, `last := LastPlanChange`, `SessionCommittedAfter(sess, *last)` e `TaskRemindedFor(sess) != fmtTime(*last)`. A parte grava `SetTaskRemindedFor(sess, fmtTime(*last))`. Assim, ela volta só depois de uma nova mudança de tarefa seguida de outro commit.
- **Sem card**, como hoje, só com `AgentCreatesCards` e `ReminderEditThreshold > 0`. Sendo `plan` o primeiro caminho editado que casa (ou `""`):
  - Se a sessão ainda não foi lembrada e `n >= ReminderEditThreshold || plan != ""`, sai o lembrete de hoje (com a linha do plano, se houver). Ele marca `MarkReminded` e, com plano, também `MarkPlanReminded`.
  - Se já foi lembrada, há `plan` e a sessão não foi lembrada do plano, sai o lembrete curto do plano e ela é marcada com `MarkPlanReminded`.

- [ ] **Step 1: Write the failing tests**
  - `TestIsGitCommit` (tabela): `true` para `git commit -m x`, `rtk git commit -m x`, `git -C /w/api commit -am x`, `cd x && git commit`, `git -c user.name=a commit`; `false` para `git log --grep commit`, `git status`, `grep -r "git commit" docs` e `""`.
  - `TestSessionCommittedAfter`: um `command` `git commit -m x` com `exit_code` 1 não conta; com 0 ou sem `exit_code`, conta; antes de `after`, não conta.
  - Em `stop_test.go` (use `cardSession` e os helpers de lá):
    - `TestTurnEndPlanReminderOnce`: card sem plano, edição em `/w/api/docs/superpowers/plans/p.md`, "Onde parei" atualizado pelo store logo depois (para não ter handoff pendente), `TurnEnd` → `HoldTurn.Reason` é exatamente o texto de plano sem registro, com `<p> = docs/superpowers/plans/p.md`. O `TurnEnd` seguinte e o `StopActive` não seguram.
    - `TestTurnEndPlanReminderWithMark`: com `IncompleteAction = "mark"` e o mesmo cenário, segura só com a linha do plano.
    - `TestTurnEndJoinsHandoffAndPlan`: edição do plano sem `note` depois → o motivo começa com o prompt de handoff, e o texto do plano vem depois de `"\n\n"`.
    - `TestTurnEndNoPlanReminderWhenRegistered`: com `SetPlan` já feito no card, a edição do plano não gera lembrete.
    - `TestTurnEndTaskReminder`: com plano registrado em t0, um `command` `git commit -m x` em t0+1m e `TurnEnd` → texto de tarefas para trás. O segundo `TurnEnd` não segura. `SetTaskStatus` em t0+2m, outro commit em t0+3m e `TurnEnd` → segura de novo. Com todas as tarefas concluídas e um commit novo, não segura. Um `command` `git commit -m x && nobre task 2 concluida` depois da última mudança não segura.
    - `TestTurnEndNoCardPlanBypassesThreshold`: sem card, uma única edição de `docs/plan-x.md` → `NobreAI Cockpit: esta sessão já editou 1 arquivo sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.\nInclui um plano (docs/plan-x.md): crie o card com nobre start "<título>" e registre as tarefas com nobre plan.`
    - `TestTurnEndNoCardPlanAfterReminder`: três edições comuns e `TurnEnd` (lembrete de hoje, sem linha de plano), depois a edição de `docs/plan-x.md` e `TurnEnd` → o lembrete curto do plano. O terceiro `TurnEnd` não segura.
    - `TestTurnEndPlanPatternsEmpty`: com `PlanPatterns = []`, nada disso dispara; o lembrete de 3 arquivos continua como hoje.
    - O `TestTurnEndRemindsOnceWithoutCard` de hoje continua passando sem mudança.
  - Na bateria (`contract.go`), cenários novos `"Stop cobra plano escrito sem registro"` e `"Stop sem card cita o plano"`. Eles gravam `PlanPatterns = []string{fx.EditPath}` nas configurações do banco do cenário (cada cenário tem banco próprio), chamam `EditAfter` e `Stop` e, com `DecodeHold`, conferem que o motivo contém `nobre plan`. Pule os cenários quando `DecodeHold` for nil, como o de handoff já faz.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ ./internal/store/ ./internal/agentcore/... ./internal/adapter/...`
Expected: FAIL nos testes novos

- [ ] **Step 3: Implement** as funções do store, `IsGitCommit` e a nova `turnEnd`. Extraia as partes 2 e 3 em `func (c *Core) planReminders(sess domain.Session, card domain.Card, ct domain.Content, set domain.Settings) ([]string, error)` para a `turnEnd` continuar legível. `fmtTime` é o formatador do store; exponha o que precisar.
- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS (Claude e Kiro rodam a bateria nova)

- [ ] **Step 5: Commit** — `feat(agentcore): cobra plano e tarefas no fim do turno (NAC-5)`

### Task 4: Web — seção Plano, blocos, cronograma e Configuração

**Files:**
- Create: `web/src/ui/PlanBlocks.tsx`, `web/src/ui/PlanBlocks.test.tsx`, `web/src/pages/card/Plan.tsx`
- Modify: `web/src/api/types.ts`, `web/src/pages/CardPage.tsx`, `web/src/pages/projects.ts`, `web/src/pages/Projects.tsx`, `web/src/pages/ProjectPage.tsx`, `web/src/pages/Config.tsx` (`CardCreation`)
- Test: `web/src/pages/CardPage.test.tsx`, `web/src/pages/projects.test.ts`, `web/src/pages/Projects.test.tsx`, `web/src/pages/Config.test.tsx`

**Interfaces:**
- Consumes: `CardDetail.content.plan`, `CardSummary.plan`, `Settings.plan_patterns` (Tasks 1 e 2).
- Produces:
  - `types.ts`: `type TaskStatus = 'a_fazer' | 'em_andamento' | 'em_revisao' | 'concluida'`, `interface PlanTask { n: number; title: string; status: TaskStatus; session_id: string; updated_at: string }`, `interface Plan { path: string; tasks: PlanTask[] }`, `Content.plan: Plan | null`, `CardSummary.plan: Plan | null`, `Settings.plan_patterns: string[]`.
  - `export const TASK_LABEL: Record<TaskStatus, string>` (rótulos da Global Constraints) em `PlanBlocks.tsx`.
  - `export function PlanBlocks({ tasks }: { tasks: PlanTask[] })`: o texto "N/M concluídas", seguido de um bloco por tarefa, em ordem. Cores: `#0CA30C` para concluída, `#FAB219` cheio para em andamento, `#FAB219` só na borda para em revisão, `var(--border)` para a fazer. Cada bloco tem `title="Tarefa n: título — rótulo"`. O conjunto tem `role="img"` e `aria-label="Plano: N de M tarefas concluídas"`.
  - `export function contextLine(c: CardSummary): string` em `projects.ts`. A primeira regra que valer:
    - `concluido` → `concluída dd/mm` (de `updated_at`).
    - Sem plano → `''`.
    - Alguma tarefa `em_andamento` ou `em_revisao` → `tarefa n: título` da primeira, com ` (em revisão)` se for revisão.
    - Todas `a_fazer` → `plano escrito, não iniciado`.
    - Todas `concluida` → `tarefas concluídas`.
    - Senão → `próxima: tarefa n: título` (a primeira `a_fazer`).
  - `export function stateLabel(c: CardSummary): string` em `projects.ts`: `concluido` → "concluída"; sinal `parado` ou `aguardando_revisao` → `signalText(c)`; `backlog` → "backlog"; senão "em andamento".

- [ ] **Step 1: Write the failing tests**
  - `PlanBlocks.test.tsx`: com 4 tarefas (1 de cada estado), mostra "1/4 concluídas", 4 blocos com os `title` esperados e o `aria-label` "Plano: 1 de 4 tarefas concluídas".
  - `projects.test.ts`: um caso de `contextLine` para cada regra acima, inclusive "tarefa 2: filtros no board (em revisão)" e "tarefas concluídas" com o card em `em_andamento`. Um caso de `stateLabel` para cada estado.
  - `CardPage.test.tsx`: um card com plano mostra a seção "Plano", com o caminho em fonte mono e cada tarefa com número, título, rótulo e data. Sem plano: "Sem plano registrado. O agente registra com nobre plan."
  - `Projects.test.tsx`: a "Entrega atual" de um projeto cujo card tem plano mostra os blocos ("1/4 concluídas"). Sem plano, só o título.
  - Na página do projeto (no teste da página que já existir, ou num `ProjectPage.test.tsx` novo), cada entrega mostra `stateLabel` e `contextLine`, e a concluída mostra "concluída dd/mm".
  - `Config.test.tsx`: a seção "Criação de cards" mostra os padrões, um por linha, num `textarea` com o rótulo "Arquivos de plano". Salvar manda `plan_patterns` sem as linhas vazias. A dica é "Vazio desliga a cobrança de plano."

- [ ] **Step 2: Run them to verify they fail**

Run: `cd web && pnpm vitest run src/ui/PlanBlocks.test.tsx src/pages/projects.test.ts src/pages/CardPage.test.tsx src/pages/Projects.test.tsx src/pages/Config.test.tsx`
Expected: FAIL

- [ ] **Step 3: Implement.** A seção Plano entra no `CardPage` logo depois de "Falta" (`Todos`), no estilo de `card/shared`. No cronograma, cada item ganha o rótulo de estado, a linha de contexto (se não vazia) e `PlanBlocks` quando há plano.
- [ ] **Step 4: Run the checks**

Run: `cd web && pnpm vitest run && pnpm tsc --noEmit`
Expected: PASS, sem erros de tipo

- [ ] **Step 5: Commit** — `feat(web): plano no card, blocos e linha de contexto no cronograma (NAC-5)`

### Task 5: Projeto da sessão num lugar só e os minors da Parte 1

**Files:**
- Modify: `internal/store/usage.go`, `internal/api/usage.go` (`SessionWithUsage`, `listSessions`), `web/src/api/types.ts`, `web/src/pages/ControlRoom.tsx`, `web/src/pages/ProjectPage.tsx`, `web/src/pages/projects.ts`, `web/src/pages/Projects.tsx`, `web/src/pages/NewTask.tsx`, `web/src/pages/Analytics.tsx`
- Test: `internal/api/usage_test.go`, `web/src/pages/projects.test.ts`, `web/src/pages/Projects.test.tsx`, novos `web/src/pages/CardsList.test.tsx`, `web/src/pages/NewTask.test.tsx`, `web/src/ui/ProjectTag.test.tsx` e o teste de Prompts (globais + do projeto)

**Interfaces:**
- Produces:
  - `const sessionProjectSQL = "COALESCE(c.project_id, s.project_id)"` em `internal/store/usage.go`, usado nas três ocorrências de hoje. O comentário dele é a definição: "o projeto de uma sessão é o do card dela; sem card, o da própria sessão".
  - `SessionWithUsage` ganha `EffectiveProjectID *int64 \`json:"effective_project_id"\``, pela mesma regra (o projeto do card da sessão, se o card existe; senão `Session.ProjectID`). `types.ts` ganha `effective_project_id: number | null`.

- [ ] **Step 1: Before changing anything, confirm each minor still applies on the current `main`.** A borda âmbar dos cards parados, por exemplo, já existe em `Board.tsx`. Risque os que não valem mais e registre isso com `nobre note`. A lista, tirada do card:
  1. Testes que faltam: `CardsList` (coluna Projeto e filtro do seletor), `NewTask` (pré-seleção), `ProjectTag` (nome sempre visível e "Sem projeto"), e Prompts mostrando os globais + os do projeto.
  2. `NewTask`: a pré-seleção "gruda". Regra: enquanto o usuário não mexer no `select`, ele segue o seletor global; depois de uma escolha manual, a escolha fica.
  3. Borda âmbar nos cards parados (provavelmente já feita).
  4. A "Entrega atual" não mostra o texto do sinal: acrescente `signalText(current)` quando houver sinal.
  5. "Última atividade" só olha os cards: passe a considerar também o `last_event_at` das sessões do projeto, por `effective_project_id`. `buildRows` ganha o parâmetro `sessions: SessionWithUsage[]`.
  6. Projeto da sessão: `ControlRoom` (as duas etiquetas) e o `liveOf` de `ProjectPage` usam `effective_project_id ?? project_id`. A criação de card a partir de uma sessão sem card continua usando `project_id`.
  7. As concluídas do cronograma reordenam quando o card é editado: ordene por `created_at` crescente (a data de início), não por `updated_at`.
  8. O teste da página Projetos com `range(7)` pode virar o dia: congele o relógio com `vi.setSystemTime`.
  9. `Analytics.tsx` importa `../app/project` duas vezes: junte os dois imports.
- [ ] **Step 2: Write the failing tests** para cada item que vale:
  - `TestListSessionsEffectiveProject` em `usage_test.go`: uma sessão com `project_id = 1` ligada a um card do projeto 2 sai com `effective_project_id == 2`, e o `group=project` de `/api/usage/totals` põe o uso dela na chave `"2"`. Uma sessão sem card sai com `effective_project_id == project_id`.
  - `projects.test.ts`: a `lastActivity` de um projeto sem cards e com uma sessão é o `last_event_at` da sessão; as concluídas saem em ordem de `created_at`.
  - Os testes de componente dos itens 1, 2 e 4.
- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/api/ -run EffectiveProject && cd web && pnpm vitest run`
Expected: FAIL nos testes novos

- [ ] **Step 4: Implement** os itens que valem.
- [ ] **Step 5: Run the full suite**

Run: `go test ./... && cd web && pnpm vitest run && pnpm tsc --noEmit`
Expected: PASS

- [ ] **Step 6: Commit** — `fix: projeto da sessão num lugar só e minors da visão por projeto (NAC-5)`
