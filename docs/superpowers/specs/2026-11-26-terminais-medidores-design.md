# Terminais: medidores e "Limpar e continuar" (NAC-63, entrega 2 de 3)

Data: 2026-11-26. Card: NAC-63. Mock aprovado (v4): https://claude.ai/artifact/RsgZfBpZQjhPz6kSDJhuJM
Entrega 1 (grade): `docs/superpowers/specs/2026-11-26-terminais-grade-design.md`, no main em a25aff2.

## Intenção

Na grade o usuário acompanha vários agentes lado a lado, mas não vê quanto do contexto cada um já usou nem quanto do plano já gastou; para isso precisa ir à Sala de Controle ou à página do card. Quando um agente chega perto do limite, a saída é "recomeçar limpo", que hoje só existe na página do card. Esta entrega põe os dois números na tela Terminais e deixa limpar o contexto e continuar dali mesmo.

## Objetivos

- Cada tile mostra o contexto da sessão que roda nele, com o visual do mock aprovado.
- O cabeçalho da tela mostra o plano, uma vez (decisão do usuário: o plano é da conta e vale igual para todos os terminais do agente).
- "Limpar e continuar" no tile, sempre com confirmação, com o agente novo aparecendo na grade.
- Nada disso piora a performance medida na entrega 1.

## Fora desta entrega

- Barra de entrada, `#` e `Ctrl+K` (entrega 3).
- Total de créditos do Kiro e percentual de plano do Kiro: não existe fonte. Mostra-se só o gasto de hoje (decisão minha).
- Qualquer medidor para o Codex: não há adaptador nem dado (decisão minha).
- "Limpar e continuar" automático ao bater o limite (decisão minha: só com confirmação).
- "Limpar e continuar" para sessão sem card: o recomeço parte do "Onde parei" do card.
- Mudar o medidor de contexto da página do card e da Sala de Controle (`ContextMeter`).

## O que seria um fracasso

- O tile mostrar o contexto ou o card de outra sessão.
- A grade engasgar: um hook de qualquer sessão redesenhar todos os tiles.
- "Limpar e continuar" abrir o agente novo fora da grade, ou encerrar a sessão antiga sem o usuário pedir.
- Mostrar um percentual de plano vencido como se fosse atual.

## Como funciona hoje (o que esta entrega toca)

- **Terminal e sessão não são ligados no servidor.** `GET /api/terminals` devolve `pid` e `card_key`; a sessão tem `pid` e o card. O único casamento existe no web, do lado inverso: `terminalFor` em `web/src/pages/ControlRoom.tsx:290-298` (mesmo pid, porque o PTY roda o próprio agente).
- **`card_key` do terminal não é confiável como card da sessão.** `nobre run` só o grava quando há chave de retomada (`internal/api/terminals.go:123-127`): uma sessão que fez `nobre start` depois aparece como "sem card" no tile. E depois de um recomeço limpo o terminal antigo mantém o `card_key` com a sessão já desligada do card.
- **Contexto:** `GET /api/context/live` (`internal/api/context_live.go:28-76`) lista as sessões vivas com captura ligada no projeto: `id`, `card_key`, `tokens`, `window_tokens`, `pct`, `pct_only`, `over80`, `rewrite`, `idle_over_ttl`. Kiro vem `pct_only` (só o percentual, janela 0). O web consulta a cada 30 s e de novo a cada evento `sessions` do stream, que sai a cada hook (`web/src/api/useStream.ts:10`).
- **Faixas de aviso:** `settings.context_warn_pcts` (padrão 60 e 80) só é usado no servidor, para o aviso ao agente. O `ContextMeter` do web usa 80 fixo.
- **Plano:** `GET /api/plan-limits` devolve as janelas de 5 horas e 7 dias (`used_pct`, `resets_at`, `received_at`, `expired`) e `warn_pct`. Vem só da status line do Claude Code. Hoje aparece só na Sala de Controle (`web/src/pages/PlanLimits.tsx`). Do Kiro existe o gasto de créditos de hoje em `GET /api/overview` (`AgentSpend.credits`).
- **Recomeçar limpo:** `POST /api/sessions/{id}/restart-clean {where_stopped}` (`internal/api/restart_clean.go`). Exige sessão viva e com card, e um "Onde parei" enviado ou com menos de 30 min. Desliga o card da sessão (que segue viva, sem card; o card em andamento vira pausado) e abre um agente novo pelo caminho do Continuar, no modo padrão das configurações. Só devolve `terminal_id` no modo `background`; no modo `terminal` o agente abre no emulador de terminal do usuário, fora da plataforma. O diálogo é `web/src/pages/context/RestartCleanDialog.tsx`, usado só na página do card.

## Abordagens consideradas

