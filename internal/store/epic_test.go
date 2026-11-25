package store

import (
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// epicCol lê a coluna atual do card pela chave.
func epicCol(t *testing.T, s *Store, key string) domain.Column {
	t.Helper()
	c, err := s.GetCard(key)
	if err != nil {
		t.Fatal(err)
	}
	return c.Column
}

func wantCol(t *testing.T, s *Store, key string, want domain.Column, step string) {
	t.Helper()
	if got := epicCol(t, s, key); got != want {
		t.Fatalf("%s: %s em %s, quer %s", step, key, got, want)
	}
}

// newEpic cria um épico no projeto.
func newEpic(t *testing.T, s *Store, projectID int64) domain.Card {
	t.Helper()
	e, err := s.CreateCard(domain.Card{ProjectID: projectID, Title: "épico", Type: domain.TypeEpico})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func newChild(t *testing.T, s *Store, projectID, epicID int64, col domain.Column) domain.Card {
	t.Helper()
	c, err := s.CreateCard(domain.Card{ProjectID: projectID, Title: "filha", ParentID: &epicID, Column: col})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func startSession(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.StartSession(newSession(id, 10)); err != nil {
		t.Fatal(err)
	}
}

func TestEpicFollowsChildren(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("api", "/w/api")
	p2, _ := s.CreateProject("web", "/w/web")
	e := newEpic(t, s, p1.ID)
	a := newChild(t, s, p1.ID, e.ID, "")
	b := newChild(t, s, p2.ID, e.ID, "")
	wantCol(t, s, e.Key, domain.Backlog, "filhas no backlog")

	startSession(t, s, "claude:a")
	if err := s.AttachSession("claude:a", a.Key, false); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, a.Key, domain.EmAndamento, "attach")
	wantCol(t, s, e.Key, domain.EmAndamento, "attach")

	// Deixa o épico desatualizado (gravação manual crua) para que o fim da
	// sessão precise recalculá-lo.
	if err := s.MoveCard(e.Key, domain.Backlog); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession("claude:a", "fim", time.Now()); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, a.Key, domain.Pausado, "fim da sessão")
	wantCol(t, s, e.Key, domain.EmAndamento, "fim da sessão")

	if err := s.MoveCard(a.Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveCard(b.Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e.Key, domain.Revisao, "filhas concluídas")

	if err := s.MoveCard(e.Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e.Key, domain.Concluido, "épico concluído à mão")

	if err := s.MoveCard(a.Key, domain.Revisao); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e.Key, domain.EmAndamento, "filha reaberta")
}

func TestEpicNewChildRecalc(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	e := newEpic(t, s, p.ID)
	newChild(t, s, p.ID, e.ID, domain.Concluido)
	wantCol(t, s, e.Key, domain.Revisao, "filha criada concluída")
	if err := s.MoveCard(e.Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	newChild(t, s, p.ID, e.ID, domain.Backlog)
	wantCol(t, s, e.Key, domain.EmAndamento, "nova filha no backlog")

	// Épico concluído sem filhas recebe a primeira, no backlog: volta ao backlog.
	vazio := newEpic(t, s, p.ID)
	if err := s.MoveCard(vazio.Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	newChild(t, s, p.ID, vazio.ID, domain.Backlog)
	wantCol(t, s, vazio.Key, domain.Backlog, "épico concluído recebe filha no backlog")

	e2 := newEpic(t, s, p.ID)
	startSession(t, s, "claude:n")
	if _, err := s.CreateCardAndAttach(domain.Card{ProjectID: p.ID, Title: "f", ParentID: &e2.ID}, "claude:n"); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e2.Key, domain.EmAndamento, "filha criada e ligada")

	e3, err := s.CreateCard(domain.Card{ProjectID: p.ID, Title: "e", Type: domain.TypeEpico, Column: domain.Pausado})
	if err != nil {
		t.Fatal(err)
	}
	if e3.Column != domain.Backlog {
		t.Fatalf("épico criado em pausado: devolvido em %s", e3.Column)
	}
	wantCol(t, s, e3.Key, domain.Backlog, "épico criado em pausado")
}

func TestEpicChildChangesParent(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	e1 := newEpic(t, s, p.ID)
	e2 := newEpic(t, s, p.ID)
	c := newChild(t, s, p.ID, e1.ID, "")
	startSession(t, s, "claude:c")
	if err := s.AttachSession("claude:c", c.Key, false); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e1.Key, domain.EmAndamento, "attach")

	got, err := s.GetCard(c.Key)
	if err != nil {
		t.Fatal(err)
	}
	got.ParentID = &e2.ID
	if err := s.UpdateCard(got); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e1.Key, domain.Backlog, "épico antigo sem filhas")
	wantCol(t, s, e2.Key, domain.EmAndamento, "épico novo")
}

