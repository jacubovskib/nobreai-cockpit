# Terminais: medidores e "Limpar e continuar" — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mostrar na grade de `/terminals` o contexto de cada sessão (por tile) e o uso do plano (uma faixa sob o cabeçalho), e permitir "Limpar e continuar" de dentro do tile, com o agente novo abrindo na própria grade.

**Architecture:** Lógica pura em módulos com teste (casamento terminal → sessão por pid, limite e nível do contexto, textos da faixa do plano) e componentes por cima. A página casa terminais e sessões e entrega a cada tile memoizado só valores primitivos. A lógica do recomeço sai do `RestartCleanDialog` para um hook usado pela página do card (sem mudança) e pelo painel do tile. No daemon, `restart-clean` passa a aceitar `mode`.

**Tech Stack:** Go (daemon), React 18 + TypeScript, `@tanstack/react-query`, vitest + Testing Library. Nenhuma dependência nova.

**Spec:** `docs/superpowers/specs/2026-11-26-terminais-medidores-design.md`. Mock: https://claude.ai/artifact/RsgZfBpZQjhPz6kSDJhuJM. Plano da entrega 1 (formato e convenções): `docs/superpowers/plans/2026-11-26-terminais-grade.md`.

## Divergências da spec

1. **De onde vêm as sessões.** A spec fala em "a lista de sessões" (linha 44). `GET /api/sessions` não serve: devolve `card_id` numérico, sem a chave nem o "Onde parei" do card (`web/src/api/types.ts:43-68`, `:208`), e só as 50 mais recentes, encerradas inclusive (`internal/api/usage.go:132-144`, `internal/store/sessions.go:274-276`). A chave e o "Onde parei" até dariam para juntar pela lista de cards que a página já carrega (`web/src/pages/Terminals.tsx:139`), mas o corte em 50 não: uma sessão viva antiga fica de fora e o tile dela perderia o medidor. `GET /api/overview` devolve em `active` todas as sessões não encerradas, com `pid`, `card` (chave, `where_stopped`, `where_stopped_at`) e `effective_project_id` (`internal/api/overview.go:169`, `:192-228`; `web/src/api/types.ts:260-267`); é a fonte que o `terminalFor` da Sala de Controle já usa (`web/src/pages/ControlRoom.tsx:292`, `:396`), também é invalidada pelo evento `sessions` (`web/src/api/useStream.ts:10`), e a spec já manda usar `useOverview` para os créditos do Kiro. **Planejado:** a grade lê `useOverview().data.active`. Não acrescenta pedido: a faixa do plano da própria spec (linha 79) já faz a tela observar a overview, que passa a ser refeita a cada evento `sessions` (com o debounce de 250 ms) enquanto a tela Terminais está aberta.
2. **"Nada toca o xterm" × faixa acima do terminal.** A spec põe a faixa de contexto e a linha de aviso entre o cabeçalho e o terminal (linhas 61-63) e diz que os medidores não tocam o xterm (linha 94). O corpo do tile é `flex: 1` (`web/src/pages/terminals/GridTile.tsx:188`): quando a faixa ou a linha aparece ou some, a altura do terminal muda e o `ResizeObserver` do `TerminalView` roda (`web/src/ui/TerminalView.tsx:369`, `:373`). Só assistindo (o padrão: todo tile abre assim) isso só reajusta a fonte; em "Interagir" vira `term.fit()` e um resize ao PTY (`TerminalView.tsx:242-245`, `:261-264`), sem gesto do usuário, o que a entrega 1 proibia. **Planejado:** faixa e linha no fluxo, como no mock, aceitando um resize ao PTY (com o debounce existente) quando elas aparecem ou somem num tile em "Interagir". A alternativa (sobrepor a linha ao topo do terminal) esconde saída do agente. Fica na conferência manual da Task 8.
3. **"Encerrar este terminal" com o tile em pausa.** A spec diz que o botão "pede a confirmação de encerrar já existente" (linha 88). Essa confirmação só existe com o tile ligado (`GridTile.tsx:181`) e o encerramento passa pelo handle do terminal montado (`TerminalView.tsx:398-414`); tile em pausa não tem terminal montado. **Planejado:** com o tile ligado o botão abre a confirmação existente; em pausa aparece só o texto "Contexto limpo. O agente novo abriu em outro terminal." (quem quiser encerrar liga o tile).

## Global Constraints

- Textos da interface em português, no tom das telas atuais. Comentários de código em português.
- Sem dependência nova. Sem consulta nova: reusa `useOverview`, `useContextLive`, `useSettings`, `usePlanLimits(24)`.
- Limite do contexto: a última faixa de `settings.context_warn_pcts`; lista vazia ou ausente → `80`. Crítico: `90`, fixo. Verde abaixo do limite, âmbar a partir do limite, vermelho a partir de `max(90, limite)`.
- Plano: âmbar a partir de `warn_pct`, vermelho a partir de `90`. Janela vencida (campo `expired` ou `resets_at` já passado no relógio do cliente) mostra "sem leitura recente", nunca o número.
- A página entrega a cada tile só valores primitivos e objetos estáveis; um evento `sessions` que não muda os valores de um tile não o redesenha.
- Props novas do `GridTile` são todas opcionais; ausentes = o tile da entrega 1 (os testes atuais de `GridTile.test.tsx` passam sem mudança).
- "Limpar e continuar" nunca roda sem o painel de confirmação; nada é encerrado sozinho. A grade pede o recomeço com `mode: "background"`.
- Sala de Controle e página do card não mudam de comportamento: o `terminalFor` mantém o atalho por `card_key`; o `RestartCleanDialog` mantém textos, corpo do POST (sem `mode`) e invalidações.
- `ContextMeter` (`web/src/pages/context/ContextMeter.tsx`) não muda.
- Commits no padrão do repositório, `feat(web): … (NAC-63)` / `feat(api): … (NAC-63)`, terminando com a linha `Co-Authored-By: Claude Fable 5.2 <noreply@anthropic.com>`.
- Rodar antes de cada commit: `cd web && npx vitest run <arquivos da tarefa> && npx tsc --noEmit`; para Go, `go test ./internal/api/...`.

## Review Focus

