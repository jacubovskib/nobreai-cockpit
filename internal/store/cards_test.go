package store

import (
	"errors"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateCardKeys(t *testing.T) {
	s := newTestStore(t)
	p, err := s.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	c1, err := s.CreateCard(domain.Card{Title: "um", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.CreateCard(domain.Card{Title: "dois", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if c1.Key != "NAC-1" || c2.Key != "NAC-2" {
		t.Fatalf("keys = %s, %s", c1.Key, c2.Key)
	}
	if c1.Column != domain.Backlog {
		t.Fatalf("column = %s", c1.Column)
	}
	got, err := s.GetCard("NAC-2")
	if err != nil || got.Title != "dois" || got.ProjectID != p.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := s.GetCard("NAC-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateMoveListCards(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("a", "/w/a")
	p2, _ := s.CreateProject("b", "/w/b")
	c1, _ := s.CreateCard(domain.Card{Title: "x", ProjectID: p1.ID})
	s.CreateCard(domain.Card{Title: "y", ProjectID: p2.ID})

	c1.Goal = "meta"
	c1.WhereStopped = "aqui"
	if err := s.UpdateCard(c1); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCard(c1.Key)
	if got.Goal != "meta" || got.WhereStopped != "aqui" {
		t.Fatalf("update = %+v", got)
	}
	if err := s.MoveCard(c1.Key, domain.Revisao); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCard(c1.Key)
	if got.Column != domain.Revisao {
		t.Fatalf("column = %s", got.Column)
	}
	if err := s.MoveCard("NAC-99", domain.Revisao); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	all, _ := s.ListCards(CardFilter{})
	if len(all) != 2 {
		t.Fatalf("all = %d", len(all))
	}
	byProj, _ := s.ListCards(CardFilter{ProjectID: p2.ID})
	if len(byProj) != 1 || byProj[0].Title != "y" {
		t.Fatalf("byProj = %+v", byProj)
	}
	s.DB.Exec(`INSERT INTO sessions(id, agent, card_id, started_at, last_event_at) VALUES ('s1','claude',?, 'x','x')`, c1.ID)
	byAgent, err := s.ListCards(CardFilter{Agent: "claude"})
	if err != nil || len(byAgent) != 1 || byAgent[0].ID != c1.ID {
		t.Fatalf("byAgent = %+v, %v", byAgent, err)
	}
}

func TestProjectForPath(t *testing.T) {
	s := newTestStore(t)
	s.CreateProject("api", "/w/api")
	s.CreateProject("sub", "/w/api/sub")
	p, err := s.ProjectForPath("/w/api/sub/x")
	if err != nil || p == nil || p.Name != "sub" {
		t.Fatalf("got %+v, %v", p, err)
	}
	p, err = s.ProjectForPath("/w/api")
	if err != nil || p == nil || p.Name != "api" {
		t.Fatalf("exact: %+v, %v", p, err)
	}
	p, err = s.ProjectForPath("/w/apix")
	if err != nil || p != nil {
		t.Fatalf("apix: %+v, %v", p, err)
	}
}

func TestProjectCRUD(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	if !p.Capture {
		t.Fatal("capture padrão deveria ser true")
	}
	p.Name = "novo"
	p.Capture = false
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	l, _ := s.ListProjects()
	if len(l) != 1 || l[0].Name != "novo" || l[0].Capture {
		t.Fatalf("list = %+v", l)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	l, _ = s.ListProjects()
	if len(l) != 0 {
		t.Fatalf("list = %+v", l)
	}
}

func TestGetCardByID(t *testing.T) {
	s := newTestStore(t)
	p, err := s.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCard(domain.Card{Title: "t", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCardByID(c.ID)
	if err != nil || got.Key != c.Key {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := s.GetCardByID(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetCardBranchResetsMarks(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	c, err := s.CreateCard(domain.Card{Title: "x", ProjectID: p.ID, Branch: "feat/x"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SetBranchHead(c.ID, "feat/x", "abc", true); err != nil || !ok {
		t.Fatalf("SetBranchHead = %v, %v", ok, err)
	}
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if ok, err := s.MarkBranchMerged(c.ID, "feat/x", at); err != nil || !ok {
		t.Fatalf("MarkBranchMerged = %v, %v", ok, err)
	}
	if ok, err := s.MarkBranchMerged(c.ID, "feat/x", at); err != nil || ok {
		t.Fatalf("2ª marca = %v, %v", ok, err)
	}
	if ok, err := s.SetBranchHead(c.ID, "feat/outro", "def", true); err != nil || ok {
		t.Fatalf("head em outro branch = %v, %v", ok, err)
	}
	got, _ := s.GetCardByID(c.ID)
	if got.BranchHead != "abc" || !got.BranchDiverged || got.BranchMergedAt == nil || !got.BranchMergedAt.Equal(at) {
		t.Fatalf("marcas = %+v", got)
	}
	if ch, err := s.SetCardBranch(c.ID, "feat/x"); err != nil || ch {
		t.Fatalf("mesmo branch = %v, %v", ch, err)
	}
	if got, _ = s.GetCardByID(c.ID); got.BranchHead != "abc" {
		t.Fatal("mesmo branch não deveria zerar")
	}
	if ch, err := s.SetCardBranch(c.ID, "feat/y"); err != nil || !ch {
		t.Fatalf("novo branch = %v, %v", ch, err)
	}
	got, _ = s.GetCardByID(c.ID)
	if got.Branch != "feat/y" || got.BranchHead != "" || got.BranchDiverged || got.BranchMergedAt != nil {
		t.Fatalf("marcas não zeradas: %+v", got)
	}
}

func TestPauseForLimitOnlyFromInProgress(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.EmAndamento)
	ok, err := s.PauseForLimit(c.ID, time.Now())
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	got, _ := s.GetCard(c.Key)
	if got.Column != domain.Pausado || got.PauseReason != "limite de custo" {
		t.Fatalf("card = %+v", got)
	}
	if ok, _ := s.PauseForLimit(c.ID, time.Now()); ok {
		t.Fatal("segunda chamada não deveria mudar")
	}
}

func TestResumeFromLimitOnlyWhenPausedByLimit(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Pausado)
	if ok, err := s.ResumeFromLimit(c.ID, time.Now()); err != nil || ok {
		t.Fatalf("pausado sem motivo: ok = %v, err = %v", ok, err)
	}
	c2 := seedCard(t, s, domain.EmAndamento)
	if _, err := s.PauseForLimit(c2.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	ok, err := s.ResumeFromLimit(c2.ID, time.Now())
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	got, _ := s.GetCard(c2.Key)
	if got.Column != domain.EmAndamento || got.PauseReason != "" {
		t.Fatalf("card = %+v", got)
	}
}

func TestAttachClearsCardFlags(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.EmAndamento)
	for _, id := range []string{"claude:a", "claude:b"} {
		if err := s.StartSession(newSession(id, 1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PauseForLimit(c.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHandoffIncomplete(c.ID, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:b", c.Key, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCard(c.Key)
	if got.Column != domain.EmAndamento || got.PauseReason != "" || got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
}

func TestUpdateCardPersistsFlags(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	c.BlockedBy, c.HandoffIncomplete = "NAC-9", true
	if err := s.UpdateCard(c); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCard(c.Key)
	if got.BlockedBy != "NAC-9" || !got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
}

func TestResumeFromLimitClearsAllMarks(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.EmAndamento)
	if _, err := s.PauseForLimit(c.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCard(c.Key)
	got.HandoffIncomplete, got.BlockedBy = true, "NAC-9"
	if err := s.UpdateCard(got); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ResumeFromLimit(c.ID, time.Now()); err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	got, _ = s.GetCard(c.Key)
	if got.PauseReason != "" || got.HandoffIncomplete || got.BlockedBy != "" {
		t.Fatalf("card = %+v", got)
	}
}

func TestSetHandoffIncompleteBumpsUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Pausado)
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := s.SetHandoffIncomplete(c.ID, true, at); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCard(c.Key)
	if !got.HandoffIncomplete || !got.UpdatedAt.Equal(at) {
		t.Fatalf("card = %+v", got)
	}
}
