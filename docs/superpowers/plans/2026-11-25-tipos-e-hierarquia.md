# Tipos e hierarquia de cards — plano de implementação (NAC-55)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** o card ganha tipo (épico, história, tarefa, bug), pai (épico), prioridade, labels e story points; a coluna do épico é calculada pelas filhas; o board ganha filtros por épico, tipo e "Esperando você".

**Architecture:** regras puras no `internal/domain` (validação, coluna do épico, "esperando você"); o store grava os campos novos e recalcula o épico na mesma transação de todo caminho que muda coluna, tipo ou pai; a API aplica os campos por um helper único usado pela web e pelo agente; a web e o `nobre` só expõem.

**Tech Stack:** Go 1.26 + SQLite (`internal/store/migrations`), React + TanStack Query + Vitest (`web/`).

**Spec:** `docs/superpowers/specs/2026-11-25-tipos-e-hierarquia-design.md`

## Global Constraints

- Textos de UI, mensagens de erro, comentários e commits em pt-BR. Commits terminam com `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` e citam `(NAC-55)`.
- Valores fixos: tipos `epico`, `historia`, `tarefa`, `bug` (padrão `tarefa`); prioridades `mais_alta`, `alta`, `media`, `baixa`, `mais_baixa` (padrão `media`); `points` ≥ 0, aceita 0.5, NULL = sem estimativa; label sem espaço, não vazia, até 50 caracteres, guardada como digitada, duplicada no mesmo card ignorada.
- Rótulos na UI: Épico, História, Tarefa, Bug; Mais alta, Alta, Média, Baixa, Mais baixa.
- Só `epico` pode ser pai; épico não tem pai; filhas podem estar em qualquer projeto.
- O épico nunca vai para Pausado, nunca guarda branch, nunca é movido por sessão, `nobre done`, merge de branch ou limite de custo.
- Migração nova: `023_card_hierarchy.sql` (a última no `main` é a 022; renumerar se outra entrar antes do merge).
- Cards existentes ficam `tarefa`/`media`/sem pai/sem labels/sem points e se comportam exatamente como hoje.
- O hook não ganha consulta além de filha → épico; nada de rede.
- Rodar a suíte com `go vet ./... && go test ./...` e `pnpm -C web test --run` e `pnpm -C web build` (tsc).

## Review Focus

1. **Filha pausada pelo reconciliador (processo morto)**, não só pelo hook `sessionEnd`: o épico precisa ser recalculado nos dois caminhos — teste na Task 3.
2. **Filha trocando de épico** (ou saindo do último): os dois épicos recalculam; o que ficou sem filhas volta para Backlog — teste na Task 3.
3. **Banco real antigo**: migração sobre um banco com cards em todas as colunas, com branch, bloqueio e pausa por limite, sem perder nada — teste na Task 2.
4. **Board filtrado por um sub-projeto com épico no grupo** acima dele: o filtro de épico mostra o épico e só as filhas daquele projeto — teste na Task 7.
5. **Entrada suja nos campos**: label com espaço, label repetida, `points` negativo, tipo/prioridade desconhecidos, pai inexistente ou que não é épico, card como pai de si mesmo — mensagens claras na web (400) e no `nobre` — testes nas Tasks 1 e 4.

---

### Task 1: Regras do domínio

**Files:**
- Create: `internal/domain/hierarchy.go`, `internal/domain/hierarchy_test.go`
- Modify: `internal/domain/types.go:26-51` (campos do `Card`)

**Interfaces:**
- Produces:
  - `type CardType string` com `TypeEpico`, `TypeHistoria`, `TypeTarefa`, `TypeBug`; `var CardTypes = []CardType{...}`; `func ValidType(t CardType) bool`; `func TypeLabel(t CardType) string` ("Épico"…).
  - `type Priority string` com `PrioMaisAlta`, `PrioAlta`, `PrioMedia`, `PrioBaixa`, `PrioMaisBaixa`; `func ValidPriority(p Priority) bool`; `func PriorityLabel(p Priority) string`.
  - Campos novos em `Card`: `Type CardType \`json:"type"\``, `ParentID *int64 \`json:"parent_id"\``, `Priority Priority \`json:"priority"\``, `Points *float64 \`json:"points"\``, `Labels []string \`json:"labels"\``.
  - `func NormalizeLabels(in []string) ([]string, error)` — apara espaços nas pontas, remove duplicadas mantendo a ordem, erro se alguma tiver espaço interno, for vazia ou passar de 50 runas.
  - `func ValidPoints(p *float64) error` — nil ok; erro se < 0.
  - `type HierarchyCheck struct { Card Card; OldType CardType; Parent *Card; ChildCount int; Live bool; New bool }` e `func CheckHierarchy(h HierarchyCheck) error`.
  - `func EpicColumn(cur Column, children []Column) Column`.
  - `func EpicManualMove(cur, to Column, children []Column) (Column, error)`.
  - `type WaitingInput struct { Column Column; OpenQuestions int; HandoffIncomplete, Ready bool; PauseReason string; Signal Signal }` e `func WaitingOnYou(in WaitingInput) (bool, string)`.

