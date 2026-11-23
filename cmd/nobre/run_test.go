package main

import (
	"bytes"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/api"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// binAdapter é um agente falso cujo binário é um executável de teste.
type binAdapter struct {
	adapter.Adapter
	bin string
}

func (b binAdapter) Binary() string { return b.bin }
func (b binAdapter) Name() string   { return "claude" }

type runEnv struct {
	srv  *httptest.Server
	mgr  *pty.Manager
	addr string
}

func newRunEnv(t *testing.T, script string) *runEnv {
	t.Helper()
	return newRunEnvOpts(t, script, pty.Options{})
}

func newRunEnvOpts(t *testing.T, script string, opts pty.Options) *runEnv {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := api.NewHub()
	t.Cleanup(hub.Close)
	mgr := pty.NewManager(opts)
	t.Cleanup(mgr.Close)
	srv := httptest.NewServer(api.New(st, api.Deps{Hub: hub, Terminals: mgr,
		Agents: agents.New(binAdapter{bin: bin})}))
	t.Cleanup(srv.Close)
	return &runEnv{srv: srv, mgr: mgr, addr: strings.TrimPrefix(srv.URL, "http://")}
}

// syncBuf é um buffer seguro para a goroutine de saída do nobre run.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type runHarness struct {
	c        *cli
	out, err *syncBuf
	execPath string
	execArgv []string
	restored int
	raw      bool
	winch    chan os.Signal
	sigs     chan os.Signal
	environ  []string
	size     [2]int
	mu       sync.Mutex
}

func newHarness(t *testing.T, addr string, in io.Reader, tty bool, env map[string]string) *runHarness {
	t.Helper()
	h := &runHarness{out: &syncBuf{}, err: &syncBuf{}, winch: make(chan os.Signal, 1), sigs: make(chan os.Signal, 1), size: [2]int{100, 40}}
	h.c = &cli{in: in, out: h.out, err: h.err,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return env[k]
		},
		cwd: func() (string, error) { return t.TempDir(), nil },
		rd: &runDeps{
			isTTY: func() bool { return tty },
			size:  func() (int, int) { h.mu.Lock(); defer h.mu.Unlock(); return h.size[0], h.size[1] },
			raw: func() (func(), error) {
				h.mu.Lock()
				h.raw = true
				h.mu.Unlock()
				return func() { h.mu.Lock(); h.restored++; h.mu.Unlock() }, nil
			},
			winch:    func() (<-chan os.Signal, func()) { return h.winch, func() {} },
			signals:  func() (<-chan os.Signal, func()) { return h.sigs, func() {} },
			environ:  func() []string { return h.environ },
			lookPath: func(n string) (string, error) { return "/fake/bin/" + n, nil },
			exec: func(p string, a []string) error {
				h.execPath, h.execArgv = p, a
				return nil
			},
		}}
	t.Setenv("NOBRE_AGENT_PID", "77")
	return h
}

func (h *runHarness) restoredCount() int { h.mu.Lock(); defer h.mu.Unlock(); return h.restored }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunAttachesAndExitsWithCode(t *testing.T) {
	e := newRunEnv(t, `echo "oi $1 $2"; exit 3`)
	h := newHarness(t, e.addr, strings.NewReader(""), true, nil)
	if code := h.c.run([]string{"run", "claude", "--flag", "x"}); code != 3 {
		t.Fatalf("code = %d err=%q", code, h.err.String())
	}
	if !strings.Contains(h.out.String(), "oi --flag x") {
		t.Fatalf("saída = %q", h.out.String())
	}
	if h.execPath != "" {
		t.Fatal("não deveria cair no exec direto")
	}
	if strings.Contains(h.out.String(), terminalReset) {
		t.Fatal("saída normal não deve resetar o terminal")
	}
	list := e.mgr.List()
	if len(list) != 1 || list[0].Origin != "nobre run" || list[0].Agent != "claude" {
		t.Fatalf("terminais = %+v", list)
	}
}

func TestRunPassesResumeKeyAndSize(t *testing.T) {
	e := newRunEnv(t, `echo "R=$NOBRE_RESUME"; stty size`)
	h := newHarness(t, e.addr, strings.NewReader(""), true, map[string]string{"NOBRE_RESUME": "NAC-7"})
	if code := h.c.run([]string{"run", "claude"}); code != 0 {
		t.Fatalf("code = %d err=%q", code, h.err.String())
	}
	out := h.out.String()
	if !strings.Contains(out, "R=NAC-7") || !strings.Contains(out, "40 100") {
		t.Fatalf("saída = %q", out)
	}
	if !h.raw || h.restoredCount() != 1 {
		t.Fatalf("raw=%v restaurado=%d", h.raw, h.restoredCount())
	}
}

