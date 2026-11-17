# Sub-projetos: monorepo e grupo de repositórios (NAC-23): plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** um projeto pode ter sub-projetos (apps de um monorepo ou repositórios de um grupo). O Cockpit soma o custo e o andamento da família e filtra por app.

**Architecture:**
- **Modelo:** `projects.parent_id` (um nível) e `path` opcional (grupo). O "projeto do repositório" (o pai, num app de monorepo) concentra tudo o que é do git.
- **App do card:** o agente informa com `nobre app <nome>`. O custo com card já segue `cards.project_id`.
- **Interface:** os filtros comparam com o conjunto da família, e as páginas Projetos e do projeto somam por app.

**Tech Stack:** Go (stdlib, SQLite pelo `store`), React + TanStack Query + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-17-sub-projetos-design.md`

## Global Constraints

- Um nível só: o pai não tem pai, e um projeto com filhos não recebe pai.
- **Monorepo:** pai com pasta, e o filho precisa ter pasta dentro da pasta do pai. **Grupo:** pai sem pasta, e os filhos têm qualquer pasta.
- Nomes de projeto únicos dentro de uma família (o `nobre app <nome>` depende disso).
- Chaves de card continuam globais (`NAC-n`). Nenhum card, sessão, prompt ou uso pode ser perdido na migração.
- O hook não ganha chamadas de git novas. A raiz do worktree vem do `locate`/`Locate` que já roda.
- A captura é do pai (inclusive grupo) e vale para os filhos.
- **Sugestões:** `pnpm-workspace.yaml` (`packages`), `package.json` (`workspaces` lista ou `{packages}`), `go.work` (`use`), `Cargo.toml` (`[workspace] members`). O `*` é expandido um nível. Sem dependência nova.
- O agente cadastra só a raiz e **lista** os sub-projetos sugeridos (até 5, mais "N na Configuração").
- Textos em pt-BR, sem travessão no meio de frase. O nome exibido de um filho é `"<pai> › <filho>"`.
- **Toda tarefa:** `go test ./...` e `cd web && pnpm test --run` verdes antes do commit.
- **Chaves livres na tabela `settings`:** se o plano do NAC-6 já tiver criado `GetKV`/`PutKV` no store, reutilize; senão, crie-os na Task 6 com essa mesma assinatura.

## Review Focus

1. **Migração num banco real:** com cards, sessões, prompts e uso apontando para projetos, depois da migração `PRAGMA foreign_key_check` volta vazio e as contagens são as mesmas. Teste na Task 1.
2. **Soma sem contar duas vezes:** o total da família é igual à soma das linhas dos filhos mais a linha "geral", centavo a centavo. Teste na Task 8.
3. **Filho de monorepo com pasta fora da pasta do pai:** recusado com 400 e mensagem clara, sem gravar nada. Teste na Task 2.
4. **Grupo escolhido em Worktrees ou levantamento:** o grupo não tem pasta, então nada roda git na pasta vazia, e aparecem os repositórios dos filhos. Teste na Task 3.
5. **`nobre app` durante a pausa por limite de custo:** passa, como os outros comandos de handoff. Nome repetido na família é recusado no cadastro. Testes nas Tasks 5 e 2.

---

### Task 1: Migração: `parent_id` e pasta opcional

**Files:**
- Create: `internal/store/migrations/011_subprojects.sql`. Se o NAC-6 já usou o 011, use o próximo número livre.
- Modify: `internal/store/store.go` (o executor de migrações aceita rodar sem FK)
- Modify: `internal/store/projects.go` (ler e gravar `parent_id`; `path` NULL vira `""`)
- Modify: `internal/domain/types.go` (`Project.ParentID *int64 \`json:"parent_id"\``)
- Test: `internal/store/migrate_subprojects_test.go`

