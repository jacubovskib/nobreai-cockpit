# Grupo com pasta e varredura de sub-projetos (NAC-27)

Data: 2026-11-18. Card: NAC-27. Continua o NAC-23 (sub-projetos), já em produção.

## Por quê

O usuário trabalha em `~/workspace/bravonix`: uma pasta que **não é git** e contém repositórios independentes lado a lado (`turing-back`, `turing-front`, `turing-front-2`, `turing-api`, `portal_core`, `portal_ai`, `documentation_center`). A raiz tem `CLAUDE.md` e `Makefile` próprios, e o usuário às vezes abre o agente nela. Os worktrees do `turing-front-2` ficam em `bravonix/.worktrees`.

Hoje o NAC-23 resolve metade:
- dá para criar um grupo "bravonix" e pôr os repositórios nele;
- mas o grupo não tem pasta, então a sessão aberta na raiz fica "Sem projeto";
- e nada encontra os repositórios sozinho, porque as sugestões só leem listas de workspaces (pnpm, npm, Go, Cargo).

O usuário quer, nas palavras dele, "tratar como projeto geral e dentro dela os subprojetos", com uma varredura automática. Na UI, ele quer escolher a pasta do projeto, ver a varredura e marcar ou desmarcar as pastas que são sub-projetos.

## Objetivos

- **Grupo com pasta:** um grupo pode apontar para uma pasta que não é git. A sessão aberta nessa pasta (fora dos repositórios dela) cai no grupo, como "geral", e entra na soma e no filtro.
- **Varredura:** dada uma pasta, o Cockpit encontra os repositórios git dentro dela e os sugere como sub-projetos.
- **UI:** "Adicionar projeto" na Configuração ganha um seletor de pastas. Ao escolher a pasta, a varredura roda na hora e mostra a lista com caixas; o usuário confirma e o Cockpit cadastra o principal e os sub-projetos marcados de uma vez. Também dá para escolher pastas de sub-projeto à mão.
- **CLI e agente:** `nobre project add <pasta>` numa pasta que não é git com repositórios dentro cadastra o grupo com pasta e lista os repositórios sugeridos. A sessão aberta numa pasta assim, ainda não cadastrada, recebe o aviso de não cadastrado e lista a sugestão ao usuário, sem cadastrar sozinha.

## Fora do escopo e o que não fazer

- Features (NAC-24).
- Mais de um nível: continua valendo a regra do NAC-23.
- Varrer o disco inteiro ou a home: a varredura é só dentro da pasta escolhida.
- **Anti-objetivo:** rodar git na pasta do grupo. Ela não é repositório; branch base, worktrees, levantamento e detecção de merge continuam por repositório filho.
- **Anti-objetivo:** cadastrar sub-projetos sem o usuário marcar. A UI e a CLI sugerem; o agente só lista.
- **Anti-objetivo:** o seletor de pastas virar um navegador de arquivos. Ele lista só nomes de diretórios, nunca conteúdo de arquivos.

## Restrições

- O seletor de pastas é servido pelo daemon (o navegador não entrega caminhos absolutos de pastas locais). A API já só aceita loopback e Host local; a listagem fica restrita à home do usuário e abaixo dela, e segue symlinks só se o destino continuar dentro da home.
- O hook do `SessionStart` só pode ler o primeiro nível da pasta (um `ReadDir` e um `stat` de `.git` por entrada), sem git.
- Sem dependência nova.

## Abordagens consideradas

1. **Grupo ganha pasta opcional (escolhida).** Reaproveita o modelo do NAC-23: o grupo continua sendo o principal "sem git", agora com uma pasta. A regra de contenção de filhos vale como no monorepo, mas cada filho mantém o seu git.
2. **Pasta-workspace como tipo novo de projeto.** Mais explícito, mas duplica regras que o grupo já tem (família, soma, filtro, captura) e obriga toda tela a conhecer um terceiro tipo.
3. **Só a varredura, sem pasta no grupo.** Resolve o cadastro, mas a sessão na raiz continua fora do quadro, e o usuário perde o "geral" que pediu.

## Design

### Modelo

Uma coluna nova. Um projeto é **grupo** quando não tem git próprio na raiz da sua pasta, ou quando não tem pasta (o caso do NAC-23). Para não depender de git em toda leitura, o tipo fica explícito: `projects.kind TEXT NOT NULL DEFAULT 'repo'` com valores `repo` e `group` (migração 013; os grupos sem pasta do NAC-23 passam a `group`).

