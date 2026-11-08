package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

const bin = "/opt/nobre/bin/nobre"

func writeSettings(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if content != "" {
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func readHooks(t *testing.T, path string) map[string][]map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	return root.Hooks
}

func backups(t *testing.T, path string) []string {
	m, _ := filepath.Glob(path + ".bak-*")
	return m
}

func TestInstallPreservesForeignHooks(t *testing.T) {
	p := writeSettings(t, `{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/other-plugin stop"}]}]}}`)
	if _, err := InstallHooks(p, bin); err != nil {
		t.Fatal(err)
	}
	h := readHooks(t, p)
	if len(h["Stop"]) != 2 {
		t.Fatalf("Stop = %v", h["Stop"])
	}
	if !strings.Contains(string(mustRead(t, p)), "other-plugin") || !strings.Contains(string(mustRead(t, p)), `"model": "opus"`) {
		t.Fatal("perdeu conteúdo de terceiros")
	}
	for _, ev := range hookEvents {
		if len(h[ev]) == 0 {
			t.Fatalf("faltou %s", ev)
		}
	}
	// matcher só em Pre/PostToolUse; comando e timeout.
	if h["PreToolUse"][0]["matcher"] != "*" || h["PostToolUse"][0]["matcher"] != "*" {
		t.Fatal("matcher * ausente")
	}
	if _, has := h["Stop"][1]["matcher"]; has {
		t.Fatal("Stop não deve ter matcher")
	}
	hk := h["Stop"][1]["hooks"].([]any)[0].(map[string]any)
	if hk["command"] != bin+" hook claude Stop" || hk["timeout"].(float64) != 10 || hk["type"] != "command" {
		t.Fatalf("%v", hk)
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInstallIdempotent(t *testing.T) {
	p := writeSettings(t, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/other stop"}]}]}}`)
	if _, err := InstallHooks(p, bin); err != nil {
		t.Fatal(err)
	}
	first := mustRead(t, p)
	bak := len(backups(t, p))
	rep, err := InstallHooks(p, bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, p)) != string(first) || len(rep.Changed) != 0 || rep.Backup != "" || len(backups(t, p)) != bak {
		t.Fatalf("segunda execução alterou algo: %+v", rep)
	}
	for ev, entries := range readHooks(t, p) {
		n := 0
		for _, e := range entries {
			for _, h := range e["hooks"].([]any) {
				if isOurs(h.(map[string]any)["command"].(string)) {
					n++
				}
			}
		}
		if n != 1 {
			t.Fatalf("%s tem %d entradas nossas", ev, n)
		}
	}
}

func TestInstallFromNewPathReplacesOld(t *testing.T) {
	p := writeSettings(t, "")
	if _, err := InstallHooks(p, "/old/bin/nobre"); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallHooks(p, "/new/bin/nobre"); err != nil {
		t.Fatal(err)
	}
	b := string(mustRead(t, p))
	if strings.Contains(b, "/old/bin/nobre") || strings.Count(b, "/new/bin/nobre hook claude Stop") != 1 {
		t.Fatal(b)
	}
}

func TestInstallInvalidJSONAborts(t *testing.T) {
	for _, bad := range []string{`{`, `{"a":1}}`, `{"a":1}]`, `[1]`, `null`} {
		p := writeSettings(t, bad)
		_, err := InstallHooks(p, bin)
		if err == nil || err.Error() != "~/.claude/settings.json não é um JSON válido; nada foi alterado" {
			t.Fatalf("%q: err = %v", bad, err)
		}
		if string(mustRead(t, p)) != bad || len(backups(t, p)) != 0 {
			t.Fatalf("%q: alterou o arquivo ou criou backup", bad)
		}
	}
}

func TestInstallKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "settings.json")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte(`{"model":"opus"}`), 0o600)
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallHooks(link, bin); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("o link simbólico foi substituído")
	}
	if len(readHooks(t, target)) != len(hookEvents) {
		t.Fatal("o destino não recebeu os hooks")
	}
}

