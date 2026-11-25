# Tipos e hierarquia de cards: épico, história, tarefa e bug (NAC-55)

Data: 2026-11-25. Card: NAC-55. Status: rascunho para aprovação.

## Por quê

O usuário vai usar o Cockpit num computador da empresa onde o Jira é a
ferramenta oficial. Ele quer controlar o trabalho no Cockpit e mandar para o
Jira só o essencial (NAC-54). Hoje o card não tem tipo nem hierarquia, e o
de:para com o Jira precisa disso: no Jira o trabalho chega como épico,
história, tarefa ou bug.

O usuário pediu, nas palavras dele, para **"trazer o modelo do Jira para a
plataforma"**, seguindo como o mercado faz gestão de projeto e produto de TI,
mas pensando num produto **"AI first e native"**. Ele concordou com cinco
inversões em relação ao Jira (seção "Inversões AI-first"). Esta peça traz a
**estrutura** do Jira, não a configurabilidade dele, e vale por si mesma, mesmo
sem Jira.

Esta é a peça 1 da ordem combinada:

1. Tipos e hierarquia no board (este documento).
2. Jira, fatia 1: conector, ligação card↔issue, de:para com templates, envio
   de status e comentário de fechamento (NAC-54).
3. Skill de configuração assistida da integração.
4. Leitura de issues novas e agente de triagem.
5. Fluxo B: criar a issue no Jira a partir do card.

## Inversões AI-first (aprovadas pelo usuário)

1. **O status nasce da atividade, não do humano.** Os hooks já movem o card. A
   coluna do épico também é calculada, a partir das filhas.
2. **O gargalo é a atenção humana.** O board ganha o filtro "Esperando você".
3. **Medir o real em vez de estimar.** O épico mostra o custo real somado das
   filhas. Story points existem, mas são opcionais.
4. **A quebra do trabalho é proposta pelo agente e aprovada pelo humano.** Nesta
   peça, isso significa que o agente sabe criar filhas de um épico. A proposta
   automática fica para a triagem (peça 4).
5. **"Pronto para começar" é pronto para um agente.** Fica para a triagem e o
   NAC-53. Nesta peça não muda nada.

## Objetivos

1. O card ganha um **tipo**: `epico`, `historia`, `tarefa` ou `bug`.
2. O card ganha um **pai**. Só um épico pode ser pai, e o épico não tem pai. As
   filhas podem estar em **projetos diferentes** do épico (o Jira permite; caso
   do épico que pega front e back).
3. **A coluna do épico é calculada pelas filhas.** Não depende de sessão nem de
   alguém arrastar.
4. O épico mostra **progresso** (filhas concluídas / total) e o **custo real**
   (o dele mais o das filhas) no lugar dos indicadores de "pronto para handoff".
5. O card ganha **prioridade** (os cinco níveis do Jira), **labels** e
   **estimativa em story points** (opcional).
6. O board ganha **filtro por épico, por tipo e "Esperando você"**, e mostra o
   tipo e o épico em cada card.
7. O agente cria e classifica cards com tipo e épico pelo `nobre`, e o
   `nobre resume` de uma filha traz o contexto do épico.

## Fora do escopo

- Qualquer coisa do Jira (NAC-54 e peças seguintes).
- Sprint, backlog priorizado como tela, roadmap, relatórios, velocidade e
  burndown (NAC-53).
- Tipos customizados, campos customizados e fluxo de colunas configurável por
  projeto. As colunas continuam as cinco de hoje.
- Subtarefa como tipo de card. A subtarefa é a **tarefa do plano** que já existe
  (`nobre plan` / `nobre task`). A ida dela para o Jira como sub-task é assunto
  do de:para.
- Mais níveis acima do épico (Initiative etc.).
- Vínculos genéricos entre cards ("relacionado", "duplica"). O `blocked_by`
  fica como está.
- Levar os campos novos para o servidor do time (NAC-35). Quando o NAC-35
  sincronizar cards, ele passa a incluir tipo, pai, prioridade, labels e
  estimativa. Esta peça não mexe no branch `feat/cockpit-produto`.
