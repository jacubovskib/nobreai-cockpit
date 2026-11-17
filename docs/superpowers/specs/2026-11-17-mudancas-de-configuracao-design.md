# Mudanças de configuração que afetam os agentes (NAC-6)

Data: 2026-11-17. Card: NAC-6.

## Por quê

Dois problemas fazem o Cockpit se comportar diferente do que o usuário espera, sem avisar ninguém:

1. **A integração com o Claude Code quebra por fora.** Outra ferramenta reescreve o `~/.claude/settings.json` e apaga os hooks, troca a status line, ou o binário `nobre` muda de lugar. O board para de registrar as sessões novas, e o usuário só percebe quando estranha o board vazio. Hoje a tela Configuração já detecta isso (checklist "Agentes detectados" com "Instalar o que falta"), mas só quando alguém abre a tela.
2. **A sessão aberta não pega a mudança de configuração.** Três opções são lidas só no início da sessão. Mudá-las com agentes rodando não tem efeito nas sessões abertas até elas acabarem: `agent_creates_cards` (o agente cria card por conta própria), `inject_paused_cards` (lista de pausados no início) e a captura do projeto (`projects.capture`, copiada para `sessions.full_capture` na criação da sessão). As outras opções (limites, regra de handoff, lembrete, "Continuar", carência) são lidas a cada evento e já valem na hora.

Nenhum dos dois aconteceu ao usuário ainda: é prevenção. A conversa de 2026-11-17 decidiu:

- **Integração:** o Cockpit avisa e só corrige quando o usuário confirma. Nada é escrito no `~/.claude` sem um clique do usuário.
- **Sessões abertas:** cada sessão guarda a configuração que recebeu. Quando uma das opções muda, a sessão recebe uma mensagem curta, uma única vez, no próximo passo dela.

## Objetivos

- O daemon confere a integração sozinho, ao subir e periodicamente, sem depender de evento de sessão. Se os hooks foram apagados, nenhuma sessão vai avisar.
- Problema encontrado vira alerta na Sala de controle, com a ação de correção num botão. A correção só roda no clique.
- Mudança em `agent_creates_cards` ou na captura do projeto chega às sessões abertas do agente principal no próximo hook que aceita contexto (`UserPromptSubmit` ou `PostToolUse`), uma vez.
- Mudar a captura do projeto passa a valer de verdade na sessão aberta: desligar para o registro detalhado na hora, ligar começa a registrar.
- Ao salvar, a tela diz quantas sessões abertas vão receber o aviso.
- Fica um histórico: o que mudou, quando, e o que foi feito (configuração alterada, integração quebrada, corrigida ou ignorada, sessão avisada).

## Fora do escopo e o que não fazer

- **Anti-objetivo:** escrever no `~/.claude/settings.json` ou em `~/.claude/skills` sem o usuário clicar. Isso inclui consertar "só os nossos hooks".
- **Anti-objetivo:** alerta que não some. Um problema ignorado só volta a alertar se o estado mudar de novo.
- **Anti-objetivo:** mensagem repetida ao agente. Cada mudança é dita uma vez por sessão; o histórico do agente não pode encher de avisos.
- Avisar o agente sobre opções que já valem na hora (limites, handoff, lembrete): o hook já aplica, não há o que dizer.
- `inject_paused_cards`: só importa no início da sessão; mudar no meio não gera aviso.
- Subagentes: o aviso vai só ao agente principal (`agent_id` vazio).
- Notificação do sistema operacional (desktop), Codex e Kiro, macOS.
- Descobrir qual ferramenta apagou os hooks.

## Restrições

- Linux, daemon local, SQLite. Sem dependência nova: nada de `fsnotify`; a conferência compara data e tamanho dos arquivos.
- Caminhos vêm de `claude.Paths` (injetável), para os testes nunca tocarem o `~/.claude` real.
- O hook do agente tem orçamento curto (o `nobre hook` desiste em 2 s). A comparação de configuração no hook é uma leitura de banco, sem git nem disco do `~/.claude`.
- O Claude Code carrega os hooks quando a sessão começa. Hooks apagados afetam as sessões **novas**; as abertas continuam mandando eventos. Pelo mesmo motivo, reinstalar não alcança uma sessão aberta enquanto os hooks estavam fora: ela só é registrada se for reiniciada.

