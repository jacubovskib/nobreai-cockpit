# Grupo com pasta e varredura de sub-projetos (NAC-27): plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** uma pasta que não é git (como `~/workspace/bravonix`) vira projeto principal, do tipo grupo, com os repositórios de dentro como sub-projetos. A sessão aberta na raiz cai em "geral". Na tela, o usuário escolhe a pasta, a varredura sugere os sub-projetos, e ele marca ou desmarca.

**Architecture:**
- **Modelo:** `projects.kind` (`repo` | `group`) passa a ser a fonte de verdade de "é grupo", no lugar de `Path == ""`. Um grupo pode ter pasta. `ProjectForPath` e `ProjectForLocation` consideram grupos com pasta, sem roubar o worktree ligado de um filho.
- **Varredura e cadastro:** a varredura (`workspaces.ScanRepos`) e o seletor de pastas são servidos pelo daemon. Um `batch` cadastra o principal e os filhos numa transação.

**Tech Stack:** Go (stdlib, SQLite pelo `store`), React + TanStack Query + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-18-grupo-com-pasta-design.md` (inclui a seção "Decisões depois da revisão do construtor", que é obrigatória).

## Global Constraints

- **Git:** nunca roda na pasta de um grupo (integração, worktrees, levantamento, `nobre branch` e detecção de merge pulam grupos).
- **Seletor de pastas:** lista só nomes de diretórios, só dentro da home do usuário (após `filepath.Clean` e `EvalSymlinks`; fora da home, 400). Nunca mostra conteúdo de arquivos.
- **Varredura:**
  - desce até 2 níveis e para numa pasta que é repositório (`.git` como pasta ou arquivo);
  - pula pastas ocultas, `node_modules`, `vendor`, `dist`, `build` e `target`;
  - não segue symlinks.
- **`SessionStart`:** numa pasta que não é git, só lê o primeiro nível do próprio cwd (nunca a home), sem git.
- **Cadastro:** o `batch` é tudo ou nada.
- **Agente:** cadastra só a pasta e lista as sugestões, sem cadastrá-las.
- **Cards:** um grupo com pasta recebe cards (sem branch e sem worktree). O grupo sem pasta continua recusando, com `msgGroupNoCards`.
- **`kind`:** não muda depois do cadastro.
- **Migração:** 013 (a 012 é do NAC-6).
- Textos em pt-BR, sem travessão no meio de frase. Sem dependência nova.
- **Toda tarefa:** `go test ./...` e `cd web && pnpm test --run` verdes antes do commit. Nunca adicionar `.superpowers/` ao git.

## Review Focus

1. **Worktree ligado de um filho fora da pasta dele, mas dentro da pasta do grupo** (`bravonix/.worktrees/turing-front-2-c-access`): cai no `turing-front-2`, não no grupo. Teste na Task 1.
2. **`GET /api/fs/dirs` com `..` ou com symlink que sai da home:** 400, sem listar nada. Teste na Task 2.
3. **Sessão num repositório não cadastrado dentro do grupo:**
   - conta como "geral" do grupo;
   - o `SessionStart` sugere `nobre project add <raiz> --pai <grupo>`;
   - se a pasta foi recusada, não sugere.

   Teste na Task 4.
4. **`batch` com um item inválido** (repositório que já pertence a outro principal): 400 com o motivo, e nada gravado. Teste na Task 3.
5. **Grupo sem pasta do NAC-23** depois da troca para `kind`: continua recusando cards, sem worktrees e sem levantamento. Testes nas Tasks 1 e 3.

---

### Task 1: `kind`, grupo com pasta e a regra de dono da sessão

**Files:**
- Create: `internal/store/migrations/013_project_kind.sql`
- Modify: `internal/domain/types.go` (`Project.Kind`, `IsGroup()`), `internal/store/projects.go` (`ProjectForPath`, `ProjectForLocation`, `CreateProject`, `UpdateProject`, `ListProjects`), `internal/store/family.go` (`CreateGroup`, `checkInside`, `RepoProject`, `moveHistory`, validações)
- Test: `internal/store/group_folder_test.go`

**Interfaces:**
- Produces:
  - `domain.Project.Kind string` com json `"kind"` e valores `"repo"` ou `"group"`;
  - `func (p domain.Project) IsGroup() bool`;
  - `(*Store) CreateGroupAt(name, path string) (domain.Project, error)`: grupo com pasta. A pasta precisa ser absoluta, não pode ser a home nem ancestral dela, e não pode ficar dentro de um projeto cadastrado (`ErrFamily`: `"a pasta do grupo fica dentro do projeto <nome>"`);
  - `CreateGroup(name)` continua criando grupo sem pasta, agora com `kind='group'`;
  - `UpdateProject` aceita mudar a pasta de um grupo, inclusive de vazia para uma pasta. Recusa se algum filho ficar fora (`ErrFamily`: `"o sub-projeto <nome> fica fora da pasta nova do grupo"`).

Regras:
- **Migração:** `ALTER TABLE projects ADD COLUMN kind TEXT NOT NULL DEFAULT 'repo'` e `UPDATE projects SET kind='group' WHERE path IS NULL`.
- **Testes de grupo:** todo teste que hoje usa `Path == ""` para dizer "é grupo" passa a usar `IsGroup()` no store (`family.go:65,109,195,280` e `projects.go:65,73,134`). "Tem pasta" continua sendo `Path != ""`.
- **Filho de grupo com pasta:** precisa ficar dentro da pasta do grupo. `RepoProject` de um filho de grupo devolve o próprio filho.
- **`ProjectForPath`:** inclui grupos com pasta no prefixo mais longo. Grupos sem pasta continuam fora.
- **`ProjectForLocation(cwd, l)`:** se o projeto achado pelo prefixo é um grupo e a sessão está num worktree ligado (`l.WorktreeLinked`) cujo `l.Main` resolve, por `ProjectForPath(Main + rel)`, para um projeto que não é grupo, esse projeto vence.

- [ ] **Step 1: Write the failing tests** (`group_folder_test.go`):
  - `TestMigrationMarksPathlessGroups`: um grupo sem pasta criado antes da 013 fica com `Kind == "group"`, e um repositório fica com `"repo"`.
  - `TestCreateGroupAtRules`: `/w/bravonix` cria. A home é recusada. `/w/repo/sub`, com `/w/repo` cadastrado, é recusado com a mensagem acima.
  - `TestGroupFolderChildMustBeInside`: filho `/w/bravonix/turing-back` passa; filho `/w/outro` dá `ErrFamily`.
  - `TestProjectForPathGroupFolder`:
    - `/w/bravonix` e `/w/bravonix/docs` caem no grupo;
    - `/w/bravonix/turing-back/src` cai no filho;
    - `/w/fora` não cai em nada.
  - `TestProjectForLocationLinkedWorktreeBeatsGroup` (Review Focus 1): grupo `/w/bravonix` com filho `/w/bravonix/turing-front-2`; `cwd=/w/bravonix/.worktrees/x/src`, `l={WorktreeLinked:true, Worktree:"/w/bravonix/.worktrees/x", Main:"/w/bravonix/turing-front-2"}`. O resultado é o filho.
  - `TestGroupGainsFolder`: grupo sem pasta com filho `/w/b/x` recebe `Path="/w/b"`; com filho `/w/c/y`, recusa.
  - `TestRepoProjectOfGroupFolderChild`: devolve o filho.
- [ ] **Step 2:** `go test ./internal/store/ -run 'MarksPathless|CreateGroupAt|GroupFolder|LinkedWorktreeBeatsGroup|GainsFolder|RepoProjectOfGroupFolder'`. Esperado: FAIL.
- [ ] **Step 3:** implementar e rodar `go test ./...`. Os testes do NAC-23 sobre grupos sem pasta precisam continuar passando.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(store): grupo com pasta e kind explícito`

