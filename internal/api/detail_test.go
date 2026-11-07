package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

type detailSessionsResp struct {
	Live     *struct{ ID string } `json:"live"`
	Sessions []struct {
		ID    string `json:"id"`
		Usage struct {
			Input   int64   `json:"input"`
			CostUSD float64 `json:"cost_usd"`
		} `json:"usage"`
	} `json:"sessions"`
	Totals struct {
		CostUSD float64 `json:"cost_usd"`
	} `json:"totals"`
}

// Re-attach A→B: o custo de A fica em A, e A continua listando a sessão com o
// uso que ela teve em A.
func TestCardDetailKeepsHistoryAfterReattach(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	ca, _ := a.st.CreateCard(domain.Card{Title: "A", ProjectID: p.ID})
	cb, _ := a.st.CreateCard(domain.Card{Title: "B", ProjectID: p.ID})
	if err := a.st.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.StartSession(domain.Session{ID: "claude:x", Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.AttachSession("claude:x", ca.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := a.st.UpsertUsage([]domain.UsageRow{{SessionID: "claude:x", MessageID: "m1", Model: "m", Input: 2_000_000, At: testNow}}); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/cards/"+cb.Key+"/attach", map[string]any{"session_id": "claude:x"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("attach B = %d %s", rec.Code, rec.Body)
	}
	if err := a.st.UpsertUsage([]domain.UsageRow{{SessionID: "claude:x", MessageID: "m2", Model: "m", Input: 1_000_000, At: testNow}}); err != nil {
		t.Fatal(err)
	}
	da := decode[detailSessionsResp](t, a.do(t, "GET", "/api/cards/"+ca.Key, nil, 0))
	if da.Totals.CostUSD != 2 || len(da.Sessions) != 1 || da.Sessions[0].ID != "claude:x" || da.Sessions[0].Usage.Input != 2_000_000 || da.Live != nil {
		t.Fatalf("A = %+v", da)
	}
	db := decode[detailSessionsResp](t, a.do(t, "GET", "/api/cards/"+cb.Key, nil, 0))
	if db.Totals.CostUSD != 1 || len(db.Sessions) != 1 || db.Sessions[0].Usage.Input != 1_000_000 || db.Live == nil {
		t.Fatalf("B = %+v", db)
	}
	list := decode[[]struct {
		Key          string `json:"key"`
		SessionCount int    `json:"session_count"`
		Totals       struct {
			CostUSD float64 `json:"cost_usd"`
		} `json:"totals"`
	}](t, a.do(t, "GET", "/api/cards", nil, 0))
	for _, c := range list {
		want := map[string]float64{ca.Key: 2, cb.Key: 1}[c.Key]
		if c.Totals.CostUSD != want || c.SessionCount != 1 {
			t.Fatalf("board %s = %+v, want custo %v", c.Key, c, want)
		}
	}
}

// Processo morto (pid_dead_at) durante a carência: não é "ao vivo".
func TestCardWithDeadProcessIsNotLive(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	if err := a.st.StartSession(domain.Session{ID: "claude:x", Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.AttachSession("claude:x", c.Key, false); err != nil {
		t.Fatal(err)
	}
	dead := testNow.Add(-5 * time.Minute)
	if err := a.st.MarkPIDDead("claude:x", dead); err != nil {
		t.Fatal(err)
	}
	list := decode[[]struct {
		Live      bool       `json:"live"`
		PIDDeadAt *time.Time `json:"pid_dead_at"`
	}](t, a.do(t, "GET", "/api/cards", nil, 0))
	if len(list) != 1 || list[0].Live || list[0].PIDDeadAt == nil || !list[0].PIDDeadAt.Equal(dead) {
		t.Fatalf("board = %+v", list)
	}
	d := decode[detailSessionsResp](t, a.do(t, "GET", "/api/cards/"+c.Key, nil, 0))
	if d.Live != nil {
		t.Fatalf("detail.live = %+v, want null", d.Live)
	}
}

// Em andamento só com sessão viva: sem ela o PATCH responde 409.
func TestPatchInProgressNeedsLiveSession(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "em_andamento"}, 0)
	if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] != msgInProgressNeedsLive {
		t.Fatalf("sem sessão = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != domain.Backlog {
		t.Fatalf("column = %s", got.Column)
	}
	if err := a.st.StartSession(domain.Session{ID: "claude:x", Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.AttachSession("claude:x", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "pausado"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("pausar = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "em_andamento"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("com sessão viva = %d %s", rec.Code, rec.Body)
	}
	if err := a.st.MarkPIDDead("claude:x", testNow); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "pausado"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("pausar = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "em_andamento"}, 0); rec.Code != http.StatusConflict {
		t.Fatalf("processo morto = %d %s", rec.Code, rec.Body)
	}
}
