package resume

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// recGit guarda os argumentos do DiffStat.
type recGit struct{ dir, since string }

func (g *recGit) Branch(string) string { return "main" }
func (g *recGit) Head(string) string   { return "" }
func (g *recGit) DiffStat(dir, since string) string {
	g.dir, g.since = dir, since
	return " a.go | 1 +"
}
func (g *recGit) Clean(string) bool             { return true }
func (g *recGit) Checkout(string, string) error { return nil }

func TestForCardDiffSincePreviousSession(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, _ := st.CreateProject("api", "/w/api")
	c, _ := st.CreateCard(domain.Card{Title: "OAuth", ProjectID: p.ID, WhereStopped: "callback"})
	t0 := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for i, x := range []domain.Session{
		{ID: "claude:old", Agent: "claude", GitHead: "head-old", StartedAt: t0},
		{ID: "claude:prev", Agent: "claude", GitHead: "head-prev", StartedAt: t0.Add(time.Hour)},
		{ID: "claude:cur", Agent: "claude", GitHead: "head-cur", StartedAt: t0.Add(2 * time.Hour)},
	} {
		if err := st.StartSession(x); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if err := st.AttachSession(x.ID, c.Key, false); err != nil {
				t.Fatal(err)
			}
			if err := st.EndSession(x.ID, "fim", t0.Add(time.Duration(i)*time.Hour+time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := st.AttachSession("claude:cur", c.Key, false); err != nil {
		t.Fatal(err)
	}
	c, _ = st.GetCard(c.Key)

	g := &recGit{}
	md, err := ForCard(st, g, c, "claude:cur")
	if err != nil {
		t.Fatal(err)
	}
	if g.dir != "/w/api" || g.since != "head-prev" {
		t.Fatalf("DiffStat(%q, %q)", g.dir, g.since)
	}
	if !strings.Contains(md, "## Onde parei\ncallback") || !strings.Contains(md, "a.go | 1 +") || !strings.Contains(md, "3 sessões") {
		t.Fatalf("pacote:\n%s", md)
	}
	// Sem exclusão: a mais recente do card.
	if _, err := ForCard(st, g, c, ""); err != nil || g.since != "head-cur" {
		t.Fatalf("since = %q err = %v", g.since, err)
	}
	// Sem git: sem diff, sem erro.
	if md, err := ForCard(st, nil, c, ""); err != nil || !strings.Contains(md, "sem mudanças registradas") {
		t.Fatalf("%v\n%s", err, md)
	}
}

func TestForCardIncludesEpicAndChildren(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, _ := st.CreateProject("api", "/w/api")
	epic, err := st.CreateCard(domain.Card{Title: "Checkout novo", ProjectID: p.ID, Type: domain.TypeEpico, Goal: "Pagar sem sair"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := st.CreateCard(domain.Card{Title: "Pagar com Pix", ProjectID: p.ID, Type: domain.TypeHistoria, ParentID: &epic.ID})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ForCard(st, nil, child, "")
	if err != nil || !strings.Contains(out, "Épico: "+epic.Key+" · Checkout novo — Pagar sem sair") {
		t.Fatalf("filha: %v\n%s", err, out)
	}
	out, err = ForCard(st, nil, epic, "")
	if err != nil || !strings.Contains(out, "## Filhas\n- "+child.Key+" · História · Pagar com Pix · Backlog") {
		t.Fatalf("épico: %v\n%s", err, out)
	}
}
