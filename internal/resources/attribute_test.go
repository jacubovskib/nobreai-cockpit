package resources

import (
	"testing"
)

func TestAttribute(t *testing.T) {
	paths := []string{"/w/api", "/w/api/.claude/worktrees/x", "/w/api-feat"}
	procs := []Proc{
		{PID: 1, PPID: 0, Comm: "systemd", Cwd: "/"},
		{PID: 10, PPID: 1, Comm: "claude", Cwd: "/w/api", CPU: 5, RSS: 300},
		{PID: 11, PPID: 10, Comm: "bash", Cwd: "/w/api/.claude/worktrees/x/src", CPU: 1, RSS: 10},
		{PID: 12, PPID: 11, Comm: "go", Cwd: "/tmp/go-build1", CPU: 80, RSS: 900}, // sem cwd em worktree: herda do pai (x)
		{PID: 20, PPID: 1, Comm: "node", Cwd: "/w/api-feat", CPU: 30, RSS: 500},
		{PID: 21, PPID: 20, Comm: "esbuild", Cwd: "/w/api-feat", CPU: 40, RSS: 100},
		{PID: 22, PPID: 20, Comm: "sh", Cwd: "/w/api-featx", CPU: 2, RSS: 1}, // prefixo sem "/": não é do api-feat... herda do pai
		{PID: 30, PPID: 1, Comm: "chrome", Cwd: "/home/u", CPU: 50, RSS: 5000},
	}
	got := Attribute(procs, paths)
	if len(got) != 3 {
		t.Fatalf("Attribute = %+v", got)
	}
	main := got["/w/api"]
	if main.CPU != 5 || main.RSS != 300 || main.Procs != 1 {
		t.Fatalf("principal = %+v", main)
	}
	x := got["/w/api/.claude/worktrees/x"]
	if x.CPU != 81 || x.RSS != 910 || x.Procs != 2 {
		t.Fatalf("aninhado = %+v", x)
	}
	if len(x.Top) != 2 || x.Top[0].Comm != "go" || x.Top[1].Comm != "bash" {
		t.Fatalf("top do aninhado = %+v", x.Top)
	}
	feat := got["/w/api-feat"]
	if feat.CPU != 72 || feat.Procs != 3 {
		t.Fatalf("api-feat = %+v", feat)
	}
	if len(feat.Top) != 3 || feat.Top[0].Comm != "esbuild" {
		t.Fatalf("top do api-feat = %+v", feat.Top)
	}
}

func TestAttributeTopLimitAndCycles(t *testing.T) {
	var procs []Proc
	for i := range 6 {
		procs = append(procs, Proc{PID: 100 + i, PPID: 1, Comm: "p", Cwd: "/w", CPU: float64(i), RSS: int64(i)})
	}
	// Ciclo de ppid (não acontece no /proc, mas não pode travar).
	procs = append(procs, Proc{PID: 7, PPID: 8, Cwd: "/x"}, Proc{PID: 8, PPID: 7, Cwd: "/y"})
	u := Attribute(procs, []string{"/w"})["/w"]
	if u.Procs != 6 || len(u.Top) != MaxTop || u.Top[0].CPU != 5 {
		t.Fatalf("u = %+v", u)
	}
}
