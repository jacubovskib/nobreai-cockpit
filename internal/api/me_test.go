package api

import (
	"net/http"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// liveSession cria uma sessão viva com o PID e, se key != "", liga ao card.
func (a *testAPI) liveSession(t *testing.T, id string, pid int, key string) {
	t.Helper()
	if err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", PID: pid, Cwd: "/w/api/sub", StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if key != "" {
		if err := a.st.AttachSession(id, key, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMeRequiresKnownPID(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "POST", "/api/me/note", map[string]string{"text": "x"}, 9999)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got := decode[map[string]string](t, rec)["error"]; got != "nenhuma sessão de agente encontrada para este processo" {
		t.Fatalf("error = %q", got)
	}
}

func TestMeRequiresCard(t *testing.T) {
	a := newTestAPI(t)
	a.liveSession(t, "claude:s", 4242, "")
	rec := a.do(t, "POST", "/api/me/note", map[string]string{"text": "x"}, 4242)
	want := `Esta sessão ainda não tem card. Use nobre start "<título>" ou nobre resume <KEY>.`
	if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] != want {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestMeNoteUpdatesWhereStopped(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/note", map[string]string{"text": "x"}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	got, _ := a.st.GetCard(c.Key)
	if got.WhereStopped != "x" || got.WhereStoppedAt == nil || !got.WhereStoppedAt.Equal(testNow) {
		t.Fatalf("card = %+v", got)
	}
	evs, _ := a.st.RecentEvents(1)
	if len(evs) != 1 || evs[0].Kind != domain.EvNote || evs[0].SessionID != "claude:s" || evs[0].CardID == nil || *evs[0].CardID != c.ID {
		t.Fatalf("evs = %+v", evs)
	}
}

func TestMeDoneMovesToRevisao(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	if rec := a.do(t, "POST", "/api/me/done", nil, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != domain.Revisao {
		t.Fatalf("column = %s", got.Column)
	}
}

func TestMeStartAndContent(t *testing.T) {
	a := newTestAPI(t)
	a.project(t) // /w/api; a sessão roda em /w/api/sub
	a.liveSession(t, "claude:s", 4242, "")
	rec := a.do(t, "POST", "/api/me/start", map[string]string{"title": "Novo", "goal": "g", "done_when": "d"}, 4242)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	if k := decode[map[string]any](t, rec)["key"]; k != "NAC-1" {
		t.Fatalf("key = %v", k)
	}
	if got, _ := a.st.GetCard("NAC-1"); got.Column != domain.EmAndamento || got.Goal != "g" {
		t.Fatalf("card = %+v", got)
	}

	todo := decode[map[string]any](t, a.do(t, "POST", "/api/me/todo", map[string]string{"text": "falta"}, 4242))
	for _, r := range []struct {
		path string
		body any
	}{
		{"/api/me/decide", map[string]string{"what": "PKCE", "why": "seguro"}},
		{"/api/me/file", map[string]string{"path": "a.go", "why": "núcleo", "lines": "L1-9"}},
		{"/api/me/todo-done", map[string]any{"id": todo["id"]}},
		{"/api/me/ask", map[string]string{"text": "e o refresh?"}},
		{"/api/me/test", map[string]string{"text": "go test ./..."}},
	} {
		if rec := a.do(t, "POST", r.path, r.body, 4242); rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
			t.Fatalf("%s = %d %s", r.path, rec.Code, rec.Body)
		}
	}
	if rec := a.do(t, "POST", "/api/me/todo-done", map[string]any{"id": 999}, 4242); rec.Code != http.StatusNotFound {
		t.Fatalf("todo alheio = %d", rec.Code)
	}
	c, _ := a.st.GetCard("NAC-1")
	ct, _ := a.st.CardContent(c.ID)
	if c.HowToTest != "go test ./..." || len(ct.Decisions) != 1 || ct.Decisions[0].SessionID != "claude:s" ||
		len(ct.Files) != 1 || len(ct.Todos) != 1 || !ct.Todos[0].Done || len(ct.Questions) != 1 {
		t.Fatalf("card = %+v content = %+v", c, ct)
	}

	st := decode[map[string]any](t, a.do(t, "GET", "/api/me/status", nil, 4242))
	if card, ok := st["card"].(map[string]any); !ok || card["key"] != "NAC-1" {
		t.Fatalf("status = %v", st)
	}

	md := decode[map[string]string](t, a.do(t, "POST", "/api/me/resume", map[string]any{"key": "NAC-1"}, 4242))["markdown"]
	if len(md) == 0 || md[:len("# NAC-1")] != "# NAC-1" {
		t.Fatalf("markdown = %q", md)
	}
}