### Task 2: Varredura de repositórios e seletor de pastas (API)

**Files:**
- Create: `internal/workspaces/repos.go`, `internal/workspaces/repos_test.go`, `internal/api/fs.go`, `internal/api/fs_test.go`
- Modify: `internal/api/server.go` (rotas; `Deps.Home string`, vazio = `os.UserHomeDir()`, para os testes)

**Interfaces:**
- Produces:
  - `workspaces.ScanRepos(root string) ([]Suggestion, error)`, com `Source: "git"`, ordenado por `Rel`.
  - `GET /api/fs/dirs?path=<abs>` → `{"path","parent","dirs":[{"name","path","git":bool,"registered":bool}]}`.
    - Sem `path`, começa na home.
    - `parent` é `""` na home.
    - As pastas ocultas são omitidas.
    - Fora da home, ou se não for diretório, 400 `"pasta fora da home do usuário"` ou `"não é uma pasta"`.
  - `GET /api/fs/scan?path=<abs>` → `{"path","git":bool,"kind":"repo"|"monorepo"|"group","name","registered":Project|null,"suggestions":[...]}`.
    - Pasta git com sugestões de workspace: `monorepo`. Pasta git sem sugestões: `repo`.
    - Pasta não git com repositórios: `group`.
    - Pasta não git sem repositórios: 400 `"a pasta não é um repositório git nem tem repositórios dentro"`.
    - Cada sugestão ganha `"registered": Project|null` e `"blocked": "<motivo>"|""`. O motivo é `"pertence a <pai>"` quando já é filho de outro principal, e `"tem sub-projetos"` quando é solto com filhos.
    - Some da lista o que estiver recusado em `subproject_dismissed` (chave do projeto principal já cadastrado).