**Interfaces:**
- Produces:
  - `domain.Project.ParentID`. `Path == ""` significa grupo.
  - `ProjectForPath` ignora projetos sem pasta.
  - **Marcador de migração:** uma migração cuja primeira linha é `-- nobre:foreign_keys=off` roda numa conexão dedicada (`s.DB.Conn`) com `PRAGMA foreign_keys=OFF` antes do `BEGIN`. Antes do `COMMIT` roda `PRAGMA foreign_key_check`, e se a verificação voltar alguma linha a migração aborta. Depois do commit, `PRAGMA foreign_keys=ON`.

- [ ] **Step 1: Write the failing tests:**
  - `TestMigrationKeepsProjectsAndRefs`: abre um banco migrado até a versão anterior (helper que aplica migrações até N), insere 2 projetos, 3 cards, 2 sessões, 1 prompt de projeto e 2 linhas de uso, e roda o resto das migrações. Verifica:
    - as mesmas contagens;
    - os mesmos `project_id` nos cards;
    - `PRAGMA foreign_key_check` sem linhas;
    - `PRAGMA foreign_keys` de volta a 1.
  - `TestGroupProjectHasNoPath`: `CreateGroup("loja")` (Task 2 cria a função; aqui, um `INSERT` com `path NULL`). Dois grupos convivem com o `UNIQUE`; `ListProjects` devolve `Path == ""`; `ProjectForPath("/qualquer")` não os escolhe.
- [ ] **Step 2:** `go test ./internal/store/ -run 'MigrationKeeps|GroupProjectHasNoPath'`. Esperado: FAIL.
- [ ] **Step 3: Migração** (procedimento de reconstrução do SQLite, como na `004`): cria `projects_new` com as mesmas colunas de hoje, com `path TEXT UNIQUE` (sem `NOT NULL`) e mais `parent_id INTEGER REFERENCES projects(id)`. Copia as linhas, apaga `projects` e renomeia. O executor ganha o marcador. Leitura e gravação de `parent_id` no `projects.go`.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(store): projetos com pai e pasta opcional`

### Task 2: Regras da família no store

**Files:**
- Create: `internal/store/family.go`, `internal/store/family_test.go`
- Modify: `internal/store/projects.go` (`CreateProject` com pai, `UpdateProject` propaga a captura, `DeleteProject` recusa pai com filhos)
- Modify: `internal/store/cards.go` (`CardFilter.ProjectIDs`), `internal/store/usage.go` (`TotalsFilter.ProjectIDs`)

**Interfaces:**
- Produces:

```go
var ErrFamily = errors.New("regra de sub-projeto") // envolvida com a mensagem específica
type Moved struct {
    Sessions int `json:"sessions"`
    Cards    int `json:"cards"`
}
func (s *Store) CreateGroup(name string) (domain.Project, error)
func (s *Store) CreateChild(name, path string, parentID int64) (domain.Project, Moved, error)
func (s *Store) SetParent(id int64, parentID *int64) (Moved, error) // nil = desliga
func (s *Store) FamilyIDs(id int64) ([]int64, error)                  // pai: pai+filhos; filho ou solto: só ele
func (s *Store) Children(id int64) ([]domain.Project, error)
func (s *Store) RepoProject(p domain.Project) (domain.Project, error) // pai do app de monorepo; senão p
func (s *Store) SetCardProject(cardID, projectID int64) error         // só dentro da mesma família
func DisplayName(p domain.Project, parent *domain.Project) string     // "loja › web"
```

- **`CardFilter.ProjectIDs []int64` e `TotalsFilter.ProjectIDs []int64`:** valem junto com o `ProjectID` antigo, como um `IN (...)`.

Regras (do spec):
- **Validação**, com mensagens de `ErrFamily`:
  - `"o projeto principal não pode ser sub-projeto de outro"`;
  - `"um projeto com sub-projetos não pode virar sub-projeto"`;
  - `"a pasta do sub-projeto precisa ficar dentro da pasta do projeto principal"`;
  - `"já existe um projeto com esse nome nesta família"`;
  - `"o projeto tem sub-projetos; desligue-os antes de excluir"`.
- **Captura:** ao ligar, o filho adota `capture` do pai. `UpdateProject` de um pai com captura alterada aplica a mesma captura aos filhos. Desligar o filho mantém a captura dele.
- **Histórico:** ao ligar ou criar um filho, as sessões do pai cujo `cwd` é a pasta do filho ou fica abaixo dela (mesmo limite de diretório de `ProjectForPath`) passam para o filho. As sessões em worktree ligado do mesmo repositório passam quando o caminho relativo à raiz do worktree (`sessions.worktree`) cai na pasta relativa do filho. Um card do pai passa se **todas** as sessões dele passaram. Um card sem sessões fica. Tudo numa transação.
- **Desligar:** não devolve nada ao pai.

- [ ] **Step 1: Write the failing tests** (`family_test.go`):
  - `TestCreateChildInsideParent`: pai `/r/loja`, filho `/r/loja/apps/web` cria, e `FamilyIDs(pai)` tem os dois.
  - `TestChildOutsideMonorepoRefused`: filho `/r/outro` sob pai `/r/loja` dá `errors.Is(err, ErrFamily)`, e nenhum projeto é criado (Review Focus 3).
  - `TestGroupAcceptsAnyPath`: grupo com filhos `/r/front` e `/r/back`.
  - `TestOneLevelOnly`: neto recusado; pai com filhos não recebe pai.
  - `TestDuplicateNameInFamilyRefused`: dois `web` na mesma família recusados (Review Focus 5); o mesmo nome em famílias diferentes passa.
  - `TestSetParentMovesHistory`: sessão do pai em `/r/loja/apps/web/src` e outra em `/r/loja`; um card com só a primeira e outro com as duas. Depois de `SetParent(web, &loja)`: `Moved{Sessions:1, Cards:1}`; o segundo card fica no pai.
  - `TestSetParentMovesWorktreeSession`: sessão com `worktree=/r/loja/.worktrees/x`, `worktree_linked=1`, `cwd=/r/loja/.worktrees/x/apps/web` passa para `web`.
  - `TestCaptureFollowsParent`: ligar adota a captura do pai, e `UpdateProject(pai capture=false)` desliga os filhos.
  - `TestDeleteParentWithChildrenRefused`.
  - `TestRepoProject`: app de monorepo devolve o pai; filho de grupo devolve ele mesmo.
  - `TestSetCardProjectOutsideFamilyRefused`.
  - `TestTotalsProjectIDs`: uso no pai e no filho; `Totals(ProjectIDs: família)` soma os dois, e `ProjectIDs:[filho]` só o filho.
- [ ] **Step 2:** `go test ./internal/store/ -run 'Child|Group|OneLevel|Duplicate|SetParent|Capture|DeleteParent|RepoProject|SetCardProject|TotalsProjectIDs'`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(store): regras de família dos sub-projetos`