func TestRunIgnoresInvalidResumeKey(t *testing.T) {
	e := newRunEnv(t, `echo "R=[$NOBRE_RESUME]"`)
	h := newHarness(t, e.addr, strings.NewReader(""), true, map[string]string{"NOBRE_RESUME": "lixo; rm"})
	if code := h.c.run([]string{"run", "claude"}); code != 0 || !strings.Contains(h.out.String(), "R=[]") {
		t.Fatalf("code=%d out=%q err=%q", code, h.out.String(), h.err.String())
	}
}

func TestRunFallsBackWhenDaemonDown(t *testing.T) {
	h := newHarness(t, "127.0.0.1:1", strings.NewReader(""), true, nil)
	code := h.c.run([]string{"run", "claude", "--resume", "abc"})
	if code != 1 { // o exec falso retorna: sinaliza falha
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(h.err.String(), "NobreAI Cockpit fora do ar: rodando sem a plataforma") {
		t.Fatalf("stderr = %q", h.err.String())
	}
	if h.execPath != "/fake/bin/claude" || strings.Join(h.execArgv, " ") != "claude --resume abc" {
		t.Fatalf("exec = %q %q", h.execPath, h.execArgv)
	}
}

func TestRunFallsBackOnCreateError(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"limite de terminais atingido"}`))
	})
	h := newHarness(t, addr, strings.NewReader(""), true, nil)
	h.c.run([]string{"run", "claude"})
	if h.execPath == "" || strings.Contains(h.err.String(), "fora do ar") ||
		!strings.Contains(h.err.String(), "Não consegui abrir na plataforma (limite de terminais atingido): rodando sem ela") {
		t.Fatalf("exec=%q stderr=%q", h.execPath, h.err.String())
	}
}

func TestRunDetachKey(t *testing.T) {
	e := newRunEnv(t, `echo ready; exec cat`)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	h := newHarness(t, e.addr, pr, true, nil)
	done := make(chan int, 1)
	go func() { done <- h.c.run([]string{"run", "claude"}) }()
	waitFor(t, "agente pronto", func() bool { return strings.Contains(h.out.String(), "ready") })
	pw.Write([]byte{0x1d})
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("não soltou")
	}
	if !strings.Contains(h.err.String(), "desconectado; o agente segue na plataforma: http://"+e.addr+"/terminals?id="+e.mgr.List()[0].ID) {
		t.Fatalf("stderr = %q", h.err.String())
	}
	if h.restoredCount() != 1 || !strings.HasSuffix(h.out.String(), terminalReset) {
		t.Fatalf("restaurado = %d out=%q", h.restoredCount(), h.out.String())
	}
	list := e.mgr.List()
	if len(list) != 1 || list[0].ExitCode != nil {
		t.Fatalf("o agente deveria seguir vivo: %+v", list)
	}
	waitFor(t, "cliente desconectado", func() bool { return e.mgr.List()[0].Clients == 0 })
}

func TestRunResizeFollowsWinch(t *testing.T) {
	e := newRunEnv(t, `echo ready; read l; stty size`)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	h := newHarness(t, e.addr, pr, true, nil)
	done := make(chan int, 1)
	go func() { done <- h.c.run([]string{"run", "claude"}) }()
	waitFor(t, "agente pronto", func() bool { return strings.Contains(h.out.String(), "ready") })
	h.mu.Lock()
	h.size = [2]int{120, 50}
	h.mu.Unlock()
	h.winch <- os.Interrupt
	waitFor(t, "resize aplicado", func() bool { l := e.mgr.List(); return len(l) == 1 && l[0].Cols == 120 && l[0].Rows == 50 })
	pw.Write([]byte("go\n"))
	if code := <-done; code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(h.out.String(), "50 120") {
		t.Fatalf("saída = %q", h.out.String())
	}
}

func TestRunDaemonStopsMidSession(t *testing.T) {
	// O daemon some no meio da sessão: o socket cai sem a mensagem de saída.
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"t-1"}`))
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		c.Write(r.Context(), websocket.MessageBinary, []byte("meio"))
		c.CloseNow()
	})
	h := newHarness(t, addr, strings.NewReader(""), true, nil)
	code := h.c.run([]string{"run", "claude"})
	if code != 1 || !strings.Contains(h.err.String(), "desconectado; o agente segue na plataforma: http://"+addr+"/terminals?id=t-1") ||
		strings.Contains(h.err.String(), "parou") || !strings.Contains(h.out.String(), "meio") ||
		!strings.HasSuffix(h.out.String(), terminalReset) || h.restoredCount() != 1 {
		t.Fatalf("code=%d stderr=%q out=%q", code, h.err.String(), h.out.String())
	}
}

