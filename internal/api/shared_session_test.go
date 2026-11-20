package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Dois processos do Claude na mesma conversa: cada um tem a sua sessão e os
// comandos nobre de um não mexem no card do outro.
func TestNobreCommandsPerProcess(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	ad := claude.New(st, adapter.Deps{Now: func() time.Time { return testNow }, Publish: hub.Publish})
	ad.SetAlive(func(pid int) bool { return pid == 100 || pid == 200 })
	t.Cleanup(ad.Wait)
	a := &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub,
		Adapters: map[string]adapter.Adapter{"claude": ad}})}
	if _, err := st.CreateProject("api", "/w/api"); err != nil {
		t.Fatal(err)
	}
	hook := func(event string, pid int) {
		t.Helper()
		payload := json.RawMessage(`{"session_id":"abc","transcript_path":"/nao/existe.jsonl","cwd":"/w/api","hook_event_name":"` + event + `"}`)
		if rec := a.do(t, "POST", "/api/hooks/claude", adapter.HookCall{Event: event, Payload: payload, AgentPID: pid}, 0); rec.Code != http.StatusOK {
			t.Fatalf("hook %s = %d %s", event, rec.Code, rec.Body)
		}
	}
	hook("SessionStart", 100)
	if rec := a.do(t, "POST", "/api/me/start", map[string]string{"title": "Principal"}, 100); rec.Code != http.StatusCreated {
		t.Fatalf("start 100 = %d %s", rec.Code, rec.Body)
	}
	hook("SessionStart", 200)
	if rec := a.do(t, "POST", "/api/me/start", map[string]string{"title": "Outra janela"}, 200); rec.Code != http.StatusCreated {
		t.Fatalf("start 200 = %d %s", rec.Code, rec.Body)
	}
	hook("UserPromptSubmit", 100)
	hook("UserPromptSubmit", 200)

	card := func(pid int) string {
		t.Helper()
		st := decode[map[string]any](t, a.do(t, "GET", "/api/me/status", nil, pid))
		c, _ := st["card"].(map[string]any)
		k, _ := c["key"].(string)
		return k
	}
	if k := card(100); k != "NAC-1" {
		t.Fatalf("status 100 = %q", k)
	}
	if k := card(200); k != "NAC-2" {
		t.Fatalf("status 200 = %q", k)
	}
	child, err := st.GetSession("claude:abc@200")
	if err != nil || child.CardID == nil || child.ParentSessionID != "claude:abc" {
		t.Fatalf("filha = %+v %v", child, err)
	}
	c2, _ := st.GetCard("NAC-2")
	if *child.CardID != c2.ID {
		t.Fatalf("card da filha = %d, NAC-2 = %d", *child.CardID, c2.ID)
	}
	main, _ := st.GetSession("claude:abc")
	c1, _ := st.GetCard("NAC-1")
	if main.PID != 100 || main.CardID == nil || *main.CardID != c1.ID {
		t.Fatalf("principal = %+v", main)
	}
}
