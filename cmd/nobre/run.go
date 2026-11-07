package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"

	"github.com/nobretech/nobreai-cockpit/internal/client"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
)

const (
	detachKey      = 0x1d // Ctrl+]
	msgNoPlatform  = "NobreAI Cockpit fora do ar: rodando sem a plataforma"
	msgRunStopped  = "NobreAI Cockpit parou: a sessão do agente foi encerrada."
	runCreateLimit = 5 * time.Second
)

var resumeKeyRe = regexp.MustCompile(`^NAC-[0-9]+$`)

// runDeps são os pontos do terminal local que os testes substituem.
type runDeps struct {
	isTTY    func() bool
	size     func() (cols, rows int)
	raw      func() (restore func(), err error)
	winch    func() (<-chan os.Signal, func())
	signals  func() (<-chan os.Signal, func())
	environ  func() []string
	lookPath func(string) (string, error)
	exec     func(path string, argv []string) error
}

func defaultRunDeps() *runDeps {
	return &runDeps{
		isTTY: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) },
		size: func() (int, int) {
			w, h, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil {
				return 0, 0
			}
			return w, h
		},
		raw: func() (func(), error) {
			old, err := term.MakeRaw(int(os.Stdin.Fd()))
			if err != nil {
				return nil, err
			}
			return func() { term.Restore(int(os.Stdin.Fd()), old) }, nil
		},
		winch: func() (<-chan os.Signal, func()) {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGWINCH)
			return ch, func() { signal.Stop(ch) }
		},
		signals: func() (<-chan os.Signal, func()) {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
			return ch, func() { signal.Stop(ch) }
		},
		lookPath: exec.LookPath,
		exec: func(path string, argv []string) error {
			return syscall.Exec(path, argv, os.Environ())
		},
	}
}

func (c *cli) runDeps() *runDeps {
	if c.rd != nil {
		return c.rd
	}
	return defaultRunDeps()
}

// terminalReset desliga os modos da TUI do agente (tela alternativa, paste
// entre colchetes, mouse, teclado kitty), mostra o cursor e zera atributos.
const terminalReset = "\x1b[?1049l\x1b[?2004l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25h\x1b[<u\x1b[0m"

// allowedEnv são as variáveis do ambiente local repassadas ao terminal.
func (rd *runDeps) allowedEnv() map[string]string {
	environ := rd.environ
	if environ == nil {
		environ = os.Environ
	}
	out := map[string]string{}
	for _, kv := range environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && pty.AllowedEnv(k) {
			out[k] = v
		}
	}
	return out
}

// safeOut escreve no stdout sem deixar um pânico do escritor escapar.
func (c *cli) safeOut(b []byte) {
	defer func() { recover() }()
	c.out.Write(b)
}

