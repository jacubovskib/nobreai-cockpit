package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func TestRenderDraftBody(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	rec := a.do(t, "POST", "/api/prompts/render", map[string]any{"card": c.Key, "body": "Card {{card.id}}: {{card.titulo}}"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("render = %d %s", rec.Code, rec.Body)
	}
	if got := decode[map[string]string](t, rec)["text"]; got != "Card "+c.Key+": OAuth2" {
		t.Fatalf("text = %q", got)
	}
	rec = a.do(t, "POST", "/api/prompts/render", map[string]any{"card": c.Key, "body": "oi {{nao.existe}}"}, 0)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("desconhecida = %d %s", rec.Code, rec.Body)
	}
	if u := decode[struct {
		Unknown []string `json:"unknown"`
	}](t, rec).Unknown; len(u) != 1 || u[0] != "nao.existe" {
		t.Fatalf("unknown = %v", u)
	}
	if rec := a.do(t, "POST", "/api/prompts/render", map[string]any{"card": "NAC-999", "body": "x"}, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("card inexistente = %d", rec.Code)
	}
}

func TestListEventsByKind(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	for _, e := range []domain.Event{
		{SessionID: "claude:s", Kind: domain.EvPrompt, Data: json.RawMessage(`{"text":"faz isso"}`), At: testNow},
		{SessionID: "claude:s", Kind: domain.EvNote, Data: json.RawMessage(`{"text":"nota"}`), At: testNow},
	} {
		if err := a.st.AddEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	rec := a.do(t, "GET", "/api/events?kind=prompt&limit=10", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("events = %d %s", rec.Code, rec.Body)
	}
	evs := decode[[]store.EventWithContext](t, rec)
	if len(evs) != 1 || evs[0].Kind != "prompt" || evs[0].Agent != "claude" || evs[0].CardKey != c.Key || string(evs[0].Data) != `{"text":"faz isso"}` {
		t.Fatalf("events = %+v", evs)
	}
	if rec := a.do(t, "GET", "/api/events", nil, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("sem kind = %d", rec.Code)
	}
}
