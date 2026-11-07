package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// realDir resolve symlinks: o git devolve caminhos já resolvidos.
func realDir(t *testing.T, dir string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// repoWithWorktree cria um repositório com um commit e um worktree ligado,
// fora do diretório do repositório, no branch feat/wt.
func repoWithWorktree(t *testing.T) (main, wt string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não instalado")
	}
	main = realDir(t, t.TempDir())
	run(t, main, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, main, "add", ".")
	run(t, main, "commit", "-q", "-m", "init")
	wt = filepath.Join(realDir(t, t.TempDir()), "repo-feat")
	run(t, main, "worktree", "add", "-q", "-b", "feat/wt", wt)
	return main, wt
}

func TestLocate(t *testing.T) {
	main, wt := repoWithWorktree(t)
	g := Exec{}

	if got := g.Locate(main); got != (Location{Root: main, Main: main}) {
		t.Fatalf("Locate(main) = %+v", got)
	}
	sub := filepath.Join(wt, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := g.Locate(sub); got != (Location{Root: wt, Main: main, Linked: true}) {
		t.Fatalf("Locate(subdir do worktree) = %+v", got)
	}
	if got := g.Locate(t.TempDir()); got != (Location{}) {
		t.Fatalf("Locate(fora de repo) = %+v", got)
	}
	if got := g.Locate(""); got != (Location{}) {
		t.Fatalf("Locate(\"\") = %+v", got)
	}
}

func TestWorktrees(t *testing.T) {
	main, wt := repoWithWorktree(t)
	g := Exec{}
	run(t, main, "worktree", "lock", wt)

	// De qualquer worktree, a lista é a mesma e começa pelo principal.
	for _, from := range []string{main, wt} {
		got, err := g.Worktrees(from)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("Worktrees(%s) = %+v", from, got)
		}
		m, w := got[0], got[1]
		if m.Path != main || !m.Main || m.Branch != "main" || len(m.Head) != 40 || m.Locked {
			t.Fatalf("principal = %+v", m)
		}
		if w.Path != wt || w.Main || w.Branch != "feat/wt" || w.Head != m.Head || !w.Locked || w.Detached {
			t.Fatalf("ligado = %+v", w)
		}
	}

	run(t, main, "worktree", "unlock", wt)
	run(t, wt, "checkout", "-q", "--detach")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	got, err := g.Worktrees(main)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[1].Detached || got[1].Branch != "" || !got[1].Prunable {
		t.Fatalf("após apagar o diretório = %+v", got)
	}

	if _, err := g.Worktrees(t.TempDir()); err == nil {
		t.Fatal("fora de repo deveria dar erro")
	}
}

func TestParseWorktrees(t *testing.T) {
	out := "worktree /r\nHEAD aaaa\nbranch refs/heads/main\n\n" +
		"worktree /r-x\nHEAD bbbb\ndetached\nlocked motivo qualquer\n\n" +
		"worktree /bare\nbare\n"
	got := parseWorktrees(out)
	want := []Worktree{
		{Path: "/r", Head: "aaaa", Branch: "main", Main: true},
		{Path: "/r-x", Head: "bbbb", Detached: true, Locked: true, LockReason: "motivo qualquer"},
		{Path: "/bare", Bare: true},
	}
	if len(got) != len(want) {
		t.Fatalf("parseWorktrees = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %+v, quer %+v", i, got[i], want[i])
		}
	}
}

func TestInspect(t *testing.T) {
	main, wt := repoWithWorktree(t)
	g := Exec{}
	// Um commit a mais no worktree, um arquivo novo, um modificado e um preparado.
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, wt, "add", "b.txt")
	run(t, wt, "commit", "-q", "-m", "adiciona b\n\ncorpo ignorado")
	if err := os.WriteFile(filepath.Join(wt, "novo.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("mudou\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, wt, "add", "c.txt")

	in, err := g.Inspect(wt, "main", "feat/wt", 5)
	if err != nil {
		t.Fatal(err)
	}
	if in.Ahead != 1 || in.Behind != 0 || !in.Compared {
		t.Fatalf("à frente/atrás = %+v", in)
	}
	if in.Staged != 1 || in.Modified != 1 || in.Untracked != 1 || in.Conflicts != 0 || in.TotalFiles != 3 {
		t.Fatalf("status = %+v", in)
	}
	if len(in.Files) != 3 {
		t.Fatalf("arquivos = %+v", in.Files)
	}
	if len(in.Commits) != 2 || in.Commits[0].Subject != "adiciona b" || in.Commits[0].Author != "t" || len(in.Commits[0].Hash) != 40 || in.Commits[0].At.IsZero() {
		t.Fatalf("commits = %+v", in.Commits)
	}

	// Sem base (worktree principal ou HEAD destacado): não compara.
	in, err = g.Inspect(main, "", "main", 5)
	if err != nil || in.Compared || in.TotalFiles != 0 {
		t.Fatalf("principal = %+v %v", in, err)
	}
	// Nomes de branch inválidos não viram opção do git.
	if in, err := g.Inspect(wt, "--output=/tmp/x", "feat/wt", 5); err != nil || in.Compared {
		t.Fatalf("base inválida = %+v %v", in, err)
	}
	if _, err := g.Inspect(t.TempDir(), "", "", 5); err == nil {
		t.Fatal("fora de repositório deveria dar erro")
	}
}

func TestParseWorktreeReasons(t *testing.T) {
	got := parseWorktrees("worktree /r\nHEAD a\nbranch refs/heads/main\n\nworktree /x\nHEAD b\nbranch refs/heads/x\nlocked disco externo\nprunable gitdir file points to non-existent location\n")
	if len(got) != 2 || got[1].LockReason != "disco externo" || got[1].PruneReason != "gitdir file points to non-existent location" || !got[1].Locked || !got[1].Prunable {
		t.Fatalf("motivos = %+v", got)
	}
}
