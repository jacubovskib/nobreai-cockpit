package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		DaemonWait:   3 * time.Second,
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
		!strings.Contains(string(unit), "\nEnvironment=\"PATH=/opt/x/bin:/usr/local/bin:/usr/bin:/bin\"\n") {
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
	if r.code != 0 || !strings.Contains(string(b), `"command": "{ /opt/x/bin/nobre statusline || cat; } | {\necho oi\n}"`) ||
		!strings.Contains(string(b), `"refreshInterval": 60`) {
		t.Fatalf("%+v\n%s", r, b)
	}
	// Hooks e status line num único ler-alterar-gravar: exatamente um backup,
	// com o conteúdo original.
	baks, _ := filepath.Glob(e.settings + ".bak-*")
	if len(baks) != 1 {
		t.Fatalf("backups = %v", baks)
	}
	if orig, _ := os.ReadFile(baks[0]); string(orig) != `{"statusLine":{"type":"command","command":"echo oi"}}` {
		t.Fatalf("backup = %s", orig)
	}

	// Original que não dá para embrulhar com segurança: recusa clara, hooks entram.
	e = newSetupEnv(t)
	os.MkdirAll(filepath.Dir(e.settings), 0o755)
	os.WriteFile(e.settings, []byte(`{"statusLine":{"type":"command","command":"# só comentário"}}`), 0o600)
	r = e.run(t, "127.0.0.1:1", "", t.TempDir())
	b, _ = os.ReadFile(e.settings)
	if r.code != 1 || !strings.Contains(r.stdout, "! Status line do Claude Code: não consigo integrar esta status line com segurança: o comando só tem comentários") ||
		!strings.Contains(string(b), `"command": "# só comentário"`) || !strings.Contains(string(b), "hook claude Stop") {
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
	// Sem settings.json, a execução cria o arquivo: não há o que guardar em backup.
	e.run(t, "127.0.0.1:1", "", t.TempDir())
	if m, _ := filepath.Glob(e.settings + ".bak-*"); len(m) != 0 {
		t.Fatalf("backup sem arquivo anterior: %v", m)
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

func TestSetupRemoveStatusLine(t *testing.T) {
	e := newSetupEnv(t)
	os.MkdirAll(filepath.Dir(e.settings), 0o755)
	orig := `input=$(cat); echo "$input" | jq -r .model # fim`
	b, _ := json.Marshal(map[string]any{"statusLine": map[string]any{"type": "command", "command": orig}})
	os.WriteFile(e.settings, b, 0o600)
	if r := e.run(t, "127.0.0.1:1", "", t.TempDir()); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	e.ran = nil
	read := func() string {
		var root struct {
			StatusLine map[string]any `json:"statusLine"`
		}
		b, _ := os.ReadFile(e.settings)
		json.Unmarshal(b, &root)
		s, _ := root.StatusLine["command"].(string)
		return s
	}
	if !strings.Contains(read(), "statusline || cat") {
		t.Fatalf("não integrou: %q", read())
	}
	// Dry-run: só diz o que faria.
	r := e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover-statusline", "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, "Removeria a integração") || !strings.Contains(read(), "statusline || cat") {
		t.Fatalf("%+v %q", r, read())
	}
	r = e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover-statusline")
	if r.code != 0 || read() != orig || !strings.Contains(r.stdout, "Status line restaurada") || len(e.ran) != 0 {
		t.Fatalf("%+v %q ran=%v", r, read(), e.ran)
	}
	// Só a status line: hooks continuam.
	if b, _ := os.ReadFile(e.settings); !strings.Contains(string(b), "hook claude Stop") {
		t.Fatalf("hooks sumiram: %s", b)
	}
	r = e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover-statusline")
	if r.code != 0 || !strings.Contains(r.stdout, "não é do nobre") {
		t.Fatalf("%+v", r)
	}
	if r := e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover-statusline", "--sem-statusline"); r.code != 2 {
		t.Fatalf("flags conflitantes: %+v", r)
	}
}

func TestSetupRemoverUndoesEverything(t *testing.T) {
	e := newSetupEnv(t)
	os.MkdirAll(filepath.Dir(e.settings), 0o755)
	orig := `{"model":"opus","statusLine":{"type":"command","command":"echo hi"},` +
		`"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/other stop"}]}]}}`
	os.WriteFile(e.settings, []byte(orig), 0o600)
	if r := e.run(t, "127.0.0.1:1", "", t.TempDir()); r.code != 0 {
		t.Fatalf("setup: %+v", r)
	}
	unit := filepath.Join(e.d.UnitDir, "nobred.service")
	installed, _ := os.ReadFile(e.settings)

	// Dry-run: nada muda.
	e.ran = nil
	r := e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover", "--dry-run")
	if b, _ := os.ReadFile(e.settings); r.code != 0 || len(e.ran) != 0 || string(b) != string(installed) {
		t.Fatalf("dry-run alterou: %+v ran=%v", r, e.ran)
	}
	if _, err := os.Stat(unit); err != nil {
		t.Fatal("dry-run apagou a unidade")
	}
	for _, want := range []string{"Removeria os hooks", "Removeria a skill", "Rodaria: systemctl --user disable --now nobred"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("falta %q em %s", want, r.stdout)
		}
	}

	r = e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover")
	if r.code != 0 || strings.Contains(r.stdout, "!") {
		t.Fatalf("remover: %+v", r)
	}
	var got, want any
	b, _ := os.ReadFile(e.settings)
	json.Unmarshal(b, &got)
	json.Unmarshal([]byte(orig), &want)
	if !jsonEqual(got, want) {
		t.Fatalf("settings depois de remover: %s", b)
	}
	if _, err := os.Stat(filepath.Join(e.d.SkillsDir, "nobre-cockpit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("skill ficou")
	}
	if _, err := os.Stat(unit); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unidade ficou")
	}
	if len(e.ran) != 2 || strings.Join(e.ran[0], " ") != "systemctl --user disable --now nobred" ||
		strings.Join(e.ran[1], " ") != "systemctl --user daemon-reload" {
		t.Fatalf("ran = %v", e.ran)
	}
	if !strings.Contains(r.stdout, "/opt/x/bin") {
		t.Fatalf("não lembrou dos binários: %s", r.stdout)
	}

	// De novo: nada a remover, sem systemctl.
	e.ran = nil
	r = e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover")
	if r.code != 0 || len(e.ran) != 0 || !strings.Contains(r.stdout, "nada a remover") {
		t.Fatalf("segunda vez: %+v ran=%v", r, e.ran)
	}
}

