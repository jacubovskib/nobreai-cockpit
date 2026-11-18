package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

func suggestionRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"apps/web", "apps/api", "packages/ui"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-workspace.yaml"), []byte("packages:\n  - 'apps/*'\n  - packages/ui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSuggestionsHideRegisteredAndDismissed(t *testing.T) {
	a := newTestAPI(t)
	root := suggestionRepo(t)
	p, err := a.st.CreateProject("loja", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.st.CreateChild("web", filepath.Join(root, "apps/web"), p.ID); err != nil {
		t.Fatal(err)
	}
	base := "/api/projects/" + strconv.FormatInt(p.ID, 10) + "/suggestions"
	if rec := a.do(t, "GET", base, nil, 0); rec.Code != http.StatusOK || len(decode[[]workspaces.Suggestion](t, rec)) != 2 {
		t.Fatalf("antes de recusar = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", base+"/dismiss", map[string]any{"rels": []string{"apps/api"}}, 0); rec.Code != http.StatusNoContent {
		t.Fatalf("dismiss = %d %s", rec.Code, rec.Body)
	}
	rec := a.do(t, "GET", base, nil, 0)
	got := decode[[]workspaces.Suggestion](t, rec)
	if len(got) != 1 || got[0].Rel != "packages/ui" {
		t.Fatalf("sugestões = %s", rec.Body)
	}
	// O levantamento traz a mesma lista.
	if rec := a.do(t, "GET", "/api/projects/999/suggestions", nil, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("projeto inexistente = %d", rec.Code)
	}
}

func TestSuggestionsEmptyIsArrayAndAddReturnsThem(t *testing.T) {
	a := newTestAPI(t)
	plain := t.TempDir()
	rec := a.do(t, "POST", "/api/projects", map[string]string{"name": "x", "path": plain}, 0)
	if rec.Code != http.StatusCreated || decode[struct{ Suggestions []workspaces.Suggestion }](t, rec).Suggestions == nil {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	root := suggestionRepo(t)
	rec = a.do(t, "POST", "/api/projects", map[string]string{"name": "loja", "path": root}, 0)
	if got := decode[struct{ Suggestions []workspaces.Suggestion }](t, rec).Suggestions; rec.Code != http.StatusCreated || len(got) != 3 {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
}

// gitScanAPI é uma API com git de verdade e um repositório com workspace pnpm
// (apps/web, apps/api, packages/ui), para exercitar o /scan.
func gitScanAPI(t *testing.T) (*testAPI, string) {
	t.Helper()
	root, _ := gitRepoWithWorktree(t)
	for _, d := range []string{"apps/web", "apps/api", "packages/ui"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-workspace.yaml"), []byte("packages:\n  - 'apps/*'\n  - packages/ui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	return &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Git: gitinfo.Exec{}})}, root
}

func TestSuggestionsSurviveCorruptDismissedValue(t *testing.T) {
	a, root := gitScanAPI(t)
	p, err := a.st.CreateProject("loja", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.DB.Exec(`INSERT INTO settings(key, value) VALUES (?, ?)`, dismissedKey, "{não é json"); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(p.ID, 10)
	rec := a.do(t, "GET", "/api/projects/"+id+"/suggestions", nil, 0)
	if rec.Code != http.StatusOK || len(decode[[]workspaces.Suggestion](t, rec)) != 3 {
		t.Fatalf("sugestões = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "GET", "/api/projects/"+id+"/scan", nil, 0)
	if rec.Code != http.StatusOK || len(decode[struct{ Suggestions []workspaces.Suggestion }](t, rec).Suggestions) != 3 {
		t.Fatalf("scan = %d %s", rec.Code, rec.Body)
	}
}

func TestScanOfASubprojectHasNoSuggestions(t *testing.T) {
	a, root := gitScanAPI(t)
	p, err := a.st.CreateProject("loja", root)
	if err != nil {
		t.Fatal(err)
	}
	web, _, err := a.st.CreateChild("web", filepath.Join(root, "apps/web"), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10)+"/scan", nil, 0)
	if rec.Code != http.StatusOK || len(decode[struct{ Suggestions []workspaces.Suggestion }](t, rec).Suggestions) != 2 {
		t.Fatalf("scan do pai = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "GET", "/api/projects/"+strconv.FormatInt(web.ID, 10)+"/scan", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("scan do app = %d %s", rec.Code, rec.Body)
	}
	if got := decode[struct{ Suggestions []workspaces.Suggestion }](t, rec).Suggestions; got == nil || len(got) != 0 {
		t.Fatalf("sugestões do app = %s", rec.Body)
	}
}

func TestGroupWithFolderSuggestsAndDismissesByGroupID(t *testing.T) {
	a := newTestAPI(t)
	g := groupWithFolder(t, a)
	for _, d := range []string{"a", "b", "docs"} {
		if err := os.MkdirAll(filepath.Join(g.Path, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(g.Path, d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	base := "/api/projects/" + strconv.FormatInt(g.ID, 10) + "/suggestions"
	rec := a.do(t, "GET", base, nil, 0)
	if got := decode[[]workspaces.Suggestion](t, rec); rec.Code != http.StatusOK || len(got) != 2 {
		t.Fatalf("sugestões do grupo = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", base+"/dismiss", map[string]any{"rels": []string{"b"}}, 0); rec.Code != http.StatusNoContent {
		t.Fatalf("dismiss = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "GET", base, nil, 0)
	if got := decode[[]workspaces.Suggestion](t, rec); len(got) != 1 || got[0].Rel != "a" {
		t.Fatalf("depois de recusar = %s", rec.Body)
	}
}

func TestPathlessGroupSuggestionsStay400(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "POST", "/api/projects", map[string]string{"name": "g"}, 0)
	g := decode[struct{ ID int64 }](t, rec)
	if rec := a.do(t, "GET", "/api/projects/"+strconv.FormatInt(g.ID, 10)+"/suggestions", nil, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("grupo sem pasta = %d", rec.Code)
	}
}
