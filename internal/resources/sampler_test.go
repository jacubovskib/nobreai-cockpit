package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeProcFS struct {
	t    *testing.T
	root string
}

func (f fakeProcFS) uptime(sec float64) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, "uptime"), []byte(fmt.Sprintf("%.2f 0.00\n", sec)), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// proc grava (ou regrava) um processo; ticks = utime, start em ticks.
func (f fakeProcFS) proc(pid, ppid int, comm, cwd string, ticks, start, rssPages int) {
	f.t.Helper()
	dir := filepath.Join(f.root, fmt.Sprint(pid))
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (%s) S %d 1 1 0 -1 0 0 0 0 0 %d 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, ticks, start)
	must(f.t, os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644))
	must(f.t, os.WriteFile(filepath.Join(dir, "statm"), []byte(fmt.Sprintf("1 %d 0 0 0 0 0\n", rssPages)), 0o644))
	must(f.t, os.Symlink(cwd, filepath.Join(dir, "cwd")))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSamplerCPU(t *testing.T) {
	fs := fakeProcFS{t, t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	s := NewSampler(fs.root, func() time.Time { return now })

	fs.uptime(1000)
	fs.proc(10, 1, "node", "/w/a", 5000, 100, 100) // começou há muito tempo
	fs.proc(11, 1, "pid-reusado", "/w/a", 900, 200, 1)
	s.Sample()
	if _, ready := s.Snapshot(); ready {
		t.Fatal("com uma amostra só ainda não há CPU")
	}

	now = now.Add(5 * time.Second)
	fs.uptime(1005)
	fs.proc(10, 1, "node", "/w/a", 5250, 100, 100) // +250 ticks em 5 s = 50%
	fs.proc(11, 1, "outro", "/w/a", 30, 100150, 1) // mesmo PID, outro processo: nasceu agora
	fs.proc(12, 1, "tsc", "/w/a", 400, 100250, 1)  // nasceu durante o intervalo: 400 ticks = 80%
	fs.proc(13, 1, "velho", "/w/a", 7000, 50, 1)   // existia mas não foi visto: sem CPU
	s.Sample()
	procs, ready := s.Snapshot()
	if !ready {
		t.Fatal("deveria estar pronto")
	}
	cpu := map[int]float64{}
	for _, p := range procs {
		cpu[p.PID] = p.CPU
	}
	want := map[int]float64{10: 50, 11: 6, 12: 80, 13: 0}
	for pid, w := range want {
		if cpu[pid] != w {
			t.Fatalf("CPU de %d = %v, quer %v (todos: %v)", pid, cpu[pid], w, cpu)
		}
	}
	if len(procs) != 4 || procs[0].RSS == 0 {
		t.Fatalf("procs = %+v", procs)
	}
}

// procC grava um processo com utime e cutime separados.
func (f fakeProcFS) procC(pid, ppid int, cwd string, utime, cutime, start int) {
	f.t.Helper()
	dir := filepath.Join(f.root, fmt.Sprint(pid))
	_ = os.RemoveAll(dir)
	must(f.t, os.MkdirAll(dir, 0o755))
	stat := fmt.Sprintf("%d (p) S %d 1 1 0 -1 0 0 0 0 0 %d 0 %d 0 20 0 1 0 %d 0 0\n", pid, ppid, utime, cutime, start)
	must(f.t, os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644))
	must(f.t, os.WriteFile(filepath.Join(dir, "statm"), []byte("1 1 0 0 0 0 0\n"), 0o644))
	must(f.t, os.Symlink(cwd, filepath.Join(dir, "cwd")))
}

func TestSamplerReapedChildCountsOnce(t *testing.T) {
	fs := fakeProcFS{t, t.TempDir()}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	s := NewSampler(fs.root, func() time.Time { return now })
	step := func(up float64) {
		now = now.Add(5 * time.Second)
		fs.uptime(up)
		s.Sample()
	}
	cpuOf := func(pid int) float64 {
		procs, _ := s.Snapshot()
		for _, p := range procs {
			if p.PID == pid {
				return p.CPU
			}
		}
		t.Fatalf("pid %d sumiu", pid)
		return 0
	}

	fs.uptime(1000)
	fs.procC(10, 1, "/w", 1000, 0, 100)
	s.Sample()
	fs.procC(12, 10, "/w", 300, 0, 100100) // filho visto com 300 ticks
	step(1005)
	if c := cpuOf(12); c != 60 {
		t.Fatalf("filho = %v", c)
	}
	// Filho terminou e foi colhido: +320 no cutime do pai (300 já contados +
	// 20 do fim da vida dele) e +30 do próprio pai.
	_ = os.RemoveAll(filepath.Join(fs.root, "12"))
	fs.procC(10, 1, "/w", 1030, 320, 100)
	step(1010)
	if c := cpuOf(10); c != 10 { // 30 + 20 ticks em 5 s
		t.Fatalf("pai após colher o filho = %v, quer 10", c)
	}
	// Filho que nasceu e morreu dentro do intervalo (nunca visto) conta uma vez, pelo pai.
	fs.procC(10, 1, "/w", 1030, 420, 100)
	step(1015)
	if c := cpuOf(10); c != 20 {
		t.Fatalf("pai com filho nunca visto = %v, quer 20", c)
	}
}
