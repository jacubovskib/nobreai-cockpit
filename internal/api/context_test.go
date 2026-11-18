package api

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

type ctxTop struct {
	Keys     []store.ContextKey     `json:"keys"`
	Sessions []store.ContextSession `json:"sessions"`
	Projects []store.ContextProject `json:"projects"`
	Context  int64                  `json:"context_total"`
	Weight   int64                  `json:"weight_total"`
}

// seedContext cria uma sessão no projeto com uma saída lida duas vezes.
func (a *testAPI) seedContext(t *testing.T, sid string, project int64, key string, tokens int64) {
	t.Helper()
	pid := project
	err := a.st.StartSession(domain.Session{ID: sid, Agent: "claude", PID: 1, Cwd: "/w", ProjectID: &pid,
		TranscriptPath: "/main", StartedAt: testNow, LastEventAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	outs := []usage.ToolOutput{{ItemID: "o-" + sid, Tool: "Read", Key: key, Bytes: tokens * 4, Tokens: tokens,
		Turn: 0, At: testNow, Result: true}}
	turns := []usage.ContextTurn{
		{Turn: 0, MessageID: "m0", At: testNow, Tokens: 100},
		{Turn: 1, MessageID: "m1", At: testNow.Add(time.Minute), Tokens: 200},
	}
	if err := a.st.SaveContext(sid, "/main", false, outs, turns, 1, usage.ContextState{LastTurn: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestContextTop(t *testing.T) {
	a := newTestAPI(t)
	pai, _ := a.st.CreateProject("loja", "/r/loja")
	filho, _, _ := a.st.CreateChild("web", "/r/loja/web", pai.ID)
	outro, _ := a.st.CreateProject("outro", "/o")
	a.seedContext(t, "s-pai", pai.ID, "a.go", 10)
	a.seedContext(t, "s-filho", filho.ID, "b.go", 50)
	a.seedContext(t, "s-outro", outro.ID, "c.go", 5)

	rec := a.do(t, "GET", "/api/context/top", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("todos = %d %s", rec.Code, rec.Body)
	}
	got := decode[ctxTop](t, rec)
	if len(got.Keys) != 3 || got.Keys[0].Key != "b.go" || len(got.Sessions) != 3 || len(got.Projects) != 3 {
		t.Fatalf("todos = %+v", got)
	}
	if got.Weight != 10+50+5 || got.Context != 3*300 {
		t.Fatalf("totais = %d/%d", got.Weight, got.Context)
	}

	// Pai expande a família (pai + filho), sem o outro projeto.
	got = decode[ctxTop](t, a.do(t, "GET", "/api/context/top?project="+itoa(pai.ID), nil, 0))
	if len(got.Keys) != 2 {
		t.Fatalf("família = %+v", got.Keys)
	}
	// exact=1 só o pai.
	got = decode[ctxTop](t, a.do(t, "GET", "/api/context/top?exact=1&project="+itoa(pai.ID), nil, 0))
	if len(got.Keys) != 1 || got.Keys[0].Key != "a.go" {
		t.Fatalf("exact = %+v", got.Keys)
	}

	for _, q := range []string{"from=xx", "to=xx", "project=abc"} {
		if rec := a.do(t, "GET", "/api/context/top?"+q, nil, 0); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d", q, rec.Code)
		}
	}
	rec = a.do(t, "GET", "/api/context/top?project=abc", nil, 0)
	if decode[map[string]string](t, rec)["error"] != "projeto inválido" {
		t.Fatalf("msg = %s", rec.Body)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestContextSession(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	a.seedContext(t, "s1", p.ID, "a.go", 10)
	rec := a.do(t, "GET", "/api/context/sessions/s1", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessão = %d %s", rec.Code, rec.Body)
	}
	got := decode[struct {
		Turns []store.ContextTurnPoint `json:"turns"`
		Top   []store.ContextOutput    `json:"top"`
	}](t, rec)
	if len(got.Turns) != 2 || len(got.Top) != 1 || got.Top[0].Key != "a.go" || got.Top[0].Rereads != 1 {
		t.Fatalf("detalhe = %+v", got)
	}
	if rec := a.do(t, "GET", "/api/context/sessions/nada", nil, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("inexistente = %d", rec.Code)
	}
}

func TestContextTop_Empty(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "GET", "/api/context/top", nil, 0)
	got := decode[map[string]any](t, rec)
	for _, k := range []string{"keys", "sessions", "projects"} {
		if l, ok := got[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s = %v, quer []", k, got[k])
		}
	}
	if rec.Code != http.StatusOK || got["context_total"] != 0.0 || got["weight_total"] != 0.0 {
		t.Fatalf("vazio = %d %s", rec.Code, rec.Body)
	}
}
