# Status do piloto — NobreAI Cockpit

Atualizado em 2026-11-15 (a seção "Configurações em vigor" vem do branch `feat/configuracoes`). Plano: `docs/plan.md`. Spec: `docs/spec.md`.

## Etapa "bases + frontend": concluída

| Task | O que entrega |
|---|---|
| 1 | Domínio e banco SQLite com migrações |
| 2 | Cards (`NAC-n`), projetos e transições do board |
| 3 | Conteúdo do card e regra de "pronto para handoff" |
| 4 | Sessões, eventos e identificação do processo do agente (`/proc`) |
| 5 | Tokens dos transcripts do Claude Code e custo |
| 6 | Prompts salvos com variáveis e pacote de retomada |
| 7 | API HTTP + SSE do `nobred` |
| 11 | Reconciliação do board, limites e `/api/overview` |
| 18 | Dados de demonstração (`nobred --seed-demo`, nunca toca o banco real) |
| 13 | Base da interface web embutida no `nobred` |
| 14 | Sala de controle, Board, lista de Cards e "Nova tarefa" |
| 15 | Tela do card (documento de handoff) |
| 16 | Analytics, Configuração e Prompts |

O processo:
- Cada task foi feita com o teste antes do código e passou por uma revisão independente.
- No fim houve uma revisão do branch inteiro, seguida de uma rodada de correções e de uma re-revisão, que aprovou todas as correções.
- `go vet ./... && go test ./...` e `pnpm -C web test --run` (56 testes) estão verdes, e `make build` funciona.

### Como ver funcionando

```bash
cd /home/nobre/workspace/nobretech/nobreai-cockpit
make build
./nobred --seed-demo      # banco separado: ~/.local/share/nobreai-cockpit/cockpit-demo.db
# abrir http://127.0.0.1:7420
```

Sem `--seed-demo`, o `nobred` usa o banco real (vazio até os hooks existirem). O daemon só escuta e atende em loopback.

## Etapa "integração": concluída

Branch `feat/piloto-integracao` (a partir de `feat/piloto-bases-frontend`).

| Task | O que entrega |
|---|---|
| 8 | Adaptador do Claude Code (hooks): contexto no início, pacote via `NOBRE_RESUME`, registro de prompts/arquivos/comandos, bloqueio único para atualizar o card, lembrete de card, limites, tokens dos transcripts (com subagentes) |
| 9 | CLI `nobre` (comandos do agente, administração e `nobre hook`, que nunca quebra o agente) |
| 10 | `nobre setup`: hooks no `~/.claude/settings.json` com backup, skill `nobre-cockpit`, serviço systemd; seção de agentes na Configuração |
| 12 | "Continuar com outro agente": abre o Claude num terminal novo (formato por terminal), com PATH correto e janela que fica aberta se falhar |
| 17 | Teste de ponta a ponta do handoff e roteiro manual `docs/piloto-checklist.md` |

Houve revisão por task, revisão final do branch, uma rodada de correções e uma re-revisão, que aprovou tudo. `go test ./...` e `pnpm -C web test --run` (65 testes) estão verdes.

### Para instalar e testar
Siga `docs/piloto-checklist.md` (instala `nobre`/`nobred` em `~/.local/bin` e roda `~/.local/bin/nobre setup`, de preferência antes com `--dry-run`).

### Corrigido depois da revisão
- A unit do `nobred` usa `KillMode=process`: terminais e sessões do Claude abertos pelo "Continuar" sobrevivem a um restart do daemon.
- O prompt padrão "Retomar card" diz que o pacote está no contexto da sessão.

### Pendências pequenas
- A prévia de prompt mostra o pacote; o "Continuar" o envia vazio.
- A unit guarda o PATH do terminal onde o setup rodou (inclusive venv/nvm).
- Outros menores estão no ledger local.

## Depois do piloto (2026-11-07)

