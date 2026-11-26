package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

// A sessão está num repositório com o branch gravado desatualizado e o comando
// rodou em outro: o card fica com o branch da pasta do comando e a sessão relê
// o dela, para o PostToolUse do próprio comando não levar esse branch ao card.
func TestMeCommandFolderBranchRefreshesSessionBranch(t *testing.T) {
	a := dirGitAPI(t, &dirGit{branches: map[string]string{"/w/api": "feat/live", "/w/b": "feat/b"}})
	a.project(t)
	b, err := a.st.CreateProject("b", "/w/b")
	if err != nil {
		t.Fatal(err)
	}
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/api", Branch: "feat/old"})
	c := a.createdCard(t, "/api/me/start", map[string]string{"title": "T", "cwd": "/w/b"}, 4242)
	if c.ProjectID != b.ID || c.Branch != "feat/b" {
		t.Fatalf("card = projeto %d (quer %d), branch %q", c.ProjectID, b.ID, c.Branch)
	}
	if s, _ := a.st.GetSession("claude:s"); s.Cwd != "/w/api" || s.Branch != "feat/live" {
		t.Fatalf("sessão = %q %q", s.Cwd, s.Branch)
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
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, Git: gitinfo.Exec{}, Home: home})
	mae := filepath.Join(home, "mae")
	// Fora da home o nobre project add recusa a pasta: o aviso não o oferece.
	fora := filepath.Join(t.TempDir(), "fora")
	if err := os.MkdirAll(filepath.Join(fora, "repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
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
		{"comando em pasta de repositórios fora da home", fora, []string{msgNotGit}, "nobre project add"},
		{"comando na home", home, []string{msgNotGit}, "nobre project add"},
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

// created é a resposta de nobre start / nobre card add.
type created struct {
	domain.Card
	ProjectLabel string `json:"project_label"`
	LinkedRepo   string `json:"linked_repo"`
	LinkedGroup  string `json:"linked_group"`
}

func (a *testAPI) create(t *testing.T, path string, body map[string]string, pid int) created {
	t.Helper()
	rec := a.do(t, "POST", path, body, pid)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s = %d %s", path, rec.Code, rec.Body)
	}
	return decode[created](t, rec)
}

func (a *testAPI) projectAt(t *testing.T, path string) *domain.Project {
	t.Helper()
	ps, err := a.st.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	return projectByPath(ps)[path]
}

// Comando dentro de um repositório git não cadastrado que fica num grupo com
// pasta: o repositório é cadastrado como filho do grupo (pela raiz, com a
// captura do grupo) e o card nasce nele; a resposta diz isso. Da segunda vez
// o projeto já existe.
func TestMeCommandLinksUnregisteredRepoToGroup(t *testing.T) {
	const front = "/w/bravonix/turing-front"
	a := dirGitAPI(t, &dirGit{
		branches: map[string]string{front + "/src": "feat/f"},
		roots:    map[string]string{front + "/src": front},
	})
	g, _ := a.bravonix(t)
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/bravonix", ProjectID: &g.ID})
	in := map[string]string{"title": "T", "cwd": front + "/src"}
	c := a.create(t, "/api/me/cards", in, 4242)
	p := a.projectAt(t, front)
	if p == nil || p.ParentID == nil || *p.ParentID != g.ID || p.Name != "turing-front" || p.Capture != g.Capture || p.IsGroup() {
		t.Fatalf("filho = %+v", p)
	}
	if c.ProjectID != p.ID || c.LinkedRepo != "turing-front" || c.LinkedGroup != "Bravonix" || c.ProjectLabel != "Bravonix › turing-front" {
		t.Fatalf("resposta = %+v", c)
	}
	c = a.create(t, "/api/me/start", in, 4242)
	if c.ProjectID != p.ID || c.LinkedRepo != "" || c.LinkedGroup != "" || c.Branch != "feat/f" || c.ProjectLabel != "Bravonix › turing-front" {
		t.Fatalf("segunda vez = %+v", c)
	}
	if ps, _ := a.st.ListProjects(); len(ps) != 3 {
		t.Fatalf("projetos = %+v", ps)
	}
}

// Worktree ligado de um repositório não cadastrado do grupo: quem vira projeto
// é o checkout principal, nunca a pasta do worktree.
func TestMeCommandLinksMainCheckoutOfLinkedWorktree(t *testing.T) {
	a := dirGitAPI(t, &dirGit{
		roots: map[string]string{"/w/wt-novo/src": "/w/wt-novo"},
		mains: map[string]string{"/w/wt-novo/src": "/w/bravonix/novo"},
	})
	g, _ := a.bravonix(t)
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/bravonix", ProjectID: &g.ID})
	c := a.create(t, "/api/me/cards", map[string]string{"title": "T", "cwd": "/w/wt-novo/src"}, 4242)
	p := a.projectAt(t, "/w/bravonix/novo")
	if p == nil || c.ProjectID != p.ID || c.LinkedRepo != "novo" || a.projectAt(t, "/w/wt-novo") != nil {
		t.Fatalf("filho = %+v resposta = %+v", p, c)
	}
}

// O vínculo automático só vale dentro de um grupo cadastrado e segue a regra
// da varredura: fora de grupo, em pasta que a varredura pula, abaixo de 2
// níveis, numa pasta do grupo que não é repositório ou num repositório que o
// usuário mandou não sugerir, nada é cadastrado.
func TestMeCommandNeverLinksOutsideTheScanRules(t *testing.T) {
	roots := map[string]string{}
	for _, r := range []string{"/w/solto", "/w/bravonix/node_modules/x", "/w/bravonix/a/b/c", "/w/bravonix/recusado"} {
		roots[r] = r
	}
	a := dirGitAPI(t, &dirGit{roots: roots})
	g, _ := a.bravonix(t)
	if err := a.st.PutKV(dismissedKey, map[string][]string{strconv.FormatInt(g.ID, 10): {"recusado"}}); err != nil {
		t.Fatal(err)
	}
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/bravonix", ProjectID: &g.ID})
	for _, cwd := range []string{"/w/solto", "/w/bravonix/node_modules/x", "/w/bravonix/a/b/c", "/w/bravonix/recusado", "/w/bravonix/docs"} {
		c := a.create(t, "/api/me/cards", map[string]string{"title": "T", "cwd": cwd}, 4242)
		if c.ProjectID != g.ID || c.LinkedRepo != "" {
			t.Fatalf("%s: resposta = %+v", cwd, c)
		}
	}
	if ps, _ := a.st.ListProjects(); len(ps) != 2 {
		t.Fatalf("cadastrou sozinho: %+v", ps)
	}
}

// nobre start numa sessão sem projeto: a sessão é adotada pelo projeto do
// card, com a captura dele. Projeto com captura desligada não adota, e sessão
// que já tem projeto continua nele.
func TestMeStartAdoptsSessionWithoutProject(t *testing.T) {
	a := dirGitAPI(t, &dirGit{})
	g, back := a.bravonix(t)
	off, err := a.st.CreateProject("off", "/w/off")
	if err != nil {
		t.Fatal(err)
	}
	off.Capture = false
	if err := a.st.UpdateProject(off); err != nil {
		t.Fatal(err)
	}
	a.liveSess(t, domain.Session{ID: "claude:nova", PID: 1, Cwd: "/w/mae"})
	a.liveSess(t, domain.Session{ID: "claude:off", PID: 2, Cwd: "/w/mae"})
	a.liveSess(t, domain.Session{ID: "claude:grupo", PID: 3, Cwd: "/w/bravonix", ProjectID: &g.ID, FullCapture: true})
	a.create(t, "/api/me/start", map[string]string{"title": "T", "cwd": back.Path}, 1)
	a.create(t, "/api/me/start", map[string]string{"title": "T", "cwd": "/w/off"}, 2)
	a.create(t, "/api/me/start", map[string]string{"title": "T", "cwd": back.Path}, 3)
	if s, _ := a.st.GetSession("claude:nova"); s.ProjectID == nil || *s.ProjectID != back.ID || !s.FullCapture || s.Cwd != "/w/mae" {
		t.Fatalf("sem projeto: %+v", s)
	}
	if s, _ := a.st.GetSession("claude:off"); s.ProjectID != nil || s.FullCapture {
		t.Fatalf("captura desligada: %+v", s)
	}
	if s, _ := a.st.GetSession("claude:grupo"); s.ProjectID == nil || *s.ProjectID != g.ID {
		t.Fatalf("já tinha projeto: %+v", s)
	}
	// nobre card add não liga a sessão a nada: também não adota.
	a.liveSess(t, domain.Session{ID: "claude:add", PID: 4, Cwd: "/w/mae"})
	a.create(t, "/api/me/cards", map[string]string{"title": "T", "cwd": back.Path}, 4)
	if s, _ := a.st.GetSession("claude:add"); s.ProjectID != nil {
		t.Fatalf("card add adotou: %+v", s)
	}
}

// A resposta só cita o projeto quando ele não é o da sessão.
func TestMeStartSaysProjectWhenNotTheSessions(t *testing.T) {
	a := dirGitAPI(t, &dirGit{})
	g, back := a.bravonix(t)
	a.liveSess(t, domain.Session{ID: "claude:s", PID: 4242, Cwd: "/w/bravonix", ProjectID: &g.ID})
	if c := a.create(t, "/api/me/start", map[string]string{"title": "T", "cwd": "/w/bravonix"}, 4242); c.ProjectLabel != "" {
		t.Fatalf("mesmo projeto: %+v", c)
	}
	if c := a.create(t, "/api/me/cards", map[string]string{"title": "T"}, 4242); c.ProjectLabel != "" {
		t.Fatalf("CLI antiga: %+v", c)
	}
	if c := a.create(t, "/api/me/start", map[string]string{"title": "T", "cwd": back.Path}, 4242); c.ProjectLabel != "Bravonix › turing-back" {
		t.Fatalf("outro projeto: %+v", c)
	}
}

// nobre project add <repo> sem --pai, com a pasta dentro de um grupo com
// pasta: o repositório nasce ligado ao grupo e a resposta diz qual.
func TestCreateProjectInsideGroupFolderAttaches(t *testing.T) {
	a := newTestAPI(t)
	g, _ := a.bravonix(t)
	rec := a.do(t, "POST", "/api/projects", map[string]any{"path": "/w/bravonix/turing-front"}, 0)
	out := decode[struct {
		domain.Project
		AttachedTo string `json:"attached_to"`
	}](t, rec)
	if rec.Code != http.StatusCreated || out.ParentID == nil || *out.ParentID != g.ID || out.AttachedTo != "Bravonix" {
		t.Fatalf("dentro do grupo = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/projects", map[string]any{"path": "/w/solto"}, 0)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), `"attached_to":"B`) || decode[domain.Project](t, rec).ParentID != nil {
		t.Fatalf("fora do grupo = %d %s", rec.Code, rec.Body)
	}
}