- [ ] **Step 1: Write the failing tests:**
  - `repos_test.go`:
    - `TestScanReposDepthAndStop`: árvore com `a/.git`, `b/c/.git`, `b/c/d/.git` (aninhado, não aparece), `e/f/g/.git` (3 níveis, não aparece) e `h/.git` como arquivo (worktree). O resultado é `["a","b/c","h"]`.
    - `TestScanReposSkips`: `.oculta/x/.git`, `node_modules/y/.git` e `dist/z/.git` não aparecem. Um symlink para um repositório não é seguido.
  - `fs_test.go` (com `Deps.Home` em `t.TempDir()`):
    - `TestFsDirsListsHomeAndMarksGit`;
    - `TestFsDirsRejectsOutsideHome` (Review Focus 2): `path=<home>/../`, um `/etc` absoluto e um symlink dentro da home apontando para fora dão 400;
    - `TestFsScanGroupFolder`: pasta com dois repositórios dá `kind "group"` e as duas sugestões. Um repositório cadastrado como filho de outro grupo vem com `blocked "pertence a outro"`;
    - `TestFsScanMonorepo`: pasta git com `pnpm-workspace.yaml` dá `kind "monorepo"`;
    - `TestFsScanEmptyFolder400`.
- [ ] **Step 2:** `go test ./internal/workspaces/ ./internal/api/ -run 'ScanRepos|FsDirs|FsScan'`. Esperado: FAIL.
- [ ] **Step 3:** implementar. Para saber se uma pasta é git, basta `os.Stat(<dir>/.git)`, sem rodar git. O `monorepo` reusa `workspaces.Detect`.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat: varredura de repositórios e seletor de pastas no daemon`

### Task 3: `batch`, pasta do grupo pela API e cards no grupo com pasta

**Files:**
- Create: `internal/api/batch.go`, `internal/api/batch_test.go`
- Modify: `internal/api/config.go` (`patchProject`: pasta de grupo), `internal/api/cards.go`, `internal/api/me.go` (`meApp --geral`; `meBranch` recusa grupo), `internal/api/continue.go`, `internal/api/worktrees.go`, `internal/api/scan.go`, `internal/api/suggestions.go`, `internal/board/integration.go`, `internal/resume/forcard.go`, `internal/adapter/claude/context.go:233` (todos: teste de grupo por `IsGroup()`)

**Interfaces:**
- Consumes: Task 1 (`CreateGroupAt`, `IsGroup`, `UpdateProject`) e Task 2 (`ScanRepos` e a lógica de `blocked`).
- Produces: `POST /api/projects/batch` com `{"path","name"?,"children":[{"path","name"?}]}` →
  `{"project":Project,"children":[Project],"moved":{"sessions":n,"cards":n}}`.
  - Se `path` já é um projeto cadastrado, só adiciona os filhos.
  - Se não é, cria um repositório (pasta git) ou um grupo com pasta (não git), detectando pelo `.git`.
  - Filho já cadastrado e solto é ligado com `SetParent`. Filho novo é criado com `CreateChild`.
  - Qualquer item inválido (fora da pasta, bloqueado ou nome repetido) dá 400 com a mensagem da regra, e nada é gravado. Tudo numa transação: se o store não tiver uma transação única para criar e ligar, crie `(*Store) CreateFamily(parent domain.Project, children []ChildSpec) (domain.Project, []domain.Project, Moved, error)` no store.
  - **Adoção** (decisão 3 do spec): depois do commit, as sessões vivas e encerradas com `project_id` NULL cuja pasta fica dentro do principal são resolvidas com o mesmo `sessionOwner` (`internal/api/configlog.go`, regra do hook com `Locate`) e adotadas só quando o dono é da família. Elas somam no `moved`.

Regras:
- **Cards num grupo com pasta são aceitos.** O grupo sem pasta continua recusando com `msgGroupNoCards` (`cards.go:140,254` e `me.go:406` passam a testar `IsGroup() && Path == ""`).
- **`nobre branch` num card de grupo:** 400 `"grupo não tem branch"`.
- **"Continuar" num card de grupo com pasta:** abre o agente na pasta do grupo, sem branch. Num grupo sem pasta, continua 409.
- **Worktrees, levantamento, sugestões de monorepo e integração:** pulam todo grupo (`IsGroup()`).
- **`PATCH /api/projects/{id}` com `path` num grupo:** usa a regra da Task 1 e adota as sessões como no `batch`.

- [ ] **Step 1: Write the failing tests** (`batch_test.go` e ajustes nos testes existentes):
  - `TestBatchCreatesGroupFolderWithChildren`: pasta não git com `a/.git` e `b/.git` cria o grupo e dois filhos, com `kind "group"`.
  - `TestBatchLinksExistingStandalone`: um `turing-back` já cadastrado e solto vira filho, e as sessões dele continuam nele.
  - `TestBatchAtomicOnBlockedChild` (Review Focus 4): um filho que pertence a outro principal dá 400 `"pertence a"`, e nem o grupo nem os outros filhos são gravados.
  - `TestBatchAdoptsRootSessions`: uma sessão sem projeto em `<grupo>/docs` vai para o grupo, e uma em `<grupo>/a/src` vai para o filho `a`. `moved.sessions == 2`.
  - `TestGroupFolderAcceptsCards`: `POST /api/cards` com o id do grupo com pasta dá 201.
  - `TestPathlessGroupStillRefusesCards` (Review Focus 5): 400 `msgGroupNoCards`.
  - `TestBranchRefusedOnGroupCard`.
  - `TestPatchGroupGainsFolder`.
- [ ] **Step 2:** `go test ./internal/api/ -run 'Batch|GroupFolderAccepts|PathlessGroup|BranchRefusedOnGroup|PatchGroupGains'`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(api): cadastro em lote e grupo com pasta recebendo cards`