func TestSetupRemoverRejectsOtherFlags(t *testing.T) {
	e := newSetupEnv(t)
	for _, f := range []string{"--sem-statusline", "--remover-statusline"} {
		if r := e.run(t, "127.0.0.1:1", "", t.TempDir(), "--remover", f); r.code == 0 {
			t.Fatalf("--remover %s deveria ser recusado: %+v", f, r)
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// O enable --now volta antes de o nobred ouvir: o cadastro do projeto espera
// o daemon subir em vez de falhar com "connection refused".
func TestSetupWaitsForDaemonBeforeRegisteringProject(t *testing.T) {
	e := newSetupEnv(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // a porta fica livre: o daemon "ainda não subiu"
	var posted bool
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			w.Write([]byte(`[]`))
		case r.Method == "POST" && r.URL.Path == "/api/projects":
			posted = true
			w.WriteHeader(201)
			w.Write([]byte(`{"id":1,"name":"repo","path":"/r"}`))
		}
	})}
	go func() {
		time.Sleep(400 * time.Millisecond)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Errorf("listen: %v", err)
			return
		}
		srv.Serve(l)
	}()
	t.Cleanup(func() { srv.Close() })
	r := e.run(t, addr, "s\n", e.repo)
	if r.code != 0 || !posted || !strings.Contains(r.stdout, "Projeto repo cadastrado") {
		t.Fatalf("%+v posted=%v", r, posted)
	}
}

func TestSetupGivesUpWaitingForDaemon(t *testing.T) {
	e := newSetupEnv(t)
	e.d.DaemonWait = 300 * time.Millisecond
	start := time.Now()
	r := e.run(t, "127.0.0.1:1", "s\n", e.repo)
	if r.code == 0 || !strings.Contains(r.stdout, "nobre project add") || time.Since(start) > 3*time.Second {
		t.Fatalf("%+v (%v)", r, time.Since(start))
	}
}

// A unidade guarda só o mínimo de reserva: o diretório do nobre, o do claude
// (onde o setup o achou) e os do sistema. venv, nvm e mise ficam de fora: o
// daemon pergunta o PATH ao shell de login na hora de usar.
func TestUnitPATHKeepsOnlyNobreClaudeAndSystem(t *testing.T) {
	claudeDir := t.TempDir()
	os.WriteFile(filepath.Join(claudeDir, "claude"), []byte("#!/bin/sh\n"), 0o755)
	term := "/home/u/.venv/bin:/home/u/.local/share/mise/installs/node/24/bin:" + claudeDir + ":/usr/bin"
	got := unitPATH(term, "/opt/x/bin")
	want := "/opt/x/bin:" + claudeDir + ":/usr/local/bin:/usr/bin:/bin"
	if got != want {
		t.Fatalf("unitPATH = %q, quer %q", got, want)
	}
	if got := unitPATH("/usr/bin", "/usr/bin"); got != "/usr/bin:/usr/local/bin:/bin" {
		t.Fatalf("sem repetir: %q", got)
	}
}
