package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitInfoTempRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não instalado")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "init")

	g := Exec{}
	if b := g.Branch(dir); b != "main" {
		t.Fatalf("Branch = %q", b)
	}
	head := g.Head(dir)
	if len(head) != 40 {
		t.Fatalf("Head = %q", head)
	}
	if !g.Clean(dir) {
		t.Fatal("esperava árvore limpa")
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g.Clean(dir) {
		t.Fatal("esperava árvore suja após arquivo novo")
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\nmais\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ds := g.DiffStat(dir, head); ds == "" {
		t.Fatal("DiffStat vazio após editar a.txt")
	}

	run(t, dir, "stash", "-u", "-q")
	run(t, dir, "branch", "feat")
	if err := g.Checkout(dir, "feat"); err != nil {
		t.Fatal(err)
	}
	if b := g.Branch(dir); b != "feat" {
		t.Fatalf("Branch após checkout = %q", b)
	}
	if err := g.Checkout(dir, "nao-existe"); err == nil {
		t.Fatal("esperava erro ao trocar para branch inexistente")
	}

	other := t.TempDir()
	if g.Branch(other) != "" || g.Head(other) != "" || g.Clean(other) {
		t.Fatal("diretório fora de repo deveria dar vazio / não limpo")
	}
}