- [ ] **Step 1: Escrever os testes que falham** em `hierarchy_test.go`:
  - `TestEpicColumn` (tabela): `(Backlog, nil)→Backlog`; `(EmAndamento, nil)→Backlog`; `(Concluido, nil)→Concluido`; `(Revisao, [Backlog,Backlog])→Backlog`; `(Backlog, [EmAndamento,Backlog])→EmAndamento`; `(Backlog, [Pausado])→EmAndamento`; `(EmAndamento, [Concluido,Concluido])→Revisao`; `(Concluido, [Concluido])→Concluido`; `(Concluido, [Concluido,Backlog])→EmAndamento`; `(Concluido, [Backlog])→Backlog`.
  - `TestEpicManualMove`: `(Revisao→Concluido, [Concluido])` ok → Concluido; `(Backlog→Concluido, nil)` ok; `(EmAndamento→Concluido, [Concluido,EmAndamento])` erro; `(Concluido→Backlog, [Concluido])` ok → Revisao (reabrir recalcula ignorando a linha "fica Concluído"); `(Concluido→EmAndamento, nil)` ok → Backlog; `(Backlog→Pausado, nil)` erro; `(Backlog→EmAndamento, [EmAndamento])` erro. Mensagem de erro: `"a coluna do épico segue as filhas"`.
  - `TestCheckHierarchy`: pai que não é épico → `"o pai precisa ser um épico"`; épico com pai → `"um épico não pode ter pai"`; card como pai de si mesmo (Parent.ID == Card.ID) → `"um card não pode ser pai de si mesmo"`; virar épico com pai → `"tire o card do épico antes"`; deixar de ser épico com `ChildCount=3` → `"remova as 3 filhas do épico antes"`; virar épico com `Live=true` e `New=false` → `"o card tem uma sessão ao vivo; encerre a sessão antes de transformá-lo em épico"`; tipo inválido → `"tipo inválido (use epico, historia, tarefa ou bug)"`; prioridade inválida → `"prioridade inválida (use mais_alta, alta, media, baixa ou mais_baixa)"`; caso válido → nil.
  - `TestNormalizeLabels`: `[" front ","front","api"]→["front","api"]`; `["a b"]` erro `"label sem espaços: \"a b\""`; `[""]` erro; 51 runas erro; `nil→nil`.
  - `TestValidPoints`: nil, 0, 0.5, 13 ok; -1 erro `"points não pode ser negativo"`.
  - `TestWaitingOnYou` (ordem do motivo = ordem abaixo): pergunta aberta fora de Concluído → `(true,"pergunta aberta")`, inclusive em EmAndamento; pergunta aberta em Concluído → false; Revisao → `"revisão"`; Pausado + HandoffIncomplete + !Ready → `"handoff incompleto"`; HandoffIncomplete com Ready → false; `PauseReason == PauseReasonLimit` → `"limite de custo"`; `Signal == SignalProntoParaRetomar` → `"pronto para retomar"`; nada disso → `(false,"")`.

- [ ] **Step 2:** `go test ./internal/domain/ -run 'Epic|Hierarchy|Labels|Points|Waiting'` → FAIL (símbolos indefinidos).
- [ ] **Step 3: Implementar** em `hierarchy.go`. `EpicColumn`: lido de cima para baixo — `cur==Concluido` e todas as filhas concluídas (ou nenhuma) → Concluido; sem filhas → Backlog; todas Backlog → Backlog; todas Concluido → Revisao; senão EmAndamento. `EpicManualMove`: `to==Concluido` só com todas concluídas ou sem filhas; `cur==Concluido && to!=Concluido` devolve `EpicColumn(Backlog, children)`; o resto é erro. `CheckHierarchy` valida, nesta ordem: tipo, prioridade, pai de si mesmo, pai não-épico, épico com pai (inclui "tire o card do épico antes" quando `OldType!=TypeEpico`), filhas ao deixar de ser épico, sessão ao vivo ao virar épico (só `!New`).
- [ ] **Step 4:** mesmo comando → PASS; `go test ./internal/domain/` inteiro → PASS.
- [ ] **Step 5: Commit** `feat(domain): tipos, prioridade e regras da hierarquia de cards (NAC-55)`.