- Proposta automática de quebra de épico em histórias (peça 4).

## Anti-objetivos

- **A coluna do épico mentir:** épico parado em Backlog enquanto as filhas
  andam, ou "Concluído" com filha aberta.
- **Quem não usa tipos sentir diferença:** os cards existentes viram `tarefa`,
  prioridade `media`, sem pai, e o board, os hooks e o `nobre` se comportam
  exatamente como hoje para eles.
- **O hook ficar mais lento:** o hook continua lendo só o banco local, e o
  recálculo do épico acontece na mesma transação que move a filha, sem consulta
  extra no caminho do hook além da filha → épico.
- **O agente criar épicos ou mudar o tipo de cards sem ter sido pedido.** A
  skill ensina a usar tipo e épico quando o usuário falar em épico/história ou
  quando o trabalho for claramente parte de um épico existente.

## Restrições

- Go no backend, React no frontend, SQLite local. Textos de UI, mensagens e
  docs em pt-BR.
- A migração local mantém todos os cards, sessões e custos. A última migração
  no `main` é a `022`. Esta peça usa a próxima livre na hora do merge.
- As chaves `NAC-n` são únicas só no banco local. Para o pai, usar o `id`
  (chave estrangeira), não a chave em texto como faz o `blocked_by`.
- Uma sessão continua ligada a um card só. O custo de uma mensagem continua
  indo para o card ligado à sessão no momento (`usage.card_id`). O custo do
  épico é soma, não realocação.

## Abordagens consideradas

1. **O épico é um card com tipo `epico` (escolhida pelo usuário).**
   - **Ganha:** o épico tem tudo que um card tem: objetivo, critério de pronto,
     decisões, perguntas, `nobre resume` para uma sessão de planejamento. No
     Jira o épico também é uma issue, então o de:para fica um para um. A
     triagem (peça 4) tem onde propor a quebra.
   - **Custa:** regras especiais para o épico: coluna calculada, sem
     indicadores de handoff, sem movimento por sessão.
2. **Épico numa tabela própria, sem ser card.**
   - **Ganha:** menos regras especiais.
   - **Custa:** o épico perde decisões, perguntas e sessões de planejamento; o
     mapeamento com o Jira fica assimétrico. Descartada.
3. **Tabela genérica de vínculos (pai, bloqueia, relacionado), como os links do
   Jira, migrando o `blocked_by`.**
   - **Ganha:** flexibilidade.
   - **Custa:** aumenta o escopo e mexe num campo que funciona. Pode vir depois,
     por cima da abordagem 1. Descartada para agora.

## Design

### Modelo de dados

Migração nova na tabela `cards`:

| coluna | tipo | padrão | observação |
|---|---|---|---|
| `type` | TEXT NOT NULL | `'tarefa'` | `epico`, `historia`, `tarefa`, `bug` |
| `parent_id` | INTEGER NULL | NULL | `REFERENCES cards(id)`; índice |
| `priority` | TEXT NOT NULL | `'media'` | `mais_alta`, `alta`, `media`, `baixa`, `mais_baixa` |
| `points` | REAL NULL | NULL | story points; NULL = sem estimativa |

Tabela nova `card_labels(card_id INTEGER NOT NULL REFERENCES cards(id), label
TEXT NOT NULL, UNIQUE(card_id, label))`, com `workspace_id`/`owner_id` como as
outras tabelas.

`domain.Card` ganha `Type`, `ParentID`, `Priority`, `Points` e `Labels`, com
constantes e validação no `internal/domain` (como `Column`).

**Regras de validação** (no domínio, usadas pela API web e pela do agente):

- `type` precisa ser um dos quatro; `priority`, um dos cinco.
- `parent_id`, quando presente, aponta para um card existente de tipo `epico`,
  diferente do próprio card.
