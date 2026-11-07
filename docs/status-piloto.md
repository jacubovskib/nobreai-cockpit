# Status do piloto — NobreAI Cockpit

Atualizado em 2026-11-07. Branch: `feat/piloto-bases-frontend`. Plano: `docs/plan.md`. Spec: `docs/spec.md`.

## Pronto (backend da etapa "bases")

| Task | O que entrega | Commits |
|---|---|---|
| 1 | Domínio e banco SQLite com migrações | 6686f1a |
| 2 | Cards (`NAC-n`), projetos e transições do board | 1b7339c |
| 3 | Conteúdo do card e regra de "pronto para handoff" | 2da6ba2 |
| 4 | Sessões, eventos e identificação do processo do agente (`/proc`) | e0ed48f |
| 5 | Tokens dos transcripts do Claude Code e custo | 0bd7de1 |
| 6 | Prompts salvos com variáveis e pacote de retomada | e8d2d2f |
| 7 | API HTTP + SSE do `nobred` (só aceita `127.0.0.1`/`localhost` e JSON) | 50ee20c..851a324 |
| 11 | Reconciliação do board, limites e `/api/overview` | 4a9f3bb..8e498e9 |
| 18 | Dados de demonstração (`nobred --seed-demo`, nunca toca o banco real) | 941a37c..60d2d0c |

Cada task passou por implementação com testes primeiro e por uma revisão independente. `go vet ./... && go test ./...` está verde.

### Como ver funcionando

```bash
cd /home/nobre/workspace/nobretech/nobreai-cockpit
go run ./cmd/nobred --seed-demo        # banco separado: ~/.local/share/nobreai-cockpit/cockpit-demo.db
curl -s -H 'Host: 127.0.0.1:7420' http://127.0.0.1:7420/api/overview | head
```

Ainda não há interface web: a API responde, mas as telas são as próximas tasks.

## Falta fazer

### Próximo: frontend (nesta ordem)
1. **Task 13** — base da UI (Vite + React + TS), tokens do design, barra lateral, cliente da API, stream SSE, embed no `nobred`. O proxy do Vite precisa de `changeOrigin: true` por causa da checagem de Host. Mostrar faixa "Dados de demonstração" quando a setting `demo` for true.
2. **Task 14** — Sala de controle e Board.
3. **Task 15** — Tela do card (handoff). O botão "Continuar com…" fica desabilitado com "disponível na próxima etapa" até a Task 12.
4. **Task 16** — Analytics, Configuração e Prompts. A seção de agentes fica com "disponível na próxima etapa" até a Task 10. O preview de prompt precisa funcionar com texto ainda não salvo (hoje a rota de render exige prompt salvo; ajustar).

### Depois: integração com os agentes
5. **Task 8** — adaptador do Claude Code (hooks).
6. **Task 9** — CLI `nobre`.
7. **Task 10** — setup automático (hooks, skill, serviço systemd).
8. **Task 12** — "Continuar com…" abrindo o terminal. **Antes dela:** validar `launch_mode` e `terminal_cmd` no `PUT /api/settings` e exigir `Content-Type: application/json` em todo POST (inclusive sem corpo).
9. **Task 17** — teste de ponta a ponta do handoff e checklist com o Claude real.

### Plano 2 (fora do piloto)
Adaptadores Codex e Kiro, modos tmux e segundo plano, "bloquear novas sessões", MCP, times.

## Decisões tomadas durante a execução

- Task 1 pulou o `git init`/cópia de docs (feitos antes, no commit 098676f).
- Os corpos dos 5 prompts padrão vêm literalmente da tela Prompts do design.
- A rota `POST /api/cards/{key}/attach` ficou na Task 7 (era da Task 14).
- `Settings` ganhou o campo `demo`.
- Ligar a um card uma sessão já encerrada deixa o card em "Pausado", nunca "Em andamento".
- Quando uma sessão troca de card, o card anterior vai para "Pausado" se não tiver outra sessão viva, e mantém o próprio histórico.
- A API local recusa Host diferente de `127.0.0.1`/`localhost` na porta certa e POST com corpo que não seja JSON (proteção contra CSRF e DNS rebinding).
- `LimitStatus` devolve erro em vez de esconder falhas de banco.
- As telas 15/16 mostram controles desabilitados para rotas que só chegam nas Tasks 10/12.

## Pontos menores anotados para a revisão final

Lista completa no ledger local (`.superpowers/sdd/plan/progress.md`, fora do git). Os que merecem atenção antes de usar no dia a dia:
- Seed de prompts e seed de demonstração não são atômicos.
- Mensagens de erro da API às vezes trazem texto técnico em inglês.
- `PUT /api/settings` com `"rules": null` apaga as regras.
- O custo por card no banco de demonstração fica próximo do design, mas não idêntico (ex.: US$ 4,41 em vez de 4,82).

## Como retomar

Retomar a execução por subagentes a partir da Task 13. O ledger em `.superpowers/sdd/plan/progress.md` marca o que está completo. Não refazer tasks marcadas como `complete`.
