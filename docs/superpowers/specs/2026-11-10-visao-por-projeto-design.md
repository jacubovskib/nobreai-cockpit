# Visão por projeto — design

Card: NAC-5. Conversa de brainstorm em 2026-11-10. Mockups aprovados em
`.superpowers/brainstorm/666439-1794329495/content/` (`board-todos.html`, opção A;
`comparacao.html`, opção A).

## Por que

O Cockpit já guarda o projeto de tudo: `cards.project_id` é obrigatório, e
`sessions.project_id` e `prompts.project_id` também existem. Mesmo assim, quase
nenhuma tela usa esse dado. O usuário sentiu falta disso em 2026-11-10, em duas
situações (palavras dele):

- **foco**: trabalhar em um repositório sem o resto atrapalhando;
- **comparação**: saber "qual projeto está gastando mais e o que anda parado",
  "visualizar no board e cards qual projeto está em que fase" e ter "uma forma de
  ver o cronograma do projeto".

O cronograma, para o usuário, nasce "quando fazemos uma spec e um planejamento de
implantação pelo agente". Ele mostra **a ordem e o andamento**, sem datas
previstas.

## Objetivos

1. Um seletor de projeto global que filtra as telas onde o foco ajuda.
2. Em "Todos", deixar claro de que projeto é cada card e o que está parado.
3. Uma página **Projetos** que compara os projetos lado a lado e abre o
   cronograma de cada um.
4. As tarefas do plano de uma entrega aparecem dentro do card e alimentam o
   cronograma, registradas pelo próprio agente.

## Fora do escopo (cards próprios)

- **NAC-6**: detectar mudanças de configuração que afetam os agentes.
- **NAC-7**: o agente pergunta se deve cadastrar um repositório não cadastrado.
  A regra de hoje (o agente nunca cadastra sozinho) **continua valendo** neste
  trabalho.
- **NAC-8**: sugerir "salvar como prompt" para pedidos repetidos.
- Configurações por projeto, como limite de custo ou regra de handoff por
  projeto. Isso fica perto do NAC-4.
- Datas previstas, estimativas ou prazos no cronograma.

## Antiobjetivos (falharia mesmo funcionando)

- A Sala de controle ou os Terminais esconderem uma sessão viva ou um alerta
  porque o seletor está em outro projeto.
- O Board virar uma lista de tarefas pequenas: o Board continua mostrando
  **entregas**.
- O usuário ter de preencher o plano ou o andamento na mão. Quem registra é o
  agente.
- O cronograma depender do formato de plano do Superpowers. Tem de funcionar
  com Codex e Kiro.

## Decisões do usuário

- **Seletor em "Todos" no Board**: as colunas de hoje continuam. Cada card ganha
  uma etiqueta com o projeto (opção A; a opção de uma raia por projeto foi
  descartada).
- **1 entrega = 1 card.** As tarefas do plano ficam **dentro do card**, não viram
  cards.
- **Cronograma**: a ordem e o andamento das entregas, sem datas previstas.
- **Comparação e cronograma ficam numa página nova "Projetos"** (opção A). A
  opção de espalhar isso pelo Analytics e pela Sala foi descartada.
- **Tarefas registradas pelo agente** com comandos `nobre`. O hook detecta um
  plano escrito sem registro e cobra no fim do turno. O usuário não discordou
  dessa proposta.

## Como fica

### 1. Seletor de projeto

- Fica na barra lateral, acima do menu (`web/src/app/Layout.tsx`). As opções
  são "Todos" e cada projeto cadastrado. O padrão é "Todos".
- O projeto escolhido vai na URL (por exemplo `?projeto=<id>`) e acompanha a
  navegação entre as telas, para dar para mandar o link.
- **Filtra:** Board, Cards, Analytics, Worktrees e Prompts. Prompts mostra os
  globais e os do projeto escolhido.
- **Não filtra:** Sala de controle e Terminais. Elas mostram a etiqueta do
  projeto em cada sessão, e as sessões sem projeto aparecem como "Sem projeto".
  Limites do plano, preços e agentes continuam globais, porque o plano é da
  conta.
- Com um projeto escolhido, "Nova tarefa" já vem com ele selecionado.