- Um card `epico` não pode ter pai.
- Não dá para mudar um card para `epico` se ele tem pai, nem tirar o tipo
  `epico` de um card que tem filhas. A mensagem diz o que fazer ("remova as 3
  filhas do épico antes" / "tire o card do épico antes").
- `points` ≥ 0, aceita meio ponto (0.5).
- **Projeto do épico:** como todo card, o épico pertence a um projeto. Ele pode
  ficar num **grupo sem pasta** (o caso de front e back como sub-projetos de um
  mesmo grupo), exceção feita só para épicos na regra que hoje recusa card em
  grupo sem pasta. Um épico assim não aceita sessão (não há pasta onde abrir o
  agente); o planejamento acontece pela web ou numa sessão num dos
  sub-projetos. Quando os repositórios não estão num grupo, o épico fica num
  deles e as filhas podem estar em qualquer projeto. **(escolha do Claude.)**
- Label: sem espaços (como no Jira), sem vazio, até 50 caracteres, guardada
  como digitada; duplicada no mesmo card é ignorada.

### A coluna do épico

O épico nunca é movido por sessão. A coluna dele é recalculada sempre que uma
filha muda de coluna, entra no épico ou sai dele:

A regra é uma função pura (filhas + coluna atual do épico → coluna nova), lida
de cima para baixo; a primeira linha que vale decide:

| situação | coluna do épico |
|---|---|
| o épico está Concluído e todas as filhas estão Concluídas (ou não há filhas) | Concluído (fica) |
| nenhuma filha | Backlog |
| todas as filhas em Backlog | Backlog |
| todas as filhas Concluídas | Revisão |
| qualquer outro caso | Em andamento |

- **Revisão no lugar de Concluído** quando as filhas terminam: o humano decide
  se o épico acabou ou se faltam histórias. É o que alimenta o "Esperando você".
  Aprovar move para Concluído (o `HumanApproved` de hoje).
- Concluído só "gruda" enquanto todas as filhas estão concluídas. Se uma filha
  reabre ou entra uma filha nova, a tabela vale de novo: um épico Concluído que
  ganha a primeira filha em Backlog volta para Backlog; com filhas concluídas e
  uma nova em Backlog, vai para Em andamento.
- Quando a última filha sai, o épico volta para Backlog (a não ser que esteja
  Concluído).
- O épico **nunca vai para Pausado**: pausa é estado de sessão. `nobre card add
  --type epico --column pausado` é recusado com mensagem.
- Mover o épico à mão: só é aceito → Concluído (com todas as filhas concluídas
  ou sem filhas) e Concluído → reabrir (recalcula pela tabela, ignorando a
  primeira linha). Qualquer outro movimento manual devolve 409 com o motivo ("a
  coluna do épico segue as filhas").
- **Virar épico:** só é aceito para um card sem sessão ao vivo e sem pai. Ao
  virar épico, o card perde `pause_reason`, `handoff_incomplete`, `blocked_by` e
  `branch`, e a coluna é recalculada pela tabela. **(escolha do Claude: evita
  épico preso em Pausado ou Em andamento.)**
- **Sessão ligada a um épico** (sessão de planejamento com `nobre resume` ou
  `nobre start` num épico) é permitida e registra custo, notas e decisões no
  épico, mas não move a coluna dele. O fim dessa sessão também não move o épico
  nem marca handoff incompleto. **(escolhas do Claude, abaixo, evitam que o
  épico seja concluído sem o humano:)**
  - o épico não guarda branch: `syncBranch` ignora épicos, e eles ficam fora de
    `ListIntegrationCandidates`, então merge de branch nunca conclui um épico;
  - `nobre done` num épico não muda a coluna: registra o que veio (como testar,
    onde parei) e responde "a coluna do épico segue as filhas";
  - os caminhos de limite de custo pausam a sessão, mas não movem o épico.
- O recálculo é uma função só do store, chamada na mesma transação de todo
  caminho que muda a coluna, o tipo ou o pai de um card: `moveCardTx`,
  `UpdateCard`, `MoveCard`, `RecordIntegration`, os caminhos de limite
  (`PauseForLimit`, `ResumeFromLimit`, `DoneFromLimit`) e o reconciliador. Quando
  a filha troca de épico, os dois épicos são recalculados.
- A mudança de coluna do épico gera o mesmo evento SSE que qualquer mudança de
  card, para o board atualizar sozinho.

### Custo e progresso do épico

- **Progresso:** `{filhas: N, concluidas: M}` no `CardSummary` e no `CardDetail`
  do épico.
- **Custo:** o `CardSummary` do épico traz o total dele (sessões de
  planejamento), o total das filhas e a soma. A soma reaproveita o mapa de
  totais por card que `listCards` já calcula. Na página do épico aparecem os
  três números.
- **Prontidão:** o épico não mostra os indicadores de "x/y para handoff". No
  lugar entra a barra de progresso.

### Esperando você

Filtro do board (e contagem no botão) com os cards que dependem de uma pessoa.
Um card entra quando qualquer um vale:

- tem pergunta aberta (`nobre ask` sem resposta), em qualquer coluna menos
  Concluído e mesmo com sessão ao vivo. **(escolha do Claude: o sinal
  `aguardando_voce` de hoje só vale em Pausado, e aí a pergunta de uma sessão de
  planejamento num épico nunca apareceria. O sinal em si não muda; o filtro
  conta as perguntas direto.)**;
- sinal `aguardando_revisao`;
- está em Revisão (inclui o épico com todas as filhas concluídas);
- `handoff_incomplete`;
- pausado por limite de custo;
- sinal `pronto_para_retomar` (o card que bloqueava terminou).

O cálculo fica no backend, num campo `waiting_on_you` (bool) com o motivo
(`waiting_reason`) no `CardSummary`, para a Sala de controle e o servidor do
time poderem usar depois.

### Board e página do card

```text
Board › app-pagamentos   [Épico: todos ▾] [Tipo: todos ▾] [Esperando você · 3]
┌ Em andamento ─────────────────┐  ┌ Revisão ──────────────────────┐
│ ◆ NAC-60 · Épico              │  │ ▲ NAC-63 · Bug  ⬆ alta        │
│   Checkout novo               │  │   Pix duplica cobrança        │
│   ▓▓▓▓░░░ 4/7 · R$ 31,20      │  │   ◆ Checkout novo             │
├───────────────────────────────┤  │   esperando você: revisão     │
│ ■ NAC-61 · História           │  └───────────────────────────────┘
│   Pagar com Pix               │
│   ◆ Checkout novo · 3 pts     │
│   plano 3/5 · R$ 4,10  ●●●○   │
└───────────────────────────────┘
```

- Cada card mostra o ícone do tipo, a prioridade quando não é `media`, os
  points quando existem, o chip do épico (link) nas filhas e as labels.
- O card de épico mostra a barra de progresso e o custo somado.
- Filtros novos: épico (inclui "sem épico"), tipo e "Esperando você". Eles se
  somam aos de projeto e agente que já existem. O filtro de épico mostra as
  filhas de todos os projetos quando o board está em "todos os projetos"; num
  projeto, mostra só as filhas daquele projeto e o épico, se for dele ou do grupo acima dele.
- **Página do card:** editores de tipo, épico (seletor dos épicos abertos de
  qualquer projeto), prioridade, labels e points. Na página do épico, a lista
  das filhas com coluna, projeto, agente da última sessão e custo, e o
  botão "Nova filha".
- **Nova tarefa** (`NewTask.tsx`) ganha tipo e épico, com padrão `tarefa` e sem
  épico. Prioridade, labels e points ficam na página do card.
- A lista de cards (`CardsList.tsx`) ganha coluna de tipo e filtro por tipo.

### API

- `CardSummary` e `CardDetail` ganham `type`, `parent_key`, `parent_title`,
  `priority`, `labels`, `points`, `waiting_on_you`, `waiting_reason` e, para
  épicos, `children` (progresso) e os totais de custo descritos acima.
- `POST /api/cards` e `PATCH /api/cards/{key}` aceitam `type`, `parent_key`
  (string vazia remove o pai), `priority`, `labels` (lista completa) e `points`
  (null remove).
- `GET /api/cards/{key}` de um épico traz as filhas.
- API do agente (`/api/me/start`, `/api/me/cards`) aceita `type`, `epic`
  (chave), `priority`, `labels` e `points`. Um endpoint novo altera esses campos
  no card da sessão ou num card indicado pela chave.

### Comando `nobre` e skill

- `nobre start` e `nobre card add` ganham `--type`, `--epic <KEY>`,
  `--priority`, `--label` (repetível) e `--points`.
- Comando novo: `nobre card set [<KEY>] [--type] [--epic <KEY> | --no-epic]
  [--priority] [--label ...] [--points]`. Sem `<KEY>`, age no card da sessão;
  com `<KEY>`, em qualquer card, para o agente poder pôr cards existentes sob um
  épico. **(escolha do Claude: um comando só em vez de um por campo.)**
- `nobre card add` continua criando no projeto da sessão. Criar uma filha em
  outro repositório se faz pela web ("Nova filha" escolhe o projeto) ou numa
  sessão naquele repositório. **(escolha do Claude: evita que o agente crie
  cards em repositórios que não está vendo.)**
- `nobre status` mostra tipo, prioridade, épico e, para épico, o progresso.
- A skill do Claude (`internal/adapter/claude/skill.md`), o steering do Kiro
  (`internal/adapter/kiro/steering.md`) e o resumo de comandos injetado no
  início da sessão (`commandsGuidance`) ensinam: quando usar tipo e épico, como
  criar as filhas de um épico (`nobre card add --epic NAC-60 --type historia`)
  e que um épico não se trabalha como uma tarefa (a sessão no épico é de
  planejamento).

### Contexto para o agente

- No pacote do `nobre resume` (`internal/resume/package.go`), a linha de meta
  ganha tipo e prioridade. Uma filha ganha uma seção curta logo após o
  cabeçalho: `Épico: NAC-60 · Checkout novo`, com o objetivo do épico em uma
  linha.
- O resume de um épico ganha a seção "Filhas" com chave, tipo, título e coluna.
- A lista de "Cards pausados neste repositório" no início da sessão não muda,
  a não ser por mostrar o tipo quando não for `tarefa`.
- O limite de 9500 caracteres do contexto injetado continua valendo; a seção do
  épico é curta e a lista de filhas é cortada com "… e mais N".

## Critérios de pronto

1. Migração aplicada numa cópia do banco real sem perder cards, sessões ou
   custo; todos os cards existentes ficam `tarefa`/`media`/sem pai.
2. Criar um épico, criar filhas em dois projetos, iniciar sessão numa filha: o
   épico vai para Em andamento sozinho; concluir todas as filhas leva o épico a
   Revisão; aprovar leva a Concluído; reabrir uma filha o devolve a Em
   andamento.
3. O épico mostra progresso e custo somado no board e na página.
4. "Esperando você" lista exatamente os casos da regra.
5. `nobre start --type historia --epic NAC-x` e `nobre card set` funcionam; o
   `nobre resume` da filha mostra o épico.
6. As regras de validação devolvem mensagens claras na web e no `nobre`.
7. Suíte Go e web verdes; testes novos para o recálculo do épico em todos os
   caminhos que movem coluna, incluindo os dois fins de sessão (hook
   `sessionEnd` e `board.Reconcile`).

## O que fica para quem constrói

- Nome exato do endpoint do agente para alterar os campos e o formato da
  saída do `nobre card set`.
- Ícones e cores dos tipos e prioridades, seguindo o visual atual do board.
- Se o recálculo do épico é chamado explicitamente em cada caminho ou a partir
  de um ponto central no store, desde que todos os caminhos listados passem
  por ele e haja teste para cada um.
- Ordenação dentro das colunas (hoje é por atualização); prioridade pode entrar
  como critério secundário se for barato.
