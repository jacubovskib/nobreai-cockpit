---
name: nobre-cockpit
description: Use quando a tarefa tiver várias etapas, mudar vários arquivos ou puder ser continuada por outro agente, quando o usuário pedir um card (ou "crie um card para isso"), ou quando citar uma chave NAC- (por exemplo NAC-12). Ensina a registrar e retomar o trabalho no NobreAI Cockpit com o comando nobre.
---

# NobreAI Cockpit

O Cockpit é o quadro (kanban) onde o trabalho dos agentes fica registrado em cards. Toda sessão já é registrada; o card guarda o histórico que permite outro agente continuar sem perguntar nada.

## Quando criar um card

- Crie quando começar algo que precisa de histórico: tarefa de várias etapas, mudanças em vários arquivos ou trabalho que outro agente pode continuar.
- Crie quando o usuário pedir um card.
- Se o usuário citar uma chave `NAC-`, não crie outro: retome com `nobre resume NAC-12`.
- Perguntas rápidas e ajustes de uma linha não viram card.
- Antes de criar, veja se o contexto da sessão lista cards pausados deste repositório e branch; retome em vez de duplicar.

## Comandos

- `nobre start "Login com OAuth" --goal "entrar com Google" --done-when "testes passam"`: cria o card e liga esta sessão.
- `nobre resume NAC-12`: liga a sessão ao card e imprime o pacote de retomada (use `--force` se outra sessão estiver viva).
- `nobre note "callback pronto; falta validar o state"`: atualiza "Onde parei".
- `nobre decide "PKCE em vez de segredo no cliente" --why "o app é público"`: registra uma decisão.
- `nobre file auth/oauth.go --why "fluxo do callback" --lines L12-96`: marca um arquivo essencial (`--lines` é opcional).
- `nobre todo add "cobrir o erro de state inválido"`: adiciona uma pendência; `nobre todo done 3` conclui a pendência 3.
- `nobre ask "Usar cookie ou localStorage para o token?"`: registra uma pergunta que precisa do usuário.
- `nobre test "go test ./auth/..."`: registra como testar.
- `nobre done`: marca o card como pronto para revisão.
- `nobre status`: mostra o card, "Onde parei" e o que falta para o handoff.

## Como manter o card útil

- Atualize "Onde parei" com `nobre note` a cada passo relevante e antes de parar. Escreva o essencial: o que está feito, o que vem agora e qualquer armadilha.
- Marque com `nobre file` os arquivos essenciais, sempre com o motivo.
- Registre decisões com o porquê, e perguntas em aberto com `nobre ask`.
- Antes de `nobre done`, confira `nobre status`: objetivo, como testar, arquivo essencial e pendências.
- Não pergunte ao usuário o que já está no card: leia o pacote de retomada e continue a partir dele.