1. Recomeço que falha depois de desligar o card (limite de 20 terminais, falha ao abrir): a sessão já ficou sem card, mas o painel continua aberto com o erro do servidor, em vez de sumir com a ação. Teste na Task 7.
2. Respostas ausentes ou malformadas de `/api/overview`, `/api/context/live`, `/api/settings` e `/api/plan-limits` (erro 500, lista no lugar de objeto): a grade segue utilizável, sem medidor e sem faixa, sem lançar. Testes nas Tasks 4 e 8.
3. Sessão com `pid` 0, sessão com `pid_dead_at`, terminal encerrado: nunca casam, mesmo com pid igual. Teste na Task 2.
4. Percentual acima de 100, janela 0 (`pct_only`) e sessão sem turno medido (tokens 0 e pct 0): barra travada em 100% com o número como veio; sem tokens no texto; sem medidor. Testes nas Tasks 3 e 6.
5. `mode` inválido ou `background` sem gerenciador de terminais no `restart-clean`: recusa antes de desligar o card da sessão. Teste na Task 1.
6. Sessão do tile troca (`/clear`) com o painel aberto: o painel segue com a sessão em que abriu e mostra o erro do servidor se ela não tiver mais o card. Teste na Task 7.

---

### Task 1: `mode` no `restart-clean` (daemon)

**Files:**
- Modify: `internal/api/restart_clean.go` (struct de entrada e a chamada de `prepareContinue`, linhas 25-27 e 68; comentário da função)
- Test: `internal/api/restart_clean_test.go`

**Interfaces:**
- Consumes: `continueInput{Agent, Mode}`, `prepareContinue`, `runContinue` de `internal/api/continue.go` (sem mudança neles: `Mode` vazio já cai em `settings.launch_mode`, linhas 168-176).
- Produces: `POST /api/sessions/{id}/restart-clean` aceita `{"where_stopped": string, "mode": string}`. `mode: "background"` abre no gerenciador de terminais e a resposta traz `terminal_id`; `mode` ausente ou `""` = comportamento de hoje. Consumido pela Task 5 (`useRestartClean` com `mode: 'background'`).

- [ ] **Step 1: Write the failing tests** em `restart_clean_test.go`, no molde de `TestContinueBackgroundStartsTerminal` (`continue_test.go:364`): `pty.NewManager(pty.Options{})`, `continueAPIWith(t, g, mgr, bgClaude(t))`, projeto em `realTempDir(t)`, card `NAC-2` nele, `a.liveSession(t, "claude:s", 4242, "NAC-2")`.
  - `TestRestartCleanModeBackgroundOpensOnPlatform`: com o padrão das configurações em `terminal`, `POST …/restart-clean {"where_stopped":"aqui","mode":"background"}` → 200, `terminal_id` não vazio, `mgr.Get(id)` acha o terminal, `len(l.calls) == 0`, a sessão ficou sem card e viva, o card em `domain.Pausado`.
  - `TestRestartCleanInvalidModeChangesNothing`: `{"where_stopped":"aqui","mode":"xyz"}` → 400 com `"modo ainda não disponível no piloto"`; com `continueAPI` (sem gerenciador) e `"mode":"background"` → 503; nos dois a sessão continua com o card, a coluna não muda e `len(l.calls) == 0`.
- [ ] **Step 2:** Run `go test ./internal/api/ -run RestartClean -v`. Expected: FAIL nos dois testes novos (o primeiro abre pelo lançador: `terminal_id` vazio e `len(l.calls) == 1`; o segundo responde 200).
- [ ] **Step 3:** Implementar: campo `Mode string \`json:"mode"\`` na struct de entrada e `continueInput{Agent: sess.Agent, Mode: in.Mode}` na chamada de `prepareContinue` (que roda antes de `DetachSessionCard`, então modo inválido não desliga nada). Atualizar o comentário da função: `{where_stopped, mode}`.
- [ ] **Step 4:** Run `go test ./internal/api/...`. Expected: PASS, inclusive os testes antigos de `restart_clean_test.go` (sem `mode`, um lançamento pelo lançador).
- [ ] **Step 5:** Commit `feat(api): restart-clean aceita mode e abre na plataforma (NAC-63)`.

### Task 2: Casamento terminal → sessão (lógica pura) e Sala de Controle

**Files:**
- Create: `web/src/pages/terminals/sessionMatch.ts`
- Modify: `web/src/pages/ControlRoom.tsx:290-298` (`terminalFor`)
- Test: `web/src/pages/terminals/sessionMatch.test.ts`; guarda existente: `web/src/pages/ControlRoom.test.tsx:98` ("Ver terminal casa a sessão pelo pid e, sem pid igual, pelo card")

**Interfaces:**
- Consumes: `TerminalInfo`, `isLive` de `web/src/api/terminals.ts`.
- Produces (consumido pela Task 8 e pelo `terminalFor`):

```ts
export interface SessionRef { id: string; pid: number; started_at: string; ended_at: string | null; pid_dead_at: string | null }
export const sessionAlive: (s: Pick<SessionRef, 'ended_at' | 'pid_dead_at'>) => boolean   // sem ended_at e sem pid_dead_at
export const samePid: (terminalPid: number, sessionPid: number) => boolean                 // os dois > 0 e iguais
// Sala de Controle: o terminal (já filtrado por isLive) de mesmo pid da sessão.
export function terminalOfSession(s: Pick<SessionRef, 'pid'>, liveTerminals: readonly TerminalInfo[]): TerminalInfo | undefined
// Grade: a sessão viva de mesmo pid do terminal vivo; havendo mais de uma, a de started_at mais recente (empate: a última da lista — decisão do plano). Sem atalho por card.
export function sessionOfTerminal<S extends SessionRef>(t: TerminalInfo, sessions: readonly S[]): S | undefined
export function matchSessions<S extends SessionRef>(terms: readonly TerminalInfo[], sessions: readonly S[]): Map<string, S>   // id do terminal → sessão; só os casados
```

