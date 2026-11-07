// Package launch inicia processos de agente fora do daemon (handoff pela UI).
package launch

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// Launcher inicia um processo e o solta do daemon.
type Launcher interface {
	Start(argv []string) error
}

// Spec descreve o processo a iniciar.
type Spec struct {
	Argv []string
}

// Exec inicia o processo com exec.Command, em uma sessão própria (Setsid), e
// o solta: o daemon não espera pelo término nem herda seus sinais.
type Exec struct {
	// ShowEnv devolve a saída de `systemctl --user show-environment` (nil =
	// roda o systemctl de verdade). Injetável em testes.
	ShowEnv func() ([]byte, error)
}

// graphicalVars são as variáveis que o terminal precisa para abrir janela.
var graphicalVars = []string{"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS"}

// withGraphicalEnv completa env com as variáveis gráficas que faltam, lidas
// de show-environment (o daemon do systemd pode ter subido antes do login
// gráfico). Falha ao consultar deixa env como está.
func withGraphicalEnv(env []string, show func() ([]byte, error)) []string {
	have := map[string]bool{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && v != "" {
			have[k] = true
		}
	}
	missing := false
	for _, k := range graphicalVars {
		if !have[k] {
			missing = true
		}
	}
	if !missing {
		return env
	}
	if show == nil {
		show = func() ([]byte, error) { return exec.Command("systemctl", "--user", "show-environment").Output() }
	}
	out, err := show()
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok || v == "" || have[k] {
			continue
		}
		for _, g := range graphicalVars {
			if k == g {
				env = append(env, k+"="+v)
				have[k] = true
			}
		}
	}
	return env
}

var _ Launcher = Exec{}

// Start executa argv sem esperar. O término é recolhido em segundo plano
// para não deixar zumbi.
func (e Exec) Start(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("comando vazio")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = withGraphicalEnv(os.Environ(), e.ShowEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("launch: %s terminou com erro: %v", argv[0], err)
		}
	}()
	return nil
}

// quote protege s para o shell com aspas simples.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Resolver resolve o comando do terminal para o caminho real do binário.
type Resolver func(name string) (string, error)

// ResolveBinary acha name no PATH e segue os symlinks (x-terminal-emulator →
// /usr/bin/terminator, por exemplo).
func ResolveBinary(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return p, nil
}

// PathWithDir põe dir no começo de path (lista separada por ":"), se ainda
// não estiver nela.
func PathWithDir(path, dir string) string {
	if dir == "" {
		return path
	}
	if path == "" {
		return dir
	}
	for _, d := range strings.Split(path, ":") {
		if d == dir {
			return path
		}
	}
	return dir + ":" + path
}

// Script monta o comando de shell que roda o agente: exporta path (se não
// vazio; a janela reaproveitada do terminator tem o PATH da instância já
// aberta), entra em dir, define as variáveis e roda o agente (sem exec, para
// a janela avisar quando ele sai com erro, ou quando o cd falha, e esperar
// Enter antes de fechar).
func Script(path, dir string, env map[string]string, agentArgv []string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	if path != "" {
		parts = append(parts, "export PATH="+quote(path)+";")
	}
	parts = append(parts, "cd "+quote(dir)+" &&")
	for _, k := range keys {
		parts = append(parts, k+"="+quote(env[k]))
	}
	for _, a := range agentArgv {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ") + `; s=$?; if [ $s -ne 0 ]; then printf '%s\n' "NobreAI Cockpit: o agente saiu com código $s (Enter fecha)"; read -r _; fi`
}

// TerminalArgv monta o argv que abre o agente num terminal (ResolveBinary).
func TerminalArgv(terminalCmd []string, path, dir string, env map[string]string, agentArgv []string) []string {
	return TerminalArgvWith(ResolveBinary, terminalCmd, path, dir, env, agentArgv)
}

// TerminalArgvWith resolve terminalCmd[0] e, pelo nome do binário, usa o
// formato de execução de comando do terminal: terminator aceita -e com uma
// string (rodada por $SHELL -c, também na partida a frio); gnome-terminal usa
// "-- sh -c" (o wrapper da Debian usa -e); xfce4-terminal usa "-x sh -c";
// konsole, xterm, alacritty e kitty usam "-e sh -c". Desconhecido: terminalCmd + o script como último argumento.
// PATH, diretório e variáveis vão dentro do script porque o terminator e o
// gnome-terminal reaproveitam uma instância aberta via DBus e descartam o
// cwd e o env de quem chamou.
func TerminalArgvWith(resolve Resolver, terminalCmd []string, path, dir string, env map[string]string, agentArgv []string) []string {
	script := Script(path, dir, env, agentArgv)
	if len(terminalCmd) > 0 {
		if bin, err := resolve(terminalCmd[0]); err == nil {
			base := filepath.Base(bin)
			switch {
			case base == "terminator":
				// O terminator roda a string com $SHELL -c; o sh -c explícito
				// é seguro com shells de login POSIX (bash/zsh/dash). Shells
				// fish/csh não são suportados no piloto.
				return []string{bin, "-e", "/bin/sh -c " + quote(script)}
			case base == "gnome-terminal.wrapper":
				// Shim Perl da Debian, estilo xterm: descarta "--".
				return []string{bin, "-e", "/bin/sh", "-c", script}
			case base == "xfce4-terminal":
				// O -e do xfce4-terminal recebe UMA string; -x recebe argv.
				return []string{bin, "-x", "/bin/sh", "-c", script}
			case strings.HasPrefix(base, "gnome-terminal"):
				return []string{bin, "--", "/bin/sh", "-c", script}
			case base == "konsole", base == "xterm", base == "alacritty", base == "kitty":
				return []string{bin, "-e", "/bin/sh", "-c", script}
			}
		}
	}
	return append(append([]string{}, terminalCmd...), script)
}
