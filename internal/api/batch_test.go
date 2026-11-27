package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

type batchResp struct {
	Project  domain.Project   `json:"project"`
	Children []domain.Project `json:"children"`
	Moved    store.Moved      `json:"moved"`
}

// repoDirs cria dir/<nome>/.git para cada nome e devolve dir.
func repoDirs(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(dir, n, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func projectCount(t *testing.T, a *testAPI) int {
	t.Helper()
	ps, err := a.st.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	return len(ps)
}

func TestBatchCreatesGroupFolderWithChildren(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "a", "b")
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{
		"path": dir, "name": "turing",
		"children": []map[string]string{{"path": dir + "/a"}, {"path": dir + "/b", "name": "bee"}},
	}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	got := decode[batchResp](t, rec)
	if got.Project.Kind != "group" || got.Project.Path != dir || got.Project.Name != "turing" {
		t.Fatalf("project = %+v", got.Project)
	}
	if len(got.Children) != 2 || got.Children[0].Name != "a" || got.Children[1].Name != "bee" {
		t.Fatalf("children = %+v", got.Children)
	}
	for _, c := range got.Children {
		if c.ParentID == nil || *c.ParentID != got.Project.ID || c.Kind != "repo" {
			t.Fatalf("filho = %+v", c)
		}
	}
}

func TestBatchRepoFolderAddsChildrenToExisting(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "apps")
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{"path": dir, "children": []map[string]string{{"path": dir + "/apps"}}}, 0)
	got := decode[batchResp](t, rec)
	if rec.Code != http.StatusCreated || got.Project.Kind != "repo" || len(got.Children) != 1 {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	// Segunda chamada: o principal já existe, só entram os filhos novos.
	if err := os.MkdirAll(filepath.Join(dir, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec = a.do(t, "POST", "/api/projects/batch", map[string]any{"path": dir, "children": []map[string]string{{"path": dir + "/web"}}}, 0)
	if rec.Code != http.StatusOK || decode[batchResp](t, rec).Project.ID != got.Project.ID {
		t.Fatalf("batch 2 = %d %s", rec.Code, rec.Body)
	}
	if n := projectCount(t, a); n != 3 {
		t.Fatalf("projetos = %d", n)
	}
}

func TestBatchLinksExistingStandalone(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "turing-back")
	back, err := a.st.CreateProject("turing-back", dir+"/turing-back")
	if err != nil {
		t.Fatal(err)
	}
	a.startSess(t, "claude:s1", dir+"/turing-back/src", &back.ID, true)
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{
		"path": dir, "name": "turing", "children": []map[string]string{{"path": dir + "/turing-back"}},
	}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	got := decode[batchResp](t, rec)
	if len(got.Children) != 1 || got.Children[0].ID != back.ID || got.Children[0].ParentID == nil || *got.Children[0].ParentID != got.Project.ID {
		t.Fatalf("children = %+v", got.Children)
	}
	x, err := a.st.GetSession("claude:s1")
	if err != nil || x.ProjectID == nil || *x.ProjectID != back.ID {
		t.Fatalf("sessão = %+v %v", x, err)
	}
}

func TestBatchAtomicOnBlockedChild(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "a", "b")
	other, err := a.st.CreateGroup("outro")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.st.CreateChild("a", dir+"/a", other.ID); err != nil {
		t.Fatal(err)
	}
	before := projectCount(t, a)
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{
		"path": dir, "children": []map[string]string{{"path": dir + "/b"}, {"path": dir + "/a"}},
	}, 0)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "pertence a outro") {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	if n := projectCount(t, a); n != before {
		t.Fatalf("projetos = %d, antes %d: o lote não é tudo ou nada", n, before)
	}
}

func TestBatchRefusesChildOutsideFolderAndDuplicateName(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "a", "b")
	other := repoDirs(t, "z")
	for name, body := range map[string]map[string]any{
		"fora": {"path": dir, "children": []map[string]string{{"path": other + "/z"}}},
		"nome": {"path": dir, "children": []map[string]string{{"path": dir + "/a", "name": "x"}, {"path": dir + "/b", "name": "X"}}},
	} {
		if rec := a.do(t, "POST", "/api/projects/batch", body, 0); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d %s", name, rec.Code, rec.Body)
		}
	}
	if n := projectCount(t, a); n != 0 {
		t.Fatalf("projetos = %d", n)
	}
}

