package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
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
func (f *fakeAdapter) Binary() string                          { return "/x/fake" }
func (f *fakeAdapter) LaunchCommand(bin, p string) []string    { return []string{bin, p} }

func newHookAPI(t *testing.T, adapters *agents.Registry) *testAPI {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	h := New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Agents: adapters})
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
	a := newHookAPI(t, agents.New(f))
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
	a := newHookAPI(t, agents.New(f))
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
		Agents: agents.New(ad)})}
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

// recGit guarda o since pedido ao DiffStat.
type recGit struct{ since string }

func (g *recGit) Branch(string) string { return "main" }
func (g *recGit) Head(string) string   { return "" }
func (g *recGit) DiffStat(_, since string) string {
	g.since = since
	return " a.go | 1 +"
}
func (g *recGit) Clean(string) bool             { return true }
func (g *recGit) Checkout(string, string) error { return nil }

func TestMeResumeDiffSincePreviousSession(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &recGit{}
	a := &testAPI{st: st, h: New(st, Deps{Now: func() time.Time { return testNow }, Git: g})}
	c := a.card(t)
	prev := domain.Session{ID: "claude:prev", Agent: "claude", PID: 1, GitHead: "head-prev", StartedAt: testNow.Add(-time.Hour)}
	cur := domain.Session{ID: "claude:cur", Agent: "claude", PID: 4242, GitHead: "head-cur", StartedAt: testNow}
	for _, x := range []domain.Session{prev, cur} {
		if err := st.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AttachSession(prev.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := st.EndSession(prev.ID, "fim", testNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/me/resume", map[string]any{"key": c.Key}, 4242)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if g.since != "head-prev" || !strings.Contains(decode[map[string]string](t, rec)["markdown"], "a.go | 1 +") {
		t.Fatalf("since = %q (quer o HEAD da sessão anterior, não o da que acabou de entrar)", g.since)
	}
	// GET /resume não exclui ninguém: base é a sessão mais recente do card.
	if rec := a.do(t, "GET", "/api/cards/"+c.Key+"/resume", nil, 0); rec.Code != http.StatusOK || g.since != "head-cur" {
		t.Fatalf("code = %d since = %q", rec.Code, g.since)
	}
}

func TestHookAcceptsLargePayload(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ad := claude.New(st, adapter.Deps{Now: func() time.Time { return testNow }})
	t.Cleanup(ad.Wait)
	a := &testAPI{st: st, h: New(st, Deps{Now: func() time.Time { return testNow }, Agents: agents.New(ad)})}
	if _, err := st.CreateProject("api", "/w/api"); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("x", 1<<20) // Write leva o conteúdo duas vezes: ~2 MiB
	payload, _ := json.Marshal(map[string]any{
		"session_id": "big", "transcript_path": "/nao/existe.jsonl", "cwd": "/w/api", "hook_event_name": "PostToolUse",
		"tool_name": "Write", "tool_input": map[string]any{"file_path": "/w/api/big.txt", "content": content},
		"tool_response": map[string]any{"type": "create", "filePath": "/w/api/big.txt", "content": content},
	})
	rec := a.do(t, "POST", "/api/hooks/claude", adapter.HookCall{Event: "PostToolUse", Payload: payload, AgentPID: 4242}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	evs, _ := st.RecentEvents(10)
	found := false
	for _, ev := range evs {
		found = found || (ev.Kind == domain.EvFileEdit && string(ev.Data) == `{"path":"big.txt"}`)
	}
	if !found {
		t.Fatalf("file_edit não registrado: %+v", evs)
	}
}

func TestHookRejectsHugePayloadAndLogs(t *testing.T) {
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	f := &fakeAdapter{}
	a := newHookAPI(t, agents.New(f))
	big := strings.Repeat("x", maxHookBody)
	rec := a.do(t, "POST", "/api/hooks/fake", map[string]any{"event": "PostToolUse", "payload": map[string]any{"c": big}}, 0)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d", rec.Code)
	}
	if f.got.Event != "" {
		t.Fatal("o adaptador não deveria ser chamado")
	}
	if out := buf.String(); !strings.Contains(out, "hook fake PostToolUse") || !strings.Contains(out, "bytes") {
		t.Fatalf("log = %q", out)
	}
}
