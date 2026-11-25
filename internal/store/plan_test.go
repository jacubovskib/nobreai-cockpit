package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

type tk struct {
	n      int
	title  string
	status domain.TaskStatus
	at     time.Time
}

func checkTasks(t *testing.T, got []domain.PlanTask, want []tk) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tarefas = %+v, quer %+v", got, want)
	}
	for i, w := range want {
		g := got[i]
		if g.N != w.n || g.Title != w.title || g.Status != w.status || !g.UpdatedAt.Equal(w.at) {
			t.Fatalf("tarefa %d = %+v, quer %+v", i, g, w)
		}
	}
}

func TestSetPlanReplacesByTitle(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	if _, err := s.SetPlan(c.ID, "docs/p.md", []string{"A", "B", "C"}, "s1", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTaskStatus(c.ID, 2, domain.TaskDone, "s1", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, err := s.SetPlan(c.ID, "docs/p.md", []string{"A", "X", "B"}, "s2", t0.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := []tk{{1, "A", domain.TaskTodo, t0}, {2, "X", domain.TaskTodo, t0.Add(2 * time.Hour)}, {3, "B", domain.TaskDone, t0.Add(time.Hour)}}
	checkTasks(t, p.Tasks, want)
	ct, err := s.CardContent(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkTasks(t, ct.Plan.Tasks, want)
	last, err := s.LastPlanChange(c.ID)
	if err != nil || last == nil || !last.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("last = %v, %v", last, err)
	}
}

func TestSetPlanDuplicateTitles(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	if _, err := s.SetPlan(c.ID, "docs/p.md", []string{"A"}, "s1", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTaskStatus(c.ID, 1, domain.TaskDone, "s1", t0); err != nil {
		t.Fatal(err)
	}
	p, err := s.SetPlan(c.ID, "docs/p.md", []string{"A", "A"}, "s1", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	checkTasks(t, p.Tasks, []tk{{1, "A", domain.TaskDone, t0}, {2, "A", domain.TaskTodo, t0.Add(time.Hour)}})
}

func TestSetPlanInvalid(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	for _, titles := range [][]string{nil, {"A", ""}, {"A", "  "}} {
		if _, err := s.SetPlan(c.ID, "docs/p.md", titles, "s", time.Now()); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v", titles, err)
		}
	}
}

func TestSetPlanTrimsTitles(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	p, err := s.SetPlan(c.ID, "docs/p.md", []string{" A ", "\tB\n"}, "s", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tasks) != 2 || p.Tasks[0].Title != "A" || p.Tasks[1].Title != "B" {
		t.Fatalf("tasks = %+v", p.Tasks)
	}
}

func TestSetTaskStatusUnknown(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	if _, err := s.SetPlan(c.ID, "docs/p.md", []string{"A", "B", "C"}, "s", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTaskStatus(c.ID, 9, domain.TaskDone, "s", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCardContentPlan(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	ct, err := s.CardContent(c.ID)
	if err != nil || ct.Plan != nil {
		t.Fatalf("sem tarefas: %+v, %v", ct.Plan, err)
	}
	if last, _ := s.LastPlanChange(c.ID); last != nil {
		t.Fatalf("last = %v", last)
	}
	if _, err := s.SetPlan(c.ID, "docs/p.md", []string{"A", "B"}, "s", time.Now()); err != nil {
		t.Fatal(err)
	}
	ct, err = s.CardContent(c.ID)
	if err != nil || ct.Plan == nil || ct.Plan.Path != "docs/p.md" || len(ct.Plan.Tasks) != 2 || ct.Plan.Tasks[1].N != 2 {
		t.Fatalf("plan = %+v, %v", ct.Plan, err)
	}
}

func TestSessionCommittedAfter(t *testing.T) {
	s := newTestStore(t)
	if err := s.StartSession(newSession("claude:1", 10)); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	add := func(data string, at time.Time) {
		t.Helper()
		if err := s.AddEvent(domain.Event{SessionID: "claude:1", Kind: domain.EvCommand, Data: []byte(data), At: at}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(after time.Time, want bool) {
		t.Helper()
		got, err := s.SessionCommittedAfter("claude:1", after)
		if err != nil || got != want {
			t.Fatalf("SessionCommittedAfter(%v) = %v, %v; quer %v", after, got, err, want)
		}
	}
	add(`{"command":"git commit -m x","exit_code":1}`, t0.Add(time.Minute))
	add(`{"command":"git commit -m x && nobre task 2 concluida"}`, t0.Add(time.Minute))
	add(`{"command":"git log --grep commit"}`, t0.Add(time.Minute))
	check(t0, false) // falhou, mudou a tarefa no mesmo comando, não é commit
	add(`{"command":"git commit -m x","exit_code":0}`, t0.Add(2*time.Minute))
	check(t0, true)
	check(t0.Add(2*time.Minute), false) // no mesmo instante não é "depois"
	add(`{"command":"rtk git commit -m y"}`, t0.Add(3*time.Minute))
	check(t0.Add(2*time.Minute), true)
	check(t0.Add(3*time.Minute), false)
	// A mensagem que só cita os comandos conta como commit.
	add(`{"command":"git commit -m \"feat(cli): nobre plan e nobre task (NAC-5)\""}`, t0.Add(4*time.Minute))
	check(t0.Add(3*time.Minute), true)
	add(`{"command":"cd /w/api && rtk nobre plan docs/p.md --task A"}`, t0.Add(5*time.Minute))
	add(`{"command":"git commit -m y; ./bin/nobre task 1 concluida"}`, t0.Add(5*time.Minute))
	check(t0.Add(4*time.Minute), false)
}

func TestSessionEditedPathsAndReminders(t *testing.T) {
	s := newTestStore(t)
	if err := s.StartSession(newSession("claude:1", 10)); err != nil {
		t.Fatal(err)
	}
	cardA := mustCard(t, s, "A")
	cardB := mustCard(t, s, "B")
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	edits := []struct {
		path string
		card *int64
	}{{"b.go", nil}, {"a.go", nil}, {"b.go", nil}, {"docs/plan.md", nil},
		{"docs/plan-a.md", &cardA}, {"x.go", &cardA}, {"docs/plan-b.md", &cardB}}
	for i, e := range edits {
		if err := s.AddEvent(domain.Event{SessionID: "claude:1", CardID: e.card, Kind: domain.EvFileEdit,
			Data: []byte(`{"path":"` + e.path + `"}`), At: t0.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	// Só as edições feitas com a sessão naquele card (ou sem card).
	for _, c := range []struct {
		card *int64
		want string
	}{{nil, "b.go,a.go,docs/plan.md"}, {&cardA, "docs/plan-a.md,x.go"}, {&cardB, "docs/plan-b.md"}} {
		got, err := s.SessionEditedPaths("claude:1", c.card)
		if err != nil || strings.Join(got, ",") != c.want {
			t.Fatalf("SessionEditedPaths(%v) = %v, %v; want %s", c.card, got, err, c.want)
		}
	}
	// O lembrete do plano vale por card: avisar no A não cala o B nem o sem card.
	if r, err := s.SessionPlanReminded("claude:1", &cardA); err != nil || r {
		t.Fatalf("SessionPlanReminded = %v, %v", r, err)
	}
	if err := s.MarkPlanReminded("claude:1", &cardA); err != nil {
		t.Fatal(err)
	}
	if r, err := s.SessionPlanReminded("claude:1", &cardA); err != nil || !r {
		t.Fatalf("SessionPlanReminded(A) depois de marcar = %v, %v", r, err)
	}
	for _, other := range []*int64{&cardB, nil} {
		if r, err := s.SessionPlanReminded("claude:1", other); err != nil || r {
			t.Fatalf("SessionPlanReminded(%v) depois de marcar A = %v, %v", other, r, err)
		}
	}
	if err := s.MarkPlanReminded("claude:1", nil); err != nil {
		t.Fatal(err)
	}
	if r, err := s.SessionPlanReminded("claude:1", nil); err != nil || !r {
		t.Fatalf("SessionPlanReminded(sem card) = %v, %v", r, err)
	}
	if v, err := s.TaskRemindedFor("claude:1"); err != nil || v != "" {
		t.Fatalf("TaskRemindedFor = %q, %v", v, err)
	}
	if err := s.SetTaskRemindedFor("claude:1", FmtTime(t0)); err != nil {
		t.Fatal(err)
	}
	if v, err := s.TaskRemindedFor("claude:1"); err != nil || v != FmtTime(t0) {
		t.Fatalf("TaskRemindedFor = %q, %v", v, err)
	}
	if err := s.MarkPlanReminded("claude:x", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkPlanReminded inexistente = %v", err)
	}
}

func mustCard(t *testing.T, s *Store, title string) int64 {
	t.Helper()
	p, err := s.CreateProject(title, "/w/"+title)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCard(domain.Card{Title: title, ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}
