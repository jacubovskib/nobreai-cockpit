package store

import (
	"encoding/json"
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

func TestTouchedFiles(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	if err := s.StartSession(domain.Session{ID: "claude:a", Agent: "claude", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	ev := func(kind, path string, min int) {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"path": path})
		if err := s.AddEvent(domain.Event{SessionID: "claude:a", Kind: kind, Data: data, At: t0.Add(time.Duration(min) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	ev(domain.EvFileRead, "lido.go", 9)
	ev(domain.EvFileRead, "ambos.go", 1)
	ev(domain.EvFileEdit, "ambos.go", 2)
	ev(domain.EvFileEdit, "editado.go", 5)
	ev(domain.EvFileRead, "velho.go", 3)
	ev(domain.EvCommand, "", 20)

	got, err := s.TouchedFiles(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []TouchedFile{
		{Path: "editado.go", Kind: "editado", Count: 1, LastAt: t0.Add(5 * time.Minute)},
		{Path: "ambos.go", Kind: "editado", Count: 2, LastAt: t0.Add(2 * time.Minute)},
		{Path: "lido.go", Kind: "lido", Count: 1, LastAt: t0.Add(9 * time.Minute)},
		{Path: "velho.go", Kind: "lido", Count: 1, LastAt: t0.Add(3 * time.Minute)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].Kind != want[i].Kind || got[i].Count != want[i].Count || !got[i].LastAt.Equal(want[i].LastAt) {
			t.Fatalf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if none, err := s.TouchedFiles(999); err != nil || len(none) != 0 {
		t.Fatalf("none = %+v, %v", none, err)
	}
}

func TestSettingsDefaultsForNewKeys(t *testing.T) {
	s := newTestStore(t)
	st, err := s.GetSettings()
	if err != nil || !st.AgentCreatesCards || !st.InjectPausedCards {
		t.Fatalf("padrões = %+v, err = %v", st, err)
	}
	// Banco antigo: grava só chaves antigas, sem as novas.
	if _, err := s.DB.Exec(`DELETE FROM settings WHERE key IN ('agent_creates_cards','inject_paused_cards')`); err != nil {
		t.Fatal(err)
	}
	if st, err = s.GetSettings(); err != nil || !st.AgentCreatesCards || !st.InjectPausedCards {
		t.Fatalf("sem as chaves = %+v, err = %v", st, err)
	}
	st.AgentCreatesCards, st.InjectPausedCards = false, false
	if err := s.PutSettings(st); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.AgentCreatesCards || got.InjectPausedCards {
		t.Fatalf("PutSettings(false) = %+v", got)
	}
}