Branch `feat/terminais` (a partir de `feat/piloto-integracao`):
- **Terminais na plataforma** (`docs/plan-terminais.md`): o `nobred` roda o agente num pseudo-terminal e o transmite por WebSocket. Há a página "Terminais", o painel "Terminal" no card e "Ver terminal" na Sala de controle. O "Continuar" ganhou o modo "Em segundo plano, com saída aqui na interface". `nobre run claude` deixa a sessão visível ao mesmo tempo no terminal local e na plataforma: Ctrl+] solta sem matar, e fechar a janela encerra o agente. A interface abre em "Somente assistir" até você clicar em "Interagir". Terminais da plataforma são encerrados quando o `nobred` para ou reinicia.
- **Worktrees** (NAC-1 a NAC-3, feitos pelo próprio agente durante o teste real): visualização dos worktrees ligados a agentes, recursos do sistema por worktree e página de detalhe. O "Continuar" abre o agente no worktree do branch do card, quando ele existir.

Revisão por task, revisão final, correções e re-revisão aprovadas. Pontos menores adiados ficam no ledger local `.superpowers/sdd/plan-terminais/progress.md`.

## Configurações em vigor (NAC-4, 2026-11-15)

Branch `feat/configuracoes` (plano `docs/plan-configuracoes.md`). Cada opção da tela Configuração agora tem efeito real, ou diz que é "em breve".

### Criação de cards
| Opção | Onde vale |
|---|---|
| Quando eu pedir | Sempre ativo. |
| Quando o agente identificar que está começando algo que precisa de histórico (`agent_creates_cards`, padrão ligado) | `SessionStart`: o contexto injetado traz o critério de criação. Desligada, ele diz "Só crie card quando o usuário pedir" (a skill obedece), o aviso de repositório não cadastrado mantém o cadastro e o levantamento mas manda listar ao usuário os cards sugeridos em vez de criá-los, e o lembrete do hook `Stop` não dispara. |
| Se ele esquecer, lembrar (`reminder_edit_threshold`, padrão 3 arquivos; desligado = 0) | Hook `Stop`: numa sessão sem card (e com `agent_creates_cards` ligado), lembra o agente uma vez por sessão (bloqueia o fim do turno) quando ela editou esse número de arquivos distintos ou mais. 0 desliga. |
| Mostrar cards pausados no início da sessão (`inject_paused_cards`, padrão ligado) | `SessionStart`: lista os cards pausados do mesmo repositório e branch. Desligada, a seção "Cards pausados neste repositório" não aparece. |

Um banco que não tem as duas chaves novas assume ligado (o comportamento de antes).

### Pronto para handoff
| Opção | Onde vale |
|---|---|
| Critérios (objetivo, onde parei, arquivo essencial, falta, como testar, sem perguntas abertas) | Barrinhas do card e do board, e a regra do fim de sessão. |
| "Onde parei" vale por (minutos) | Prontidão do card. |
| Se a sessão terminar sem cumprir a regra | `Pedir ao agente que complete` (`ask_agent`): o hook `Stop` bloqueia uma vez o fim do turno quando a sessão editou arquivos depois do último "Onde parei" (só se o critério "Onde parei" estiver ligado). `Marcar como handoff incompleto` (`mark`): ao encerrar a sessão, se o card ficou em Pausado, sem outra sessão viva e fora da regra, o card guarda a marca `handoff_incomplete` e a Sala de controle alerta "NAC-x foi pausado com handoff incompleto". Hook de fim de sessão e reconciliador usam o mesmo caminho (`board.FinishSession`). Trocar de `mark` para `ask_agent` esconde o alerta, mas a marca fica guardada. |
| Continuar com… (terminal ou segundo plano) e comando do terminal | Botão "Continuar" do card. |
| Carência (minutos) | Reconciliador: encerra a sessão cujo processo sumiu. |

### Limites de custo
Ação em 100% "Bloquear o agente e mover o card para Pausado": o hook `PreToolUse` nega a ferramenta (comandos `nobre` sempre passam, para o agente registrar o handoff), move o card para Pausado com o motivo "limite de custo" e registra o evento. Na primeira ferramenta permitida depois de o limite ser elevado, o card volta para Em andamento. "Só avisar" não bloqueia nada. O aviso do plano (`plan_warn_pct`) vale na Sala de controle.