---

### Task 2: Store — esquema, campos, labels e filhas

**Files:**
- Create: `internal/store/migrations/023_card_hierarchy.sql`, `internal/store/hierarchy_test.go`
- Modify: `internal/store/cards.go` (`cardCols`:24, `scanCard`:42, `createCardTx`:122, `UpdateCard`:214, `ListIntegrationCandidates`:196)

**Interfaces:**
- Consumes: campos de `domain.Card` da Task 1.
- Produces:
  - `func (s *Store) ListChildren(parentID int64) ([]domain.Card, error)` — ordenado por id.
  - `func (s *Store) CountChildren(parentID int64) (int, error)`.
  - `CreateCard`/`CreateCardAndAttach`/`UpdateCard` passam a gravar `type`, `parent_id`, `priority`, `points` e substituir as labels (apagar e inserir na mesma transação).
  - `UpdateCard` passa a rodar numa transação (a Task 3 pendura o recálculo nela).

- [ ] **Step 1: Migração** `023_card_hierarchy.sql`:
  ```sql
  ALTER TABLE cards ADD COLUMN type TEXT NOT NULL DEFAULT 'tarefa';
  ALTER TABLE cards ADD COLUMN parent_id INTEGER REFERENCES cards(id);
  ALTER TABLE cards ADD COLUMN priority TEXT NOT NULL DEFAULT 'media';
  ALTER TABLE cards ADD COLUMN points REAL;
  CREATE INDEX idx_cards_parent ON cards(parent_id);
  CREATE TABLE card_labels (
    workspace_id TEXT NOT NULL DEFAULT 'local',
    owner_id TEXT NOT NULL DEFAULT 'me',
    card_id INTEGER NOT NULL REFERENCES cards(id),
    label TEXT NOT NULL,
    UNIQUE(card_id, label)
  );
  ```
- [ ] **Step 2: Testes que falham** em `hierarchy_test.go`:
  - `TestCardHierarchyRoundTrip`: criar épico (`Type: TypeEpico`) e história com `ParentID=&epico.ID`, `Priority: PrioAlta`, `Points: ptr(0.5)`, `Labels: ["front","api"]`; `GetCard` devolve tudo igual; `UpdateCard` com `Labels: ["api"]` e `Points: nil` → `GetCard` traz `["api"]` e `Points==nil`.
  - `TestCardDefaults`: card criado sem os campos volta com `Type==TypeTarefa`, `Priority==PrioMedia`, `ParentID==nil`, `Labels` vazio.
  - `TestListChildren`: duas filhas em projetos diferentes aparecem em `ListChildren(epico.ID)`; `CountChildren`==2.
  - `TestIntegrationCandidatesSkipEpics`: épico com branch em Revisão não aparece em `ListIntegrationCandidates`.
  - `TestMigrationKeepsLegacyCards` (Review Focus 3): abrir um banco criado com migrações até 022 (padrão de `migrate_subprojects_test.go`), com cards em cada coluna, com branch, `blocked_by` e `pause_reason='limite de custo'`; após `Open` os cards mantêm coluna, branch, bloqueio e pausa, e ficam `tarefa`/`media`.
- [ ] **Step 3:** `go test ./internal/store/ -run 'Hierarchy|Defaults|ListChildren|SkipEpics|Legacy'` → FAIL.
- [ ] **Step 4: Implementar.** `cardCols` ganha `type, parent_id, priority, points, (SELECT group_concat(label, char(31)) FROM card_labels l WHERE l.card_id=cards.id ORDER BY rowid)` (todos os usos de `cardCols` são `FROM cards` sem alias); `scanCard` lê `sql.NullInt64`, `sql.NullFloat64`, `sql.NullString` e separa as labels por `\x1f`. `createCardTx` grava os campos (vazio → padrão) e as labels. `ListIntegrationCandidates` ganha `AND type<>'epico'`.
- [ ] **Step 5:** mesmo comando → PASS; `go test ./internal/store/` → PASS.
- [ ] **Step 6: Commit** `feat(store): tipo, pai, prioridade, points e labels no card (NAC-55)`.

---