### Task 4: CLI e agente

**Files:**
- Modify: `cmd/nobre/admin.go` (`project add`), `internal/client/client.go` (`ProjectsBatch`, se a CLI usar), `internal/adapter/claude/context.go` (`unregisteredRoot`), `internal/adapter/claude/skill.md`
- Test: `cmd/nobre/admin_test.go`, `internal/adapter/claude/unregistered_test.go`

**Interfaces:**
- Consumes: Tasks 1-3.
- Produces:
  - `nobre project add <pasta>` numa pasta não git com repositórios cadastra o grupo com pasta (`POST /api/projects/batch` sem filhos) e imprime:
    - `"Grupo <nome> cadastrado: <pasta>"`;
    - em seguida, `"Sub-projetos sugeridos (liste ao usuário, não cadastre sem ele pedir):"` com até 5 linhas `"  <rel>  (git)"` e `"  mais N na Configuração → Projetos"`.

    Pasta não git sem repositórios: o erro de hoje.
  - `unregisteredRoot(cwd, l)` passa a cobrir dois casos novos, devolvendo a raiz e o tipo do aviso:
    1. **cwd não é git e não tem projeto, mas tem repositórios no primeiro nível:** o aviso é o de hoje, com a pasta, e manda cadastrar com `nobre project add <pasta>`.
    2. **cwd num repositório (`l.Main`) não cadastrado dentro de um grupo com pasta, e não recusado em `subproject_dismissed`:** o aviso é `"NobreAI Cockpit: o repositório <raiz> fica dentro do grupo <nome> e não está cadastrado. Liste ao usuário a opção de cadastrá-lo com nobre project add <raiz> --pai <id do grupo>; não cadastre sem ele pedir."`. A sessão continua registrada no grupo como "geral", e o contexto normal do registro vem junto.

