package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"

	"github.com/nobretech/nobreai-cockpit/internal/client"
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

// run implementa `nobre run <agente> [args...]`: pede um terminal ao daemon e
// liga o terminal local a ele. Sem daemon, roda o agente direto.
func (c *cli) runAgent(args []string) (code int) {
	if len(args) == 0 {
		fmt.Fprintln(c.err, "uso: nobre run <agente> [args...]")
		return 2
	}
	agent, rest := args[0], args[1:]
	rd := c.runDeps()
	tty := rd.isTTY()
	dir, err := c.cwd()
	if err != nil {
		fmt.Fprintln(c.err, err)
		return 1
	}
	cols, rows := 0, 0
	if tty {
		cols, rows = rd.size()
	}
	body := map[string]any{"agent": agent, "dir": dir, "args": append([]string{}, rest...),
		"cols": cols, "rows": rows, "origin": "nobre run"}
	if k := c.getenv("NOBRE_RESUME"); resumeKeyRe.MatchString(k) {
		body["resume_key"] = k
	}
	cl := c.client()
	cctx, cancel := context.WithTimeout(context.Background(), runCreateLimit)
	var info struct {
		ID string `json:"id"`
	}
	err = cl.Do(cctx, "POST", "/api/terminals", body, &info)
	cancel()
	if err != nil {
		return c.runDirect(rd, agent, rest, err)
	}

	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	conn, _, err := websocket.Dial(wctx, "ws://"+c.addr()+"/api/terminals/"+url.PathEscape(info.ID)+"/ws", nil)
	if err != nil {
		// Terminal criado mas sem conexão: desfaz para não deixar um agente órfão.
		dctx, dcancel := context.WithTimeout(context.Background(), runCreateLimit)
		cl.Do(dctx, "DELETE", "/api/terminals/"+url.PathEscape(info.ID), nil, nil)
		dcancel()
		return c.runDirect(rd, agent, rest, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	// O terminal local volta ao normal em qualquer saída: retorno, pânico ou sinal.
	restore := func() {}
	if tty {
		if r, err := rd.raw(); err == nil {
			var once sync.Once
			restore = func() { once.Do(r) }
		} else {
			fmt.Fprintf(c.err, "não consegui colocar o terminal em modo raw: %v\n", err)
		}
	}
	defer restore()
	defer func() {
		if r := recover(); r != nil {
			restore()
			fmt.Fprintf(c.err, "erro inesperado no nobre run: %v\n", r)
			code = 1
		}
	}()

	exitCode := make(chan int, 1)
	readDone := make(chan struct{})
	go func() { // saída do agente → stdout
		defer close(readDone)
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
	go func() { // stdin → agente; Ctrl+] solta o terminal local
		buf := make([]byte, 4096)
		for {
			n, err := c.in.Read(buf)
			if n > 0 {
				data := buf[:n]
				for i, b := range data {
					if b == detachKey && tty {
						if i > 0 {
							conn.Write(wctx, websocket.MessageBinary, data[:i])
						}
						close(detach)
						return
					}
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

	var winch <-chan os.Signal
	if tty {
		ch, stop := rd.winch()
		defer stop()
		winch = ch
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	for {
		select {
		case <-winch:
			if w, h := rd.size(); w > 0 && h > 0 {
				m, _ := json.Marshal(map[string]any{"type": "resize", "cols": w, "rows": h})
				conn.Write(wctx, websocket.MessageText, m)
			}
		case <-detach:
			return c.detach(conn, restore, readDone, wcancel)
		case <-sigs:
			return c.detach(conn, restore, readDone, wcancel)
		case <-readDone:
			restore()
			select {
			case n := <-exitCode:
				return n
			default:
			}
			fmt.Fprintf(c.err, "\n%s\n", msgRunStopped)
			return 1
		}
	}
}

// detach fecha só a conexão: o agente segue vivo na plataforma.
func (c *cli) detach(conn *websocket.Conn, restore func(), readDone <-chan struct{}, cancel func()) int {
	conn.Close(websocket.StatusNormalClosure, "")
	cancel()
	<-readDone
	restore()
	fmt.Fprintf(c.err, "\ndesconectado; o agente segue na plataforma: http://%s/terminais\n", c.addr())
	return 0
}

// runDirect roda o agente localmente (exec), sem a plataforma.
func (c *cli) runDirect(rd *runDeps, agent string, args []string, cause error) int {
	fmt.Fprintln(c.err, msgNoPlatform)
	if ae, ok := cause.(*client.APIError); ok {
		fmt.Fprintf(c.err, "(%s)\n", ae.Message)
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
