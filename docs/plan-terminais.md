# Terminais na plataforma: plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Permitir assistir e interagir, dentro da interface web, com o terminal de cada agente: sessões abertas pela plataforma ("Continuar" no modo segundo plano) e sessões abertas pelo usuário com `nobre run claude`.

**Architecture:** O `nobred` passa a ser dono de pseudo-terminais (PTY). Cada terminal roda o agente com cwd/env/PATH corretos, guarda as saídas recentes num buffer circular e as transmite por WebSocket. A UI usa xterm.js para mostrar e enviar teclas. O `nobre run` cria um terminal no daemon e conecta o terminal local do usuário a ele, então a mesma sessão aparece no terminal local e na plataforma.

**Tech Stack:** Go (`github.com/creack/pty`, `github.com/coder/websocket`), React + `@xterm/xterm` + `@xterm/addon-fit`.

**Spec:** `docs/spec.md` §6.3.1 (modo "segundo plano, com a saída transmitida para a interface") e §12. Pedido do usuário em 2026-11-07: "Preciso de conseguir ver em terminais simulados na plataforma o que está acontecendo com o agente" (pulou as perguntas; decisões abaixo são do Claude).

## Global Constraints

- Mesmas do `docs/plan.md` (pt-BR, Linux, só loopback, `localOnly`).
- WebSocket: além do `localOnly` (Host e RemoteAddr loopback), exigir `Origin` igual a `http://127.0.0.1:<porta>` ou `http://localhost:<porta>`, ou ausente (cliente CLI). Origem diferente → 403. Browsers permitem WebSocket entre sites, então sem essa checagem qualquer página poderia digitar no terminal do agente.
- Buffer circular de saída: 2 MiB por terminal (quem conecta depois recebe o histórico recente).
- Terminal encerrado continua listado por 30 min (para ver a saída final) e depois é descartado. Máximo de 20 terminais vivos; acima disso, 409 "limite de terminais atingido".
- O processo do agente no PTY é filho do `nobred`, em sessão própria (`Setsid`). Terminais da plataforma (Continuar em segundo plano e `nobre run`) são encerrados quando o `nobred` para ou reinicia (o master do PTY fecha e o kernel envia SIGHUP; o shutdown envia SIGTERM antes). Para sessões que precisam sobreviver a um restart, use Continuar no terminal externo.
- O nome do processo do agente continua `claude` (caminho absoluto sem resolver symlink), para a identificação da sessão pelos hooks funcionar.

## Review Focus

1. **Página de outro site tentando abrir o WebSocket** → 403 pela checagem de Origin. (Task 2)
2. **Cliente lento ou aba esquecida aberta** → o PTY nunca bloqueia; assinante lento é desconectado; o buffer continua. (Task 1)
3. **Agente termina** → todos os clientes recebem o código de saída e o terminal fica "encerrado"; nenhum goroutine ou fd vaza. (Task 1)
4. **`nobre run` com o daemon fora** → roda o agente direto no terminal local (sem plataforma), com aviso em stderr; nunca impede o usuário de trabalhar. (Task 3)
5. **Redimensionar a janela** (local ou navegador) → o PTY recebe o tamanho; com vários clientes, vale o último que redimensionou. (Tasks 1, 3, 4)

---

### Task 1: Gerenciador de PTY no daemon

**Files:**
- Create: `internal/pty/manager.go`, `internal/pty/ring.go`
- Test: `internal/pty/manager_test.go`, `internal/pty/ring_test.go`

**Interfaces:**
- Produces:
  - `pty.Ring` com `NewRing(size int) *Ring`, `Write(p []byte) (int, error)`, `Snapshot() []byte` (últimos `size` bytes em ordem).
  - `pty.Manager` com `NewManager(opts Options) *Manager`, `Options{RingSize int; MaxLive int; Retention time.Duration; Now func() time.Time}` (padrões 2 MiB, 20, 30 min).
  - `(*Manager).Start(spec StartSpec) (*Terminal, error)` com `StartSpec{Argv []string; Dir string; Env []string; Cols, Rows uint16; CardKey, Agent, Origin string}` (`Origin` ∈ `"plataforma"|"nobre run"`). Erro `ErrTooMany` acima do limite.
  - `(*Manager).Get(id string) (*Terminal, bool)`, `List() []Info`, `Kill(id string) error`, `Close()` (mata todos; usado no shutdown).
  - `Info{ID, CardKey, Agent, Origin, Title string; PID int; StartedAt time.Time; EndedAt *time.Time; ExitCode *int; Cols, Rows uint16; Clients int}`.
  - `(*Terminal).Subscribe() (snapshot []byte, ch <-chan []byte, cancel func())` — canal com buffer; assinante que não lê é desconectado (canal fechado), nunca bloqueia o leitor do PTY.
  - `(*Terminal).Write(p []byte) error` (entrada), `Resize(cols, rows uint16) error`, `Done() <-chan struct{}`, `Info() Info`.
  - IDs: `t-<8 hex aleatórios>`.
