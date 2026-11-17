package launch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: 2 * time.Second, FailTTL: time.Nanosecond}
	u.Get(time.Hour)
	os.WriteFile(sh, []byte("#!/bin/sh\nprintf '__NOBRE_PATH__/agora\\n'\n"), 0o755)
	time.Sleep(time.Millisecond)
	if got := u.Get(time.Hour); got != "/agora:/usr/bin" {
		t.Fatalf("depois do FailTTL o shell é consultado de novo: %q", got)
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

// Um rc que deixa um filho em segundo plano com o stdout herdado não pode
// derrubar um marcador válido (o WaitDelay fecha os pipes), e o filho morre.
func TestUserPATHAcceptsMarkerWhenRcLeavesBackgroundChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	sh := fakeShell(t, "zsh", `sleep 30 & echo $! > `+pidFile+`; printf '__NOBRE_PATH__/bom\n'`)
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: 3 * time.Second}
	start := time.Now()
	if got := u.Get(0); got != "/bom:/usr/bin" {
		t.Fatalf("%q", got)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("demorou %v", d)
	}
	b, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(2 * time.Second)
	for pid > 0 && syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("o filho %d do rc ficou vivo", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Falha depois de um sucesso: vale o último PATH bom, não o mínimo; e a
// falha fica guardada por FailTTL (sem rodar o shell a cada chamada).
func TestUserPATHFailureKeepsLastGoodAndIsRemembered(t *testing.T) {
	count := filepath.Join(t.TempDir(), "n")
	sh := fakeShell(t, "bash", `echo x >> `+count+`; printf '__NOBRE_PATH__/bom\n'`)
	var changes []string
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: 2 * time.Second, FailTTL: time.Hour,
		OnChange: func(p string) { changes = append(changes, p) }}
	if got := u.Get(0); got != "/bom:/usr/bin" {
		t.Fatalf("%q", got)
	}
	os.WriteFile(sh, []byte("#!/bin/sh\necho x >> "+count+"\nexit 1\n"), 0o755)
	for i := 0; i < 3; i++ {
		if got := u.Get(0); got != "/bom:/usr/bin" {
			t.Fatalf("falha deveria manter o último bom: %q", got)
		}
	}
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "x"); n != 2 {
		t.Fatalf("shell rodou %d vezes; quer 2 (sucesso + 1 falha guardada)", n)
	}
	if len(changes) != 1 || changes[0] != "/bom:/usr/bin" {
		t.Fatalf("OnChange = %v", changes)
	}
}

// Chamadas simultâneas esperam um único shell, e o lock não fica preso
// enquanto ele roda (uma leitura com cache válido responde na hora).
func TestUserPATHSingleFlight(t *testing.T) {
	count := filepath.Join(t.TempDir(), "n")
	sh := fakeShell(t, "zsh", `echo x >> `+count+`; sleep 0.4; printf '__NOBRE_PATH__/um\n'`)
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: 3 * time.Second}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); u.Get(0) }()
	}
	wg.Wait()
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "x"); n != 1 {
		t.Fatalf("shell rodou %d vezes", n)
	}
	go u.Get(0) // probe novo em andamento...
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	if got := u.Get(time.Hour); got != "/um:/usr/bin" || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("cache bloqueado pelo probe: %q em %v", got, time.Since(start))
	}
}

func TestUserPATHHugeLineBeforeMarker(t *testing.T) {
	sh := fakeShell(t, "bash", `head -c 2000000 /dev/zero | tr '\0' a; printf '\n__NOBRE_PATH__/x\n'`)
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin", Timeout: 3 * time.Second}
	if got := u.Get(0); got != "/x:/usr/bin" {
		t.Fatalf("%q", got)
	}
}

// O shell parte do PATH de reserva, não do PATH do daemon (que o OnChange já
// mesclou): um diretório que saiu do rc sai também do PATH.
func TestUserPATHProbeStartsFromFallback(t *testing.T) {
	t.Setenv("PATH", "/venv-velho/bin:/usr/bin:/bin")
	sh := fakeShell(t, "bash", `printf '__NOBRE_PATH__/rc:%s\n' "$PATH"`)
	u := &UserPATH{Shell: sh, Fallback: "/usr/bin:/bin", Timeout: 2 * time.Second}
	if got := u.Get(0); got != "/rc:/usr/bin:/bin" {
		t.Fatalf("%q", got)
	}
}
