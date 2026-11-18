package store

import (
	"errors"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func mustProject(t *testing.T, s *Store, name, path string) domain.Project {
	t.Helper()
	p, err := s.CreateProject(name, path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustChild(t *testing.T, s *Store, name, path string, parent int64) domain.Project {
	t.Helper()
	p, _, err := s.CreateChild(name, path, parent)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func sessionIn(t *testing.T, s *Store, id string, pid int, project int64, cwd, worktree string, card *int64) {
	t.Helper()
	x := newSession(id, pid)
	x.Cwd, x.ProjectID, x.CardID = cwd, &project, card
	if worktree != "" {
		x.Worktree, x.WorktreeLinked = worktree, true
	}
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
}

func projectOf(t *testing.T, s *Store, session string) int64 {
	t.Helper()
	x, err := s.GetSession(session)
	if err != nil || x.ProjectID == nil {
		t.Fatalf("%s: %+v %v", session, x, err)
	}
	return *x.ProjectID
}

func TestCreateChildInsideParent(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/apps/web", loja.ID)
	if web.ParentID == nil || *web.ParentID != loja.ID {
		t.Fatalf("web = %+v", web)
	}
	ids, err := s.FamilyIDs(loja.ID)
	if err != nil || len(ids) != 2 || ids[0] != loja.ID || ids[1] != web.ID {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	if ids, _ := s.FamilyIDs(web.ID); len(ids) != 1 || ids[0] != web.ID {
		t.Fatalf("filho: %v", ids)
	}
	kids, err := s.Children(loja.ID)
	if err != nil || len(kids) != 1 || kids[0].ID != web.ID {
		t.Fatalf("kids = %v, %v", kids, err)
	}
}

func TestChildOutsideMonorepoRefused(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	for _, path := range []string{"/r/outro", "/r/loja", "/r/loja2/x"} {
		if _, _, err := s.CreateChild("x", path, loja.ID); !errors.Is(err, ErrFamily) {
			t.Fatalf("%s: err = %v", path, err)
		}
	}
	ps, _ := s.ListProjects()
	if len(ps) != 1 {
		t.Fatalf("projetos = %+v", ps)
	}
}

func TestGroupAcceptsAnyPath(t *testing.T) {
	s := newTestStore(t)
	g, err := s.CreateGroup("meu-grupo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("outro-grupo"); err != nil {
		t.Fatalf("dois grupos devem coexistir: %v", err)
	}
	if g.Path != "" {
		t.Fatalf("g = %+v", g)
	}
	var isNull bool
	s.DB.QueryRow(`SELECT path IS NULL FROM projects WHERE id=?`, g.ID).Scan(&isNull)
	if !isNull {
		t.Fatal("grupo deve gravar path NULL")
	}
	mustChild(t, s, "front", "/r/front", g.ID)
	mustChild(t, s, "back", "/r/back", g.ID)
	if ids, _ := s.FamilyIDs(g.ID); len(ids) != 3 {
		t.Fatalf("ids = %v", ids)
	}
	g.Name = "renomeado"
	if err := s.UpdateProject(g); err != nil {
		t.Fatal(err)
	}
}

func TestOneLevelOnly(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/web", loja.ID)
	if _, _, err := s.CreateChild("neto", "/r/loja/web/x", web.ID); !errors.Is(err, ErrFamily) {
		t.Fatalf("neto: %v", err)
	}
	outro := mustProject(t, s, "outro", "/o")
	pid := outro.ID
	if _, err := s.SetParent(loja.ID, &pid); !errors.Is(err, ErrFamily) {
		t.Fatalf("pai com filhos: %v", err)
	}
	if _, err := s.SetParent(outro.ID, &web.ID); !errors.Is(err, ErrFamily) {
		t.Fatalf("pai que é filho: %v", err)
	}
}

func TestDuplicateNameInFamilyRefused(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	mustChild(t, s, "web", "/r/loja/a/web", loja.ID)
	if _, _, err := s.CreateChild("web", "/r/loja/b/web", loja.ID); !errors.Is(err, ErrFamily) {
		t.Fatalf("irmão: %v", err)
	}
	if _, _, err := s.CreateChild("loja", "/r/loja/c", loja.ID); !errors.Is(err, ErrFamily) {
		t.Fatalf("nome do pai: %v", err)
	}
	outra := mustProject(t, s, "outra", "/r/outra")
	mustChild(t, s, "web", "/r/outra/web", outra.ID)
	// solto com nome repetido na família: ligar é recusado
	solto := mustProject(t, s, "web", "/r/loja/solto")
	lid := loja.ID
	if _, err := s.SetParent(solto.ID, &lid); !errors.Is(err, ErrFamily) {
		t.Fatalf("ligar duplicado: %v", err)
	}
	// renomear para um nome da família
	kids, _ := s.Children(loja.ID)
	kids[0].Name = "LOJA"
	if err := s.UpdateProject(kids[0]); !errors.Is(err, ErrFamily) {
		t.Fatalf("renomear: %v", err)
	}
}

func TestSetParentMovesHistory(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustProject(t, s, "web", "/r/loja/apps/web")
	c1, _ := s.CreateCard(domain.Card{Title: "so web", ProjectID: loja.ID})
	c2, _ := s.CreateCard(domain.Card{Title: "misto", ProjectID: loja.ID})
	c3, _ := s.CreateCard(domain.Card{Title: "sem sessao", ProjectID: loja.ID})
	sessionIn(t, s, "claude:a", 1, loja.ID, "/r/loja/apps/web/src", "", &c1.ID)
	sessionIn(t, s, "claude:b", 2, loja.ID, "/r/loja", "", &c2.ID)
	sessionIn(t, s, "claude:c", 3, loja.ID, "/r/loja/apps/web2", "", &c2.ID)
	m, err := s.SetParent(web.ID, &loja.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m != (Moved{Sessions: 1, Cards: 1}) {
		t.Fatalf("moved = %+v", m)
	}
	if projectOf(t, s, "claude:a") != web.ID || projectOf(t, s, "claude:b") != loja.ID || projectOf(t, s, "claude:c") != loja.ID {
		t.Fatal("sessões no lugar errado")
	}
	for c, want := range map[int64]int64{c1.ID: web.ID, c2.ID: loja.ID, c3.ID: loja.ID} {
		got, _ := s.GetCardByID(c)
		if got.ProjectID != want {
			t.Fatalf("card %d em %d, quer %d", c, got.ProjectID, want)
		}
	}
	// desligar não devolve nada
	m, err = s.SetParent(web.ID, nil)
	if err != nil || m != (Moved{}) {
		t.Fatalf("desligar: %+v %v", m, err)
	}
	if projectOf(t, s, "claude:a") != web.ID {
		t.Fatal("desligar devolveu sessão ao pai")
	}
}

func TestSetParentMovesWorktreeSession(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustProject(t, s, "web", "/r/loja/apps/web")
	sessionIn(t, s, "claude:w", 1, loja.ID, "/r/loja/.worktrees/x/apps/web", "/r/loja/.worktrees/x", nil)
	sessionIn(t, s, "claude:r", 2, loja.ID, "/r/loja/.worktrees/x", "/r/loja/.worktrees/x", nil)
	m, err := s.SetParent(web.ID, &loja.ID)
	if err != nil || m.Sessions != 1 {
		t.Fatalf("m = %+v, %v", m, err)
	}
	if projectOf(t, s, "claude:w") != web.ID || projectOf(t, s, "claude:r") != loja.ID {
		t.Fatal("worktree mal atribuído")
	}
}

func TestCaptureFollowsParent(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	loja.Capture = false
	if err := s.UpdateProject(loja); err != nil {
		t.Fatal(err)
	}
	web := mustProject(t, s, "web", "/r/loja/web") // captura ligada
	if _, err := s.SetParent(web.ID, &loja.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Children(loja.ID); p[0].Capture {
		t.Fatal("filho deveria adotar captura desligada")
	}
	loja.Capture = true
	if err := s.UpdateProject(loja); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Children(loja.ID); !p[0].Capture {
		t.Fatal("filho deveria seguir o pai ligado")
	}
	loja.Capture = false
	if err := s.UpdateProject(loja); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Children(loja.ID); p[0].Capture {
		t.Fatal("filho deveria seguir o pai desligado")
	}
	// desligar mantém a captura do filho
	s.SetParent(web.ID, nil)
	loja.Capture = true
	s.UpdateProject(loja)
	ps, _ := s.ListProjects()
	for _, p := range ps {
		if p.ID == web.ID && p.Capture {
			t.Fatal("filho desligado mudou de captura")
		}
	}
}

func TestDeleteParentWithChildrenRefused(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/web", loja.ID)
	if err := s.DeleteProject(loja.ID); !errors.Is(err, ErrFamily) {
		t.Fatalf("err = %v", err)
	}
	if err := s.DeleteProject(web.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(loja.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRepoProject(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/web", loja.ID)
	g, _ := s.CreateGroup("g")
	front := mustChild(t, s, "front", "/r/front", g.ID)
	if got, err := s.RepoProject(web); err != nil || got.ID != loja.ID {
		t.Fatalf("monorepo: %+v %v", got, err)
	}
	if got, _ := s.RepoProject(front); got.ID != front.ID {
		t.Fatalf("grupo: %+v", got)
	}
	if got, _ := s.RepoProject(loja); got.ID != loja.ID {
		t.Fatalf("pai: %+v", got)
	}
	if DisplayName(web, &loja) != "loja › web" || DisplayName(loja, nil) != "loja" {
		t.Fatal("DisplayName")
	}
}

func TestSetCardProjectOutsideFamilyRefused(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/web", loja.ID)
	outro := mustProject(t, s, "outro", "/o")
	c, _ := s.CreateCard(domain.Card{Title: "x", ProjectID: loja.ID})
	if err := s.SetCardProject(c.ID, outro.ID); !errors.Is(err, ErrFamily) {
		t.Fatalf("fora: %v", err)
	}
	if err := s.SetCardProject(c.ID, web.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCardProject(c.ID, loja.ID); err != nil {
		t.Fatalf("irmão/pai: %v", err)
	}
	if err := s.SetCardProject(9999, loja.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("card inexistente: %v", err)
	}
}

func TestTotalsProjectIDs(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/web", loja.ID)
	outro := mustProject(t, s, "outro", "/o")
	sessionIn(t, s, "claude:p", 1, loja.ID, "/r/loja", "", nil)
	sessionIn(t, s, "claude:f", 2, web.ID, "/r/loja/web", "", nil)
	sessionIn(t, s, "claude:o", 3, outro.ID, "/o", "", nil)
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertUsage([]domain.UsageRow{
		{SessionID: "claude:p", MessageID: "m1", Model: "m", Input: 100, At: at},
		{SessionID: "claude:f", MessageID: "m2", Model: "m", Input: 10, At: at},
		{SessionID: "claude:o", MessageID: "m3", Model: "m", Input: 1000, At: at},
	}); err != nil {
		t.Fatal(err)
	}
	sum := func(f TotalsFilter) int64 {
		ts, err := s.Totals(f)
		if err != nil {
			t.Fatal(err)
		}
		var n int64
		for _, x := range ts {
			n += x.Input
		}
		return n
	}
	fam, _ := s.FamilyIDs(loja.ID)
	if got := sum(TotalsFilter{GroupBy: "agent", ProjectIDs: fam}); got != 110 {
		t.Fatalf("família = %d", got)
	}
	if got := sum(TotalsFilter{GroupBy: "agent", ProjectIDs: []int64{web.ID}}); got != 10 {
		t.Fatalf("filho = %d", got)
	}
	s.CreateCard(domain.Card{Title: "a", ProjectID: loja.ID})
	s.CreateCard(domain.Card{Title: "b", ProjectID: web.ID})
	s.CreateCard(domain.Card{Title: "c", ProjectID: outro.ID})
	cs, err := s.ListCards(CardFilter{ProjectIDs: fam})
	if err != nil || len(cs) != 2 {
		t.Fatalf("cards: %v %v", cs, err)
	}
}

func TestUpdateProjectKeepsFamilyInvariants(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/web", loja.ID)

	moved := loja
	moved.Path = "/r/outra"
	if err := s.UpdateProject(moved); !errors.Is(err, ErrFamily) {
		t.Fatalf("pai muda de pasta deixando filho fora: %v", err)
	}
	flip := loja
	flip.Path = ""
	if err := s.UpdateProject(flip); !errors.Is(err, ErrFamily) {
		t.Fatalf("monorepo virando grupo: %v", err)
	}
	out := web
	out.Path = "/r/outro"
	if err := s.UpdateProject(out); !errors.Is(err, ErrFamily) {
		t.Fatalf("filho fora do pai: %v", err)
	}
	out.Path = ""
	if err := s.UpdateProject(out); !errors.Is(err, ErrFamily) {
		t.Fatalf("filho sem pasta: %v", err)
	}
	// o pai pode mudar de pasta se os filhos continuam dentro? so se continuam dentro da nova.
	same := loja
	same.Name = "loja2"
	if err := s.UpdateProject(same); err != nil {
		t.Fatalf("renomear pai: %v", err)
	}
	// captura do filho segue o pai
	web.Capture = false
	if err := s.UpdateProject(web); err != nil {
		t.Fatal(err)
	}
	kids, _ := s.Children(loja.ID)
	if !kids[0].Capture {
		t.Fatal("captura do filho divergiu do pai")
	}
}

// NAC-27: um grupo pode ganhar pasta, desde que os filhos fiquem dentro dela.
func TestUpdateGroupWithChildrenGainsPathOnlyAroundThem(t *testing.T) {
	s := newTestStore(t)
	g, _ := s.CreateGroup("g")
	mustChild(t, s, "front", "/r/front", g.ID)
	g.Path = "/x"
	if err := s.UpdateProject(g); !errors.Is(err, ErrFamily) {
		t.Fatalf("pasta que deixa filho fora: %v", err)
	}
	g.Path = "/r"
	if err := s.UpdateProject(g); err != nil {
		t.Fatalf("pasta que contém os filhos: %v", err)
	}
}

func TestAssignSessionsInWorktreeRespectsApps(t *testing.T) {
	s := newTestStore(t)
	loja := mustProject(t, s, "loja", "/r/loja")
	web := mustChild(t, s, "web", "/r/loja/apps/web", loja.ID)
	wt := "/r/loja/.worktrees/x"
	for id, cwd := range map[string]string{"claude:w": wt + "/apps/web/src", "claude:r": wt} {
		x := newSession(id, len(id))
		x.Cwd, x.Worktree, x.WorktreeLinked = cwd, wt, true
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.AssignSessionsInWorktree(loja.ID, wt)
	if err != nil || n != 2 {
		t.Fatalf("n = %d, %v", n, err)
	}
	if projectOf(t, s, "claude:w") != web.ID || projectOf(t, s, "claude:r") != loja.ID {
		t.Fatal("worktree mal atribuído")
	}
}