- [ ] **Step 1: Write the failing tests** em `sessionMatch.test.ts`, um `it` cada: pid igual casa; sessão com `ended_at` é ignorada; sessão com `pid_dead_at` é ignorada; terminal encerrado (`ended_at`) não casa; duas sessões vivas no mesmo pid → a de `started_at` mais recente; pid 0 na sessão ou no terminal nunca casa; **sem atalho por card**: terminal `{pid: 999, card_key: 'NAC-5'}` e sessão `{pid: 200}` do card NAC-5 → `undefined`; `matchSessions` devolve só os terminais casados; `terminalOfSession({pid: 100}, [t(pid 100), t(pid 300)])` devolve o de pid 100 e `terminalOfSession({pid: 0}, …)` devolve `undefined`.
- [ ] **Step 2:** Run `cd web && npx vitest run src/pages/terminals/sessionMatch.test.ts`. Expected: FAIL (módulo não existe).
- [ ] **Step 3:** Implementar `sessionMatch.ts` com as assinaturas acima.
- [ ] **Step 4:** Em `ControlRoom.tsx`, `terminalFor` passa a obter `own` com `terminalOfSession(s, live)`; o resto fica igual (o atalho `forCard(live, s.card.key).find((t) => !sessionPids.has(t.pid))` continua). Nenhum teste da Sala de Controle muda.
- [ ] **Step 5:** Run `npx vitest run src/pages/terminals/sessionMatch.test.ts src/pages/ControlRoom.test.tsx && npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 6:** Commit `feat(web): casamento terminal → sessão por pid, compartilhado com a Sala de Controle (NAC-63)`.

### Task 3: Limite, nível e textos do contexto (lógica pura)

**Files:**
- Create: `web/src/pages/terminals/contextLevel.ts`
- Test: `web/src/pages/terminals/contextLevel.test.ts`

**Interfaces:**
- Consumes: `formatTokensPt` de `web/src/pages/context/format.ts`; `LiveContextSummary`, `ActiveSession` de `web/src/api/types.ts`; `TerminalInfo`.
- Produces (consumido pelas Tasks 6, 7 e 8):

```ts
export const CTX_CRIT = 90, CTX_DEFAULT_LIMIT = 80
export type CtxLevel = 'ok' | 'warn' | 'crit'
export const CTX_COLOR: Record<CtxLevel, string>   // ok: 'var(--ok)', warn: 'var(--warn)', crit: 'var(--err)'
export function ctxLimit(warnPcts: readonly number[] | undefined): number   // último item; vazio, ausente ou não-lista → 80
export function ctxLevel(pct: number, limit: number): CtxLevel               // crit: pct >= max(90, limit); warn: pct >= limit
export function ctxDetail(tokens: number, windowTokens: number, pctOnly: boolean): string
export function ctxWarning(pct: number, limit: number, simple: boolean): string
// Valores do tile, todos primitivos. projectId serve só ao place (Task 8); não vai ao tile.
export interface TileMeter {
  sessionId: string            // '' = sem sessão casada
  cardKey: string              // sessão casada: o card dela ('' se não tem); sem sessão: t.card_key
  projectId: number | null     // effective_project_id da sessão casada; null sem sessão
  ctxPct: number | null        // Math.round(pct); null = sem dado
  ctxTokens: number; ctxWindow: number; ctxPctOnly: boolean
  whereStopped: string; whereStoppedAt: string | null
}
export function tileMeter(t: TerminalInfo, s: Pick<ActiveSession, 'id' | 'card' | 'effective_project_id'> | undefined, c: LiveContextSummary | undefined): TileMeter
```

- Formato dos números (decisão do plano): `formatTokensPt`, o mesmo do `ContextMeter` ("124 mil", "1,2 milhão"), no lugar do "124k" do mock.
- "Sem turno medido" (decisão do plano, a partir de `internal/store/context_live.go:146-168`: sem turnos o daemon devolve tokens 0 e pct 0): `c` ausente, ou `c.tokens === 0 && c.pct === 0` → `ctxPct: null`.
- O nível usa o percentual arredondado, o mesmo número que o tile mostra (decisão do plano).

- [ ] **Step 1: Write the failing tests** em `contextLevel.test.ts`:
  - `ctxLimit([60, 80])` → `80`; `ctxLimit([70])` → `70`; `ctxLimit([])`, `ctxLimit(undefined)` e `ctxLimit('x' as never)` → `80`.
  - `ctxLevel(79, 80)` → `'ok'`; `(80, 80)` → `'warn'`; `(89, 80)` → `'warn'`; `(90, 80)` → `'crit'`; limite acima de 90: `(92, 95)` → `'ok'`, `(95, 95)` → `'crit'`; `(90, 90)` → `'crit'`.
  - `ctxDetail(124000, 200000, false)` → `'124 mil de 200 mil'`; `ctxDetail(0, 0, true)` → `'o agente informa só a %'`.
  - `ctxWarning(84, 80, false)` → `'Contexto em 84%, acima do limite de 80%. Limpe e continue de onde parou.'`; `ctxWarning(84, 80, true)` → `'Contexto em 84%. Limpe e continue de onde parou.'`.
  - `tileMeter`: sem sessão → `{ sessionId: '', cardKey: t.card_key, projectId: null, ctxPct: null, … }`; sessão com card `NAC-9` e contexto `{pct: 61.6, tokens: 124000, window_tokens: 200000}` → `sessionId`, `cardKey: 'NAC-9'`, `projectId` = `effective_project_id`, `ctxPct: 62`, `whereStopped`/`whereStoppedAt` do card; sessão **sem card** num terminal com `card_key: 'NAC-1'` → `cardKey: ''`; contexto `{tokens: 0, pct: 0}` → `ctxPct: null`; `pct_only` com `pct: 62` → `ctxPct: 62`, `ctxPctOnly: true`; `pct: 104.2` → `ctxPct: 104`.
- [ ] **Step 2:** Run `npx vitest run src/pages/terminals/contextLevel.test.ts`. Expected: FAIL (módulo não existe).
- [ ] **Step 3:** Implementar `contextLevel.ts`.
- [ ] **Step 4:** Run o mesmo comando e `npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(web): limite, nível e textos do contexto do tile (NAC-63)`.

### Task 4: Faixa do plano

**Files:**
- Create: `web/src/pages/terminals/planStrip.ts`, `web/src/pages/terminals/PlanStrip.tsx`
- Test: `web/src/pages/terminals/planStrip.test.ts`, `web/src/pages/terminals/PlanStrip.test.tsx`

**Interfaces:**
- Consumes: `duration` de `web/src/pages/PlanLimits.tsx`; `PlanLimits`, `PlanWindowState`, `AgentSpend` de `web/src/api/types.ts`; `usePlanLimits`, `useOverview` de `web/src/api/queries.ts`; `agentInfo` de `web/src/ui/agents`.
- Produces, em `planStrip.ts`:

```ts
export const PLAN_CRIT = 90
export type PlanTone = 'ok' | 'warn' | 'crit' | 'stale'
export interface PlanWindowView {
  key: 'five_hour' | 'seven_day'
  label: string     // 'Sessão de 5 h' | 'Semana'
  short: string     // '5 h' | 'sem'
  pct: number | null // null = vencida
  value: string     // '62%' | 'sem leitura recente'
  reset: string     // 'reinicia em 2 h 14 min (17:30)' | 'reinicia em 2 d 3 h (sáb 09:00)' | 'sem horário de reinício' | '' (vencida)
  tone: PlanTone
}
export function resetShort(iso: string, now: number, weekday: boolean): string
export function planWindow(key: 'five_hour' | 'seven_day', w: PlanWindowState | null | undefined, warnPct: number, now: number): PlanWindowView | null   // null = sem dado
export function claudeWindows(p: PlanLimits | undefined, now: number): PlanWindowView[]   // [] = sem dado algum: nem cartão nem chip
export function claudeChip(ws: PlanWindowView[]): string                                   // 'Claude 5 h 62% · sem 41%'
export function worstTone(ws: PlanWindowView[]): PlanTone                                  // crit > warn > stale > ok
export function kiroText(perAgent: readonly AgentSpend[] | undefined, hasKiro: boolean): string | null   // '12,3 créditos hoje'
```

- Produces, em `PlanStrip.tsx` (consumido pela Task 8): `export const PlanStrip: React.NamedExoticComponent<{ simple: boolean; hasKiro: boolean }>` (`React.memo`). O relógio de 30 s (`now`) vive dentro do componente, como em `PlanLimits.tsx:97-101`, para não redesenhar a página.
- Decisões do plano: o texto de reinício segue a spec ("(17:30)", "(sáb 09:00)"), sem o "às" do painel da Sala de Controle; com uma janela sem dado e a outra com dado, aparece só a que tem dado; créditos com no máximo 1 casa decimal (`toLocaleString('pt-BR', { maximumFractionDigits: 1 })`); "terminal Kiro na grade" = qualquer terminal da lista com `agent === 'kiro'`.

- [ ] **Step 1: Write the failing tests** em `planStrip.test.ts`, com `now` fixo (`new Date(2026, 10, 26, 15, 16).getTime()`, hora local):
  - normal: janela `{used_pct: 62, resets_at: 17:30 de hoje, expired: false}`, `warnPct: 80` → `value: '62%'`, `tone: 'ok'`, `reset: 'reinicia em 2 h 14 min (17:30)'`; semanal com reinício no sábado 28 às 09:00 a partir de `new Date(2026, 10, 26, 6, 0)` → `'reinicia em 2 d 3 h (sáb 09:00)'`.
  - acima do aviso: `used_pct: 85` → `tone: 'warn'`; `used_pct: 90` → `'crit'`; `used_pct: 93` com `warnPct: 95` → `'crit'`.
  - vencida: `expired: true` → `pct: null`, `value: 'sem leitura recente'`, `reset: ''`, `tone: 'stale'`; `expired: false` com `resets_at` um minuto antes de `now` → o mesmo.
  - `resets_at: null` → `reset: 'sem horário de reinício'`.
  - sem dado: `planWindow('five_hour', null, 80, now)` → `null`; `claudeWindows(undefined, now)` e `claudeWindows({five_hour: null, seven_day: null, …}, now)` → `[]`; `claudeWindows([] as never, now)` → `[]` (resposta malformada).
  - `claudeChip` das duas janelas (62 e 41) → `'Claude 5 h 62% · sem 41%'`; com a de 5 h vencida → `'Claude 5 h sem leitura recente · sem 41%'`.
  - Kiro: `kiroText([{agent: 'kiro', credits: 12.34, …}], true)` → `'12,3 créditos hoje'`; `hasKiro: false` → `null`; `credits: 0` → `null`; `kiroText(undefined, true)` → `null`.
- [ ] **Step 2:** Run `npx vitest run src/pages/terminals/planStrip.test.ts`. Expected: FAIL (módulo não existe).
- [ ] **Step 3:** Implementar `planStrip.ts`.
- [ ] **Step 4: Write the failing tests** em `PlanStrip.test.tsx` (fetch simulado por rota, como o `mock` de `web/src/pages/context/context.test.tsx:9-22`; `QueryClientProvider`):
  - Detalhado: `<section aria-label="Uso do plano">` com o cartão "Claude" contendo `role="meter"` de nome `Sessão de 5 h: 62% usado` e `Semana: 41% usado`, e os textos de reinício; o `meter` de 62% tem `data-tone="ok"`, o de 85% com `warn_pct: 80` tem `data-tone="warn"` e o de 92% `data-tone="crit"`.
  - Simples: os chips `Claude 5 h 62% · sem 41%` (com `data-tone` igual ao pior tom das janelas) e, com `hasKiro` e créditos 12.34, `Kiro 12,3 créditos hoje`; nenhum `role="meter"`.
  - janela vencida mostra "sem leitura recente" e nenhum percentual dela.
  - sem dado do Claude e sem Kiro: o contêiner fica vazio (`container.firstChild` é `null`).
  - `/api/plan-limits?hours=24` e `/api/overview` respondendo 500, ou uma lista: nada renderizado, nenhum erro lançado.
- [ ] **Step 5:** Run `npx vitest run src/pages/terminals/PlanStrip.test.tsx`. Expected: FAIL (componente não existe).
- [ ] **Step 6:** Implementar `PlanStrip.tsx`: `usePlanLimits(24)` (a mesma chave da Sala de Controle) e `useOverview()`; cartão "Claude" (só com `claudeWindows(...).length > 0`) e cartão "Kiro" (só com `kiroText(...) !== null`); barras de 6 px no Detalhado; cada `meter` e cada chip leva `data-tone` (`ok|warn|crit|stale`), que é o que os testes conferem, e a cor correspondente (`var(--ok)` / `var(--warn)` / `var(--err)`; `var(--text-2)` para `stale`); devolve `null` sem nenhum cartão. Ler `overview.today?.per_agent` com guarda.
- [ ] **Step 7:** Run `npx vitest run src/pages/terminals/planStrip.test.ts src/pages/terminals/PlanStrip.test.tsx src/pages/PlanLimits.test.tsx && npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 8:** Commit `feat(web): faixa do plano dos Terminais (NAC-63)`.

