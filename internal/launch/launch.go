// Package launch inicia processos de agente fora do daemon (handoff pela UI).
package launch

import (
	"fmt"
	"log"
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
type Exec struct{}

var _ Launcher = Exec{}

// Start executa argv sem esperar. O término é recolhido em segundo plano
// para não deixar zumbi.
func (Exec) Start(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("comando vazio")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
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

// Script monta o comando de shell que roda o agente: entra em dir, define as
// variáveis e roda o agente (sem exec, para a janela avisar quando ele sai
// com erro, ou quando o cd falha, e esperar Enter antes de fechar).
func Script(dir string, env map[string]string, agentArgv []string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{"cd " + quote(dir) + " &&"}
	for _, k := range keys {
		parts = append(parts, k+"="+quote(env[k]))
	}
	for _, a := range agentArgv {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ") + `; s=$?; if [ $s -ne 0 ]; then printf '%s\n' "NobreAI Cockpit: o agente saiu com código $s (Enter fecha)"; read -r _; fi`
}

// TerminalArgv monta o argv que abre o agente num terminal (ResolveBinary).
func TerminalArgv(terminalCmd []string, dir string, env map[string]string, agentArgv []string) []string {
	return TerminalArgvWith(ResolveBinary, terminalCmd, dir, env, agentArgv)
}

// TerminalArgvWith resolve terminalCmd[0] e, pelo nome do binário, usa o
// formato de execução de comando do terminal: terminator aceita -e com uma
// string (rodada por $SHELL -c, também na partida a frio); gnome-terminal usa
// "-- sh -c"; konsole, xterm, xfce4-terminal, alacritty e kitty usam
// "-e sh -c". Desconhecido: terminalCmd + o script como último argumento.
// Diretório e variáveis vão dentro do script porque o terminator e o
// gnome-terminal reaproveitam uma instância aberta via DBus e descartam o
// cwd e o env de quem chamou.
func TerminalArgvWith(resolve Resolver, terminalCmd []string, dir string, env map[string]string, agentArgv []string) []string {
	script := Script(dir, env, agentArgv)
	if len(terminalCmd) > 0 {
		if bin, err := resolve(terminalCmd[0]); err == nil {
			base := filepath.Base(bin)
			switch {
			case base == "terminator":
				// O terminator roda a string com $SHELL -c; sh -c explícito
				// vale mesmo se o shell do usuário for fish etc.
				return []string{bin, "-e", "/bin/sh -c " + quote(script)}
			case strings.HasPrefix(base, "gnome-terminal"):
				return []string{bin, "--", "/bin/sh", "-c", script}
			case base == "konsole", base == "xterm", base == "xfce4-terminal", base == "alacritty", base == "kitty":
				return []string{bin, "-e", "/bin/sh", "-c", script}
			}
		}
	}
	return append(append([]string{}, terminalCmd...), script)
}
