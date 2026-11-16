# NobreAI Cockpit — Kanban para agentes de IA (design)

Data: 2026-11-05 · Status: rascunho para aprovação

> Nome do produto: **NobreAI Cockpit** (decidido pelo usuário). CLI `nobre`, daemon `nobred`, prefixo de card `NAC-` — escolha do Claude (evita conflito com o Cockpit da Red Hat no Linux).

## 1. Intenção e porquê

Hoje, quando uma tarefa troca de agente (ex.: Claude → Codex), o contexto fica preso nos logs de sessão
da ferramenta anterior. O agente novo precisa "garimpar" as últimas sessões do outro para descobrir o que
estava sendo feito — decisões, arquivos tocados, o que falta, onde parou. Tudo isso se perde ou custa caro.

O NobreAI Cockpit é um kanban feito **para agentes**, acessado por **CLI e API**, em que cada tarefa é um **card**
que é a fonte da verdade do trabalho. O card nasce quando o usuário pede ou quando o agente identifica
que está começando algo que precisa de histórico, e é enriquecido continuamente, de forma que **qualquer outro agente, de qualquer provider,
continue exatamente de onde o anterior parou**. Um humano usa uma interface web para acompanhar,
verificar e configurar.

## 2. Critérios de aceitação (palavras do usuário → teste)

O produto falhou — mesmo "funcionando" — se:

1. **O agente novo ainda precisar perguntar o que estava sendo feito.**
   Teste: ao retomar um card, o agente começa a trabalhar sem fazer perguntas sobre o estado da tarefa.
2. **Eu não encontrar tokens gastos por sessão e valor monetário.**
   Teste: para qualquer sessão, card, agente e provider, a UI mostra tokens (entrada/saída/cache) e custo em dinheiro.
3. **O kanban não funcionar corretamente.**
   Teste: a coluna de um card sempre reflete o estado real. Card "Em andamento" sem nenhuma sessão viva é bug.

## 3. Objetivos

- **Card como fonte da verdade**, criado sob pedido ou por julgamento do agente e enriquecido automaticamente.
- **Captura garantida por hooks**: mesmo que o agente "esqueça", os hooks registram o esqueleto.
- **Multi-provider**: Claude (Claude Code), Codex, Kiro na v1; arquitetura de adaptadores para adicionar outros.
- **Handoff por dois caminhos**:
  - UI: clicar em "Continuar com <agente>" no card → o daemon inicia o agente com o contexto carregado.
  - Terminal: `nobre resume NAC-42` (ou passar o ID para o agente) → ele puxa tudo sozinho.
- **Visibilidade de uso e custo**: tokens e valor monetário por sessão, card, agente e provider.
- **Limites de custo por agente** (e por card — escolha do Claude).
- **Setup automático**: localizar agentes instalados, instalar hooks e skills/instruções necessárias, mostrar checklist de saúde.

## 4. Não-objetivos (v1)

- Times / multiusuário / comunicação entre agentes de pessoas diferentes. **Fica para depois**, mas o
  modelo de dados nasce com `workspace` e `owner` para não exigir migração dolorosa.
- Hospedagem em nuvem. v1 é local.
- Substituir o próprio agente (o Cockpit orquestra e registra; não é um agente).

## 5. Anti-objetivos

- Card que é só um "resumo feito no fim" — ele precisa ser vivo e atualizado durante o trabalho.
- Depender da boa vontade do agente para registrar o essencial.
- Status do board que mente sobre o estado real.

## 6. O card

### 6.0 Quando um card nasce (decidido pelo usuário)

- **Quando o usuário pede**: "crie um card para isso", `nobre start`, ou um ID com `nobre resume <ID>`.
- **Quando o agente identifica** que está começando algo que precisa de histórico: tarefa de várias
  etapas, mudanças em vários arquivos, ou algo que pode ser continuado por outro agente. A skill instalada
  ensina esse critério. Perguntas rápidas não viram card.

Escolhas do Claude para manter a garantia de "mesmo se o agente esquecer":
- **Toda sessão é registrada**, com ou sem card ("sessão sem card"), então tokens e custo nunca se perdem.
- **No início da sessão**, o hook injeta os cards pausados do mesmo repositório e branch, para que o agente
  retome em vez de duplicar.
- **Se o agente esquecer**, o hook avisa quando uma sessão sem card edita 3 ou mais arquivos (limite
  configurável). A Sala de controle mostra as sessões sem card, com "Criar card desta sessão".
- Uma sessão pode ser ligada a um card depois de começar. O histórico já capturado entra no card.

### 6.1 Duas fontes de informação

| Fonte | O que registra | Como |
|---|---|---|
| **Hooks (fatos, automático)** | início/fim de sessão, provider/modelo, arquivos lidos e editados, comandos executados, tokens, custo, último prompt do usuário | hooks de cada agente → evento para o daemon |
| **Agente (julgamento)** | objetivo, decisões + porquê, arquivos essenciais + por que cada um importa, feito, falta, "onde parei", bloqueios | CLI/API (`nobre note`, `nobre decide`, `nobre file`, `nobre next`…) via skill/instrução instalada |