## Abordagens consideradas

### Integração quebrada por fora

1. **Avisar e corrigir com confirmação (escolhida pelo usuário).** Alerta na Sala de controle com botão. É o mais seguro, e o usuário fica sabendo que outra ferramenta mexeu no arquivo dele. O custo: o board fica sem registrar até o usuário ver o alerta.
2. **Corrigir sozinho e avisar, com trava contra briga entre ferramentas.** Registra mais rápido, mas escreve no arquivo do usuário sem perguntar. O usuário recusou.
3. **Corrigir em silêncio.** Descartada: esconde que outra ferramenta está mexendo no arquivo.

### Como perceber a mudança

1. **Conferência periódica pela data e tamanho dos arquivos (escolhida, decisão minha).** Barata, sem dependência, e pega também o binário que mudou de lugar.
2. **inotify (`fsnotify`).** Instantâneo, mas é dependência nova, e o `settings.json` costuma ser trocado por rename, o que obriga a vigiar o diretório. A diferença de 30 s não importa aqui.
3. **Conferir só no início da sessão.** Descartada: com os hooks apagados, a sessão não avisa que começou.

### Avisar a sessão aberta

1. **Mensagem única no próximo hook que aceita contexto (escolhida pelo usuário).** `UserPromptSubmit` e `PostToolUse` aceitam `hookSpecificOutput.additionalContext`, que entra no contexto do agente.
2. **Só avisar na tela.** O agente continua seguindo a orientação antiga, que já está no contexto dele.
3. **Reler a opção em cada hook sem avisar.** Não resolve `agent_creates_cards`: a orientação é texto no contexto do agente, e só outra mensagem a substitui.

## Design

### Parte 1: saúde da integração

**Monitor no daemon.** Uma goroutine em `nobred` roda a conferência:

- Roda ao subir e a cada 30 s. Compara a data de modificação e o tamanho de `settings.json`, do `SKILL.md` do Cockpit e do binário `nobre` gravado nos hooks com os da última volta.
- Se algo mudou, ou a cada 5 min mesmo sem mudança, roda a conferência completa. Ela reaproveita a lógica de `Adapter.Detect()` (hooks, status line, skill, binário dos hooks) sem chamar `claude --version`; um `Detect` leve sem a versão é decisão do construtor.
- Guarda o resultado em memória, publica `"integration"` no stream quando o resultado muda e grava no histórico (abaixo) cada transição de OK para problema.

**Quando um item vira alerta:**

| Item | Alerta quando | Texto (exemplo) | Ação |
|---|---|---|---|
| Hooks | faltam eventos nossos | "Os hooks do Cockpit sumiram do ~/.claude/settings.json. As sessões novas do Claude Code não serão registradas." | **Reinstalar** (`POST /api/agents/claude/install`) · Ignorar |
| Binário dos hooks | o hook aponta para um `nobre` que não existe | "Os hooks apontam para um nobre que não existe mais (<caminho>)." | **Reinstalar** · Ignorar |
| Skill | sumiu ou está desatualizada | "A skill do Cockpit está desatualizada." | **Reinstalar** · Ignorar |
| Status line | **estava integrada e deixou de estar** | "A status line do Cockpit foi trocada por outro comando." | **Restaurar a do Cockpit** (`POST /api/agents/claude/statusline` install) · Manter a atual |
| CLI | sumiu do PATH | "O claude não foi encontrado no PATH." | só "Ver Configuração" (não há o que reinstalar) |

**Todo item só alerta na transição de OK para problema** (decisão minha, depois da revisão). O monitor grava em `settings` (chave `integration_baseline`) o último estado OK de cada item, o que sobrevive a reinícios do daemon. Assim:

- quem nunca integrou a status line não vê alerta;
- um daemon que nunca viu os hooks instalados não alerta: instalação nova, `make demo` ou `make dev`. Nesse caso o checklist da Configuração continua mostrando o que falta, como hoje.

