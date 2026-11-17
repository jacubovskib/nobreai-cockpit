# Sub-projetos: monorepo e grupo de repositórios (NAC-23)

Data: 2026-11-17. Card: NAC-23. Features (etiqueta que agrupa cards entre apps) ficaram para o NAC-24.

## Por quê

O usuário quer, nas palavras dele, "ver o custo e andamento do monorepo somado e filtrar por app".

Hoje cada projeto é uma pasta, e a sessão cai no projeto cuja pasta é o prefixo mais longo do diretório dela (`store.ProjectForPath`). Dá para cadastrar `loja` e `loja/apps/web` como dois projetos, mas eles não se conhecem:

- o custo e o andamento não somam;
- o filtro mostra um ou outro;
- os dois listariam os mesmos worktrees e teriam cada um o seu branch base, embora o repositório seja um só.

Front e back em repositórios separados têm o mesmo problema: não há como ver os dois como uma coisa só.

## Objetivos

- Um projeto pode ter sub-projetos. Isso cobre dois casos:
  - **monorepo:** os apps são pastas do mesmo repositório;
  - **grupo:** um projeto sem pasta própria, cujos sub-projetos são repositórios separados.
- **Para onde vai cada sessão e cada card:** a sessão fica no projeto da pasta onde começou: em `apps/web`, inclusive dentro de um worktree do monorepo, vai para `web`; na raiz, para o principal ("geral"). O card vai para o app que o agente informa com `nobre app <nome>`, ou que o usuário escolhe na página do card. O custo de uma sessão com card segue o card.
- **Soma:** escolher o projeto principal mostra o total (custo, cards por coluna, sessões vivas) e a parte de cada app. O que foi feito na raiz aparece como "geral".
- **Filtro por app:** o seletor de projeto mostra os sub-projetos abaixo do principal. Escolher o principal mostra tudo da família; escolher `web` mostra só o dele.
- **Cadastro:** ao cadastrar um monorepo, o Cockpit lê as listas de workspaces que o repositório declara e sugere os sub-projetos; o usuário confirma.
- **Projetos já cadastrados:** um projeto que já existe pode virar sub-projeto de outro (o caso `loja` mais `loja/apps/web` cadastrados soltos).
- Mover um card de app à mão, dentro da mesma família.

## Fora do escopo e o que não fazer

- Features: ficam para o NAC-24.
- Mais de um nível. Um sub-projeto não tem sub-projetos, e um projeto com sub-projetos não tem pai.
- Branch base, limites de custo ou regra de handoff por app.
- Decidir o app pelos arquivos que a sessão editou, ou trocar o projeto da sessão quando o agente muda de pasta: o app vem do card (`nobre app`), e a sessão sem card fica onde começou.
- Ler `project.json` do Nx ou listas de apps de outras ferramentas além das listadas em "Sugestão de sub-projetos".
- **Anti-objetivo:** contar custo duas vezes. O total da família é a soma do principal mais cada filho, uma vez cada.
- **Anti-objetivo:** listar o mesmo worktree duas vezes, ou mostrar dois branches base para o mesmo repositório.
- **Anti-objetivo:** o agente cadastrar dezenas de sub-projetos sozinho. Um repositório como o `mscloud` pode declarar muitos pacotes, e quem confirma é o usuário.

## Restrições

- Linux, SQLite e o `store` existente. A migração precisa manter todos os projetos, cards, sessões e prompts.
- Chaves de card continuam globais (`NAC-n`).
- O hook continua rápido. Achar o projeto de uma sessão nova já roda `projectFor(cwd)` (prefixo mais longo e, para worktree ligado, git). A regra nova não pode acrescentar mais chamadas de git do que já existem.

## Abordagens consideradas

1. **Sub-projeto é um projeto com pai (escolhida pelo usuário).** Cada app é um projeto normal com `parent_id`.
   - Reaproveita o prefixo mais longo, cards, custo por projeto, captura e prompts. A soma é "o projeto mais os filhos", e o grupo de repositórios entra do mesmo jeito (pai sem pasta).
   - Custo: o que é do repositório (branch base, worktrees, levantamento) precisa subir para o pai no monorepo, e a pasta do projeto deixa de ser obrigatória.
2. **"Apps" como tabela separada dentro do projeto.** A separação é mais limpa: o git fica sempre no projeto. Mas toda tela ganha um segundo nível, e o grupo de repositórios separados precisaria de um terceiro conceito.
3. **Só filtrar pela pasta, sem cadastro.** Descartada: card criado com `nobre card add` não tem pasta, e ficaria fora da soma e do filtro.

## Design

### Modelo

`projects` ganha `parent_id INTEGER NULL REFERENCES projects(id)`, e `path` deixa de ser obrigatório. Projeto sem pasta é um grupo. O `UNIQUE` continua, e o SQLite aceita vários `NULL`. Isso exige reconstruir a tabela na migração, como em `004_price_cache_nullable.sql`.

