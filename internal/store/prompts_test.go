package store

import (
	"errors"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func TestSeedsOnFirstOpenOnly(t *testing.T) {
	path := t.TempDir() + "/c.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := s.ListPrompts()
	if len(ps) != 5 {
		t.Fatalf("seeds = %d", len(ps))
	}
	if err := s.DeletePrompt("especificar"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, _ = Open(path)
	defer s.Close()
	if ps, _ = s.ListPrompts(); len(ps) != 4 {
		t.Fatalf("reabrir re-semeou: %d", len(ps))
	}
	d, err := s.DefaultHandoffPrompt()
	if err != nil || d.Slug != "retomar-card" {
		t.Fatalf("default = %+v, %v", d, err)
	}
	g, _ := s.GetPrompt("revisar-pr")
	if len(g.Agents) != 2 || g.ProjectID != nil {
		t.Fatalf("%+v", g)
	}
}

func TestPromptCRUDAndDefaultExclusive(t *testing.T) {
	s := newTestStore(t)
	p, err := s.CreatePrompt(domain.Prompt{Name: "X", Slug: "x", Tag: "Spec", Body: "b", IsDefaultHandoff: true})
	if err != nil || p.ID == 0 {
		t.Fatal(err)
	}
	d, _ := s.DefaultHandoffPrompt()
	if d.Slug != "x" {
		t.Fatalf("default = %s", d.Slug)
	}
	if n := countDefaults(t, s); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
	r, _ := s.GetPrompt("retomar-card")
	r.IsDefaultHandoff = true
	r.Body = "novo"
	if err := s.UpdatePrompt(r); err != nil {
		t.Fatal(err)
	}
	if d, _ = s.DefaultHandoffPrompt(); d.Slug != "retomar-card" || d.Body != "novo" {
		t.Fatalf("%+v", d)
	}
	if n := countDefaults(t, s); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	if err := s.MarkPromptUsed("x", at); err != nil {
		t.Fatal(err)
	}
	g, _ := s.GetPrompt("x")
	if g.Uses != 1 || g.LastUsedAt == nil || !g.LastUsedAt.Equal(at) {
		t.Fatalf("%+v", g)
	}
	if err := s.DeletePrompt("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPrompt("x"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.UpdatePrompt(domain.Prompt{Slug: "nada"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.MarkPromptUsed("nada", at); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func countDefaults(t *testing.T, s *Store) int {
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM prompts WHERE is_default_handoff=1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