### 6.2 Conteúdo mínimo

- ID, título, projeto/repositório, branch, coluna
- Objetivo (o que é "pronto")
- **Onde parei** (texto curto, sempre o mais recente no topo)
- Decisões (decisão, motivo, quem/qual sessão, quando)
- Arquivos essenciais (caminho, motivo, linhas relevantes opcionais) — distintos de "arquivos tocados" (automático)
- Feito / Falta (checklist)
- Sessões (timeline: agente, provider, modelo, duração, tokens, custo, resumo)
- Comandos relevantes / como testar
- Bloqueios e perguntas abertas

### 6.3 "Pronto para handoff"

Regra configurável que define o mínimo para um card ser retomável (ex.: "Onde parei" atualizado há
menos de N minutos, objetivo preenchido, ≥1 arquivo essencial, "Falta" não vazio). A UI mostra se o
card está pronto e o que falta. Quando uma sessão termina sem cumprir a regra, o hook de fim de
sessão pede ao agente que complete (onde o provider permitir) e, senão, marca o card como "handoff incompleto".

A ação é escolhida em Configuração: `ask_agent` bloqueia o fim do turno uma vez para o agente completar o
"Onde parei"; `mark` grava a marca de handoff incompleto no card quando a sessão termina com o card em
Pausado, sem outra sessão viva e fora da regra (hook de fim de sessão e reconciliador seguem o mesmo
caminho). A Sala de controle só alerta enquanto a opção for `mark`; trocar para `ask_agent` esconde o
alerta, mas a marca continua guardada. A marca some quando o card volta para Em andamento.

### 6.3.1 Onde o agente abre no handoff pela UI (decidido pelo usuário)

Configurável **globalmente** (Configuração › Pronto para handoff) e **sobrescrevível a cada clique** em
"Continuar com…" no card. Modos:
- nova janela do terminal;
- sessão tmux (`nobre-<ID>`);
- segundo plano (modo headless do agente), com a saída transmitida para a interface.

Os três modos fazem parte do escopo. Escolha do Claude: o padrão é a nova janela do terminal, e a v1 entrega
primeiro terminal e tmux, com o modo segundo plano por último no plano. O agente abre no branch do card.

### 6.4 Pacote de retomada

`nobre resume <ID>` gera um pacote de contexto (markdown) com tudo acima, mais o diff/estado do git
desde a última sessão, entregue ao agente novo como prompt inicial/contexto.

## 7. Kanban

Colunas (escolha do Claude): **Backlog → Em andamento → Pausado/Handoff → Revisão → Concluído**.

- Movimentos automáticos pelos eventos de sessão: sessão inicia → Em andamento; sessão termina sem
  conclusão → Pausado/Handoff; agente declara pronto → Revisão; humano aprova → Concluído.
- O humano pode mover manualmente; o sistema reconcilia (ex.: heartbeat ausente → sai de "Em andamento").
- Um card pode ter, no máximo, uma sessão "dona" ativa por vez (evitar dois agentes no mesmo card sem querer).

## 8. Custos e limites

- Tokens (entrada, saída, cache) e custo em dinheiro por sessão, agregados por card/agente/provider/dia.
- Tabela de preços por modelo, editável (preços mudam).
- Limites por agente (diário/mensal) e por card. Escolha do Claude: aviso em 80%, pausa em 100%, configurável.
- Moeda de exibição configurável (BRL/USD) — escolha do Claude.
- **Pausa por limite**: com a ação "pausar", o hook bloqueia as ferramentas (exceto `nobre`), e o card vai
  para Pausado com o motivo "limite de custo". Quando o limite é elevado, o card volta para Em andamento na
  próxima ferramenta permitida. Continuar com outro agente automaticamente e limites mensais ficam para depois.

## 9. Setup automático

- Detecta CLIs instalados (Claude Code, Codex, Kiro), versão e caminho.
- Instala/verifica: hooks, skill/instruções (ex.: CLAUDE.md/AGENTS.md/steering) que ensinam o agente a
  usar o `nobre`, e servidor MCP do Cockpit quando o provider suportar.
- Checklist de saúde por agente: CLI encontrado · hooks instalados · skill instalada · último evento recebido.

## 9.1 Prompts salvos (pedido do usuário)

Interpretação do Claude (a confirmar): duas coisas.

- **Biblioteca de prompts reutilizáveis**, com nome, atalho (slug), tag, agentes compatíveis e escopo
  (global ou por projeto). O texto aceita variáveis do card, preenchidas na hora de usar: `{{card.id}}`,
  `{{card.titulo}}`, `{{card.onde_parei}}`, `{{card.pacote}}`, `{{card.arquivos}}`, `{{card.como_testar}}`,
  `{{git.branch}}`, `{{handoff.faltando}}`. Variável desconhecida aparece como erro na prévia.
- **Salvar a partir de uma sessão**: os hooks já capturam os prompts que o usuário digita. A tela lista
  esses prompts e permite promover qualquer um para a biblioteca.
