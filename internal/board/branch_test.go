package board

import (
	"path/filepath"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

type fakeBranches struct{ base string }

func (f fakeBranches) DetectBase(string) string                { return f.base }
func (f fakeBranches) BranchTip(string, string) (string, bool) { return "", false }
func (f fakeBranches) IsAncestor(string, string, string) bool  { return false }

func TestSyncCardBranchIgnoresBase(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, _ := st.CreateProject("api", "/w/api")
	if err := st.SetProjectBase(p.ID, "main"); err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateCard(domain.Card{Title: "x", ProjectID: p.ID, Branch: "feat/a"})
	if err != nil {
		t.Fatal(err)
	}
	g := fakeBranches{base: "develop"}
	for _, b := range []string{"main", "", "HEAD"} {
		if ch, err := SyncCardBranch(st, g, c.ID, b); err != nil || ch {
			t.Fatalf("Sync(%q) = %v, %v", b, ch, err)
		}
	}
	if got, _ := st.GetCardByID(c.ID); got.Branch != "feat/a" {
		t.Fatalf("branch = %q", got.Branch)
	}
	if ch, err := SyncCardBranch(st, g, c.ID, "feat/x"); err != nil || !ch {
		t.Fatalf("Sync(feat/x) = %v, %v", ch, err)
	}
	if got, _ := st.GetCardByID(c.ID); got.Branch != "feat/x" {
		t.Fatalf("branch = %q", got.Branch)
	}
	// Sem base configurada, vale a detectada.
	if err := st.SetProjectBase(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if ch, _ := SyncCardBranch(st, g, c.ID, "develop"); ch {
		t.Fatal("base detectada deveria ser ignorada")
	}
	// Sem git, só "" e HEAD são ignorados.
	if ch, _ := SyncCardBranch(st, nil, c.ID, "develop"); !ch {
		t.Fatal("sem git deveria sincronizar")
	}
}

func TestRepoBaseUsesParent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	parent, _ := st.CreateProject("mono", "/w/mono")
	if err := st.SetProjectBase(parent.ID, "main"); err != nil {
		t.Fatal(err)
	}
	parent.BaseBranch = "main"
	app, _, err := st.CreateChild("web", "/w/mono/web", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetProjectBase(app.ID, "x"); err != nil {
		t.Fatal(err)
	}
	app.BaseBranch = "x"
	g := fakeBranches{base: "develop"}
	if got := RepoBase(st, g, app); got != "main" {
		t.Fatalf("RepoBase(app) = %q, want main", got)
	}
	if got := RepoBase(st, g, parent); got != "main" {
		t.Fatalf("RepoBase(parent) = %q, want main", got)
	}
	// Um filho de grupo tem repositório próprio: usa a própria base.
	group, _ := st.CreateGroup("grupo")
	kid, _, err := st.CreateChild("svc", "/w/svc", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := RepoBase(st, g, kid); got != "develop" {
		t.Fatalf("RepoBase(filho de grupo) = %q, want develop", got)
	}
}
