package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func fakeProc(t *testing.T, root string, pid, ppid int, comm string) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (%s) S %d 1 1 0 -1 0\n", pid, comm, ppid)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindAgentAncestor(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, 100, 1, "claude")
	fakeProc(t, root, 200, 100, "bash")
	fakeProc(t, root, 300, 200, "nobre")

	got, err := FindAgentAncestor(root, 300, []string{"claude"})
	if err != nil || got != 100 {
		t.Fatalf("got %d, %v; want 100", got, err)
	}
	if _, err := FindAgentAncestor(root, 300, []string{"codex"}); err == nil {
		t.Fatal("esperava erro sem match")
	}
}

func TestFindAgentAncestorCommWithSpaces(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, 100, 1, "my agent) x")
	fakeProc(t, root, 200, 100, "sh")
	got, err := FindAgentAncestor(root, 200, []string{"my agent) x"})
	if err != nil || got != 100 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestFindAgentAncestorCycle(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, 10, 20, "a")
	fakeProc(t, root, 20, 10, "b")
	if _, err := FindAgentAncestor(root, 10, []string{"x"}); err == nil {
		t.Fatal("esperava erro em ciclo")
	}
}

func TestAlive(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, 100, 1, "claude")
	if !Alive(root, 100) || Alive(root, 101) || Alive(root, 0) {
		t.Fatal("Alive incorreto")
	}
}
