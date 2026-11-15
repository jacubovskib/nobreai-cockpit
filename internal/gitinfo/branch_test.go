package gitinfo

import (
	"os/exec"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepoOn(t *testing.T, branch string, commit bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não instalado")
	}
	d := t.TempDir()
	gitT(t, d, "init", "-q", "-b", branch)
	if commit {
		gitT(t, d, "commit", "-q", "--allow-empty", "-m", "init")
	}
	return d
}

func TestDetectBasePrefersDevelop(t *testing.T) {
	d := newRepoOn(t, "main", true)
	gitT(t, d, "branch", "develop")
	if got := (Exec{}).DetectBase(d); got != "develop" {
		t.Fatalf("base = %q", got)
	}
}

func TestDetectBaseMainThenMaster(t *testing.T) {
	if got := (Exec{}).DetectBase(newRepoOn(t, "main", true)); got != "main" {
		t.Fatalf("main: %q", got)
	}
	if got := (Exec{}).DetectBase(newRepoOn(t, "master", true)); got != "master" {
		t.Fatalf("master: %q", got)
	}
	if got := (Exec{}).DetectBase(newRepoOn(t, "main", false)); got != "" {
		t.Fatalf("sem commits: %q", got)
	}
}

func TestBranchTipAndIsAncestor(t *testing.T) {
	d := newRepoOn(t, "main", true)
	gitT(t, d, "checkout", "-q", "-b", "feat")
	gitT(t, d, "commit", "-q", "--allow-empty", "-m", "feat")
	g := Exec{}
	tip, ok := g.BranchTip(d, "feat")
	if !ok || len(tip) != 40 {
		t.Fatalf("tip = %q %v", tip, ok)
	}
	if g.IsAncestor(d, tip, "main") {
		t.Fatal("feat ainda não está em main")
	}
	gitT(t, d, "checkout", "-q", "main")
	gitT(t, d, "merge", "-q", "--no-ff", "-m", "merge", "feat")
	if !g.IsAncestor(d, tip, "main") {
		t.Fatal("feat deveria estar em main")
	}
	if tip, ok := g.BranchTip(d, "nao-existe"); ok || tip != "" {
		t.Fatalf("inexistente = %q %v", tip, ok)
	}
	if g.IsAncestor(d, tip, "nao-existe") {
		t.Fatal("ref inexistente deve dar false")
	}
}
