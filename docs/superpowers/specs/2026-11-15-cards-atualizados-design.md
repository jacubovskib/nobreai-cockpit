# Cards que se mantêm atualizados e cadastro de projetos pelo agente (NAC-9)

Data: 2026-11-15. Card: NAC-9.

## Por quê

O próximo agente só trabalha bem se o card que ele lê estiver atual. Hoje há três fontes de contexto velho e uma lacuna:

1. **O card não acompanha o git.** NAC-1..3 estão em Revisão desde 7/11, embora tenham entrado na `main` nesse dia. O branch do card fica congelado na criação: NAC-4 e NAC-5 marcam `main`, mas o NAC-5 foi feito em `feat/visao-projeto`.
2. **Perguntas e "Como testar" envelhecem.** O agente consegue abrir pergunta (`nobre ask`), mas não consegue respondê-la. O pacote de retomada do NAC-5 trouxe duas perguntas que já estavam decididas, e um "Como testar" da fase de design.
3. *(Motivo da pausa e "bloqueado por": fica no NAC-4, que já cria `pause_reason`.)*
4. **Repositório não cadastrado fica fora do board.** 14 sessões reais, cerca de US$ 984, em `mscloud`, `bravonix` e outros, não têm projeto nem card. O hook não diz nada ao agente fora de projeto cadastrado.

Pedido do usuário (2026-11-15): implementar 1, 2 e 4. No 4, o agente valida se o projeto existe. Se não existir, cadastra e, com base no histórico, cria os cards importantes para avançar. A regra deve valer sempre, para qualquer agente.

## Objetivos

- **Branch do card:** segue o branch de trabalho da sessão, nunca a base. O agente também pode informá-lo (`nobre branch`).
- **Integração detectada:** quando o branch do card entra na base do projeto, o card registra o fato. Se estava em Revisão, vai para Concluído.
- **Perguntas:** o agente responde com `nobre answer <id> "<resposta>"`. Os ids aparecem no `nobre ask`, no `nobre status` e no pacote.
- **`nobre done`:** recusa quando falta "Como testar", quando ele é mais antigo que a última edição de arquivo do card, ou quando há pergunta sem resposta. `--force` passa por cima.
- **Repositório não cadastrado:** o SessionStart avisa e o agente cadastra (`nobre project add`). O cadastro religa as sessões antigas daquela pasta, inclusive a sessão atual. `nobre project scan` entrega o levantamento. O agente cria até 5 cards no Backlog com `nobre card add`, sem ligar a sessão a eles.

## Fora do escopo e o que não fazer

- Motivo de pausa, "bloqueado por" e prontidão por destino: ficam no NAC-4 e numa conversa de design futura.
- Detectar merge por squash: o histórico perde o ancestral, então o card não conclui sozinho. Fica registrado como limite.
- `git fetch` ou qualquer escrita no git pelo daemon: só leitura, com `--no-optional-locks`.
- **Anti-objetivo:** concluir sozinho um card que não terminou. O NAC-5 teve a parte 1 integrada e continua. Só Revisão vai para Concluído; os outros cards ganham o registro e ficam onde estão.
- **Anti-objetivo:** inundar o board. No `mscloud` há 110 branches não integradas. O levantamento filtra, e o agente cria no máximo 5 cards.
- **Anti-objetivo:** cadastrar a pasta pessoal. A extensão do GNOME roda `claude /usage` em `/home/nobre` a cada 5 minutos.

## Abordagens consideradas

- **A. Regra só na skill ou no `CLAUDE.md`:** depende de o agente carregar a skill, só vale para o Claude e não tem parte mecânica. Descartada.
- **B. Hook + skill + CLI (escolhida):** o hook é o gatilho garantido em toda sessão e em todo adaptador futuro (Codex e Kiro do plano 2). A skill é o passo a passo. O CLI e o daemon fazem a parte mecânica (levantamento, cadastro, religar sessões), igual para todo agente.
- **C. O daemon cadastra e cria os cards sozinho:** escolher o que é "importante" precisa de julgamento. Descartada, mas o cadastro e o levantamento ficam no código, como em B.

Na integração:
- **Escolhido:** ancestral no git (`merge-base --is-ancestor`), contra a base local e contra `origin/<base>`, lembrando o último commit visto do branch.
- **Descartado:** consultar PRs no GitHub (`gh`), porque exige rede e autenticação e não vale para repositório sem remote, como este.

## Decisões

### Do usuário
- Cadastrar o repositório e criar os cards pelo agente, sem pedir confirmação. Isso substitui a regra antiga "não cadastre por conta própria" e o NAC-7.
- Itens 1, 2 e 4 neste card. O 3 fica no NAC-4.

### Do Claude (o usuário pode corrigir)
- **Card importante:** no máximo 5 por levantamento. O levantamento mostra:
  - branches com commits nos últimos 30 dias que ainda não estão na base, com destaque para as dos últimos 14 dias;
  - worktrees;
  - planos com tarefas abertas;
  - sessões recentes com custo.

  Ficam de fora `backup/*`, a base e branches sem commits próprios. Branches da mesma entrega viram um card só.
- **Cadastro:** só repositórios git e nunca a pasta pessoal. Um worktree conta como o checkout principal (`Locate().Main`). Projeto cadastrado com captura desligada funciona como "ignorar": o hook não volta a avisar.
- **Base do projeto:** a primeira que existir entre `develop` (local ou `origin/develop`), o alvo de `origin/HEAD`, `main` e `master`. Fica gravada no cadastro, e `nobre project base <branch>` troca. Num projeto antigo, sem base gravada, a detecção roda a cada verificação.
- **Integração:** um card só é avaliado se tiver branch diferente da base e se o daemon já tiver visto esse branch com commits fora da base. Branch apagado logo depois do merge, sem ter sido visto antes, não conclui o card (limite aceito). A verificação roda a cada 2 minutos, só para cards em Em andamento, Pausado ou Revisão.
- **Registro da integração:** entra como decisão do card: "Branch `<b>` integrada em `<base>` (`<sha7>`)", com o porquê "detectado pelo Cockpit". Uma decisão aparece no pacote de retomada sem precisar de sessão.
- **Sessões antigas:** cadastrar um projeto religa as sessões sem projeto cujo `cwd` está dentro da pasta (com o mesmo limite de diretório de `ProjectForPath`) e liga a captura delas.
- **Cards fechados à mão:** NAC-1..3 vão para Concluído pela API depois da entrega, com a evidência do merge `18a717f`. Os branches deles já foram apagados, então o detector não os alcança.

## Fica com quem implementar

- O formato exato do texto do `nobre project scan` (legível para o agente; a lista de campos está no plano).
- O ponto exato da UI para a marca "branch integrada" (Board e cabeçalho do card).