### Task 3: Store — coluna do épico em todos os caminhos

**Files:**
- Create: `internal/store/epic.go`, `internal/store/epic_test.go`
- Modify: `internal/store/sessions.go:310-326` (`moveCardTx`), `internal/store/cards.go` (`UpdateCard`, `MoveCard`:300, `RecordIntegration`:279, `PauseForLimit`:310, `ResumeFromLimit`:322, `DoneFromLimit`:335)

**Interfaces:**
- Consumes: `domain.EpicColumn` (Task 1); `ListChildren` (Task 2).
- Produces:
  - `func recalcEpicTx(tx *sql.Tx, epicID int64) error` — lê a coluna do épico e das filhas, aplica `domain.EpicColumn`, grava se mudou (com `updated_at`).
  - `func recalcParentTx(tx *sql.Tx, cardID int64) error` — recalcula o pai do card, se houver.

- [ ] **Step 1: Testes que falham** em `epic_test.go` (usar sessões reais via `UpsertSession`/`AttachSession`/`EndSession` como em `sessions_test.go`):
  - `TestEpicFollowsChildren`: épico + filhas A (projeto 1) e B (projeto 2) → épico Backlog; `AttachSession` em A → épico EmAndamento; `EndSession` de A → A Pausado, épico EmAndamento; A e B para Concluido via `MoveCard` → épico Revisao; `MoveCard(épico, Concluido)` → Concluido; `MoveCard(A, Revisao)` → épico EmAndamento.
  - `TestEpicReconcilePath` (Review Focus 1): filha em andamento cuja sessão termina pelo caminho do reconciliador (`board.Reconcile` com PID morto, como nos testes existentes de reconcile em `internal/board`) → épico recalculado. Colocar este teste em `internal/board/` se o caminho exigir.
  - `TestEpicChildChangesParent` (Review Focus 2): filha em andamento muda de épico via `UpdateCard` → épico antigo, agora sem filhas, vai para Backlog; épico novo vai para EmAndamento.
  - `TestEpicIgnoresOwnSession`: `AttachSession` direto no épico → épico continua na coluna calculada; `EndSession` → idem.
  - `TestEpicIgnoresLimitsAndIntegration`: `PauseForLimit(épico)` devolve false e não muda; `RecordIntegration` no épico não o conclui.
  - `TestBecomeEpicRecalc`: tarefa em Pausado com `pause_reason`, `blocked_by` e branch vira épico via `UpdateCard` (a API zera os campos; aqui o teste passa já zerados) → coluna Backlog.
- [ ] **Step 2:** `go test ./internal/store/ ./internal/board/ -run Epic` → FAIL.
- [ ] **Step 3: Implementar.** `moveCardTx`: se o card é épico, não move (retorna nil); depois de mover uma filha, `recalcParentTx`. `UpdateCard`: dentro da transação, lê o `parent_id` antigo antes de gravar; depois recalcula o pai antigo, o novo e o próprio card se for épico. `MoveCard`: vira transação; se o card é épico, grava o `to` recebido (a API já validou com `EpicManualMove`) e não recalcula a si mesmo; senão `recalcParentTx`. `RecordIntegration` e os três de limite: `AND type<>'epico'` no UPDATE e `recalcParentTx` quando mudou (os de limite viram transação).
- [ ] **Step 4:** mesmo comando → PASS; `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(store): coluna do épico calculada pelas filhas (NAC-55)`.

---

### Task 4: API — campos, validação, resumo do épico e "esperando você"

**Files:**
- Create: `internal/api/hierarchy.go`, `internal/api/hierarchy_test.go`
- Modify: `internal/api/cards.go` (`cardInput`:112, `createCard`:121, `patchCard`:175), `internal/api/detail.go` (`CardSummary`, `CardDetail`, `cardSummary`, `cardDetail`)