Regras de validação (API e store):
- **Um nível só:** o pai não pode ter pai, e um projeto com filhos não pode receber pai.
- **Grupo:** pode ter filhos com qualquer pasta. Não aceita sessão direto: sem pasta, `ProjectForPath` nunca o escolhe.
- **Pai com pasta (monorepo):** o filho precisa ter pasta **dentro** da pasta do pai.
- **Excluir:** projeto com filhos não pode ser excluído; os filhos saem antes ou são desligados.
- Desligar um filho (`parent_id = NULL`) o devolve a projeto solto, com seus cards e sessões.

**Tipo do filho, derivado e não guardado:**
- **App de monorepo:** a pasta do filho fica dentro da pasta do pai.
- **Repositório do grupo:** o pai não tem pasta.

**O "projeto do repositório"** de um projeto é:
- o pai, para um app de monorepo;
- o próprio projeto, nos outros casos.

Tudo o que é do git usa o projeto do repositório: branch base (`board.ProjectBase`), lista de worktrees (`/api/worktrees`), detecção de merge (`board.RunIntegrations`), recusa do branch base no `nobre branch` e levantamento (`nobre project scan`). O `base_branch` de um app de monorepo é ignorado, e a tela não o oferece.

### Para onde vai a sessão

1. **Pasta principal:** `ProjectForPath(cwd)` já escolhe a pasta mais funda, então sessão em `loja/apps/web` cai no `web`, e na raiz cai no `loja`.
2. **Worktree ligado do monorepo** (`loja/.worktrees/feat-x/apps/web`): hoje o worktree é ligado ao projeto pelo repositório principal (`AssignSessionsInWorktree` e o `projectFor` do adaptador). A regra nova:
   - acha o projeto do repositório como hoje;
   - calcula o caminho do `cwd` relativo à raiz do worktree (`apps/web`);
   - escolhe o filho cujo caminho relativo à pasta do pai é o prefixo mais longo desse caminho;
   - sem filho que case, fica no pai.

   Não há chamada de git a mais: a raiz do worktree já vem do `locate` que o hook faz.
3. **Captura:** é do projeto principal (inclusive de um grupo) e vale para a família. `projects.capture` de um filho é mantido igual ao do pai. Ligar ou desligar no pai aplica aos filhos, e a tela não mostra o controle no filho.

   Ao ligar um projeto que já existe a um pai com captura diferente, a tela avisa antes de confirmar, por exemplo: "A captura de bravonix passa a seguir a de loja (ligada)." Ao confirmar, o filho adota a do pai. Na CLI, `nobre project pai` imprime o mesmo aviso. Desligar o filho mantém a captura que ele tinha (decisão minha).

4. **Histórico ao criar um sub-projeto** (decisão minha). Ao criar um filho, ou ligar um projeto que já existe, sob um pai que já tem histórico:
   - as sessões do pai cujo `cwd` está dentro da pasta do filho, ou cujo caminho relativo no worktree casa com ela, passam para o filho;
   - um card do pai passa para o filho só se **todas** as sessões dele forem desse filho.

   A resposta da API e da CLI diz quantas sessões e cards mudaram, como o `reassigned` de hoje. Desligar um filho não devolve nada ao pai: ele vira projeto solto com o que tem.

### Cards

- Card criado numa sessão vai para o projeto da sessão (filho ou pai). O `nobre card add` também usa o projeto da sessão (`sessionProject`) e, sem sessão, o do `cwd`.
- **O agente informa o app do card** (escolha do usuário). A sessão continua com o projeto de onde começou, como hoje: no monorepo, quase sempre a raiz. O que põe o trabalho no app certo é o card, porque o custo de uma sessão com card já vai para o projeto do card (`COALESCE(c.project_id, s.project_id)` em `Totals`).
  - **Comando:** `nobre app <nome>` move o card da sessão para o sub-projeto `<nome>` da mesma família; `nobre app --geral` devolve ao principal. Nome fora da família é recusado, com a lista dos apps válidos. Sem card ligado, o comando avisa e não faz nada.
  - **Skill e contexto do `SessionStart`:** numa sessão de projeto com sub-projetos, o contexto lista os apps ("Apps deste projeto: web (apps/web), api (apps/api)") e orienta: quando o card for de um app só, rode `nobre app <nome>` ao criar o card ou ao começar a mexer nele.
  - O pacote de retomada mostra o app do card ("loja › web").
  - **Sessão sem card:** fica no projeto onde começou. Na raiz, isso é "geral".
  - Por que não as outras opções: a sessão seguir a pasta moveria o custo anterior e trocaria de projeto quando o agente pula entre apps; guardar o app em cada trecho de uso muda a gravação do uso e continua ambíguo num turno que mexe em dois apps.