func TestInstallKeepsShellOperatorsReadable(t *testing.T) {
	cmd := `a && b 2>/dev/null <in | c`
	p := writeSettings(t, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"a && b 2>/dev/null <in | c"}]}]}}`)
	if _, err := InstallHooks(p, bin); err != nil {
		t.Fatal(err)
	}
	b := string(mustRead(t, p))
	if !strings.Contains(b, `"command": "`+cmd+`"`) || strings.Contains(b, `\u00`) {
		t.Fatalf("comando alterado: %s", b)
	}
}

func TestBackupNameCollisionGetsSuffix(t *testing.T) {
	p := writeSettings(t, `{"model":"opus"}`)
	now := time.Date(2026, 11, 7, 13, 4, 5, 0, time.UTC)
	os.WriteFile(p+".bak-20261107130405", []byte("antigo"), 0o600)
	rep, err := installHooks(p, bin, now, true)
	if err != nil || rep.Backup != p+".bak-20261107130405-1" {
		t.Fatalf("%v %q", err, rep.Backup)
	}
	if string(mustRead(t, p+".bak-20261107130405")) != "antigo" {
		t.Fatal("sobrescreveu backup existente")
	}
}

func TestEmptyPathsAreNotConfigured(t *testing.T) {
	e := newEnv(t)
	if _, err := e.a.Install(); err == nil || err.Error() != "caminhos não configurados" {
		t.Fatalf("err = %v", err)
	}
	d := e.a.Detect()
	if d.Found || len(d.Checks) != 1 || d.Checks[0].Detail != "caminhos não configurados" {
		t.Fatalf("%+v", d)
	}
}

func TestInstallCreatesBackup(t *testing.T) {
	orig := `{"model":"opus"}`
	p := writeSettings(t, orig)
	now := time.Date(2026, 11, 7, 13, 4, 5, 0, time.UTC)
	rep, err := installHooks(p, bin, now, true)
	if err != nil {
		t.Fatal(err)
	}
	want := p + ".bak-20261107130405"
	if rep.Backup != want || string(mustRead(t, want)) != orig {
		t.Fatalf("backup = %q", rep.Backup)
	}
}

func TestInstallCreatesMissingFileWithoutBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "novo", "settings.json")
	rep, err := InstallHooks(p, bin)
	if err != nil || rep.Backup != "" || len(readHooks(t, p)) != len(hookEvents) {
		t.Fatalf("%v %+v", err, rep)
	}
}

func TestPlanHooksDoesNotWrite(t *testing.T) {
	p := writeSettings(t, `{}`)
	rep, err := PlanHooks(p, bin)
	if err != nil || len(rep.Changed) != len(hookEvents) || string(mustRead(t, p)) != "{}" || len(backups(t, p)) != 0 {
		t.Fatalf("%v %+v", err, rep)
	}
}

func TestInstallSkill(t *testing.T) {
	dir := t.TempDir()
	if err := InstallSkill(dir); err != nil {
		t.Fatal(err)
	}
	b := string(mustRead(t, filepath.Join(dir, "nobre-cockpit", "SKILL.md")))
	for _, want := range []string{"name: nobre-cockpit", "NAC-", "nobre start", "nobre decide", "--why", "Onde parei"} {
		if !strings.Contains(b, want) {
			t.Fatalf("skill sem %q", want)
		}
	}
}

func TestDetectReportsMissingHooks(t *testing.T) {
	e := newEnv(t)
	p := writeSettings(t, `{}`)
	if _, err := InstallHooks(p, bin); err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	json.Unmarshal(mustRead(t, p), &root)
	delete(root["hooks"].(map[string]any), "Stop")
	b, _ := json.Marshal(root)
	os.WriteFile(p, b, 0o600)

	cli := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755)
	e.a.SetPaths(Paths{SettingsPath: p, SkillsDir: t.TempDir(), NobreBin: bin, ClaudeBin: cli,
		Version: func(string) string { return "2.1.0" }})
	d := e.a.Detect()
	if !d.Found || d.Path != cli || d.Version != "2.1.0" || len(d.Checks) != 5 {
		t.Fatalf("%+v", d)
	}
	by := map[string]bool{}
	detail := map[string]string{}
	for _, c := range d.Checks {
		by[c.Key], detail[c.Key] = c.OK, c.Detail
	}
	if !by["cli"] || by["hooks"] || !strings.Contains(detail["hooks"], "Stop") || strings.Contains(detail["hooks"], "SessionEnd") {
		t.Fatalf("%+v", d.Checks)
	}
	if by["skill"] || by["events"] {
		t.Fatalf("%+v", d.Checks)
	}
}

func TestDetectAllGoodAndNoCLI(t *testing.T) {
	e := newEnv(t)
	p := writeSettings(t, "")
	skills := t.TempDir()
	nobre := filepath.Join(t.TempDir(), "nobre")
	if err := os.WriteFile(nobre, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.a.SetPaths(Paths{SettingsPath: p, SkillsDir: skills, NobreBin: nobre, ClaudeBin: filepath.Join(t.TempDir(), "ausente")})
	if _, err := e.a.Install(); err != nil {
		t.Fatal(err)
	}
	d := e.a.Detect()
	if d.Found {
		t.Fatal("CLI inexistente não deveria ser Found")
	}
	for _, c := range d.Checks {
		if c.Key == "hooks" || c.Key == "skill" {
			if !c.OK {
				t.Fatalf("%+v", c)
			}
		}
	}
}

func TestDetectEventsFromLastSession(t *testing.T) {
	e := newEnv(t)
	e.a.SetPaths(Paths{SettingsPath: writeSettings(t, ""), SkillsDir: t.TempDir(), NobreBin: bin, ClaudeBin: "/nao/existe"})
	at := e.now.Add(-5 * time.Minute)
	if err := e.st.StartSession(domain.Session{ID: "claude:z", Agent: "claude", PID: 1, StartedAt: at, LastEventAt: at}); err != nil {
		t.Fatal(err)
	}
	for _, c := range e.a.Detect().Checks {
		if c.Key == "events" && (!c.OK || c.Detail != "último há 5 min") {
			t.Fatalf("%+v", c)
		}
	}
}
