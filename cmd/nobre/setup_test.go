package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type setupEnv struct {
	d        *setupDeps
	ran      [][]string
	runErr   error
	repo     string
	settings string
}

func newSetupEnv(t *testing.T) *setupEnv {
	t.Helper()
	root := t.TempDir()
	e := &setupEnv{repo: filepath.Join(root, "repo")}
	os.MkdirAll(filepath.Join(e.repo, ".git"), 0o755)
	e.settings = filepath.Join(root, "claude", "settings.json")
	e.d = &setupDeps{
		SettingsPath: e.settings,
		SkillsDir:    filepath.Join(root, "claude", "skills"),
		UnitDir:      filepath.Join(root, "systemd", "user"),
		NobreBin:     "/opt/x/bin/nobre",
		NobredBin:    "/opt/x/bin/nobred",
		Run: func(name string, args ...string) error {
			e.ran = append(e.ran, append([]string{name}, args...))
			return e.runErr
		},
	}
	return e
}

func (e *setupEnv) run(t *testing.T, addr, stdin, cwd string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	c := &cli{in: strings.NewReader(stdin), out: &out, err: &errb, sd: e.d,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return ""
		},
		cwd:    func() (string, error) { return cwd, nil },
		opener: func(string) error { return nil }}
	code := c.run(append([]string{"setup"}, args...))
	return result{code: code, stdout: out.String(), err: errb.String()}
}

func TestSetupInstallsEverythingAndRegistersProject(t *testing.T) {
	e := newSetupEnv(t)
	var posted map[string]string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			w.Write([]byte(`[]`))
		case r.Method == "POST" && r.URL.Path == "/api/projects":
			json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(201)
			w.Write([]byte(`{"id":1,"name":"repo","path":"` + posted["path"] + `"}`))
		default:
			t.Errorf("rota inesperada %s %s", r.Method, r.URL.Path)
		}
	})
	r := e.run(t, addr, "s\n", filepath.Join(e.repo, "sub"))
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	b, _ := os.ReadFile(e.settings)
	if !strings.Contains(string(b), "/opt/x/bin/nobre hook claude SessionStart") {
		t.Fatalf("settings: %s", b)
	}
	if _, err := os.Stat(filepath.Join(e.d.SkillsDir, "nobre-cockpit", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	unit, _ := os.ReadFile(filepath.Join(e.d.UnitDir, "nobred.service"))
	if !strings.Contains(string(unit), "ExecStart=/opt/x/bin/nobred\n") || !strings.Contains(string(unit), "Restart=on-failure") {
		t.Fatalf("unit: %s", unit)
	}
	if len(e.ran) != 2 || strings.Join(e.ran[0], " ") != "systemctl --user daemon-reload" ||
		strings.Join(e.ran[1], " ") != "systemctl --user enable --now nobred" {
		t.Fatalf("ran = %v", e.ran)
	}
	if posted["path"] != e.repo {
		t.Fatalf("posted = %v", posted)
	}
	if !strings.Contains(r.stdout, "[s/N]") || strings.Count(r.stdout, "✓") < 4 || strings.Contains(r.stdout, "!") {
		t.Fatalf("stdout: %s", r.stdout)
	}
}

func TestSetupDeclineProjectAndNotGit(t *testing.T) {
	e := newSetupEnv(t)
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("API não deveria ser chamada") })
	if r := e.run(t, addr, "n\n", e.repo); r.code != 0 || !strings.Contains(r.stdout, "não cadastrado") {
		t.Fatalf("%+v", r)
	}
	if r := e.run(t, addr, "", t.TempDir()); r.code != 0 || strings.Contains(r.stdout, "[s/N]") {
		t.Fatalf("%+v", r)
	}
}

func TestSetupDryRunWritesNothing(t *testing.T) {
	e := newSetupEnv(t)
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("API não deveria ser chamada") })
	r := e.run(t, addr, "s\n", e.repo, "--dry-run")
	if r.code != 0 || len(e.ran) != 0 {
		t.Fatalf("%+v ran=%v", r, e.ran)
	}
	for _, p := range []string{e.settings, e.d.SkillsDir, e.d.UnitDir} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s foi criado no dry-run", p)
		}
	}
	for _, want := range []string{"dry-run", "nobred.service", "enable --now nobred", "Instalaria hooks"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("falta %q em %s", want, r.stdout)
		}
	}
}

func TestSetupFlagsPendingWhenSystemctlFails(t *testing.T) {
	e := newSetupEnv(t)
	e.runErr = errors.New("sem sessão systemd")
	r := e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 1 || !strings.Contains(r.stdout, "! systemctl --user daemon-reload") || !strings.Contains(r.err, "pendências") {
		t.Fatalf("%+v", r)
	}
	// Hooks e skill foram instalados mesmo assim.
	if _, err := os.Stat(e.settings); err != nil {
		t.Fatal(err)
	}
}

func TestSetupInvalidSettingsLeavesFileAndRerunIsIdempotent(t *testing.T) {
	e := newSetupEnv(t)
	os.MkdirAll(filepath.Dir(e.settings), 0o755)
	os.WriteFile(e.settings, []byte("{"), 0o600)
	r := e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 1 || !strings.Contains(r.stdout, "! Hooks do Claude Code: ~/.claude/settings.json não é um JSON válido") {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(e.settings); string(b) != "{" {
		t.Fatal("alterou settings inválido")
	}
	os.Remove(e.settings)
	e.run(t, "127.0.0.1:1", "", t.TempDir())
	e.ran = nil
	r = e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 0 || !strings.Contains(r.stdout, "já instalados") {
		t.Fatalf("%+v", r)
	}
	if m, _ := filepath.Glob(e.settings + ".bak-*"); len(m) != 0 {
		t.Fatal("backup em execução idempotente")
	}
}

func TestSetupNobredMissing(t *testing.T) {
	e := newSetupEnv(t)
	e.d.NobredBin = ""
	r := e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 1 || !strings.Contains(r.stdout, "não achei o nobred") || len(e.ran) != 0 {
		t.Fatalf("%+v", r)
	}
}
