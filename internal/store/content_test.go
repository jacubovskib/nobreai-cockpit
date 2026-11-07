package store

import (
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func newCard(t *testing.T, s *Store) domain.Card {
	t.Helper()
	p, err := s.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCard(domain.Card{Title: "x", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUpsertEssentialFile(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	if err := s.UpsertEssentialFile(domain.EssentialFile{CardID: c.ID, Path: "a.go", Lines: "1-5", Why: "velho"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEssentialFile(domain.EssentialFile{CardID: c.ID, Path: "a.go", Lines: "10-20", Why: "novo"}); err != nil {
		t.Fatal(err)
	}
	ct, err := s.CardContent(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct.Files) != 1 || ct.Files[0].Why != "novo" || ct.Files[0].Lines != "10-20" {
		t.Fatalf("files = %+v", ct.Files)
	}
}

func TestCardContentRoundTrip(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	at := time.Now().UTC().Truncate(time.Second)
	if err := s.SetWhereStopped(c.ID, "parei aqui", at); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCard(c.Key)
	if got.WhereStopped != "parei aqui" || got.WhereStoppedAt == nil || !got.WhereStoppedAt.Equal(at) {
		t.Fatalf("card = %+v", got)
	}
	d, err := s.AddDecision(domain.Decision{CardID: c.ID, What: "usar sqlite", Why: "simples", At: at})
	if err != nil || d.ID == 0 {
		t.Fatalf("decision = %+v, %v", d, err)
	}
	td, err := s.AddTodo(domain.Todo{CardID: c.ID, Text: "escrever"})
	if err != nil || td.ID == 0 {
		t.Fatal(err)
	}
	if err := s.SetTodoDone(td.ID, true); err != nil {
		t.Fatal(err)
	}
	q, err := s.AddQuestion(domain.Question{CardID: c.ID, Text: "e agora?"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AnswerQuestion(q.ID, "assim"); err != nil {
		t.Fatal(err)
	}
	ct, err := s.CardContent(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct.Decisions) != 1 || ct.Decisions[0].What != "usar sqlite" || !ct.Decisions[0].At.Equal(at) {
		t.Fatalf("decisions = %+v", ct.Decisions)
	}
	if len(ct.Todos) != 1 || !ct.Todos[0].Done {
		t.Fatalf("todos = %+v", ct.Todos)
	}
	if len(ct.Questions) != 1 || ct.Questions[0].Answer != "assim" {
		t.Fatalf("questions = %+v", ct.Questions)
	}
	if err := s.SetTodoDone(999, true); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
	if err := s.AnswerQuestion(999, "x"); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
	if err := s.SetWhereStopped(999, "x", at); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestLastEventAt(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	got, err := s.LastEventAt(c.ID, "where_stopped_edit")
	if err != nil || got != nil {
		t.Fatalf("got = %v, %v", got, err)
	}
	t1 := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	t2 := t1.Add(time.Minute)
	if _, err := s.DB.Exec(`INSERT INTO sessions(id, agent, started_at, last_event_at) VALUES ('s1','claude',?,?)`, fmtTime(t1), fmtTime(t1)); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{t1, t2} {
		if _, err := s.DB.Exec(`INSERT INTO events(session_id, card_id, kind, at) VALUES ('s1', ?, 'edit', ?)`, c.ID, fmtTime(at)); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.LastEventAt(c.ID, "edit")
	if err != nil || got == nil || !got.Equal(t2) {
		t.Fatalf("got = %v, %v", got, err)
	}
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	s := newTestStore(t)
	st, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Rules["goal"] || st.Rules["no_open_questions"] || st.WhereStoppedTTLMin != 15 || st.GraceMin != 15 ||
		st.ReminderEditThreshold != 3 || st.LaunchMode != "terminal" || st.Currency != "USD" ||
		st.IncompleteAction != "ask_agent" || len(st.TerminalCmd) != 2 {
		t.Fatalf("defaults = %+v", st)
	}
	st.GraceMin = 30
	st.Rules["no_open_questions"] = true
	st.BRLRate = 5.5
	if err := s.PutSettings(st); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.GraceMin != 30 || !got.Rules["no_open_questions"] || got.BRLRate != 5.5 || got.WhereStoppedTTLMin != 15 {
		t.Fatalf("got = %+v", got)
	}
}
