package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func TestSettingsPutMergesOverCurrent(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "PUT", "/api/settings", map[string]any{"grace_min": 30, "rules": map[string]bool{"how_to_test": false}}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	st, _ := a.st.GetSettings()
	if st.GraceMin != 30 || st.Rules["how_to_test"] || !st.Rules["goal"] || st.WhereStoppedTTLMin != 15 {
		t.Fatalf("settings = %+v", st)
	}
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"incomplete_action": "x"}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("ação inválida = %d", rec.Code)
	}
}

func TestLimitsPricesProjects(t *testing.T) {
	a := newTestAPI(t)
	if rec := a.do(t, "PUT", "/api/limits", map[string]any{"agent": "claude", "daily_usd": 15, "action": "stop"}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("limite com ação inválida = %d", rec.Code)
	}
	if rec := a.do(t, "PUT", "/api/limits", map[string]any{"agent": "claude", "daily_usd": 15}, 0); rec.Code != http.StatusOK {
		t.Fatalf("limite = %d %s", rec.Code, rec.Body)
	}
	ls := decode[[]domain.Limit](t, a.do(t, "GET", "/api/limits", nil, 0))
	if len(ls) != 1 || ls[0].DailyUSD != 15 || ls[0].WarnPct != 80 || ls[0].Action != "warn" {
		t.Fatalf("limits = %+v", ls)
	}

	for _, body := range []map[string]any{{"model": "m"}, {"model": "m", "input_per_m": 3}, {"model": "m", "output_per_m": 3}} {
		rec := a.do(t, "PUT", "/api/prices", body, 0)
		if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "Informe os preços de entrada e saída (maiores que zero)" {
			t.Fatalf("preço sem entrada/saída = %d %s", rec.Code, rec.Body)
		}
	}
	if rec := a.do(t, "PUT", "/api/prices", map[string]any{"model": "m", "input_per_m": 3, "output_per_m": 15}, 0); rec.Code != http.StatusOK {
		t.Fatalf("preço = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "DELETE", "/api/prices?model=m", nil, 0); rec.Code != http.StatusOK {
		t.Fatalf("delete preço = %d", rec.Code)
	}
	if ps := decode[[]domain.Price](t, a.do(t, "GET", "/api/prices", nil, 0)); len(ps) != 0 {
		t.Fatalf("prices = %+v", ps)
	}

	if rec := a.do(t, "POST", "/api/projects", map[string]any{"path": "/w/api"}, 0); rec.Code != http.StatusCreated {
		t.Fatalf("projeto = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", "/api/projects", map[string]any{"path": "/w/api/"}, 0); rec.Code != http.StatusConflict {
		t.Fatalf("projeto duplicado = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "PATCH", "/api/projects/1", map[string]any{"capture": false}, 0); rec.Code != http.StatusOK {
		t.Fatalf("patch projeto = %d %s", rec.Code, rec.Body)
	}
	ps := decode[[]domain.Project](t, a.do(t, "GET", "/api/projects", nil, 0))
	if len(ps) != 1 || ps[0].Name != "api" || ps[0].Capture {
		t.Fatalf("projects = %+v", ps)
	}
	if _, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: 1}); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "DELETE", "/api/projects/1", nil, 0); rec.Code != http.StatusConflict {
		t.Fatalf("delete projeto com card = %d %s", rec.Code, rec.Body)
	}
}

func TestSessionsWithUsage(t *testing.T) {
	a := newTestAPI(t)
	if err := a.st.StartSession(domain.Session{ID: "claude:s", Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.UpsertUsage([]domain.UsageRow{{SessionID: "claude:s", MessageID: "m1", Model: "sem-preco", Input: 10, Output: 5, At: testNow}}); err != nil {
		t.Fatal(err)
	}
	type sessionJSON struct {
		ID       string `json:"id"`
		Unpriced bool   `json:"unpriced"`
		Usage    struct {
			Input int64 `json:"input"`
		} `json:"usage"`
	}
	got := decode[[]sessionJSON](t, a.do(t, "GET", "/api/sessions?limit=5", nil, 0))
	if len(got) != 1 || got[0].ID != "claude:s" || !got[0].Unpriced || got[0].Usage.Input != 10 {
		t.Fatalf("sessions = %+v", got)
	}
	if rec := a.do(t, "GET", "/api/usage/totals?group=nada", nil, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("grupo inválido = %d", rec.Code)
	}
	if rec := a.do(t, "GET", "/api/usage/totals?group=agent&from=2026-11-01&to=2026-11-08", nil, 0); rec.Code != http.StatusOK {
		t.Fatalf("totals = %d %s", rec.Code, rec.Body)
	}
}

// Preço de cache em branco (null) fica desconhecido; 0 explícito fica 0.
func TestPutPriceCacheNull(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "PUT", "/api/prices", map[string]any{"model": "m", "input_per_m": 3, "output_per_m": 15, "cache_write_per_m": nil, "cache_read_per_m": 0}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	got := decode[[]map[string]any](t, a.do(t, "GET", "/api/prices", nil, 0))
	if len(got) != 1 || got[0]["cache_write_per_m"] != nil || got[0]["cache_read_per_m"] != float64(0) {
		t.Fatalf("prices = %v", got)
	}
	if rec := a.do(t, "PUT", "/api/prices", map[string]any{"model": "m", "input_per_m": 3, "output_per_m": 15, "cache_read_per_m": -1}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("negativo = %d", rec.Code)
	}
}

func TestCreateProjectReassignsAndSetsBase(t *testing.T) {
	main, _ := gitRepoWithWorktree(t)
	gitRun(t, main, "branch", "develop")
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	a := &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Git: gitinfo.Exec{}})}
	now := testNow
	if err := st.StartSession(domain.Session{ID: "claude:1", Agent: "claude", ProviderSessionID: "1", PID: 5, Cwd: main, StartedAt: now, LastEventAt: now}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/projects", map[string]any{"path": main}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("projeto = %d %s", rec.Code, rec.Body)
	}
	out := decode[map[string]any](t, rec)
	if out["reassigned"] != float64(1) || out["base_branch"] != "develop" {
		t.Fatalf("resposta = %v", out)
	}
	x, err := st.GetSession("claude:1")
	if err != nil || x.ProjectID == nil || !x.FullCapture {
		t.Fatalf("sessão = %+v, %v", x, err)
	}
}
