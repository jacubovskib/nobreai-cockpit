package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fullProc escreve um processo completo: stat com tempos e starttime, statm e
// o symlink cwd (cwd "" = sem cwd legível).
func fullProc(t *testing.T, root string, pid, ppid int, comm, cwd string, utime, stime, cutime, cstime, start, rssPages int) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Campos após ")": estado ppid pgrp session tty tpgid flags minflt cminflt
	// majflt cmajflt utime stime cutime cstime priority nice threads itreal starttime.
	stat := fmt.Sprintf("%d (%s) S %d 1 1 0 -1 0 0 0 0 0 %d %d %d %d 20 0 1 0 %d 0 0\n", pid, comm, ppid, utime, stime, cutime, cstime, start)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "statm"), []byte(fmt.Sprintf("9999 %d 10 1 0 5 0\n", rssPages)), 0o644); err != nil {
		t.Fatal(err)
	}
	if cwd != "" {
		if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadProcs(t *testing.T) {
	root := t.TempDir()
	fullProc(t, root, 10, 1, "node (vite)", "/w/api", 100, 20, 7, 3, 5000, 256)
	fullProc(t, root, 11, 10, "bash", "/w/api/src (deleted)", 1, 1, 0, 0, 5001, 10)
	fullProc(t, root, 12, 1, "kworker", "", 0, 0, 0, 0, 1, 0) // sem cwd legível
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	ps := ReadProcs(root)
	if len(ps) != 2 {
		t.Fatalf("ReadProcs = %+v", ps)
	}
	byPID := map[int]Proc{}
	for _, p := range ps {
		byPID[p.PID] = p
	}
	n := byPID[10]
	if n.PPID != 1 || n.Comm != "node (vite)" || n.Cwd != "/w/api" || n.Ticks != 130 || n.Start != 5000 || n.RSS != 256*int64(os.Getpagesize()) {
		t.Fatalf("node = %+v", n)
	}
	if b := byPID[11]; b.Cwd != "/w/api/src" || b.PPID != 10 {
		t.Fatalf("bash = %+v", b)
	}
}
