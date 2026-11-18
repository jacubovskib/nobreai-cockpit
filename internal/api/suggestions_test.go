package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

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
