# Cards que se mantêm atualizados e cadastro de projetos pelo agente: plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O agente sempre recebe um card atual. O card segue o branch e conclui ao integrar, as perguntas podem ser respondidas pelo CLI, o `nobre done` cobra "Como testar" atual, e um repositório não cadastrado é cadastrado pelo próprio agente, com levantamento do histórico e cards no Backlog.

**Architecture:**
- Uma migração (`009`) dá base ao projeto e marcas de branch ao card.
- O pacote `board` ganha a sincronização do branch do card (chamada pelo hook e pelas rotas do agente) e o detector de integração (um laço de 2 minutos no `nobred`).
- O pacote novo `internal/scan` faz o levantamento só de leitura do repositório, exposto em `GET /api/projects/{id}/scan`.
- O hook de SessionStart passa a avisar quando o repositório não está cadastrado. A skill e as instruções explicam o fluxo.

**Tech Stack:** Go 1.26 (SQLite, `net/http`, `os/exec` do git), React + TanStack Query + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-15-cards-atualizados-design.md`. Card NAC-9.

## Global Constraints

- Valem as constraints de `docs/plan.md`. UI, mensagens do CLI e textos injetados em pt-BR.
- Nunca tocar o `~/.claude` real, o `systemctl` nem o `nobred` instalado (porta 7420). Testes em `t.TempDir()` e `httptest`; testes de git criam repositórios reais em `t.TempDir()` (padrão de `internal/api/worktrees_test.go`).
- Git pelo daemon: só leitura, sempre por `gitRO` (`--no-optional-locks`), nunca `fetch`.
- Datas no SQL são comparadas com `julianday(col)`.
- A migração é `internal/store/migrations/009_branch_tracking.sql`, e o teste de `user_version` em `internal/store/store_test.go` sobe para 9.
- Texto exato da frase de repositório não cadastrado: `NobreAI Cockpit: este repositório não está cadastrado no board`.
- Texto exato da decisão de integração: `Branch <branch> integrada em <base> (<sha7>)`, com o porquê `detectado pelo Cockpit`.
- Levantamento: janela de 30 dias, destaque para os últimos 14 dias, no máximo 20 branches e 10 sessões. Ficam de fora `backup/*` e a base.
- O agente cria no máximo 5 cards por levantamento (regra da skill).

## Review Focus

1. **Card com branch igual à base (NAC-4 e NAC-5 marcam `main`) em Revisão:** nunca é concluído pelo detector (teste na Task 4).
2. **Branch apagado sem que o daemon o tenha visto com commits próprios:** o card não muda (teste na Task 4).
3. **Merge feito só no remoto (`origin/<base>` à frente da base local):** conta como integrado (teste na Task 4).
4. **Cadastro feito de dentro de um worktree ligado:** cadastra o checkout principal e religa as sessões do worktree e do principal (teste na Task 5).
5. **Sessão na pasta pessoal ou fora de um repositório git:** não recebe aviso de cadastro (teste na Task 5).

---

### Task 1: Perguntas que o agente responde

**Files:**
- Modify: `internal/api/me.go` (rota `POST /api/me/answer`, `meAnswer`), `internal/domain/column.go` (`EvAnswer`), `internal/resume/package.go` (ids nas perguntas), `internal/client/client.go`, `cmd/nobre/agent.go` (`answer`, `ask` e `status`), `cmd/nobre/main.go` (despacho e usage)
- Test: `internal/api/me_test.go`, `internal/resume/package_test.go`, `cmd/nobre/agent_test.go`

**Interfaces:**
- Produces: `domain.EvAnswer = "answer"`.
- Produces: `POST /api/me/answer` `{id int64, answer string}`:
  - devolve 200 `{id, answer}`;
  - 400 com resposta vazia;
  - 404 `pergunta não encontrada neste card` quando a pergunta não é do card da sessão.

  Usa o `store.AnswerQuestion` que já existe.
- Produces: `POST /api/me/ask` passa a devolver `{id, text}` (hoje o CLI ignora o corpo), e `nobre ask` imprime `Pergunta <id> registrada.`.
- Produces: `nobre answer <id> "<resposta>"` imprime `Resposta registrada.`.
- Produces: `nobre status` lista as perguntas abertas como `- [<id>] <texto>`, abaixo de "Perguntas em aberto:".
- Produces: o pacote de retomada lista as perguntas abertas como `- [<id>] <texto>`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMeAnswerOwnCardQuestion(t *testing.T)      // ask → id; answer {id, "cookie"} → 200; CardContent: Answer "cookie"; 1 evento EvAnswer
func TestMeAnswerOtherCardQuestion404(t *testing.T) // pergunta criada noutro card → 404 "pergunta não encontrada neste card"; resposta não gravada
func TestMeAnswerEmpty400(t *testing.T)             // answer "  " → 400
func TestPackageQuestionsShowIDs(t *testing.T)      // Questions [{ID:3,Text:"x"},{ID:4,Text:"y",Answer:"z"}] → contém "- [3] x" e não contém "y"
func TestCLIAnswer(t *testing.T)                    // nobre answer 3 "cookie" → POST /api/me/answer {"id":3,"answer":"cookie"}; saída "Resposta registrada."
func TestCLIAnswerUsage(t *testing.T)               // nobre answer abc "x" e nobre answer 3 → código 2
func TestCLIAskPrintsID(t *testing.T)               // servidor devolve {"id":7,...} → "Pergunta 7 registrada."
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/api/ ./internal/resume/ ./cmd/nobre/ -run 'Answer|QuestionsShowIDs|AskPrintsID'`
Expected: FAIL (rota e comando inexistentes)

- [ ] **Step 3: Implement**

- `meAnswer` segue o padrão de `meTodoDone`: confere o id em `CardContent(c.ID).Questions`, depois chama `AnswerQuestion`, registra o evento e publica `"cards"`.
- `ask` e `status` usam o retorno do client. Inclua `answer` no `usage` e no `helpFor`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/api/ ./internal/resume/ ./cmd/nobre/`
Expected: PASS

- [ ] **Step 5: Commit**

`git commit -m "feat: agente responde perguntas do card (nobre answer)"`

---

### Task 2: `nobre done` cobra "Como testar" atual e perguntas respondidas

**Files:**
- Modify: `internal/api/me.go` (`meDone`), `internal/client/client.go`, `cmd/nobre/agent.go` (`done --force`), `cmd/nobre/main.go`
- Test: `internal/api/me_test.go`, `cmd/nobre/agent_test.go`, e o teste de ponta a ponta em `internal/e2e` se ele chamar `done` sem "Como testar"

**Interfaces:**
- Produces: `POST /api/me/done` aceita o corpo opcional `{"force": bool}` (sem corpo vale `false`). Sem `force`, responde 409 com uma mensagem por problema, separada por `; `, e o card não muda.
  - `"Como testar" vazio: registre com nobre test`
  - `"Como testar" é anterior à última edição de arquivo: atualize com nobre test`. Vale quando `LastEventAt(card, EvFileEdit)` é maior que `LastEventAt(card, EvHowToTest)`. Sem nenhum evento `EvHowToTest`, mas com o texto preenchido pela UI, conta como atual.
  - `<n> pergunta(s) sem resposta: responda com nobre answer <id>`

  A mensagem 409 termina com ` (ou nobre done --force)`.
- Produces: `nobre done [--force]`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMeDoneRefusesWithoutHowToTest(t *testing.T)   // card em_andamento sem how_to_test → 409 contém "registre com nobre test"; col continua em_andamento
func TestMeDoneRefusesStaleHowToTest(t *testing.T)     // evento how_to_test em t0, file_edit em t1 > t0 → 409 contém "anterior à última edição"
func TestMeDoneRefusesOpenQuestion(t *testing.T)       // how_to_test atual + 1 pergunta sem resposta → 409 contém "1 pergunta(s) sem resposta"
func TestMeDoneOKWhenCurrent(t *testing.T)             // how_to_test depois da última edição, sem perguntas → 200, col revisao
func TestMeDoneForce(t *testing.T)                     // sem how_to_test, {"force":true} → 200, col revisao
func TestCLIDoneForce(t *testing.T)                    // nobre done --force → corpo {"force":true}
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/api/ ./cmd/nobre/ -run 'MeDone|CLIDone'`
Expected: FAIL

- [ ] **Step 3: Implement**

- Ler o corpo de forma tolerante: aceite um corpo vazio, mesmo que `readJSON` exija JSON. Siga a regra de CSRF de `server.go`: POST com corpo exige `Content-Type` JSON, e sem corpo deve continuar aceito. Confira como `meDone` é chamado hoje.
- Ajuste os testes existentes que chamam `done` sem "Como testar" para registrar antes um `test`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`git commit -m "feat: nobre done cobra como testar atual e perguntas respondidas"`

---

### Task 3: Base do projeto e branch do card que acompanha o trabalho

**Files:**
- Create: `internal/store/migrations/009_branch_tracking.sql`, `internal/board/branch.go`, `internal/gitinfo/branch.go`
- Modify:
  - `internal/domain/types.go` (`Project.BaseBranch`; `Card.BranchHead`, `Card.BranchDiverged`, `Card.BranchMergedAt`)
  - `internal/store/projects.go` e `internal/store/cards.go` (colunas e funções novas)
  - `internal/adapter/claude/hooks.go` (sincronizar depois de `moveSession` e do attach no SessionStart)
  - `internal/api/me.go` (sincronizar em `meStart` e `meResume`; rota `POST /api/me/branch`)
  - `internal/api/config.go` (`PATCH /api/projects/{id}` aceita `base_branch`)
  - `internal/client/client.go`, `cmd/nobre/agent.go` (`nobre branch <nome>`), `cmd/nobre/admin.go` (`nobre project base <branch> [path]`), `cmd/nobre/main.go`
  - `internal/store/store_test.go` (`user_version` = 9)
  - `docs/plan-configuracoes.md`: a migração do NAC-4 passa a ser `010`, e a Task 1 dele toca o mesmo bloco de `sessionStart` que a Task 5 daqui
  - `web/src/api/types.ts` (campos novos)
- Test: `internal/gitinfo/branch_test.go`, `internal/board/branch_test.go`, `internal/store/cards_test.go`, `internal/adapter/claude/hooks_test.go`, `internal/api/me_test.go`, `cmd/nobre/agent_test.go`

**Interfaces:**
- Migração:

  ```sql
  ALTER TABLE projects ADD COLUMN base_branch TEXT NOT NULL DEFAULT '';
  ALTER TABLE cards ADD COLUMN branch_head TEXT NOT NULL DEFAULT '';
  ALTER TABLE cards ADD COLUMN branch_diverged INTEGER NOT NULL DEFAULT 0;
  ALTER TABLE cards ADD COLUMN branch_merged_at TEXT;
  ```

- Produces: os campos novos:
  - `domain.Project.BaseBranch string` (json `base_branch`);
  - `domain.Card.BranchHead string` (`branch_head`), `BranchDiverged bool` (`branch_diverged`), `BranchMergedAt *time.Time` (`branch_merged_at`).
- Produces (`internal/gitinfo/branch.go`, todas com `gitRO`):

  ```go
  type BranchReader interface {
      DetectBase(repo string) string                 // develop (local ou origin/develop) → alvo de origin/HEAD → main → master → ""
      BranchTip(repo, branch string) (string, bool)  // sha completo de refs/heads/<branch>; false se não existe
      IsAncestor(repo, commit, ref string) bool      // merge-base --is-ancestor; false se ref não existe
  }
  var _ BranchReader = Exec{}
  ```

- Produces (store):
  - `SetProjectBase(id int64, base string) error`.
  - `SetCardBranch(cardID int64, branch string) (bool, error)`. Só age se o branch for diferente do atual; nesse caso zera `branch_head`, `branch_diverged` e `branch_merged_at`. Devolve se mudou.
  - `SetBranchHead(cardID int64, head string, diverged bool) error`.
  - `MarkBranchMerged(cardID int64, at time.Time) error`.
- Produces (`internal/board/branch.go`):
  - `ProjectBase(git gitinfo.BranchReader, p domain.Project) string`: devolve `p.BaseBranch`; se vazio, `git.DetectBase(p.Path)`.
  - `SyncCardBranch(st *store.Store, git gitinfo.BranchReader, cardID int64, branch string) (bool, error)`.
    - Não faz nada quando `branch` é `""`, `"HEAD"` ou igual a `ProjectBase` do projeto do card.
    - Nos outros casos chama `SetCardBranch`.
    - Com `git == nil`, só ignora `""` e `"HEAD"`.
- Produces: `POST /api/me/branch` `{branch string}` chama `SyncCardBranch` e devolve o card. A recusa de branch vazio dá 400. Branch igual à base dá 409 `"<b>" é a base do projeto; o card fica com o branch de trabalho`.
- Produces: `nobre branch <nome>` e `nobre project base <branch> [path]`. O `project base` acha o projeto por caminho pela lista de projetos e manda `PATCH`.

- [ ] **Step 1: Write the failing tests**

```go
// gitinfo (repositório real em t.TempDir)
func TestDetectBasePrefersDevelop(t *testing.T)      // branches main e develop → "develop"
func TestDetectBaseMainThenMaster(t *testing.T)      // só main → "main"; só master → "master"; nenhum → "" (repo sem commits)
func TestBranchTipAndIsAncestor(t *testing.T)        // feat com 1 commit: BranchTip ok; IsAncestor(tip,"main") false; após merge true; branch inexistente → ("", false)
// store
func TestSetCardBranchResetsMarks(t *testing.T)      // SetBranchHead("abc", true) + MarkBranchMerged; SetCardBranch("feat/y") → true, marcas zeradas; mesma branch → false
// board
func TestSyncCardBranchIgnoresBase(t *testing.T)     // projeto com base "main": Sync("main") → false, branch do card inalterado; Sync("feat/x") → true
// hook
func TestMoveSessionSyncsCardBranch(t *testing.T)    // sessão com card; evento com cwd num worktree em feat/x → card.Branch "feat/x"
// api + cli
func TestMeBranch(t *testing.T)                      // {"branch":"feat/x"} → 200 e card.Branch; {"branch":"main"} (base) → 409
func TestCLIBranchAndProjectBase(t *testing.T)       // nobre branch feat/x → POST /api/me/branch; nobre project base develop → PATCH base_branch
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/gitinfo/ ./internal/store/ ./internal/board/ ./internal/adapter/claude/ ./internal/api/ ./cmd/nobre/`
Expected: FAIL

- [ ] **Step 3: Implement**

- `api.Deps.Git` e as deps do adaptador continuam `gitinfo.GitInfo`. Para obter `BranchReader`, use type assertion, como já se faz com `WorktreeReader`.
- No hook, sincronize depois de `moveSession` e no `sessionStart` quando a sessão for ligada pelo `NOBRE_RESUME`. Use o branch da sessão.
- Na API, sincronize em `meStart` e `meResume` com `sess.Branch`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./... && pnpm -C web test --run`
Expected: PASS

- [ ] **Step 5: Commit**

`git commit -m "feat: base do projeto e branch do card que acompanha a sessão"`

---

### Task 4: Detector de integração

**Files:**
- Create: `internal/board/integration.go`
- Modify:
  - `cmd/nobred/main.go`: laço a cada 2 minutos, com o mesmo padrão de parada do reconciliador; publica `"cards"` quando algo muda;
  - `internal/store/cards.go`: listar os cards candidatos;
  - `web/src/pages/Board.tsx` e `web/src/pages/CardPage.tsx`: marca "branch integrada".
- Test: `internal/board/integration_test.go`, `web/src/pages/Board.test.tsx`

**Interfaces:**
- Consumes: `BranchReader`, `ProjectBase`, `SetBranchHead`, `MarkBranchMerged` (Task 3).
- Produces: `CheckIntegrations(st *store.Store, git gitinfo.BranchReader, now time.Time) (changed bool, err error)`. Para cada card em `em_andamento`, `pausado` ou `revisao` com `branch != ""` e `branch_merged_at` nulo, num projeto com caminho:

  ```
  base := ProjectBase(git, projeto); pule se base == "" ou branch == base
  in(c) := IsAncestor(repo, c, base) || IsAncestor(repo, c, "origin/"+base)
  tip, ok := BranchTip(repo, branch)
  se ok e !in(tip):                     SetBranchHead(card, tip, true)          // tem commits próprios
  se ok e in(tip) e card.BranchDiverged: integrado em tip
  se !ok e card.BranchDiverged e card.BranchHead != "" e in(card.BranchHead): integrado em card.BranchHead
  integrado em sha: MarkBranchMerged; AddDecision{What: "Branch <branch> integrada em <base> (<sha[:7]>)", Why: "detectado pelo Cockpit"};
                    se col == revisao → MoveCard(concluido)
  ```

  Erros por card não param os outros (`errors.Join`).
- Produces: `RunIntegrations(ctx, st, git, every, now func() time.Time, onChange func(), logf)`, com a mesma forma de `RunReconciler`.
- Produces (UI): marca `branch integrada` (classe `badge`) no card do Board e no cabeçalho do CardPage quando `branch_merged_at` está preenchido e a coluna não é `concluido`.

- [ ] **Step 1: Write the failing tests**

```go
func TestIntegrationConcludesReviewCard(t *testing.T)       // card revisao em feat/x; 1ª passada: diverged, head = tip; merge em main; 2ª: col concluido, merged_at, decisão "Branch feat/x integrada em main (<sha7>)"
func TestIntegrationKeepsInProgressCard(t *testing.T)       // mesmo fluxo com em_andamento → col continua em_andamento; merged_at e decisão gravados
func TestIntegrationDeletedBranchAfterSeen(t *testing.T)    // diverged visto; merge; git branch -D feat/x → integrado pelo branch_head
func TestIntegrationDeletedBranchNeverSeen(t *testing.T)    // card com branch inexistente e sem head → nada muda (Review Focus 2)
func TestIntegrationIgnoresBaseBranchCard(t *testing.T)     // card revisao com branch "main" → nada muda (Review Focus 1)
func TestIntegrationRemoteOnlyMerge(t *testing.T)           // merge só em refs/remotes/origin/main (update-ref) → integrado (Review Focus 3)
func TestIntegrationRunsOnce(t *testing.T)                  // 3ª passada depois de integrado → changed false, só 1 decisão
```

```ts
it('mostra "branch integrada" em card não concluído')  // branch_merged_at preenchido, column em_andamento → texto; concluido → sem o texto
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/board/ -run Integration && pnpm -C web test --run Board`
Expected: FAIL

- [ ] **Step 3: Implement `CheckIntegrations` e `RunIntegrations` em `internal/board/integration.go`, ligue o laço no `nobred` e adicione a marca na UI**

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./... && pnpm -C web test --run`
Expected: PASS

- [ ] **Step 5: Commit**

`git commit -m "feat: card registra a integração do branch e conclui se estava em revisão"`

---

### Task 5: Cadastro pelo agente e aviso do hook

**Files:**
- Modify:
  - `internal/store/projects.go`: `ReassignSessions`;
  - `internal/api/config.go`: `createProject` religa as sessões, grava a base e devolve `reassigned`;
  - `internal/api/me.go`: `msgNoProject`;
  - `internal/adapter/claude/context.go`: aviso de não cadastrado;
  - `cmd/nobre/admin.go`: `project add` resolve a raiz principal e imprime as sessões religadas;
  - `internal/client/client.go`.
- Test: `internal/store/projects_test.go` (criar se não existir), `internal/api/config_test.go`, `internal/adapter/claude/hooks_test.go`, `cmd/nobre/admin_test.go`

**Interfaces:**
- Produces: `(s *Store) ReassignSessions(projectID int64, root string) (int, error)`. Grava `project_id` e `full_capture = 1` nas sessões com `project_id` nulo cujo `cwd` é `root` ou fica abaixo de `root + "/"`, o mesmo limite de `ProjectForPath`. Devolve quantas mudaram.
- Produces: `POST /api/projects`:
  - depois de criar, chama `ReassignSessions(p.ID, p.Path)`;
  - se `base_branch` vier vazio e o Git implementar `BranchReader`, grava `DetectBase(p.Path)`;
  - responde 201 com o projeto mais `"reassigned": <n>`, e publica `"sessions"` e `"cards"`.
- Produces: `nobre project add [path]`:
  - com o git disponível, troca o caminho por `Locate(path).Main` quando ele não for vazio (de dentro de um worktree, cadastra o principal);
  - imprime `Projeto <nome> cadastrado: <path> (base <base>, <n> sessões religadas)`.
- Produces: `const unregisteredPhrase = "NobreAI Cockpit: este repositório não está cadastrado no board"`.
- Produces: no `sessionStart`, quando a sessão não tem projeto, não há retomada e a sessão não é de captura completa, chamar `unregisteredRoot(cwd)`. Se ela devolver uma raiz não vazia, responder `additionalContext` com:

  ```
  unregisteredPhrase + " (<raiz>).\nAntes de seguir com o pedido do usuário, cadastre com nobre project add <raiz> e faça o levantamento com nobre project scan; crie os cards importantes com nobre card add (veja a skill nobre-cockpit). O cadastro liga esta sessão ao board.\n\n" + instructions
  ```

- Produces: `func (a *Adapter) unregisteredRoot(cwd string) string`:
  - devolve `Locate(cwd).Main`, ou `""`;
  - devolve `""` quando: o Git não implementa `WorktreeReader`; não há repositório; a raiz é `os.UserHomeDir()`; já existe projeto para a raiz em `ProjectForPath`, inclusive com captura desligada, que funciona como "ignorar".
- Produces: `msgNoProject = "Este repositório não está cadastrado no NobreAI Cockpit. Cadastre com nobre project add e faça o levantamento com nobre project scan (veja a skill nobre-cockpit)."`

- [ ] **Step 1: Write the failing tests**

```go
func TestReassignSessionsRespectsBoundary(t *testing.T)   // sessões em /r, /r/sub, /r-outro, /x → ReassignSessions(p,"/r") = 2; /r-outro e /x continuam sem projeto
func TestCreateProjectReassignsAndSetsBase(t *testing.T)  // sessão viva sem projeto no repo (branches main e develop) → POST /api/projects → reassigned 1, base_branch "develop"; sessão com project_id e full_capture
func TestSessionStartUnregisteredRepoWarns(t *testing.T)  // repo git não cadastrado → additionalContext começa com unregisteredPhrase e contém "nobre project add <raiz>"
func TestSessionStartHomeNoWarning(t *testing.T)          // cwd = HOME (t.Setenv("HOME", repo)) → resposta vazia (Review Focus 5)
func TestSessionStartNonGitNoWarning(t *testing.T)        // cwd fora de repo → resposta vazia (Review Focus 5)
func TestSessionStartIgnoredProjectNoWarning(t *testing.T)// projeto cadastrado com capture=false → resposta vazia
func TestCLIProjectAddFromLinkedWorktree(t *testing.T)    // nobre project add dentro de um worktree ligado → POST com path do checkout principal (Review Focus 4)
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/store/ ./internal/api/ ./internal/adapter/claude/ ./cmd/nobre/ -run 'Reassign|CreateProject|Unregistered|HomeNoWarning|NonGitNoWarning|IgnoredProject|ProjectAdd'`
Expected: FAIL

- [ ] **Step 3: Implement**

O `cli` do `nobre` hoje não tem git. Acrescente o campo `git gitinfo.WorktreeReader`, padrão `gitinfo.Exec{}`, que os testes trocam.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`git commit -m "feat: agente cadastra o repositório e o cadastro religa as sessões"`

---

### Task 6: Levantamento do histórico e cards criados pelo agente

**Files:**
- Create: `internal/scan/scan.go`, `internal/scan/scan_test.go`
- Modify:
  - `internal/api/config.go`: `GET /api/projects/{id}/scan`;
  - `internal/api/me.go`: `POST /api/me/cards`;
  - `internal/store/cards.go`: se faltar, criar card com coluna e "Onde parei" informados;
  - `internal/client/client.go`, `cmd/nobre/admin.go` (`project scan`), `cmd/nobre/agent.go` (`card add`), `cmd/nobre/main.go`.
- Test: `internal/scan/scan_test.go`, `internal/api/config_test.go`, `internal/api/me_test.go`, `cmd/nobre/admin_test.go`, `cmd/nobre/agent_test.go`

**Interfaces:**
- Produces (`internal/scan`):

  ```go
  type Branch struct { Name, Subject, Worktree string; LastCommit time.Time; Ahead int; Recent bool } // Recent: último commit ≤ 14 dias
  type Plan   struct { Path string; Open, Done int; Modified time.Time }
  type Result struct { Repo, Base string; Branches []Branch; Plans []Plan }
  func Run(repo, base string, now time.Time) (Result, error)
  ```

  - **Branches:** só locais e com `Ahead > 0` em relação à base (`rev-list --count base..b`). Último commit nos últimos 30 dias. Ficam de fora `backup/*` e a base. Ordem do mais recente para o mais antigo, no máximo 20. `Worktree` é o caminho do worktree em que o branch está, quando houver.
  - **Planos:** arquivos `.md` em `docs/` (recursivo, até 4 níveis) cujo nome contém `plan`, com `Open` (`- [ ]`) maior que zero e modificados nos últimos 30 dias. `Done` conta `- [x]`.
  - Todo git com `gitRO`.
- Produces: `GET /api/projects/{id}/scan` responde 200 com:

  ```
  {repo, base, branches, plans,
   sessions: [{id, started_at, ended_at, branch, cwd, cost_usd}],
   cards:    [{key, title, column, branch}]}
  ```

  - `sessions`: sessões do projeto nos últimos 30 dias com custo maior que zero, as 10 de maior custo.
  - `cards`: todos os cards do projeto, para o agente não duplicar.
  - Projeto inexistente: 404.
- Produces: `POST /api/me/cards` `{title, goal, done_when, branch, where_stopped, column}`:
  - cria o card no projeto da sessão **sem** ligar a sessão;
  - `column` aceita só `backlog` (padrão) ou `pausado`; outro valor dá 400;
  - sessão sem projeto dá 409 `msgNoProject`;
  - responde 201 com o card.
- Produces: `nobre project scan [path]` imprime o levantamento em texto, com estas seções: `Base:`, `Branches com trabalho fora da base`, `Planos com tarefas abertas`, `Sessões recentes com custo`, `Cards que já existem`. Cada branch mostra nome, idade, commits à frente, assunto do último commit e worktree.
- Produces: `nobre card add "<título>" [--goal] [--done-when] [--branch] [--where "<onde parei>"] [--column backlog|pausado]` imprime `Card <KEY> criado no <coluna> (sessão não ligada).`.

- [ ] **Step 1: Write the failing tests**

```go
func TestScanFiltersBranches(t *testing.T)     // repo com main; feat/a (commit hoje), feat/velha (commit com GIT_COMMITTER_DATE de 40 dias), backup/x, feat/integrada (merged) → só feat/a, Ahead 1, Recent true
func TestScanPlans(t *testing.T)               // docs/plan-x.md com "- [ ] a\n- [x] b" → Open 1 Done 1; docs/plan-pronto.md só com [x] → ausente; docs/notas.md → ausente
func TestScanEndpoint(t *testing.T)            // projeto + repo real → 200 com base, branches, cards; id inexistente → 404
func TestMeCardsCreatesWithoutAttach(t *testing.T) // sessão ligada ao card A; POST /api/me/cards {title:"B", column:"pausado", where_stopped:"x"} → 201, B em pausado com where_stopped; sessão continua no card A
func TestMeCardsBadColumn400(t *testing.T)     // column "revisao" → 400
func TestCLICardAdd(t *testing.T)              // nobre card add "B" --column pausado --where "x" → corpo esperado; saída "Card NAC-2 criado no pausado (sessão não ligada)."
func TestCLIProjectScanPrintsSections(t *testing.T) // servidor fake devolve o scan → saída contém as 5 seções
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/scan/ ./internal/api/ ./cmd/nobre/ -run 'Scan|MeCards|CardAdd'`
Expected: FAIL

- [ ] **Step 3: Implement**

O `nobre project scan` sem caminho usa o cwd. Acha o projeto pelo prefixo do caminho na lista de projetos, e de dentro de um worktree resolve pelo `Locate().Main`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`git commit -m "feat: levantamento do histórico do projeto e cards criados pelo agente"`

---

### Task 7: Skill, instruções do hook e ajuda do CLI

**Files:**
- Modify: `internal/adapter/claude/skill.md`, `internal/adapter/claude/context.go` (`instructions`), `cmd/nobre/main.go` (`usage`, `helpFor`), `internal/adapter/claude/binary_test.go`
- Test: `internal/adapter/claude/binary_test.go`

**Interfaces:**
- Consumes: os comandos das Tasks 1 a 6 e `unregisteredPhrase` (Task 5).
- A skill:
  - **Seção "Antes de tudo":** use `nobre` quando o contexto tiver a frase registrada **ou** a frase de não cadastrado. Remova a proibição de cadastrar.
  - **Seção nova "Repositório não cadastrado":**
    1. `nobre project add <raiz>`;
    2. `nobre project scan`;
    3. escolha até **5** cards: entregas com branch recente (14 dias) ou worktree ativo, e planos com tarefas abertas. Agrupe branches da mesma entrega. Não duplique cards existentes. Ignore o que está parado há mais de 30 dias;
    4. crie com `nobre card add`. Use `--column pausado --where` quando houver trabalho começado (branch com commits), e Backlog para o resto. Informe `--branch` sempre que houver;
    5. diga ao usuário, em poucas linhas, o que cadastrou e criou, e siga com o pedido dele.

    Se o usuário não quiser o repositório no board, ele desliga a captura do projeto na Configuração.
  - **Comandos novos:** `nobre answer`, `nobre branch` (rodar ao criar ou entrar no branch ou worktree do card), `nobre card add`, `nobre project scan`, `nobre project base`, `nobre done --force` (só com o motivo registrado em `nobre note`).
  - **"Como manter o card útil":** responda as perguntas com `nobre answer` assim que forem decididas. Atualize "Como testar" depois da última mudança de código, antes do `nobre done`.
- `instructions`: acrescente na linha "Com card" os comandos `nobre answer <id> "<resposta>"` e `nobre branch <nome>`.
- `binary_test.go`: troque a asserção de "não cadastre" por: a skill contém `unregisteredPhrase`, `nobre project scan` e `nobre card add`, e **não** contém `não cadastre`.

- [ ] **Step 1: Atualize o teste `TestSkillGatesOnRegisteredPhrase` com as asserções novas e veja-o falhar**

Run: `go test ./internal/adapter/claude/ -run Skill`
Expected: FAIL

- [ ] **Step 2: Atualize `skill.md`, `instructions`, `usage` e `helpFor`**

- [ ] **Step 3: Rode tudo**

Run: `go vet ./... && go test ./... && pnpm -C web test --run && make build`
Expected: PASS e build ok

- [ ] **Step 4: Commit**

`git commit -m "docs: skill e instruções ensinam cadastro, levantamento, answer e branch"`