### 2. Board e Cards em "Todos"

- Cada card mostra uma etiqueta colorida com o nome do projeto. A cor é fixa
  por projeto e o nome aparece sempre (não só a cor).
- **Parado** (decisão do Claude): card em **Em andamento ou Pausado** sem
  nenhuma atividade (evento ou sessão) há mais de 24 h. O card mostra "parado
  há N dias" (ou horas). Backlog não conta como parado, porque ainda não
  começou. Concluído também não. Revisão tem sinal próprio ("aguardando revisão"; ver a seção no fim).
  Como toda sessão que termina leva o card de Em andamento para Pausado, um
  card pausado há menos de 24 h é só um intervalo, não algo parado.
- A lista de Cards ganha uma coluna "Projeto" e segue o seletor.
- Com um projeto escolhido, a etiqueta pode ficar oculta (fica a critério de
  quem implementar).

### 3. Página Projetos (item novo no menu)

Uma linha por projeto, com o período padrão de **7 dias**, que pode ser mudado:

- **Custo no período**, com uma barra comparativa entre os projetos. Inclui as
  sessões **sem card** que pertencem ao projeto (via `sessions.project_id`), não
  só o uso ligado a cards.
  - Decisão do Claude: projeto com captura desligada (`capture=false`) aparece
    na lista com "captura desligada" no lugar dos números.
  - O significado de `capture=false` não muda: a sessão continua sem projeto.
    Ligar o custo ao projeto mesmo sem captura é uma decisão separada, que não
    entra aqui.
- **Entrega atual** (decisão do Claude): o card com atividade mais recente
  entre os que estão em Em andamento, Pausado ou Revisão, com o progresso das
  tarefas do plano ("1/4" e os blocos). Sem plano registrado, aparece só o
  título. Sem nenhum card nessas colunas, aparece "nada em andamento".
- **Parados**: quantos cards estão parados e o mais antigo ("1 card · 2d").
- **Última atividade.**

A linha leva à página do projeto.

### 4. Página do projeto: cronograma

- Lista as entregas (cards) do projeto **em ordem**: concluídas, depois em
  andamento, depois paradas, depois o backlog. Dentro de cada grupo, a ordem é
  por data de início ou conclusão.
  - Decisão do Claude sobre os grupos: "em andamento" reúne Em andamento,
    Pausado há menos de 24 h e Revisão. "Paradas" segue a definição da seção 2.
- Cada entrega mostra: título, chave, estado (concluída, em andamento, parada há
  N dias ou backlog), os blocos de progresso das tarefas e uma linha de
  contexto. Por exemplo: "merge 09/11", "tarefa 2: filtros no board" ou "plano
  escrito, não iniciado".
- Clicar numa entrega abre o card.

### 5. Plano dentro do card

- O card ganha uma seção **Plano**: o caminho do arquivo do plano (se houver) e
  a lista de tarefas, com número, título, estado e data da última mudança.
- Estados de uma tarefa: `a fazer`, `em andamento`, `em revisão`, `concluída`.
- **Card tem plano** = o card tem pelo menos 1 tarefa registrada. O card guarda
  **um** caminho de plano. A spec não é registrada como plano.
- **Tarefas e "Falta"** (decisão do Claude): as duas listas convivem.
  - A tarefa é um passo do plano. O item de "Falta" (`nobre todo`) é uma
    pendência solta.
  - Na prontidão do handoff, tarefas em aberto satisfazem o critério "lista
    Falta não está vazia" (`internal/domain/readiness.go`).
  - O pacote de retomada e o prompt de handoff listam as tarefas em aberto.
  - A skill explica quando usar cada uma.
- Um mesmo arquivo de plano pode ser ligado a mais de um card, cada um com as
  próprias tarefas. O caso esperado, porém, é um plano por entrega.

### 6. Como as tarefas chegam ao Cockpit

**Comandos novos para o agente** (os nomes finais ficam com quem implementar,
seguindo o padrão dos que já existem):

- `nobre plan <arquivo> [--task "<título>" ...]`: liga um plano ao card e
  registra as tarefas em ordem. Rodar de novo atualiza a lista (o plano mudou).
