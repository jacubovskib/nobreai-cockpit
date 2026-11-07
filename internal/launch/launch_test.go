package launch

import (
	"fmt"
	"reflect"
	"testing"
)

const wantScript = `cd '/w/api' && NOBRE_RESUME='NAC-1' 'claude' '--' 'Continue o NAC-1: '\''x'\'''; s=$?; if [ $s -ne 0 ]; then printf '%s\n' "NobreAI Cockpit: o agente saiu com código $s (Enter fecha)"; read -r _; fi`

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
		got := TerminalArgvWith(res, c.cmd, "/w/api", env, agent)
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
