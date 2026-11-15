package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

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
	if err := a.st.TouchSession("claude:s", testNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/me/test", map[string]string{"text": "go test"}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("test = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", "/api/me/done", nil, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != domain.Revisao {
		t.Fatalf("column = %s", got.Column)
	}
	evs, _ := a.st.RecentEvents(1)
	if len(evs) != 1 || evs[0].Kind != domain.EvDone || evs[0].SessionID != "claude:s" {
		t.Fatalf("evs = %+v", evs)
	}
	if x, _ := a.st.GetSession("claude:s"); !x.LastEventAt.Equal(testNow) {
		t.Fatalf("sessão não foi tocada: %+v", x)
	}
}

func TestMeTestRecordsEvent(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	if err := a.st.TouchSession("claude:s", testNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/me/test", map[string]string{"text": "go test ./..."}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	evs, _ := a.st.RecentEvents(1)
	if len(evs) != 1 || evs[0].Kind != domain.EvHowToTest || evs[0].CardID == nil || *evs[0].CardID != c.ID {
		t.Fatalf("evs = %+v", evs)
	}
	if x, _ := a.st.GetSession("claude:s"); !x.LastEventAt.Equal(testNow) {
		t.Fatalf("sessão não foi tocada: %+v", x)
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

func TestMeStartWithoutProjectAsksTheUser(t *testing.T) {
	a := newTestAPI(t)
	a.liveSession(t, "claude:s", 4242, "")
	rec := a.do(t, "POST", "/api/me/start", map[string]string{"title": "Novo"}, 4242)
	want := "Este repositório não está cadastrado no NobreAI Cockpit. Peça ao usuário para cadastrar (nobre project add); não cadastre por conta própria."
	if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] != want {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestMeAnswerOwnCardQuestion(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	q := decode[map[string]any](t, a.do(t, "POST", "/api/me/ask", map[string]string{"text": "qual cookie?"}, 4242))
	if q["id"] == nil || q["text"] != "qual cookie?" {
		t.Fatalf("ask = %v", q)
	}
	rec := a.do(t, "POST", "/api/me/answer", map[string]any{"id": q["id"], "answer": "cookie"}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("answer = %d %s", rec.Code, rec.Body)
	}
	if got := decode[map[string]any](t, rec); got["id"] != q["id"] || got["answer"] != "cookie" {
		t.Fatalf("resposta = %v", got)
	}
	ct, _ := a.st.CardContent(c.ID)
	if len(ct.Questions) != 1 || ct.Questions[0].Answer != "cookie" {
		t.Fatalf("content = %+v", ct)
	}
	evs, _ := a.st.RecentEvents(10)
	n := 0
	for _, e := range evs {
		if e.Kind == domain.EvAnswer {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("eventos answer = %d (%+v)", n, evs)
	}
}

func TestMeAnswerOtherCardQuestion404(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	other, err := a.st.CreateCard(domain.Card{Title: "Outro", ProjectID: c.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	q, err := a.st.AddQuestion(domain.Question{CardID: other.ID, Text: "?"})
	if err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/answer", map[string]any{"id": q.ID, "answer": "x"}, 4242)
	if rec.Code != http.StatusNotFound || decode[map[string]string](t, rec)["error"] != "pergunta não encontrada neste card" {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if ct, _ := a.st.CardContent(other.ID); ct.Questions[0].Answer != "" {
		t.Fatalf("resposta gravada: %+v", ct)
	}
}

func TestMeAnswerEmpty400(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	q, _ := a.st.AddQuestion(domain.Question{CardID: c.ID, Text: "?"})
	if rec := a.do(t, "POST", "/api/me/answer", map[string]any{"id": q.ID, "answer": "  "}, 4242); rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

// doingCard devolve um card em_andamento com sessão viva ligada.
func (a *testAPI) doingCard(t *testing.T) domain.Card {
	t.Helper()
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	if err := a.st.MoveCard(c.Key, domain.EmAndamento); err != nil {
		t.Fatal(err)
	}
	return c
}

func (a *testAPI) event(t *testing.T, c domain.Card, kind string, at time.Time) {
	t.Helper()
	if err := a.st.AddEvent(domain.Event{SessionID: "claude:s", CardID: &c.ID, Kind: kind, At: at}); err != nil {
		t.Fatal(err)
	}
}

func (a *testAPI) wantDoneRefused(t *testing.T, c domain.Card, body any, contains ...string) {
	t.Helper()
	rec := a.do(t, "POST", "/api/me/done", body, 4242)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	msg := decode[map[string]string](t, rec)["error"]
	for _, w := range append(contains, " (ou nobre done --force)") {
		if !strings.Contains(msg, w) {
			t.Fatalf("falta %q em %q", w, msg)
		}
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != domain.EmAndamento {
		t.Fatalf("column = %s", got.Column)
	}
}

func TestMeDoneRefusesWithoutHowToTest(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	a.wantDoneRefused(t, c, nil, `"Como testar" vazio: registre com nobre test`)
	a.wantDoneRefused(t, c, map[string]bool{"force": false}, "registre com nobre test")
}

func TestMeDoneRefusesStaleHowToTest(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	a.event(t, c, domain.EvHowToTest, testNow.Add(-2*time.Hour))
	a.event(t, c, domain.EvFileEdit, testNow.Add(-time.Hour))
	got, _ := a.st.GetCard(c.Key)
	got.HowToTest = "antigo"
	if err := a.st.UpdateCard(got); err != nil {
		t.Fatal(err)
	}
	a.wantDoneRefused(t, c, nil, `"Como testar" é anterior à última edição de arquivo: atualize com nobre test`)
}

func TestMeDoneRefusesOpenQuestion(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	if rec := a.do(t, "POST", "/api/me/test", map[string]string{"text": "go test"}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("test = %d %s", rec.Code, rec.Body)
	}
	q, _ := a.st.AddQuestion(domain.Question{CardID: c.ID, Text: "?"})
	a.wantDoneRefused(t, c, nil, "1 pergunta(s) sem resposta: responda com nobre answer <id>")
	if err := a.st.AnswerQuestion(q.ID, "sim"); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/me/done", nil, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestMeDoneCombinesProblems(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	a.st.AddQuestion(domain.Question{CardID: c.ID, Text: "?"})
	a.wantDoneRefused(t, c, nil, `registre com nobre test; 1 pergunta(s)`)
}

func TestMeDoneOKWhenCurrent(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	a.event(t, c, domain.EvFileEdit, testNow.Add(-2*time.Hour))
	if rec := a.do(t, "POST", "/api/me/test", map[string]string{"text": "go test"}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("test = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", "/api/me/done", nil, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != domain.Revisao {
		t.Fatalf("column = %s", got.Column)
	}
}

func TestMeDoneHowToTestFromUIWithoutEventIsCurrent(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	got, _ := a.st.GetCard(c.Key)
	got.HowToTest = "da UI"
	a.st.UpdateCard(got)
	a.event(t, c, domain.EvFileEdit, testNow.Add(-time.Hour))
	if rec := a.do(t, "POST", "/api/me/done", nil, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestMeDoneForce(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	a.st.AddQuestion(domain.Question{CardID: c.ID, Text: "?"})
	if rec := a.do(t, "POST", "/api/me/done", map[string]bool{"force": true}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != domain.Revisao {
		t.Fatalf("column = %s", got.Column)
	}
}