Regras para grupo com pasta:
- O filho precisa ter pasta **dentro** da pasta do grupo (mesma validação do monorepo).
- Cada filho é o seu próprio "projeto do repositório" (`RepoProject` devolve o filho, como hoje para grupos).
- `ProjectForPath` passa a considerar grupos com pasta: o prefixo mais longo continua valendo, então uma sessão em `bravonix/turing-back/src` cai no `turing-back`, e uma em `bravonix` ou `bravonix/docs` cai no grupo.
- Worktree ligado de um filho que fica fora da pasta dele (como `bravonix/.worktrees/turing-front-2-c-access`): a regra do NAC-23 (`ProjectForLocation`, pelo repositório principal) já o leva ao filho; o prefixo do grupo não pode vencer essa regra. Teste obrigatório.
- Git nunca roda na pasta de um grupo: integração, worktrees, levantamento e `nobre branch` pulam grupos, como já fazem com grupo sem pasta.
- Cards: continua valendo "grupo não recebe cards" do NAC-23? **Não para grupo com pasta.** Uma sessão na raiz do bravonix pode criar um card (por exemplo, para o `Makefile` da raiz). Grupo com pasta recebe cards; grupo sem pasta continua recusando. O "Continuar" de um card do grupo abre o agente na pasta do grupo, sem branch.
- Captura: é do grupo e vale para a família, como no NAC-23.

### Varredura

`workspaces.ScanRepos(root string) ([]Suggestion, error)`, no pacote do NAC-23:
- Desce até **2 níveis** abaixo da pasta. Para de descer numa pasta que é repositório (tem `.git`, pasta ou arquivo).
- Pula pastas ocultas (`.algo`), `node_modules`, `vendor`, `dist`, `build` e `target`. Por isso `.projects` e `.worktrees` do bravonix não aparecem.
- Não segue symlinks.
- Devolve as pastas que são repositórios, com `Source "git"`, ordenadas por caminho relativo.

Quando a pasta escolhida **é** um repositório git, a varredura usa as listas de workspaces do NAC-23 (monorepo). Quando **não é**, usa `ScanRepos` (grupo com pasta). As sugestões já cadastradas ou recusadas somem da lista, como no NAC-23.

### Seletor de pastas (API)

- `GET /api/fs/dirs?path=<abs>` devolve `{path, parent, dirs: [{name, path, git: bool, registered: bool}]}`. Sem `path`, começa na home. Fora da home, 400.
- `GET /api/fs/scan?path=<abs>` devolve `{path, git: bool, kind: "repo"|"monorepo"|"group", registered: <projeto ou null>, suggestions: [...]}`. É a mesma varredura da CLI, para uma pasta ainda não cadastrada.
- `POST /api/projects/batch` com `{"path", "name"?, "children": [{"path","name"?}...]}` cadastra o principal e os filhos numa transação e responde o principal, os filhos e `moved` (o histórico que passou para os filhos, como no NAC-23). Se o principal já existir, só adiciona os filhos.

### UI (Configuração → Projetos → "Adicionar projeto")

1. O botão abre um painel com o seletor: a pasta atual no topo, com o caminho clicável por partes, e a lista de subpastas. As subpastas que são repositórios git aparecem marcadas com um selo "git", e as já cadastradas com "cadastrado". Há um campo para colar um caminho, como hoje.
2. "Usar esta pasta" roda a varredura e mostra:
   - o tipo detectado: "Repositório", "Monorepo (apps sugeridos)" ou "Pasta com repositórios (grupo)";
   - o nome do projeto (editável, padrão o nome da pasta);
   - a lista de sub-projetos sugeridos, todos marcados, com caixa para desmarcar e o caminho relativo;
   - "Adicionar outra pasta como sub-projeto…", que reabre o seletor dentro da pasta do projeto para escolher à mão.
3. "Cadastrar" chama o `batch` e mostra o resultado: o projeto, quantos sub-projetos e quantas sessões e cards passaram para eles. As pastas desmarcadas podem ser marcadas como "não sugerir", com uma caixa no rodapé.

Projeto já cadastrado: o mesmo painel abre com "Varrer de novo" na linha do projeto principal, mostrando só as sugestões novas.

