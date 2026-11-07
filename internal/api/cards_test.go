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