- [ ] **Step 1: Testes que falham**
  `TestRingKeepsLastBytes` (escrever 3× o tamanho → Snapshot = últimos `size` bytes); `TestStartEchoesOutput` (`/bin/sh -c 'printf oi'` → snapshot contém "oi", `ExitCode=0`, `Done` fecha); `TestInputReachesProcess` (`cat` + Write("abc\n") → saída contém "abc"); `TestSlowSubscriberDropped` (assinante que não lê é desconectado e o processo continua escrevendo sem travar); `TestResizeApplies` (`stty size` após Resize(100,40) → "40 100"); `TestMaxLive` (limite 1 → segundo Start = ErrTooMany); `TestRetentionPrunes` (relógio falso: encerrado há 31 min some do List); `TestCloseKillsAll` (sem goroutines vazando: `runtime.NumGoroutine` volta ao patamar).
- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/pty/ -v`.
- [ ] **Step 3: Implementar** com `creack/pty` (`pty.StartWithSize`, `Setsid`), uma goroutine leitora por terminal que escreve no Ring e faz fan-out não bloqueante.
- [ ] **Step 4: Rodar e ver passar** (também com `-race`).
- [ ] **Step 5: Commit** — `feat: gerenciador de pseudo-terminais no nobred`

---

### Task 2: API de terminais e WebSocket

**Files:**
- Create: `internal/api/terminals.go`
- Modify: `internal/api/server.go` (Deps.Terminals *pty.Manager, rotas), `cmd/nobred/main.go` (cria o Manager e fecha no shutdown)
- Test: `internal/api/terminals_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `GET /api/terminals` → `[]Info` (JSON snake_case).
  - `POST /api/terminals {"agent":"claude","dir":"<abs>","args":[...],"cols":120,"rows":32,"card_key":"","origin":"nobre run"}` → `Info` (201). Resolve o binário pelo adaptador (`Adapter.Binary()`), env = ambiente do daemon + `PATH` de sessão (o mesmo do Continuar) + `TERM=xterm-256color`; `dir` precisa existir. Agente desconhecido → 400 "agente indisponível no piloto".
  - `DELETE /api/terminals/{id}` → mata o processo (204).
  - `GET /api/terminals/{id}/ws` → WebSocket. Mensagens do servidor: binárias = bytes do terminal (primeiro o snapshot); texto JSON `{"type":"exit","code":N}` ao encerrar e `{"type":"info",...}` ao conectar. Mensagens do cliente: binárias = entrada (teclas); texto JSON `{"type":"resize","cols":C,"rows":R}`. Query `?readonly=1` ignora entrada.
  - Checagem de Origin conforme Global Constraints; publica `terminals` no Hub ao criar/encerrar.
- [ ] **Step 1: Testes que falham** (servidor `httptest` real + cliente `coder/websocket`): `TestTerminalWSStreamsAndAcceptsInput` (cria `cat`, envia "x\n", recebe "x"); `TestTerminalWSSnapshotForLateJoiner`; `TestTerminalWSExitMessage`; `TestTerminalWSRejectsForeignOrigin` (Origin `http://evil.example` → 403); `TestTerminalWSReadonlyIgnoresInput`; `TestCreateTerminalUnknownAgent`; `TestDeleteTerminalKills`. Agente nos testes: adaptador falso cujo `Binary()` devolve `/bin/cat` ou `/bin/sh`.
- [ ] **Step 2: Rodar e ver falhar.**
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar** (com `-race`).
- [ ] **Step 5: Commit** — `feat: API e WebSocket dos terminais`

---

### Task 3: Modo "segundo plano" no Continuar e `nobre run`

**Files:**
- Modify: `internal/api/continue.go` (modo `background`), `internal/api/config.go` (aceitar `launch_mode: background` como padrão válido), `cmd/nobre/main.go` + Create `cmd/nobre/run.go`, `internal/client/client.go`
- Test: `internal/api/continue_test.go`, `cmd/nobre/run_test.go`

