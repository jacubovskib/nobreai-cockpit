---
inclusion: always
---

# NobreAI Cockpit

O Cockpit é o quadro (kanban) onde o trabalho dos agentes fica registrado em cards. O card guarda o histórico que permite outro agente continuar sem perguntar nada.

## Antes de tudo: esta sessão é registrada?

- Use os comandos `nobre` só quando o contexto desta sessão contiver a frase "NobreAI Cockpit: esta sessão é registrada no board" (repositório cadastrado), a frase "NobreAI Cockpit: este repositório não está cadastrado no board" (veja "Repositório não cadastrado") ou a frase "NobreAI Cockpit: esta pasta não está cadastrada no board" (veja "Pasta não cadastrada").
- Sem nenhuma dessas frases, não rode comandos `nobre`.
- Se um comando `nobre` responder que o repositório não está cadastrado, siga "Repositório não cadastrado".

## Repositório não cadastrado

Quando o contexto trouxer a frase "NobreAI Cockpit: este repositório não está cadastrado no board", cadastre sem perguntar ao usuário, antes de seguir com o pedido dele:

1. `nobre project add <raiz>`: use a raiz que a frase indica. Dentro de um worktree, cadastra o checkout principal. Imprime o branch base e as sessões religadas; esta sessão passa a ser registrada.
2. `nobre project scan`: levanta o branch base, os branches com trabalho fora da base (`(recente)` = últimos 14 dias), os planos com tarefas abertas, as sessões recentes com custo e os cards que já existem.
3. Escolha até 5 cards: entregas com branch recente (14 dias) ou worktree ativo, e planos com tarefas abertas. Agrupe branches da mesma entrega num só card. Não duplique cards que já existem. Ignore o que está parado há mais de 30 dias.
4. Crie cada um com `nobre card add "<título>" --goal "<objetivo>" --done-when "<critério>" --branch <branch>`. Informe `--branch` sempre que houver branch. Se o trabalho já começou (branch com commits), acrescente `--column pausado --where "<onde parei>"`; o resto fica no Backlog (padrão). O comando não liga esta sessão ao card.
5. Se o `nobre project add` ou o `nobre project scan` imprimir "Sub-projetos sugeridos", liste-os ao usuário (até 5; o resto fica na Configuração → Projetos) e não os cadastre: quem decide é ele. Para parar de sugerir uma pasta, use `nobre project scan --nao-sugerir apps/web`.
6. Diga ao usuário, em poucas linhas, o que cadastrou e quais cards criou, e siga com o pedido dele.

Se o usuário não quiser o repositório no board, ele desliga a captura do projeto na Configuração. Se o `nobre project add` falhar, avise o usuário e siga sem card.

## Pasta não cadastrada

Quando o contexto trouxer a frase "NobreAI Cockpit: esta pasta não está cadastrada no board", a sessão está numa pasta que não é git e tem repositórios dentro. Pergunte ao usuário se quer cadastrá-la como grupo; só rode `nobre project add <pasta>` (a pasta que a frase indica) se ele aceitar. Depois, liste ao usuário os sub-projetos sugeridos que o comando imprimir, sem cadastrá-los. Se ele recusar, siga com o pedido sem comandos `nobre`.

## Quando criar um card

- Crie quando começar algo que precisa de histórico: tarefa de várias etapas, mudanças em vários arquivos ou trabalho que outro agente pode continuar.
- Crie quando o usuário pedir um card.
- Se o usuário citar uma chave `NAC-`, não crie outro: retome com `nobre resume NAC-12`.
- Perguntas rápidas e ajustes de uma linha não viram card.
- Antes de criar, veja se o contexto da sessão lista cards pausados deste repositório e branch; retome em vez de duplicar.
- Se o contexto da sessão disser "Só crie card quando o usuário pedir", só crie card quando o usuário pedir (vale também para os cards do levantamento: liste-os ao usuário).

## Comandos

