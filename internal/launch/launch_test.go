package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const wantScript = `export PATH='/usr/bin:/opt/nobre'; cd '/w/api' && NOBRE_RESUME='NAC-1' 'claude' '--' 'Continue o NAC-1: '\''x'\'''; s=$?; if [ $s -ne 0 ]; then printf '%s\n' "NobreAI Cockpit: o agente saiu com código $s (Enter fecha)"; read -r _; fi`

func fakeResolve(m map[string]string) Resolver {
	return func(n string) (string, error) {
		if r, ok := m[n]; ok {
			return r, nil
		}
		return "", fmt.Errorf("não achei %s", n)
	}
}

func TestTerminalArgvTemplates(t *testing.T) {
	env := map[string]string{"NOBRE_RESUME": "NAC-1"}
	agent := []string{"claude", "--", "Continue o NAC-1: 'x'"}
	res := fakeResolve(map[string]string{
		"x-terminal-emulator": "/usr/bin/terminator",
		"gnome-terminal":      "/usr/bin/gnome-terminal.real",
		"xterm":               "/usr/bin/xterm",
		"wrapper":             "/usr/bin/gnome-terminal.wrapper",
		"xfce":                "/usr/bin/xfce4-terminal",
		"kitty":               "/usr/bin/kitty",
		"meu-term":            "/opt/meu-term",
		"osascript":           "/usr/bin/osascript",
	})
	cases := []struct {
		name string
		cmd  []string
		want []string
	}{
		{"terminator via x-terminal-emulator", []string{"x-terminal-emulator", "-e"},
			[]string{"/usr/bin/terminator", "-e", "/bin/sh -c " + quote(wantScript)}},
		{"gnome-terminal", []string{"gnome-terminal", "--"}, []string{"/usr/bin/gnome-terminal.real", "--", "/bin/sh", "-c", wantScript}},
		{"wrapper do gnome-terminal", []string{"wrapper", "--"}, []string{"/usr/bin/gnome-terminal.wrapper", "-e", "/bin/sh", "-c", wantScript}},
		{"xfce4-terminal", []string{"xfce"}, []string{"/usr/bin/xfce4-terminal", "-x", "/bin/sh", "-c", wantScript}},
		{"xterm", []string{"xterm"}, []string{"/usr/bin/xterm", "-e", "/bin/sh", "-c", wantScript}},
		{"kitty", []string{"kitty", "-x"}, []string{"/usr/bin/kitty", "-e", "/bin/sh", "-c", wantScript}},
		{"desconhecido", []string{"meu-term", "--run"}, []string{"meu-term", "--run", wantScript}},
		{"não resolve", []string{"sumiu", "-e"}, []string{"sumiu", "-e", wantScript}},
	}
	for _, c := range cases {
		got := TerminalArgvWith(res, c.cmd, "/usr/bin:/opt/nobre", "/w/api", env, agent)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestExecStartRejectsEmpty(t *testing.T) {
	if err := (Exec{}).Start(nil); err == nil {
		t.Fatal("esperava erro")
	}
}

func TestWithGraphicalEnvFillsMissing(t *testing.T) {
	show := func() ([]byte, error) {
		return []byte("DISPLAY=:1\nXAUTHORITY=/run/xa\nHOME=/x\nWAYLAND_DISPLAY=\n"), nil
	}
	got := withGraphicalEnv([]string{"DISPLAY=:0", "PATH=/bin"}, show)
	want := []string{"DISPLAY=:0", "PATH=/bin", "XAUTHORITY=/run/xa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	// nada falta: não consulta
	full := []string{"DISPLAY=:0", "WAYLAND_DISPLAY=w", "XAUTHORITY=x", "DBUS_SESSION_BUS_ADDRESS=d"}
	called := false
	withGraphicalEnv(full, func() ([]byte, error) { called = true; return nil, nil })
	if called {
		t.Fatal("consultou sem necessidade")
	}
	// erro: env intacto
	if got := withGraphicalEnv([]string{"A=b"}, func() ([]byte, error) { return nil, fmt.Errorf("x") }); len(got) != 1 {
		t.Fatalf("got %q", got)
	}
}

func TestScriptExportsQuotedPATH(t *testing.T) {
	got := Script("/a b:/it's", "/w", nil, []string{"/home/u/.local/bin/claude"})
	want := `export PATH='/a b:/it'\''s'; cd '/w' && '/home/u/.local/bin/claude'; `
	if len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("script = %q", got)
	}
	// Sem PATH não exporta nada.
	if got := Script("", "/w", nil, []string{"x"}); got[:4] != "cd '" {
		t.Fatalf("script = %q", got)
	}
}

func TestPathWithDir(t *testing.T) {
	cases := []struct{ path, dir, want string }{
		{"/usr/bin:/bin", "/home/u/.local/bin", "/home/u/.local/bin:/usr/bin:/bin"},
		{"/usr/bin:/home/u/.local/bin", "/home/u/.local/bin", "/usr/bin:/home/u/.local/bin"},
		{"", "/opt/n", "/opt/n"},
		{"/usr/bin", "", "/usr/bin"},
	}
	for _, c := range cases {
		if got := PathWithDir(c.path, c.dir); got != c.want {
			t.Errorf("PathWithDir(%q, %q) = %q, quero %q", c.path, c.dir, got, c.want)
		}
	}
}

func TestAppleScriptString(t *testing.T) {
	got := appleScriptString(`/bin/sh -c 'cd "/w" && echo \'\''x'`)
	want := `"/bin/sh -c 'cd \"/w\" && echo \\'\\''x'"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestResolveBinaryDarwinFallsBackToOsascript(t *testing.T) {
	dir := t.TempDir()
	osa := filepath.Join(dir, "osascript")
	if err := os.WriteFile(osa, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	want, _ := filepath.EvalSymlinks(osa)
	if got, err := resolveBinaryOn("darwin", "x-terminal-emulator"); err != nil || got != want {
		t.Fatalf("darwin: %q, %v", got, err)
	}
	if _, err := resolveBinaryOn("linux", "x-terminal-emulator"); err == nil {
		t.Fatal("no Linux não troca pelo osascript")
	}
}

// TestTerminalArgvOsascriptUsesScriptFile: o Terminal.app digita o texto do
// "do script" no shell interativo; o pacote de retomada (várias linhas, tabs)
// vai num arquivo 0600 que se apaga, e só "exec /bin/sh '<arquivo>'" é
// digitado.
func TestTerminalArgvOsascriptUsesScriptFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	res := fakeResolve(map[string]string{"osascript": "/usr/bin/osascript"})
	agent := []string{"claude", "--", "linha 1\n\tlinha 2 'x' \"y\""}
	argv := TerminalArgvWith(res, []string{"osascript"}, "/usr/bin", "/w/api", map[string]string{"NOBRE_RESUME": "NAC-1"}, agent)
	if len(argv) != 9 || argv[0] != "/usr/bin/osascript" || argv[2] != `tell application "Terminal"` || argv[6] != "activate" || argv[8] != "end tell" {
		t.Fatalf("argv = %q", argv)
	}
	const pre = `do script "exec /bin/sh '`
	if !strings.HasPrefix(argv[4], pre) || !strings.HasSuffix(argv[4], `'"`) {
		t.Fatalf("do script = %q", argv[4])
	}
	file := strings.TrimSuffix(strings.TrimPrefix(argv[4], pre), `'"`)
	if strings.ContainsAny(file, "\n\t'\"") {
		t.Fatalf("caminho digitado com caracteres especiais: %q", file)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := "rm -f \"$0\"\n" + Script("/usr/bin", "/w/api", map[string]string{"NOBRE_RESUME": "NAC-1"}, agent) + "\n"
	if string(b) != want {
		t.Fatalf("script =\n%s\nwant\n%s", b, want)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("modo = %v", st.Mode().Perm())
	}
}

// TestExecStartOsascriptError: o osascript que sai com erro (Automação
// negada) vira erro do Start, em vez de sumir no log.
func TestExecStartOsascriptError(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "osascript")
	script := "#!/bin/sh\necho 'execution error: Not authorized to send Apple events to Terminal. (-1743)' >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Exec{ShowEnv: func() ([]byte, error) { return nil, nil }}.Start([]string{fake, "-e", "x"})
	if err == nil || !strings.Contains(err.Error(), "-1743") || !strings.Contains(err.Error(), "Automação") {
		t.Fatalf("err = %v", err)
	}
	ok := filepath.Join(dir, "ok", "osascript")
	os.MkdirAll(filepath.Dir(ok), 0o755)
	os.WriteFile(ok, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if err := (Exec{ShowEnv: func() ([]byte, error) { return nil, nil }}).Start([]string{ok}); err != nil {
		t.Fatalf("osascript ok: %v", err)
	}
}