### Sinais do card
Mostrados no board, na lista e no card, valem na hora (os três primeiros não esperam 24 h):
- **bloqueado por NAC-x**: card em Pausado ou Backlog com `nobre block NAC-x [--why ...]` e o bloqueador existente e não concluído. `nobre block --clear` remove; chave inexistente é recusada.
- **pronto para retomar (NAC-x concluído)**: o bloqueador foi concluído.
- **aguardando você há …**: card em Pausado com pergunta aberta.
- **parado**: Em andamento ou Pausado sem atividade há mais de 24 h (o agente possivelmente travou: mais de 24 h sem atividade).
- **aguardando revisão**: card em Revisão sem atividade há mais de 24 h.

`pause_reason`, `handoff_incomplete` e `blocked_by` são limpos quando o card volta para Em andamento (inclusive ao ser retomado por outra sessão). Se o bloqueador for apagado, o card cai na regra normal.

### Ainda "em breve"
- Modo tmux do "Continuar" (aparece desabilitado).
- Adaptadores Codex e Kiro.
- Limites mensais.
- Moeda nos textos gerados pelo daemon (SessionStart, avisos): saem sempre em US$; a moeda escolhida vale só na interface.
- Continuar com outro agente automaticamente ao pausar por limite.

## Plano 2 (fora do piloto)
Adaptadores Codex e Kiro, modos tmux e segundo plano, "bloquear novas sessões", limites mensais, linha do tempo das sessões e último prompt no pacote, indicador "Retomadas sem perguntas", MCP, times.

## Decisões tomadas durante a execução

1. Task 1 pulou o `git init`/cópia de docs (feitos antes, no commit 098676f).
2. Os corpos dos 5 prompts padrão vêm literalmente da tela Prompts do design.
3. A rota `POST /api/cards/{key}/attach` ficou na Task 7.
4. `Settings` ganhou o campo `demo`.
5. Ligar a um card uma sessão já encerrada deixa o card em "Pausado".
6. Quando uma sessão troca de card, o card anterior vai para "Pausado" se não tiver outra sessão viva, e mantém o próprio histórico.
7. A API local só aceita conexões de loopback, Host `127.x`/`localhost` na porta certa, e POST com corpo em JSON (proteção contra CSRF, DNS rebinding e acesso pela rede).
8. `LimitStatus` devolve erro em vez de esconder falhas de banco.
9. Controles que dependem das Tasks 10/12 aparecem desabilitados com "disponível na próxima etapa".
10. `/cards` é uma lista pesquisável de todos os cards.
11. Botão "Nova tarefa" para você mesmo criar cards no Backlog.
12. Novas rotas: `POST /api/prompts/render` (prévia de prompt não salvo) e `GET /api/events?kind=`.
13. Preço de entrada e de saída precisa ser maior que zero.
14. Sessão viva = `ended_at` nulo **e** processo existente. Durante a carência a UI mostra "processo encerrado há N min".
15. Mover um card para "Em andamento" sem sessão viva é recusado (409).
16. O custo fica no card em que foi gasto (`usage.card_id`), mesmo depois de um handoff.
17. Preço de cache em branco = desconhecido. Se houver tokens de cache, o custo vira "sem preço". `0` explícito = grátis. Preços de cache 0 já salvos viraram "desconhecido".
18. Sessão cujo processo morreu não impede outra sessão de assumir o card.
19. Uso registrado antes de a sessão ter card vai para o card quando ela é ligada.
20. O workspace de execução local (`.superpowers/`, fora do git) foi mantido para retomar as Tasks 8+.

## Como retomar

O piloto está completo. O próximo trabalho é o plano 2. O ledger em `.superpowers/sdd/plan/progress.md` guarda as decisões e os pontos menores adiados.
