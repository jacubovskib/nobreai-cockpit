package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/integration"
)

type memStore struct{ kv map[string]string }

func (m *memStore) GetKV(key string, v any) (bool, error) {
	raw, ok := m.kv[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal([]byte(raw), v)
}
func (m *memStore) PutKV(key string, v any) error {
	b, err := json.Marshal(v)
	m.kv[key] = string(b)
	return err
}
func (m *memStore) AddConfigLog(string, any, time.Time) error { return nil }

// editSettings muda o settings.json como outra ferramenta faria e garante uma
// data de modificação nova.
func editSettings(t *testing.T, path string, at time.Time, f func(root map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	f(root)
	out, _ := json.Marshal(root)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, at, at)
}

// Com o adaptador de verdade: Ignorar a status line ou a skill vale até o
// próprio item mudar; outra chave do settings.json não traz o alerta de volta.
func TestIgnoreSurvivesUnrelatedSettingsWrite(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	skills := filepath.Join(dir, "skills")
	bin := filepath.Join(dir, "bin", "nobre")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(settings, []byte(`{"statusLine":{"type":"command","command":"echo a"}}`), 0o600)
	a := claude.New(nil, adapter.Deps{Now: time.Now})
	a.SetPaths(claude.Paths{SettingsPath: settings, SkillsDir: skills, NobreBin: bin, ClaudeBin: bin})
	if _, err := a.Install(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.InstallStatusLine(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m := &integration.Monitor{Health: a.Health, Files: a.WatchedFiles, NobreBin: a.NobreBinary,
		Store: &memStore{kv: map[string]string{}}, Now: func() time.Time { return now }, DaemonStart: now.Add(time.Hour)}
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("tudo instalado: %+v", p)
	}
	// Outra ferramenta troca a status line e reescreve a skill.
	editSettings(t, settings, now.Add(time.Minute), func(r map[string]any) {
		r["statusLine"] = map[string]any{"type": "command", "command": "echo a"}
	})
	os.WriteFile(claude.SkillFile(skills), []byte("outra skill\n"), 0o644)
	if p := m.CheckNow(); len(p) != 2 {
		t.Fatalf("quer status line e skill: %+v", p)
	}
	for _, k := range []string{"statusline", "skill"} {
		if err := m.Ignore(k); err != nil {
			t.Fatal(err)
		}
	}
	editSettings(t, settings, now.Add(2*time.Minute), func(r map[string]any) { r["model"] = "opus" })
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("outra chave do settings.json trouxe o alerta: %+v", p)
	}
	editSettings(t, settings, now.Add(3*time.Minute), func(r map[string]any) {
		r["statusLine"] = map[string]any{"type": "command", "command": "echo b"}
	})
	if p := m.CheckNow(); len(p) != 1 || p[0].Key != "statusline" {
		t.Fatalf("comando novo deve alertar só a status line: %+v", p)
	}
}