**Interfaces:**
- Consumes: Task 1 (regras), Task 2 (`ListChildren`, `CountChildren`), store `HasLiveSession`, `SetCardBranch`.
- Produces:
  - `type hierarchyInput struct { Type *string \`json:"type"\`; ParentKey *string \`json:"parent_key"\`; Priority *string \`json:"priority"\`; Labels *[]string \`json:"labels"\`; Points json.RawMessage \`json:"points"\` }` — `parent_key: ""` remove o pai; `points: null` remove; ausente não muda.
  - `func (s *server) applyHierarchy(c *domain.Card, in hierarchyInput, isNew bool) (int, string)` — devolve `(0,"")` se ok ou (status HTTP, mensagem). Resolve o pai por `normKey`, monta `domain.HierarchyCheck` (com `CountChildren` e `HasLiveSession` para card existente), normaliza labels, valida points. Status: 404 para pai inexistente, 409 para sessão ao vivo ao virar épico, 400 para o resto. Ao virar épico, zera `PauseReason`, `HandoffIncomplete`, `BlockedBy` e `Branch` (card existente: `SetCardBranch(c.ID, "")`).
  - `type EpicProgress struct { Total int \`json:"total"\`; Done int \`json:"done"\` }`.
  - `CardSummary` ganha `ParentKey string \`json:"parent_key"\``, `ParentTitle string \`json:"parent_title"\``, `Children *EpicProgress \`json:"children"\``, `ChildrenTotals *store.Total \`json:"children_totals"\``, `TreeTotals *store.Total \`json:"tree_totals"\``, `WaitingOnYou bool \`json:"waiting_on_you"\``, `WaitingReason string \`json:"waiting_reason"\``.
  - `CardDetail` ganha `ParentKey`, `ParentTitle`, `Children *EpicProgress`, `ChildrenTotals`, `TreeTotals` e `ChildCards []CardSummary \`json:"child_cards"\`` (só épico; vazio = `[]`).
  - `func addTotals(a, b store.Total) store.Total` — soma tokens, custo, créditos e sessões; `Unpriced` é OU.

- [ ] **Step 1: Testes que falham** em `hierarchy_test.go` (usar o servidor de teste de `cards_test.go`):
  - `TestCreateEpicAndChild`: `POST /api/cards` com `type: "epico"` → 201; `POST` com `type:"historia", parent_key:"<épico>", priority:"alta", labels:["front"], points:3` → 201 e `GET` traz `parent_key`, `parent_title`.
  - `TestEpicOnGroupWithoutFolder`: `POST` em grupo sem pasta com `type:"epico"` → 201; com `type:"tarefa"` → 400 `msgGroupNoCards` (como hoje).
  - `TestHierarchyValidation` (Review Focus 5): labels `["a b"]`, `points:-1`, `type:"xyz"`, `priority:"urgente"`, `parent_key` inexistente (404 `"card NAC-999 não encontrado"`), pai não-épico, pai de si mesmo, épico com pai → 400 com as mensagens da Task 1.
  - `TestBecomeEpicRules`: `PATCH type:"epico"` em card com sessão viva → 409 com a mensagem da Task 1; sem sessão, card em Pausado com bloqueio e branch → 200, e o card volta em Backlog sem `blocked_by`, `pause_reason` nem `branch`.
  - `TestEpicManualMoveAPI`: épico com filha em andamento, `PATCH column:"concluido"` → 409 `"a coluna do épico segue as filhas"`; com todas concluídas → 200.
  - `TestEpicSummary`: épico com 3 filhas (1 concluída) em dois projetos, com uso registrado na filha e no épico → `children == {total:3, done:1}`, `children_totals.cost_usd` = soma das filhas, `tree_totals.cost_usd` = épico + filhas; `readiness.items` vazio para o épico; `GET /api/cards/<épico>` traz `child_cards` com 3 itens.
  - `TestWaitingOnYouAPI`: card em EmAndamento com sessão viva e pergunta aberta → `waiting_on_you:true, waiting_reason:"pergunta aberta"`; card em Revisão → `"revisão"`; card em Backlog sem nada → false.
- [ ] **Step 2:** `go test ./internal/api/ -run 'Epic|Hierarchy|Waiting'` → FAIL.
- [ ] **Step 3: Implementar.** `cardInput` embute `hierarchyInput`; `createCard` aceita grupo sem pasta quando `type=="epico"` (branch continua proibido) e chama `applyHierarchy(…, true)`. `patchCard` embute `hierarchyInput`, chama `applyHierarchy(…, false)` antes de gravar; se o card é (ou passa a ser) épico e veio `column`, usa `domain.EpicManualMove` com as colunas das filhas e não aplica a regra de sessão viva de Em andamento; mover para grupo sem pasta é aceito para épico. `cardSummary`/`cardDetail`: preencher pai (um `GetCardByID`), para épico `ListChildren` + progresso + totais (do mapa `byCard`), readiness com `Items` vazio e `Ready=false` para épico, e `WaitingOnYou` via `domain.WaitingOnYou` com o sinal calculado **antes** da supressão por sessão viva. `listCards` já tem `byCard`; passe-o ao cálculo dos totais das filhas em vez de consultar de novo.
- [ ] **Step 4:** mesmo comando → PASS; `go test ./internal/api/` → PASS.
- [ ] **Step 5: Commit** `feat(api): tipos, épico e esperando você na API da web (NAC-55)`.