- **A. Casar terminal e sessão no web** (escolhida). Um módulo novo inverte o `terminalFor`: para cada terminal, a sessão viva de mesmo pid. A lista de sessões já é invalidada pelo evento `sessions`, então a ligação aparece assim que o primeiro hook da sessão chega. Sem mudança de esquema.
- **B. O servidor devolver `session_id` em `GET /api/terminals`.** Uma fonte só, mas a lista de terminais só é republicada quando um terminal muda; a sessão nasce depois (no primeiro hook) e o campo ficaria velho até outro evento, ou o servidor passaria a publicar `terminals` a cada sessão nova. Mais acoplamento para o mesmo resultado.
- **C. Gravar `terminal_id` na sessão.** Exige migração e que o hook saiba em que terminal está. Caro para o que esta entrega precisa.

## Desenho

### Ligação terminal → sessão

- Módulo novo no web, usado pela grade e pela Sala de Controle (o `terminalFor` de lá passa a usar o mesmo casamento por pid; o comportamento da Sala de Controle não muda).
- Regra: a sessão do terminal é a sessão **viva** (sem `ended_at` e sem `pid_dead_at`) com `pid` igual ao do terminal. Havendo mais de uma (um `/clear` cria sessão nova no mesmo processo; uma conversa retomada pode reabrir uma sessão antiga, que mantém o `started_at` de antes), vale a de `last_event_at` mais recente, depois a de `started_at` mais recente, depois a última da lista; `last_event_at` ausente ou vazio vale o `started_at`. É a mesma ordem com que o daemon decide a sessão atual do processo (`SessionByPID`).
- **Sem o atalho por `card_key`** que o `terminalFor` tem: na grade ele mostraria no tile antigo o contexto da sessão nova.
- Terminal encerrado, ou sem sessão casada: sem medidor e sem "Limpar e continuar".
- **O card do tile passa a ser o da sessão casada**, quando houver; sem sessão casada, vale o `card_key` do terminal, como hoje. Isso corrige o "sem card" de quem fez `nobre start` depois de abrir o terminal. As abas de projeto seguem a mesma regra (o projeto do card da sessão).

### Contexto no tile (como no mock)

- **Limite:** a última faixa de `context_warn_pcts` (padrão 80), a mesma que a tela de Configuração já edita. **Crítico:** 90, fixo. Cores: verde abaixo do limite, âmbar a partir do limite, vermelho a partir de 90 (ou do limite, se ele for maior que 90). O card "Contexto no limite" do mock, com o seletor 70/80/90 e a caixa "Oferecer limpar e continuar", não entra: o limite já tem lugar na Configuração e a oferta fica sempre ligada (decisão minha).
- **Detalhado:** uma faixa entre o cabeçalho do tile e o terminal: `Contexto 62%` em negrito, `· 124k de 200k`, e à direita o link `Limpar`. Embaixo, barra de 6 px com um traço vertical no limite (`title`: "Limite configurado 80%"). Sessão `pct_only` (Kiro) mostra `Contexto 62% · o agente informa só a %`, sem tokens.
- **Simples:** sem faixa; um chip `ctx 62%` no cabeçalho do tile, antes do estado (`title`: "Contexto usado"), nas mesmas cores.
- **No limite, nos dois modos:** uma linha de aviso sob o cabeçalho (âmbar; vermelha a partir do crítico): "Contexto em 84%, acima do limite de 80%. Limpe e continue de onde parou." e o botão `Limpar e continuar`. No Simples o texto é "Contexto em 84%. Limpe e continue de onde parou.". A linha só aparece quando a ação está disponível (ver abaixo) e some enquanto o painel de recomeço está aberto.
- **"~N turnos até o limite"** do mock fica fora: o resumo de `GET /api/context/live` não traz esse número, e buscar o detalhe por sessão custaria uma consulta por tile (decisão minha).
- **Sem dado** (projeto sem captura, sessão sem turno medido, sem sessão casada, terminal encerrado): nem chip nem faixa. O mock mostra a faixa em tile encerrado; aqui não há sessão viva de onde ler (decisão minha).
- Tile em pausa também mostra chip e faixa: o dado vem da API, não do xterm.
- É um componente novo da grade; o `ContextMeter` das outras telas fica como está.

### Plano no cabeçalho da tela (como no mock)

Uma faixa sob o cabeçalho da página, acima das abas de projeto.

- **Detalhado:** um cartão por agente com dado.
  - `Claude` (o nome do plano não vem da status line; o "Claude Max" do mock vira só "Claude"): `Sessão de 5 h` 62% · "reinicia em 2 h 14 min (17:30)"; `Semana` 41% · "reinicia em 2 d 3 h (sáb 09:00)". Barras: âmbar a partir de `warn_pct`, vermelho a partir de 90.
  - `Kiro`: "12,3 créditos hoje". Os "Créditos do ciclo" com percentual do mock não entram: não há fonte para o total. Só aparece com terminal Kiro na grade e gasto maior que zero.
  - Codex: nada (o mock mostra um cartão explicando; sem adaptador do Codex ele não teria quando aparecer).
