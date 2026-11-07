package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func newSession(id string, pid int) domain.Session {
	now := time.Now().UTC()
	return domain.Session{ID: id, Agent: "claude", ProviderSessionID: id, PID: pid, StartedAt: now, LastEventAt: now}
}

func seedCard(t *testing.T, s *Store, col domain.Column) domain.Card {
	t.Helper()
	var pid int64
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(id),0) FROM projects`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if pid == 0 {
		p, err := s.CreateProject("api", "/w/api")
		if err != nil {
			t.Fatal(err)
		}
		pid = p.ID
	}
	c, err := s.CreateCard(domain.Card{Title: "t", ProjectID: pid, Column: col})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStartSessionIdempotent(t *testing.T) {
	s := newTestStore(t)
	a := newSession("claude:1", 10)
	if err := s.StartSession(a); err != nil {
		t.Fatal(err)
	}
	a.Model = "outro"
	if err := s.StartSession(a); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession("claude:1")
	if err != nil || got.Model != "" || got.PID != 10 || got.EndedAt != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := s.GetSession("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionByPID(t *testing.T) {
	s := newTestStore(t)
	old := newSession("claude:old", 10)
	old.StartedAt = time.Now().UTC().Add(-time.Hour)
	cur := newSession("claude:cur", 10)
	for _, x := range []domain.Session{old, cur} {
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SessionByPID(10)
	if err != nil || got == nil || got.ID != "claude:cur" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := s.EndSession("claude:cur", "exit", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ = s.SessionByPID(10)
	if got == nil || got.ID != "claude:old" {
		t.Fatalf("got %+v", got)
	}
	if got, err := s.SessionByPID(99); err != nil || got != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAttachSessionOneOwner(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	for _, id := range []string{"claude:a", "claude:b"} {
		if err := s.StartSession(newSession(id, 1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c.Key); got.Column != domain.EmAndamento {
		t.Fatalf("column = %s", got.Column)
	}
	if err := s.AttachSession("claude:b", c.Key, false); !errors.Is(err, ErrCardBusy) {
		t.Fatalf("err = %v, want ErrCardBusy", err)
	}
	if b, _ := s.GetSession("claude:b"); b.CardID != nil {
		t.Fatal("B não deveria ter card")
	}
	if err := s.AttachSession("claude:b", c.Key, true); err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetSession("claude:a")
	b, _ := s.GetSession("claude:b")
	if a.CardID != nil || b.CardID == nil || *b.CardID != c.ID {
		t.Fatalf("a=%v b=%v", a.CardID, b.CardID)
	}
	// reanexar a mesma sessão não é conflito
	if err := s.AttachSession("claude:b", c.Key, false); err != nil {
		t.Fatal(err)
	}
	// sessão encerrada não bloqueia
	if err := s.EndSession("claude:b", "exit", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(newSession("claude:c", 2)); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:c", c.Key, false); err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := s.AttachSession("claude:c", "NAC-99", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := s.AttachSession("nope", c.Key, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAttachCopiesEvents(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	if err := s.StartSession(newSession("claude:a", 1)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.AddEvent(domain.Event{SessionID: "claude:a", Kind: domain.EvFileEdit, Data: json.RawMessage(`{"path":"a.go"}`), At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	evs, _ := s.RecentEvents(10)
	if len(evs) != 2 || evs[0].CardID != nil {
		t.Fatalf("evs = %+v", evs)
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	evs, _ = s.RecentEvents(10)
	for _, e := range evs {
		if e.CardID == nil || *e.CardID != c.ID {
			t.Fatalf("event sem card: %+v", e)
		}
	}
	// evento novo já nasce com card_id
	if err := s.AddEvent(domain.Event{SessionID: "claude:a", Kind: domain.EvPrompt, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	evs, _ = s.RecentEvents(1)
	if len(evs) != 1 || evs[0].Kind != domain.EvPrompt || evs[0].CardID == nil || string(evs[0].Data) != "{}" {
		t.Fatalf("evs = %+v", evs)
	}
}

func TestEndSessionMovesCard(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	if err := s.StartSession(newSession("claude:a", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession("claude:a", "exit", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c.Key); got.Column != domain.Pausado {
		t.Fatalf("column = %s", got.Column)
	}
	a, _ := s.GetSession("claude:a")
	if a.EndedAt == nil || a.EndReason != "exit" {
		t.Fatalf("a = %+v", a)
	}

	c2 := seedCard(t, s, domain.Backlog)
	if err := s.StartSession(newSession("claude:b", 2)); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:b", c2.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveCard(c2.Key, domain.Revisao); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession("claude:b", "exit", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c2.Key); got.Column != domain.Revisao {
		t.Fatalf("column = %s", got.Column)
	}
	if err := s.EndSession("nope", "x", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLiveTouchRemindForCard(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	for i, id := range []string{"claude:a", "claude:b"} {
		if err := s.StartSession(newSession(id, i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(time.Minute)
	if err := s.TouchSession("claude:a", at); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchSession("nope", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	a, _ := s.GetSession("claude:a")
	if !a.LastEventAt.Equal(at) {
		t.Fatalf("last = %v want %v", a.LastEventAt, at)
	}
	if err := s.MarkReminded("claude:a"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReminded("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := s.EndSession("claude:b", "exit", time.Now()); err != nil {
		t.Fatal(err)
	}
	live, err := s.LiveSessions()
	if err != nil || len(live) != 1 || live[0].ID != "claude:a" {
		t.Fatalf("live = %+v, %v", live, err)
	}
	for_card, err := s.SessionsForCard(c.ID)
	if err != nil || len(for_card) != 1 || for_card[0].ID != "claude:a" {
		t.Fatalf("for card = %+v, %v", for_card, err)
	}
}

func TestAttachEndedSessionLeavesCardPaused(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	if err := s.StartSession(newSession("claude:old", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AddEvent(domain.Event{SessionID: "claude:old", Kind: domain.EvFileEdit, Data: json.RawMessage(`{"path":"a.go"}`), At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession("claude:old", "exit", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:old", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c.Key); got.Column != domain.Pausado {
		t.Fatalf("column = %s, want pausado", got.Column)
	}
	if x, _ := s.GetSession("claude:old"); x.CardID == nil || *x.CardID != c.ID {
		t.Fatalf("session card = %v", x.CardID)
	}
	evs, _ := s.RecentEvents(10)
	if len(evs) != 1 || evs[0].CardID == nil || *evs[0].CardID != c.ID {
		t.Fatalf("evs = %+v", evs)
	}
}

func TestRecentSessions(t *testing.T) {
	s := newTestStore(t)
	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"claude:1", "claude:2", "claude:3"} {
		x := newSession(id, i+1)
		x.StartedAt = base.Add(time.Duration(i) * time.Minute)
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.RecentSessions(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "claude:3" || got[1].ID != "claude:2" {
		t.Fatalf("got %+v", got)
	}
}

func TestReattachMovesPreviousCardToPaused(t *testing.T) {
	s := newTestStore(t)
	c1 := seedCard(t, s, domain.Backlog)
	c2 := seedCard(t, s, domain.Backlog)
	if err := s.StartSession(newSession("claude:a", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:a", c1.Key, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c1.Key); got.Column != domain.EmAndamento {
		t.Fatalf("c1 = %s", got.Column)
	}
	if err := s.AttachSession("claude:a", c2.Key, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c1.Key); got.Column != domain.Pausado {
		t.Fatalf("c1 = %s, want pausado", got.Column)
	}
	if got, _ := s.GetCard(c2.Key); got.Column != domain.EmAndamento {
		t.Fatalf("c2 = %s, want em_andamento", got.Column)
	}

	// card anterior com outra sessão viva continua em andamento
	if err := s.StartSession(newSession("claude:b", 2)); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(newSession("claude:c", 3)); err != nil {
		t.Fatal(err)
	}
	c3 := seedCard(t, s, domain.Backlog)
	if err := s.AttachSession("claude:b", c3.Key, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE sessions SET card_id=? WHERE id='claude:c'`, c3.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:b", c1.Key, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c3.Key); got.Column != domain.EmAndamento {
		t.Fatalf("c3 = %s, want em_andamento (claude:c ainda viva)", got.Column)
	}
}
