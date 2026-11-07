package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// fakeAdapter devolve reply/err fixos e guarda a última chamada.
type fakeAdapter struct {
	reply adapter.HookReply
	err   error
	got   adapter.HookCall
	ctx   context.Context
}

func (f *fakeAdapter) Name() string           { return "fake" }
func (f *fakeAdapter) ProcessNames() []string { return []string{"fake"} }
func (f *fakeAdapter) HandleHook(ctx context.Context, h adapter.HookCall) (adapter.HookReply, error) {
	f.got, f.ctx = h, ctx
	return f.reply, f.err
}
func (f *fakeAdapter) Detect() adapter.Detection               { return adapter.Detection{} }
func (f *fakeAdapter) Install() (adapter.InstallReport, error) { return adapter.InstallReport{}, nil }
func (f *fakeAdapter) LaunchCommand(p string) []string         { return []string{"fake", p} }

func newHookAPI(t *testing.T, adapters map[string]adapter.Adapter) *testAPI {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	h := New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Adapters: adapters})
	return &testAPI{st: st, h: h, hub: hub}
}

func TestHookUnknownAgent(t *testing.T) {
	a := newHookAPI(t, nil)
	rec := a.do(t, "POST", "/api/hooks/codex", map[string]any{"event": "Stop", "payload": map[string]any{}}, 0)
	if rec.Code != http.StatusNotFound || decode[map[string]string](t, rec)["error"] != "agente desconhecido" {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

func TestHookPassesCallAndReply(t *testing.T) {
	f := &fakeAdapter{reply: adapter.HookReply{Stdout: `{"decision":"block"}`}}
	a := newHookAPI(t, map[string]adapter.Adapter{"fake": f})
	rec := a.do(t, "POST", "/api/hooks/fake", map[string]any{
		"event": "Stop", "payload": map[string]any{"session_id": "s"}, "agent_pid": 77, "env": map[string]string{"NOBRE_RESUME": "NAC-1"},
	}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got := decode[adapter.HookReply](t, rec); got.Stdout != `{"decision":"block"}` || got.Exit != 0 {
		t.Fatalf("reply = %+v", got)
	}
	if f.got.Event != "Stop" || f.got.AgentPID != 77 || f.got.Env["NOBRE_RESUME"] != "NAC-1" || string(f.got.Payload) != `{"session_id":"s"}` {
		t.Fatalf("call = %+v", f.got)
	}
	if f.ctx.Done() != nil {
		t.Fatal("o adaptador deve receber um contexto desligado da requisição")
	}
}

func TestHookErrorNeverBreaksAgent(t *testing.T) {
	f := &fakeAdapter{err: errors.New("banco fora")}
	a := newHookAPI(t, map[string]adapter.Adapter{"fake": f})
	rec := a.do(t, "POST", "/api/hooks/fake", map[string]any{"event": "Stop", "payload": map[string]any{}}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if got := decode[adapter.HookReply](t, rec); got.Stdout != "" || got.Exit != 0 {
		t.Fatalf("reply = %+v", got)
	}
}

func TestHookClaudeEndToEnd(t *testing.T) {
	var a *testAPI
	hub := NewHub()
	ch, cancel := hub.Subscribe()
	defer cancel()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	t.Cleanup(hub.Close)
	ad := claude.New(st, adapter.Deps{Now: func() time.Time { return testNow }, Publish: hub.Publish})
	t.Cleanup(ad.Wait)
	a = &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub,
		Adapters: map[string]adapter.Adapter{"claude": ad}})}
	if _, err := st.CreateProject("api", "/w/api"); err != nil {
		t.Fatal(err)
	}

	payload := json.RawMessage(`{"session_id":"abc","transcript_path":"/nao/existe.jsonl","cwd":"/w/api","hook_event_name":"SessionStart","source":"startup"}`)
	rec := a.do(t, "POST", "/api/hooks/claude", adapter.HookCall{Event: "SessionStart", Payload: payload, AgentPID: 4242}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	reply := decode[adapter.HookReply](t, rec)
	if !strings.Contains(reply.Stdout, `"hookEventName":"SessionStart"`) || !strings.Contains(reply.Stdout, "nobre start") {
		t.Fatalf("reply = %+v", reply)
	}
	x, err := st.GetSession("claude:abc")
	if err != nil || x.PID != 4242 || !x.FullCapture {
		t.Fatalf("sessão = %+v %v", x, err)
	}
	// A sessão vira alvo das rotas do agente pelo PID.
	if rec := a.do(t, "GET", "/api/me/status", nil, 4242); rec.Code != http.StatusOK {
		t.Fatalf("me/status = %d %s", rec.Code, rec.Body)
	}
	select {
	case k := <-ch:
		if k != "sessions" {
			t.Fatalf("kind = %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("nada publicado no stream")
	}
}