func TestBatchAdoptsRootSessions(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "a")
	a.startSess(t, "claude:docs", dir+"/docs", nil, true)
	a.startSess(t, "claude:src", dir+"/a/src", nil, true)
	a.startSess(t, "claude:far", t.TempDir(), nil, true)
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{
		"path": dir, "children": []map[string]string{{"path": dir + "/a"}},
	}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	got := decode[batchResp](t, rec)
	if got.Moved.Sessions != 2 {
		t.Fatalf("moved = %+v", got.Moved)
	}
	docs, _ := a.st.GetSession("claude:docs")
	src, _ := a.st.GetSession("claude:src")
	far, _ := a.st.GetSession("claude:far")
	if docs.ProjectID == nil || *docs.ProjectID != got.Project.ID {
		t.Fatalf("docs = %+v", docs.ProjectID)
	}
	if src.ProjectID == nil || *src.ProjectID != got.Children[0].ID {
		t.Fatalf("src = %+v", src.ProjectID)
	}
	if far.ProjectID != nil {
		t.Fatalf("far = %+v", far.ProjectID)
	}
}

func TestPatchGroupGainsFolder(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "a")
	g, err := a.st.CreateGroup("turing")
	if err != nil {
		t.Fatal(err)
	}
	a.startSess(t, "claude:docs", dir+"/docs", nil, true)
	rec := a.do(t, "PATCH", "/api/projects/"+strconv.FormatInt(g.ID, 10), map[string]any{"name": "turing", "path": dir}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body)
	}
	got := decode[struct {
		domain.Project
		Moved store.Moved `json:"moved"`
	}](t, rec)
	if got.Path != dir || got.Kind != "group" || got.Moved.Sessions != 1 {
		t.Fatalf("patch = %+v", got)
	}
	if x, _ := a.st.GetSession("claude:docs"); x.ProjectID == nil || *x.ProjectID != g.ID {
		t.Fatalf("sessão = %+v", x.ProjectID)
	}
}

func groupWithFolder(t *testing.T, a *testAPI) domain.Project {
	t.Helper()
	g, err := a.st.CreateGroupAt("turing", repoDirs(t, "a"))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGroupFolderAcceptsCards(t *testing.T) {
	a := newTestAPI(t)
	g := groupWithFolder(t, a)
	rec := a.do(t, "POST", "/api/cards", map[string]any{"title": "x", "project_id": g.ID}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("card = %d %s", rec.Code, rec.Body)
	}
}

func TestPathlessGroupStillRefusesCards(t *testing.T) {
	a := newTestAPI(t)
	g, err := a.st.CreateGroup("loja")
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/cards", map[string]any{"title": "x", "project_id": g.ID}, 0)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != msgGroupNoCards {
		t.Fatalf("card = %d %s", rec.Code, rec.Body)
	}
}

func TestBranchRefusedOnGroupCard(t *testing.T) {
	a := newTestAPI(t)
	g := groupWithFolder(t, a)
	c, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, c.Key)
	rec := a.do(t, "POST", "/api/me/branch", map[string]string{"branch": "feat/x"}, 4242)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "grupo não tem branch" {
		t.Fatalf("branch = %d %s", rec.Code, rec.Body)
	}
}

// noGit falha o teste se o daemon tocar no git: nunca roda na pasta de um grupo.
type noGit struct{ t *testing.T }

func (g noGit) Branch(d string) string      { g.t.Errorf("Branch(%s)", d); return "" }
func (g noGit) Head(d string) string        { g.t.Errorf("Head(%s)", d); return "" }
func (g noGit) DiffStat(d, _ string) string { g.t.Errorf("DiffStat(%s)", d); return "" }
func (g noGit) Clean(d string) bool         { g.t.Errorf("Clean(%s)", d); return true }
func (g noGit) Checkout(d, b string) error  { g.t.Errorf("Checkout(%s, %s)", d, b); return nil }
func (g noGit) Locate(d string) gitinfo.Location {
	g.t.Errorf("Locate(%s)", d)
	return gitinfo.Location{}
}
func (g noGit) Worktrees(d string) ([]gitinfo.Worktree, error) {
	g.t.Errorf("Worktrees(%s)", d)
	return nil, nil
}

func TestContinueGroupCardOpensInFolderWithoutGit(t *testing.T) {
	a, l := continueAPI(t, noGit{t})
	dir := repoDirs(t, "a")
	g, err := a.st.CreateGroupAt("turing", dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.st.CreateCard(domain.Card{Title: "geral", ProjectID: g.ID, Branch: "feat/x"})
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/cards/"+c.Key+"/continue", map[string]any{"agent": "claude", "prompt_slug": "nenhum"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("continue = %d %s", rec.Code, rec.Body)
	}
	if len(l.calls) != 1 || !strings.Contains(l.calls[0][len(l.calls[0])-1], dir) {
		t.Fatalf("lançamentos = %q", l.calls)
	}
}

func TestSyncCardBranchSkipsGroupCard(t *testing.T) {
	a := newTestAPI(t)
	g := groupWithFolder(t, a)
	c, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := board.SyncCardBranch(a.st, nil, c.ID, "feat/x"); err != nil || changed {
		t.Fatalf("changed = %v err = %v", changed, err)
	}
	if got, _ := a.st.GetCardByID(c.ID); got.Branch != "" {
		t.Fatalf("branch = %q", got.Branch)
	}
}