### Task 3: O git é do projeto do repositório

**Files:**
- Modify: `internal/board/branch.go` (`ProjectBase` recebe o projeto do repositório), `internal/board/integration.go`
- Modify: `internal/api/worktrees.go`, `internal/api/worktree_detail.go`, `internal/api/scan.go`, `internal/api/me.go` (recusa da base no `meBranch`), `internal/api/config.go` (`base_branch` de um app é ignorado)
- Modify: `internal/adapter/claude/context.go` (`pausedCards` olha a família do repositório)
- Test: `internal/board/branch_test.go`, `internal/api/worktrees_test.go`, `internal/adapter/claude/hooks_test.go`

**Interfaces:**
- Consumes: `RepoProject`, `FamilyIDs`, `Children` (Task 2).
- Produces: `board.RepoBase(st *store.Store, git gitinfo.BranchReader, p domain.Project) string` = `ProjectBase(git, RepoProject(p))`. Todo chamador de `ProjectBase` com um projeto que pode ser app passa a usar `RepoBase`.

Regras:
- **`/api/worktrees`:**
  - lista os worktrees uma vez por repositório: um item para o pai de monorepo, sem itens para os apps;
  - num grupo, não roda git (pasta vazia) e lista os filhos, cada um com seus worktrees;
  - os cards e sessões de um worktree de monorepo incluem os da família.