---

### Task 5: API do agente, `nobre` e branch do épico

**Files:**
- Modify: `internal/api/me.go` (`meRoutes`:37, `meStart`:207, `meCards`:237, `meDone`:679, `meStatus`), `internal/board/branch.go:32` (`SyncCardBranch`), `internal/client/client.go` (`Start`:182, `AddCard`:383), `cmd/nobre/agent.go` (`start`, `card`), `cmd/nobre/main.go` (texto de ajuda), `internal/api/me_test.go`, `cmd/nobre/agent_test.go`

**Interfaces:**
- Consumes: `hierarchyInput`, `applyHierarchy` (Task 4).
- Produces:
  - `POST /api/me/card-set` (`withSession`): corpo `{ "key"?: string } + hierarchyInput`. Sem `key`, usa o card da sessão (sem card → 409 `msgNoCard`); com `key`, qualquer card. Devolve o card.
  - `type CardFields struct { Type, Priority string; ParentKey *string; Labels []string; Points *float64; ClearPoints bool }` em `internal/client` e `func (f CardFields) body() map[string]any`.
  - `func (c *Client) Start(ctx context.Context, title, goal, doneWhen string, f CardFields) (domain.Card, error)`.
  - `func (c *Client) AddCard(ctx context.Context, in map[string]string, f CardFields) (domain.Card, error)`.
  - `func (c *Client) CardSet(ctx context.Context, key string, f CardFields) (domain.Card, error)`.

- [ ] **Step 1: Testes que falham.**
  - `me_test.go`: `TestMeStartWithEpic` (`type:"historia", parent_key:<épico>` → card criado com pai e ligado à sessão; épico vai para EmAndamento); `TestMeCardsEpicPausedRefused` (`type:"epico", column:"pausado"` → 400 `"um épico não vai para Pausado; crie em backlog"`); `TestMeCardSetOtherCard` (`key:"NAC-x", parent_key:<épico>` → NAC-x ganha o pai); `TestMeDoneOnEpic` (409 `"NAC-n é um épico: a coluna do épico segue as filhas (nada mudou)"`, sem evento done); `TestEpicSkipsBranchSync` (`nobre start --type epico` numa sessão em branch de feature → épico com `branch==""`).
  - `agent_test.go`: `nobre start "x" --type historia --epic NAC-1 --priority alta --label front --label api --points 2` envia o corpo com esses campos; `nobre card set NAC-2 --no-epic` envia `parent_key:""`; `nobre card set --no-points` envia `points:null`; saída de `card set`: `"Card NAC-2 atualizado: História · Alta · épico NAC-1."` (partes ausentes omitidas).
- [ ] **Step 2:** `go test ./internal/api/ ./cmd/nobre/ -run 'Epic|CardSet|MeStartWith|MeCardsEpic|MeDoneOnEpic'` → FAIL.
- [ ] **Step 3: Implementar.** `meStart`/`meCards` embutem `hierarchyInput` e chamam `applyHierarchy(…, true)` antes de criar. `SyncCardBranch` devolve `(false, nil)` para épico. `meDone` recusa épico antes de qualquer outra checagem. Flags: `--type`, `--epic`, `--no-epic`, `--priority`, `--label` (repetível, `flag.Value` de lista), `--points`, `--no-points` em `start`, `card add` e no novo `card set [<KEY>]`; usage de `card` passa a listar `add` e `set`. `nobre status` mostra uma linha `Tipo: História · Prioridade: Alta · Épico: NAC-1 · Checkout novo` (prioridade só se ≠ média; épico mostra `Filhas: 4/7 concluídas`).
- [ ] **Step 4:** mesmo comando → PASS; `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(nobre): tipo, épico e card set pelo agente (NAC-55)`.

---

### Task 6: Contexto do agente — resume, início de sessão e skill

**Files:**
- Modify: `internal/resume/package.go:22` (`BuildIn`), `internal/resume/forcard.go`, `internal/agentcore/start.go` (`commandsGuidance`:43, lista de pausados:182), `internal/adapter/claude/skill.md`, `internal/adapter/kiro/steering.md`, testes em `internal/resume/` e `internal/agentcore/`

