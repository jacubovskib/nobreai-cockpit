package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func git(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitOn(t *testing.T, dir, branch, file string, ago time.Duration) {
	t.Helper()
	git(t, dir, nil, "checkout", "-q", branch)
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	d := time.Now().Add(-ago).Format(time.RFC3339)
	git(t, dir, []string{"GIT_COMMITTER_DATE=" + d, "GIT_AUTHOR_DATE=" + d}, "add", ".")
	git(t, dir, []string{"GIT_COMMITTER_DATE=" + d, "GIT_AUTHOR_DATE=" + d}, "commit", "-q", "-m", "trabalho "+file)
}

func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não instalado")
	}
	d := t.TempDir()
	git(t, d, nil, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(d, "base.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, d, nil, "add", ".")
	git(t, d, nil, "commit", "-q", "-m", "init")
	return d
}

func TestScanFiltersBranches(t *testing.T) {
	d := newRepo(t)
	for _, b := range []string{"feat/a", "feat/velha", "backup/x", "feat/integrada"} {
		git(t, d, nil, "branch", b)
	}
	commitOn(t, d, "feat/a", "a.txt", 0)
	commitOn(t, d, "feat/velha", "v.txt", 40*24*time.Hour)
	commitOn(t, d, "backup/x", "b.txt", 0)
	git(t, d, nil, "checkout", "-q", "main")

	res, err := Run(d, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Branches) != 1 {
		t.Fatalf("branches = %+v", res.Branches)
	}
	b := res.Branches[0]
	if b.Name != "feat/a" || b.Ahead != 1 || !b.Recent || b.Subject != "trabalho a.txt" {
		t.Fatalf("branch = %+v", b)
	}
}

func TestScanPlans(t *testing.T) {
	d := newRepo(t)
	docs := filepath.Join(d, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, body := range map[string]string{
		"plan-x.md":      "- [ ] a\n- [x] b\n",
		"plan-pronto.md": "- [x] a\n",
		"notas.md":       "- [ ] a\n",
	} {
		if err := os.WriteFile(filepath.Join(docs, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Run(d, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plans) != 1 || res.Plans[0].Path != "docs/plan-x.md" || res.Plans[0].Open != 1 || res.Plans[0].Done != 1 {
		t.Fatalf("plans = %+v", res.Plans)
	}
}