- **Onde se usa**:
  - no handoff, com o campo "Prompt inicial" do card ("Retomar card" é o padrão, configurável);
  - pelo CLI: `nobre prompt use <slug> --card <ID>`;
  - pelo hook de fim de sessão: o prompt "Fechar sessão com handoff" é enviado quando a regra de
    "pronto para handoff" não foi cumprida.
- Métricas simples: número de usos e data do último uso por prompt.

## 10. Arquitetura (decisões)

Decididas pelo usuário:
- **Backend, daemon e CLI em Go.**
- **Frontend em React.**

Escolhas do Claude:
- **Daemon local** (`nobred`) que: recebe eventos dos hooks, expõe API HTTP (REST + stream de eventos para
  a UI), serve a UI em `localhost`, e inicia agentes no handoff pela UI.
- **CLI** (`nobre`) fala com o daemon pela API; os hooks também chamam o CLI.
- **SQLite** como armazenamento local.
- **Adaptador por provider** (interface Go): detectar, instalar hooks/skills, iniciar sessão com contexto,
  traduzir eventos de hook e extrair uso de tokens.
- Modelo de dados com `workspace_id` e `owner_id` desde o início (preparação para times).
- Opcional: servidor MCP do Cockpit para agentes que preferem ferramentas MCP a CLI.

## 11. A verificar antes do plano

- **Capacidades de hooks de Codex e Kiro** (quais eventos existem, se dá para capturar arquivos editados e
  tokens). Claude Code tem hooks (SessionStart, PostToolUse, Stop etc.); Codex e Kiro precisam ser confirmados.
  Onde não houver hook, fallback: wrapper (`nobre run codex …`) e/ou leitura dos logs de sessão locais.
- Como cada provider expõe uso de tokens (evento de hook, log local, ou API).

## 12. Interface web (design)

Telas, desenhadas no Claude Design: https://claude.ai/artifact/VZs84mPU4RS1rWCPQg2K8L

1. **Sala de controle**: agentes ativos agora e em qual card; custo do dia; alertas (limite 80%, card
   "em andamento" sem sessão viva, handoff incompleto).
2. **Board**: kanban com as colunas acima; card mostra provider, indicador ao vivo, custo, prontidão de handoff.
3. **Detalhe do card**: o documento de handoff completo + "Continuar com…" + comando `nobre resume`.
4. **Analytics**: tokens e custo por sessão, card, agente, provider, ao longo do tempo.
5. **Configuração**: agentes detectados + checklist de saúde, limites de custo, regra de "pronto para handoff", projetos.
6. **Prompts**: biblioteca com editor, variáveis e prévia preenchida com um card real; lista de prompts capturados nas sessões com "Salvar como prompt".
7. **Projetos**: tabela comparativa (custo no período, entrega atual, parados, aguardando você, última
   atividade) e, por projeto, o cronograma das entregas (concluídas, em andamento, paradas, backlog).
   O seletor de projeto da barra lateral (`?projeto=<id>`) filtra Board, Cards, Analytics, Worktrees e
   Prompts; a Sala de controle e os Terminais só mostram a etiqueta do projeto.

Visual (escolha do Claude): tema escuro "grafite operacional", Geist + Geist Mono; cor reservada para
dados. Providers têm cor fixa por identidade (Claude #3987E5, Codex #D95926, Kiro #199E70, validadas para
daltonismo no fundo escuro), sempre acompanhada do nome. Status usam ícone + texto, nunca só cor.

## 12.1 Decisões do Claude após a revisão do builder

- **Sessão viva** = processo do agente ainda rodando (PID observado pelo daemon), mesmo que ocioso no
  prompt. O `Stop` do Claude Code marca fim de *turno*, não de sessão. A sessão termina quando o processo
  sai (ou com `SessionEnd`, quando existir). Card em "Em andamento" cujo processo morreu → Pausado/Handoff
  após 15 min de carência (configurável).
- **Captura por projeto**: os hooks são instalados globalmente, mas o daemon só cria cards para sessões
  dentro de repositórios registrados em Configuração › Projetos. Fora deles, ignora.
- **Fallback por provider**: quando faltar hook, vale usar o wrapper `nobre run <agente>` e/ou ler os logs
  de sessão locais. Se não der para obter tokens exatos, mostrar "estimado" ou "indisponível". O provider
  continua na v1, e a Configuração mostra o que falta (ex.: Kiro "Parcial").
- **Custo em planos de assinatura**: o padrão é mostrar o equivalente em preço de API, com opção de mostrar
  só tokens.
- **Ação ao atingir 100%**: configurável por agente (pausar sessão e mover para Handoff / só bloquear novas
  sessões / só avisar). Em providers sem hook que bloqueie, "pausar" cai para "bloquear novas sessões".
- **SO da v1**: Linux primeiro (máquina do usuário); macOS em seguida. Windows fora da v1.

## 13. Deixado para o builder

Formato exato dos eventos, esquema SQL, layout de pastas do repo, estratégia de testes, empacotamento/instalação.
