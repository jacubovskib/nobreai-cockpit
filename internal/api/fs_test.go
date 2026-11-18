package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

type fsDirsResp struct {
	Path   string `json:"path"`
	Parent string `json:"parent"`
	Dirs   []struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		Git        bool   `json:"git"`
		Registered bool   `json:"registered"`
	} `json:"dirs"`
}

type fsScanResp struct {
	Path        string          `json:"path"`
	Git         bool            `json:"git"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Registered  *domain.Project `json:"registered"`
	Suggestions []struct {
		Rel        string          `json:"rel"`
		Source     string          `json:"source"`
		Registered *domain.Project `json:"registered"`
		Blocked    string          `json:"blocked"`
	} `json:"suggestions"`
}

// fsAPI monta a API com a home em um diretório temporário (resolvido).
func fsAPI(t *testing.T) (*testAPI, string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	h := New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Home: home})
	return &testAPI{st: st, h: h, hub: hub}, home
}

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFsDirsListsHomeAndMarksGit(t *testing.T) {
	a, home := fsAPI(t)
	mkdirs(t, home, "zeta", "loja/.git", ".oculta", "plain")
	if err := os.WriteFile(filepath.Join(home, "arquivo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.CreateProject("loja", filepath.Join(home, "loja")); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/fs/dirs", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("dirs = %d %s", rec.Code, rec.Body)
	}
	got := decode[fsDirsResp](t, rec)
	if got.Path != home || got.Parent != "" || len(got.Dirs) != 3 {
		t.Fatalf("resposta = %s", rec.Body)
	}
	if d := got.Dirs[0]; d.Name != "loja" || !d.Git || !d.Registered || d.Path != filepath.Join(home, "loja") {
		t.Fatalf("loja = %+v", d)
	}
	if d := got.Dirs[1]; d.Name != "plain" || d.Git || d.Registered {
		t.Fatalf("plain = %+v", d)
	}
	rec = a.do(t, "GET", "/api/fs/dirs?path="+url.QueryEscape(filepath.Join(home, "plain")), nil, 0)
	if sub := decode[fsDirsResp](t, rec); sub.Parent != home {
		t.Fatalf("parent = %q", sub.Parent)
	}
}

func TestFsDirsRejectsOutsideHome(t *testing.T) {
	a, home := fsAPI(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "fuga")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{home + "/../", "/etc", filepath.Join(home, "fuga")} {
		rec := a.do(t, "GET", "/api/fs/dirs?path="+url.QueryEscape(p), nil, 0)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %q = %d %s", p, rec.Code, rec.Body)
		}
	}
	rec := a.do(t, "GET", "/api/fs/dirs?path="+url.QueryEscape(filepath.Join(home, "f.txt")), nil, 0)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("arquivo = %d", rec.Code)
	}
	rec = a.do(t, "GET", "/api/fs/scan?path="+url.QueryEscape("/etc"), nil, 0)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("scan fora da home = %d", rec.Code)
	}
}

func TestFsScanGroupFolder(t *testing.T) {
	a, home := fsAPI(t)
	mkdirs(t, home, "ws/api/.git", "ws/web/.git", "outro/x/.git")
	g, err := a.st.CreateGroup("outro")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.st.CreateChild("x", filepath.Join(home, "ws/web"), g.ID); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/fs/scan?path="+url.QueryEscape(filepath.Join(home, "ws")), nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("scan = %d %s", rec.Code, rec.Body)
	}
	got := decode[fsScanResp](t, rec)
	if got.Kind != "group" || got.Git || got.Name != "ws" || got.Registered != nil || len(got.Suggestions) != 2 {
		t.Fatalf("resposta = %s", rec.Body)
	}
	if s := got.Suggestions[0]; s.Rel != "api" || s.Source != "git" || s.Registered != nil || s.Blocked != "" {
		t.Fatalf("api = %+v", s)
	}
	if s := got.Suggestions[1]; s.Rel != "web" || s.Registered == nil || s.Blocked != "pertence a outro" {
		t.Fatalf("web = %+v", s)
	}
}

func TestFsScanBlockedWithChildrenAndDismissed(t *testing.T) {
	a, home := fsAPI(t)
	mkdirs(t, home, "ws/api/.git", "ws/web/.git", "ws/lib/.git", "ws/api/sub")
	api, err := a.st.CreateProject("api", filepath.Join(home, "ws/api"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.st.CreateChild("sub", filepath.Join(home, "ws/api/sub"), api.ID); err != nil {
		t.Fatal(err)
	}
	g, err := a.st.CreateGroupAt("ws", filepath.Join(home, "ws"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.PutKV(dismissedKey, map[string][]string{strconv.FormatInt(g.ID, 10): {"lib"}}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/fs/scan?path="+url.QueryEscape(filepath.Join(home, "ws")), nil, 0)
	got := decode[fsScanResp](t, rec)
	if got.Registered == nil || got.Registered.ID != g.ID || len(got.Suggestions) != 2 {
		t.Fatalf("resposta = %s", rec.Body)
	}
	if s := got.Suggestions[0]; s.Rel != "api" || s.Blocked != "tem sub-projetos" {
		t.Fatalf("api = %+v", s)
	}
	if s := got.Suggestions[1]; s.Rel != "web" || s.Blocked != "" {
		t.Fatalf("web = %+v", s)
	}
}

func TestFsScanMonorepo(t *testing.T) {
	a, home := fsAPI(t)
	mkdirs(t, home, "mono/.git", "mono/apps/web")
	if err := os.WriteFile(filepath.Join(home, "mono/pnpm-workspace.yaml"), []byte("packages:\n  - 'apps/*'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/fs/scan?path="+url.QueryEscape(filepath.Join(home, "mono")), nil, 0)
	got := decode[fsScanResp](t, rec)
	if rec.Code != http.StatusOK || got.Kind != "monorepo" || !got.Git || len(got.Suggestions) != 1 || got.Suggestions[0].Rel != "apps/web" {
		t.Fatalf("resposta = %d %s", rec.Code, rec.Body)
	}
	mkdirs(t, home, "solo/.git")
	rec = a.do(t, "GET", "/api/fs/scan?path="+url.QueryEscape(filepath.Join(home, "solo")), nil, 0)
	if got := decode[fsScanResp](t, rec); got.Kind != "repo" || len(got.Suggestions) != 0 {
		t.Fatalf("repo = %s", rec.Body)
	}
}

func TestFsScanEmptyFolder400(t *testing.T) {
	a, home := fsAPI(t)
	mkdirs(t, home, "vazia/nada")
	rec := a.do(t, "GET", "/api/fs/scan?path="+url.QueryEscape(filepath.Join(home, "vazia")), nil, 0)
	if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != msgNothingHere {
		t.Fatalf("scan vazia = %d %s", rec.Code, rec.Body)
	}
}

// A home não pode ser cadastrada: a varredura dela responde a mesma mensagem
// do cadastro, sem varrer.
func TestFsScanHomeRefused(t *testing.T) {
	a, home := fsAPI(t)
	mkdirs(t, home, "api/.git")
	for _, q := range []string{"", "?path=" + url.QueryEscape(home)} {
		rec := a.do(t, "GET", "/api/fs/scan"+q, nil, 0)
		if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != msgHomeProject {
			t.Fatalf("scan %q = %d %s", q, rec.Code, rec.Body)
		}
	}
}