- [ ] **Step 1: Write the failing tests:**
  - `TestProjectAddGroupFolderPrintsSuggestions` (cmd, com servidor fake).
  - `TestUnregisteredNonGitFolderWithRepos`: o contexto contém `"nobre project add <pasta>"`. Uma pasta não git sem repositórios não recebe aviso, e a home nunca recebe.
  - `TestUnregisteredRepoInsideGroupSuggestsPai` (Review Focus 3): sessão em `<grupo>/novo` (git, não cadastrado). O contexto contém `"--pai"`, e a sessão tem `ProjectID == grupo`. Depois de recusar `novo` no `subproject_dismissed`, o aviso some.
- [ ] **Step 2:** `go test ./cmd/nobre/ ./internal/adapter/claude/ -run 'GroupFolder|NonGitFolder|InsideGroup'`. Esperado: FAIL.
- [ ] **Step 3:** implementar e acrescentar uma linha na skill sobre grupos com pasta, no mesmo tom.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat: cadastro de grupo com pasta pelo terminal e aviso ao agente`

### Task 5: Tela "Adicionar projeto" com seletor e varredura

**Files:**
- Create: `web/src/pages/config/FolderPicker.tsx`, `web/src/pages/config/AddProject.tsx`, `web/src/pages/config/AddProject.test.tsx`
- Modify: `web/src/api/types.ts` (`Project.kind`, tipos de `fs/dirs`, `fs/scan` e `batch`), `web/src/pages/Config.tsx` (a seção Projetos usa `AddProject` no lugar do campo de caminho; "Varrer de novo" na linha de um principal)

**Interfaces:**
- Consumes: as rotas das Tasks 2 e 3.

Comportamento (texto e layout do spec):
1. **Seletor:**
   - caminho atual em partes clicáveis;
   - subpastas com selos `git` e `cadastrado`;
   - campo "colar caminho";
   - botão "Usar esta pasta".
2. **Resultado da varredura:**
   - tipo detectado: `"Repositório"`, `"Monorepo (apps sugeridos)"` ou `"Pasta com repositórios (grupo)"`;
   - campo "Nome";
   - lista de sugestões, todas marcadas, menos as `blocked`, que ficam desabilitadas com o motivo. Uma sugestão já cadastrada aparece com `"já cadastrado: entra no grupo"`;
   - "Adicionar outra pasta como sub-projeto…", que reabre o seletor começando na pasta do projeto;
   - a caixa "não sugerir as pastas desmarcadas".
3. **"Cadastrar":** chama o `batch` e mostra `"<nome> cadastrado com N sub-projetos."` e, se for o caso, `"N sessões e M cards passaram para os sub-projetos."` (com singular e plural). Com a caixa marcada, chama a recusa das desmarcadas.
4. **"Varrer de novo" num principal:** abre o resultado direto, só com as sugestões novas.

- [ ] **Step 1: Write the failing tests** (`AddProject.test.tsx`, com `fetch` mockado):
  - `browses into a folder and shows git badges`;
  - `scan of a group folder lists suggestions checked and blocked disabled`;
  - `register posts batch with checked children only`;
  - `dismiss unchecked when box marked`;
  - `manual add opens picker inside project folder`.
- [ ] **Step 2:** `cd web && pnpm test --run AddProject`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
- [ ] **Step 4:** `cd web && pnpm test --run && pnpm build`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(web): adicionar projeto escolhendo a pasta, com varredura de sub-projetos`