func TestRunSkipsPlatformWithoutTTY(t *testing.T) {
	e := newRunEnv(t, `echo nao-deveria`)
	h := newHarness(t, e.addr, strings.NewReader(""), false, nil)
	h.c.run([]string{"run", "claude", "-p", "oi"})
	if strings.Join(h.execArgv, " ") != "claude -p oi" || strings.Contains(h.err.String(), "fora do ar") || len(e.mgr.List()) != 0 || h.raw {
		t.Fatalf("argv=%q stderr=%q terminais=%d raw=%v", h.execArgv, h.err.String(), len(e.mgr.List()), h.raw)
	}
}

func TestRunSignalsKillAgentAndRestore(t *testing.T) {
	// Fechar a janela (SIGHUP), kill (SIGTERM) ou SIGINT/SIGQUIT externos
	// encerram o agente: só Ctrl+] solta sem matar.
	for sig, want := range map[syscall.Signal]int{syscall.SIGINT: 130, syscall.SIGQUIT: 131, syscall.SIGHUP: 129, syscall.SIGTERM: 143} {
		e := newRunEnv(t, `echo ready; exec cat`)
		pr, pw := io.Pipe()
		h := newHarness(t, e.addr, pr, true, nil)
		done := make(chan int, 1)
		go func() { done <- h.c.run([]string{"run", "claude"}) }()
		waitFor(t, "agente pronto", func() bool { return strings.Contains(h.out.String(), "ready") })
		h.sigs <- sig
		if code := <-done; code != want {
			t.Fatalf("%v: code = %d (quero %d)", sig, code, want)
		}
		if h.restoredCount() != 1 || !strings.HasSuffix(h.out.String(), terminalReset) {
			t.Fatalf("%v: restaurado=%d out=%q", sig, h.restoredCount(), h.out.String())
		}
		waitFor(t, "agente encerrado", func() bool {
			l := e.mgr.List()
			return len(l) == 1 && l[0].ExitCode != nil && l[0].EndReason == "killed"
		})
		pw.Close()
	}
}

// chatty é um agente que escreve sem parar.
const chatty = `echo ready; while :; do echo tique; done`

func TestRunDetachNoAgentBytesAfterReset(t *testing.T) {
	for i := 0; i < 3; i++ {
		e := newRunEnv(t, chatty)
		pr, pw := io.Pipe()
		h := newHarness(t, e.addr, pr, true, nil)
		done := make(chan int, 1)
		go func() { done <- h.c.run([]string{"run", "claude"}) }()
		waitFor(t, "agente pronto", func() bool { return strings.Count(h.out.String(), "tique") > 5 })
		pw.Write([]byte{0x1d})
		if code := <-done; code != 0 {
			t.Fatalf("code = %d", code)
		}
		out := h.out.String()
		time.Sleep(50 * time.Millisecond)
		if !strings.HasSuffix(out, terminalReset) || h.out.String() != out {
			t.Fatalf("bytes do agente depois do reset: %q", out[max(0, len(out)-80):])
		}
		pw.Close()
	}
}