**Interfaces:**
- Consumes: `ListChildren`, `GetCardByID` (store); `domain.TypeLabel`, `domain.PriorityLabel`.
- Produces: `type Family struct { Epic *domain.Card; Children []domain.Card }` e `func BuildWith(c domain.Card, project string, ct domain.Content, sessions []domain.Session, totals []store.Total, diffStat string, fam Family) string`; `BuildIn` passa a chamar `BuildWith` com `Family{}`. `ForCard` monta a `Family`.

- [ ] **Step 1: Testes que falham.**
  - `TestBuildWithEpicParent`: história com épico → linha de meta começa com `História`; inclui `prioridade Alta` só quando ≠ média; logo após o cabeçalho, `Épico: NAC-60 · Checkout novo — <objetivo do épico em uma linha>`.
  - `TestBuildWithEpicChildren`: épico com 12 filhas → seção `## Filhas` com `- NAC-61 · História · Pagar com Pix · Em andamento` para as 10 primeiras e `… e mais 2`.
  - `TestBuildInUnchangedForTarefa`: card `tarefa`/`media` sem pai → saída idêntica à de hoje (proteção do anti-objetivo "quem não usa tipos não sente diferença"; o tipo `Tarefa` não aparece na meta).
  - `agentcore`: a lista de pausados mostra `(História)` depois da chave quando o tipo ≠ tarefa.
- [ ] **Step 2:** `go test ./internal/resume/ ./internal/agentcore/` → FAIL.
- [ ] **Step 3: Implementar.** Tipo na meta só quando ≠ `tarefa`. `commandsGuidance` ganha, antes de `nobre status`: `nobre card set [--type historia|bug|...] [--epic <KEY>] quando o card for parte de um épico;`. Na skill e no steering, uma seção "Épicos e tipos" com: use `--type`/`--epic` quando o usuário falar em épico/história ou o trabalho for parte de um épico existente; para quebrar um épico, `nobre card add "<título>" --epic NAC-60 --type historia` por filha; a sessão num épico é de planejamento (não se marca done, a coluna segue as filhas); nunca crie épicos nem mude o tipo de cards sem o usuário pedir. Conferir que o contexto injetado continua ≤ `maxContextRunes`.
- [ ] **Step 4:** mesmo comando → PASS; `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(agente): épico e filhas no resume e na skill (NAC-55)`.

---

### Task 7: Web — tipos, board e filtros

**Files:**
- Modify: `web/src/api/types.ts` (`Card`, `CardSummary`, `CardDetail`), `web/src/pages/boardModel.ts`, `web/src/pages/boardModel.test.ts`, `web/src/pages/Board.tsx`, `web/src/pages/Board.test.tsx`

**Interfaces:**
- Consumes: JSON da Task 4.
- Produces (em `boardModel.ts`):
  - `export type CardType = 'epico' | 'historia' | 'tarefa' | 'bug'`, `export type Priority = 'mais_alta' | 'alta' | 'media' | 'baixa' | 'mais_baixa'` (em `types.ts`), `TYPE_LABEL: Record<CardType,string>`, `PRIORITY_LABEL: Record<Priority,string>`.
  - `export interface BoardFilter { epic: string | 'none' | ''; type: CardType | ''; waiting: boolean }`.
  - `export function applyBoardFilter(cards: CardSummary[], f: BoardFilter): CardSummary[]` — `epic` = chave do épico: mantém o épico e as filhas (`parent_key`); `'none'` = sem pai e não-épico; `waiting` = `waiting_on_you`.
  - `export function epicOptions(cards: CardSummary[]): { key: string; title: string }[]`.

- [ ] **Step 1: Testes que falham.**
  - `boardModel.test.ts`: `applyBoardFilter` com épico NAC-60 e filhas → só o épico e as filhas; `'none'`; `type:'bug'`; `waiting:true`; combinação com a lista já filtrada por projeto (Review Focus 4: a lista de entrada já vem filtrada por `effectiveIds`, e o épico do grupo acima precisa estar nela — ver Step 3).
  - `Board.test.tsx`: tile de épico mostra `4/7` e o custo de `tree_totals` e não mostra "para handoff"; tile de filha mostra o chip `◆ Checkout novo` com link para o épico, `3 pts`, a prioridade quando ≠ média e as labels; botão `Esperando você · N` com N = cards visíveis com `waiting_on_you`; card `tarefa`/`media` sem pai renderiza como hoje.
