package kiro

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const sessID = "kiro:" + providerID

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

type fakeGit struct{}

func (fakeGit) Branch(string) string           { return "main" }
func (fakeGit) Head(string) string             { return "abc123" }
func (fakeGit) DiffStat(string, string) string { return "" }
func (fakeGit) Clean(string) bool              { return true }
func (fakeGit) Checkout(string, string) error  { return nil }

func adapterDeps() adapter.Deps {
	return adapter.Deps{Now: func() time.Time { return t0 }, Git: fakeGit{}}
}

type env struct {
	st *store.Store
	a  *Adapter
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cockpit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.CreateProject("api", "/w/api"); err != nil {
		t.Fatal(err)
	}
	return &env{st: st, a: New(st, adapterDeps())}
}

// call manda a fixture como o `nobre hook kiro <event>` mandaria.
func (e *env) call(t *testing.T, event, name string, pid int) adapter.HookReply {
	t.Helper()
	r, err := e.a.HandleHook(context.Background(), adapter.HookCall{Event: event, Payload: readFixture(t, name), AgentPID: pid})
	if err != nil {
		t.Fatalf("%s: %v", event, err)
	}
	return r
}

func (e *env) events(t *testing.T, kind string) []domain.Event {
	t.Helper()
	all, err := e.st.SessionEvents([]string{sessID}, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Event
	for _, ev := range all {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

// O SessionEnd chega com o kiro-cli já morto (sem PID do agente): encerra a
// sessão kiro:<id> pelo id do payload, sem reabri-la nem trocar o PID.
func TestSessionEndWithoutAgentPIDFinishes(t *testing.T) {
	e := newEnv(t)
	e.call(t, "SessionStart", "session_start", 4242)
	if x, err := e.st.GetSession(sessID); err != nil || x.PID != 4242 || x.Agent != "kiro" || !x.Live() {
		t.Fatalf("sessão = %+v, %v", x, err)
	}
	if r := e.call(t, "SessionEnd", "session_end", 0); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	x, err := e.st.GetSession(sessID)
	if err != nil || x.EndedAt == nil || x.EndReason != "saiu do kiro-cli" || x.PID != 4242 {
		t.Fatalf("sessão = %+v, %v", x, err)
	}
	evs := e.events(t, domain.EvSessionEnd)
	var d struct {
		Reason string `json:"reason"`
	}
	if len(evs) != 1 || json.Unmarshal(evs[0].Data, &d) != nil || d.Reason != "shutdown" {
		t.Fatalf("session_end = %+v", evs)
	}
}

// SessionEnd de uma sessão que o Cockpit não viu nascer não cria nada.
func TestSessionEndUnknownSessionNotCreated(t *testing.T) {
	e := newEnv(t)
	if r := e.call(t, "SessionEnd", "session_end", 0); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	if _, err := e.st.GetSession(sessID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("sessão criada pelo SessionEnd: err = %v", err)
	}
}

// O início traz as instruções em texto puro no stdout, sem JSON.
func TestSessionStartPlainContext(t *testing.T) {
	e := newEnv(t)
	r := e.call(t, "SessionStart", "session_start", 4242)
	if r.Exit != 0 || r.Stderr != "" || len(r.Stdout) == 0 || r.Stdout[0] == '{' {
		t.Fatalf("resposta = %+v", r)
	}
	if json.Valid([]byte(r.Stdout)) {
		t.Fatalf("contexto do início em JSON: %s", r.Stdout)
	}
}

func TestCapabilities(t *testing.T) {
	var a any = New(nil, adapter.Deps{})
	ad := a.(*Adapter)
	if ad.Name() != "kiro" || !reflect.DeepEqual(ad.ProcessNames(), []string{"kiro-cli"}) || ad.SessionEnv() != "KIRO_SESSION_ID" {
		t.Fatalf("nome %q processos %v variável %q", ad.Name(), ad.ProcessNames(), ad.SessionEnv())
	}
	if su, ok := a.(agentcore.SpendUnit); !ok || !su.SpendsCredits() {
		t.Fatal("o Kiro gasta créditos")
	}
	if g, ok := a.(agentcore.Guide); !ok || g.GuideName() == "" {
		t.Fatal("o Kiro cita o steering")
	}
	// Sem janelas filhas, memórias nem notas. O leitor do histórico
	// (Ingester) também lê a cada prompt, como Observer.
	if _, ok := a.(agentcore.Ingester); !ok {
		t.Fatal("o Kiro lê o histórico")
	}
	if _, ok := a.(agentcore.Observer); !ok {
		t.Fatal("o Kiro observa o prompt para ler o histórico")
	}
	if _, ok := a.(agentcore.Router); ok {
		t.Fatal("o Kiro não escolhe a sessão")
	}
	if _, ok := a.(agentcore.Memories); ok {
		t.Fatal("o Kiro não tem memórias")
	}
	if _, ok := a.(agentcore.Noter); ok {
		t.Fatal("o Kiro não tem notas")
	}
}
