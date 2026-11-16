package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
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
	want := "Este repositório não está cadastrado no NobreAI Cockpit. Cadastre com nobre project add e faça o levantamento com nobre project scan (veja a skill nobre-cockpit)."
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

func TestMeBranch(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	if err := a.st.SetProjectBase(c.ProjectID, "main"); err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/branch", map[string]string{"branch": "feat/x"}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Branch != "feat/x" {
		t.Fatalf("branch = %q", got.Branch)
	}
	rec = a.do(t, "POST", "/api/me/branch", map[string]string{"branch": "main"}, 4242)
	want := `"main" é a base do projeto; o card fica com o branch de trabalho`
	if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] != want {
		t.Fatalf("base = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Branch != "feat/x" {
		t.Fatalf("branch = %q", got.Branch)
	}
	if rec := a.do(t, "POST", "/api/me/branch", map[string]string{"branch": " "}, 4242); rec.Code != http.StatusBadRequest {
		t.Fatalf("vazio = %d", rec.Code)
	}
}

func TestPatchProjectBaseBranch(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	rec := a.do(t, "PATCH", "/api/projects/"+strconv.FormatInt(p.ID, 10), map[string]string{"base_branch": "develop"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	ps, _ := a.st.ListProjects()
	if ps[0].BaseBranch != "develop" || ps[0].Name != p.Name {
		t.Fatalf("projeto = %+v", ps[0])
	}
}

func TestMeStartBranchFollowsWorkingBranchNotBase(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	if err := a.st.SetProjectBase(p.ID, "main"); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct{ branch, want string }{{"main", ""}, {"feat/x", "feat/x"}} {
		id, pid := "claude:s"+strconv.Itoa(i), 4300+i
		if err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", PID: pid, Cwd: "/w/api", Branch: c.branch, StartedAt: testNow}); err != nil {
			t.Fatal(err)
		}
		rec := a.do(t, "POST", "/api/me/start", map[string]string{"title": "T" + c.branch}, pid)
		if rec.Code != http.StatusCreated {
			t.Fatalf("start = %d %s", rec.Code, rec.Body)
		}
		if got := decode[domain.Card](t, rec); got.Branch != c.want {
			t.Fatalf("sessão em %q: branch do card = %q, quer %q", c.branch, got.Branch, c.want)
		}
	}
}

func TestMeCardsCreatesWithoutAttach(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	c, _ = a.st.GetCard(c.Key) // em_andamento depois de ligar a sessão
	rec := a.do(t, "POST", "/api/me/cards", map[string]string{"title": "B", "column": "pausado", "where_stopped": "x", "branch": "feat/b"}, 4242)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	b, err := a.st.GetCard(decode[domain.Card](t, rec).Key)
	if err != nil || b.Column != domain.Pausado || b.WhereStopped != "x" || b.WhereStoppedAt == nil || b.Branch != "feat/b" || b.ProjectID != c.ProjectID {
		t.Fatalf("card = %+v, %v", b, err)
	}
	sess, _ := a.st.GetSession("claude:s")
	if sess.CardID == nil || *sess.CardID != c.ID {
		t.Fatalf("sessão saiu do card A: %+v", sess.CardID)
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != c.Column {
		t.Fatalf("card A mudou de coluna: %s", got.Column)
	}
}

func TestMeCardsDefaultsToBacklog(t *testing.T) {
	a := newTestAPI(t)
	a.project(t)
	a.liveSession(t, "claude:s", 4242, "")
	rec := a.do(t, "POST", "/api/me/cards", map[string]string{"title": "B"}, 4242)
	if rec.Code != http.StatusCreated || decode[domain.Card](t, rec).Column != domain.Backlog {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestMeCardsBadColumn400(t *testing.T) {
	a := newTestAPI(t)
	a.project(t)
	a.liveSession(t, "claude:s", 4242, "")
	rec := a.do(t, "POST", "/api/me/cards", map[string]string{"title": "B", "column": "revisao"}, 4242)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestMeCardsNoProject409(t *testing.T) {
	a := newTestAPI(t)
	a.liveSession(t, "claude:s", 4242, "")
	rec := a.do(t, "POST", "/api/me/cards", map[string]string{"title": "B"}, 4242)
	if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] != msgNoProject {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

// gitAPI é o testAPI com um Git falso cujo branch atual o teste controla.
func gitAPI(t *testing.T, g *fakeGit) *testAPI {
	t.Helper()
	a := newTestAPI(t)
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, Git: g})
	return a
}

// staleSession cria a sessão viva gravada em feat/old no projeto /w/api (base main).
func (a *testAPI) staleSession(t *testing.T, pid int) domain.Project {
	t.Helper()
	p := a.project(t)
	if err := a.st.SetProjectBase(p.ID, "main"); err != nil {
		t.Fatal(err)
	}
	if err := a.st.StartSession(domain.Session{ID: "claude:s", Agent: "claude", PID: pid, Cwd: "/w/api",
		Branch: "feat/old", StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	return p
}

// O agente trocou de branch no mesmo diretório (git switch -c): nobre start
// usa o branch atual, não o gravado na sessão.
func TestMeStartUsesLiveBranch(t *testing.T) {
	a := gitAPI(t, &fakeGit{branch: "feat/new"})
	a.staleSession(t, 4242)
	rec := a.do(t, "POST", "/api/me/start", map[string]string{"title": "T"}, 4242)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	if got := decode[domain.Card](t, rec); got.Branch != "feat/new" {
		t.Fatalf("branch do card = %q", got.Branch)
	}
	if s, _ := a.st.GetSession("claude:s"); s.Branch != "feat/new" || s.Cwd != "/w/api" {
		t.Fatalf("sessão = %+v", s)
	}
}

func TestMeResumeUsesLiveBranch(t *testing.T) {
	a := gitAPI(t, &fakeGit{branch: "feat/new"})
	p := a.staleSession(t, 4242)
	c, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: p.ID, Column: domain.Pausado})
	if err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/me/resume", map[string]string{"key": c.Key}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("resume = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Branch != "feat/new" {
		t.Fatalf("branch do card = %q", got.Branch)
	}
}

// nobre branch grava o branch informado no card, mas a sessão passa a ter o
// branch atual: o PostToolUse do próprio comando não desfaz a escolha.
func TestMeBranchRefreshesSessionBranch(t *testing.T) {
	a := gitAPI(t, &fakeGit{branch: "feat/live"})
	p := a.staleSession(t, 4242)
	c, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.AttachSession("claude:s", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/me/branch", map[string]string{"branch": "feat/x"}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("branch = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Branch != "feat/x" {
		t.Fatalf("branch do card = %q", got.Branch)
	}
	if s, _ := a.st.GetSession("claude:s"); s.Branch != "feat/live" {
		t.Fatalf("branch da sessão = %q", s.Branch)
	}
}

// Fora de um repositório git, o agente não é mandado cadastrar: a sessão segue sem card.
func TestMeStartNonGitCwdDoesNotSuggestRegister(t *testing.T) {
	a := newTestAPI(t)
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, Git: gitinfo.Exec{}})
	dir := t.TempDir()
	if err := a.st.StartSession(domain.Session{ID: "claude:s", Agent: "claude", PID: 4242, Cwd: dir, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/me/start", "/api/me/cards"} {
		rec := a.do(t, "POST", path, map[string]string{"title": "T"}, 4242)
		msg := decode[map[string]string](t, rec)["error"]
		if rec.Code != http.StatusConflict || msg != msgNotGit || strings.Contains(msg, "nobre project add") {
			t.Fatalf("%s = %d %q", path, rec.Code, msg)
		}
	}
}

// O card nunca guarda a base: nobre card add --branch <base> cria sem branch.
func TestMeCardsDropsBaseBranch(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	if err := a.st.SetProjectBase(p.ID, "main"); err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, "")
	rec := a.do(t, "POST", "/api/me/cards", map[string]string{"title": "B", "branch": "main"}, 4242)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got := decode[domain.Card](t, rec); got.Branch != "" {
		t.Fatalf("branch = %q", got.Branch)
	}
}

func TestMeBlock(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	other, err := a.st.CreateCard(domain.Card{Title: "Outro", ProjectID: c.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/block", map[string]string{"key": other.Key, "why": "precisa do merge"}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("block = %d %s", rec.Code, rec.Body)
	}
	if got := decode[domain.Card](t, rec); got.BlockedBy != other.Key {
		t.Fatalf("card = %+v", got)
	}
	ct, _ := a.st.CardContent(c.ID)
	if len(ct.Decisions) != 1 || ct.Decisions[0].What != "Bloqueado por "+other.Key || ct.Decisions[0].Why != "precisa do merge" {
		t.Fatalf("decisões = %+v", ct.Decisions)
	}
	rec = a.do(t, "POST", "/api/me/block", map[string]string{"key": c.Key}, 4242)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "um card não pode bloquear a si mesmo" {
		t.Fatalf("própria = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/me/block", map[string]string{"key": "NAC-999"}, 4242)
	if rec.Code != http.StatusNotFound || decode[map[string]string](t, rec)["error"] != "card NAC-999 não encontrado" {
		t.Fatalf("inexistente = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/me/block", map[string]string{"key": ""}, 4242)
	if rec.Code != http.StatusOK || decode[domain.Card](t, rec).BlockedBy != "" {
		t.Fatalf("limpar = %d %s", rec.Code, rec.Body)
	}
}

// I1: o limite de custo pausa o card com a sessão ainda viva; o nobre done
// dessa sessão o leva para Revisão e limpa o motivo da pausa.
func TestMeDoneFromLimitPauseMovesToRevisao(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	if ok, err := a.st.PauseForLimit(c.ID, testNow); err != nil || !ok {
		t.Fatalf("PauseForLimit = %v %v", ok, err)
	}
	if err := a.st.SetHandoffIncomplete(c.ID, true, testNow); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/me/test", map[string]string{"text": "go test"}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("test = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", "/api/me/done", nil, 4242); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	got, _ := a.st.GetCard(c.Key)
	if got.Column != domain.Revisao || got.PauseReason != "" || got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
	evs, _ := a.st.RecentEvents(1)
	if len(evs) != 1 || evs[0].Kind != domain.EvDone {
		t.Fatalf("evs = %+v", evs)
	}
}

// I1: pausado por limite ainda passa pelas checagens do done.
func TestMeDoneFromLimitPauseKeepsChecks(t *testing.T) {
	a := newTestAPI(t)
	c := a.doingCard(t)
	if _, err := a.st.PauseForLimit(c.ID, testNow); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/me/done", nil, 4242)
	if rec.Code != http.StatusConflict || !strings.Contains(decode[map[string]string](t, rec)["error"], "registre com nobre test") {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.Column != domain.Pausado || got.PauseReason != domain.PauseReasonLimit {
		t.Fatalf("card = %+v", got)
	}
}

// I1: fora de Em andamento (e sem ser pausa por limite) o done não finge sucesso.
func TestMeDoneRefusesWhenNotInProgress(t *testing.T) {
	for _, tc := range []struct {
		col  domain.Column
		want string
	}{
		{domain.Revisao, "o card está em Revisão; nobre done só move cards em andamento"},
		{domain.Pausado, "o card está em Pausado; nobre done só move cards em andamento"},
		{domain.Concluido, "o card está em Concluído; nobre done só move cards em andamento"},
	} {
		t.Run(string(tc.col), func(t *testing.T) {
			a := newTestAPI(t)
			c := a.doingCard(t)
			if err := a.st.MoveCard(c.Key, tc.col); err != nil {
				t.Fatal(err)
			}
			rec := a.do(t, "POST", "/api/me/done", map[string]bool{"force": true}, 4242)
			if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] != tc.want {
				t.Fatalf("code = %d %s", rec.Code, rec.Body)
			}
			if got, _ := a.st.GetCard(c.Key); got.Column != tc.col {
				t.Fatalf("column = %s", got.Column)
			}
			if evs, _ := a.st.RecentEvents(5); len(evs) != 0 {
				for _, e := range evs {
					if e.Kind == domain.EvDone {
						t.Fatalf("done registrado: %+v", evs)
					}
				}
			}
		})
	}
}

// nobre block nac-2 grava a chave normalizada (NAC-2).
func TestMeBlockNormalizesKey(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	if _, err := a.st.CreateCard(domain.Card{Title: "Outro", ProjectID: c.ProjectID}); err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/block", map[string]string{"key": " nac-2 "}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("block = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.BlockedBy != "NAC-2" {
		t.Fatalf("blocked_by = %q", got.BlockedBy)
	}
	ct, _ := a.st.CardContent(c.ID)
	if len(ct.Decisions) != 1 || ct.Decisions[0].What != "Bloqueado por NAC-2" {
		t.Fatalf("decisões = %+v", ct.Decisions)
	}
	rec = a.do(t, "POST", "/api/me/block", map[string]string{"key": "nac-1"}, 4242)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("própria em minúsculas = %d %s", rec.Code, rec.Body)
	}
}
