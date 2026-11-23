package agentcore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

const fakeID = "fake:s1"

// fakeGit responde branch e HEAD fixos.
type fakeGit struct{ branch string }

func (g fakeGit) Branch(string) string           { return g.branch }
func (g fakeGit) Head(string) string             { return "abc123" }
func (g fakeGit) DiffStat(string, string) string { return "" }
func (g fakeGit) Clean(string) bool              { return true }
func (g fakeGit) Checkout(string, string) error  { return nil }

// fakeDriver devolve o próximo evento como tradução e guarda a resposta que
// recebe para formatar.
type fakeDriver struct {
	next    Event
	replies []Reply
}

func (d *fakeDriver) Name() string { return "fake" }

func (d *fakeDriver) Translate(event string, _ []byte) (Event, error) { return d.next, nil }

func (d *fakeDriver) Render(_ Event, r Reply) (adapter.HookReply, error) {
	d.replies = append(d.replies, r)
	return adapter.HookReply{Stdout: strings.Join(r.Context, "|")}, nil
}

// fullDriver tem as capacidades opcionais: responde com um texto fixo e
// anota as leituras do histórico pedidas.
type fullDriver struct {
	fakeDriver
	ingested []string
}

func (d *fullDriver) Respond(_ *Core, sess domain.Session, ev Event) (Reply, error) {
	return Reply{Context: []string{"resposta " + sess.ID}}, nil
}

func (d *fullDriver) Ingest(sess domain.Session, ev Event) {
	d.ingested = append(d.ingested, sess.ID+" "+ev.TranscriptPath)
}

type env struct {
	st *store.Store
	c  *Core
	d  *fakeDriver
}

func newEnv(t *testing.T, d Driver) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cockpit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{st: st}
	e.c = New(st, adapter.Deps{Now: func() time.Time { return t0 }, Git: fakeGit{branch: "main"}}, d)
	return e
}

func newFake(t *testing.T) (*env, *fakeDriver) {
	d := &fakeDriver{}
	e := newEnv(t, d)
	e.d = d
	return e, d
}

func (e *env) handle(t *testing.T, d *fakeDriver, ev Event) adapter.HookReply {
	t.Helper()
	if ev.ProviderSessionID == "" {
		ev.ProviderSessionID = "s1"
	}
	if ev.Cwd == "" {
		ev.Cwd = "/w/api"
	}
	pid := 4242
	if ev.PID != 0 {
		pid = ev.PID
	}
	d.next = ev
	r, err := e.c.Handle(context.Background(), adapter.HookCall{AgentPID: pid})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *env) events(t *testing.T, kind string) []map[string]any {
	t.Helper()
	all, err := e.st.RecentEvents(1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, ev := range all {
		if ev.Kind != kind {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(ev.Data, &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func (e *env) project(t *testing.T) domain.Project {
	t.Helper()
	p, err := e.st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStartedCreatesSessionWithLocationAndProject(t *testing.T) {
	e, d := newFake(t)
	p := e.project(t)
	e.handle(t, d, Event{Kind: Started, Source: "startup", TranscriptPath: "/t/s1.jsonl"})
	x, err := e.st.GetSession(fakeID)
	if err != nil {
		t.Fatal(err)
	}
	if x.Agent != "fake" || x.ProviderSessionID != "s1" || x.PID != 4242 || x.Cwd != "/w/api" || x.Branch != "main" ||
		x.GitHead != "abc123" || x.TranscriptPath != "/t/s1.jsonl" || !x.StartedAt.Equal(t0) {
		t.Fatalf("sessão = %+v", x)
	}
	if x.ProjectID == nil || *x.ProjectID != p.ID || !x.FullCapture {
		t.Fatalf("projeto = %v, captura = %v", x.ProjectID, x.FullCapture)
	}
	if evs := e.events(t, domain.EvSessionStart); len(evs) != 1 || evs[0]["source"] != "startup" {
		t.Fatalf("eventos = %v", evs)
	}
}

func TestPromptRecordedOnlyWithFullCapture(t *testing.T) {
	e, d := newFake(t)
	e.handle(t, d, Event{Kind: Prompt, Prompt: "sem projeto"})
	if evs := e.events(t, domain.EvPrompt); len(evs) != 0 {
		t.Fatalf("prompt gravado sem captura: %v", evs)
	}

	e, d = newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Prompt, Prompt: "corrija o login"})
	if evs := e.events(t, domain.EvPrompt); len(evs) != 1 || evs[0]["text"] != "corrija o login" {
		t.Fatalf("eventos = %v", evs)
	}
}

func TestToolAfterRecordsFilesAndCommands(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/auth/oauth.go"}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolRead, Path: "/etc/hosts"}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit}}) // sem caminho: nada
	code := 1
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolCommand, Command: "go test ./...", ExitCode: &code}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolCommand, Command: "ls"}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolOther, Name: "WebFetch"}})

	if evs := e.events(t, domain.EvFileEdit); len(evs) != 1 || evs[0]["path"] != "auth/oauth.go" {
		t.Fatalf("edições = %v", evs)
	}
	if evs := e.events(t, domain.EvFileRead); len(evs) != 1 || evs[0]["path"] != "/etc/hosts" {
		t.Fatalf("leituras = %v", evs)
	}
	cmds := e.events(t, domain.EvCommand)
	if len(cmds) != 2 {
		t.Fatalf("comandos = %v", cmds)
	}
	byCmd := map[string]map[string]any{}
	for _, c := range cmds {
		byCmd[c["command"].(string)] = c
	}
	if c := byCmd["go test ./..."]; c == nil || c["exit_code"] != float64(1) {
		t.Fatalf("comando com código = %v", c)
	}
	if c := byCmd["ls"]; c == nil || c["exit_code"] != nil {
		t.Fatalf("comando sem código = %v", c)
	}
}

