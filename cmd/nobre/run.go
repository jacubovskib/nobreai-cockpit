package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	msgDaemonEnded = "O NobreAI Cockpit parou e encerrou a sessão; retome com `claude --continue` no mesmo diretório."
	msgKilled      = "O agente foi encerrado pela plataforma."
	runCreateLimit = 5 * time.Second
	runKillLimit   = 2 * time.Second
)

// kittyDetach é Ctrl+] no protocolo de teclado do kitty (CSI u), que o agente
// pode ter ligado no terminal local.
var kittyDetach = []byte("\x1b[93;5u")

// detachIndex devolve a posição da primeira tecla de soltar (Ctrl+] cru ou
// CSI u) no bloco lido, ou -1. Uma sequência partida entre duas leituras não
// é reconhecida.
func detachIndex(b []byte) int {
	i := bytes.IndexByte(b, detachKey)
	if j := bytes.Index(b, kittyDetach); j >= 0 && (i < 0 || j < i) {
		i = j
	}
	return i
}

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
		return c.runDirect(rd, agent, rest, "")
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
		return c.runDirect(rd, agent, rest, platformWarning(err))
	}
	termPath := "/api/terminals/" + url.PathEscape(info.ID)
	killAgent := func(limit time.Duration) error {
		dctx, dcancel := context.WithTimeout(context.Background(), limit)
		defer dcancel()
		return cl.Do(dctx, "DELETE", termPath, nil, nil)
	}

	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	conn, _, err := websocket.Dial(wctx, "ws://"+c.addr()+termPath+"/ws", nil)
	if err != nil {
		// Terminal criado mas sem conexão: desfaz para não deixar um agente órfão.
		if derr := killAgent(runCreateLimit); derr != nil {
			fmt.Fprintf(c.err, "não consegui desfazer o terminal %s: %v\n", info.ID, derr)
		}
		return c.runDirect(rd, agent, rest, refusedWarning(err.Error()))
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
	type exitMsg struct {
		Code   int    `json:"code"`
		Reason string `json:"reason"`
	}
	exited := make(chan exitMsg, 1)
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
				exitMsg
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "exit" {
				select {
				case exited <- m.exitMsg:
				default:
				}
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
				if i := detachIndex(data); i >= 0 {
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

	detachedMsg := fmt.Sprintf("desconectado; o agente segue na plataforma: http://%s/terminals?id=%s", c.addr(), url.QueryEscape(info.ID))
	// hangUp corta a conexão e espera o leitor parar: depois dela nenhum byte
	// do agente chega ao stdout, então o reset de leave fica por último.
	hangUp := func() {
		conn.CloseNow()
		wcancel()
		<-readDone
	}

	for {
		select {
		case <-winch:
			if w, h := rd.size(); w > 0 && h > 0 {
				m, _ := json.Marshal(map[string]any{"type": "resize", "cols": w, "rows": h})
				conn.Write(wctx, websocket.MessageText, m)
			}
		case <-detach: // Ctrl+]: solta só o terminal local; o agente segue
			hangUp()
			leave(true)
			fmt.Fprintf(c.err, "\n%s\n", detachedMsg)
			return 0
		case sig := <-sigs:
			// Janela fechada (SIGHUP), kill (SIGTERM) ou SIGINT/SIGQUIT de fora
			// (no modo raw o Ctrl+C vai como tecla): o agente morre junto.
			n := 1
			if ss, ok := sig.(syscall.Signal); ok {
				n = int(ss)
			}
			kerr := killAgent(runKillLimit)
			hangUp()
			leave(true)
			if kerr != nil {
				fmt.Fprintf(c.err, "\nnão consegui encerrar o agente (%v); ele segue na plataforma: http://%s/terminals?id=%s\n", kerr, c.addr(), url.QueryEscape(info.ID))
			}
			return 128 + n
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
			case m := <-exited:
				// O agente pode ter deixado modos de TUI ligados se não saiu
				// sozinho e limpo.
				leave(m.Code >= 128 || m.Reason != pty.EndAgent)
				switch m.Reason {
				case pty.EndDaemon:
					fmt.Fprintf(c.err, "\n%s\n", msgDaemonEnded)
				case pty.EndKilled:
					fmt.Fprintf(c.err, "\n%s\n", msgKilled)
				}
				return m.Code
			default:
			}
			// Fechou sem mensagem de fim (cliente lento, queda da conexão): o
			// agente segue no daemon.
			leave(true)
			fmt.Fprintf(c.err, "\n%s\n", detachedMsg)
			return 1
		}
	}
}

// platformWarning: "fora do ar" só quando não houve resposta do daemon; se
// ele respondeu e recusou (409, 400…), mostra o motivo.
func platformWarning(err error) string {
	var ae *client.APIError
	if errors.As(err, &ae) {
		return refusedWarning(ae.Message)
	}
	return msgNoPlatform
}

func refusedWarning(reason string) string {
	return "Não consegui abrir na plataforma (" + reason + "): rodando sem ela"
}

// runDirect roda o agente localmente (exec), sem a plataforma; warn (se não
// vazio) vai para o stderr antes.
func (c *cli) runDirect(rd *runDeps, agent string, args []string, warn string) int {
	if warn != "" {
		fmt.Fprintln(c.err, warn)
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
