# Limites do plano na plataforma: plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mostrar no Cockpit os limites do plano Claude (sessão de 5 horas e semana), com percentual, horário de reinício, histórico e alerta.

**Architecture:** O único canal documentado é o JSON que o Claude Code passa à status line (`rate_limits.five_hour` e `rate_limits.seven_day`, com `used_percentage` 0–100 e `resets_at` em epoch segundos; ausente fora de Pro/Max/Ultra, antes da primeira resposta da sessão ou depois do reinício da janela — fonte: https://code.claude.com/docs/en/statusline). Um comando novo, `nobre statusline`, entra no pipe da status line: repassa o JSON intacto para a status line do usuário e envia os limites ao `nobred`. O daemon guarda as leituras e a interface mostra um painel na Sala de controle.

**Tech Stack:** Go (daemon/CLI), React (UI) — mesmos padrões do repositório.

**Spec:** pedido do usuário em 2026-11-08 ("e os limites da sessão? dá para acompanhar pela plataforma?" → "pode implementar"). Decisões abaixo são do Claude.

## Global Constraints

- Constraints de `docs/plan.md` (pt-BR, Linux, loopback, `localOnly`, JSON em toda escrita).
- **A status line do usuário nunca pode quebrar nem atrasar perceptivelmente:** `nobre statusline` escreve no stdout exatamente os bytes recebidos (antes de qualquer rede), fecha o stdout e entrega ao daemon num processo destacado (`nobre statusline --enviar`, 2 s) sem esperar, ignora SIGPIPE e sai 0 sempre. Daemon fora, JSON inválido ou sem `rate_limits` → só repassa.
- **Forma do wrapper no `settings.json`:** `statusLine.command = "{ <nobre absoluto> statusline || cat; } | {\n<comando original>\n}"` (grupo sempre fechado em nova linha). O `|| cat` só roda se o binário do nobre faltar (exit 127): a status line sai igual à original mesmo com o nobre desinstalado ou movido. As formas antigas `<nobre> statusline | {\n<orig>\n}` e `<nobre> statusline | { <orig> ; }` ainda são reconhecidas (e reinstaladas na forma nova). Assim o comando original continua rodando no shell do próprio Claude Code, sem mudança de comportamento. Original terminando em `\` sozinho ou só com comentários: recusa com mensagem pt-BR ("não consigo integrar esta status line com segurança: ...") em vez de gerar shell inválido. Sem status line anterior: `"<nobre> statusline --resumo"` (imprime um resumo curto, ex.: `5h 23% · semana 41%`). Reconhecimento idempotente pelo prefixo `"[{ ]<qualquer caminho>/nobre statusline"`; reinstalar troca o caminho sem duplicar; desinstalar devolve o comando original exato. `refreshInterval`: definir `60` se ausente (não mexer se o usuário já definiu).
- **Consentimento:** `nobre setup` integra a status line por padrão (`--sem-statusline` pula; `--remover-statusline` desfaz). Na UI, "Instalar o que falta" só instala hooks e skill; a status line é uma ação separada ("Integrar status line" / "Remover integração", `POST /api/agents/claude/statusline {"action":"install"|"uninstall"}`).
- **Leitura vigente por janela:** a de `resets_at` mais novo; com o mesmo `resets_at`, a de maior `used_pct`; `resets_at` nulo só vale se nenhuma leitura da janela tiver `resets_at` (aí vale a mais recente por `received_at`). Sessões ociosas reenviam o `rate_limits` cacheado a cada `refreshInterval`, então "a última recebida" alternaria entre sessões. Ao gravar: descartar (sem inserir nem atualizar) a leitura com `resets_at` mais antigo que o da vigente ainda não expirada, ou com o mesmo `resets_at` e `used_pct` menor que o máximo já visto para ele. Coalescência por minuto do relógio (UTC): uma linha por janela, minuto e `resets_at`, que nunca reduz `used_pct`. O daemon só publica `plan` quando a vigente (`used_pct`/`resets_at`) de alguma janela muda. A UI trata como expirada a janela cujo `resets_at` já passou no relógio do cliente (painel e alerta).
- Mesmo cuidado da Task 10 do piloto ao editar `settings.json`: backup O_EXCL antes de mudar, JSON inválido aborta sem escrever, symlink preservado, sem escapar `&&`/`<`/`>`, idempotente.
- Ruling H: testes nunca tocam o `~/.claude` real nem o `nobred` instalado (porta 7420).

## Review Focus

1. **Status line com o daemon fora** → saída idêntica à original, em < 400 ms. (Task 2)
2. **Comando original com aspas, `$`, `{}`, `;`, várias linhas** → continua funcionando dentro de `{ ...; }`. (Task 2)
3. **Janela ausente / plano sem limites** → painel mostra "sem dados do plano ainda" ou "plano sem limites de uso", nunca 0%. (Tasks 1, 3)
4. **Várias sessões enviando leituras** → a leitura mais recente (por horário de recebimento) vale; o histórico não explode (uma linha por minuto no máximo por janela). (Task 1)

---

### Task 1: Daemon — leituras dos limites, API e alerta

**Files:**
- Create: `internal/store/migrations/00N_plan_limits.sql` (próximo número livre), `internal/store/planlimits.go`, `internal/api/planlimits.go`
- Modify: `internal/api/server.go` (rotas), `internal/api/overview.go` (alerta), `internal/domain/types.go` (Settings: `PlanWarnPct int`, json `plan_warn_pct`, padrão 80)
- Test: `internal/store/planlimits_test.go`, `internal/api/planlimits_test.go`, `internal/api/overview_test.go`

**Interfaces:**
- Tabela `plan_limits(id, workspace_id, owner_id, window TEXT CHECK(window IN ('five_hour','seven_day')), used_pct REAL, resets_at TEXT NULL, received_at TEXT, session_id TEXT)`.
- `store.RecordPlanLimits(r domain.PlanReading) error` com `domain.PlanReading{SessionID string; At time.Time; FiveHour, SevenDay *domain.PlanWindow}` e `PlanWindow{UsedPct float64; ResetsAt *time.Time}` — grava uma linha por janela presente, mas se a última linha daquela janela tem menos de 60 s e o mesmo `resets_at`, atualiza em vez de inserir.
- `store.LatestPlanLimits() (map[string]domain.PlanWindowState, error)` e `store.PlanLimitsHistory(window string, since time.Time) ([]domain.PlanPoint, error)`. `PlanWindowState{UsedPct float64; ResetsAt *time.Time; ReceivedAt time.Time; Expired bool}` (`Expired` = `resets_at` já passou).
- `POST /api/plan-limits` com o corpo `{"session_id":"...","rate_limits":{...como o Claude Code manda...}}` → 204. Valida: `used_percentage` entre 0 e 100 (fora → descarta a janela), `resets_at` inteiro positivo.
- `GET /api/plan-limits?hours=24` → `{"five_hour": PlanWindowState|null, "seven_day": ...|null, "history": {"five_hour":[...], "seven_day":[...]}, "warn_pct": 80}`. Publica `plan` no Hub a cada gravação.
- Overview ganha alerta `plan_limit`: "Sessão de 5 h do plano em <pct>% (reinicia às <HH:MM>)." / "Limite semanal do plano em <pct>% (reinicia <dia> às <HH:MM>)." quando `used_pct >= plan_warn_pct` e a janela não expirou.
- [ ] Testes que falham: gravar/ler, coalescência de 60 s, janela expirada, validação, GET com histórico, alerta com textos exatos. → implementar → passar → commit `feat: limites do plano no nobred`.

---

### Task 2: `nobre statusline` e instalação na status line

**Files:**
- Create: `cmd/nobre/statusline.go`, `internal/adapter/claude/statusline.go`
- Modify: `cmd/nobre/main.go` (comando e ajuda), `internal/adapter/claude/install.go`/`detect.go` (instalar e checar a status line), `cmd/nobre/setup.go` (passo novo), `internal/client/client.go`
- Test: `cmd/nobre/statusline_test.go`, `internal/adapter/claude/statusline_test.go`

**Interfaces:**
- `nobre statusline [--resumo]`: lê stdin (até 1 MiB), escreve os bytes no stdout imediatamente (sem `--resumo`) ou o resumo `5h <pct>% · semana <pct>%` (com `--resumo`; janelas ausentes omitidas; nada se não houver nenhuma), depois POST `/api/plan-limits` com timeout total de 300 ms; sempre sai 0, sem stderr.
- `claude.InstallStatusLine(settingsPath, nobreBin string) (adapter.InstallReport, error)` e `claude.UninstallStatusLine(...)` seguindo a forma do wrapper das Global Constraints; `Detect` ganha o check `statusline` ("Status line integrada").
- `nobre setup` instala a status line junto com os hooks (com `--dry-run` mostrando o que mudaria) e `nobre setup --sem-statusline` pula esse passo.
- [ ] Testes que falham: repassa bytes idênticos com daemon fora em < 400 ms; resumo; POST enviado com o JSON; instalação com comando original real do usuário (o snippet bash com `$(cat)`, `${cwd/#$HOME/~}`, aspas e `printf` com `\033`) — rodar o comando resultante com `sh -c` e com `bash -c` alimentando um JSON de exemplo e comparar a saída com a do original; idempotência; reinstalação de outro caminho; desinstalação devolve o original; sem status line → `--resumo`; `refreshInterval` só quando ausente; JSON inválido aborta; backup. → implementar → passar → commit `feat: nobre statusline envia os limites do plano`.

---

### Task 3: Painel "Limites do plano"

**Files:**
- Create: `web/src/pages/PlanLimits.tsx` (componente do painel)
- Modify: `web/src/pages/ControlRoom.tsx` (painel ao lado de "Custo hoje × limite diário"), `web/src/pages/Config.tsx` (campo "Avisar quando o limite do plano passar de N%"), `web/src/api/types.ts`, `queries.ts`, `useStream.ts` (kind `plan`)
- Test: `web/src/pages/PlanLimits.test.tsx`

**Interfaces:**
- Painel com duas linhas (Sessão de 5 h, Semana): barra 0–100% com marcador no `warn_pct`, "<pct>%", "reinicia em <tempo> (às HH:MM)" e "atualizado há <tempo>"; janela ausente → "sem dados ainda"; janela expirada → "reiniciada — aguardando nova leitura". Pequeno histórico (sparkline das últimas 24 h, com `aria-label` descrevendo o mínimo/máximo). Quando nunca houve leitura: texto explicando que os dados chegam pela status line do Claude Code e link para Configuração.
- Config: no bloco de agentes, o check "Status line integrada" (vem do Detect) e o campo do percentual de aviso.
- [ ] Testes que falham: renderiza percentuais e reinício; ausente/expirado; marcador; Config salva `plan_warn_pct`. → implementar → passar (`pnpm -C web test --run`, `pnpm -C web build`, `make build`) → commit `feat: painel de limites do plano`.
