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

	// settings.json ausente: a status line não foi trocada por outro comando
	e.a.SetPaths(Paths{SettingsPath: filepath.Join(t.TempDir(), "settings.json"), SkillsDir: skills, NobreBin: bin, ClaudeBin: cli})
	if c := healthByKey(e.a.Health())["statusline"]; c.Code != "no_settings" || c.State != "missing" {
		t.Fatalf("%+v", c)
	}
}

// State resume o próprio item: o comando da status line e a data e o tamanho
// da skill; outra chave do settings.json não o muda.
func TestHealthStatePerItem(t *testing.T) {
	e := newEnv(t)
	skills := t.TempDir()
	p := writeSettings(t, `{"statusLine":{"type":"command","command":"echo a"}}`)
	e.a.SetPaths(Paths{SettingsPath: p, SkillsDir: skills, NobreBin: bin, ClaudeBin: "/nao/existe"})
	h := healthByKey(e.a.Health())
	if h["statusline"].State != "echo a" || h["skill"].State != "missing" {
		t.Fatalf("%+v", h)
	}
	os.WriteFile(p, []byte(`{"model":"opus","statusLine":{"type":"command","command":"echo a"}}`), 0o600)
	if c := healthByKey(e.a.Health())["statusline"]; c.State != "echo a" {
		t.Fatalf("%+v", c)
	}
	sf := SkillFile(skills)
	os.MkdirAll(filepath.Dir(sf), 0o755)
	os.WriteFile(sf, []byte("velha"), 0o644)
	if c := healthByKey(e.a.Health())["skill"]; c.State == "missing" || c.State == "" {
		t.Fatalf("%+v", c)
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
