# Monitor de contexto e sessão Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Medidor ao vivo de contexto/cache por sessão (por janela), avisos curtos ao agente e na tela, "Recomeçar limpo" pelo card, e os acabamentos da sessão filha.

**Architecture:** O leitor de transcript (NAC-31) passa a gravar o detalhe de uso de cada turno e a janela dona de cada linha (galho de `parentUuid` a partir do prompt de cada janela). Consultas e uma rota montam o medidor; os hooks `UserPromptSubmit`/`PostToolUse` respondem os avisos com estado persistido; "Recomeçar limpo" reaproveita o "Continuar".

**Tech Stack:** Go (SQLite), React + TypeScript + Vitest.

**Spec:** docs/superpowers/specs/2026-11-20-monitor-de-contexto-design.md

## Global Constraints

- Migração nova `internal/store/migrations/017_context_monitor.sql` (a última é 016).
- Tokens estimados = bytes/4 (como `usage.EstimateTokens`).
- Aviso de contexto (texto exato, com números formatados em pt-BR e "mil"): `Contexto em {pct}% da janela ({tokens} tokens). Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card.` — faixas 60 e 80; uma vez por faixa por segmento de compactação.
- Aviso de saída grande (texto exato): `Essa saída teve ~{tokens} tokens e fica sendo relida a cada turno. Prefira ler trechos (offset/limit, grep) em vez do arquivo inteiro.` — acima de 15.000 tokens; no máximo 3 por sessão.
- Configuráveis (settings, com padrões): `context_warn_pcts` [60, 80], `big_output_tokens` 15000, `big_output_max` 3, `agent_context_warnings` true, `model_windows` {"claude-opus-5-5": 1000000}, padrão 200000, `[1m]` no nome → 1000000.
- Fixos: regravação grande = `cache_write` ≥ 50.000 num turno, "recente" = 60 min; parada além do cache = último evento há mais que o TTL (1 h se o último turno gravou `ephemeral_1h`, senão 5 min) com contexto > 200.000; destaque de contexto > 80%.
- Avisos entregues como `hookSpecificOutput.additionalContext` (como `notice.go`/`contextReply`), combinados com os existentes.
- Nenhum outro texto novo no contexto do agente; nada chama modelo.
- Textos de tela em português, curtos.

## Review Focus

- Conversa com uma janela só: nada muda na atribuição (tudo na principal) e o medidor funciona (Task 2, Task 3).
- Releitura do histórico (versão nova do leitor) não duplica uso nem turnos e termina em segundos (Task 1).
- Prompt repetido igual nas duas janelas (ex.: "ok") não atribui o galho errado: casa texto + janela de tempo + o mais recente ainda não usado (Task 2).
- Avisos não repetem a cada prompt e voltam a valer depois de compactar; desligados com `agent_context_warnings=false` (Task 4).
- "Recomeçar limpo" sem "Onde parei" recente exige o texto; a sessão antiga fica viva sem card e o card abre no terminal novo (Task 5).

---

### Task 1: Leitor v2 — detalhe de uso, TTL e linhas com uuid

**Files:** Modify `internal/usage/context.go` (+ testes), `internal/store/context.go` (+ testes), `internal/adapter/claude/ingest.go`; Create `internal/store/migrations/017_context_monitor.sql`; Modify `internal/store/store_test.go` (user_version 17).

