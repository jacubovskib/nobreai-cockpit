package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

type testAPI struct {
	st  *store.Store
	h   http.Handler
	hub *Hub
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	h := New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub})
	return &testAPI{st: st, h: h, hub: hub}
}

// do executa a requisição; pid > 0 envia X-Nobre-Agent-PID.
func (a *testAPI) do(t *testing.T, method, path string, body any, pid int) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Host = "127.0.0.1:7420"
	req.RemoteAddr = "127.0.0.1:50000"
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json")
	}
	if pid > 0 {
		req.Header.Set("X-Nobre-Agent-PID", strconv.Itoa(pid))
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("json: %v (%s)", err, rec.Body.String())
	}
	return v
}

func (a *testAPI) project(t *testing.T) domain.Project {
	t.Helper()
	p, err := a.st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (a *testAPI) card(t *testing.T) domain.Card {
	t.Helper()
	p := a.project(t)
	c, err := a.st.CreateCard(domain.Card{Title: "OAuth2", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateAndGetCard(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	rec := a.do(t, "POST", "/api/cards", map[string]any{"title": "Migrar OAuth2", "goal": "g", "project_id": p.ID}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	created := decode[map[string]any](t, rec)
	if created["key"] != "NAC-1" || created["title"] != "Migrar OAuth2" || created["column"] != "backlog" {
		t.Fatalf("created = %v", created)
	}

	rec = a.do(t, "GET", "/api/cards/NAC-1", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	var d struct {
		Key       string `json:"key"`
		Readiness struct {
			Total int `json:"total"`
			Met   int `json:"met"`
		} `json:"readiness"`
		Sessions []any          `json:"sessions"`
		Content  map[string]any `json:"content"`
		Live     *any           `json:"live"`
		Touched  []any          `json:"touched_files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Key != "NAC-1" || d.Readiness.Total != 5 || d.Sessions == nil || d.Live != nil || d.Touched == nil {
		t.Fatalf("detail = %s", rec.Body)
	}
	if todos, ok := d.Content["todos"].([]any); !ok || len(todos) != 0 {
		t.Fatalf("content.todos deveria ser [] e não null: %s", rec.Body)
	}

	if rec := a.do(t, "POST", "/api/cards", map[string]any{"title": "x"}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("sem projeto = %d", rec.Code)
	}
	if rec := a.do(t, "GET", "/api/cards/NAC-9", nil, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("inexistente = %d", rec.Code)
	}
	list := decode[[]map[string]any](t, a.do(t, "GET", "/api/cards", nil, 0))
	if len(list) != 1 || list[0]["key"] != "NAC-1" {
		t.Fatalf("list = %v", list)
	}
}

func TestPatchCard(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "voando"}, 0)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "coluna inválida" {
		t.Fatalf("coluna inválida = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "revisao", "where_stopped": "aqui", "how_to_test": "go test"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	got, _ := a.st.GetCard(c.Key)
	if got.Column != domain.Revisao || got.WhereStopped != "aqui" || got.HowToTest != "go test" || got.Title != "OAuth2" {
		t.Fatalf("card = %+v", got)
	}
	if got.WhereStoppedAt == nil || !got.WhereStoppedAt.Equal(testNow) {
		t.Fatalf("where_stopped_at = %v", got.WhereStoppedAt)
	}
}

func TestAttachRoute(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	for _, id := range []string{"claude:live", "claude:other"} {
		if err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	rec := a.do(t, "POST", "/api/cards/"+c.Key+"/attach", map[string]any{"session_id": "claude:live"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/cards/"+c.Key+"/attach", map[string]any{"session_id": "claude:other"}, 0)
	if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] == "" {
		t.Fatalf("busy = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/cards/"+c.Key+"/attach", map[string]any{"session_id": "claude:other", "force": true}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("force = %d %s", rec.Code, rec.Body)
	}
}

func TestPromptRenderUnknown(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "POST", "/api/prompts", map[string]any{"name": "X", "slug": "x", "body": "oi {{x}}"}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST prompt = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/prompts/x/render", map[string]any{}, 0)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("render = %d %s", rec.Code, rec.Body)
	}
	var got struct {
		Unknown []string `json:"unknown"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Unknown) != 1 || got.Unknown[0] != "x" {
		t.Fatalf("body = %s", rec.Body)
	}

	c := a.card(t)
	rec = a.do(t, "POST", "/api/prompts/retomar-card/render", map[string]any{"card": c.Key}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("render card = %d %s", rec.Code, rec.Body)
	}
	if text := decode[map[string]string](t, rec)["text"]; !bytes.Contains([]byte(text), []byte("NAC-1 (OAuth2)")) {
		t.Fatalf("text = %q", text)
	}
}

func TestCardDetailTouchedFiles(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	if err := a.st.StartSession(domain.Session{ID: "claude:s", Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.AttachSession("claude:s", c.Key, false); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct{ kind, path string }{{domain.EvFileRead, "a.go"}, {domain.EvFileEdit, "b.go"}} {
		data, _ := json.Marshal(map[string]string{"path": e.path})
		if err := a.st.AddEvent(domain.Event{SessionID: "claude:s", Kind: e.kind, Data: data, At: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	var d struct {
		TouchedFiles []struct {
			Path  string `json:"path"`
			Kind  string `json:"kind"`
			Count int    `json:"count"`
		} `json:"touched_files"`
	}
	rec := a.do(t, "GET", "/api/cards/"+c.Key, nil, 0)
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.TouchedFiles) != 2 || d.TouchedFiles[0].Path != "b.go" || d.TouchedFiles[0].Kind != "editado" ||
		d.TouchedFiles[1].Path != "a.go" || d.TouchedFiles[1].Kind != "lido" || d.TouchedFiles[1].Count != 1 {
		t.Fatalf("touched_files = %s", rec.Body)
	}
}

// setCard grava coluna e updated_at direto no banco do teste.
func (a *testAPI) setCard(t *testing.T, key string, col domain.Column, updated time.Time) {
	t.Helper()
	if _, err := a.st.DB.Exec(`UPDATE cards SET col=?, updated_at=? WHERE key=?`, string(col), updated.UTC().Format(time.RFC3339Nano), key); err != nil {
		t.Fatal(err)
	}
}

func TestListCardsSignal(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	keys := map[string]string{}
	for _, name := range []string{"pausado", "revisao", "vivo"} {
		c, err := a.st.CreateCard(domain.Card{Title: name, ProjectID: p.ID})
		if err != nil {
			t.Fatal(err)
		}
		keys[name] = c.Key
	}
	old := testNow.Add(-72 * time.Hour)
	a.setCard(t, keys["pausado"], domain.Pausado, old)
	a.setCard(t, keys["revisao"], domain.Revisao, old)
	a.setCard(t, keys["vivo"], domain.EmAndamento, old)

	endedPaused, endedReview := testNow.Add(-48*time.Hour), testNow.Add(-30*time.Hour)
	for id, k := range map[string]string{"claude:p": "pausado", "claude:r": "revisao", "claude:v": "vivo"} {
		a.liveSession(t, id, 100+len(id)+int(id[len(id)-1]), keys[k])
	}
	if err := a.st.EndSession("claude:p", "x", endedPaused); err != nil {
		t.Fatal(err)
	}
	if err := a.st.EndSession("claude:r", "x", endedReview); err != nil {
		t.Fatal(err)
	}
	for id, at := range map[string]time.Time{"claude:p": endedPaused, "claude:r": endedReview} {
		if _, err := a.st.DB.Exec(`UPDATE sessions SET last_event_at=? WHERE id=?`, at.Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
	}
	// o EndSession pode mexer no card; fixa de novo o estado do teste
	a.setCard(t, keys["pausado"], domain.Pausado, old)
	a.setCard(t, keys["revisao"], domain.Revisao, old)
	if _, err := a.st.DB.Exec(`UPDATE sessions SET last_event_at=? WHERE id='claude:v'`, testNow.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	rec := a.do(t, "GET", "/api/cards", nil, 0)
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body)
	}
	got := map[string]CardSummary{}
	for _, c := range decode[[]CardSummary](t, rec) {
		got[c.Key] = c
	}
	if c := got[keys["pausado"]]; c.Signal != domain.SignalParado || c.SignalSince == nil || !c.SignalSince.Equal(endedPaused) {
		t.Fatalf("pausado = %q %v", c.Signal, c.SignalSince)
	}
	if c := got[keys["revisao"]]; c.Signal != domain.SignalAguardandoRevisao || c.SignalSince == nil || !c.SignalSince.Equal(endedReview) {
		t.Fatalf("revisao = %q %v", c.Signal, c.SignalSince)
	}
	if c := got[keys["vivo"]]; c.Signal != "" || c.SignalSince != nil {
		t.Fatalf("vivo = %q %v", c.Signal, c.SignalSince)
	}
}

func TestListCardsSignalWithoutSessions(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	if err := a.st.MoveCard(c.Key, domain.Pausado); err != nil {
		t.Fatal(err)
	}
	old := testNow.Add(-48 * time.Hour)
	a.setCard(t, c.Key, domain.Pausado, old)
	rec := a.do(t, "GET", "/api/cards", nil, 0)
	list := decode[[]CardSummary](t, rec)
	if len(list) != 1 || list[0].Signal != domain.SignalParado || list[0].SignalSince == nil || !list[0].SignalSince.Equal(old) {
		t.Fatalf("list = %+v", list)
	}
}

func TestListCardsSignalLiveIdleHasNoSignal(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.setCard(t, c.Key, domain.EmAndamento, testNow.Add(-72*time.Hour))
	a.liveSession(t, "claude:idle", 4242, c.Key)
	if _, err := a.st.DB.Exec(`UPDATE sessions SET last_event_at=? WHERE id='claude:idle'`, testNow.Add(-48*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	a.setCard(t, c.Key, domain.EmAndamento, testNow.Add(-72*time.Hour))
	list := decode[[]CardSummary](t, a.do(t, "GET", "/api/cards", nil, 0))
	if len(list) != 1 || !list[0].Live || list[0].Signal != "" || list[0].SignalSince != nil {
		t.Fatalf("list = %+v", list)
	}
}

func TestCardSummaryBlockedSignal(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	blocker, _ := a.st.CreateCard(domain.Card{Title: "Bloqueador", ProjectID: p.ID})
	blocked, _ := a.st.CreateCard(domain.Card{Title: "Bloqueado", ProjectID: p.ID})
	orphan, _ := a.st.CreateCard(domain.Card{Title: "Órfão", ProjectID: p.ID})
	a.setCard(t, blocker.Key, domain.EmAndamento, testNow)
	a.setCard(t, blocked.Key, domain.Pausado, testNow.Add(-time.Minute))
	a.setCard(t, orphan.Key, domain.Pausado, testNow.Add(-time.Minute))
	for key, by := range map[string]string{blocked.Key: blocker.Key, orphan.Key: "NAC-999"} {
		if _, err := a.st.DB.Exec(`UPDATE cards SET blocked_by=? WHERE key=?`, by, key); err != nil {
			t.Fatal(err)
		}
	}
	get := func() map[string]CardSummary {
		rec := a.do(t, "GET", "/api/cards", nil, 0)
		if rec.Code != 200 {
			t.Fatalf("code %d %s", rec.Code, rec.Body)
		}
		got := map[string]CardSummary{}
		for _, c := range decode[[]CardSummary](t, rec) {
			got[c.Key] = c
		}
		return got
	}
	got := get()
	if c := got[blocked.Key]; c.Signal != domain.SignalBloqueado || c.BlockedBy != blocker.Key || c.SignalSince == nil {
		t.Fatalf("bloqueado = %+v", c)
	}
	if c := got[orphan.Key]; c.Signal != "" {
		t.Fatalf("órfão = %q", c.Signal)
	}
	a.setCard(t, blocker.Key, domain.Concluido, testNow)
	if c := get()[blocked.Key]; c.Signal != domain.SignalProntoParaRetomar {
		t.Fatalf("pronto = %q", c.Signal)
	}
}

func TestPatchBlockedBy(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	if _, err := a.st.CreateCard(domain.Card{Title: "Outro", ProjectID: c.ProjectID}); err != nil {
		t.Fatal(err)
	}
	patch := func(v string) *httptest.ResponseRecorder {
		return a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"blocked_by": v}, 0)
	}
	for _, self := range []string{"NAC-1", "nac-1"} {
		if rec := patch(self); rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "um card não pode bloquear a si mesmo" {
			t.Fatalf("própria %q = %d %s", self, rec.Code, rec.Body)
		}
	}
	if rec := patch("NAC-999"); rec.Code != http.StatusNotFound || decode[map[string]string](t, rec)["error"] != "card NAC-999 não encontrado" {
		t.Fatalf("inexistente = %d %s", rec.Code, rec.Body)
	}
	if rec := patch(" nac-2 "); rec.Code != http.StatusOK {
		t.Fatalf("minúsculas = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.BlockedBy != "NAC-2" {
		t.Fatalf("blocked_by = %q", got.BlockedBy)
	}
	if rec := patch(""); rec.Code != http.StatusOK {
		t.Fatalf("limpar = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.BlockedBy != "" {
		t.Fatalf("blocked_by = %q", got.BlockedBy)
	}
}

// I3: sair de Pausado limpa o motivo da pausa e a marca de handoff
// incompleto; mudar outro campo sem mudar a coluna não limpa.
func TestPatchColumnChangeClearsPauseMarks(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	pause := func() {
		t.Helper()
		got, _ := a.st.GetCard(c.Key)
		got.Column, got.PauseReason, got.HandoffIncomplete = domain.Pausado, domain.PauseReasonLimit, true
		if err := a.st.UpdateCard(got); err != nil {
			t.Fatal(err)
		}
	}
	pause()
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"title": "Novo"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.PauseReason != domain.PauseReasonLimit || !got.HandoffIncomplete {
		t.Fatalf("sem mudar a coluna limpou: %+v", got)
	}
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "pausado"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.PauseReason != domain.PauseReasonLimit || !got.HandoffIncomplete {
		t.Fatalf("mesma coluna limpou: %+v", got)
	}
	for _, col := range []domain.Column{domain.Revisao, domain.Backlog, domain.Concluido} {
		pause()
		if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": col}, 0); rec.Code != http.StatusOK {
			t.Fatalf("PATCH %s = %d %s", col, rec.Code, rec.Body)
		}
		if got, _ := a.st.GetCard(c.Key); got.Column != col || got.PauseReason != "" || got.HandoffIncomplete {
			t.Fatalf("%s: card = %+v", col, got)
		}
	}
}