O daemon de demonstração (`--seed-demo`) não roda o monitor, porque ele olharia o `~/.claude` real.

**Remoção feita de propósito não alerta** (decisão minha):

- **Botão da Configuração que remove a status line:** é uma ação do daemon. Ele atualiza o `integration_baseline` (o item deixa de ser "OK") e registra `integration_removed` no histórico.
- **CLI (`nobre setup --remover`, remover só a status line):** escreve direto nos arquivos. Por isso avisa o daemon antes, em melhor esforço: uma chamada à API que tira os itens do baseline. Se o daemon não responder, segue sem aviso.
- **`nobre setup --remover` completo:** passa a parar o serviço antes de mexer nos arquivos, para o monitor não ver a remoção.

**Skill e daemon de versões diferentes** (decisão minha). O `make setup` e o `nobre setup` gravam a skill nova sem reiniciar o daemon, e o daemon antigo veria a skill nova como "desatualizada". Regra:

- **Skill ausente:** alerta com "Reinstalar".
- **Skill diferente da embutida, com o binário `nobre` em disco mais novo que o início do daemon:** o alerta é "O Cockpit foi atualizado; reinicie o daemon (`make restart`)". Não tem botão de reinstalar, porque ele gravaria a skill velha por cima da nova.
- **Skill diferente da embutida, com o binário em disco mais antigo:** alerta com "Reinstalar".

**Binário `nobre` que mudou de lugar** (decisão minha). Hoje `Paths.NobreBin` é resolvido uma vez, quando o daemon sobe. O "Reinstalar" passa a resolver o caminho de novo antes de gravar: ao lado do `nobred`, depois no PATH do usuário (`launch.UserPATH`). Se o caminho não for achado, o alerta fica sem botão e diz "o nobre não foi encontrado; rode `make install`". Assim o botão nunca regrava o mesmo caminho morto, e a mensagem "removidos de novo" não dispara por engano.

**Alerta na Sala de controle.** Novo tipo `integration` em `overview.Alerts`, vindo do estado do monitor. O botão principal mostra antes o que vai mudar ("Vai recolocar os hooks SessionStart, … no ~/.claude/settings.json; o resto do arquivo não muda") e depois o relatório da instalação (`InstallReport.Changed`).

- Depois de reinstalar, o monitor confere na hora, e o alerta some se ficou OK.
- O texto pós-correção lembra: "Sessões abertas enquanto os hooks estavam fora não são registradas; reinicie-as."
- Se o mesmo item quebrar de novo em menos de 10 min depois de uma reinstalação, o texto passa a ser "Os hooks do Cockpit foram removidos de novo logo depois de reinstalados: outra ferramenta pode estar reescrevendo o ~/.claude/settings.json." A ação continua a mesma.

**Ignorar.** Grava em `settings` (chave `integration_ignored`) a impressão do problema: chave do item, detalhe, e data e tamanho do arquivo. O alerta some e só volta se a impressão mudar. Corrigir limpa a impressão daquele item.

**Configuração.** O checklist "Agentes detectados" passa a ler o mesmo estado do monitor (sem duplicar a lógica) e continua com "Instalar o que falta".

### Parte 2: aviso às sessões abertas

**O que cada sessão viu.** A sessão ganha a coluna `seen_config` (texto JSON), preenchida no `SessionStart` com os valores entregues no contexto: `{"agent_creates_cards": true, "capture": true}`. Duas coisas ficam separadas (decisão minha, depois da revisão):

- `sessions.full_capture` diz se o hook grava o detalhe.
- `seen_config.capture` diz o que o agente principal ouviu.

Assim um subagente pode parar de gravar na hora sem "gastar" o aviso que é do agente principal.

**Quem aplica a mudança de captura é a API, não o hook** (decisão minha). O hook continua sendo só uma leitura de banco, sem git:

