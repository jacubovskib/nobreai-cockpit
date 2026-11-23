package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/kiro"
)

// kiroSetup liga o Kiro do setup a um HOME temporário; withBin cria o kiro-cli.
func kiroSetup(t *testing.T, e *setupEnv, withBin bool) (hooks, steering string) {
	t.Helper()
	home := t.TempDir()
	hooks = filepath.Join(home, ".kiro", "hooks", "nobreai-cockpit.json")
	steering = filepath.Join(home, ".kiro", "steering", "nobreai-cockpit.md")
	bin := filepath.Join(home, "bin", "kiro-cli")
	if withBin {
		os.MkdirAll(filepath.Dir(bin), 0o755)
		os.WriteFile(bin, []byte("x"), 0o755)
	}
	nobre := filepath.Join(home, "bin", "nobre")
	os.MkdirAll(filepath.Dir(nobre), 0o755)
	os.WriteFile(nobre, []byte("x"), 0o755)
	a := kiro.New(nil, adapter.Deps{})
	a.SetPaths(kiro.Paths{HooksFile: hooks, SteeringFile: steering, NobreBin: nobre, KiroBin: bin})
	e.d.Kiro = a
	return hooks, steering
}

func TestSetupKiroInstallsAndRemoves(t *testing.T) {
	e := newSetupEnv(t)
	hooks, steering := kiroSetup(t, e, true)
	cwd := t.TempDir()

	r := e.run(t, "127.0.0.1:1", "", cwd, "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, "[dry-run] Hooks do Kiro seriam gravados em "+hooks) ||
		!strings.Contains(r.stdout, "[dry-run] Steering do Kiro seriam gravados em "+steering) {
		t.Fatalf("dry-run: %+v", r)
	}
	if _, err := os.Stat(hooks); err == nil {
		t.Fatal("dry-run gravou")
	}

	r = e.run(t, "127.0.0.1:1", "", cwd)
	if r.code != 0 || !strings.Contains(r.stdout, "✓ Hooks do Kiro gravados em "+hooks) {
		t.Fatalf("setup: %+v", r)
	}
	for _, p := range []string{hooks, steering} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	r = e.run(t, "127.0.0.1:1", "", cwd)
	if !strings.Contains(r.stdout, "Hooks do Kiro já instalados") {
		t.Fatalf("segunda vez: %s", r.stdout)
	}

	r = e.run(t, "127.0.0.1:1", "", cwd, "--remover", "--dry-run")
	if _, err := os.Stat(hooks); err != nil || !strings.Contains(r.stdout, "[dry-run] Removeria "+hooks) {
		t.Fatalf("remover dry: %+v", r)
	}
	r = e.run(t, "127.0.0.1:1", "", cwd, "--remover")
	if r.code != 0 {
		t.Fatalf("remover: %+v", r)
	}
	for _, p := range []string{hooks, steering} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s continua", p)
		}
	}
}

func TestSetupKiroNotFoundSkips(t *testing.T) {
	e := newSetupEnv(t)
	hooks, _ := kiroSetup(t, e, false)
	r := e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 0 || strings.Count(r.stdout, "Kiro não encontrado") != 1 {
		t.Fatalf("setup: %+v", r)
	}
	if _, err := os.Stat(hooks); err == nil {
		t.Fatal("instalou sem kiro-cli")
	}
}