- [ ] **Step 2:** `pnpm -C web test --run boardModel Board` → FAIL.
- [ ] **Step 3: Implementar.** No `Board.tsx`, o filtro de projeto (`useProjectFilter().ids`) passa a manter também os épicos cujo projeto é o grupo-pai de um projeto filtrado (use `familyIds`/o pai do projeto em `web/src/app/project.ts`), só quando há filtro de épico ativo ou o épico tem filha visível. Selects "Épico" e "Tipo" e o botão "Esperando você" ficam junto das pílulas de agente; estado na URL como os filtros atuais. Ícones: ◆ épico, ■ história, ● tarefa, ▲ bug; cor segue `var(--text-2)`, prioridade alta/mais alta em `var(--warn)`.
- [ ] **Step 4:** mesmo comando → PASS; `pnpm -C web test --run` e `pnpm -C web build` → PASS.
- [ ] **Step 5: Commit** `feat(web): tipos, épico e esperando você no board (NAC-55)`.

---

### Task 8: Web — página do card, nova tarefa e lista

**Files:**
- Create: `web/src/pages/card/Hierarchy.tsx`, `web/src/pages/card/EpicChildren.tsx`
- Modify: `web/src/pages/CardPage.tsx`, `web/src/pages/CardPage.test.tsx`, `web/src/pages/NewTask.tsx`, `web/src/pages/NewTask.test.tsx`, `web/src/pages/CardsList.tsx`, `web/src/pages/CardsList.test.tsx`

**Interfaces:**
- Consumes: `TYPE_LABEL`, `PRIORITY_LABEL`, `epicOptions` (Task 7); `PATCH /api/cards/{key}` com `type`, `parent_key`, `priority`, `labels`, `points` (Task 4).
- Produces: `export function Hierarchy({ card }: { card: CardDetail })` e `export function EpicChildren({ card }: { card: CardDetail })`.

- [ ] **Step 1: Testes que falham.**
  - `CardPage.test.tsx`: `Hierarchy` salva tipo/épico/prioridade/labels (campo de texto separado por vírgula ou espaço → lista)/points via PATCH com o corpo esperado; erro 400/409 aparece no `ErrorLine` com a mensagem do servidor; o seletor de épico lista os épicos não concluídos de qualquer projeto e oferece "sem épico"; página de épico mostra `EpicChildren` com chave, tipo, título, coluna, projeto, agente da última sessão e custo, o resumo `custo do épico · das filhas · total`, e o botão "Nova filha" (abre `NewTask` com o épico preenchido e projeto escolhível); página de épico não mostra o painel de prontidão.
  - `NewTask.test.tsx`: escolher tipo e épico envia `type` e `parent_key`; padrão envia `type:"tarefa"` sem `parent_key`; com `type:"epico"`, grupos sem pasta aparecem na lista de projetos.
  - `CardsList.test.tsx`: coluna Tipo e filtro por tipo.
- [ ] **Step 2:** `pnpm -C web test --run CardPage NewTask CardsList` → FAIL.
- [ ] **Step 3: Implementar** seguindo o padrão de `card/BlockedBy.tsx` (useMutation + `patch` + invalidar `['card', key]` e `['cards']`) e os estilos de `card/shared.tsx`.
- [ ] **Step 4:** mesmo comando → PASS; `pnpm -C web test --run` e `pnpm -C web build` → PASS.
- [ ] **Step 5: Commit** `feat(web): editar tipo e épico, filhas do épico e nova filha (NAC-55)`.

---

### Task 9: Verificação ponta a ponta

**Files:** nenhum novo (só correções que aparecerem).

- [ ] **Step 1:** `make test` → tudo verde.
- [ ] **Step 2:** cópia do banco real (`sqlite3 ~/.local/share/nobreai-cockpit/cockpit.db ".backup <scratchpad>/copia.db"`), subir um `nobred` de teste com `NOBRE_ADDR=127.0.0.1:<porta livre> go run ./cmd/nobred --db <scratchpad>/copia.db` (o `nobre` aponta para ele com a mesma `NOBRE_ADDR`) e conferir: `PRAGMA user_version` = 23, contagem de cards/sessões/uso igual à do original, todos os cards `tarefa`/`media`.
- [ ] **Step 3:** no daemon de teste, roteiro do critério 2 do spec: criar épico pela web, filhas em dois projetos, `nobre start --type historia --epic` numa sessão, encerrar, concluir as filhas, aprovar o épico, reabrir uma filha — conferir a coluna do épico a cada passo no board.
- [ ] **Step 4:** registrar no card com `nobre test "<roteiro acima>"`.