func TestToolAfterWithoutCaptureRecordsNothing(t *testing.T) {
	e, d := newFake(t)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/a.go"}})
	if evs := e.events(t, domain.EvFileEdit); len(evs) != 0 {
		t.Fatalf("edições = %v", evs)
	}
}

func TestEndedFinishesWithTranslatedReason(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	e.handle(t, d, Event{Kind: Ended, Reason: "prompt_input_exit"})
	x, _ := e.st.GetSession(fakeID)
	if x.EndedAt == nil || x.EndReason != "usuário saiu" {
		t.Fatalf("sessão = %+v", x)
	}
	if evs := e.events(t, domain.EvSessionEnd); len(evs) != 1 || evs[0]["reason"] != "prompt_input_exit" {
		t.Fatalf("eventos = %v", evs)
	}

	// Motivo desconhecido fica como veio; sem motivo, o padrão.
	for reason, want := range map[string]string{"desconhecido": "desconhecido", "": "encerrada pelo agente"} {
		e, d := newFake(t)
		e.handle(t, d, Event{Kind: Started})
		e.handle(t, d, Event{Kind: Ended, Reason: reason})
		if x, _ := e.st.GetSession(fakeID); x.EndReason != want {
			t.Fatalf("motivo %q: %q", reason, x.EndReason)
		}
	}
}

func TestEndedNeverCreatesNorReopens(t *testing.T) {
	e, d := newFake(t)
	if r := e.handle(t, d, Event{Kind: Ended}); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	if _, err := e.st.GetSession(fakeID); err == nil {
		t.Fatal("SessionEnd criou a sessão")
	}
	if len(d.replies) != 0 {
		t.Fatal("hook ignorado chegou ao Render")
	}

	e.handle(t, d, Event{Kind: Started})
	e.handle(t, d, Event{Kind: Ended})
	e.handle(t, d, Event{Kind: Ended, PID: 5555}) // repetido, com outro PID
	if x, _ := e.st.GetSession(fakeID); x.EndedAt == nil {
		t.Fatalf("SessionEnd reabriu a sessão: %+v", x)
	}
	e.handle(t, d, Event{Kind: Prompt}) // qualquer outro evento reabre
	if x, _ := e.st.GetSession(fakeID); !x.Live() {
		t.Fatalf("sessão não reaberta: %+v", x)
	}
}

func TestOptionalCapabilities(t *testing.T) {
	d := &fullDriver{}
	e := newEnv(t, d)
	e.handle(t, &d.fakeDriver, Event{Kind: TurnEnd, TranscriptPath: "/t/a.jsonl"})
	if got := e.handle(t, &d.fakeDriver, Event{Kind: Prompt}); got.Stdout != "resposta "+fakeID {
		t.Fatalf("resposta = %+v", got)
	}
	e.handle(t, &d.fakeDriver, Event{Kind: Ended, TranscriptPath: "/t/b.jsonl"})
	if len(d.ingested) != 2 || d.ingested[0] != fakeID+" /t/a.jsonl" || d.ingested[1] != fakeID+" /t/b.jsonl" {
		t.Fatalf("leituras = %v", d.ingested)
	}
}

// routerDriver resolve a sessão por conta própria: ignora tudo.
type routerDriver struct{ fakeDriver }

func (d *routerDriver) Route(*Core, Event) (domain.Session, bool, error) {
	return domain.Session{}, true, ErrIgnore
}

func TestRouterIgnoreRepliesEmpty(t *testing.T) {
	d := &routerDriver{}
	e := newEnv(t, d)
	if r := e.handle(t, &d.fakeDriver, Event{Kind: Started}); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	if _, err := e.st.GetSession(fakeID); err == nil {
		t.Fatal("sessão criada apesar do Router")
	}
}

func TestMissingSessionIDFails(t *testing.T) {
	e, d := newFake(t)
	d.next = Event{Kind: Started}
	if _, err := e.c.Handle(context.Background(), adapter.HookCall{}); err == nil {
		t.Fatal("esperava erro sem id de sessão")
	}
}

func TestOtherEventOnlyTouchesSession(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	e.c.Deps().Now = func() time.Time { return t0.Add(time.Minute) }
	if r := e.handle(t, d, Event{Kind: Other}); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	if x, _ := e.st.GetSession(fakeID); !x.LastEventAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("sessão = %+v", x)
	}
}