- **Integrações:** pulam projetos sem pasta e apps de monorepo. O pai cobre os cards da família: `ListCards(ProjectIDs: FamilyIDs(pai))`.
- **`scan`:** num app de monorepo, roda na pasta do pai. Num grupo, responde 400 `"grupo não tem repositório; rode o levantamento em cada sub-projeto"`.
- **`pausedCards`:** com o projeto da sessão dentro de um monorepo, usa `FamilyIDs` do pai.

- [ ] **Step 1: Write the failing tests:**
  - `TestRepoBaseUsesParent`: app com `BaseBranch` "x" e pai com "main" dá `"main"`.
  - `TestWorktreesListsMonorepoOnce`: pai mais 2 apps dão um item só (o pai), com os cards dos apps.
  - `TestWorktreesGroupRunsNoGitOnEmptyPath`: o `WorktreeReader` fake falha se chamado com `""`; o grupo com 2 filhos lista os dois (Review Focus 4).
  - `TestScanGroupRefused`: 400 com a mensagem.
  - `TestIntegrationCoversFamilyCards`: card de app com o branch integrado na base do pai fica com `BranchMergedAt` preenchido.
  - `TestPausedCardsIncludeFamily`: sessão na raiz do monorepo recebe no `SessionStart` um card pausado do `web` no mesmo branch.
- [ ] **Step 2:** `go test ./internal/board/ ./internal/api/ ./internal/adapter/claude/ -run 'RepoBase|MonorepoOnce|GroupRunsNoGit|ScanGroup|CoversFamily|PausedCardsIncludeFamily'`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat: branch base, worktrees e integração ficam no repositório do monorepo`

### Task 4: Sessão em worktree do monorepo cai no app

**Files:**
- Modify: `internal/adapter/claude/hooks.go` (`projectFor`)
- Modify: `internal/store/projects.go` (`AssignSessionsInWorktree` respeita os apps)
- Test: `internal/adapter/claude/hooks_test.go`, `internal/store/projects_test.go`

**Interfaces:**
- Consumes: `Children` (Task 2).
- Produces: `projectFor(cwd)`, para um worktree ligado, devolve o filho cuja pasta relativa ao pai é o prefixo mais longo de `rel(loc.Root, cwd)`, senão o pai. Isso já acontece de graça com o `ProjectForPath(filepath.Join(loc.Main, rel))` de hoje, desde que os filhos tenham pasta dentro do pai: o teste confirma. A mudança real fica em `AssignSessionsInWorktree(projectID, worktree)`, que hoje joga todas as sessões do worktree no pai. Ele passa a escolher, por sessão, o filho cujo caminho relativo casa com `cwd` menos `worktree`.

- [ ] **Step 1: Write the failing tests:**
  - `TestWorktreeSessionLandsInApp` (hooks): pai `/r/loja`, filho `apps/web`, sessão nova com `cwd=/r/loja/.worktrees/x/apps/web/src`. O `fakeGit` com `Locate` devolve `Root=/r/loja/.worktrees/x`, `Main=/r/loja`, `Linked=true`. A sessão fica com `ProjectID == web`.
  - `TestWorktreeRootSessionLandsInParent`: `cwd` na raiz do worktree fica com o pai.
  - `TestAssignSessionsInWorktreeRespectsApps` (store): duas sessões sem projeto no mesmo worktree, uma em `apps/web` e outra na raiz. Depois de `AssignSessionsInWorktree(pai, wt)`, uma fica no filho e outra no pai.
- [ ] **Step 2:** `go test ./internal/adapter/claude/ ./internal/store/ -run 'WorktreeSession|WorktreeRoot|RespectsApps'`. Esperado: FAIL (pelo menos o do store).
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat: sessão num worktree do monorepo cai no app da pasta`

### Task 5: `nobre app` e o agente sabendo dos apps

