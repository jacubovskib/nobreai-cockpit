package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

func (a *testAPI) startSess(t *testing.T, id, cwd string, project *int64, capture bool) {
	t.Helper()
	err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", PID: 1, Cwd: cwd,
		ProjectID: project, FullCapture: capture, StartedAt: testNow, LastEventAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
}

func (a *testAPI) logKinds(t *testing.T) []domain.ConfigLogEntry {
	t.Helper()
	es, err := a.st.RecentConfigLog(100)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func TestPutSettingsCountsNotifiedSessions(t *testing.T) {
	a := newTestAPI(t)
	p, _ := a.st.CreateProject("api", "/w/api")
	seen := domain.SeenConfig{AgentCreatesCards: true, Capture: true}
	for _, id := range []string{"claude:a", "claude:b", "claude:c"} {
		a.startSess(t, id, "/w/api", &p.ID, true)
		if ok, err := a.st.SwapSeenConfig(id, nil, seen); err != nil || !ok {
			t.Fatalf("seen: %v %v", ok, err)
		}
	}
	if err := a.st.EndSession("claude:c", "x", testNow); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "PUT", "/api/settings", map[string]any{"agent_creates_cards": false}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	out := decode[map[string]any](t, rec)
	if out["notified_sessions"] != float64(2) || out["agent_creates_cards"] != false {
		t.Fatalf("resposta = %v", out)
	}
	es := a.logKinds(t)
	if len(es) != 1 || es[0].Kind != domain.LogSettingChanged {
		t.Fatalf("log = %+v", es)
	}
	var d map[string]any
	if err := json.Unmarshal(es[0].Data, &d); err != nil || d["key"] != "agent_creates_cards" || d["from"] != true || d["to"] != false {
		t.Fatalf("data = %v %v", d, err)
	}
}

func TestPutSettingsUnchangedNoLog(t *testing.T) {
	a := newTestAPI(t)
	st, _ := a.st.GetSettings()
	rec := a.do(t, "PUT", "/api/settings", st, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	if n := decode[map[string]any](t, rec)["notified_sessions"]; n != float64(0) {
		t.Fatalf("notified = %v", n)
	}
	if es := a.logKinds(t); len(es) != 0 {
		t.Fatalf("log = %+v", es)
	}
}

func TestPatchProjectCaptureOffStopsLiveSessions(t *testing.T) {
	a := newTestAPI(t)
	p, _ := a.st.CreateProject("api", "/w/api")
	a.startSess(t, "claude:s", "/w/api", &p.ID, true)
	rec := a.do(t, "PATCH", fmt.Sprintf("/api/projects/%d", p.ID), map[string]any{"name": "api", "path": "/w/api", "capture": false}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	if n := decode[map[string]any](t, rec)["notified_sessions"]; n != float64(1) {
		t.Fatalf("notified = %v", n)
	}
	x, _ := a.st.GetSession("claude:s")
	if x.FullCapture || x.ProjectID == nil || *x.ProjectID != p.ID {
		t.Fatalf("sessão = %+v", x)
	}
	if es := a.logKinds(t); len(es) != 1 || es[0].Kind != domain.LogProjectCapture {
		t.Fatalf("log = %+v", es)
	}
}

func TestPatchProjectCaptureOnAdoptsSessions(t *testing.T) {
	a := newTestAPI(t)
	p, _ := a.st.CreateProject("api", "/w/api")
	patch := func(capture bool) map[string]any {
		rec := a.do(t, "PATCH", fmt.Sprintf("/api/projects/%d", p.ID), map[string]any{"name": "api", "path": "/w/api", "capture": capture}, 0)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
		}
		return decode[map[string]any](t, rec)
	}
	patch(false)
	a.startSess(t, "claude:s", "/w/api/src", nil, false)
	a.startSess(t, "claude:out", "/w/other", nil, false)
	if n := patch(true)["notified_sessions"]; n != float64(1) {
		t.Fatalf("notified = %v", n)
	}
	x, _ := a.st.GetSession("claude:s")
	if x.ProjectID == nil || *x.ProjectID != p.ID || !x.FullCapture {
		t.Fatalf("sessão = %+v", x)
	}
	if o, _ := a.st.GetSession("claude:out"); o.ProjectID != nil || o.FullCapture {
		t.Fatalf("fora do projeto mudou: %+v", o)
	}
}

func TestPatchProjectCaptureOnTurnsBackFamilySessionsOnly(t *testing.T) {
	a := newTestAPI(t)
	p, _ := a.st.CreateProject("api", "/w/api")
	a.startSess(t, "claude:off", "/w/api", &p.ID, false)
	a.startSess(t, "claude:on", "/w/api", &p.ID, true)
	if _, err := a.st.DB.Exec(`UPDATE projects SET capture=0 WHERE id=?`, p.ID); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "PATCH", fmt.Sprintf("/api/projects/%d", p.ID), map[string]any{"name": "api", "path": "/w/api", "capture": true}, 0)
	if n := decode[map[string]any](t, rec)["notified_sessions"]; n != float64(1) {
		t.Fatalf("notified = %v (%s)", n, rec.Body)
	}
}

func TestPatchParentCaptureFamily(t *testing.T) {
	a := newTestAPI(t)
	parent, _ := a.st.CreateProject("mono", "/w/mono")
	child, _, err := a.st.CreateChild("web", "/w/mono/apps/web", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.startSess(t, "claude:child", "/w/mono/apps/web", &child.ID, true)
	path := fmt.Sprintf("/api/projects/%d", parent.ID)
	rec := a.do(t, "PATCH", path, map[string]any{"name": "mono", "path": "/w/mono", "capture": false}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	if n := decode[map[string]any](t, rec)["notified_sessions"]; n != float64(1) {
		t.Fatalf("notified = %v", n)
	}
	if x, _ := a.st.GetSession("claude:child"); x.FullCapture {
		t.Fatalf("sessão do filho segue capturando: %+v", x)
	}
	// Ligar de novo adota a sessão sem projeto sob a pasta do filho, no filho.
	a.startSess(t, "claude:loose", "/w/mono/apps/web/src", nil, false)
	rec = a.do(t, "PATCH", path, map[string]any{"name": "mono", "path": "/w/mono", "capture": true}, 0)
	if n := decode[map[string]any](t, rec)["notified_sessions"]; n != float64(2) {
		t.Fatalf("notified = %v (%s)", n, rec.Body)
	}
	x, _ := a.st.GetSession("claude:loose")
	if x.ProjectID == nil || *x.ProjectID != child.ID || !x.FullCapture {
		t.Fatalf("sessão = %+v", x)
	}
	if c, _ := a.st.GetSession("claude:child"); !c.FullCapture {
		t.Fatalf("filho não religou: %+v", c)
	}
	if es := a.logKinds(t); len(es) != 2 {
		t.Fatalf("log deve ter uma linha por mudança de captura: %+v", es)
	}
}

func TestConfigLogRoute(t *testing.T) {
	a := newTestAPI(t)
	for i, k := range []string{"a", "b"} {
		if err := a.st.AddConfigLog(k, map[string]int{"n": i}, testNow.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	es := decode[[]domain.ConfigLogEntry](t, a.do(t, "GET", "/api/config-log?limit=1", nil, 0))
	if len(es) != 1 || es[0].Kind != "b" {
		t.Fatalf("log = %+v", es)
	}
	if es := decode[[]domain.ConfigLogEntry](t, a.do(t, "GET", "/api/config-log", nil, 0)); len(es) != 2 {
		t.Fatalf("log = %+v", es)
	}
	if rec := a.do(t, "GET", "/api/config-log?limit=x", nil, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit inválido = %d", rec.Code)
	}
}

func TestPutSettingsLogsRulesAndSameLengthTerminalCmd(t *testing.T) {
	a := newTestAPI(t)
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"rules": map[string]bool{"goal": false}}, 0); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"terminal_cmd": []string{"xterm", "-e"}}, 0); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"terminal_cmd": []string{"kitty", "-e"}}, 0); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	got := map[string]int{}
	for _, e := range a.logKinds(t) {
		var d map[string]any
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		got[d["key"].(string)]++
	}
	if got["rules"] != 1 || got["terminal_cmd"] != 2 {
		t.Fatalf("log por chave = %v", got)
	}
}

