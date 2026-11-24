package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/kiro"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
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
	a := guideAPI(t) // o aviso cita a skill do driver do Claude
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

// guideAPI é o testAPI com os drivers do Claude e do Kiro registrados.
func guideAPI(t *testing.T) *testAPI {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reg := agents.New(claude.New(st, adapter.Deps{}), kiro.New(st, adapter.Deps{}))
	return &testAPI{st: st, h: New(st, Deps{Now: func() time.Time { return testNow }, Agents: reg})}
}

// O aviso de repositório não cadastrado cita o guia do nobre do agente da
// sessão (agentcore.Guide); sem driver registrado, não cita guia.
func TestMeNoProjectCitesAgentGuide(t *testing.T) {
	a := guideAPI(t)
	st := a.st
	base := "Este repositório não está cadastrado no NobreAI Cockpit. Cadastre com nobre project add e faça o levantamento com nobre project scan"
	for _, c := range []struct {
		id, agent string
		pid       int
		want      string
	}{
		{"claude:s", "claude", 4242, base + " (veja a skill nobre-cockpit)."},
		{"kiro:s", "kiro", 4343, base + " (veja as instruções do Cockpit no steering do Kiro)."},
		{"codex:s", "codex", 4444, base + "."},
	} {
		if err := st.StartSession(domain.Session{ID: c.id, Agent: c.agent, PID: c.pid, Cwd: "/w/fora", StartedAt: testNow}); err != nil {
			t.Fatal(err)
		}
		rec := a.do(t, "POST", "/api/me/cards", map[string]string{"title": "B"}, c.pid)
		if got := decode[map[string]string](t, rec)["error"]; rec.Code != http.StatusConflict || got != c.want {
			t.Errorf("%s: code = %d, erro = %q", c.agent, rec.Code, got)
		}
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

func TestMeAppMovesCard(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	web, _, err := a.st.CreateChild("web", "/w/api/apps/web", c.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/app", map[string]any{"app": "web"}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.ProjectID != web.ID {
		t.Fatalf("project_id = %d, quer %d", got.ProjectID, web.ID)
	}
	// Dentro do app, --geral volta ao pai.
	rec = a.do(t, "POST", "/api/me/app", map[string]any{"geral": true}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("geral = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.ProjectID != c.ProjectID {
		t.Fatalf("project_id = %d, quer %d", got.ProjectID, c.ProjectID)
	}
	if rec := a.do(t, "POST", "/api/me/app", map[string]any{}, 4242); rec.Code != http.StatusBadRequest {
		t.Fatalf("vazio = %d", rec.Code)
	}
}

func TestMeAppUnknownListsApps(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.st.CreateChild("web", "/w/api/apps/web", c.ProjectID)
	a.st.CreateChild("admin", "/w/api/apps/admin", c.ProjectID)
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/app", map[string]any{"app": "mobile"}, 4242)
	want := "app desconhecido: mobile. Apps deste projeto: admin, web"
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != want {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestMeAppNoChildren(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/app", map[string]any{"app": "web"}, 4242)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "este projeto não tem sub-projetos" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestMeAppGeralRefusedWhenFamilyRootIsAGroup(t *testing.T) {
	a := newTestAPI(t)
	g, err := a.st.CreateGroup("loja")
	if err != nil {
		t.Fatal(err)
	}
	repo, _, err := a.st.CreateChild("api", "/w/loja/api", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Um card herdado no grupo (o store não impede).
	c, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/app", map[string]any{"geral": true}, 4242)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != msgGroupNoCards {
		t.Fatalf("geral no grupo = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/me/app", map[string]any{"app": "api"}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("app = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.ProjectID != repo.ID {
		t.Fatalf("project_id = %d", got.ProjectID)
	}
}

// doSessionHeader chama /api/me/note só com X-Nobre-Agent-Session (e PID opcional).
func (a *testAPI) doSessionHeader(t *testing.T, header string, pid int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/me/note", strings.NewReader(`{"text":"x"}`))
	req.Host = "127.0.0.1:7420"
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set("X-Nobre-Agent-Session", header)
	}
	if pid > 0 {
		req.Header.Set("X-Nobre-Agent-PID", strconv.Itoa(pid))
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return rec
}

func (a *testAPI) kiroSession(t *testing.T, id string, pid int, key string) {
	t.Helper()
	if err := a.st.StartSession(domain.Session{ID: id, Agent: "kiro", PID: pid, Cwd: "/w/api/sub", StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.AttachSession(id, key, false); err != nil {
		t.Fatal(err)
	}
}

func TestMeAchaSessaoPeloCabecalhoDoAgente(t *testing.T) {
	a := newTestAPI(t)
	key := a.card(t).Key
	a.kiroSession(t, "kiro:sess_x", 111, key)
	// PID desconhecido, cabeçalho de sessão viva.
	if rec := a.doSessionHeader(t, "kiro:sess_x", 9999); rec.Code != http.StatusOK {
		t.Fatalf("PID desconhecido + cabeçalho: %d %s", rec.Code, rec.Body)
	}
	// Sem PID algum.
	if rec := a.doSessionHeader(t, "kiro:sess_x", 0); rec.Code != http.StatusOK {
		t.Fatalf("só cabeçalho: %d %s", rec.Code, rec.Body)
	}
}

func TestMeCabecalhoDeSessaoEncerradaOuDesconhecida404(t *testing.T) {
	a := newTestAPI(t)
	key := a.card(t).Key
	a.kiroSession(t, "kiro:sess_x", 111, key)
	if err := a.st.EndSession("kiro:sess_x", "exit", testNow); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"kiro:sess_x", "kiro:sess_nao_existe"} {
		rec := a.doSessionHeader(t, h, 0)
		if rec.Code != http.StatusNotFound || decode[map[string]string](t, rec)["error"] != msgNoSession {
			t.Fatalf("%s: %d %s", h, rec.Code, rec.Body)
		}
	}
}

func TestMeSemPIDNemCabecalho400(t *testing.T) {
	a := newTestAPI(t)
	if rec := a.doSessionHeader(t, "", 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	// Cabeçalho malformado (agente desconhecido) conta como ausente.
	if rec := a.doSessionHeader(t, "zzz:sess_x", 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("agente desconhecido: %d %s", rec.Code, rec.Body)
	}
}

func TestMePIDVivoTemPrioridadeSobreCabecalho(t *testing.T) {
	a := newTestAPI(t)
	key := a.card(t).Key
	a.liveSession(t, "claude:s1", 4242, "")
	a.kiroSession(t, "kiro:sess_x", 111, key)
	// PID acha claude:s1 (sem card -> 409); o cabeçalho (com card) é ignorado.
	rec := a.doSessionHeader(t, "kiro:sess_x", 4242)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestMePlanAndTask(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	if rec := a.do(t, "POST", "/api/me/plan", map[string]any{"path": "docs/p.md", "tasks": []string{"A", "B"}}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("plan = %d %s", rec.Code, rec.Body)
	}
	rec := a.do(t, "POST", "/api/me/task", map[string]any{"n": 1, "status": "andamento"}, 4242)
	if rec.Code != http.StatusOK || decode[map[string]any](t, rec)["status"] != "em_andamento" {
		t.Fatalf("task = %d %s", rec.Code, rec.Body)
	}
	cards := decode[[]map[string]any](t, a.do(t, "GET", "/api/cards", nil, 0))
	plan, _ := cards[0]["plan"].(map[string]any)
	tasks, _ := plan["tasks"].([]any)
	if len(tasks) != 2 || tasks[0].(map[string]any)["status"] != "em_andamento" {
		t.Fatalf("plan = %v", cards[0]["plan"])
	}
	evs, _ := a.st.RecentEvents(10)
	kinds := map[string]bool{}
	for _, e := range evs {
		if e.CardID != nil && *e.CardID == c.ID {
			kinds[e.Kind] = true
		}
	}
	if !kinds[domain.EvPlan] || !kinds[domain.EvTask] {
		t.Fatalf("eventos = %v", kinds)
	}

	rec = a.do(t, "POST", "/api/me/task", map[string]any{"n": 9, "status": "concluida"}, 4242)
	if rec.Code != http.StatusNotFound || decode[map[string]string](t, rec)["error"] != "tarefa 9 não existe no plano do card" {
		t.Fatalf("n=9 = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/me/task", map[string]any{"n": 1, "status": "feito"}, 4242)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "estado inválido: use a-fazer, andamento, revisao ou concluida" {
		t.Fatalf("feito = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/me/plan", map[string]any{"path": "docs/p.md", "tasks": []string{}}, 4242)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "informe ao menos uma tarefa" {
		t.Fatalf("vazio = %d %s", rec.Code, rec.Body)
	}

	a.liveSession(t, "claude:n", 4343, "")
	for _, p := range []string{"/api/me/plan", "/api/me/task"} {
		if rec := a.do(t, "POST", p, map[string]any{"tasks": []string{"A"}, "n": 1, "status": "concluida"}, 4343); rec.Code != http.StatusConflict {
			t.Fatalf("%s sem card = %d %s", p, rec.Code, rec.Body)
		}
	}
}
