package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/kiro"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
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
	a.SetPaths(kiro.Paths{HooksFile: hooks, SteeringFile: steering, KiroBin: bin})
	e.d.Drivers = []adapter.Adapter{a}
	e.d.NobreBin = nobre // o setup passa o nobre que roda para o driver
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
	if b, _ := os.ReadFile(hooks); !strings.Contains(string(b), e.d.NobreBin+" hook kiro SessionStart") {
		t.Fatalf("hooks sem o nobre do setup: %s", b)
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

// setupDriver é um terceiro agente com instalação própria: o setup o instala
// e remove pelas capacidades, sem mudar o setup.go.
type setupDriver struct {
	adapter.Adapter
	name string
	err  error
	got  []string
}

func (d *setupDriver) Name() string { return d.name }
func (d *setupDriver) SetupInstall(nobre string, dry bool) (adapter.InstallReport, error) {
	d.got = append(d.got, fmt.Sprintf("install %s %v", nobre, dry))
	return adapter.InstallReport{Messages: []string{"Hooks do Codex gravados."}}, d.err
}
func (d *setupDriver) SetupUninstall(dry bool) (adapter.InstallReport, error) {
	d.got = append(d.got, fmt.Sprintf("uninstall %v", dry))
	return adapter.InstallReport{Messages: []string{"Hooks do Codex removidos."}}, d.err
}

func TestSetupDriverCapabilities(t *testing.T) {
	e := newSetupEnv(t)
	d := &setupDriver{name: "codex"}
	// O Claude dos drivers compilados não tem as capacidades: fica de fora
	// (os passos dele seguem explícitos no setup).
	claudeDrv := agents.Compiled()[0]
	if _, ok := claudeDrv.(adapter.SetupInstaller); ok {
		t.Fatal("o Claude não instala pelo passo de cada agente")
	}
	e.d.Drivers = []adapter.Adapter{claudeDrv, d}
	cwd := t.TempDir()
	r := e.run(t, "127.0.0.1:1", "", cwd, "--dry-run")
	if !strings.Contains(r.stdout, "✓ [dry-run] Hooks do Codex gravados.\n") {
		t.Fatalf("dry-run: %s", r.stdout)
	}
	e.run(t, "127.0.0.1:1", "", cwd)
	e.run(t, "127.0.0.1:1", "", cwd, "--remover", "--dry-run")
	r = e.run(t, "127.0.0.1:1", "", cwd, "--remover")
	if !strings.Contains(r.stdout, "✓ Hooks do Codex removidos.\n") {
		t.Fatalf("remover: %s", r.stdout)
	}
	want := []string{"install /opt/x/bin/nobre true", "install /opt/x/bin/nobre false", "uninstall true", "uninstall false"}
	if strings.Join(d.got, ",") != strings.Join(want, ",") {
		t.Fatalf("chamadas = %v", d.got)
	}

	// Falha vira pendência com o nome de tela do agente.
	d.err = errors.New("sem permissão")
	r = e.run(t, "127.0.0.1:1", "", cwd)
	if r.code == 0 || !strings.Contains(r.stdout, "! Codex: sem permissão\n") {
		t.Fatalf("falha: %+v", r)
	}
	r = e.run(t, "127.0.0.1:1", "", cwd, "--remover")
	if r.code == 0 || !strings.Contains(r.stdout, "! Codex: sem permissão\n") {
		t.Fatalf("falha ao remover: %+v", r)
	}
}
