package gitinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func worktreePaths(t *testing.T, repo string) map[string]bool {
	t.Helper()
	list, err := (Exec{}).Worktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, w := range list {
		m[w.Path] = true
	}
	return m
}

func TestRemoveWorktreeAndPrune(t *testing.T) {
	d := newRepoOn(t, "main", true)
	d, _ = filepath.EvalSymlinks(d)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	g := Exec{}

	clean := filepath.Join(base, "clean")
	gitT(t, d, "worktree", "add", "-q", "-b", "clean", clean)
	if err := g.RemoveWorktree(d, clean, false); err != nil {
		t.Fatal(err)
	}
	if worktreePaths(t, d)[clean] {
		t.Fatal("worktree limpo deveria sumir da lista")
	}

	dirty := filepath.Join(base, "dirty")
	gitT(t, d, "worktree", "add", "-q", "-b", "dirty", dirty)
	if err := os.WriteFile(filepath.Join(dirty, "novo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.RemoveWorktree(d, dirty, false); err == nil {
		t.Fatal("sem force deveria recusar worktree com arquivo novo")
	}
	if !worktreePaths(t, d)[dirty] {
		t.Fatal("worktree sujo não pode sumir sem force")
	}
	if err := g.RemoveWorktree(d, dirty, true); err != nil {
		t.Fatal(err)
	}
	if worktreePaths(t, d)[dirty] {
		t.Fatal("com force deveria sumir")
	}

	gone := filepath.Join(base, "gone")
	gitT(t, d, "worktree", "add", "-q", "-b", "gone", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if err := g.PruneWorktrees(d); err != nil {
		t.Fatal(err)
	}
	if worktreePaths(t, d)[gone] {
		t.Fatal("prune deveria limpar o registro")
	}
	if err := g.RemoveWorktree(d, "-x", false); err == nil {
		t.Fatal("caminho com hífen deveria ser recusado")
	}
}

func TestDeleteBranchSafe(t *testing.T) {
	d := newRepoOn(t, "main", true)
	g := Exec{}
	gitT(t, d, "branch", "merged")
	if err := g.DeleteBranch(d, "merged"); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.BranchTip(d, "merged"); ok {
		t.Fatal("branch integrado deveria ter sido apagado")
	}

	gitT(t, d, "checkout", "-q", "-b", "wip")
	gitT(t, d, "commit", "-q", "--allow-empty", "-m", "wip")
	gitT(t, d, "checkout", "-q", "main")
	if err := g.DeleteBranch(d, "wip"); err == nil {
		t.Fatal("branch não integrado não pode ser apagado (nunca -D)")
	}
	if _, ok := g.BranchTip(d, "wip"); !ok {
		t.Fatal("branch não integrado deveria continuar existindo")
	}
	if err := g.DeleteBranch(d, "-x"); err == nil {
		t.Fatal("nome com hífen deveria ser recusado")
	}
}
