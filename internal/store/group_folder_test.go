package store

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestMigrationMarksPathlessGroups(t *testing.T) {
	path := t.TempDir() + "/cockpit.db"
	s, err := openUpTo(path, 12)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO projects(name, path) VALUES ('g', NULL), ('r', '/w/r')`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateUpTo(1 << 30); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ps, err := s.ListProjects()
	if err != nil || len(ps) != 2 {
		t.Fatalf("%+v %v", ps, err)
	}
	if ps[0].Kind != "group" || !ps[0].IsGroup() || ps[1].Kind != "repo" || ps[1].IsGroup() {
		t.Errorf("kinds: %+v", ps)
	}
}

func TestCreateGroupAtRules(t *testing.T) {
	s := newTestStore(t)
	g, err := s.CreateGroupAt("bravonix", "/w/bravonix")
	if err != nil || g.Kind != "group" || g.Path != "/w/bravonix" {
		t.Fatalf("%+v %v", g, err)
	}
	home, _ := os.UserHomeDir()
	for _, bad := range []string{home, "/", "relativo/x"} {
		if _, err := s.CreateGroupAt("x", bad); !errors.Is(err, ErrFamily) {
			t.Errorf("%q: esperava ErrFamily, veio %v", bad, err)
		}
	}
	mustProject(t, s, "repo", "/w/repo")
	_, err = s.CreateGroupAt("y", "/w/repo/sub")
	if !errors.Is(err, ErrFamily) || !strings.Contains(err.Error(), "a pasta do grupo fica dentro do projeto repo") {
		t.Errorf("dentro de projeto: %v", err)
	}
	// grupo sem pasta continua existindo e com kind group
	g2, err := s.CreateGroup("solto")
	if err != nil || g2.Kind != "group" {
		t.Errorf("%+v %v", g2, err)
	}
	r := mustProject(t, s, "r2", "/w/r2")
	if r.Kind != "repo" {
		t.Errorf("repo kind = %q", r.Kind)
	}
}

func TestGroupFolderChildMustBeInside(t *testing.T) {
	s := newTestStore(t)
	g, _ := s.CreateGroupAt("bravonix", "/w/bravonix")
	if _, _, err := s.CreateChild("turing-back", "/w/bravonix/turing-back", g.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateChild("outro", "/w/outro", g.ID); !errors.Is(err, ErrFamily) {
		t.Errorf("esperava ErrFamily, veio %v", err)
	}
}

func TestProjectForPathGroupFolder(t *testing.T) {
	s := newTestStore(t)
	g, _ := s.CreateGroupAt("bravonix", "/w/bravonix")
	k := mustChild(t, s, "turing-back", "/w/bravonix/turing-back", g.ID)
	for cwd, want := range map[string]int64{
		"/w/bravonix":                 g.ID,
		"/w/bravonix/docs":            g.ID,
		"/w/bravonix/turing-back/src": k.ID,
	} {
		p, err := s.ProjectForPath(cwd)
		if err != nil || p == nil || p.ID != want {
			t.Errorf("%s: %+v %v, esperava %d", cwd, p, err, want)
		}
	}
	if p, _ := s.ProjectForPath("/w/fora"); p != nil {
		t.Errorf("/w/fora: %+v", p)
	}
	// grupo sem pasta continua fora
	if _, err := s.CreateGroup("semPasta"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.ProjectForPath("/"); p != nil {
		t.Errorf("/: %+v", p)
	}
}

func TestProjectForLocationLinkedWorktreeBeatsGroup(t *testing.T) {
	s := newTestStore(t)
	g, _ := s.CreateGroupAt("bravonix", "/w/bravonix")
	k := mustChild(t, s, "turing-front-2", "/w/bravonix/turing-front-2", g.ID)
	l := SessionLocation{WorktreeLinked: true, Worktree: "/w/bravonix/.worktrees/x", Main: "/w/bravonix/turing-front-2"}
	p, err := s.ProjectForLocation("/w/bravonix/.worktrees/x/src", l)
	if err != nil || p == nil || p.ID != k.ID {
		t.Fatalf("%+v %v, esperava o filho", p, err)
	}
	// Main fora de qualquer projeto: fica o grupo.
	l.Main = "/w/bravonix/nao-cadastrado"
	p, _ = s.ProjectForLocation("/w/bravonix/.worktrees/x/src", l)
	if p == nil || p.ID != g.ID {
		t.Errorf("esperava o grupo: %+v", p)
	}
}

func TestGroupGainsFolder(t *testing.T) {
	s := newTestStore(t)
	g, _ := s.CreateGroup("g")
	mustChild(t, s, "x", "/w/b/x", g.ID)
	g.Path = "/w/c"
	if err := s.UpdateProject(g); !errors.Is(err, ErrFamily) || !strings.Contains(err.Error(), "o sub-projeto x fica fora da pasta nova do grupo") {
		t.Fatalf("esperava recusa: %v", err)
	}
	g.Path = "/w/b"
	if err := s.UpdateProject(g); err != nil {
		t.Fatal(err)
	}
	ps, _ := s.ListProjects()
	if ps[0].Path != "/w/b" || ps[0].Kind != "group" {
		t.Errorf("%+v", ps[0])
	}
}

func TestRepoProjectOfGroupFolderChild(t *testing.T) {
	s := newTestStore(t)
	g, _ := s.CreateGroupAt("bravonix", "/w/bravonix")
	k := mustChild(t, s, "turing-back", "/w/bravonix/turing-back", g.ID)
	got, err := s.RepoProject(k)
	if err != nil || got.ID != k.ID {
		t.Errorf("%+v %v", got, err)
	}
}
