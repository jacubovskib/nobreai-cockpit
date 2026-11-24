package store

import (
	"errors"
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
	s.SetPlan(c.ID, "docs/p.md", []string{"A"}, "s1", t0)
	s.SetTaskStatus(c.ID, 1, domain.TaskDone, "s1", t0)
	p, err := s.SetPlan(c.ID, "docs/p.md", []string{"A", "A"}, "s1", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	checkTasks(t, p.Tasks, []tk{{1, "A", domain.TaskDone, t0}, {2, "A", domain.TaskTodo, t0.Add(time.Hour)}})
}

func TestSetPlanInvalid(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	for _, titles := range [][]string{nil, {"A", ""}, {" A"}} {
		if _, err := s.SetPlan(c.ID, "docs/p.md", titles, "s", time.Now()); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v", titles, err)
		}
	}
}

func TestSetTaskStatusUnknown(t *testing.T) {
	s := newTestStore(t)
	c := newCard(t, s)
	s.SetPlan(c.ID, "docs/p.md", []string{"A", "B", "C"}, "s", time.Now())
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
	s.SetPlan(c.ID, "docs/p.md", []string{"A", "B"}, "s", time.Now())
	ct, err = s.CardContent(c.ID)
	if err != nil || ct.Plan == nil || ct.Plan.Path != "docs/p.md" || len(ct.Plan.Tasks) != 2 || ct.Plan.Tasks[1].N != 2 {
		t.Fatalf("plan = %+v, %v", ct.Plan, err)
	}
}
