package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeLaunchctl simula o launchctl: print responde conforme o serviço está
// carregado; bootstrap carrega (falhando failBoots vezes antes) e bootout
// descarrega.
type fakeLaunchctl struct {
	loaded    bool
	failBoots int
	ran       []string
}

func (f *fakeLaunchctl) run(name string, args ...string) error {
	cmd := name + " " + strings.Join(args, " ")
	f.ran = append(f.ran, cmd)
	if name != "launchctl" {
		return errors.New("comando inesperado: " + cmd)
	}
	switch args[0] {
	case "print":
		if !f.loaded {
			return errors.New("exit status 113")
		}
	case "bootstrap":
		if f.failBoots > 0 {
			f.failBoots--
			return errors.New("Bootstrap failed: 5: Input/output error")
		}
		f.loaded = true
	case "bootout":
		f.loaded = false
	}
	return nil
}

func newLaunchdEnv(t *testing.T) (*setupEnv, *fakeLaunchctl) {
	e := newSetupEnv(t)
	root := t.TempDir()
	f := &fakeLaunchctl{}
	e.d.GOOS, e.d.UID = "darwin", 501
	e.d.AgentsDir = filepath.Join(root, "Library", "LaunchAgents")
	e.d.LogDir = filepath.Join(root, "Library", "Logs")
	e.d.Run = f.run
	e.d.Sleep = func(time.Duration) {}
	return e, f
}

func TestSetupDarwinInstallsLaunchAgent(t *testing.T) {
	e, f := newLaunchdEnv(t)
	r := e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 0 || strings.Contains(r.stdout, "!") {
		t.Fatalf("%+v", r)
	}
	plist, err := os.ReadFile(filepath.Join(e.d.AgentsDir, "io.nobretech.nobred.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>io.nobretech.nobred</string>",
		"<array>\n\t\t<string>/opt/x/bin/nobred</string>\n\t</array>",
		"<key>PATH</key>\n\t\t<string>/opt/x/bin:/usr/bin:/home/u/.local/bin:/usr/local/bin:/bin</string>",
		"<key>SuccessfulExit</key>\n\t\t<false/>",
		"<key>AbandonProcessGroup</key>\n\t<true/>",
		"<string>" + filepath.Join(e.d.LogDir, "nobred.log") + "</string>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Fatalf("plist sem %q:\n%s", want, plist)
		}
	}
	if _, err := os.Stat(e.d.UnitDir); !os.IsNotExist(err) {
		t.Fatalf("no macOS não grava unidade do systemd: %v", err)
	}
	want := []string{
		"launchctl enable gui/501/io.nobretech.nobred",
		"launchctl print gui/501/io.nobretech.nobred",
		"launchctl bootstrap gui/501 " + filepath.Join(e.d.AgentsDir, "io.nobretech.nobred.plist"),
	}
	if strings.Join(f.ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ran =\n%s", strings.Join(f.ran, "\n"))
	}
	if !strings.Contains(r.stdout, "Daemon nobred ativo (launchd: gui/501/io.nobretech.nobred") {
		t.Fatalf("stdout: %s", r.stdout)
	}

	// Sem mudança e carregado: só garante que está de pé, sem reiniciar.
	f.ran = nil
	if r := e.run(t, "127.0.0.1:1", "", t.TempDir()); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if last := f.ran[len(f.ran)-1]; last != "launchctl kickstart gui/501/io.nobretech.nobred" {
		t.Fatalf("ran = %v", f.ran)
	}

	// nobred mudou de lugar: descarrega o antigo e carrega o novo, mesmo com o
	// launchd recusando as primeiras tentativas.
	f.ran, f.failBoots = nil, 2
	e.d.NobredBin = "/opt/y/bin/nobred"
	r = e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 0 || !f.loaded || !strings.Contains(r.stdout, "reiniciado para usar o LaunchAgent novo") {
		t.Fatalf("%+v ran=%v", r, f.ran)
	}
	joined := strings.Join(f.ran, "\n")
	if !strings.Contains(joined, "launchctl bootout gui/501/io.nobretech.nobred\nlaunchctl bootstrap") || strings.Count(joined, "bootstrap") != 3 {
		t.Fatalf("ran =\n%s", joined)
	}
}

func TestSetupDarwinBootstrapFailureIsPending(t *testing.T) {
	e, f := newLaunchdEnv(t)
	f.failBoots = 99
	r := e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code == 0 || !strings.Contains(r.stdout, "! launchctl bootstrap gui/501") {
		t.Fatalf("%+v", r)
	}
}

func TestSetupDarwinDryRunWritesNothing(t *testing.T) {
	e, f := newLaunchdEnv(t)
	r := e.run(t, "127.0.0.1:1", "", t.TempDir(), "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, "[dry-run] Rodaria: launchctl bootstrap gui/501") {
		t.Fatalf("%+v", r)
	}
	if len(f.ran) != 0 {
		t.Fatalf("dry-run rodou %v", f.ran)
	}
	if _, err := os.Stat(e.d.AgentsDir); !os.IsNotExist(err) {
		t.Fatalf("dry-run gravou: %v", err)
	}
}

func TestRemoverDarwinStopsAndDeletesLaunchAgent(t *testing.T) {
	e, f := newLaunchdEnv(t)
	if r := e.run(t, "127.0.0.1:1", "", t.TempDir()); r.code != 0 {
		t.Fatalf("setup: %+v", r)
	}
	f.ran = nil
	r := e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover")
	if r.code != 0 || f.loaded {
		t.Fatalf("%+v ran=%v", r, f.ran)
	}
	if len(f.ran) < 2 || f.ran[1] != "launchctl bootout gui/501/io.nobretech.nobred" {
		t.Fatalf("ran = %v", f.ran)
	}
	if isFile(filepath.Join(e.d.AgentsDir, "io.nobretech.nobred.plist")) {
		t.Fatal("plist ficou")
	}
	r = e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover")
	if !strings.Contains(r.stdout, "Serviço do nobred não instalado") {
		t.Fatalf("segunda remoção: %+v", r)
	}
}

func TestPlistEscapesXML(t *testing.T) {
	p := plistContent("/Users/a&b/bin/nobred", "/x<y>", "/l/n.log")
	if !strings.Contains(p, "/Users/a&amp;b/bin/nobred") || !strings.Contains(p, "/x&lt;y&gt;") {
		t.Fatal(p)
	}
}