- **`PATCH /api/projects/{id}` desligando a captura:** põe `full_capture = false` nas sessões vivas daquele projeto. A gravação de detalhe para na hora, inclusive nos hooks de subagente. O `project_id` fica: o custo continua contando no projeto e o vínculo com o card fica como está.
- **`PATCH` ligando a captura:** acha as sessões vivas daquela pasta com `projectFor(cwd)`, que roda git só aqui, na API, e também acha worktrees ligados fora da pasta. Nelas grava `project_id` e `full_capture = true`.

- Sessões criadas antes da migração ficam com `seen_config` vazio, tratado como "viu o padrão de agora" para não gerar aviso falso.
- Sessão criada sem `SessionStart` (evento perdido) grava `seen_config` no primeiro hook, com os valores atuais, sem aviso.

**Conferência no hook.** Em `UserPromptSubmit` e `PostToolUse` do agente principal (`agent_id` vazio):

1. **Captura** (`sess.FullCapture` comparado com `seen_config.capture`, com a mudança já aplicada pela API):
   - **Desligada:** contexto "NobreAI Cockpit: a captura deste projeto foi desligada. Não use mais os comandos nobre nesta sessão." A sessão e o uso continuam registrados.
   - **Ligada:** o contexto é o mesmo texto que o `SessionStart` daria a uma sessão registrada (`instructionsFor(set)`), precedido de "NobreAI Cockpit: a captura deste projeto foi ligada."
2. **`agent_creates_cards`**, só com a sessão capturando e o valor diferente de `seen_config`. Contexto: "NobreAI Cockpit: a configuração mudou." mais a orientação nova (`guidanceFor(set)`). Desligada, isso inclui "Só crie card quando o usuário pedir"; ligada, volta o critério de criação.
3. Grava `seen_config` **antes** de responder, para o aviso sair uma vez só. Se a resposta se perder (timeout do `nobre hook`), o aviso não se repete: é aceito.

Se os dois mudaram, vai uma mensagem só. Captura desligada dispensa a parte de `agent_creates_cards`.

Hoje o `PostToolUse` só responde em sessão com captura; a conferência roda antes desse filtro.

**Ao salvar.** `PUT /api/settings`, quando `agent_creates_cards` muda, devolve `notified_sessions`: as sessões vivas do agente principal com captura cujo `seen_config` difere do valor novo. O `PATCH /api/projects/{id}` faz o mesmo quando `capture` muda, contando as sessões vivas daquele projeto (ou daquela pasta, no caso de ligar). A tela mostra, ao lado do "Salvo": "3 sessões abertas vão receber o aviso no próximo passo." Com zero sessões, nada aparece.

### Histórico

Tabela nova `config_log` (`id`, `at`, `kind`, `data` JSON). A tabela `events` exige sessão, e boa parte destes registros não tem. Tipos:

- `setting_changed` (chave, de, para);
- `project_capture_changed` (projeto, de, para);
- `integration_broken` (item, detalhe);
- `integration_fixed` (item, `InstallReport.Changed`);
- `integration_ignored` (item);
- `integration_removed` (item, origem: Configuração ou CLI);
- `agent_notified` (sessão, o que mudou).

A Configuração ganha a seção "Histórico de mudanças", com as últimas 50 linhas em ordem decrescente, em frases ("14:32 · Hooks do Cockpit removidos do settings.json", "14:35 · Hooks reinstalados por você"). A Sala de controle mostra só os alertas, não o histórico.

## O que fica para o construtor

- Como separar o `Detect` leve (sem `--version`) do completo, e onde mora o estado do monitor (pacote próprio ou dentro do adaptador).
- Nome exato das rotas, do tipo do alerta e das chaves em `settings`, e o formato da impressão de "Ignorar".
- Retenção do `config_log` (sugestão: manter os últimos 500).
- Textos finais das mensagens, desde que digam o que mudou e o que o agente ou usuário deve fazer.
- Testes: monitor com `Paths` em diretório temporário (apagar hooks, mover binário, trocar status line, skill velha), transição e Ignorar; hooks com sessão viva para cada caso da Parte 2, incluindo aviso único e subagente sem aviso; contagem de `notified_sessions`.