### Task 5: Lógica do recomeço num hook compartilhado

**Files:**
- Create: `web/src/pages/context/useRestartClean.ts`
- Modify: `web/src/pages/context/RestartCleanDialog.tsx` (passa a usar o hook; JSX e textos iguais)
- Test: `web/src/pages/context/useRestartClean.test.tsx`; guardas existentes, sem mudança: bloco `describe('RestartCleanDialog')` em `web/src/pages/context/context.test.tsx:92-131` e `web/src/pages/CardPage.test.tsx:287-298` ("mostra o contexto das sessões vivas do card e recomeça limpo")

**Interfaces:**
- Consumes: `post` de `web/src/api/client`; o `mode` da Task 1.
- Produces (consumido pela Task 7 e pelo `RestartCleanDialog`):

```ts
export const WHERE_STOPPED_MAX_MIN = 30   // muda de RestartCleanDialog.tsx para cá; o diálogo reexporta
export interface RestartCleanOptions {
  sessionId: string; whereStopped: string; whereStoppedAt: string | null
  mode?: 'background'                      // ausente = o corpo do POST não leva mode
  now?: number
  onDone?: (terminalId: string | undefined) => void
}
export interface RestartClean {
  text: string; setText(v: string): void
  needsText: boolean        // registro velho/vazio, ou o servidor pediu ("registre onde parou")
  canSend: boolean          // !pending && !(needsText && text.trim() === '')
  pending: boolean; done: boolean
  terminalId: string | undefined
  error: Error | null
  send(): void
}
export function useRestartClean(o: RestartCleanOptions): RestartClean
```

