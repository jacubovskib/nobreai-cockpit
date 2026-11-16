package gitinfo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestDeleteBranchAtComparesTip(t *testing.T) {
	d := newRepoOn(t, "main", true)
	g := Exec{}
	gitT(t, d, "branch", "merged")
	tip, _ := g.BranchTip(d, "merged")

	// Ponta diferente da verificada: o git não apaga.
	gitT(t, d, "commit", "-q", "--allow-empty", "-m", "depois")
	other, _ := g.BranchTip(d, "main")
	if err := g.DeleteBranchAt(d, "merged", other); err == nil {
		t.Fatal("ponta diferente não pode apagar")
	} else if strings.Contains(err.Error(), "\n") || strings.Contains(err.Error(), "cannot lock") {
		t.Fatalf("erro deve ser uma linha em pt-BR: %q", err)
	}
	if _, ok := g.BranchTip(d, "merged"); !ok {
		t.Fatal("branch deveria continuar")
	}

	for _, bad := range []struct{ branch, tip string }{
		{"-x", tip}, {"merged", "HEAD"}, {"merged", tip[:7]}, {"merged", "-" + tip[1:]}, {"merged", strings.ToUpper(tip)},
	} {
		if err := g.DeleteBranchAt(d, bad.branch, bad.tip); err == nil {
			t.Fatalf("%q %q deveria ser recusado", bad.branch, bad.tip)
		}
	}
	if _, ok := g.BranchTip(d, "merged"); !ok {
		t.Fatal("branch deveria continuar")
	}

	if err := g.DeleteBranchAt(d, "merged", tip); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.BranchTip(d, "merged"); ok {
		t.Fatal("branch deveria ter sido apagado")
	}
}

// Com status.showUntrackedFiles=no o git status esconde arquivos novos; nem a
// leitura nem o worktree remove podem deixar de vê-los.
func TestUntrackedSeenWithShowUntrackedNo(t *testing.T) {
	d := newRepoOn(t, "main", true)
	d, _ = filepath.EvalSymlinks(d)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	g := Exec{}
	gitT(t, d, "config", "status.showUntrackedFiles", "no")
	wt := filepath.Join(base, "wt")
	gitT(t, d, "worktree", "add", "-q", "-b", "wt", wt)
	novo := filepath.Join(wt, "novo.txt")
	if err := os.WriteFile(novo, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("FullStatus", func(t *testing.T) {
		in, err := g.FullStatus(wt)
		if err != nil {
			t.Fatal(err)
		}
		if in.TotalFiles != 1 || in.Files[0].Path != "novo.txt" || in.Files[0].Code != "??" {
			t.Fatalf("status = %+v", in)
		}
	})

	if err := g.RemoveWorktree(d, wt, false); err == nil {
		t.Fatal("sem force deveria recusar worktree com arquivo novo")
	}
	if _, err := os.Stat(novo); err != nil {
		t.Fatal("o arquivo novo precisa sobreviver")
	}
}

func TestRemoveWorktreeSubmodulesNeedForce(t *testing.T) {
	d := newRepoOn(t, "main", true)
	d, _ = filepath.EvalSymlinks(d)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	sub := newRepoOn(t, "main", true)
	g := Exec{}
	wt := filepath.Join(base, "wt")
	gitT(t, d, "worktree", "add", "-q", "-b", "wt", wt)
	gitT(t, wt, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	gitT(t, wt, "commit", "-q", "-m", "sub")

	if err := g.RemoveWorktree(d, wt, false); !errors.Is(err, ErrSubmodules) {
		t.Fatalf("erro = %v", err)
	}
	if !worktreePaths(t, d)[wt] {
		t.Fatal("sem force não pode remover")
	}
	if err := g.RemoveWorktree(d, wt, true); err != nil {
		t.Fatal(err)
	}
}

// O worktree remove de um worktree sem diretório limpa só aquele registro.
func TestRemoveMissingWorktreeOnlyThatRecord(t *testing.T) {
	d := newRepoOn(t, "main", true)
	d, _ = filepath.EvalSymlinks(d)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	g := Exec{}
	g1, g2 := filepath.Join(base, "g1"), filepath.Join(base, "g2")
	gitT(t, d, "worktree", "add", "-q", "-b", "g1", g1)
	gitT(t, d, "worktree", "add", "-q", "-b", "g2", g2)
	os.RemoveAll(g1)
	os.RemoveAll(g2)
	if err := g.RemoveWorktree(d, g1, false); err != nil {
		t.Fatal(err)
	}
	l := worktreePaths(t, d)
	if l[g1] || !l[g2] {
		t.Fatalf("lista = %v", l)
	}
}

func TestGitWriteErrorIsOneLine(t *testing.T) {
	d := newRepoOn(t, "main", true)
	err := (Exec{}).RemoveWorktree(d, filepath.Join(t.TempDir(), "nada"), false)
	if err == nil || strings.Contains(err.Error(), "\n") || strings.HasPrefix(err.Error(), "fatal:") {
		t.Fatalf("erro = %q", err)
	}
}