**Interfaces:**
- `usage.ContextTurn` ganha `Input, CacheRead, CacheWrite int64`, `TTL string` ("1h"|"5m"|""), `UUID, ParentUUID string` (da última linha do turno), e `ToolOutput` ganha `UUID, ParentUUID string`. O parser também devolve as linhas `user` de prompt: `Prompt{UUID, ParentUUID, Text string, At time.Time}` (texto do usuário, não `tool_result`).
- Migração 017: `context_turns` + colunas `input, cache_read, cache_write INTEGER NOT NULL DEFAULT 0`, `ttl TEXT NOT NULL DEFAULT ''`, `window_session_id TEXT NOT NULL DEFAULT ''`; `tool_outputs` + `window_session_id TEXT NOT NULL DEFAULT ''`; `usage` + `window_session_id TEXT NOT NULL DEFAULT ''`; tabela `window_lines(session_id TEXT, path TEXT, uuid TEXT, window_session_id TEXT, PRIMARY KEY(session_id, path, uuid))` (só linhas de janelas que não são a principal); tabela `window_prompts(id INTEGER PK, session_id TEXT, text_norm TEXT, at TEXT, used INTEGER NOT NULL DEFAULT 0)`; tabela `session_alerts(session_id TEXT PRIMARY KEY, warned_pct INTEGER NOT NULL DEFAULT 0, warned_segment INTEGER NOT NULL DEFAULT -1, big_outputs INTEGER NOT NULL DEFAULT 0, child_notice_sent INTEGER NOT NULL DEFAULT 0)`.
- Versão do leitor: setting KV `context_reader_version`; ao subir com versão < 2, zera `transcript_offsets.tool_offset`/`segment` e apaga `context_turns`/`tool_outputs` antes da passada do histórico (que já existe), uma vez.
- [ ] Testes: parser extrai input/cache_read/cache_write/ttl (`cache_creation.ephemeral_1h_input_tokens` > 0 → "1h"; `ephemeral_5m_input_tokens` > 0 → "5m"); uuid/parentUuid de turnos e saídas; prompts do usuário (texto string ou bloco text) e não os tool_result; store grava as colunas novas; versão < 2 dispara uma releitura única sem duplicar (rodar `BackfillContext` duas vezes).
- [ ] Commit `feat(usage): detalhe de cache por turno e versao do leitor`

### Task 2: Atribuição por janela

**Files:** Modify `internal/adapter/claude/hooks.go` (registrar prompt no `UserPromptSubmit`: `window_prompts` com a sessão roteada, texto normalizado e horário), `internal/adapter/claude/ingest.go` / `internal/store/context.go` / `internal/store/usage.go` (atribuir), testes em `internal/adapter/claude/window_test.go`.

**Regras:** ao ler uma linha com uuid: (a) se é prompt do usuário e existe `window_prompts` não usado com mesmo texto normalizado, de uma sessão da mesma conversa, com `at` até 30 s antes ou depois do horário da linha → a janela é essa sessão (marca o prompt como usado; com vários, o mais próximo no tempo); (b) senão herda a janela do `parentUuid` (procurando em `window_lines` e no que já foi visto na leitura); (c) senão a principal. Linhas de janela ≠ principal vão para `window_lines`. `context_turns`, `tool_outputs` e `usage` gravam `window_session_id`; `usage.card_id` passa a ser o card da janela dona (não o da sessão que disparou a leitura).
- [ ] Testes: conversa com uma janela → tudo na principal; duas janelas com prompts distintos → turnos/saídas/uso de cada galho na sessão certa e custo no card de cada uma; prompt igual nas duas janelas ("ok") resolvido pelo tempo; galho continua certo atravessando leituras incrementais (pai numa leitura, filho na seguinte); resumo de compactação sem dono → principal.
- [ ] Commit `feat: uso, contexto e saidas atribuidos a janela certa`

### Task 3: Consultas do medidor, janelas por modelo e rota

**Files:** Create `internal/store/context_live.go` (+ testes), `internal/api/context_live.go` (+ testes); Modify `internal/domain/readiness.go` (settings do plano com padrões), `internal/api/config.go` (validação: pcts entre 1 e 99 em ordem crescente, big_output_tokens ≥ 1000, big_output_max 0–20, janelas ≥ 1000).

**Interfaces:**
- `store.SessionContext(sessionID string) (ContextLive, error)` com: `Tokens` (último turno da janela no transcript principal), `Model`, `WindowTokens`, `WindowEstimated bool`, `Pct`, `GrowthPerTurn` (média desde a última compactação, 0 com menos de 3 turnos), `TurnsToLimit` (estimado até 95% da janela; 0 sem crescimento), `TurnsSinceCompact`, `Compactions`, `CacheHitPct` (últimos 20 turnos), `RecentRewrite` (maior `cache_write` ≥ 50.000 nos últimos 60 min, com horário), `TTL`, `IdleSec` (desde `last_event_at`), `IdleOverTTL bool` (com contexto > 200.000), `TopOutputs` (5 maiores da janela).
- Janela: `model_windows` + regra `[1m]` + "observado acima → 1.000.000, fixo para a sessão".
- `GET /api/sessions/{id}/context` → `ContextLive`; `GET /api/context/live` → lista das sessões vivas com captura (resumo: id, card, tokens, pct, flags `over80`, `rewrite`, `idle_over_ttl`).
- [ ] Testes com turnos semeados: pct e janela (tabela, `[1m]`, observado), crescimento/turnos até o limite, hit rate, regravação recente, idle sobre TTL (1h vs 5m), top outputs por janela, sessão sem turnos → zeros; validação dos settings.
- [ ] Commit `feat: medidor de contexto por sessao`