- `nobre task <n> <estado>`: muda o estado da tarefa *n*.

**A skill** (`internal/adapter/claude/skill.md`) ensina quando usar: ao terminar
de escrever uma spec ou um plano, e a cada tarefa iniciada, mandada para revisão
ou concluída.

**O hook garante**, no adaptador do Claude Code (`internal/adapter/claude/hooks.go`):

- No `PostToolUse` de escrita, se o arquivo bater com um padrão de **plano**,
  o daemon guarda que esta sessão escreveu um plano.
  - Padrão inicial: `docs/plan*.md`, ajustável em Configuração.
  - Specs ficam de fora (decisão do Claude): elas ainda não têm tarefas, e no
    fluxo atual o plano vem depois, muitas vezes em outra sessão.
- No `Stop`, se a sessão escreveu um plano e o card não tem plano registrado,
  aparece um lembrete no mesmo formato do lembrete de "editou N arquivos sem
  card": "você escreveu um plano; registre as tarefas com `nobre plan`".
- Se a sessão escreveu um plano e **não tem card**, o lembrete de "N arquivos
  sem card" ganha uma linha: "inclui um plano: crie o card com `nobre start` e
  registre com `nobre plan`". Não aparece um segundo lembrete separado.
- No `Stop`, se o card tem plano e houve commits desde a última mudança de
  tarefa, aparece um lembrete para atualizar com `nobre task`. Fica a critério
  de quem implementar como detectar o commit (comando `git commit` visto no
  `PostToolUse` ou mudança de `git_head`).
- Os lembretes seguem a regra dos lembretes de hoje: não podem prender o agente
  em loop (no máximo uma vez por condição).

O Cockpit **não lê** o conteúdo de `progress.md` nem interpreta o markdown do
plano. Quem informa as tarefas é o agente.

## Restrições

- Valem as restrições de `docs/plan.md` e as do napkin. Textos em pt-BR. Nunca
  tocar o `~/.claude` real, o `systemctl` nem o `nobred` instalado nos testes.
- Migrações: número seguinte ao último na `main` **no momento da
  implementação**. O NAC-4 reservou `009_card_flags.sql`. Confira os worktrees
  em andamento antes (`git worktree list`).
- Mudanças nos hooks valem para os dois caminhos de fim de sessão quando
  couber (hook e reconciliador; ver napkin).
- Datas no SQL são comparadas com `julianday(col)`.
- Cor do projeto: nunca a única informação. O nome aparece sempre. Não usar as
  cores reservadas dos providers (Claude #3987E5, Codex #D95926, Kiro #199E70)
  como cor de projeto.

## Entregas sugeridas

1. **Parte 1: seletor, etiquetas e página Projetos.** Usa só dados que já
   existem, mais o agrupamento de custo por projeto. Já entrega o foco e a
   comparação de custo e de cards parados.
2. **Parte 2: plano no card e cronograma.** Tabelas novas para o plano e as
   tarefas, os comandos `nobre plan` e `nobre task`, a skill, os lembretes do
   hook, a seção Plano do card, os blocos de progresso na página Projetos e o
   cronograma.

## Revisão esperando o humano (proposta do Claude, o usuário pediu a recomendação)

- Um card em **Revisão** há mais de 24 h **não** conta como "parado". Ele
  ganha um sinal próprio: "**aguardando revisão** há N dias".
- Motivo: "parado" quer dizer que o trabalho do agente travou, e a ação é
  retomar. "Aguardando revisão" quer dizer que a bola está com o usuário, e a
  ação é revisar. Se os dois se misturarem, o usuário não sabe o que fazer.
- Na página Projetos, ao lado de "Parados", entra a coluna "**Aguardando
  você**" (quantos cards e o mais antigo).
- No cronograma, a entrega fica no grupo "em andamento", com o sinal
  "aguardando revisão há N dias".

## Fica com quem implementar

- O esquema das tabelas do plano e das tarefas e o formato da API.
- O formato exato do parâmetro do projeto na URL e onde ele é guardado.
- A paleta das etiquetas de projeto (dentro das restrições acima).
- O formato exato dos comandos `nobre plan` e `nobre task` e das mensagens dos
  lembretes.
