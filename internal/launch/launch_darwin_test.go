//go:build darwin

package launch

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestOsascriptArgvCompiles compila (sem rodar) o AppleScript que o
// Continuar passa ao osascript, com aspas, apóstrofos e barras no caminho:
// um erro de escape apareceria aqui, sem abrir janela nem pedir permissão.
func TestOsascriptArgvCompiles(t *testing.T) {
	env := map[string]string{"NOBRE_RESUME": "NAC-1"}
	agent := []string{"claude", "--", `Continue o NAC-1: 'x' "y" \z`}
	if p, err := ResolveBinary("x-terminal-emulator"); err != nil || filepath.Base(p) != "osascript" {
		t.Fatalf("x-terminal-emulator no macOS = %q, %v; want osascript", p, err)
	}
	argv := TerminalArgvWith(ResolveBinary, []string{"x-terminal-emulator", "-e"}, "/usr/bin", `/tmp/a "b" 'c'`, env, agent)
	if filepath.Base(argv[0]) != "osascript" {
		t.Fatalf("argv = %q", argv)
	}
	out := filepath.Join(t.TempDir(), "x.scpt")
	cmd := exec.Command("osacompile", append([]string{"-o", out}, argv[1:]...)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("osacompile: %v\n%s\nargv = %q", err, b, argv)
	}
}