func TestPatchProjectCaptureOffCountsOnlyCapturing(t *testing.T) {
	a := newTestAPI(t)
	p, _ := a.st.CreateProject("api", "/w/api")
	a.startSess(t, "claude:on", "/w/api", &p.ID, true)
	a.startSess(t, "claude:off", "/w/api", &p.ID, false)
	rec := a.do(t, "PATCH", fmt.Sprintf("/api/projects/%d", p.ID), map[string]any{"name": "api", "path": "/w/api", "capture": false}, 0)
	if n := decode[map[string]any](t, rec)["notified_sessions"]; n != float64(1) {
		t.Fatalf("notified = %v (%s)", n, rec.Body)
	}
}

// locGit é um Git falso que sabe de quais repositórios são os worktrees
// ligados (pasta do worktree -> checkout principal).
type locGit struct {
	*fakeGit
	mains map[string]string
}

func (g *locGit) Locate(dir string) gitinfo.Location {
	return gitinfo.Location{Root: dir, Main: g.mains[dir], Linked: g.mains[dir] != ""}
}
func (g *locGit) Worktrees(string) ([]gitinfo.Worktree, error) { return nil, nil }

func (a *testAPI) startLinkedSess(t *testing.T, id, cwd, worktree string) {
	t.Helper()
	err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", PID: 1, Cwd: cwd, Worktree: worktree, WorktreeLinked: true,
		StartedAt: testNow, LastEventAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
}