**Files:**
- Modify: `internal/api/me.go` (rota `POST /api/me/app`)
- Modify: `internal/client/client.go` (`SetApp`)
- Modify: `cmd/nobre/main.go` (subcomando `app`, ajuda)
- Modify: `internal/adapter/claude/nobrecmd.go` (`"app"` em `handoffSubcommands`)
- Modify: `internal/adapter/claude/context.go` (lista de apps no `SessionStart`), `internal/resume` (cabeçalho com `DisplayName`), `internal/adapter/claude/skill.md`
- Test: `internal/api/me_test.go`, `cmd/nobre/agent_test.go`, `internal/adapter/claude/nobrecmd_test.go`, `internal/adapter/claude/hooks_test.go`

**Interfaces:**
- Consumes: `SetCardProject`, `Children`, `FamilyIDs`, `DisplayName` (Task 2).
- Produces:
  - `POST /api/me/app` com `{"app": "web"}` ou `{"geral": true}` responde o card.
  - `(c *Client) SetApp(ctx context.Context, app string, geral bool) (domain.Card, error)`.

Regras e textos:
- **Nome fora da família:** 400 `"app desconhecido: <nome>. Apps deste projeto: web, api"`.
- **Projeto sem filhos:** 400 `"este projeto não tem sub-projetos"`.
- **Sem card:** o mesmo erro dos outros `withSessionCard`.
- **CLI:** `nobre app <nome>` e `nobre app --geral`. A saída é `"Card NAC-x agora em loja › web."`.
- **`SessionStart`:** numa sessão capturada de projeto cuja família tem filhos, o contexto ganha:
  - `"Apps deste projeto: web (apps/web), api (apps/api)."`
  - `"Quando o card for de um app só, rode nobre app <nome> ao criar o card ou ao começar a mexer nele; nobre app --geral devolve ao projeto principal."`
- **Pacote de retomada e `nobre status`:** mostram o projeto como `DisplayName`.
- **Skill:** ganha uma linha na lista de comandos e uma em "Como manter o card útil", com o mesmo texto.

- [ ] **Step 1: Write the failing tests:**
  - `TestMeAppMovesCard`: card no pai; `POST /api/me/app {"app":"web"}` deixa `card.project_id == web`. `{"geral":true}` volta ao pai.
  - `TestMeAppUnknownListsApps`: 400 contendo `"Apps deste projeto: api, web"` (ordem alfabética).
  - `TestMeAppNoChildren`: 400 `"este projeto não tem sub-projetos"`.
  - `TestAppIsHandoffCommand` (nobrecmd): `plainNobreCommand("nobre app web") == true` (Review Focus 5).
  - `TestCLIApp` (agent_test, com servidor fake): imprime `"Card NAC-1 agora em loja › web."`.
  - `TestSessionStartListsApps`: o contexto contém `"Apps deste projeto: web (apps/web)"`; num projeto solto, não contém `"Apps deste projeto"`.
- [ ] **Step 2:** `go test ./internal/api/ ./cmd/nobre/ ./internal/adapter/claude/ -run 'MeApp|AppIsHandoff|CLIApp|ListsApps'`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat: nobre app põe o card no app certo do monorepo`

### Task 6: Sugestão de sub-projetos

**Files:**
- Create: `internal/workspaces/detect.go`, `internal/workspaces/detect_test.go`
- Modify: `internal/api/config.go` (rotas de sugestão e recusa), `internal/api/scan.go` (sugestões no levantamento)
- Modify: `cmd/nobre/admin.go` (`project scan` mostra "Sub-projetos sugeridos" e aceita `--nao-sugerir`)
- Modify: `internal/adapter/claude/context.go` (aviso de repositório não cadastrado), `internal/adapter/claude/skill.md`
- Modify: `internal/store/settings.go` (`GetKV`/`PutKV`, se ainda não existirem; veja Global Constraints)

**Interfaces:**
- Produces:

```go
package workspaces
type Suggestion struct {
    Path   string `json:"path"`   // absoluto
    Rel    string `json:"rel"`    // relativo à raiz, ex.: "apps/web"
    Name   string `json:"name"`   // último pedaço
    Source string `json:"source"` // "pnpm-workspace.yaml", "package.json", "go.work", "Cargo.toml"
}
func Detect(root string) ([]Suggestion, error) // ordenado por Rel, sem duplicatas
```

  - `GET /api/projects/{id}/suggestions` responde `[]Suggestion`, sem as já cadastradas e sem as recusadas (chave `subproject_dismissed`, mapa `"<id>"` → `[]rel`).
  - `POST /api/projects/{id}/suggestions/dismiss` com `{"rels":[...]}`.
  - `scan.Result` ganha `Suggestions []workspaces.Suggestion \`json:"suggestions"\`` (só na resposta da API; o pacote `scan` não muda).

