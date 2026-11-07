# Status do piloto — NobreAI Cockpit

Atualizado em 2026-11-07. Branch: `feat/piloto-bases-frontend`. Plano: `docs/plan.md`. Spec: `docs/spec.md`.

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

### Cuidado conhecido
Se o "Continuar" abrir o terminator quando nenhuma janela dele estiver aberta, esse terminal e o Claude dentro dele ficam no grupo do serviço `nobred` e **são encerrados se o daemon for reiniciado** (rebuild, `nobre setup` de novo, `systemctl --user restart nobred`). Correção de uma linha pendente: `KillMode=process` na unit. Até lá, mantenha uma janela do terminator aberta antes de clicar em "Iniciar Claude Code neste card".

### Pendências pequenas
- O prompt padrão "Retomar card" ainda diz "pacote abaixo", mas o pacote chega pelo início da sessão.
- A prévia de prompt mostra o pacote; o "Continuar" o envia vazio.
- A unit guarda o PATH do terminal onde o setup rodou (inclusive venv/nvm).
- Outros menores estão no ledger local.

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