- Na página do card, o campo do projeto vira uma escolha restrita à família: o pai ou um dos filhos. É assim que se move um card de app. Fora da família, a escolha não aparece.
- Os cards pausados no `SessionStart` (mesmo repositório e branch) passam a considerar a família inteira de um monorepo. Um card do `web` aparece para uma sessão na raiz do mesmo branch.

### Soma e filtro

- **Filtro (`?projeto=<id>`):**
  - escolher um projeto com filhos filtra pela família (o pai e os filhos);
  - escolher um filho filtra só por ele;
  - vale em Board, Cards, Analytics, Worktrees e Prompts, que hoje comparam `project_id === projectId` e passam a comparar com o conjunto da família;
  - no servidor, `TotalsFilter` e `CardFilter` ganham `ProjectIDs []int64` no lugar do id único (com o id único mantido por compatibilidade).
- **Seletor da barra lateral:** os filhos aparecem indentados abaixo do pai, com o nome do pai antes ("loja › web") onde o espaço for curto. A etiqueta do card em "Todos" mostra "loja › web".
- **Página Projetos:**
  - o principal aparece com os números da família (custo no período, entrega atual, parados, última atividade) e uma seta para abrir as linhas dos filhos;
  - a família tem uma linha "geral" com o que caiu direto no pai;
  - projeto solto continua como hoje.
- **Página do projeto principal:**
  - ganha um quadro por app (custo no período, cards por coluna, sessões vivas), com a linha "geral" e o total;
  - o cronograma mostra as entregas da família, cada uma com a etiqueta do app;
  - a página de um filho mostra só o dele, com um link para o principal.

```
loja (monorepo)            US$ 182   12 cards   2 sessões vivas
  web    apps/web           US$ 95    6 cards
  api    apps/api           US$ 61    4 cards
  geral  (raiz)             US$ 26    2 cards
```

- **Prompts:** um filho vê os prompts globais, os do pai e os dele.
- **Sala de controle e Terminais:** continuam sem filtro (decisão do NAC-5) e mostram a etiqueta "loja › web".

### Sugestão de sub-projetos

Ao cadastrar um projeto com pasta (UI ou `nobre project add`), e no `nobre project scan`, o Cockpit lê na raiz:

- `pnpm-workspace.yaml` (`packages:`);
- `package.json` (`workspaces` como lista ou `{packages: [...]}`);
- `go.work` (`use`);
- `Cargo.toml` (`[workspace] members`).

Os padrões com `*` são expandidos um nível (`apps/*` vira `apps/web`, `apps/api`). Ficam só as pastas que existem e não estão cadastradas. O nome sugerido é o último pedaço da pasta.

- **UI (Configuração → Projetos):** o projeto mostra "N sub-projetos sugeridos", com uma caixa por pasta e os botões "Adicionar selecionados" e "Não sugerir estes".
- **Recusa lembrada** (decisão minha): as pastas recusadas ficam guardadas por projeto (chave `subproject_dismissed` em `settings`, mapa projeto → pastas) e não voltam a ser sugeridas, nem na UI, nem no `scan`, nem no aviso ao agente. A CLI recusa com `nobre project scan --nao-sugerir <pasta>...`.
- **CLI:**
  - `nobre project scan` lista as sugestões numa seção "Sub-projetos sugeridos";
  - `nobre project add <pasta> --pai <id|pasta>` cadastra um filho;
  - `nobre project pai <id|pasta> <id-do-pai|pasta|--nenhum>` liga ou desliga um projeto que já existe.
- **Agente:** a skill e o aviso de repositório não cadastrado dizem para cadastrar só a raiz e **listar** ao usuário os sub-projetos sugeridos, sem cadastrá-los. Com mais de 5 sugestões, o agente cita as 5 primeiras e diz quantas mais existem na Configuração.

### Grupo de repositórios

- **Configuração → Projetos:** ganha "Novo grupo", só com nome. Um projeto solto pode ser posto no grupo ("Mover para o grupo…").
- **CLI:** `nobre project grupo "<nome>"` cria; `nobre project pai <projeto> <grupo>` liga.
- O grupo não tem branch base, worktrees nem levantamento próprios: cada filho tem os seus. Na página de Worktrees, o grupo aparece como cabeçalho com os repositórios dos filhos abaixo.

## O que fica para o construtor

- Formato exato da migração (reconstrução da tabela `projects`) e o teste que confere que os dados sobrevivem.
- Nomes das funções do store para a família (por exemplo `FamilyIDs(id)` e `RepoProject(p)`) e das rotas novas (sugestões, mudança de pai).
- Como o seletor mostra a árvore em tela estreita.
- Leitor de YAML para o `pnpm-workspace.yaml`: só a lista `packages`, sem dependência nova se der para ler linha a linha.
- Textos finais, seguindo o tom das telas atuais.