- `nobre start "Login com OAuth" --goal "entrar com Google" --done-when "testes passam"`: cria o card e liga esta sessão.
- `nobre resume NAC-12`: liga a sessão ao card e imprime o pacote de retomada (use `--force` se outra sessão estiver viva).
- `nobre note "callback pronto; falta validar o state"`: atualiza "Onde parei".
- `nobre decide "PKCE em vez de segredo no cliente" --why "o app é público"`: registra uma decisão.
- `nobre file auth/oauth.go --why "fluxo do callback" --lines L12-96`: marca um arquivo essencial (`--lines` é opcional).
- `nobre todo add "cobrir o erro de state inválido"`: adiciona uma pendência; `nobre todo done 3` conclui a pendência 3.
- `nobre plan docs/plan-oauth.md --task "callback" --task "refresh token"`: liga o plano ao card e registra as tarefas em ordem; rode de novo quando o plano mudar (as tarefas de mesmo título mantêm o estado).
- `nobre task 2 andamento`: muda o estado da tarefa 2 (`a-fazer`, `andamento`, `revisao`, `concluida`).
- `nobre ask "Usar cookie ou localStorage para o token?"`: registra uma pergunta que precisa do usuário.
- `nobre answer 4 "cookie httpOnly"`: responde a pergunta 4 do card (os ids aparecem em `nobre ask`, `nobre status` e no pacote de retomada como `- [4] <texto>`).
- `nobre block NAC-4 --why "precisa do merge do NAC-4"`: marca que este card só continua depois do NAC-4 (o board mostra "bloqueado por NAC-4"); `nobre block --clear` remove.
- `nobre branch feat/oauth`: informa o branch de trabalho do card; rode ao criar ou entrar no branch ou worktree do card. Recusa o branch base do projeto.
- `nobre app web`: põe o card no app (sub-projeto) certo do monorepo; `nobre app --geral` devolve ao projeto principal.
- `nobre test "go test ./auth/..."`: registra como testar.
- `nobre done`: marca o card como pronto para revisão. Recusa se "Como testar" estiver vazio ou mais antigo que a última edição de arquivo, ou se houver pergunta sem resposta. `nobre done --force` ignora essas recusas; use só depois de registrar o motivo com `nobre note`.
- `nobre card add "Migrar sessões" --goal "..." --done-when "..." --branch feat/sessoes --where "parei na migration 3" --column pausado`: cria um card sem ligar esta sessão (`--column` aceita `backlog` ou `pausado`).
- `nobre project scan`: levantamento só de leitura do projeto (inclui os sub-projetos sugeridos; `--nao-sugerir rel,rel` recusa); `nobre project base main`: define o branch base (`nobre project base <branch> [path]`).
- `nobre project add <pasta>`: numa pasta que não é git e tem repositórios dentro, cadastra um grupo com pasta e lista os repositórios como sub-projetos sugeridos. Pergunte ao usuário antes de cadastrar a pasta; depois, mostre a lista e só cadastre um deles (`nobre project add <repo> --pai <id do grupo>`) se ele pedir.
- `nobre status`: mostra o card, "Onde parei" e o que falta para o handoff.

## Épicos e tipos

- Use `--type historia|bug|tarefa|epico` e `--epic <KEY>` (em `nobre start`, `nobre card add` ou depois com `nobre card set [<KEY>] ...`) quando o usuário falar em épico ou história, ou quando o trabalho for parte de um épico existente. `--no-epic` solta o card do épico; `--priority` e `--points` também valem. `--label` define a lista completa de labels do card (repita a flag para várias; ao mudar labels de um card existente, inclua as que ficam) e `--no-labels` limpa todas.
- Para quebrar um épico, crie uma filha por entrega: `nobre card add "<título>" --epic NAC-60 --type historia`.
- A sessão num épico é de planejamento: não marque o épico como done; a coluna dele segue as filhas.
- Nunca crie épicos nem mude o tipo de um card sem o usuário pedir.

## Como manter o card útil

- Atualize "Onde parei" com `nobre note` a cada passo relevante e antes de parar. Escreva o essencial: o que está feito, o que vem agora e qualquer armadilha.
- Ao terminar de escrever um plano de implementação, registre as tarefas com `nobre plan` (a spec não é plano). A cada tarefa iniciada, mandada para revisão ou concluída, rode `nobre task`. A tarefa é um passo do plano; `nobre todo` é uma pendência solta, fora do plano.
- Marque com `nobre file` os arquivos essenciais, sempre com o motivo.
- Registre decisões com o porquê, e perguntas em aberto com `nobre ask`; assim que uma pergunta for decidida, responda com `nobre answer <id> "<resposta>"`.
- Se o card só puder continuar depois de outro, rode `nobre block <KEY> --why "<motivo>"` antes de parar; o board passa a mostrar "bloqueado por <KEY>" e, quando o outro concluir, "pronto para retomar".
- Rode `nobre branch <nome>` ao criar ou entrar no branch ou worktree do card, para o card acompanhar o trabalho.
- Quando o card for de um app só, rode `nobre app <nome>` ao criar o card ou ao começar a mexer nele; `nobre app --geral` devolve ao projeto principal (o contexto da sessão lista os apps do projeto).
- Atualize "Como testar" (`nobre test`) depois da última mudança de código e antes de `nobre done`; um "Como testar" mais antigo que a última edição bloqueia o `done`.
- Antes de `nobre done`, confira `nobre status`: objetivo, como testar, arquivo essencial e pendências.
- Não pergunte ao usuário o que já está no card: leia o pacote de retomada e continue a partir dele.