- **Simples:** chips `Claude 5 h 62% · sem 41%` e `Kiro 12,3 créditos hoje`, nas mesmas cores.
- Janela `expired`: "sem leitura recente" no lugar do percentual, nunca o número velho. Sem dado algum do Claude, o cartão e o chip do Claude não aparecem. Sem nenhum cartão, a faixa não ocupa espaço.
- Reusa `usePlanLimits` e `useOverview`; nenhuma consulta nova.

### Limpar e continuar

- **Onde:** o link `Limpar` da faixa (Detalhado), o botão `Limpar e continuar` da linha de aviso (os dois modos) e um item "Limpar e continuar" no menu ⋯ (para o Simples abaixo do limite; decisão minha).
- **Quando está disponível:** terminal vivo, sessão casada viva e com card. Com sessão casada mas sem card, o item do menu aparece desabilitado com "precisa de um card", e link e linha de aviso não aparecem. Sem sessão casada, nada aparece.
- **Confirmação, como no mock:** um painel dentro do tile, sob o cabeçalho e por cima do terminal (não um modal), com rolagem própria se o tile for baixo. Título "Recomeçar limpo"; texto "A sessão atual segue viva, mas sem o card. O agente novo começa do “Onde parei” abaixo, com o contexto zerado."; campo "Onde parei" editável, preenchido com o do card da sessão; o aviso de registro velho ou vazio que o diálogo de hoje já dá; botões `Limpar e continuar` e `Cancelar`. Nunca roda sem o painel. A lógica (envio, validação do registro, erros) é a do `RestartCleanDialog`, compartilhada; a página do card não muda.
- **O agente novo abre na plataforma:** a grade pede o recomeço no modo `background`, qualquer que seja o padrão das configurações. `POST /api/sessions/{id}/restart-clean` passa a aceitar `mode` no corpo, repassado ao Continuar; sem o campo, nada muda (a página do card segue no modo padrão).
- **Diferença do mock:** lá o mesmo tile continua com a sessão nova. Aqui o recomeço abre um processo novo, logo um **terminal novo**: ele entra no início da grade (regra da entrega 1), recebe o foco e a tela rola até ele.
- **O terminal antigo fica:** a sessão antiga segue viva e sem card, e o tile dela passa a "sem card", com o painel trocado por "Contexto limpo. O agente novo abriu em outro terminal." e o botão `Encerrar este terminal`, que pede a confirmação de encerrar já existente. Nada é encerrado sozinho.
- **Erros:** os do endpoint aparecem no painel (sessão já terminou, falta "Onde parei", agente indisponível, limite de 20 terminais).

### Performance (parte do contrato)

- O evento `sessions` chega a cada hook de qualquer sessão. A página casa terminais e sessões e entrega a cada tile só valores primitivos (percentual arredondado, tokens, janela, nível da faixa, chave do card, id da sessão): um tile cujos valores não mudaram não redesenha.
- Os medidores são HTML simples; nada toca o xterm nem o WebSocket.
- A bancada da entrega 1 roda de novo antes do merge, pelos botões, e precisa dar "aprovado" nos dois perfis.

## Erros e casos de borda

- Sessão casada some (processo morreu): o medidor sai do tile na próxima atualização.
- `/clear` no agente: a sessão nova do mesmo pid assume o tile; o percentual cai.
- Dois terminais do mesmo card (antigo e novo depois de limpar): cada um mostra a sua sessão; só o novo tem o card.
- Percentual acima de 100 (janela estimada errada): a barra trava em 100% e o número aparece como veio.

## Testes

- Unidade: casamento terminal → sessão (pid igual, sessão morta ignorada, duas sessões no mesmo pid, sem atalho por card); limite e nível (verde, âmbar, vermelho) a partir de `context_warn_pcts`, inclusive limite acima de 90 e lista vazia; textos da faixa de plano (normal, acima do aviso, vencida, sem dado, Kiro, tempo até reiniciar).
- Componente: chip, faixa, barra e traço do limite nos dois modos, `pct_only`, sem dado, linha de aviso no limite e no crítico; link, botão e item do menu habilitados, desabilitado e ausentes; painel abre com o "Onde parei" do card da sessão; sucesso foca o terminal novo e oferece encerrar o antigo; a página do card segue com o diálogo de hoje.
- Página: um evento `sessions` que não muda os valores de um tile não o redesenha; o card do tile vem da sessão.
- Daemon (Go): `restart-clean` com `mode: "background"` abre na plataforma e devolve `terminal_id` mesmo com padrão `terminal`; sem `mode`, comportamento de hoje.
- Sala de Controle: os testes de "Ver terminal" seguem passando.
- Bancada: rodada manual registrada no card antes do merge.

## Deixado para quem implementa

- Nome e lugar do módulo de casamento e dos componentes novos.
- Formato exato dos números (`124 mil`, `1,2 M`), reaproveitando `context/format.ts` onde servir.
- Como a lógica do `RestartCleanDialog` é compartilhada com o painel do tile (hook extraído ou outra forma), desde que a página do card não mude.