func (a *testAPI) patchCapture(t *testing.T, p domain.Project, on bool) float64 {
	t.Helper()
	rec := a.do(t, "PATCH", fmt.Sprintf("/api/projects/%d", p.ID), map[string]any{"name": p.Name, "path": p.Path, "capture": on}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	n, _ := decode[map[string]any](t, rec)["notified_sessions"].(float64)
	return n
}

func (a *testAPI) owner(t *testing.T, id string) (*int64, bool) {
	t.Helper()
	x, err := a.st.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	return x.ProjectID, x.FullCapture
}

// Um projeto avulso B sem captura dentro da pasta da família A é o dono das
// sessões na pasta dele (como no hook): ligar a captura de A não as adota.
func TestCaptureOnSkipsSessionsOwnedByNestedProject(t *testing.T) {
	a := newTestAPI(t)
	fam, _ := a.st.CreateProject("a", "/w/a")
	b, _ := a.st.CreateProject("b", "/w/a/b")
	a.patchCapture(t, fam, false)
	a.patchCapture(t, b, false)
	a.startSess(t, "claude:b", "/w/a/b/src", nil, false)
	a.startSess(t, "claude:a", "/w/a/src", nil, false)
	if n := a.patchCapture(t, fam, true); n != 1 {
		t.Fatalf("notified = %v", n)
	}
	if pid, capture := a.owner(t, "claude:b"); pid != nil || capture {
		t.Fatalf("sessão de B adotada por A: %v %v", pid, capture)
	}
	if pid, capture := a.owner(t, "claude:a"); pid == nil || *pid != fam.ID || !capture {
		t.Fatalf("sessão de A: %v %v", pid, capture)
	}
}

// Worktree ligado dentro do repositório (.worktrees/x) ou fora dele: a sessão
// vai para o sub-projeto do mesmo lugar no checkout principal, como no hook.
func TestCaptureOnAdoptsLinkedWorktreeSessionsInApp(t *testing.T) {
	a := newTestAPI(t)
	g := &locGit{fakeGit: &fakeGit{branch: "main"}, mains: map[string]string{
		"/r/loja/.worktrees/x": "/r/loja", "/tmp/wt": "/r/loja",
	}}
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, Git: g})
	mono, _ := a.st.CreateProject("loja", "/r/loja")
	web, _, err := a.st.CreateChild("web", "/r/loja/apps/web", mono.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.patchCapture(t, mono, false)
	a.startLinkedSess(t, "claude:in", "/r/loja/.worktrees/x/apps/web/src", "/r/loja/.worktrees/x")
	a.startLinkedSess(t, "claude:out", "/tmp/wt/apps/web", "/tmp/wt")
	a.startLinkedSess(t, "claude:root", "/tmp/wt", "/tmp/wt")
	a.startLinkedSess(t, "claude:other", "/tmp/outro", "/tmp/outro")
	if n := a.patchCapture(t, mono, true); n != 3 {
		t.Fatalf("notified = %v", n)
	}
	for id, want := range map[string]int64{"claude:in": web.ID, "claude:out": web.ID, "claude:root": mono.ID} {
		if pid, capture := a.owner(t, id); pid == nil || *pid != want || !capture {
			t.Fatalf("%s: projeto %v captura %v, quer %d", id, pid, capture, want)
		}
	}
	if pid, _ := a.owner(t, "claude:other"); pid != nil {
		t.Fatalf("sessão fora do repositório adotada: %v", *pid)
	}
}
