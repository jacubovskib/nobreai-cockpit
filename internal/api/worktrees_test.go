package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resources"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitRepoWithWorktree cria um repositório (branch main) e um worktree ligado
// no branch feat/wt, fora do diretório do repositório.
func gitRepoWithWorktree(t *testing.T) (main, wt string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não instalado")
	}
	real := func(d string) string {
		r, err := filepath.EvalSymlinks(d)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	main = real(t.TempDir())
	gitRun(t, main, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "add", ".")
	gitRun(t, main, "commit", "-q", "-m", "init")
	wt = filepath.Join(real(t.TempDir()), "api-feat")
	gitRun(t, main, "worktree", "add", "-q", "-b", "feat/wt", wt)
	return main, wt
}

type worktreesResp []struct {
	Project struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
	Error     string `json:"error"`
	Worktrees []struct {
		Path     string `json:"path"`
		Branch   string `json:"branch"`
		Main     bool   `json:"main"`
		Prunable bool   `json:"prunable"`
		Sessions []struct {
			ID           string `json:"id"`
			Live         bool   `json:"live"`
			LastActivity string `json:"last_activity"`
			Card         *struct {
				Key string `json:"key"`
			} `json:"card"`
		} `json:"sessions"`
		Cards []struct {
			Key string `json:"key"`
		} `json:"cards"`
	} `json:"worktrees"`
	Orphans []struct {
		ID       string `json:"id"`
		Worktree string `json:"worktree"`
	} `json:"orphans"`
}

func worktreesAPI(t *testing.T) *testAPI {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	h := New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Git: gitinfo.Exec{}})
	return &testAPI{st: st, h: h, hub: hub}
}

