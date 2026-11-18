package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

func healthByKey(cs []adapter.Check) map[string]adapter.Check {
	m := map[string]adapter.Check{}
	for _, c := range cs {
		m[c.Key] = c
	}
	return m
}

func TestHealthCodes(t *testing.T) {
	e := newEnv(t)
	skills := t.TempDir()
	cli := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755)

	// sem hooks nem skill
	p := writeSettings(t, `{}`)
	e.a.SetPaths(Paths{SettingsPath: p, SkillsDir: skills, NobreBin: bin, ClaudeBin: cli})
	h := healthByKey(e.a.Health())
	if h["hooks"].Code != "missing" || h["skill"].Code != "missing" || h["cli"].Code != "" || !h["cli"].OK {
		t.Fatalf("%+v", h)
	}
	if _, ok := h["events"]; ok {
		t.Fatal("Health não traz events")
	}

	// hooks apontando para binário inexistente
	if _, err := InstallHooks(p, "/nao/existe/nobre"); err != nil {
		t.Fatal(err)
	}
	if c := healthByKey(e.a.Health())["hooks"]; c.Code != "dead_binary" || c.OK {
		t.Fatalf("%+v", c)
	}

	// skill diferente
	sf := SkillFile(skills)
	os.MkdirAll(filepath.Dir(sf), 0o755)
	os.WriteFile(sf, []byte("velha"), 0o644)
	if c := healthByKey(e.a.Health())["skill"]; c.Code != "outdated" {
		t.Fatalf("%+v", c)
	}

	// CLI ausente
	e.a.SetPaths(Paths{SettingsPath: p, SkillsDir: skills, NobreBin: bin, ClaudeBin: "/nao/existe"})
	if c := healthByKey(e.a.Health())["cli"]; c.Code != "not_found" || c.OK {
		t.Fatalf("%+v", c)
	}

	// settings.json ilegível
	bad := writeSettings(t, `{`)
	e.a.SetPaths(Paths{SettingsPath: bad, SkillsDir: skills, NobreBin: bin, ClaudeBin: cli})
	h = healthByKey(e.a.Health())
	if h["hooks"].Code != "unreadable" || h["statusline"].Code != "unreadable" {
		t.Fatalf("%+v", h)
	}
}

func TestHealthSkipsVersion(t *testing.T) {
	e := newEnv(t)
	cli := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755)
	e.a.SetPaths(Paths{SettingsPath: writeSettings(t, `{}`), SkillsDir: t.TempDir(), NobreBin: bin, ClaudeBin: cli,
		Version: func(string) string { t.Fatal("Health não deve pedir a versão"); return "" }})
	e.a.Health()
}

func TestWatchedFiles(t *testing.T) {
	e := newEnv(t)
	e.a.SetPaths(Paths{SettingsPath: "/s/settings.json", SkillsDir: "/s/skills", NobreBin: "/b/nobre"})
	got := e.a.WatchedFiles()
	want := []string{"/s/settings.json", "/s/skills/nobre-cockpit/SKILL.md", "/b/nobre"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("%v", got)
	}
}
