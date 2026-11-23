# Integração com o Kiro e núcleo de agentes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O `kiro-cli` funciona no Cockpit igual ao Claude Code (sessões, cards, avisos, terminal, contexto em %, créditos), sobre um núcleo de regras que não conhece agente nenhum.

**Architecture:** Portas e adaptadores. `internal/agentcore` recebe eventos normalizados e devolve respostas normalizadas (as regras do Cockpit); cada agente é um driver (`internal/adapter/claude`, `internal/adapter/kiro`) que traduz hooks e histórico. Um registro (`internal/agents`) substitui o `claude` escrito direto na plataforma. Uma bateria de contrato roda contra cada driver.

**Tech Stack:** Go (SQLite), React + TypeScript + Vitest.

**Spec:** docs/superpowers/specs/2026-11-23-integracao-kiro-design.md

**Pré-requisito:** o NAC-40 (`feat/monitor-contexto`) mesclado no `main`. Trabalhar num worktree novo a partir do `main` (branch `feat/kiro`).

## Global Constraints

- Nenhum `if agent == "claude"`/`"kiro"` em `internal/agentcore`, `internal/api`, `internal/board`, `internal/store` ou `web/`: diferença de agente vai no driver ou numa capacidade opcional (interface descoberta por asserção de tipo).
- A suíte atual do adaptador do Claude (`internal/adapter/claude/*_test.go`) passa sem afrouxar asserções; só muda o que for import/construção.
- Créditos nunca somados com dólares: campos e colunas próprios (`credits`), exibidos ao lado ("N créditos"), nunca dentro de um total em US$.
- Kiro: só harness V3 (`kiro-cli` ≥ 3.1); lançador passa `--v3`; nenhuma flag de permissão (nunca `--trust-all-tools`); o Cockpit não usa `--resume-id`.
- Kiro instalado só em arquivos inteiros do Cockpit: `~/.kiro/hooks/nobreai-cockpit.json` e `~/.kiro/steering/nobreai-cockpit.md`. Nada de arquivos do usuário é editado; remover = apagar esses dois.
- Eventos do Kiro instalados: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SessionEnd`, `PostCompact`; comando `"<nobre> hook kiro <Evento>"`, `timeout` 10.
- Id da sessão Kiro: `kiro:<session_id>` (o `session_id` já vem como `sess_<uuid>`). Processo do agente: `kiro-cli`. Variável de sessão: `KIRO_SESSION_ID`.
- Limites em dólares (`board.LimitState`) não bloqueiam sessões cujo driver mede gasto em créditos; sem limites em créditos.
- Aviso de contexto em % (Kiro, sem tokens), texto exato: `Contexto em {pct}% da janela. Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card.` — mesmas faixas e regras de repetição do NAC-40.
- Migração nova `internal/store/migrations/019_agent_credits.sql` (a 018 é do NAC-40; conferir no `main` antes e renumerar se preciso).
- Textos de tela em português, curtos.

## Review Focus

- `SessionEnd` do Kiro chega com o `kiro-cli` já morto (AgentPID 0): encerra a sessão `kiro:<id>` existente e nunca cria uma sessão nova (Task 6).
- Ferramentas do Kiro fora do caminho feliz: `str_replace`/`fs_append`/`delete_file` contam como edição, `grep_search`/`file_search` como leitura sem arquivo, `mcp_<srv>_<tool>` e desconhecidas como "outro" sem evento de arquivo (Task 6).
- `messages.jsonl` ainda não existe no primeiro hook, ou está noutra pasta-hash: o leitor não falha nem enche o log; lê no hook seguinte (Task 8).
- Card com sessões do Claude e do Kiro: totais do card/projeto/período mostram US$ e créditos separados, e o US$ não muda com o Kiro (Tasks 5 e 10).
- Claude intocado: um prompt, uma edição, um `nobre start` e um `Stop` com handoff produzem exatamente as mesmas respostas e linhas no banco antes e depois da extração (Tasks 2–4).

---

### Task 1: Registro de agentes

**Files:** Create `internal/agents/agents.go` (+ `agents_test.go`); Modify `cmd/nobred/main.go`, `internal/client/client.go` (`DetectAgentPID`), `internal/api/agents.go`, `internal/api/terminals.go`, `internal/api/continue.go`, `internal/api/restart_clean.go`, `internal/api/overview.go` (`agentNames`), `internal/api/server.go` (Deps).

**Interfaces:**
- Produces: `agents.Registry` com `New(list ...adapter.Adapter) *Registry`, `Get(name string) (adapter.Adapter, bool)`, `All() []adapter.Adapter` (ordem de registro), `ProcessNames() map[string][]string`, `AllProcessNames() []string`; `agents.Known = []string{"claude", "codex", "kiro"}` e `agents.DisplayName(name string) string` ("Claude Code", "Codex", "Kiro"); `agents.Static() []string` = nomes de processo de todos os drivers compilados (sem store, para o `nobre`).
- `api.Deps.Agents *agents.Registry` substitui `Adapters map[string]adapter.Adapter`.

- [ ] Testes: registro devolve na ordem; `ProcessNames()` alimenta `board.Liveness.Names`; `/api/terminals` e `/api/cards/{key}/continue` aceitam qualquer agente registrado com `Binary() != ""` e recusam os não registrados com a mensagem atual (`msgAgentUnavailable`); `/api/agents` lista `Known` com os registrados detectados e os campos `installable` e `status_line`; `DetectAgentPID` acha um ancestral `kiro-cli` (proc falso).
- [ ] Trocar os pontos com `"claude"` fixo pelo registro (o padrão `in.Agent == ""` vira o primeiro de `All()`).
- [ ] `go test ./...` passa; commit `refactor: registro de agentes no lugar do claude fixo`.

### Task 2: Núcleo — eventos normalizados e ciclo da sessão

**Files:** Create `internal/agentcore/event.go`, `internal/agentcore/core.go`, `internal/agentcore/session.go`, `internal/agentcore/location.go` (+ testes); Modify `internal/adapter/claude/hooks.go`, `claude.go`; Create `internal/adapter/claude/translate.go`, `render.go`; Modify `internal/adapter/adapter.go` (`HookReply` ganha `Stderr string`), `cmd/nobre/hook.go` (escreve `Stderr` no stderr).

**Interfaces:**
- Produces (`agentcore`):
  - `type Kind int` com `Started, Prompt, ToolBefore, ToolAfter, TurnEnd, Ended, Compacted`.
  - `type ToolKind int` com `ToolRead, ToolEdit, ToolCommand, ToolOther`.
  - `type Event struct { Kind; Agent, ProviderSessionID, Cwd, Source, Prompt, Reason, TranscriptPath, SubagentID string; PID int; At time.Time; Env map[string]string; Tool Tool; StopActive bool }`, `type Tool struct { Name string; Kind ToolKind; Path, Command string; ExitCode *int; OutputBytes int64; RawInput, RawOutput json.RawMessage }`.
  - `type Reply struct { Context []string; Block *Decision; HoldTurn *Decision; Raw *adapter.HookReply }`, `type Decision struct{ Reason string }`.
  - `type Driver interface { Name() string; Translate(event string, payload []byte) (Event, error); Render(ev Event, r Reply) (adapter.HookReply, error) }`.
  - Capacidades opcionais: `type Router interface { Route(c *Core, ev Event) (domain.Session, bool /*handled*/, error) }` (Claude: janelas duplas/sessão filha); `type Ingester interface { Ingest(sess domain.Session, ev Event) }` (leitura do histórico depois do hook).
  - `func New(st *store.Store, deps adapter.Deps, d Driver) *Core`; `func (c *Core) Handle(ctx context.Context, h adapter.HookCall) (adapter.HookReply, error)` = Translate → sessão → regras → Render. Métodos exportados que o `Router` do Claude precisa: `CreateSession`, `Reopen`, `Touch`, `FollowMain`, `Locate`.
- Move para o núcleo (comportamento idêntico): `session`/`createSession`/reabrir/tocar, `followMain`/`moveSession`/`followBranch`/`syncCardBranch`/`locate`/`projectRoot`/`projectRelative`, `event`, eventos de prompt/arquivo/comando do `postToolUse`, `sessionEnd`/`endReasons`, `errIgnore`. Ficam no Claude: `payload`, `route`/`createChild`/`ownedElsewhere`/`ingestID` (como `Router`), leitura do transcript (como `Ingester`), `mainOnlyEvents` vira `Event.SubagentID != ""` + `Kind` no núcleo.

- [ ] Testes do núcleo com um driver falso: Started cria sessão `agente:<id>` com local e projeto; Prompt grava `EvPrompt` só com captura completa; ToolAfter com `ToolEdit`/`ToolRead` grava evento com caminho relativo ao projeto e `ToolCommand` grava comando e `exit_code`; Ended encerra com o motivo traduzido e nunca cria sessão (sessão inexistente → `errIgnore`, resposta vazia).
- [ ] `claude.Adapter.HandleHook` vira `core.Handle`; `translate.go` mapeia `Read`→leitura, `Edit/Write/MultiEdit/NotebookEdit`→edição, `Bash`→comando; `render.go` mantém exatamente os JSON atuais.
- [ ] `go test ./internal/adapter/claude/ ./internal/agentcore/ ./cmd/nobre/` passa sem mudar asserções; commit `refactor: nucleo de sessoes fora do adaptador do claude`.

### Task 3: Núcleo — respostas (início de sessão, avisos, limites, fim de turno)

**Files:** Create `internal/agentcore/start.go`, `notice.go`, `alerts.go`, `limits.go`, `stop.go`, `nobrecmd.go` (+ testes, movendo os testes correspondentes do Claude quando testam regra e não formato); Modify/Delete `internal/adapter/claude/context.go`, `notice.go`, `alerts.go`, `nobrecmd.go`.

**Interfaces:**
- Consumes: Task 2.
- Produces: capacidades opcionais do driver: `type Memories interface { MemoriesBlock(sess domain.Session, set domain.Settings, room int) string }` (Claude); `type SpendUnit interface { SpendsCredits() bool }` (Kiro devolve true → limites em US$ não bloqueiam). O aviso de contexto escolhe o texto (tokens ou %) pelo `ContextLive.PctOnly` da Task 5; até a Task 5 existir, só o texto atual.
- Move para o núcleo: montagem do contexto do `SessionStart` (instruções, cards pausados, pacote de retomada, apps, não cadastrado), aviso de configuração, avisos de contexto e de saída grande (com `session_alerts`), bloqueio por limite no `ToolBefore` (`Reply.Block`), cobrança de handoff e lembrete sem card no `TurnEnd` (`Reply.HoldTurn`), detecção de comando `nobre` simples.
- Texto das instruções sem nome de agente (já é: confirmar; "skill" só no texto que o Claude instala).

- [ ] Testes do núcleo: limite em US$ estourado bloqueia `ToolBefore` de driver sem `SpendUnit` e não bloqueia de driver com `SpendsCredits()==true`; `TurnEnd` com arquivos editados depois da última nota e `IncompleteAction=="ask_agent"` devolve `HoldTurn`; avisos de contexto usam o texto em % quando o medidor é `PctOnly`.
- [ ] Suíte do Claude passa sem mudar asserções; commit `refactor: avisos, limites e fim de turno no nucleo`.

### Task 4: Teste de contrato

**Files:** Create `internal/agentcore/contracttest/contract.go` (bateria exportada) ; Create `internal/adapter/claude/contract_test.go`.

**Interfaces:**
- Produces: `contracttest.Fixtures struct { Start, Prompt, ReadAfter, EditAfter, CommandAfter, Stop, End []byte; ReadPath, EditPath, Command string; DecodeContext func(adapter.HookReply) []string; DecodeHold func(adapter.HookReply) (string, bool) }`; `func Run(t *testing.T, newDriver func(st *store.Store, deps adapter.Deps) agentcore.Driver, fx Fixtures)`.
- Cenários: sessão nasce no `Start`; `NOBRE_RESUME=<card>` liga ao card e o contexto traz o pacote de retomada; prompt registrado; leitura/edição/comando viram eventos com caminho relativo; `Stop` com handoff pendente segura o turno (ou, se `DecodeHold` é nil, o texto aparece no próximo prompt); `End` encerra.
- [ ] Rodar contra o Claude com fixtures de `internal/adapter/claude/testdata`; passa. Commit `test: contrato dos drivers de agente`.

### Task 5: Gasto em créditos e contexto em % nos dados

**Files:** Create `internal/store/migrations/019_agent_credits.sql`; Modify `internal/domain/types.go` (`UsageRow.Credits float64`), `internal/store/usage.go` (`UpsertUsage`, `Totals`/`Total` ganham `Credits`), `internal/store/context.go` + `internal/usage/context.go` (`ContextTurn.Pct *float64`), `internal/store/context_live.go` (`ContextLive.PctOnly bool`), `internal/store/store_test.go` (user_version 19).

- Migração: `ALTER TABLE usage ADD COLUMN credits REAL NOT NULL DEFAULT 0`; `ALTER TABLE context_turns ADD COLUMN pct REAL` (NULL = sem %).
- `SessionContext`: turnos com `pct` não nulo e `tokens = 0` → `Pct` = último `pct`, `PctOnly = true`, `WindowTokens = 0`, crescimento e turnos até o limite em pontos percentuais (mesma regra de 95%), sem cache/TTL.
- [ ] Testes: linha com créditos soma em `Total.Credits` e não altera `Total.CostUSD` nem liga `Total.Unpriced` (linha do Kiro tem tokens 0 e modelo sem preço); totais mistos Claude+Kiro no mesmo card; medidor `PctOnly` com 3 turnos (10%, 14%, 18%) → `GrowthPerTurn` 4 (pp) e `TurnsToLimit` 19; `ContextTotals` (Analytics) ignora turnos só com %.
- [ ] Commit `feat(store): creditos e contexto em porcentagem`.

### Task 6: Driver do Kiro — hooks

**Files:** Create `internal/adapter/kiro/kiro.go`, `translate.go`, `render.go` (+ testes), `internal/adapter/kiro/testdata/*.json` (payloads do teste de 2026-11-23: `session_start`, `user_prompt_submit`, `pre_tool_use_{read,bash,write}`, `post_tool_use_{read,bash,write}`, `stop`, `session_end`), `internal/adapter/kiro/contract_test.go`; Modify `internal/agents/agents.go` (registra o Kiro), `cmd/nobred/main.go`.

**Interfaces:**
- Produces: `kiro.New(st *store.Store, deps adapter.Deps) *Adapter` (implementa `adapter.Adapter`, `agentcore.Driver`, `agentcore.SpendUnit`); `Name()=="kiro"`, `ProcessNames()==[]string{"kiro-cli"}`, `SessionEnv() string == "KIRO_SESSION_ID"`.
- Tradução: `SessionStart`→Started (`Source` "startup"), `UserPromptSubmit`→Prompt, `PreToolUse`/`PostToolUse`→ToolBefore/ToolAfter, `Stop`→TurnEnd, `SessionEnd`→Ended (`reason`), `PostCompact`→Compacted. Ferramentas: `read_file`→leitura (`tool_input.path`); `fs_write`, `str_replace`, `fs_append`, `delete_file`→edição (`path`); `execute_bash`, `execute_pwsh`, `run_command`→comando (`command`; `exit_code` lido de `Exit Code: N` no fim de `tool_response`); o resto→outro. `OutputBytes` = `len(tool_response)`.
- Render: `Context` → textos juntos por `\n\n` no `Stdout`, `Exit` 0; `Block` → `Exit` 2, motivo no `Stderr`; `HoldTurn` → `Stdout` `{"decision":"block","reason":...}` (primeiro passo verifica ao vivo; se o Kiro não seguir, `Render` devolve vazio e o núcleo guarda o texto para o próximo `Prompt` via `session_alerts`/marca, e `DecodeHold` fica nil no contrato).
- [ ] Verificar ao vivo (pasta temporária, `kiro-cli chat --v3`): um hook de `Stop` com `{"decision":"block","reason":"x"}` faz o agente continuar? Registrar o resultado no card e escolher o caminho do `HoldTurn`.
- [ ] Testes: tradução de cada fixture; Review Focus de ferramentas; `SessionEnd` com AgentPID 0 encerra `kiro:<id>` e com sessão inexistente não cria; contrato `contracttest.Run` passa.
- [ ] Commit `feat(kiro): driver de hooks do kiro-cli`.

### Task 7: Kiro — instalação, saúde e lançamento

**Files:** Create `internal/adapter/kiro/install.go`, `detect.go`, `steering.md` (embed) (+ testes); Modify `cmd/nobre/setup.go` (instala/remove o Kiro quando o `kiro-cli` existe), `internal/integration/monitor.go` só se precisar vigiar os dois arquivos (via `WatchedFiles`).

**Interfaces:**
- Produces: `Install()`, `PlanInstall()`, `Uninstall(dry bool) (adapter.InstallReport, error)`, `Detect()`/`Health()` com checks `binary` (encontrado e versão ≥ 3.1, `Code: "outdated"` abaixo), `hooks` (arquivo com os 7 eventos apontando para o `nobre` atual), `steering` (arquivo presente e igual ao embutido), `last_event`; `WatchedFiles()`; `Binary()` (como o Claude: `PATH`, depois `~/.local/bin/kiro-cli`); `LaunchCommand(bin, prompt) = []string{bin, "chat", "--v3", prompt}`.
- `steering.md`: front matter `inclusion: always`; conteúdo = as instruções da skill do Claude (`internal/adapter/claude/skill.md`) sem o que é específico do Claude.
- [ ] Testes com HOME temporário: instalar escreve os dois arquivos (JSON `{"version":"v1","hooks":[...]}` com 7 entradas), reinstalar sem mudança não altera nada, remover apaga só os dois, saúde acusa cada falta; `LaunchCommand` exato.
- [ ] Verificar ao vivo numa pasta não confiada: os hooks globais disparam? Se não, check `hooks` com `Detail` explicando e registrar no card.
- [ ] Commit `feat(kiro): instalacao, saude e lancamento`.

### Task 8: Kiro — leitor de uso e contexto

**Files:** Create `internal/adapter/kiro/ingest.go` (+ testes, `testdata/messages.jsonl` sintético com `usage_summary`, `session_metadata/contextUsage`, `tool_result`).

**Interfaces:**
- Consumes: Task 5 (`UsageRow.Credits`, `ContextTurn.Pct`), Task 2 (`agentcore.Ingester`).
- `Ingest(sess, ev)`: assíncrono como o do Claude (mesmo padrão de `wg`/mutex por sessão); acha `~/.kiro/sessions/*/<session_id>/messages.jsonl` (glob; guarda o caminho em `sessions.transcript_path`); lê do cursor (`FileOffset`/`SetFileOffset`). `usage_summary` → `UsageRow{MessageID: turnId (ou executionId), Credits: soma de promptTurnSummaries[].usage onde unit=="credit", Model: session.json modelId}`; último `contextUsage.usagePercentage` de cada `executionId` → `ContextTurn{Pct}`; `tool_result.content` → `ToolOutput` (tamanho estimado como no Claude).
- [ ] Testes: créditos de dois turnos; releitura não duplica; arquivo inexistente → nada, sem erro no log; linha parcial no fim não avança o cursor.
- [ ] Commit `feat(kiro): creditos e contexto lidos do historico`.

### Task 9: Sessão achada pela variável do agente

**Files:** Modify `internal/client/client.go` (cabeçalho `X-Nobre-Agent-Session` com `<agente>:<valor>` da primeira variável de sessão presente entre os drivers), `internal/api/me.go` (`withSession`: PID primeiro; se não achar, pelo cabeçalho), testes.

- [ ] Testes: com PID desconhecido e cabeçalho `kiro:sess_x` de sessão viva → acha; cabeçalho de sessão encerrada → 404 atual; sem os dois → 400 atual.
- [ ] Commit `feat: comandos nobre acham a sessao pela variavel do agente`.

### Task 10: Telas

**Files:** Modify `web/src/api/types.ts` (`credits` nos totais/sessões, `pct_only` no medidor), `web/src/pages/context/ContextMeter.tsx`, `web/src/pages/ControlRoom.tsx`, `web/src/pages/CardPage.tsx`, `web/src/pages/Analytics.tsx`, `web/src/pages/Projects.tsx`/`ProjectPage.tsx`, `web/src/pages/NewTask.tsx`, `web/src/pages/card/ContinuePanel.tsx`, `web/src/ui/agents.ts` (+ testes); `internal/api` onde as respostas de custo são montadas (incluir `credits`).

- Onde há custo em US$, quando `credits > 0` aparece "N créditos" ao lado (formato pt-BR, 2 casas); nunca somado.
- Medidor `pct_only`: mostra só a %, "janela informada pelo agente", sem tokens/cache/TTL.
- Configuração: o cartão do agente usa `installable`/`status_line` vindos de `/api/agents` (Task 1 acrescenta esses dois booleanos ao `agentInfo`: driver registrado / implementa `adapter.StatusLiner`), no lugar de `info.agent === 'claude'` e de `AGENT_IDS` fixo.
- Nova tarefa: seletor de agente entre os instalados (`/api/agents` com `found`); padrão = primeiro instalado. "Continuar": seletor mantido, padrão = agente da última sessão do card.
- [ ] Testes Vitest: card com US$ e créditos mostra os dois; medidor `pct_only`; seletores com padrão certo; `npx tsc --noEmit -p .` limpo.
- [ ] Commit `feat(web): kiro nas telas, creditos e seletor de agente`.

### Task 11: Conferir ao vivo

- [ ] `go test ./...`, `go test -race ./internal/agentcore/ ./internal/adapter/...`, `cd web && npx vitest run && npx tsc --noEmit -p .`.
- [ ] Depois do merge e do `make update`: `nobre setup` instala o Kiro; numa pasta de projeto cadastrado, `kiro-cli chat --v3`, `nobre start "teste kiro"` pelo agente, uma leitura, uma edição, um comando, `/quit`. Conferir no board: sessão `kiro:` ligada ao card, eventos, créditos no card, medidor em %, sessão encerrada. Abrir o mesmo card pelo "Continuar" com Kiro. Repetir um prompt no Claude para confirmar que nada mudou.
- [ ] Registrar o resultado no card (NAC-42) e apagar o card de teste.
