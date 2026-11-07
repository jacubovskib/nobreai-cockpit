# Checklist do piloto: NobreAI Cockpit com o Claude Code real

Roteiro manual para validar, nesta máquina, os três critérios de aceite do
piloto (seção 2 de `docs/spec.md`). O teste automático
(`go test ./internal/e2e/`) cobre a lógica com um agente simulado; este roteiro
cobre o que só o Claude Code de verdade mostra.

Antes de começar: use um repositório Git descartável ou de baixo risco, com a
árvore limpa, e o Claude Code já autenticado.

## 1. Instalar

1. Na raiz do projeto: `make build`. Isso gera `./nobre` e `./nobred`. Em
   seguida coloque-os no PATH desta sessão do shell:
   `export PATH="$PWD:$PATH"`. Os comandos abaixo usam `nobre` e `nobred`
   sem caminho.
2. Veja o que o setup faria, sem gravar nada: `nobre setup --dry-run`.
   Confira os arquivos citados (`~/.claude/settings.json`, a skill
   `nobre-cockpit` e a unidade `nobred.service`).
3. Aplique: `nobre setup`. Ele grava um backup `settings.json.bak-<data>` ao
   lado do `settings.json`, instala os hooks e a skill, e liga o `nobred` como
   serviço de usuário.
4. Confirme: `systemctl --user status nobred` (ativo) e `nobre ui` (abre a
   interface em http://127.0.0.1:7420). Em **Configuração**, na seção
   **Agentes detectados**, o Claude Code deve aparecer como detectado e com os
   hooks instalados (se faltar algo, use **Instalar o que falta**).
5. Em **Configuração**, na seção **Preços e moeda**, cadastre o preço do modelo
   que você usa (**Adicionar modelo**, preencha entrada, saída, escrita e
   leitura de cache, em US$ por milhão de tokens, e clique em **Salvar**).
   Sem preço o custo aparece como "sem preço", nunca como zero.

## 2. Registrar um repositório

1. `cd` no repositório de teste e rode `nobre project add` (ou use o
   `nobre setup`, que oferece cadastrar o repositório atual). Também dá para
   usar **Configuração > Projetos > Adicionar repositório**.
2. `nobre project list` deve mostrar o projeto.

## 3. Sessão A: o card nasce

1. No repositório, abra `claude`.
2. Peça uma tarefa de várias etapas, por exemplo: "Adicione um endpoint de
   health com teste e documente no README".
3. Observe no board (sem recarregar):
   - a sessão aparece em **Sala de controle**, em "Agentes rodando agora";
   - o Claude cria um card (`nobre start`) e ele surge na coluna **Em andamento** do
     **Board**, com o agente Claude ao lado;
   - ao longo do trabalho o card ganha "Onde parei", decisões, arquivos
     essenciais, pendências e "Como testar".
4. Se o Claude não criar o card sozinho, peça: "crie um card para isso".
5. Encerre a sessão (`/exit` ou Ctrl+D). O card deve ir para **Pausado /
   Handoff**, com a prontidão do handoff completa. Confirme que os tokens e o
   custo da sessão apareceram no card.

## 4. Sessão B: continuar pela UI

1. No **Board**, abra o card. No painel **Continuar com outro agente**,
   deixe o agente Claude Code, o prompt inicial padrão ("Retomar card
   (padrão)") e "Abrir em" no padrão, e clique em **Iniciar Claude Code neste
   card**. A interface mostra "Terminal aberto.".
2. Uma nova janela de terminal abre no diretório do projeto, já com o Claude.
   (Se o repositório tiver mudanças não salvas ou estiver em outro branch, o
   Cockpit recusa e explica; faça commit ou stash.)
3. Sem digitar nada, observe o Claude partir do prompt "Você está continuando
o card NAC-N ..." e do pacote de retomada.
4. O card volta para **Em andamento** com 2 sessões.

## 5. Confirmar os três critérios

**Critério 1: o agente continua sem perguntar o que já foi dito.**
Na sessão B, o Claude deve citar o "Onde parei", respeitar as decisões já
tomadas, abrir os arquivos essenciais e retomar as pendências, sem pedir que
você repita o contexto. Anote qualquer pergunta que ele tenha feito sobre algo
que já estava no card.

**Critério 2: tokens e US$ por sessão.**
No card, cada sessão (A e B) mostra tokens de entrada, saída e cache e o custo
em US$. O total do card é a soma das duas. Compare com o que o próprio Claude
Code mostra (`/cost`): os valores devem ser próximos, e a diferença deve ser
explicável (tokens ainda não lidos do transcript).

**Critério 3: colunas corretas, inclusive com o processo morto.**
1. Com a sessão B viva, o card está em **Em andamento**.
2. Encerre B normalmente: o card vai para **Pausado**.
3. Continue de novo (seção 4) e agora mate o processo sem encerrar: em outro
   terminal, `pkill -9 -f claude` (cuidado: isso derruba todas as suas sessões
   do Claude) ou `kill -9 <pid>` do processo certo.
4. Em até cerca de 30 s o card deixa de contar como sessão viva, mas ainda
   fica em **Em andamento** dentro da carência, mostrando "processo encerrado
   há N min" no Board, na Sala de controle e na página do card. Espere a
   carência de 15 min: o card sai de **Em andamento** e vai para
   **Pausado**.
5. Com o card sem sessão viva, tente arrastá-lo no **Board** para **Em andamento** (ou
   `curl` com PATCH na API): deve ser recusado, porque Em andamento significa
   "há uma sessão viva".

## Desfazer

1. Desfaça os hooks. Há duas situações:
   - Existe `~/.claude/settings.json.bak-<data>`: o `nobre setup` só cria
     backup quando o `settings.json` já existia. Copie o mais antigo (o do
     seu primeiro `nobre setup`) de volta para `~/.claude/settings.json`.
     Atenção: isso descarta qualquer edição que você fez no `settings.json`
     depois do setup. Se editou, prefira remover à mão só as entradas de hook
     que chamam `nobre hook` (eventos SessionStart, UserPromptSubmit,
     PreToolUse, PostToolUse, Stop e SessionEnd).
   - Não existe backup: o `settings.json` foi criado pelo setup. Abra o
     arquivo e remova as entradas de hook do `nobre hook`; se não sobrar nada
     seu, pode apagar o arquivo.
2. Pare e desative o daemon: `systemctl --user disable --now nobred`.
   Opcional: remova `~/.config/systemd/user/nobred.service` e rode
   `systemctl --user daemon-reload`.
3. Remova a skill: `rm -r ~/.claude/skills/nobre-cockpit`.
4. Os dados ficam em `~/.local/share/nobreai-cockpit/cockpit.db`; apague a
   pasta se quiser recomeçar do zero.

## Limites conhecidos

- Só Linux: o daemon lê `/proc` para saber se o processo do agente vive.
- O botão "Continuar" monta um comando para o shell de login POSIX (bash, zsh,
  dash). Shells como fish ou csh não são suportados no piloto.
- Só o Claude Code tem adaptador. Codex e Kiro chegam no plano 2.
- Risco de segurança: o daemon escuta só em `127.0.0.1` e recusa outros
  `Host` e conteúdo que não seja JSON, mas qualquer processo do mesmo usuário
  do sistema pode chamar a API local (e, por ela, criar cards, ler o conteúdo e
  abrir terminais). Não rode o piloto em máquina compartilhada.
- O custo só é calculado para modelos com preço cadastrado.
