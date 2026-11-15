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
	"regexp"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/client"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
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
  nobre branch <nome>                                     informa o branch de trabalho do card
  nobre answer <id> "<resposta>"                          responde uma pergunta do card
  nobre test "<como testar>"                              registra como testar
  nobre done [--force]                                    marca o card como pronto para revisão
  nobre status                                            mostra o card e a prontidão do handoff

Administração:
  nobre project add [path] [--name <nome>]                cadastra um projeto (padrão: diretório atual)
  nobre project base <branch> [path]                      define o branch base do projeto
  nobre project list                                      lista os projetos
  nobre prompt list                                       lista os prompts
  nobre prompt use <slug> [--card KEY]                    imprime o prompt preenchido e marca o uso
  nobre prompt save --name <n> --tag <t>                  salva um prompt (corpo pelo stdin)
  nobre run <agente> [args...]                            roda o agente com o terminal visível na plataforma (Ctrl+] solta)
  nobre ui                                                abre o Cockpit no navegador
  nobre setup [--dry-run] [--sem-statusline]              instala hooks, skill e status line do Claude Code e ativa o daemon
  nobre setup --remover-statusline [--dry-run]            só remove a integração da status line (devolve o comando original)
  nobre statusline [--resumo]                             status line do Claude Code: repassa o stdin e envia os limites do plano

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
	// sd são os caminhos e comandos do setup; nil = padrões do usuário.
	sd *setupDeps
	// rd são os pontos do terminal local do `nobre run`; nil = padrões.
	rd *runDeps
	// spawn entrega o payload dos limites a um processo destacado; nil = envia
	// na hora, com 300 ms (testes).
	spawn func(payload []byte)
	// git acha o repositório principal de um diretório; nil = não resolve.
	git gitinfo.WorktreeReader
}

func newCLI() *cli {
	c := &cli{in: os.Stdin, out: os.Stdout, err: os.Stderr, getenv: os.Getenv, cwd: os.Getwd, git: gitinfo.Exec{},
		opener: func(url string) error { return exec.Command("xdg-open", url).Start() }}
	c.spawn = func(p []byte) { spawnStatuslineSender(c.addr())(p) }
	return c
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
	if cmd == "statusline" && !wantsHelp(rest) {
		return c.statusline(rest)
	}
	if cmd == "run" && (len(rest) == 0 || rest[0] != "-h" && rest[0] != "--help") {
		return c.runAgent(rest) // os argumentos depois do agente são do agente, inclusive --help
	}
	if wantsHelp(rest) {
		fmt.Fprint(c.out, helpFor(cmd))
		return 0
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
		err = c.ask(rest)
	case "branch":
		err = c.branch(rest)
	case "answer":
		err = c.answer(rest)
	case "test":
		err = c.simple("test", "como testar", rest, "text")
	case "decide":
		err = c.decide(rest)
	case "file":
		err = c.file(rest)
	case "todo":
		err = c.todo(rest)
	case "done":
		err = c.done(rest)
	case "status":
		err = c.status()
	case "project":
		err = c.project(rest)
	case "prompt":
		err = c.prompt(rest)
	case "ui":
		err = c.ui()
	case "setup":
		err = c.setup(rest)
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

// parse separa flags e argumentos posicionais. Flags conhecidas valem em
// qualquer posição (antes ou depois do texto); "--" encerra as flags. Um
// argumento que não é uma flag declarada é texto: "- item" e "-x" ficam como
// posicionais; só "--nome" desconhecido é erro (provável erro de digitação,
// use "--" para um texto que comece assim). Sem flags declaradas, tudo é
// texto (nobre note "--x"), salvo um "--" inicial (só o inicial), que é descartado.
func (c *cli) parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	known := map[string]*flag.Flag{}
	fs.VisitAll(func(f *flag.Flag) { known[f.Name] = f })
	if len(known) == 0 {
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		return append([]string(nil), args...), nil
	}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		dashes := len(a) - len(name)
		val, hasVal := "", false
		if k := strings.IndexByte(name, '='); k >= 0 {
			name, val, hasVal = name[:k], name[k+1:], true
		}
		f, ok := known[name]
		if !ok || dashes > 2 {
			if dashes == 2 && flagName.MatchString(name) {
				return nil, usagef("%s: flag desconhecida: --%s", fs.Name(), name)
			}
			pos = append(pos, a)
			continue
		}
		if bf, isBool := f.Value.(interface{ IsBoolFlag() bool }); isBool && bf.IsBoolFlag() {
			if !hasVal {
				val = "true"
			}
		} else if !hasVal {
			if i+1 >= len(args) {
				return nil, usagef("%s: a flag --%s precisa de um valor", fs.Name(), name)
			}
			i++
			val = args[i]
		}
		if err := fs.Set(name, val); err != nil {
			return nil, usagef("%s: valor inválido para --%s: %v", fs.Name(), name, err)
		}
	}
	return pos, nil
}

var flagName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

func joinText(pos []string) string { return strings.TrimSpace(strings.Join(pos, " ")) }

// wantsHelp diz se há -h/--help antes de um "--" (depois dele é texto).
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "-h", "--help", "-help":
			return true
		}
	}
	return false
}

// helpFor devolve as linhas de uso do comando, tiradas do texto de usage.
func helpFor(cmd string) string {
	var b strings.Builder
	for _, l := range strings.Split(usage, "\n") {
		if t := strings.TrimSpace(l); t == "nobre "+cmd || strings.HasPrefix(t, "nobre "+cmd+" ") {
			b.WriteString("  " + t + "\n")
		}
	}
	if b.Len() == 0 {
		return usage
	}
	return "Uso:\n" + b.String()
}