### Task 6: Interface: `kind` no lugar de `path === ''` e documentação

**Files:**
- Modify: `web/src/app/project.ts` (`isGroup(p)`), e todo `path === ''` ou `path !== ''` que significa "grupo" em `web/src` (Worktrees, CardPage, NewTask, Config, projects.ts, ProjectPage, entre outros: faça um grep). Também `README.md` (seção "Monorepo e sub-projetos": grupo com pasta, a tela nova e o `nobre project add` numa pasta não git) e `docs/status-piloto.md` (seção NAC-27).
- Test: os testes existentes ajustados, mais:
  - `CardPage.test.tsx`: `group with folder is a valid card target`;
  - `Worktrees.test.tsx`: `group with folder renders as header without git`.

Regras:
- Grupo com pasta aparece como cabeçalho em Worktrees, como o grupo sem pasta, sem rodar nada.
- Ele é destino válido de card em CardPage e NewTask. O grupo sem pasta continua fora.
- A linha "geral" da página do principal mostra `(raiz)` no lugar do caminho completo. Isso conserta o detalhe visual anotado no NAC-25.

- [ ] **Step 1:** escrever os dois testes novos. Esperado: FAIL.
- [ ] **Step 2:** `cd web && pnpm test --run CardPage Worktrees`. Esperado: FAIL.
- [ ] **Step 3:** implementar e escrever a documentação.
- [ ] **Step 4:** `cd web && pnpm test --run && pnpm build`, depois `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(web): grupo com pasta nas telas e documentação`

### Task 7: Validação ao vivo e instalação

- [ ] **Step 1:** backup do banco com a API de backup do `sqlite3` do Python (`backup-pre-nac27-<data>.db`) e `make update`.
- [ ] **Step 2:** cadastrar o bravonix pela tela:
  1. Configuração → Adicionar projeto → seletor até `~/workspace/bravonix`;
  2. "Usar esta pasta" mostra "Pasta com repositórios (grupo)" com os 7 repositórios, e `turing-back` como "já cadastrado: entra no grupo";
  3. desmarcar o que o usuário não quiser: perguntar a ele antes de cadastrar, porque é o banco real;
  4. "Cadastrar".
- [ ] **Step 3:** conferir:
  - na página Projetos, `bravonix ▸ apps`, com "geral";
  - em Worktrees, o grupo como cabeçalho, com os worktrees do `turing-front-2` sob ele;
  - uma sessão de teste na raiz do bravonix cai em "geral".
- [ ] **Step 4:** registrar no card NAC-27 (`nobre test`, `nobre note`) e `nobre done`.