**Interfaces:**
- Consumes: Tasks 1–2.
- Produces:
  - `POST /api/cards/{key}/continue` com `mode: "background"` (ou padrão `background` nas Settings): mesmas validações e mesmo checkout do modo terminal, mas inicia via `Manager.Start` com `Dir` do projeto, `Env` com `NOBRE_RESUME=<KEY>` e PATH de sessão, `Argv = Adapter.LaunchCommand(bin, prompt)`, `CardKey`, `Origin "plataforma"`; resposta inclui `terminal_id`.
  - `nobre run <agente> [args...]`: pede ao daemon um terminal (`POST /api/terminals` com cwd, tamanho atual do terminal, args, `NOBRE_RESUME` do ambiente se existir) e conecta o terminal local em modo raw ao WebSocket (stdin → binário; saída → stdout; SIGWINCH → resize). Termina com o código de saída do agente. Ctrl+] solta o terminal local sem matar o agente (o agente segue visível na plataforma). Daemon fora ou erro na criação → `exec` direto do agente localmente, com aviso em stderr "NobreAI Cockpit fora do ar: rodando sem a plataforma".
- [ ] **Step 1: Testes que falham**: `TestContinueBackgroundStartsTerminal` (Manager real com `/bin/sh` falso como agente; resposta com `terminal_id`; env contém `NOBRE_RESUME`); `TestRunAttachesAndExitsWithCode` (servidor de teste + agente `/bin/sh -c 'exit 3'` → `nobre run` sai 3 e repassa a saída); `TestRunFallsBackWhenDaemonDown` (executor falso registra exec direto + aviso); `TestRunDetachKey` (Ctrl+] solta sem DELETE).
- [ ] **Step 2: Rodar e ver falhar.**
- [ ] **Step 3: Implementar** (`golang.org/x/term` para raw mode; restaurar o terminal sempre, inclusive em pânico/sinal).
- [ ] **Step 4: Rodar e ver passar.**
- [ ] **Step 5: Commit** — `feat: continuar em segundo plano e nobre run com terminal na plataforma`

---

### Task 4: Terminais na interface

**Files:**
- Create: `web/src/ui/TerminalView.tsx`, `web/src/pages/Terminals.tsx`, `web/src/api/terminals.ts`
- Modify: `web/src/app/routes.tsx` e `Layout.tsx` (item "Terminais" na barra lateral), `web/src/pages/CardPage.tsx` (aba/painel "Terminal" quando o card tem terminal vivo ou recente), `web/src/pages/card/ContinuePanel.tsx` (habilitar "Em segundo plano, com saída aqui na interface"; ao iniciar nesse modo, abrir o terminal do card), `web/src/pages/ControlRoom.tsx` (botão "Ver terminal" nos agentes rodando que têm terminal), `web/src/api/useStream.ts` (kind `terminals`)
- Test: `web/src/ui/TerminalView.test.tsx`, `web/src/pages/Terminals.test.tsx`

**Interfaces:**
- Consumes: Task 2 (rotas e protocolo), Task 3 (`terminal_id` no continue).
- Produces:
  - `TerminalView({ id, readonly })`: xterm.js + fit addon; conecta em `/api/terminals/{id}/ws` (ws:// relativo ao host da página); escreve bytes binários; envia teclas e resize (debounce 100 ms); mostra faixa "encerrado (código N)" ao receber `exit`; reconecta 3× com backoff se cair; botão "Somente assistir"/"Interagir" alterna `readonly`; botão "Encerrar agente" (confirmação inline, sem `window.confirm`) → DELETE.
  - Página "Terminais": lista (agente, card, origem, iniciado há, ao vivo/encerrado, clientes) + o terminal selecionado em tela cheia.
  - Tema do xterm com os tokens do design (fundo `#0B0D11`, texto `#E8EAEE`), fonte Geist Mono.
- [ ] **Step 1: Testes que falham** (WebSocket falso injetável): `TerminalView` escreve no xterm os bytes recebidos; envia resize ao montar; mostra "encerrado (código 0)"; em `readonly` não envia teclas. `Terminals` lista itens do `GET /api/terminals` e abre o selecionado.
- [ ] **Step 2: Rodar e ver falhar.**
- [ ] **Step 3: Implementar.** Vite dev proxy: `/api` com `ws: true`.
- [ ] **Step 4: Rodar e ver passar** + `pnpm -C web build` + `make build`.
- [ ] **Step 5: Commit** — `feat: terminais dos agentes na interface`
