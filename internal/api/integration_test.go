package api

import (
	"os"
	"reflect"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/integration"
)

type fakeIntegration struct {
	problems []integration.Problem
	ignored  []string
	fixed    [][]string
	changed  [][]string
	removed  [][]string
	origins  []string
	checks   int
}

func (f *fakeIntegration) Problems() []integration.Problem { return f.problems }
func (f *fakeIntegration) CheckNow() []integration.Problem { f.checks++; return f.problems }
func (f *fakeIntegration) Ignore(k string) error           { f.ignored = append(f.ignored, k); return nil }
func (f *fakeIntegration) Fixed(keys, changed []string) {
	f.fixed = append(f.fixed, keys)
	f.changed = append(f.changed, changed)
}
func (f *fakeIntegration) Removed(keys []string, origin string) {
	f.removed = append(f.removed, keys)
	f.origins = append(f.origins, origin)
}

func TestOverviewHasIntegrationAlert(t *testing.T) {
	f := &fakeIntegration{problems: []integration.Problem{{Key: "hooks", Text: "Os hooks sumiram.", Action: integration.ActionReinstall}}}
	a, _, _ := agentsAPIWith(t, f)
	o := decode[Overview](t, a.do(t, "GET", "/api/overview", nil, 0))
	for _, al := range o.Alerts {
		if al.Kind == "integration" {
			if al.Agent != "claude" || al.Text != "Os hooks sumiram." || al.Integration == nil || al.Integration.Action != "reinstall" {
				t.Fatalf("%+v", al)
			}
			return
		}
	}
	t.Fatalf("sem alerta de integração: %+v", o.Alerts)
}

func TestIgnoreRoute(t *testing.T) {
	f := &fakeIntegration{}
	a, _, _ := agentsAPIWith(t, f)
	rec := a.do(t, "POST", "/api/integration/hooks/ignore", nil, 0)
	if rec.Code != 204 || !reflect.DeepEqual(f.ignored, []string{"hooks"}) {
		t.Fatalf("%d %v", rec.Code, f.ignored)
	}
}

func TestRemovedRoute(t *testing.T) {
	f := &fakeIntegration{}
	a, _, _ := agentsAPIWith(t, f)
	rec := a.do(t, "POST", "/api/integration/removed", map[string]any{"keys": []string{"statusline"}, "origin": "cli"}, 0)
	if rec.Code != 204 || !reflect.DeepEqual(f.removed, [][]string{{"statusline"}}) || f.origins[0] != "cli" {
		t.Fatalf("%d %v %v", rec.Code, f.removed, f.origins)
	}
	rec = a.do(t, "POST", "/api/integration/removed", map[string]any{"keys": []string{"banana"}, "origin": "cli"}, 0)
	if rec.Code != 400 || len(f.removed) != 1 {
		t.Fatalf("%d %v", rec.Code, f.removed)
	}
}

func TestInstallMarksFixed(t *testing.T) {
	f := &fakeIntegration{}
	a, _, _ := agentsAPIWith(t, f)
	if rec := a.do(t, "POST", "/api/agents/claude/install", map[string]any{}, 0); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !reflect.DeepEqual(f.fixed, [][]string{{"hooks", "skill"}}) || len(f.changed[0]) == 0 {
		t.Fatalf("%v %v", f.fixed, f.changed)
	}
}

// Reinstalar sem nada a mudar não é um conserto: não vira linha de
// "corrigida" no histórico (o monitor só confere de novo).
func TestInstallWithoutChangesDoesNotMarkFixed(t *testing.T) {
	f := &fakeIntegration{}
	a, settings, _ := agentsAPIWith(t, f)
	os.WriteFile(settings, []byte(`{"statusLine":{"type":"command","command":"echo oi"}}`), 0o600)
	for range 2 {
		if rec := a.do(t, "POST", "/api/agents/claude/install", map[string]any{}, 0); rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if rec := a.do(t, "POST", "/api/agents/claude/statusline", map[string]any{"action": "install"}, 0); rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	if !reflect.DeepEqual(f.fixed, [][]string{{"hooks", "skill"}, {"statusline"}}) || f.checks != 2 {
		t.Fatalf("fixed=%v checks=%d", f.fixed, f.checks)
	}
}

func TestStatusLineMarksFixedAndRemoved(t *testing.T) {
	f := &fakeIntegration{}
	a, settings, _ := agentsAPIWith(t, f)
	os.WriteFile(settings, []byte(`{"statusLine":{"type":"command","command":"echo oi"}}`), 0o600)
	if rec := a.do(t, "POST", "/api/agents/claude/statusline", map[string]any{"action": "install"}, 0); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !reflect.DeepEqual(f.fixed, [][]string{{"statusline"}}) {
		t.Fatalf("%v", f.fixed)
	}
	if rec := a.do(t, "POST", "/api/agents/claude/statusline", map[string]any{"action": "uninstall"}, 0); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !reflect.DeepEqual(f.removed, [][]string{{"statusline"}}) || f.origins[0] != "config" {
		t.Fatalf("%v %v", f.removed, f.origins)
	}
}

func TestStatusLineUninstallMarksRemoved(t *testing.T) { TestStatusLineMarksFixedAndRemoved(t) }

func TestInstallPlanDoesNotWrite(t *testing.T) {
	a, settings, skills := agentsAPIWith(t, nil)
	before := []byte(`{"model":"x"}`)
	os.WriteFile(settings, before, 0o600)
	rec := a.do(t, "GET", "/api/agents/claude/install/plan", nil, 0)
	rep := decode[struct {
		Changed []string `json:"changed"`
	}](t, rec)
	if rec.Code != 200 || len(rep.Changed) == 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	after, _ := os.ReadFile(settings)
	if string(after) != string(before) {
		t.Fatalf("settings mudou: %s", after)
	}
	if ents, _ := os.ReadDir(skills); len(ents) != 0 {
		t.Fatalf("skill gravada: %v", ents)
	}
}

func TestNilIntegration503(t *testing.T) {
	a, _, _ := agentsAPIWith(t, nil)
	for _, r := range [][2]string{{"GET", "/api/integration"}, {"POST", "/api/integration/check"},
		{"POST", "/api/integration/hooks/ignore"}, {"POST", "/api/integration/removed"}} {
		var body any
		if r[1] == "/api/integration/removed" {
			body = map[string]any{"keys": []string{"hooks"}}
		}
		if rec := a.do(t, r[0], r[1], body, 0); rec.Code != 503 {
			t.Fatalf("%v: %d", r, rec.Code)
		}
	}
	o := decode[Overview](t, a.do(t, "GET", "/api/overview", nil, 0))
	for _, al := range o.Alerts {
		if al.Kind == "integration" {
			t.Fatal("alerta sem monitor")
		}
	}
}