- Corpo do POST: `{ where_stopped }` só se o texto foi editado ou `needsText`; `mode` só se informado. Invalidações iguais às de hoje (`'card', 'cards', 'sessions', 'context-live', 'session-context', 'terminals', 'overview'`), em `onSettled`.

- [ ] **Step 1: Write the failing tests** em `useRestartClean.test.tsx` (`renderHook` com `QueryClientProvider`; fetch simulado e o auxiliar `bodies` como em `context.test.tsx`):
  - registro recente, sem edição, sem `mode` → corpo `{}`; com `mode: 'background'` → corpo `{ mode: 'background' }`;
  - texto editado com `mode: 'background'` → `{ where_stopped: 'novo', mode: 'background' }`;
  - `whereStoppedAt: null` → `needsText` verdadeiro, `canSend` falso com texto vazio e verdadeiro depois de `setText('x')`;
  - sucesso com `{ ok: true, terminal_id: 't9' }` → `done`, `terminalId === 't9'` e `onDone` chamado uma vez com `'t9'`;
  - erro 400 `registre onde parou antes de recomeçar` → `error.message` igual e `needsText` passa a verdadeiro; erro 409 → `error.message` literal e `done` falso.
- [ ] **Step 2:** Run `npx vitest run src/pages/context/useRestartClean.test.tsx`. Expected: FAIL (módulo não existe).
- [ ] **Step 3:** Implementar o hook movendo a lógica de `RestartCleanDialog.tsx:22-34` e trocar o corpo do diálogo pelo uso do hook (sem `mode`). O diálogo mantém `export { WHERE_STOPPED_MAX_MIN }`.
- [ ] **Step 4:** Run `npx vitest run src/pages/context src/pages/CardPage.test.tsx && npx tsc --noEmit`. Expected: PASS, sem nenhuma mudança nos testes existentes (o primeiro caso do diálogo continua exigindo o corpo `{}`).
- [ ] **Step 5:** Commit `refactor(web): lógica do recomeçar limpo num hook compartilhado (NAC-63)`.

### Task 6: Medidor de contexto no tile

**Files:**
- Create: `web/src/pages/terminals/TileContext.tsx`
- Modify: `web/src/pages/terminals/GridTile.tsx`
- Test: `web/src/pages/terminals/TileContext.test.tsx`, `web/src/pages/terminals/GridTile.test.tsx` (casos novos; os atuais não mudam)

**Interfaces:**
- Consumes: `ctxLevel`, `ctxDetail`, `ctxWarning`, `CTX_COLOR`, `CTX_DEFAULT_LIMIT` (Task 3); `barPct`, `pctText` de `web/src/pages/context/format.ts`.
- Produces, em `TileContext.tsx` (HTML simples, sem estado):

```tsx
export function CtxChip(p: { pct: number; limit: number }): JSX.Element                    // 'ctx 62%', title "Contexto usado"
export function CtxStrip(p: { pct: number; tokens: number; windowTokens: number; pctOnly: boolean; limit: number; onClear?: () => void }): JSX.Element
export function CtxWarning(p: { pct: number; limit: number; simple: boolean; onClear: () => void }): JSX.Element
```

- Produces, props novas do `GridTile` (todas opcionais; consumidas pela Task 8; a Task 7 acrescenta as do recomeço):

```ts
cardKey?: string            // card efetivo do tile; ausente = t.card_key
sessionId?: string          // '' ou ausente = sem sessão casada
ctxPct?: number | null      // inteiro; null ou ausente = sem dado
ctxTokens?: number; ctxWindow?: number; ctxPctOnly?: boolean
ctxLimit?: number           // ausente = 80
```

- No `GridTile`: `const card = p.cardKey ?? t.card_key` passa a valer no link do card, no "sem card" e no `aria-label` da seção. `const canRestart = !ended && !!p.sessionId && card !== ''`. Sem dado (`ctxPct` nulo ou ausente) ou terminal encerrado: nem chip, nem faixa, nem linha.
  - Simples: `<CtxChip>` no cabeçalho, imediatamente antes do estado.
  - Detalhado: `<CtxStrip>` entre o cabeçalho e o corpo, com `onClear` só quando `canRestart`.
  - Nos dois modos, com `ctxLevel(pct, limit) !== 'ok'` e `canRestart`: `<CtxWarning>` sob o cabeçalho (acima da faixa).
  - Tile em pausa (`live` falso) também mostra chip, faixa e linha.
  - Nesta task `onClear` chama um `openRestart` local que ainda não faz nada visível (`const openRestart = () => {}` com o comentário "painel na Task 7"); os testes desta task só conferem presença e ausência dos controles.
- Marcação fixa (os testes dependem dela):
  - Todo elemento colorido pelo nível leva `data-level="ok|warn|crit"`; os testes conferem o atributo, não a cor (como o `data-over` do `ContextMeter`).
  - `CtxChip`: `<span data-testid="ctx-chip" data-level=… title="Contexto usado">ctx 62%</span>`, cor do nível.
  - `CtxStrip`: `<div data-testid="ctx-strip">`; primeira linha `<strong>Contexto 62%</strong>`, `· {ctxDetail(...)}` em `var(--text-2)` e, com `onClear`, `<button type="button">Limpar</button>` à direita (estilo de link); embaixo `<div role="progressbar" aria-label="Contexto usado" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(barPct(pct))}>` de 6 px com o preenchimento `data-testid="ctx-bar"` (`data-level`, largura `barPct(pct)%`, cor do nível) e o traço `data-testid="ctx-limit"` em `left: {limit}%` com `title="Limite configurado 80%"`.
  - `CtxWarning`: `<div role="status" data-testid="ctx-warning" data-level="warn|crit">` com `ctxWarning(pct, limit, simple)` e `<button type="button">Limpar e continuar</button>`; âmbar em `warn`, vermelha em `crit`.

- [ ] **Step 1: Write the failing tests** em `TileContext.test.tsx`:
  - chip: `pct 62, limit 80` → texto `ctx 62%`, `title` "Contexto usado", `data-level="ok"`; `84` → `"warn"`; `92` → `"crit"`.
  - faixa: `Contexto 62%` em `<strong>`, o texto `· 124 mil de 200 mil`, barra com `aria-valuenow` 62, largura `62%` e `data-level="ok"`, traço com `left: 80%` e `title` "Limite configurado 80%"; `pctOnly` → `· o agente informa só a %` e nenhum "mil"; `pct 104` → texto `Contexto 104%` e barra com largura `100%`; sem `onClear` não há botão "Limpar"; com `onClear`, clicar chama uma vez.
  - aviso: `84/80`, Detalhado → `data-level="warn"` e o texto completo da spec; Simples → o texto curto; `92/80` → `data-level="crit"`; o botão "Limpar e continuar" chama `onClear`.
