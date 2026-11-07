// Package launch inicia processos de agente fora do daemon (handoff pela UI).
package launch

import (
	"fmt"
	"os/exec"
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
	go func() { _ = cmd.Wait() }()
	return nil
}

// quote protege s para o shell com aspas simples.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TerminalArgv monta o argv que abre o agente num terminal: terminalCmd mais
// um único argumento "cd <dir> && K=V … exec <agente>". Diretório e variáveis
// vão dentro do comando porque o terminator e o gnome-terminal reaproveitam
// uma instância já aberta via DBus e descartam o cwd e o env de quem chamou.
func TerminalArgv(terminalCmd []string, dir string, env map[string]string, agentArgv []string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{"cd " + quote(dir) + " &&"}
	for _, k := range keys {
		parts = append(parts, k+"="+quote(env[k]))
	}
	parts = append(parts, "exec")
	for _, a := range agentArgv {
		parts = append(parts, quote(a))
	}
	out := append([]string{}, terminalCmd...)
	return append(out, strings.Join(parts, " "))
}
