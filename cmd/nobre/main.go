// Comando nobre: CLI do NobreAI Cockpit para agentes, hooks e administração.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/client"
)

const defaultAddr = "127.0.0.1:7420"

const usage = `nobre: CLI do NobreAI Cockpit

Agente (use dentro de uma sessão de agente):
  nobre start "<título>" [--goal ...] [--done-when ...]   cria um card e liga esta sessão
  nobre resume <KEY> [--force]                            liga esta sessão ao card e imprime o pacote
  nobre note "<onde parei>"                               atualiza "Onde parei"
  nobre decide "<decisão>" --why "<porquê>"               registra uma decisão
  nobre file <path> --why "<motivo>" [--lines L12-96]     marca um arquivo essencial
  nobre todo add "<texto>"                                adiciona uma pendência
  nobre todo done <id>                                    conclui uma pendência
  nobre ask "<pergunta>"                                  registra uma pergunta em aberto
  nobre test "<como testar>"                              registra como testar
  nobre done                                              marca o card como pronto para revisão
  nobre status                                            mostra o card e a prontidão do handoff

Administração:
  nobre project add [path] [--name <nome>]                cadastra um projeto (padrão: diretório atual)
  nobre project list                                      lista os projetos
  nobre prompt list                                       lista os prompts
  nobre prompt use <slug> [--card KEY]                    imprime o prompt preenchido e marca o uso
  nobre prompt save --name <n> --tag <t>                  salva um prompt (corpo pelo stdin)
  nobre ui                                                abre o Cockpit no navegador

Hooks (chamados pelo agente, nunca falham):
  nobre hook <agente> <Evento>

O daemon é achado em NOBRE_ADDR (padrão ` + defaultAddr + `).
`

// cli reúne as dependências do programa, para os testes injetarem.
type cli struct {
	in     io.Reader
	out    io.Writer
	err    io.Writer
	getenv func(string) string
	cwd    func() (string, error)
	opener func(url string) error
}

func newCLI() *cli {
	return &cli{in: os.Stdin, out: os.Stdout, err: os.Stderr, getenv: os.Getenv, cwd: os.Getwd,
		opener: func(url string) error { return exec.Command("xdg-open", url).Start() }}
}

func main() { os.Exit(newCLI().run(os.Args[1:])) }

func (c *cli) addr() string {
	if a := c.getenv("NOBRE_ADDR"); a != "" {
		return a
	}
	return defaultAddr
}

func (c *cli) run(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(c.out, usage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	if cmd == "hook" {
		return c.hook(rest)
	}
	var err error
	switch cmd {
	case "start":
		err = c.start(rest)
	case "resume":
		err = c.resume(rest)
	case "note":
		err = c.simple("note", "texto", rest, "text")
	case "ask":
		err = c.simple("ask", "pergunta", rest, "text")
	case "test":
		err = c.simple("test", "como testar", rest, "text")
	case "decide":
		err = c.decide(rest)
	case "file":
		err = c.file(rest)
	case "todo":
		err = c.todo(rest)
	case "done":
		err = c.done()
	case "status":
		err = c.status()
	case "project":
		err = c.project(rest)
	case "prompt":
		err = c.prompt(rest)
	case "ui":
		err = c.ui()
	default:
		fmt.Fprintf(c.err, "comando desconhecido: %s\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		var ue *usageError
		if errors.As(err, &ue) {
			fmt.Fprintln(c.err, ue.Error())
			return 2
		}
		fmt.Fprintln(c.err, err.Error())
		return 1
	}
	return 0
}

// usageError é erro de uso do comando (argumento faltando), não da API.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return &usageError{fmt.Sprintf(format, a...)} }

func (c *cli) client() *client.Client { return client.New(c.addr()) }

func ctx() context.Context { return context.Background() }

// parse aceita flags antes, depois e entre os argumentos posicionais.
func (c *cli) parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usagef("%s: %v", fs.Name(), err)
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func joinText(pos []string) string { return strings.TrimSpace(strings.Join(pos, " ")) }