Regras:
- Lê os quatro arquivos sem dependência nova:
  - **`pnpm-workspace.yaml`:** linhas `- 'x'` ou `- x` abaixo de `packages:`;
  - **`package.json`:** `encoding/json`;
  - **`go.work`:** `use` em linha ou bloco;
  - **`Cargo.toml`:** o array `members` dentro de `[workspace]`, lido linha a linha.
- Expande `*` só no último pedaço (`filepath.Glob(root/apps/*)`), só diretórios, e ignora padrões com `!` ou `**`.
- Ignora pastas inexistentes e a própria raiz.
- **Aviso ao agente:** depois do cadastro (`nobre project add`), a saída do comando lista até 5 sugestões e `"mais N na Configuração → Projetos"`. A skill diz para listar ao usuário, sem cadastrar.

- [ ] **Step 1: Write the failing tests** (`detect_test.go`, árvore em `t.TempDir()`):
  - `TestDetectPnpm`: `packages:\n  - 'apps/*'\n  - packages/ui` com `apps/web`, `apps/api` e `packages/ui` dá os 3 com `Source "pnpm-workspace.yaml"`.
  - `TestDetectPackageJSONBothForms`: lista e objeto `{packages:[...]}`.
  - `TestDetectGoWork`: `use ./svc/a` e o bloco `use (\n ./svc/b\n)`.
  - `TestDetectCargo`: `[workspace]\nmembers = ["crates/*"]`.
  - `TestDetectSkipsMissingNegatedAndRoot`: `!apps/old`, `**` e `.` ignorados; pasta inexistente ignorada.
  - `TestDetectDedup`: a mesma pasta em pnpm e package.json aparece uma vez.
  - (api) `TestSuggestionsHideRegisteredAndDismissed`: `apps/web` já cadastrado e `apps/api` recusado: só `packages/ui`.
  - (cmd) `TestProjectScanPrintsSuggestions`: saída contém `"Sub-projetos sugeridos"` e `"apps/web"`; com `--nao-sugerir apps/web` chama a rota de recusa.
