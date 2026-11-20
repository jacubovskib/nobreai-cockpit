package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

func liveAPISession(t *testing.T, a *testAPI, id string, full bool, card *int64) {
	t.Helper()
	x := domain.Session{ID: id, Agent: "claude", ProviderSessionID: id, PID: 1, TranscriptPath: "/main", Model: "claude-sonnet-x",
		StartedAt: testNow, LastEventAt: testNow, FullCapture: full, CardID: card}
	if err := a.st.StartSession(x); err != nil {
		t.Fatal(err)
	}
}

func liveAPITurn(t *testing.T, a *testAPI, id string, tokens, write int64) {
	t.Helper()
	turn := usage.ContextTurn{Turn: 0, MessageID: "m", At: testNow.Add(-time.Minute), Tokens: tokens, CacheWrite: write, Input: tokens - write}
	if err := a.st.SaveContext(id, "/main", false, nil, []usage.ContextTurn{turn}, 1, usage.ContextState{LastTurn: -1}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionContextRoute(t *testing.T) {
	a := newTestAPI(t)
	liveAPISession(t, a, "claude:s", true, nil)
	liveAPITurn(t, a, "claude:s", 100000, 0)
	got := decode[store.ContextLive](t, a.do(t, "GET", "/api/sessions/claude:s/context", nil, 0))
	if got.Tokens != 100000 || got.WindowTokens != 200000 || got.Pct != 50 || got.TopOutputs == nil {
		t.Fatalf("%+v", got)
	}
	if rec := a.do(t, "GET", "/api/sessions/nada/context", nil, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("inexistente = %d", rec.Code)
	}
}

func TestContextLiveRoute(t *testing.T) {
	a := newTestAPI(t)
	empty := decode[[]LiveContext](t, a.do(t, "GET", "/api/context/live", nil, 0))
	if empty == nil || len(empty) != 0 {
		t.Fatalf("vazio = %#v", empty)
	}
	p, err := a.st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	card, err := a.st.CreateCard(domain.Card{Title: "Medidor", ProjectID: p.ID, Column: domain.EmAndamento})
	if err != nil {
		t.Fatal(err)
	}
	liveAPISession(t, a, "claude:a", true, &card.ID)
	liveAPITurn(t, a, "claude:a", 170000, 60000)
	liveAPISession(t, a, "claude:b", true, nil)
	liveAPITurn(t, a, "claude:b", 20000, 0)
	liveAPISession(t, a, "claude:nofull", false, nil)
	got := decode[[]LiveContext](t, a.do(t, "GET", "/api/context/live", nil, 0))
	by := map[string]LiveContext{}
	for _, l := range got {
		by[l.ID] = l
	}
	if len(got) != 2 {
		t.Fatalf("lista = %+v", got)
	}
	if a1 := by["claude:a"]; a1.CardKey != card.Key || a1.CardTitle != "Medidor" || !a1.Over80 || !a1.Rewrite || a1.Pct != 85 || a1.Tokens != 170000 {
		t.Errorf("a = %+v", a1)
	}
	if b := by["claude:b"]; b.CardKey != "" || b.Over80 || b.Rewrite || b.IdleOverTTL {
		t.Errorf("b = %+v", b)
	}
}