- [ ] **Step 2: Write the failing tests** em `GridTile.test.tsx` (novo `describe('contexto no tile')`, com o `setup` existente):
  - sem as props novas: nenhum `ctx-chip`, `ctx-strip` nem `ctx-warning` (o tile da entrega 1);
  - Detalhado com `sessionId: 's1', ctxPct: 62, ctxTokens: 124000, ctxWindow: 200000` → `ctx-strip` presente com o botão "Limpar", sem `ctx-chip` e sem `ctx-warning`;
  - Simples com os mesmos valores → `ctx-chip` antes do elemento de estado (ordem no DOM), sem `ctx-strip`;
  - `ctxPct: 84` nos dois modos → `ctx-warning` com `data-level="warn"`; `ctxPct: 92` → `crit`; `ctxLimit: 95` com `ctxPct: 92` → sem aviso;
  - sessão sem card (`sessionId: 's1', cardKey: ''`, `ctxPct: 84`) → faixa sem o botão "Limpar", sem `ctx-warning`, e o cabeçalho mostra "sem card" mesmo com `t.card_key: 'NAC-7'`;
  - sem sessão (`sessionId: ''`, `ctxPct: 84`) → sem botão "Limpar" e sem aviso;
  - `cardKey: 'NAC-9'` com `t.card_key: ''` → link `NAC-9` para `/cards/NAC-9`;
  - terminal encerrado com `ctxPct: 62` → nada do medidor; tile em pausa (`live: false`) com `ctxPct: 62` → `ctx-strip` presente.