- [ ] **Step 2:** `go test ./internal/workspaces/ ./internal/api/ ./cmd/nobre/ -run 'Detect|Suggestions|PrintsSuggestions'`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat: sugere sub-projetos a partir das listas de workspaces do repositório`

### Task 7: API e CLI de cadastro: filhos, grupos, pai, card

**Files:**
- Modify: `internal/api/config.go` (`createProject` com `parent_id`, grupo, `patchProject` com `parent_id`), `internal/api/cards.go` (`PATCH /api/cards/{key}` aceita `project_id` da família), `internal/api/usage.go` (`?project=` expande a família)
- Modify: `cmd/nobre/admin.go` (`project add --pai`, `project pai`, `project grupo`)
- Test: `internal/api/config_test.go`, `internal/api/usage_test.go`, `internal/api/cards_test.go`, `cmd/nobre/admin_test.go`

**Interfaces:**
- Consumes: Task 2 inteira.
- Produces:
  - `POST /api/projects` com `{"name","path","parent_id"?}`. Sem `path` e sem `parent_id`, cria um grupo. Responde o projeto mais `"moved": Moved` (e o `reassigned` de hoje).
  - `PATCH /api/projects/{id}` com `"parent_id": <id>|null` responde o projeto mais `"moved"`. As mensagens de `ErrFamily` viram 400.
  - `GET /api/usage/totals?project=<id>` passa a filtrar por `FamilyIDs(id)`.
  - `PATCH /api/cards/{key}` com `{"project_id": n}` só dentro da família; fora dela, 400 `"o card só pode ir para um projeto da mesma família"`.
  - **CLI:**
    - `nobre project add <pasta> --pai <id|pasta>`;
    - `nobre project pai <id|pasta> <id|pasta|--nenhum>`;
    - `nobre project grupo "<nome>"`.

    As três imprimem `"N sessões e M cards passaram para <nome>."` quando `moved` não é zero. A `project pai` também imprime `"A captura de <filho> passa a seguir a de <pai> (ligada|desligada)."` quando elas diferiam.

- [ ] **Step 1: Write the failing tests:**
  - `TestCreateGroupViaAPI`: `POST {"name":"loja"}` dá 201 com `path == ""`.
  - `TestCreateChildViaAPIReportsMoved`.
  - `TestPatchParentErrorsAre400`: pasta fora do pai dá 400 com a mensagem do Task 2.
  - `TestUsageTotalsExpandsFamily`: `?project=<pai>` soma pai mais filho; `?project=<filho>` só o filho.
  - `TestPatchCardProjectWithinFamily`: no filho, 200; num projeto de outra família, 400.
  - `TestCLIProjectPaiWarnsCapture`: filho com captura desligada sob pai ligado imprime o aviso.
- [ ] **Step 2:** `go test ./internal/api/ ./cmd/nobre/ -run 'CreateGroup|CreateChild|PatchParent|ExpandsFamily|PatchCardProject|ProjectPai'`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(api): cadastro de sub-projetos e grupos, card movido dentro da família`

### Task 8: Interface: família no seletor, filtros, soma por app

**Files:**
- Modify: `web/src/api/types.ts` (`Project.parent_id: number | null`)
- Modify: `web/src/app/project.ts` (`familyIds`, `displayName`, `useProjectFilter` devolve `ids`), `web/src/app/Layout.tsx` (seletor em árvore), `web/src/ui/ProjectTag.tsx`
- Modify: `web/src/pages/Board.tsx`, `CardsList.tsx`, `Analytics.tsx`, `Worktrees.tsx`, `Prompts.tsx` (filtro pelo conjunto)
- Modify: `web/src/pages/projects.ts`, `Projects.tsx`, `ProjectPage.tsx` (soma por app)
- Test: `web/src/app/project.test.ts`, `web/src/pages/projects.test.ts`, `web/src/pages/Projects.test.tsx`, `web/src/pages/Board.test.tsx`

**Interfaces:**
- Consumes: `parent_id` (Task 1), `?project=` expandido (Task 7).
- Produces:
  - `familyIds(projects: Project[], id: number): number[]`;
  - `displayName(p: Project, projects: Project[]): string` (`"loja › web"`);
  - `useProjectFilter()` devolve também `ids: number[] | null` (null = Todos);
  - `buildRows` devolve, para um pai, `children: ProjectRow[]` e `general: ProjectRow` (o que caiu no pai), com o total da família na linha do pai.

Regras:
- Filtros trocam `c.project_id === projectId` por `ids.includes(c.project_id)`. Exceção, Prompts: com um filho escolhido, mostra os globais, os do pai e os do filho.
- `ProjectTag` usa `displayName`; a Sala de controle e os Terminais passam a mostrar "loja › web" sem filtrar nada.
- **Seletor:** filhos indentados abaixo do pai, ordenados por nome. O grupo aparece como opção normal.
- **Projetos:** linha do pai com o total e um botão "▸ apps" (`aria-expanded`) que abre as linhas dos filhos mais "geral".
- **Página de um pai:** quadro "Por app" com colunas `app | pasta | custo no período | cards (Em andamento/Pausado/Revisão/Concluído) | sessões vivas`, linha "geral" e linha "total". O cronograma ganha a etiqueta do app em cada entrega.
- **Página de um filho:** link "parte de <pai>".