func TestGroupCardRefusesBranch(t *testing.T) {
	a := newTestAPI(t)
	g := groupWithFolder(t, a)
	rec := a.do(t, "POST", "/api/cards", map[string]any{"title": "x", "project_id": g.ID, "branch": "feat/x"}, 0)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "grupo não tem branch" {
		t.Fatalf("card = %d %s", rec.Code, rec.Body)
	}
}

// Grupo com a captura desligada funciona como "ignorar": cadastrar de novo não
// adota as sessões soltas na pasta dele (como o hook, que não as liga).
func TestBatchSkipsAdoptionWhenCaptureOff(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "a")
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{"path": dir}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	g := decode[batchResp](t, rec).Project
	a.patchCapture(t, g, false)
	a.startSess(t, "claude:docs", dir+"/docs", nil, false)
	rec = a.do(t, "POST", "/api/projects/batch", map[string]any{"path": dir}, 0)
	if rec.Code != http.StatusOK || decode[batchResp](t, rec).Moved.Sessions != 0 {
		t.Fatalf("batch 2 = %d %s", rec.Code, rec.Body)
	}
	if pid, capture := a.owner(t, "claude:docs"); pid != nil || capture {
		t.Fatalf("sessão adotada com a captura desligada: %v %v", pid, capture)
	}
}

// O cadastro pela tela também adota as sessões em worktrees ligados fora da
// pasta do repositório, como o POST /api/projects fazia; no monorepo cada uma
// vai para o app do mesmo lugar no checkout principal.
func TestBatchAdoptsLinkedWorktreeSessions(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "apps/web")
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt, other := t.TempDir(), t.TempDir()
	g := &locGit{fakeGit: &fakeGit{branch: "feat/x"}, mains: map[string]string{wt: dir, other: "/r/outro"}}
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, Git: g})
	a.startLinkedSess(t, "claude:raiz", wt, wt)
	a.startLinkedSess(t, "claude:web", wt+"/apps/web/src", wt)
	a.startLinkedSess(t, "claude:outro", other, other)
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{"path": dir, "children": []map[string]string{{"path": dir + "/apps/web"}}}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	got := decode[batchResp](t, rec)
	if got.Moved.Sessions != 2 {
		t.Fatalf("moved = %+v", got.Moved)
	}
	if pid, capture := a.owner(t, "claude:raiz"); pid == nil || *pid != got.Project.ID || !capture {
		t.Fatalf("raiz = %v %v", pid, capture)
	}
	if pid, _ := a.owner(t, "claude:web"); pid == nil || *pid != got.Children[0].ID {
		t.Fatalf("web = %v", pid)
	}
	if pid, _ := a.owner(t, "claude:outro"); pid != nil {
		t.Fatalf("outro = %v", pid)
	}
}

// Card com branch que vai para o grupo perde o branch: card de grupo nunca tem.
func TestPatchCardIntoGroupClearsBranch(t *testing.T) {
	a := newTestAPI(t)
	g := groupWithFolder(t, a)
	kid, _, err := a.st.CreateChild("a", g.Path+"/a", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: kid.ID, Branch: "feat/x"})
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"project_id": g.ID}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body)
	}
	if got := decode[domain.Card](t, rec); got.ProjectID != g.ID || got.Branch != "" {
		t.Fatalf("card = %+v", got)
	}
	if got, _ := a.st.GetCardByID(c.ID); got.Branch != "" {
		t.Fatalf("branch gravado = %q", got.Branch)
	}
}

// Filhos sem nome informado num grupo (o nobre project add da pasta): nome que
// bate com o do grupo ou com o de outro filho vira um nome distinto em vez de
// recusar o lote; um projeto solto já cadastrado cujo nome bate fica de fora
// (não é renomeado) e o resto entra.
func TestBatchGroupNameClashNeverBlocks(t *testing.T) {
	a := newTestAPI(t)
	dir := repoDirs(t, "a/api", "b/api", "turing", "solto")
	if _, err := a.st.CreateProject("API", dir+"/solto"); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/projects/batch", map[string]any{
		"path": dir, "name": "turing",
		"children": []map[string]string{{"path": dir + "/a/api"}, {"path": dir + "/b/api"}, {"path": dir + "/solto"}, {"path": dir + "/turing"}},
	}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	got := decode[batchResp](t, rec)
	var names []string
	for _, c := range got.Children {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "api,b/api,turing-2" {
		t.Fatalf("filhos = %v", names)
	}
	if p := a.projectAt(t, dir+"/solto"); p == nil || p.ParentID != nil || p.Name != "API" {
		t.Fatalf("solto = %+v", p)
	}
}
