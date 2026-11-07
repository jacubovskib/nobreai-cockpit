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