### Task 4: Avisos ao agente

**Files:** Modify `internal/adapter/claude/hooks.go` (`UserPromptSubmit`: aviso de faixa; `PostToolUse`: aviso de saída grande; usar `session_alerts`), `internal/adapter/claude/notice.go` (combinar), `internal/store` (ler/gravar `session_alerts`), mover o "aviso de outra janela já enviado" do mapa em memória para `session_alerts.child_notice_sent`; testes `internal/adapter/claude/alerts_test.go`.

- [ ] Testes: cruza 60% → aviso com o texto exato uma vez; segundo prompt ainda em 65% → nada; 82% → aviso de 80%; nova compactação (segmento novo) e 61% → avisa 60% de novo; `agent_context_warnings=false` → nada; saída de 16.000 tokens no PostToolUse → aviso; quarta saída grande → nada; texto de saída grande respeita `big_output_tokens`; aviso de outra janela sobrevive a reinício (estado no banco); avisos combinados com o de configuração numa resposta só.
- [ ] Commit `feat(claude): avisos de contexto e de saida grande ao agente`

### Task 5: Recomeçar limpo

**Files:** Modify `internal/api/continue.go` ou Create `internal/api/restart_clean.go` (+ testes); `internal/store` (desligar o card da sessão: `card_id` NULL e card em `pausado`).

- `POST /api/sessions/{id}/restart-clean` `{"where_stopped": "texto opcional"}`: se o "Onde parei" do card está vazio ou com mais de 30 min e o corpo não traz texto → 400 "registre onde parou antes de recomeçar"; com texto → grava; desliga o card da sessão (sessão segue viva) e põe o card em pausado; chama o mesmo caminho do "Continuar" (mesmo agente, modo padrão das configurações) e devolve o resultado dele. Sessão sem card → 400.
- [ ] Testes: os três caminhos (sem texto e velho → 400; com texto → grava, desliga, continua; recente sem texto → continua); sessão filha com card próprio.
- [ ] Commit `feat(api): recomecar limpo pelo card`

### Task 6: Telas

**Files:** Create `web/src/pages/context/ContextMeter.tsx` (+ testes), `web/src/pages/context/RestartCleanDialog.tsx` (+ testes); Modify `web/src/pages/ControlRoom.tsx` (coluna/medidor por sessão viva com destaques e linha expansível com o detalhe), `web/src/pages/CardPage.tsx` (seção "Contexto" das sessões vivas do card com o detalhe e o botão "Recomeçar limpo"), `web/src/pages/Config.tsx` (avisos: faixas, saída grande, máximo, liga/desliga; janelas por modelo na seção de preços), `web/src/api/types.ts`, `queries.ts`, `useStream.ts` (contexto invalidado por `usage` e `sessions`).

- [ ] Testes: medidor mostra tokens, % e "janela estimada"; destaques (acima de 80%, regravação recente, parada além do cache); detalhe com crescimento, turnos até o limite, cache, top saídas; diálogo exige texto quando o "Onde parei" está velho e chama a rota; configurações salvam e validam.
- [ ] Commit `feat(web): medidor de contexto, recomecar limpo e configuracoes`

### Task 7: Acabamentos da sessão filha

**Files:** Modify `internal/adapter/claude/hooks.go` (`createChild` localiza pela cwd do payload da filha, como `createSession`), `internal/board/reconciler.go` (processo vivo = PID vivo e `comm` "claude", mesma checagem do roteamento, injetável) + testes.

- [ ] Testes: filha nascida com cwd diferente da principal já tem branch/worktree próprios; reconciliador trata PID reciclado (comm ≠ claude) como morto.
- [ ] Commit `fix: filha localizada ao nascer e reconciliador confere o processo`