func TestEpicIgnoresOwnSession(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	e := newEpic(t, s, p.ID)
	newChild(t, s, p.ID, e.ID, domain.Concluido)
	wantCol(t, s, e.Key, domain.Revisao, "filha concluída")

	startSession(t, s, "claude:e")
	if err := s.AttachSession("claude:e", e.Key, false); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e.Key, domain.Revisao, "attach no épico")
	if err := s.EndSession("claude:e", "fim", time.Now()); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e.Key, domain.Revisao, "fim da sessão do épico")
}

func TestEpicIgnoresLimitsAndIntegration(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	e := newEpic(t, s, p.ID)
	a := newChild(t, s, p.ID, e.ID, "")
	startSession(t, s, "claude:a")
	if err := s.AttachSession("claude:a", a.Key, false); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e.Key, domain.EmAndamento, "attach")

	now := time.Now()
	if changed, err := s.PauseForLimit(e.ID, now); err != nil || changed {
		t.Fatalf("PauseForLimit(épico) = %v, %v", changed, err)
	}
	wantCol(t, s, e.Key, domain.EmAndamento, "limite no épico")

	// O épico em revisão com um branch que entra na base não é concluído.
	if _, err := s.SetCardBranch(e.ID, "feat/e"); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveCard(e.Key, domain.Revisao); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordIntegration(e.ID, "feat/e", now, domain.Decision{What: "merge"}); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, e.Key, domain.Revisao, "integração no épico")
}

func TestEpicChildLimitsAndIntegration(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	e := newEpic(t, s, p.ID)
	a := newChild(t, s, p.ID, e.ID, "")
	startSession(t, s, "claude:a")
	if err := s.AttachSession("claude:a", a.Key, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	// Épico desatualizado: cada caminho de limite precisa recalculá-lo.
	steps := []struct {
		name string
		run  func() (bool, error)
	}{
		{"PauseForLimit", func() (bool, error) { return s.PauseForLimit(a.ID, now) }},
		{"ResumeFromLimit", func() (bool, error) { return s.ResumeFromLimit(a.ID, now) }},
		{"PauseForLimit de novo", func() (bool, error) { return s.PauseForLimit(a.ID, now) }},
		{"DoneFromLimit", func() (bool, error) { return s.DoneFromLimit(a.ID, now) }},
	}
	for _, st := range steps {
		if err := s.MoveCard(e.Key, domain.Backlog); err != nil {
			t.Fatal(err)
		}
		if changed, err := st.run(); err != nil || !changed {
			t.Fatalf("%s(filha) = %v, %v", st.name, changed, err)
		}
		wantCol(t, s, e.Key, domain.EmAndamento, st.name)
	}
	wantCol(t, s, a.Key, domain.Revisao, "DoneFromLimit")

	if _, err := s.SetCardBranch(a.ID, "feat/a"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.RecordIntegration(a.ID, "feat/a", now, domain.Decision{What: "merge"}); err != nil || !ok {
		t.Fatalf("RecordIntegration(filha) = %v, %v", ok, err)
	}
	wantCol(t, s, a.Key, domain.Concluido, "integração na filha")
	wantCol(t, s, e.Key, domain.Revisao, "integração na filha")
}

func TestBecomeEpicRecalc(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	c, err := s.CreateCard(domain.Card{ProjectID: p.ID, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	c.Column, c.PauseReason, c.BlockedBy, c.Branch = domain.Pausado, "motivo", "NAC-9", "feat/t"
	if err := s.UpdateCard(c); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, c.Key, domain.Pausado, "tarefa pausada")

	c.Type, c.PauseReason, c.BlockedBy, c.Branch = domain.TypeEpico, "", "", ""
	if err := s.UpdateCard(c); err != nil {
		t.Fatal(err)
	}
	wantCol(t, s, c.Key, domain.Backlog, "virou épico")
}