func TestRunDetachKittyKey(t *testing.T) {
	e := newRunEnv(t, `echo ready; exec cat`)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	h := newHarness(t, e.addr, pr, true, nil)
	done := make(chan int, 1)
	go func() { done <- h.c.run([]string{"run", "claude"}) }()
	waitFor(t, "agente pronto", func() bool { return strings.Contains(h.out.String(), "ready") })
	pw.Write([]byte("\x1b[93;5u")) // Ctrl+] no protocolo de teclado do kitty
	select {
	case code := <-done:
		if code != 0 || !strings.Contains(h.err.String(), "desconectado; o agente segue na plataforma") {
			t.Fatalf("code=%d stderr=%q", code, h.err.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("não soltou")
	}
	if l := e.mgr.List(); len(l) != 1 || l[0].ExitCode != nil {
		t.Fatalf("o agente deveria seguir vivo: %+v", l)
	}
}

func TestRunDaemonShutdownEndsSession(t *testing.T) {
	e := newRunEnv(t, `echo ready; exec cat`)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	h := newHarness(t, e.addr, pr, true, nil)
	done := make(chan int, 1)
	go func() { done <- h.c.run([]string{"run", "claude"}) }()
	waitFor(t, "agente pronto", func() bool { return strings.Contains(h.out.String(), "ready") })
	go e.mgr.Close() // o nobred parando
	code := <-done
	if code != 143 || !strings.Contains(h.err.String(), "O NobreAI Cockpit parou e encerrou a sessão; retome com `claude --continue` no mesmo diretório.") ||
		!strings.HasSuffix(h.out.String(), terminalReset) || h.restoredCount() != 1 {
		t.Fatalf("code=%d stderr=%q out=%q", code, h.err.String(), h.out.String())
	}
}

func TestRunKilledFromPlatform(t *testing.T) {
	e := newRunEnv(t, `echo ready; exec cat`)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	h := newHarness(t, e.addr, pr, true, nil)
	done := make(chan int, 1)
	go func() { done <- h.c.run([]string{"run", "claude"}) }()
	waitFor(t, "agente pronto", func() bool { return strings.Contains(h.out.String(), "ready") })
	_ = e.mgr.Kill(e.mgr.List()[0].ID)
	if code := <-done; code != 143 || !strings.Contains(h.err.String(), "encerrado pela plataforma") || !strings.HasSuffix(h.out.String(), terminalReset) {
		t.Fatalf("code=%d stderr=%q", code, h.err.String())
	}
}

func TestRunAgentSignalExitResets(t *testing.T) {
	e := newRunEnv(t, `kill -9 $$`)
	h := newHarness(t, e.addr, strings.NewReader(""), true, nil)
	if code := h.c.run([]string{"run", "claude"}); code != 137 || !strings.HasSuffix(h.out.String(), terminalReset) {
		t.Fatalf("code=%d out=%q", code, h.out.String())
	}
}

func TestRunSlowClientDisconnectsWithoutKilling(t *testing.T) {
	e := newRunEnvOpts(t, `sleep 0.5; printf '%080d' 0; sleep 30`, pty.Options{SubLimit: 16})
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	h := newHarness(t, e.addr, pr, true, nil)
	code := h.c.run([]string{"run", "claude"})
	id := e.mgr.List()[0].ID
	if code != 1 || !strings.Contains(h.err.String(), "desconectado; o agente segue na plataforma: http://"+e.addr+"/terminals?id="+id) ||
		!strings.HasSuffix(h.out.String(), terminalReset) {
		t.Fatalf("code=%d stderr=%q", code, h.err.String())
	}
	if l := e.mgr.List(); l[0].ExitCode != nil {
		t.Fatalf("não deveria matar o agente: %+v", l)
	}
}

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("saída quebrou") }

func TestRunRestoresOnGoroutinePanic(t *testing.T) {
	e := newRunEnv(t, `echo ready; exec cat`)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	h := newHarness(t, e.addr, pr, true, nil)
	h.c.out = panicWriter{}
	done := make(chan int, 1)
	go func() { done <- h.c.run([]string{"run", "claude"}) }()
	select {
	case code := <-done:
		if code != 1 || !strings.Contains(h.err.String(), "saída quebrou") || h.restoredCount() != 1 {
			t.Fatalf("code=%d stderr=%q restaurado=%d", code, h.err.String(), h.restoredCount())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("não terminou")
	}
}

func TestRunForwardsAllowedEnvOnly(t *testing.T) {
	e := newRunEnv(t, `echo "A=$ANTHROPIC_API_KEY L=$LC_ALL P=[$MY_SECRET] B=$HTTPS_PROXY"`)
	h := newHarness(t, e.addr, strings.NewReader(""), true, nil)
	h.environ = []string{"ANTHROPIC_API_KEY=k1", "LC_ALL=C", "MY_SECRET=x", "HTTPS_PROXY=http://p:1", "PATH=/evil", "LD_PRELOAD=/x", "NOBRE_FOO=1"}
	if code := h.c.run([]string{"run", "claude"}); code != 0 {
		t.Fatalf("code = %d %q", code, h.err.String())
	}
	if !strings.Contains(h.out.String(), "A=k1 L=C P=[] B=http://p:1") {
		t.Fatalf("saída = %q", h.out.String())
	}
	rd := &runDeps{environ: func() []string { return h.environ }}
	m := rd.allowedEnv()
	if len(m) != 3 || m["PATH"] != "" || m["LD_PRELOAD"] != "" || m["NOBRE_FOO"] != "" {
		t.Fatalf("allowedEnv = %v", m)
	}
}

func TestRunWarnsOnInvalidResumeKey(t *testing.T) {
	e := newRunEnv(t, `true`)
	h := newHarness(t, e.addr, strings.NewReader(""), true, map[string]string{"NOBRE_RESUME": "lixo"})
	h.c.run([]string{"run", "claude"})
	if !strings.Contains(h.err.String(), "NOBRE_RESUME inválido") {
		t.Fatalf("stderr = %q", h.err.String())
	}
}

func TestRunPassesHelpToAgent(t *testing.T) {
	h := newHarness(t, "127.0.0.1:1", strings.NewReader(""), true, nil)
	h.c.run([]string{"run", "claude", "--help"})
	if strings.Join(h.execArgv, " ") != "claude --help" {
		t.Fatalf("argv = %q", h.execArgv)
	}
}

func TestRunNeedsAgent(t *testing.T) {
	h := newHarness(t, "127.0.0.1:1", strings.NewReader(""), false, nil)
	if code := h.c.run([]string{"run"}); code != 2 {
		t.Fatalf("code = %d", code)
	}
}
