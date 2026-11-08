package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// agentsAPI monta a API com o adaptador do Claude apontando para pastas
// temporárias: nunca toca o ~/.claude real.
func agentsAPI(t *testing.T) (*testAPI, string, string) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	settings := filepath.Join(t.TempDir(), "settings.json")
	skills := t.TempDir()
	ad := claude.New(st, adapter.Deps{Now: func() time.Time { return testNow }})
	// O nobre dos hooks precisa existir para o Detect dar os hooks como OK.
	nobre := filepath.Join(t.TempDir(), "opt", "bin", "nobre")
	if err := os.MkdirAll(filepath.Dir(nobre), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nobre, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ad.SetPaths(claude.Paths{SettingsPath: settings, SkillsDir: skills, NobreBin: nobre, ClaudeBin: "/nao/existe"})
	hub := NewHub()
	t.Cleanup(hub.Close)
	h := New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Adapters: map[string]adapter.Adapter{"claude": ad}})
	return &testAPI{st: st, h: h, hub: hub}, settings, skills
}

func TestAgentsListAndInstall(t *testing.T) {
	a, settings, skills := agentsAPI(t)
	rec := a.do(t, "GET", "/api/agents", nil, 0)
	type row struct {
		Agent     string            `json:"agent"`
		Detection adapter.Detection `json:"detection"`
	}
	rows := decode[[]row](t, rec)
	if rec.Code != 200 || len(rows) != 3 || rows[0].Agent != "claude" || rows[1].Agent != "codex" || rows[2].Agent != "kiro" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rows[0].Detection.Found || len(rows[0].Detection.Checks) != 5 {
		t.Fatalf("%+v", rows[0])
	}
	for _, r := range rows[1:] {
		if r.Detection.Found || len(r.Detection.Checks) != 1 || r.Detection.Checks[0].Detail != "Adaptador chega no plano 2" {
			t.Fatalf("%+v", r)
		}
	}

	rec = a.do(t, "POST", "/api/agents/claude/install", map[string]any{}, 0)
	rep := decode[adapter.InstallReport](t, rec)
	if rec.Code != 200 || len(rep.Changed) == 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	b, err := os.ReadFile(settings)
	if err != nil || !strings.Contains(string(b), "/opt/bin/nobre hook claude Stop") {
		t.Fatalf("settings: %v %s", err, b)
	}
	if _, err := os.Stat(filepath.Join(skills, "nobre-cockpit", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	rows = decode[[]row](t, a.do(t, "GET", "/api/agents", nil, 0))
	for _, c := range rows[0].Detection.Checks {
		if (c.Key == "hooks" || c.Key == "skill") && !c.OK {
			t.Fatalf("%+v", c)
		}
		// "Instalar o que falta" não integra a status line (consentimento).
		if c.Key == "statusline" && c.OK {
			t.Fatalf("status line integrada sem pedido: %+v", c)
		}
	}
	if strings.Contains(string(b), "statusLine") {
		t.Fatalf("install mexeu na status line: %s", b)
	}
}

func TestAgentStatusLineInstallAndUninstall(t *testing.T) {
	a, settings, _ := agentsAPI(t)
	os.WriteFile(settings, []byte(`{"statusLine":{"type":"command","command":"echo oi"}}`), 0o600)
	ch, cancel := a.hub.Subscribe()
	defer cancel()
	statusOK := func() bool {
		t.Helper()
		type row struct {
			Detection adapter.Detection `json:"detection"`
		}
		for _, c := range decode[[]row](t, a.do(t, "GET", "/api/agents", nil, 0))[0].Detection.Checks {
			if c.Key == "statusline" {
				return c.OK
			}
		}
		t.Fatal("sem check statusline")
		return false
	}
	rec := a.do(t, "POST", "/api/agents/claude/statusline", map[string]any{"action": "install"}, 0)
	rep := decode[adapter.InstallReport](t, rec)
	if rec.Code != 200 || len(rep.Changed) != 1 || rep.Backup == "" || !statusOK() {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	select {
	case k := <-ch:
		if k != "config" {
			t.Fatalf("hub = %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("sem publicação config")
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), `statusline || cat; } | {\necho oi\n}`) {
		t.Fatalf("settings: %s", b)
	}
	rec = a.do(t, "POST", "/api/agents/claude/statusline", map[string]any{"action": "uninstall"}, 0)
	if rec.Code != 200 || statusOK() {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(settings); !strings.Contains(string(b), `"command": "echo oi"`) {
		t.Fatalf("settings: %s", b)
	}
	// Ação inválida, recusa da status line, agente sem adaptador ou desconhecido.
	if rec := a.do(t, "POST", "/api/agents/claude/statusline", map[string]any{"action": "x"}, 0); rec.Code != 400 {
		t.Fatalf("ação inválida = %d", rec.Code)
	}
	os.WriteFile(settings, []byte(`{"statusLine":{"type":"command","command":"# só comentário"}}`), 0o600)
	rec = a.do(t, "POST", "/api/agents/claude/statusline", map[string]any{"action": "install"}, 0)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "não consigo integrar esta status line com segurança") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", "/api/agents/codex/statusline", map[string]any{"action": "install"}, 0); rec.Code != http.StatusNotImplemented {
		t.Fatalf("codex = %d", rec.Code)
	}
	if rec := a.do(t, "POST", "/api/agents/xyz/statusline", map[string]any{"action": "install"}, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("xyz = %d", rec.Code)
	}
}

func TestAgentInstallInvalidSettingsAndUnknown(t *testing.T) {
	a, settings, _ := agentsAPI(t)
	os.WriteFile(settings, []byte("{"), 0o600)
	rec := a.do(t, "POST", "/api/agents/claude/install", map[string]any{}, 0)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "não é um JSON válido") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", "/api/agents/codex/install", map[string]any{}, 0); rec.Code != http.StatusNotImplemented {
		t.Fatalf("%d", rec.Code)
	}
	if rec := a.do(t, "POST", "/api/agents/xyz/install", map[string]any{}, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}