```
Adicionar projeto
~/workspace / bravonix                      [colar caminho]
  docs/            poc/          tools/
  turing-back/ git·cadastrado   turing-front/ git   portal_core/ git ...
[Usar esta pasta]

bravonix · Pasta com repositórios (grupo)      Nome [bravonix]
  [x] turing-back          (já cadastrado: entra no grupo)
  [x] turing-front
  [x] turing-front-2
  [x] turing-api
  [x] portal_core
  [x] portal_ai
  [ ] documentation_center
  + Adicionar outra pasta como sub-projeto…
  [ ] não sugerir as pastas desmarcadas
[Cadastrar]
```

Um repositório já cadastrado como projeto solto aparece na lista com "já cadastrado: entra no grupo". Marcado, ele é ligado ao grupo (com a regra de histórico e captura do NAC-23), e não recadastrado.

### CLI e agente

- `nobre project add <pasta>`: se a pasta não é git e a varredura acha repositórios, cadastra como grupo com pasta e imprime "Sub-projetos sugeridos" (até 5 mais "mais N na Configuração → Projetos"). Se a pasta não é git e não tem repositórios, recusa como hoje.
- `SessionStart` numa pasta que não é git: se o próprio cwd (nunca a home) tem repositórios no primeiro nível e não está cadastrado, a sessão recebe o aviso de não cadastrado com essa pasta, como acontece hoje com repositórios. A skill diz para cadastrar só a pasta e listar as sugestões ao usuário.

## O que fica para o construtor

- Formato da migração 013 e do `kind`, e como os grupos existentes são marcados.
- Nomes exatos das funções e componentes; layout do seletor em tela estreita.
- Limites de segurança da listagem (home, symlinks) com testes.
- Textos finais.

## Decisões depois da revisão do construtor (minhas, para o usuário conferir)

1. **Repositório não cadastrado dentro da pasta do grupo:** a sessão nele conta como "geral" do grupo (está dentro da pasta que o usuário cadastrou) e o `SessionStart` avisa que há um repositório novo, sugerindo `nobre project add <raiz> --pai <grupo>`, a não ser que essa pasta tenha sido recusada ("não sugerir"). O hook usa a raiz git que o `locate` já calcula; nada de git a mais. A tela do grupo mostra o repositório onde a sessão rodou (o `cwd` já está na sessão).
2. **Grupo sem pasta que já existe (NAC-23):** ganha pasta com `PATCH /api/projects/{id}` `{"path": ...}`, permitido só para `kind = group`; recusa se algum filho ficar fora da pasta nova. Hoje o banco do usuário não tem nenhum grupo (só nobreai-cockpit, mscloud e turing-back), então o bravonix nasce pelo `batch`. O `batch` acha o principal pelo caminho.
3. **Histórico do próprio grupo:** ao cadastrar o grupo com pasta, as sessões sem projeto cuja pasta fica dentro dele são adotadas pela regra do hook (`ProjectForLocation`): as que caem num filho vão para o filho, as demais para o grupo. Sessões que já têm projeto não sobem. A resposta traz `moved: {sessions, cards}` com o total de sessões e cards que mudaram de projeto (grupo e filhos).
4. **Repositório já ligado a outro principal, ou solto com filhos:** aparece na lista desabilitado, com o motivo ("pertence a X", "tem sub-projetos"). O `batch` é tudo ou nada para o que foi marcado; um item inválido devolve 400 com o motivo e nada é gravado. Um solto sem filhos marcado é ligado ao grupo (regra de histórico e captura do NAC-23).
5. **Repositório que é monorepo, achado pela varredura:** entra como filho simples, sem apps (um nível só). Pasta escolhida que é git e tem repositórios aninhados: tratada como repositório ou monorepo; os aninhados são ignorados.
6. **Sobreposição:** a pasta do grupo não pode ficar dentro de um projeto cadastrado (400). Repositórios cadastrados dentro da pasta e não marcados continuam soltos; o prefixo mais longo os mantém no projeto deles.
7. **Cards do grupo com pasta:** branch vazio; `nobre branch` e a detecção de merge recusam ou pulam ("grupo não tem branch"); sem worktree. Filtro e soma tratam o card como "geral".
8. **`kind` manda:** todo teste de grupo passa a usar `kind = 'group'` (hoje vários usam `Path == ""`). O `kind` não muda depois do cadastro, nem se alguém rodar `git init` na pasta.