func TestWorktreesJoinsSessionsAndCards(t *testing.T) {
	a := worktreesAPI(t)
	main, wt := gitRepoWithWorktree(t)
	p, err := a.st.CreateProject("api", main)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.CreateProject("solto", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	feat, err := a.st.CreateCard(domain.Card{Title: "feat", ProjectID: p.ID, Branch: "feat/wt"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := a.st.CreateCard(domain.Card{Title: "antigo", ProjectID: p.ID, Branch: "main", Column: domain.Concluido})
	if err != nil {
		t.Fatal(err)
	}
	_ = old

	start := func(id, cwd, worktree string, linked bool) {
		t.Helper()
		x := domain.Session{ID: id, Agent: "claude", PID: 100, Cwd: cwd, Worktree: worktree, WorktreeLinked: linked,
			ProjectID: &p.ID, StartedAt: testNow.Add(-3 * time.Hour), LastEventAt: testNow.Add(-time.Minute)}
		if err := a.st.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	// Viva no worktree ligado, dona do card feat, com uma edição.
	start("claude:wt", filepath.Join(wt, "pkg"), wt, true)
	if err := a.st.AttachSession("claude:wt", feat.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := a.st.AddEvent(domain.Event{SessionID: "claude:wt", Kind: domain.EvFileEdit, Data: []byte(`{"path":"pkg/x.go"}`), At: testNow}); err != nil {
		t.Fatal(err)
	}
	// Encerrada há 2 h no principal (aparece); encerrada há 3 dias (não).
	start("claude:recente", main, main, false)
	if err := a.st.EndSession("claude:recente", "exit", testNow.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	start("claude:velha", main, main, false)
	if err := a.st.EndSession("claude:velha", "exit", testNow.Add(-72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Sessão anterior à migração (sem worktree gravado): casa pelo cwd.
	start("claude:legado", filepath.Join(wt, "pkg"), "", false)
	// Viva num worktree que já foi removido.
	start("claude:orfa", "/tmp/sumiu", "/tmp/sumiu", true)

	rec := a.do(t, "GET", "/api/worktrees", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	got := decode[worktreesResp](t, rec)
	if len(got) != 2 {
		t.Fatalf("projetos = %d", len(got))
	}
	api, solto := got[0], got[1]
	if api.Project.ID != p.ID || api.Error != "" || len(api.Worktrees) != 2 {
		t.Fatalf("api = %+v", api)
	}
	if solto.Error == "" || len(solto.Worktrees) != 0 {
		t.Fatalf("projeto fora de repositório deveria trazer erro: %+v", solto)
	}

	m, w := api.Worktrees[0], api.Worktrees[1]
	if m.Path != main || !m.Main || m.Branch != "main" {
		t.Fatalf("principal = %+v", m)
	}
	if len(m.Sessions) != 1 || m.Sessions[0].ID != "claude:recente" || m.Sessions[0].Live {
		t.Fatalf("sessões do principal = %+v", m.Sessions)
	}
	if len(m.Cards) != 0 {
		t.Fatalf("card concluído não deveria aparecer: %+v", m.Cards)
	}

	if w.Path != wt || w.Main || w.Branch != "feat/wt" {
		t.Fatalf("ligado = %+v", w)
	}
	if len(w.Sessions) != 2 {
		t.Fatalf("sessões do ligado = %+v", w.Sessions)
	}
	s := w.Sessions[0] // vivas primeiro, a mais recente antes
	if s.ID != "claude:wt" && s.ID != "claude:legado" {
		t.Fatalf("sessão = %+v", s)
	}
	for _, s := range w.Sessions {
		if !s.Live {
			t.Fatalf("sessão %s deveria estar viva", s.ID)
		}
		if s.ID == "claude:wt" && (s.Card == nil || s.Card.Key != feat.Key || s.LastActivity != "Edit pkg/x.go") {
			t.Fatalf("sessão do worktree = %+v", s)
		}
	}
	if len(w.Cards) != 1 || w.Cards[0].Key != feat.Key {
		t.Fatalf("cards do branch = %+v", w.Cards)
	}

	if len(api.Orphans) != 1 || api.Orphans[0].ID != "claude:orfa" || api.Orphans[0].Worktree != "/tmp/sumiu" {
		t.Fatalf("órfãs = %+v", api.Orphans)
	}
}

func TestWorktreesWithoutGitReader(t *testing.T) {
	a := newTestAPI(t) // sem Git
	a.project(t)
	got := decode[worktreesResp](t, a.do(t, "GET", "/api/worktrees", nil, 0))
	if len(got) != 1 || got[0].Error == "" || got[0].Worktrees == nil || got[0].Orphans == nil {
		t.Fatalf("sem git = %+v", got)
	}
}

type fakeResources struct {
	procs []resources.Proc
	ready bool
	disk  map[string][]string // caminho medido → exclusões recebidas
}

func (f *fakeResources) Snapshot() ([]resources.Proc, bool) { return f.procs, f.ready }
func (f *fakeResources) Disk(path string, exclude []string) resources.DiskInfo {
	f.disk[path] = exclude
	at := testNow.Add(-4 * time.Minute)
	return resources.DiskInfo{Bytes: 3 << 30, MeasuredAt: &at}
}

type resourcesResp []struct {
	Worktrees []struct {
		Path      string `json:"path"`
		Resources *struct {
			Ready    bool    `json:"ready"`
			CPU      float64 `json:"cpu_pct"`
			RSS      int64   `json:"rss_bytes"`
			Procs    int     `json:"procs"`
			CPUCores int     `json:"cpu_cores"`
			Top      []struct {
				PID  int     `json:"pid"`
				Comm string  `json:"comm"`
				CPU  float64 `json:"cpu_pct"`
			} `json:"top"`
			Disk *struct {
				Bytes   int64 `json:"bytes"`
				Pending bool  `json:"pending"`
			} `json:"disk"`
		} `json:"resources"`
	} `json:"worktrees"`
}

func TestWorktreesResources(t *testing.T) {
	main, wt := gitRepoWithWorktree(t)
	nested := filepath.Join(main, ".claude", "worktrees", "x")
	gitRun(t, main, "worktree", "add", "-q", "-b", "exp/x", nested)
	fr := &fakeResources{ready: true, disk: map[string][]string{}, procs: []resources.Proc{
		{PID: 10, PPID: 1, Comm: "claude", Cwd: main, CPU: 4, RSS: 300 << 20},
		{PID: 20, PPID: 1, Comm: "node", Cwd: filepath.Join(wt, "web"), CPU: 120, RSS: 900 << 20},
		{PID: 21, PPID: 20, Comm: "esbuild", Cwd: "/tmp", CPU: 60, RSS: 100 << 20},
		{PID: 30, PPID: 1, Comm: "chrome", Cwd: "/home/u", CPU: 99, RSS: 5 << 30},
	}}
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &testAPI{st: st, h: New(st, Deps{Now: func() time.Time { return testNow }, Git: gitinfo.Exec{}, Resources: fr})}
	if _, err := st.CreateProject("api", main); err != nil {
		t.Fatal(err)
	}
	got := decode[resourcesResp](t, a.do(t, "GET", "/api/worktrees", nil, 0))
	if len(got) != 1 || len(got[0].Worktrees) != 3 {
		t.Fatalf("resp = %+v", got)
	}
	byPath := map[string]int{}
	for i, w := range got[0].Worktrees {
		byPath[w.Path] = i
		if w.Resources == nil || !w.Resources.Ready || w.Resources.CPUCores < 1 || w.Resources.Disk == nil || w.Resources.Disk.Bytes != 3<<30 {
			t.Fatalf("recursos de %s = %+v", w.Path, w.Resources)
		}
	}
	m := got[0].Worktrees[byPath[main]].Resources
	if m.CPU != 4 || m.RSS != 300<<20 || m.Procs != 1 {
		t.Fatalf("principal = %+v", m)
	}
	f := got[0].Worktrees[byPath[wt]].Resources
	if f.CPU != 180 || f.Procs != 2 || len(f.Top) != 2 || f.Top[0].Comm != "node" || f.Top[1].PID != 21 {
		t.Fatalf("ligado = %+v", f)
	}
	if n := got[0].Worktrees[byPath[nested]].Resources; n.Procs != 0 || n.Top == nil {
		t.Fatalf("aninhado sem processos = %+v", n)
	}
	// O principal não conta o worktree aninhado dentro dele.
	if ex := fr.disk[main]; len(ex) != 1 || ex[0] != nested {
		t.Fatalf("exclusões do principal = %v", ex)
	}
	if ex := fr.disk[wt]; len(ex) != 0 {
		t.Fatalf("exclusões do ligado = %v", ex)
	}
}

func TestWorktreesResourcesNullWithoutReader(t *testing.T) {
	a := worktreesAPI(t)
	main, _ := gitRepoWithWorktree(t)
	if _, err := a.st.CreateProject("api", main); err != nil {
		t.Fatal(err)
	}
	got := decode[resourcesResp](t, a.do(t, "GET", "/api/worktrees", nil, 0))
	for _, w := range got[0].Worktrees {
		if w.Resources != nil {
			t.Fatalf("sem leitor, resources deveria ser null: %+v", w.Resources)
		}
	}
}
