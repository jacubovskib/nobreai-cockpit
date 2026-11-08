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
			switch k {
			case "NOBRE_ADDR":
				return addr
			case "PATH":
				return "/usr/bin:/home/u/.local/bin"
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
	// KillMode=process: terminais/agentes abertos pelo "Continuar" (filhos do
	// nobred) sobrevivem a restart/stop do serviço.
	if !strings.Contains(string(unit), "ExecStart=/opt/x/bin/nobred\n") || !strings.Contains(string(unit), "Restart=on-failure") ||
		!strings.Contains(string(unit), "\nKillMode=process\n") ||
		!strings.Contains(string(unit), "\nEnvironment=\"PATH=/opt/x/bin:/usr/bin:/home/u/.local/bin\"\n") {
		t.Fatalf("unit: %s", unit)
	}
	if len(e.ran) != 3 ||
		strings.Join(e.ran[0], " ") != "systemctl --user import-environment DISPLAY WAYLAND_DISPLAY XAUTHORITY DBUS_SESSION_BUS_ADDRESS" ||
		strings.Join(e.ran[1], " ") != "systemctl --user daemon-reload" ||
		strings.Join(e.ran[2], " ") != "systemctl --user enable --now nobred" {
		t.Fatalf("ran = %v", e.ran)
	}
	if posted["path"] != e.repo {
		t.Fatalf("posted = %v", posted)
	}
	if !strings.Contains(r.stdout, "[s/N]") || strings.Count(r.stdout, "✓") < 4 || strings.Contains(r.stdout, "!") {
		t.Fatalf("stdout: %s", r.stdout)
	}
}

func TestSetupStatusLineStepAndOptOut(t *testing.T) {
	e := newSetupEnv(t)
	os.MkdirAll(filepath.Dir(e.settings), 0o755)
	os.WriteFile(e.settings, []byte(`{"statusLine":{"type":"command","command":"echo oi"}}`), 0o600)
	r := e.run(t, "127.0.0.1:1", "", t.TempDir())
	b, _ := os.ReadFile(e.settings)
	if r.code != 0 || !strings.Contains(string(b), `"command": "/opt/x/bin/nobre statusline | {\necho oi\n}"`) ||
		!strings.Contains(string(b), `"refreshInterval": 60`) {
		t.Fatalf("%+v\n%s", r, b)
	}

	e = newSetupEnv(t)
	r = e.run(t, "127.0.0.1:1", "", t.TempDir(), "--sem-statusline")
	b, _ = os.ReadFile(e.settings)
	if r.code != 0 || strings.Contains(string(b), "statusLine") || !strings.Contains(r.stdout, "--sem-statusline") {
		t.Fatalf("%+v\n%s", r, b)
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
	for _, want := range []string{"dry-run", "nobred.service", "enable --now nobred", "Instalaria hooks", "Integraria a status line"} {
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
	// A primeira execução gravou settings.json em dois passos (hooks e status
	// line): o segundo passo deixa um backup do intermediário.
	old, _ := filepath.Glob(e.settings + ".bak-*")
	for _, f := range old {
		os.Remove(f)
	}
	e.ran = nil
	r = e.run(t, "127.0.0.1:1", "", t.TempDir())
	if r.code != 0 || !strings.Contains(r.stdout, "já instalados") || !strings.Contains(r.stdout, "Status line já integrada") {
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

func TestSetupRestartsDaemonWhenUnitChanged(t *testing.T) {
	e := newSetupEnv(t)
	cwd := t.TempDir()
	e.run(t, "127.0.0.1:1", "", cwd)
	for _, r := range e.ran {
		if r[2] == "restart" {
			t.Fatalf("primeira instalação não reinicia: %v", e.ran)
		}
	}
	e.ran = nil
	e.run(t, "127.0.0.1:1", "", cwd)
	if len(e.ran) != 3 {
		t.Fatalf("unidade igual: %v", e.ran)
	}
	e.ran = nil
	e.d.NobredBin = "/opt/y/bin/nobred"
	r := e.run(t, "127.0.0.1:1", "", cwd)
	last := e.ran[len(e.ran)-1]
	if r.code != 0 || strings.Join(last, " ") != "systemctl --user restart nobred" {
		t.Fatalf("%+v ran=%v", r, e.ran)
	}
}

func TestUnitPATHQuotedForSystemd(t *testing.T) {
	got := unitContent("/opt/x/nobred", `/a b:/100%:/q"uote:/back\slash`)
	want := `Environment="PATH=/a b:/100%%:/q\"uote:/back\\slash"` + "\n"
	if !strings.Contains(got, "\n"+want) {
		t.Fatalf("unit:\n%s\nquero a linha %q", got, want)
	}
	if !strings.Contains(unitContent("/x", "/a\n/b:/c"), `Environment="PATH=/c"`) {
		t.Fatalf("entrada com quebra de linha deveria sumir:\n%s", unitContent("/x", "/a\n/b:/c"))
	}
	if strings.Contains(unitContent("/x", ""), "Environment=") {
		t.Fatal("PATH vazio não deveria gerar Environment=")
	}
}
