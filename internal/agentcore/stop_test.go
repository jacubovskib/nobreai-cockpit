package agentcore

import (
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// cardSession cria o card com "Onde parei" em t0 e liga a sessão a ele pelo
// NOBRE_RESUME.
func (e *env) cardSession(t *testing.T, d *fakeDriver) domain.Card {
	t.Helper()
	p := e.project(t)
	at := t0
	c, err := e.st.CreateCard(domain.Card{ProjectID: p.ID, Title: "Migrar OAuth2", Branch: "main",
		WhereStopped: "lendo o oauth.go", WhereStoppedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	e.handle(t, d, Event{Kind: Started, Source: "startup", Env: map[string]string{"NOBRE_RESUME": c.Key}})
	return c
}

func TestTurnEndHoldsWhenEditedAfterWhereStopped(t *testing.T) {
	e, d := newFake(t)
	c := e.cardSession(t, d)
	e.c.Deps().Now = func() time.Time { return t0.Add(time.Minute) }
	e.handle(t, d, Event{Kind: TurnEnd})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("sem edição segurou o turno: %+v", r.HoldTurn)
	}
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/auth/oauth.go"}})
	e.c.Deps().Now = func() time.Time { return t0.Add(2 * time.Minute) }
	e.handle(t, d, Event{Kind: TurnEnd})
	r := lastReply(t, d)
	if r.HoldTurn == nil || !strings.Contains(r.HoldTurn.Reason, "Antes de encerrar, atualize o card "+c.Key) ||
		strings.Contains(r.HoldTurn.Reason, "{{") {
		t.Fatalf("HoldTurn = %+v", r.HoldTurn)
	}
	// A continuação de um turno segurado nunca é segurada de novo.
	e.handle(t, d, Event{Kind: TurnEnd, StopActive: true})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("StopActive segurou: %+v", r.HoldTurn)
	}
	// Com IncompleteAction "mark", só marca no fim da sessão.
	set, err := e.st.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	set.IncompleteAction = "mark"
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("mark segurou: %+v", r.HoldTurn)
	}
}

func TestTurnEndRemindsOnceWithoutCard(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + f}})
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	want := `NobreAI Cockpit: esta sessão já editou 3 arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`
	if r := lastReply(t, d); r.HoldTurn == nil || r.HoldTurn.Reason != want {
		t.Fatalf("lembrete = %+v", r.HoldTurn)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("lembrou de novo: %+v", r.HoldTurn)
	}
}