// runAgent implementa `nobre run <agente> [args...]`: pede um terminal ao
// daemon e liga o terminal local a ele. Sem TTY (pipes) ou sem daemon, roda o
// agente direto.
func (c *cli) runAgent(args []string) (code int) {
	if len(args) == 0 {
		fmt.Fprintln(c.err, "uso: nobre run <agente> [args...]")
		return 2
	}
	agent, rest := args[0], args[1:]
	rd := c.runDeps()
	if !rd.isTTY() { // pipes: transparente, sem plataforma e sem aviso
		return c.runDirect(rd, agent, rest, nil, false)
	}
	dir, err := c.cwd()
	if err != nil {
		fmt.Fprintln(c.err, err)
		return 1
	}
	cols, rows := rd.size()
	body := map[string]any{"agent": agent, "dir": dir, "args": append([]string{}, rest...),
		"cols": cols, "rows": rows, "origin": "nobre run", "env": rd.allowedEnv()}
	if k := c.getenv("NOBRE_RESUME"); k != "" {
		if resumeKeyRe.MatchString(k) {
			body["resume_key"] = k
		} else {
			fmt.Fprintf(c.err, "NOBRE_RESUME inválido (%q), ignorando\n", k)
		}
	}
	cl := c.client()
	cctx, cancel := context.WithTimeout(context.Background(), runCreateLimit)
	var info struct {
		ID string `json:"id"`
	}
	err = cl.Do(cctx, "POST", "/api/terminals", body, &info)
	cancel()
	if err != nil {
		return c.runDirect(rd, agent, rest, err, true)
	}

	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	conn, _, err := websocket.Dial(wctx, "ws://"+c.addr()+"/api/terminals/"+url.PathEscape(info.ID)+"/ws", nil)
	if err != nil {
		// Terminal criado mas sem conexão: desfaz para não deixar um agente órfão.
		dctx, dcancel := context.WithTimeout(context.Background(), runCreateLimit)
		if derr := cl.Do(dctx, "DELETE", "/api/terminals/"+url.PathEscape(info.ID), nil, nil); derr != nil {
			fmt.Fprintf(c.err, "não consegui desfazer o terminal %s: %v\n", info.ID, derr)
		}
		dcancel()
		return c.runDirect(rd, agent, rest, err, true)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	// O terminal local volta ao normal em qualquer saída: retorno, pânico ou
	// sinal. leave restaura o termios e, se o agente não saiu normalmente,
	// zera os modos de TUI que ele deixou ligados.
	restoreRaw := func() {}
	if r, err := rd.raw(); err == nil {
		restoreRaw = r
	} else {
		fmt.Fprintf(c.err, "não consegui colocar o terminal em modo raw: %v\n", err)
	}
	var once sync.Once
	leave := func(reset bool) {
		once.Do(func() {
			restoreRaw()
			if reset {
				c.safeOut([]byte(terminalReset))
			}
		})
	}
	defer leave(true)
	defer func() {
		if r := recover(); r != nil {
			leave(true)
			fmt.Fprintf(c.err, "erro inesperado no nobre run: %v\n", r)
			code = 1
		}
	}()

	fatal := make(chan any, 2)
	guard := func() {
		if r := recover(); r != nil {
			leave(true)
			fatal <- r
		}
	}
	exitCode := make(chan int, 1)
	readDone := make(chan struct{})
	go func() { // saída do agente -> stdout
		defer close(readDone)
		defer guard()
		for {
			typ, data, err := conn.Read(wctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				c.out.Write(data)
				continue
			}
			var m struct {
				Type string `json:"type"`
				Code int    `json:"code"`
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "exit" {
				exitCode <- m.Code
			}
		}
	}()

	detach := make(chan struct{})
	go func() { // stdin -> agente; Ctrl+] solta o terminal local
		defer guard()
		buf := make([]byte, 4096)
		for {
			n, err := c.in.Read(buf)
			if n > 0 {
				data := buf[:n]
				if i := bytes.IndexByte(data, detachKey); i >= 0 {
					if i > 0 {
						conn.Write(wctx, websocket.MessageBinary, data[:i])
					}
					close(detach)
					return
				}
				if conn.Write(wctx, websocket.MessageBinary, data) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	winch, stopWinch := rd.winch()
	defer stopWinch()
	sigs, stopSigs := rd.signals()
	defer stopSigs()

	// release solta só a conexão: o agente segue vivo na plataforma. O termios
	// é restaurado antes do handshake de fechamento, que tem prazo curto.
	release := func(code int) int {
		leave(true)
		fmt.Fprintf(c.err, "\ndesconectado; o agente segue na plataforma: http://%s/terminais\n", c.addr())
		closed := make(chan struct{})
		go func() { conn.Close(websocket.StatusNormalClosure, ""); close(closed) }()
		select {
		case <-closed:
		case <-time.After(500 * time.Millisecond):
			conn.CloseNow()
		}
		wcancel()
		<-readDone
		return code
	}

	for {
		select {
		case <-winch:
			if w, h := rd.size(); w > 0 && h > 0 {
				m, _ := json.Marshal(map[string]any{"type": "resize", "cols": w, "rows": h})
				conn.Write(wctx, websocket.MessageText, m)
			}
		case <-detach:
			return release(0)
		case sig := <-sigs:
			n := 1
			if ss, ok := sig.(syscall.Signal); ok {
				n = int(ss)
			}
			return release(128 + n)
		case r := <-fatal:
			fmt.Fprintf(c.err, "\nerro inesperado no nobre run: %v\n", r)
			conn.CloseNow()
			wcancel()
			return 1
		case <-readDone:
			select {
			case r := <-fatal:
				fmt.Fprintf(c.err, "\nerro inesperado no nobre run: %v\n", r)
				return 1
			default:
			}
			select {
			case n := <-exitCode:
				leave(false)
				return n
			default:
			}
			leave(true)
			fmt.Fprintf(c.err, "\n%s\n", msgRunStopped)
			return 1
		}
	}
}

// runDirect roda o agente localmente (exec), sem a plataforma.
func (c *cli) runDirect(rd *runDeps, agent string, args []string, cause error, warn bool) int {
	if warn {
		fmt.Fprintln(c.err, msgNoPlatform)
		if ae, ok := cause.(*client.APIError); ok {
			fmt.Fprintf(c.err, "(%s)\n", ae.Message)
		}
	}
	path, err := rd.lookPath(agent)
	if err != nil {
		fmt.Fprintf(c.err, "não encontrei o comando %s: %v\n", agent, err)
		return 127
	}
	err = rd.exec(path, append([]string{agent}, args...))
	fmt.Fprintf(c.err, "não consegui executar %s: %v\n", path, err)
	return 1
}