- [ ] **Step 3:** Run `npx vitest run src/pages/terminals/TileContext.test.tsx src/pages/terminals/GridTile.test.tsx`. Expected: FAIL nos casos novos; os antigos do `GridTile` passam.
- [ ] **Step 4:** Implementar `TileContext.tsx` e ligar no `GridTile.tsx`.
- [ ] **Step 5:** Run o mesmo comando e `npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 6:** Commit `feat(web): medidor de contexto no tile (NAC-63)`.

### Task 7: "Limpar e continuar" no tile

**Files:**
- Create: `web/src/pages/terminals/TileRestart.tsx`
- Modify: `web/src/pages/terminals/GridTile.tsx`
- Test: `web/src/pages/terminals/GridTile.test.tsx` (novo `describe('Limpar e continuar')`)

**Interfaces:**
- Consumes: `useRestartClean` (Task 5); `canRestart`, `card`, `openRestart` e as props da Task 6; `ErrorLine`, `btn`, `btnPrimary`, `input`, `muted` de `web/src/pages/card/shared`.
- Produces, em `TileRestart.tsx`:

```tsx
export interface RestartPanelProps {
  sessionId: string; whereStopped: string; whereStoppedAt: string | null
  onDone(terminalId: string | undefined): void
  onCancel(): void
}
export function RestartPanel(p: RestartPanelProps): JSX.Element   // usa useRestartClean({ ...p, mode: 'background' })
```

- Produces, props novas do `GridTile` (opcionais; consumidas pela Task 8):

```ts
whereStopped?: string; whereStoppedAt?: string | null   // "Onde parei" do card da sessão casada
onRestarted?(terminalId: string): void                  // o agente novo abriu no terminal terminalId
```

- Estado local do tile: `const [restartFor, setRestartFor] = useState<string | null>(null)` (o id da sessão **no momento em que o painel abriu**; o painel não depende mais de `canRestart` depois de aberto — decisão do plano) e `const [cleared, setCleared] = useState(false)`. `openRestart` da Task 6 vira `() => { setMenu(false); setRestartFor(p.sessionId ?? null) }`.
- Onde a ação aparece: o botão "Limpar" da faixa e o botão da linha de aviso (Task 6) e, no menu ⋯, o item `Limpar e continuar` entre "Interagir" e "Encerrar agente": habilitado com `canRestart`; com `sessionId` e sem card, `disabled` com o texto `Limpar e continuar (precisa de um card)`; sem `sessionId`, ausente.
- Painel (`RestartPanel`): dentro do corpo do tile (o `div` do corpo ganha `position: 'relative'`), `position: 'absolute', inset: 0, overflow: 'auto', zIndex: 4`, fundo `var(--surface, #14171D)`; o terminal continua montado por baixo. `<section aria-labelledby>` com o título "Recomeçar limpo"; o texto `A sessão atual segue viva, mas sem o card. O agente novo começa do “Onde parei” abaixo, com o contexto zerado.`; `<label>` "Onde parei" com `<textarea>`; com `needsText`, o aviso `O registro está velho ou vazio: confirme ou atualize o texto antes de recomeçar.`; botões `Limpar e continuar` (`disabled={!canSend}`; `Abrindo…` enquanto envia) e `Cancelar`; `<ErrorLine error={error} />`.
- Enquanto o painel está aberto ou `cleared`, a linha de aviso e o botão "Limpar" da faixa não aparecem.
- Sucesso (`onDone`): `setRestartFor(null); setCleared(true); if (terminalId) p.onRestarted?.(terminalId)`. Com `cleared`, no lugar do painel (mesma posição, sobre o corpo): `<div role="status">Contexto limpo. O agente novo abriu em outro terminal.</div>` e, só com o tile ligado e o terminal vivo (`live && !ended`), o botão `Encerrar este terminal`, que faz `setConfirming(true)` (a confirmação "Encerrar o agente e o terminal?" que já existe); um botão `Fechar` tira o aviso (`setCleared(false)`; decisão do plano: a spec não diz como o aviso sai).

- [ ] **Step 1: Write the failing tests** em `GridTile.test.tsx` (fetch simulado por rota como em `context.test.tsx`, com o auxiliar `bodies`; props base `sessionId: 's1', cardKey: 'NAC-7', ctxPct: 84, ctxTokens: 168000, ctxWindow: 200000, whereStopped: 'Parei no passo 3', whereStoppedAt: new Date().toISOString(), onRestarted: vi.fn()`):
  - menu ⋯: com card, o item "Limpar e continuar" está habilitado; com `cardKey: ''`, existe o item `Limpar e continuar (precisa de um card)` desabilitado; com `sessionId: ''`, não há item que comece com "Limpar e continuar";
  - o botão "Limpar" da faixa abre o painel: região "Recomeçar limpo", o texto da spec e a caixa de texto com `Parei no passo 3`; a linha de aviso e o botão "Limpar" somem; `fetch` de `restart-clean` ainda não foi chamado;
  - o botão da linha de aviso e o item do menu abrem o mesmo painel; "Cancelar" fecha e a linha de aviso volta;
  - confirmar envia `POST /api/sessions/s1/restart-clean` com o corpo `{ mode: 'background' }` (registro recente, sem edição); com a resposta `{ ok: true, terminal_id: 't9' }`: `onRestarted` chamado uma vez com `'t9'`, aparece "Contexto limpo. O agente novo abriu em outro terminal." e o botão "Encerrar este terminal"; clicar nele mostra "Encerrar o agente e o terminal?" e `handle.kill` só é chamado depois de "Sim, encerrar";
  - `whereStoppedAt: null` → o aviso de registro velho aparece e o botão de confirmar fica desabilitado com o texto vazio;
  - **erro depois do desligamento**: resposta 409 `{ error: 'limite de terminais atingido — o card ficou pausado; use Continuar no card para abrir de novo.' }`; em seguida `rerenderWith({ cardKey: '' })` (a sessão perdeu o card) → o painel continua aberto e o `role="alert"` mostra a mensagem literal; `onRestarted` não foi chamado;
  - **sessão trocada com o painel aberto**: abrir o painel, `rerenderWith({ sessionId: 's2' })`, confirmar → o POST vai para `/api/sessions/s1/restart-clean`;
  - sucesso com o tile em pausa (`live: false`): a mensagem "Contexto limpo…" aparece e o botão "Encerrar este terminal" não.
- [ ] **Step 2:** Run `npx vitest run src/pages/terminals/GridTile.test.tsx`. Expected: FAIL nos casos novos.
- [ ] **Step 3:** Implementar `TileRestart.tsx` e o estado, o item do menu e os dois blocos no `GridTile.tsx`.
- [ ] **Step 4:** Run `npx vitest run src/pages/terminals src/pages/context && npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(web): Limpar e continuar no tile (NAC-63)`.

### Task 8: Página Terminais ligada às sessões, ao contexto e ao plano

**Files:**
- Modify: `web/src/pages/Terminals.tsx`, `web/src/pages/terminals/gridModel.ts` (`place`)
- Test: `web/src/pages/Terminals.test.tsx`, `web/src/pages/terminals/gridModel.test.ts`

**Interfaces:**
- Consumes: `matchSessions` (Task 2); `tileMeter`, `ctxLimit`, `TileMeter` (Task 3); `PlanStrip` (Task 4); as props do `GridTile` das Tasks 6 e 7; `useOverview`, `useContextLive`, `useSettings` de `web/src/api/queries.ts`.
- Produces, em `gridModel.ts`: `place(terms, projects, cards, over?: ReadonlyMap<string, { sessionId: string; cardKey: string; projectId: number | null }>)`. Para um terminal com entrada de `sessionId` não vazio: o projeto é `projectId` (se existir em `projects`); senão, o card `cardKey` na lista de cards; senão, o diretório. Sem entrada ou com `sessionId` vazio: a regra de hoje. Sem `over`, nada muda (os testes atuais de `place` passam sem mudança). Decisão do plano: o projeto do tile casado é o `effective_project_id` da sessão (o projeto do card dela; sem card, o da própria sessão), para o terminal antigo continuar na mesma aba depois de limpar.
- Em `Terminals.tsx`:
  - `const sessions = Array.isArray(overviewQ.data?.active) ? overviewQ.data.active : NO_SESSIONS` (constante `NO_SESSIONS: ActiveSession[] = []`, como `NO_CARDS`); `const ctxRows = Array.isArray(ctxQ.data) ? ctxQ.data : NO_CTX`; `const limit = ctxLimit(settingsQ.data?.context_warn_pcts)`.
  - `const ctxById = useMemo(() => new Map(ctxRows.map((c) => [c.id, c])), [ctxRows])` (id da sessão → resumo do contexto).
  - `const matched = useMemo(() => matchSessions(list, sessions), [list, sessions])`; `const meters = useMemo(() => new Map(list.map((t) => [t.id, tileMeter(t, matched.get(t.id), ctxById.get(matched.get(t.id)?.id ?? ''))])), [list, matched, ctxById])`; `placed = useMemo(() => place(list, projects, cards, meters), …)`.
  - `SlotProps` ganha, como valores primitivos: `cardKey: string; sessionId: string; ctxPct: number | null; ctxTokens: number; ctxWindow: number; ctxPctOnly: boolean; ctxLimit: number; whereStopped: string; whereStoppedAt: string | null`. O `TileSlot` os repassa pelo `...rest` e cria `onRestarted = useCallback((tid: string) => actions.restarted(tid), [actions])`. Nenhum objeto novo por render chega ao `TileSlot`.
  - `TileActions` ganha `restarted(terminalId: string): void`, que faz `setPendingFocus(terminalId)` (estado `string | null` da página). Um efeito, quando `pendingFocus` está em `data`, chama `actions.focus(pendingFocus)`, `setScrollTo(pendingFocus)` e limpa o estado (o terminal novo entra no início da grade pelo `reconcile` que já existe; não muda de aba nem amplia o tile — decisão do plano).
  - `<PlanStrip simple={layout.simple} hasKiro={hasKiro} />` logo abaixo do cabeçalho da página e acima dos avisos e das abas, só com `list.length > 0`; `const hasKiro = useMemo(() => list.some((t) => t.agent === 'kiro'), [list])`.

- [ ] **Step 1: Write the failing test** em `gridModel.test.ts`: com `over` dizendo `{ sessionId: 's1', cardKey: 'NAC-9', projectId: 3 }` para um terminal de `card_key: ''` e `dir` do projeto 4 → `rootId: 1, subId: 3`; entrada com `sessionId: ''` → a regra de hoje; `{ sessionId: 's1', cardKey: '', projectId: null }` num terminal com `card_key` de um card do projeto 2 → vale o diretório (o card do terminal não conta).
- [ ] **Step 2: Ajustar o auxiliar de teste** em `Terminals.test.tsx`: `api(terms, projects = [], cards = [], extra: Record<string, unknown> = {})` responde por URL: `extra[url]` se houver; `/api/overview` → `{ active: [], today: { per_agent: [] }, alerts: [], no_card: [], events: [] }`; `/api/context/live` → `[]`; `/api/settings` → `{ context_warn_pcts: [60, 80] }`; URL começando com `/api/plan-limits` → `{ five_hour: null, seven_day: null, history: { five_hour: null, seven_day: null }, warn_pct: 80 }`. Os três testes que simulam `fetch` direto e devolvem a lista de terminais para qualquer URL (`Terminals.test.tsx:189`, `:267`, `:291`) ficam como estão: eles guardam as leituras defensivas (Review Focus 2).
- [ ] **Step 3: Write the failing tests** em `Terminals.test.tsx`. Fixtures criadas uma vez por teste: `sess = (id, pid, key, o = {}) => ({ id, agent: 'claude', pid, started_at: ago(5), ended_at: null, pid_dead_at: null, effective_project_id: null, last_activity: '', card: key ? { key, title: key, project_id: 1, where_stopped: 'Parei no passo 3', where_stopped_at: new Date().toISOString() } : null, ...o })` e `ctx = (id, pct, o = {}) => ({ id, card_key: '', card_title: '', tokens: pct * 2000, window_tokens: 200000, pct, over80: pct > 80, rewrite: false, idle_over_ttl: false, ...o })`.
  - **o card do tile vem da sessão**: terminal `a` (`pid: 101, card_key: ''`, `dir: '/outro'`) e sessão `sess('sa', 101, 'NAC-9', { effective_project_id: 4 })` com `PROJECTS` → o tile mostra o link `NAC-9`, a etiqueta de projeto "solo" e a aba `solo 1` existe; terminal `b` (`pid: 102, card_key: 'NAC-2'`) casado com `sess('sb', 102, null)` → "sem card"; terminal `c` (`pid: 103, card_key: 'NAC-3'`) sem sessão → link `NAC-3`.
  - **contexto no tile certo**: `ctx('sa', 62)` e `ctx('sb', 84)` → o tile `a` mostra `Contexto 62%` e o `b` `Contexto 84%`; terminal sem sessão casada não tem `ctx-strip`; com `/api/settings` → `{ context_warn_pcts: [50, 70] }`, o traço do tile `a` tem `title` "Limite configurado 70%".
  - **um evento `sessions` que não muda os valores de um tile não o redesenha** (no molde de "interagir num tile não redesenha o outro", `Terminals.test.tsx:302`): terminais `a` (pid 101, `NAC-1`) e `b` (pid 102, `NAC-2`); estado mutável lido pelo `fetch` com `sess('sa', 101, 'NAC-1')`, `sess('sb', 102, 'NAC-2')`, `sess('sx', 999, null)` e `ctx('sa', 62)`, `ctx('sb', 40)`. Depois de `settle()`, guardar `renders.a` e `renders.b`. (1) Mudar só o que não chega ao tile — `last_activity` e `last_event_at` de `sa`, e `ctx('sx', 10)` na lista — e disparar `await act(async () => { invalidateForKind(qc, 'sessions') })` (de `web/src/api/useStream.ts`), `settle()`: `renders.a` e `renders.b` iguais aos guardados. (2) Mudar `ctx('sa', 71)` e disparar de novo: `renders.a` maior, `renders.b` igual, e o tile `a` mostra `Contexto 71%`.
  - **faixa do plano**: com `extra` de `/api/plan-limits?hours=24` trazendo 62% e 41% → `section` "Uso do plano" antes do `tablist` "Projetos" no DOM; em Simples aparece `Claude 5 h 62% · sem 41%`; sem dado do plano não existe a `section`.
  - **limpar e continuar abre o agente novo na grade**: com `PROJECTS`, terminal `a` (pid 101, `card_key: 'NAC-1'`, `dir: '/w/solo'`) com `sess('sa', 101, 'NAC-1', { effective_project_id: 4 })` e `ctx('sa', 84)`; `POST /api/sessions/sa/restart-clean` responde `{ ok: true, terminal_id: 'novo' }` e, a partir dele, o estado passa a ter o terminal `novo` (pid 201, `card_key: 'NAC-1'`, mais recente), `sess('sa', 101, null, { effective_project_id: 4 })` e `sess('sn', 201, 'NAC-1', { effective_project_id: 4 })`. Clicar "Limpar e continuar" na linha de aviso de `a` e confirmar no painel → `tileIds()` vira `['novo', 'a']`; o terminal de `novo` tem `data-active="true"`; `scrollIntoView` (espiado como em `Terminals.test.tsx:173`) foi chamado no tile `novo`; o tile `a` mostra "sem card" e "Contexto limpo. O agente novo abriu em outro terminal."; o corpo do POST é `{ mode: 'background' }`; e, na aba `solo`, os dois tiles continuam visíveis (o antigo fica no projeto da sessão).
  - **respostas malformadas**: `extra` com `/api/overview` → `[]`, `/api/context/live` → `{}`, `/api/settings` → `[]`, `/api/plan-limits?hours=24` → `[]` → a grade mostra os tiles com o card do terminal, sem `ctx-strip` e sem a `section` do plano.
- [ ] **Step 4:** Run `npx vitest run src/pages/Terminals.test.tsx src/pages/terminals/gridModel.test.ts`. Expected: FAIL nos casos novos; os casos antigos passam.
- [ ] **Step 5:** Implementar `place` com `over` e a ligação em `Terminals.tsx` conforme as Interfaces.
- [ ] **Step 6:** Run `go build ./... && go test ./...` e `cd web && npx vitest run && npx tsc --noEmit && npm run build`. Expected: PASS em tudo; `ControlRoom.test.tsx`, `context.test.tsx`, `CardPage.test.tsx` e `PlanLimits.test.tsx` sem nenhuma mudança neles.
- [ ] **Step 7:** Commit `feat(web): Terminais com medidores, plano e Limpar e continuar ligados à sessão (NAC-63)`.
- [ ] **Step 8 (manual, antes do merge):** rodar `cd web && npm run bench` num Chrome com GPU e a janela visível, clicar nos botões "Agente" e "Enxurrada" (um de cada vez, esperando o veredito) e registrar no card NAC-63, com `nobre note`, os números de cada perfil (quadros por segundo, tarefas longas, atraso p95 e máximo, renderizador) e o veredito. A entrega só é mergeada com "aprovado" nos dois perfis com WebGL.

---

## Verificação final (antes da revisão do branch)

- `go build ./... && go test ./...`
- `cd web && npx tsc --noEmit && npx vitest run && npm run build`
- Conferir à mão, com o daemon de desenvolvimento:
  - abrir `/terminals` com um agente Claude num card: faixa "Contexto N%" no Detalhado, chip `ctx N%` no Simples, faixa do plano sob o cabeçalho;
  - "Limpar" → painel no tile → "Limpar e continuar": o agente novo aparece no início da grade, em foco; o tile antigo fica "sem card" com "Encerrar este terminal";
  - com um tile em "Interagir", ver o que acontece com o terminal quando a faixa ou a linha de aviso aparece (Divergência 2) e anotar no card;
  - abrir a página de um card com sessão viva: o "Recomeçar limpo" de lá segue igual (modo padrão das configurações); abrir a Sala de Controle: "Ver terminal" segue igual.
