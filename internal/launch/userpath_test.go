package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeShell grava um "shell" (script sh) com o nome name que roda body.
func fakeShell(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUserPATHReadsLoginShellAtUseTime(t *testing.T) {
	sh := fakeShell(t, "zsh", `printf 'ruído do rc\n__NOBRE_PATH__/velho\n'`)
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin:/bin", NobreDir: "/opt/nobre/bin", Timeout: 2 * time.Second}
	if got, want := u.Get(0), "/opt/nobre/bin:/velho:/usr/bin:/bin"; got != want {
		t.Fatalf("primeiro: %q, quer %q", got, want)
	}
	// O usuário trocou a versão do node: sem reiniciar o daemon, a próxima
	// leitura fresca vê o PATH novo; a de cache, não.
	os.WriteFile(sh, []byte("#!/bin/sh\nprintf '__NOBRE_PATH__/novo:/usr/bin\\nfim do rc\\n'\n"), 0o755)
	if got := u.Get(time.Hour); !strings.Contains(got, "/velho") {
		t.Fatalf("cache: %q", got)
	}
	if got, want := u.Get(0), "/opt/nobre/bin:/novo:/usr/bin:/bin"; got != want {
		t.Fatalf("fresco: %q, quer %q", got, want)
	}
}

func TestUserPATHFallsBackAndDoesNotCacheFailure(t *testing.T) {
	cases := map[string]string{
		"sem marcador":   `echo oi`,
		"marcador vazio": `printf '__NOBRE_PATH__\n'`,
		"saída com erro": `printf '__NOBRE_PATH__/x\n'; exit 3`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			u := &UserPATH{Shell: fakeShell(t, "bash", body), Fallback: "/usr/bin", NobreDir: "/n", Timeout: 2 * time.Second}
			if got := u.Get(time.Hour); got != "/n:/usr/bin" {
				t.Fatalf("%q", got)
			}
		})
	}
	sh := fakeShell(t, "bash", `echo oi`)
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: 2 * time.Second}
	u.Get(time.Hour)
	os.WriteFile(sh, []byte("#!/bin/sh\nprintf '__NOBRE_PATH__/agora\\n'\n"), 0o755)
	if got := u.Get(time.Hour); got != "/agora:/usr/bin" {
		t.Fatalf("falha não pode ficar em cache: %q", got)
	}
}

// Um rc que deixa um filho segurando o stdout não pode travar o daemon.
func TestUserPATHTimeoutKillsHangingShell(t *testing.T) {
	sh := fakeShell(t, "zsh", `sleep 30 & printf '__NOBRE_PATH__/x\n'; sleep 30`)
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: 300 * time.Millisecond}
	start := time.Now()
	got := u.Get(0)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("demorou %v", d)
	}
	if got != "/usr/bin" {
		t.Fatalf("timeout deveria cair no fallback: %q", got)
	}
}

func TestUserPATHSkipsUnknownShells(t *testing.T) {
	for _, name := range []string{"fish", "csh", "sh"} {
		sh := fakeShell(t, name, `printf '__NOBRE_PATH__/x\n'`)
		u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: time.Second}
		if got := u.Get(0); got != "/usr/bin" {
			t.Fatalf("%s: %q", name, got)
		}
	}
	if got := (&UserPATH{Fallback: "/usr/bin"}).Get(0); got != "/usr/bin" {
		t.Fatalf("sem shell: %q", got)
	}
}

func TestLookPathIn(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "claude")
	os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "naoexec"), []byte("x"), 0o644)
	if got := LookPathIn("claude", "/nao/existe:"+dir); got != exe {
		t.Fatalf("%q", got)
	}
	if got := LookPathIn("naoexec", dir); got != "" {
		t.Fatalf("não executável: %q", got)
	}
	if got := LookPathIn("claude", ":relativo"); got != "" {
		t.Fatalf("diretório relativo: %q", got)
	}
}
