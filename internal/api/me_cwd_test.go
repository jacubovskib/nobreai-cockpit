package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

// dirGit é um Git falso que responde por pasta: branch, topo do worktree e
// checkout principal. Pasta fora dos mapas não é um repositório.
type dirGit struct {
	*fakeGit
	branches map[string]string
	roots    map[string]string
	mains    map[string]string
}

func (g *dirGit) Branch(dir string) string { return g.branches[dir] }
func (g *dirGit) Locate(dir string) gitinfo.Location {
	root := g.roots[dir]
	if root == "" {
		return gitinfo.Location{}
	}
	main := g.mains[dir]
	if main == "" {
		main = root
	}
	return gitinfo.Location{Root: root, Main: main, Linked: root != main}
}
func (g *dirGit) Worktrees(string) ([]gitinfo.Worktree, error) { return nil, nil }

func dirGitAPI(t *testing.T, g *dirGit) *testAPI {
	t.Helper()
	g.fakeGit = &fakeGit{}
	a := newTestAPI(t)
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, Git: g})
	return a
}

// bravonix cadastra o grupo com pasta /w/bravonix e o filho turing-back.
func (a *testAPI) bravonix(t *testing.T) (group, back domain.Project) {
	t.Helper()
	group, err := a.st.CreateGroupAt("Bravonix", "/w/bravonix")
	if err != nil {
		t.Fatal(err)
	}
	back, _, err = a.st.CreateChild("turing-back", "/w/bravonix/turing-back", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	return group, back
}

func (a *testAPI) liveSess(t *testing.T, x domain.Session) {
	t.Helper()
	x.Agent, x.StartedAt = "claude", testNow
	if err := a.st.StartSession(x); err != nil {
		t.Fatal(err)
	}
}

// createdCard cria pelo caminho path e devolve o card gravado.
func (a *testAPI) createdCard(t *testing.T, path string, body map[string]string, pid int) domain.Card {
	t.Helper()
	rec := a.do(t, "POST", path, body, pid)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s = %d %s", path, rec.Code, rec.Body)
	}
	c, err := a.st.GetCard(decode[domain.Card](t, rec).Key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A sessão começou na pasta-mãe sem git; o comando rodou dentro de um
// repositório filho cadastrado: o card nasce nele, com o branch de lá.
func TestMeCommandFolderPicksRegisteredChildRepo(t *testing.T) {
	a := dirGitAPI(t, &dirGit{
		branches: map[string]string{"/w/mae/turing-back": "feat/tb"},
		roots:    map[string]string{"/w/mae/turing-back": "/w/mae/turing-back"},
	})
	p, err := a.st.CreateProject("turing-back", "/w/mae/turing-back")
	if err != nil {
		t.Fatal(err)
	}
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/mae"})
	in := map[string]string{"title": "T", "cwd": "/w/mae/turing-back"}
	if c := a.createdCard(t, "/api/me/cards", in, 4242); c.ProjectID != p.ID {
		t.Fatalf("card add: projeto = %d, quer %d", c.ProjectID, p.ID)
	}
	if c := a.createdCard(t, "/api/me/start", in, 4242); c.ProjectID != p.ID || c.Branch != "feat/tb" {
		t.Fatalf("start: projeto = %d (quer %d), branch = %q", c.ProjectID, p.ID, c.Branch)
	}
	// A localização da sessão não muda: ela continua na pasta-mãe.
	if s, _ := a.st.GetSession("claude:s"); s.Cwd != "/w/mae" || s.Branch != "" {
		t.Fatalf("sessão = %q %q", s.Cwd, s.Branch)
	}
}

// A sessão está ligada ao grupo; o comando rodou dentro do filho: o filho vence.
func TestMeCommandFolderBeatsSessionProject(t *testing.T) {
	a := dirGitAPI(t, &dirGit{
		branches: map[string]string{"/w/bravonix/turing-back/src": "feat/tb"},
		roots:    map[string]string{"/w/bravonix/turing-back/src": "/w/bravonix/turing-back"},
	})
	g, back := a.bravonix(t)
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/bravonix", ProjectID: &g.ID})
	in := map[string]string{"title": "T", "cwd": "/w/bravonix/turing-back/src"}
	if c := a.createdCard(t, "/api/me/cards", in, 4242); c.ProjectID != back.ID {
		t.Fatalf("card add: projeto = %d, quer %d", c.ProjectID, back.ID)
	}
	if c := a.createdCard(t, "/api/me/start", in, 4242); c.ProjectID != back.ID || c.Branch != "feat/tb" {
		t.Fatalf("start: projeto = %d (quer %d), branch = %q", c.ProjectID, back.ID, c.Branch)
	}
}

// Sessão e comando na pasta do grupo: o card nasce no grupo (grupo com pasta
// aceita cards), sem branch, e nobre app o leva para um filho.
func TestMeCommandInGroupFolderCreatesGroupCard(t *testing.T) {
	a := dirGitAPI(t, &dirGit{})
	g, back := a.bravonix(t)
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/bravonix", ProjectID: &g.ID})
	c := a.createdCard(t, "/api/me/start", map[string]string{"title": "T", "cwd": "/w/bravonix"}, 4242)
	if c.ProjectID != g.ID || c.Branch != "" {
		t.Fatalf("card = projeto %d (quer %d), branch %q", c.ProjectID, g.ID, c.Branch)
	}
	if rec := a.do(t, "POST", "/api/me/app", map[string]any{"app": "turing-back"}, 4242); rec.Code != http.StatusOK {
		t.Fatalf("app = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(c.Key); got.ProjectID != back.ID {
		t.Fatalf("depois do nobre app: projeto = %d, quer %d", got.ProjectID, back.ID)
	}
}

// CLI antiga (sem cwd) ou cwd que não é absoluto: vale a regra de antes, o
// projeto da sessão.
func TestMeWithoutCommandFolderKeepsSessionProject(t *testing.T) {
	a := dirGitAPI(t, &dirGit{})
	g, _ := a.bravonix(t)
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/bravonix", ProjectID: &g.ID})
	for _, in := range []map[string]string{{"title": "T"}, {"title": "T", "cwd": ""}, {"title": "T", "cwd": "turing-back"}} {
		if c := a.createdCard(t, "/api/me/cards", in, 4242); c.ProjectID != g.ID {
			t.Fatalf("%v: projeto = %d, quer %d", in, c.ProjectID, g.ID)
		}
	}
}

// A pasta do comando não tem projeto cadastrado: vale o projeto da sessão,
// com o branch da sessão.
func TestMeCommandFolderUnregisteredFallsBackToSession(t *testing.T) {
	a := dirGitAPI(t, &dirGit{branches: map[string]string{"/w/api": "feat/a", "/w/outro": "feat/o"}})
	p := a.project(t)
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/api", Branch: "feat/a", ProjectID: &p.ID})
	c := a.createdCard(t, "/api/me/start", map[string]string{"title": "T", "cwd": "/w/outro"}, 4242)
	if c.ProjectID != p.ID || c.Branch != "feat/a" {
		t.Fatalf("card = projeto %d (quer %d), branch %q", c.ProjectID, p.ID, c.Branch)
	}
}

// O projeto veio da pasta do comando e o git não leu branch lá: o card não
// herda o branch da sessão, que é de outro repositório.
func TestMeCommandFolderBranchNeverComesFromSessionRepo(t *testing.T) {
	a := dirGitAPI(t, &dirGit{branches: map[string]string{"/w/api": "feat/a"}})
	a.project(t)
	b, err := a.st.CreateProject("b", "/w/b")
	if err != nil {
		t.Fatal(err)
	}
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/api", Branch: "feat/a"})
	c := a.createdCard(t, "/api/me/start", map[string]string{"title": "T", "cwd": "/w/b"}, 4242)
	if c.ProjectID != b.ID || c.Branch != "" {
		t.Fatalf("card = projeto %d (quer %d), branch %q", c.ProjectID, b.ID, c.Branch)
	}
}

// Sessão num worktree ligado dentro do monorepo, ligada ao app: a pasta do
// comando é traduzida para o checkout principal e o card continua no app.
func TestMeCommandFolderInLinkedWorktreeKeepsApp(t *testing.T) {
	const wt, cwd = "/w/loja/.worktrees/x", "/w/loja/.worktrees/x/apps/web"
	a := dirGitAPI(t, &dirGit{
		branches: map[string]string{cwd: "feat/x"},
		roots:    map[string]string{cwd: wt},
		mains:    map[string]string{cwd: "/w/loja"},
	})
	root, err := a.st.CreateProject("loja", "/w/loja")
	if err != nil {
		t.Fatal(err)
	}
	web, _, err := a.st.CreateChild("web", "/w/loja/apps/web", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: cwd, Branch: "feat/x", Worktree: wt, WorktreeLinked: true, ProjectID: &web.ID})
	c := a.createdCard(t, "/api/me/start", map[string]string{"title": "T", "cwd": cwd}, 4242)
	if c.ProjectID != web.ID || c.Branch != "feat/x" {
		t.Fatalf("card = projeto %d (quer %d), branch %q", c.ProjectID, web.ID, c.Branch)
	}
}

// Nada cadastrado. Pasta sem git com repositórios dentro: o aviso diz que ela
// pode virar grupo e cita o comando. Repositório git: o aviso de cadastro de
// sempre. Pasta sem git e sem repositórios: segue sem card.
func TestMeNoProjectMessageByFolderKind(t *testing.T) {
	a := newTestAPI(t)
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, Git: gitinfo.Exec{}})
	mae := t.TempDir()
	repo := filepath.Join(mae, "bravonix", "turing-back")
	vazia := filepath.Join(mae, "bravonix", "docs")
	for _, d := range []string{repo, vazia} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "init", "-q", "-b", "main")
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: mae})
	folder := "Esta pasta não é um repositório git, mas tem repositórios dentro e pode ser cadastrada como grupo."
	cases := []struct {
		name, cwd string
		want      []string
		not       string
	}{
		{"CLI antiga, sessão na pasta-mãe", "", []string{folder, "nobre project add " + mae}, "não pode ser cadastrada"},
		{"comando na pasta-mãe", mae, []string{folder, "nobre project add " + mae}, "não pode ser cadastrada"},
		{"comando na pasta do grupo", filepath.Dir(repo), []string{folder, "nobre project add " + filepath.Dir(repo)}, "não pode ser cadastrada"},
		{"comando no repositório filho", repo, []string{msgNoProject}, "não é um repositório git"},
		{"comando em pasta sem repositórios", vazia, []string{msgNotGit}, "nobre project add"},
	}
	for _, c := range cases {
		for _, path := range []string{"/api/me/start", "/api/me/cards"} {
			in := map[string]string{"title": "T"}
			if c.cwd != "" {
				in["cwd"] = c.cwd
			}
			rec := a.do(t, "POST", path, in, 4242)
			msg := decode[map[string]string](t, rec)["error"]
			if rec.Code != http.StatusConflict || strings.Contains(msg, c.not) {
				t.Fatalf("%s %s = %d %q", c.name, path, rec.Code, msg)
			}
			for _, w := range c.want {
				if !strings.Contains(msg, w) {
					t.Fatalf("%s %s: falta %q em %q", c.name, path, w, msg)
				}
			}
		}
	}
	if ps, _ := a.st.ListProjects(); len(ps) != 0 {
		t.Fatalf("cadastrou projeto sozinho: %+v", ps)
	}
}
