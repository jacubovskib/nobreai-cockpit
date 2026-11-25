package store

import (
	"reflect"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func ptrF(f float64) *float64 { return &f }

func TestCardHierarchyRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	epico, err := s.CreateCard(domain.Card{ProjectID: p.ID, Title: "e", Type: domain.TypeEpico})
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.CreateCard(domain.Card{ProjectID: p.ID, Title: "h", Type: domain.TypeHistoria,
		ParentID: &epico.ID, Priority: domain.PrioAlta, Points: ptrF(0.5), Labels: []string{"front", "api"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCard(h.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != domain.TypeHistoria || got.ParentID == nil || *got.ParentID != epico.ID ||
		got.Priority != domain.PrioAlta || got.Points == nil || *got.Points != 0.5 ||
		!reflect.DeepEqual(got.Labels, []string{"front", "api"}) {
		t.Fatalf("round trip: %+v", got)
	}
	got.Labels, got.Points = []string{"api"}, nil
	if err := s.UpdateCard(got); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCard(h.Key)
	if got.Points != nil || !reflect.DeepEqual(got.Labels, []string{"api"}) {
		t.Fatalf("após update: %+v", got)
	}
}

func TestCardDefaults(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	c, _ := s.CreateCard(domain.Card{ProjectID: p.ID, Title: "x"})
	got, _ := s.GetCard(c.Key)
	if got.Type != domain.TypeTarefa || got.Priority != domain.PrioMedia || got.ParentID != nil || len(got.Labels) != 0 || got.Points != nil {
		t.Fatalf("padrões: %+v", got)
	}
}

func TestListChildren(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("a", "/w/a")
	p2, _ := s.CreateProject("b", "/w/b")
	e, _ := s.CreateCard(domain.Card{ProjectID: p1.ID, Title: "e", Type: domain.TypeEpico})
	a, _ := s.CreateCard(domain.Card{ProjectID: p1.ID, Title: "a", ParentID: &e.ID})
	b, _ := s.CreateCard(domain.Card{ProjectID: p2.ID, Title: "b", ParentID: &e.ID})
	s.CreateCard(domain.Card{ProjectID: p1.ID, Title: "solta"})
	kids, err := s.ListChildren(e.ID)
	if err != nil || len(kids) != 2 || kids[0].ID != a.ID || kids[1].ID != b.ID {
		t.Fatalf("filhas: %+v err=%v", kids, err)
	}
	if n, err := s.CountChildren(e.ID); err != nil || n != 2 {
		t.Fatalf("count = %d err=%v", n, err)
	}
}

func TestIntegrationCandidatesSkipEpics(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("a", "/w/a")
	e, _ := s.CreateCard(domain.Card{ProjectID: p.ID, Title: "e", Type: domain.TypeEpico, Column: domain.Revisao, Branch: "feat/e"})
	t1, _ := s.CreateCard(domain.Card{ProjectID: p.ID, Title: "t", Column: domain.Revisao, Branch: "feat/t"})
	got, err := s.ListIntegrationCandidates()
	if err != nil || len(got) != 1 || got[0].ID != t1.ID {
		t.Fatalf("candidatos (épico %d): %+v err=%v", e.ID, got, err)
	}
}

func TestMigrationKeepsLegacyCards(t *testing.T) {
	path := t.TempDir() + "/cockpit.db"
	s, err := openUpTo(path, 22)
	if err != nil {
		t.Fatalf("abrir v22: %v", err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO projects(name, path) VALUES ('a','/a')`)
	for i, col := range domain.Columns {
		exec(`INSERT INTO cards(key, project_id, title, col, branch, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			"NAC-"+string(rune('1'+i)), 1, "c", string(col), "feat/"+string(col), "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
	}
	exec(`UPDATE cards SET blocked_by='NAC-9' WHERE key='NAC-2'`)
	exec(`UPDATE cards SET pause_reason='limite de custo' WHERE key='NAC-3'`)
	if err := s.migrateUpTo(1 << 30); err != nil {
		t.Fatalf("migrar: %v", err)
	}
	defer s.Close()
	cards, err := s.ListCards(CardFilter{})
	if err != nil || len(cards) != len(domain.Columns) {
		t.Fatalf("cards: %d err=%v", len(cards), err)
	}
	for i, c := range cards {
		if c.Column != domain.Columns[i] || c.Branch != "feat/"+string(domain.Columns[i]) {
			t.Errorf("%s: coluna/branch mudou: %+v", c.Key, c)
		}
		if c.Type != domain.TypeTarefa || c.Priority != domain.PrioMedia || c.ParentID != nil || c.Points != nil || len(c.Labels) != 0 {
			t.Errorf("%s: padrões: %+v", c.Key, c)
		}
	}
	if cards[1].BlockedBy != "NAC-9" || cards[2].PauseReason != "limite de custo" {
		t.Errorf("bloqueio/pausa perdidos: %+v %+v", cards[1], cards[2])
	}
}