- [ ] **Step 1: Write the failing tests:**
  - `project.test.ts`:
    - `familyIds of parent includes children`;
    - `familyIds of child is only itself`;
    - `displayName of child is "loja › web"`.
  - `projects.test.ts`, `family total equals children plus general`: custos 95,10 + 61,05 + 26,00. O total do pai é exatamente 182,15, e a soma das linhas é igual ao total (Review Focus 2).
  - `Board.test.tsx`:
    - `parent filter shows children cards`: `?projeto=<pai>` mostra card de app;
    - `child filter hides parent cards`.
  - `Projects.test.tsx`, `expands apps`: clicar em "▸ apps" mostra `web`, `api` e `geral`.
- [ ] **Step 2:** `cd web && pnpm test --run project projects Board Projects`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** `cd web && pnpm test --run && pnpm build`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(web): seletor em família, filtros por app e soma por app`

### Task 9: Interface de cadastro e card; documentação

**Files:**
- Modify: `web/src/pages/Config.tsx` (seção Projetos: "Novo grupo", "Mover para…", sugestões, aviso de captura)
- Modify: `web/src/pages/CardPage.tsx` (ou o painel de detalhes do card: escolha do projeto dentro da família)
- Modify: `README.md` (seção curta "Monorepo e sub-projetos" com `nobre project add --pai`, `nobre app`), `docs/status-piloto.md` (seção NAC-23)
- Test: `web/src/pages/Config.test.tsx`, `web/src/pages/CardPage.test.tsx`

Comportamento:
- **Configuração → Projetos:**
  - filhos indentados;
  - "Novo grupo" (só nome);
  - em cada projeto solto, "Mover para…", com a lista de pais válidos: grupos, e monorepos cuja pasta contém a dele. Se a captura difere, a confirmação mostra `"A captura de <filho> passa a seguir a de <pai> (ligada|desligada)."`; depois de confirmar, mostra `"N sessões e M cards passaram para <filho>."`;
  - num filho, "Desligar do projeto principal";
  - o toggle de captura só no pai e nos soltos.
- **Sugestões:** "N sub-projetos sugeridos", com uma caixa por pasta (marcadas por padrão) e os botões "Adicionar selecionados" (um `POST /api/projects` com `parent_id` por pasta) e "Não sugerir estes".
- **Card:** o campo Projeto vira um `<select>` com o pai e os filhos quando o projeto do card tem família, e faz `PATCH /api/cards/{key}`.

- [ ] **Step 1: Write the failing tests:**
  - `Config.test.tsx`:
    - `move to parent warns about capture`;
    - `adds selected suggestions as children` (dois `POST` com `parent_id`);
    - `dismiss suggestions calls dismiss route`;
    - `capture toggle hidden on child`.
  - `CardPage.test.tsx`: `card project select limited to family` (só pai e filhos nas opções) e um `PATCH` com `project_id` ao trocar.
- [ ] **Step 2:** `cd web && pnpm test --run Config CardPage`. Esperado: FAIL.
- [ ] **Step 3:** implementar e escrever os dois documentos.
- [ ] **Step 4:** `cd web && pnpm test --run && pnpm build`, depois `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(web): cadastro de sub-projetos e grupos, app do card na página do card`

### Task 10: Validação ao vivo e instalação

- [ ] **Step 1:** backup do banco com a API de backup do `sqlite3` do Python (`backup-pre-nac23-<data>.db`) e `make update`. Confira que `nobre project list` e o board mostram tudo como antes.
- [ ] **Step 2:** num monorepo real ou de teste (`pnpm-workspace.yaml` com `apps/*`), cadastre a raiz. As sugestões aparecem; adicione duas. Abra uma sessão na raiz, crie um card e rode `nobre app web`. O board filtrado pelo `web` mostra o card, e a página do principal soma por app com "geral".
- [ ] **Step 3:** crie um grupo e mova dois projetos soltos para ele. A soma e o filtro do grupo funcionam, e Worktrees lista os dois repositórios.
- [ ] **Step 4:** registre no card NAC-23 (`nobre test`, `nobre note`) e `nobre done`.
