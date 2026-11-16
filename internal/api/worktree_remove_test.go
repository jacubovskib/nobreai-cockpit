package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resources"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

type removeResp struct {
	Error            string `json:"error"`
	Removed          bool   `json:"removed"`
	BranchDeleted    bool   `json:"branch_deleted"`
	BranchKeptReason string `json:"branch_kept_reason"`
	NeedsConfirm     bool   `json:"needs_confirm"`
	TotalFiles       int    `json:"total_files"`
	Files            []struct {
		Path string `json:"path"`
		Code string `json:"code"`
	} `json:"files"`
	Processes []struct {
		PID  int    `json:"pid"`
		Name string `json:"name"`
	} `json:"processes"`
}

func rmBody(pid int64, path string, force, delBranch bool) map[string]any {
	return map[string]any{"project": pid, "path": path, "force": force, "delete_branch": delBranch}
}

func listedPaths(t *testing.T, a *testAPI) map[string]bool {
	t.Helper()
	got := decode[worktreesResp](t, a.do(t, "GET", "/api/worktrees", nil, 0))
	m := map[string]bool{}
	for _, w := range got[0].Worktrees {
		m[w.Path] = true
	}
	return m
}

func rmSetup(t *testing.T) (a *testAPI, p *domain.Project, main, wt string) {
	t.Helper()
	a = worktreesAPI(t)
	main, wt = gitRepoWithWorktree(t)
	pr, err := a.st.CreateProject("api", main)
	if err != nil {
		t.Fatal(err)
	}
	return a, &pr, main, wt
}

func TestDeleteWorktreeClean(t *testing.T) {
	a, p, main, wt := rmSetup(t)
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	got := decode[removeResp](t, rec)
	if !got.Removed || got.BranchDeleted {
		t.Fatalf("resp = %+v", got)
	}
	l := listedPaths(t, a)
	if l[wt] || !l[main] {
		t.Fatalf("lista = %v", l)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("pasta deveria ter sido removida")
	}
}

func TestDeleteWorktreeRefusesMain(t *testing.T) {
	a, p, main, _ := rmSetup(t)
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, main, true, true), 0)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if e := decode[removeResp](t, rec).Error; e != "o checkout principal não pode ser excluído" {
		t.Fatalf("erro = %q", e)
	}
	if _, err := os.Stat(main); err != nil {
		t.Fatal("principal não pode ser tocado")
	}
}

func TestDeleteWorktreeRefusesLiveSession(t *testing.T) {
	a, p, _, wt := rmSetup(t)
	x := domain.Session{ID: "claude:live", Agent: "claude", PID: 100, Cwd: wt, Worktree: wt, WorktreeLinked: true,
		ProjectID: &p.ID, StartedAt: testNow.Add(-time.Hour), LastEventAt: testNow.Add(-time.Minute)}
	if err := a.st.StartSession(x); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, true), 0)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if e := decode[removeResp](t, rec).Error; e != "há uma sessão viva neste worktree; encerre-a antes de excluir" {
		t.Fatalf("erro = %q", e)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("pasta não pode ser removida")
	}
}

func TestDeleteWorktreeRefusesLockedAndOldLiveSession(t *testing.T) {
	a, p, main, wt := rmSetup(t)
	// Sessão viva, mas parada há mais de 24 h: continua valendo.
	x := domain.Session{ID: "claude:velha", Agent: "claude", PID: 100, Cwd: wt, Worktree: wt, WorktreeLinked: true,
		ProjectID: &p.ID, StartedAt: testNow.Add(-72 * time.Hour), LastEventAt: testNow.Add(-72 * time.Hour)}
	if err := a.st.StartSession(x); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0); rec.Code != http.StatusConflict {
		t.Fatalf("sessão viva antiga: code = %d %s", rec.Code, rec.Body)
	}
	if err := a.st.EndSession("claude:velha", "exit", testNow); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "worktree", "lock", "--reason", "pendrive", wt)
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0)
	if rec.Code != http.StatusConflict {
		t.Fatalf("travado: code = %d %s", rec.Code, rec.Body)
	}
	if e := decode[removeResp](t, rec).Error; e != "worktree travado (pendrive); destrave com git worktree unlock" {
		t.Fatalf("erro = %q", e)
	}
}

func TestDeleteWorktreeNeedsConfirm(t *testing.T) {
	a, p, _, wt := rmSetup(t)
	if err := os.WriteFile(filepath.Join(wt, "novo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	got := decode[removeResp](t, rec)
	if !got.NeedsConfirm || got.TotalFiles != 1 || len(got.Files) != 1 || got.Files[0].Path != "novo.txt" || got.Files[0].Code != "??" {
		t.Fatalf("resp = %+v", got)
	}
	if got.Error != "o worktree tem alterações não commitadas ou processos rodando; confirme para excluir mesmo assim" {
		t.Fatalf("erro = %q", got.Error)
	}
	if !listedPaths(t, a)[wt] {
		t.Fatal("nada deveria ter sido removido")
	}
	rec = a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("force: code = %d %s", rec.Code, rec.Body)
	}
	if listedPaths(t, a)[wt] {
		t.Fatal("com force deveria ter sumido")
	}
}

func TestDeleteWorktreeNeedsConfirmWithProcesses(t *testing.T) {
	main, wt := gitRepoWithWorktree(t)
	fr := &fakeResources{ready: true, disk: map[string][]string{}, procs: []resources.Proc{
		{PID: 20, PPID: 1, Comm: "node", Cwd: filepath.Join(wt, "web"), CPU: 1, RSS: 1 << 20},
	}}
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &testAPI{st: st, h: New(st, Deps{Now: func() time.Time { return testNow }, Git: gitinfo.Exec{}, Resources: fr})}
	p, err := st.CreateProject("api", main)
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	got := decode[removeResp](t, rec)
	if !got.NeedsConfirm || len(got.Processes) != 1 || got.Processes[0].PID != 20 || got.Processes[0].Name != "node" {
		t.Fatalf("resp = %+v", got)
	}
}

func TestDeleteWorktreeBranchOnlyIfMerged(t *testing.T) {
	// Integrado: o branch feat/wt aponta para o mesmo commit de main.
	a, p, main, wt := rmSetup(t)
	got := decode[removeResp](t, a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, true), 0))
	if !got.Removed || !got.BranchDeleted || got.BranchKeptReason != "" {
		t.Fatalf("integrado = %+v", got)
	}
	if _, ok := (gitinfo.Exec{}).BranchTip(main, "feat/wt"); ok {
		t.Fatal("branch deveria ter sido apagado")
	}

	// Não integrado: commit só no branch.
	a, p, main, wt = rmSetup(t)
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt, "add", ".")
	gitRun(t, wt, "commit", "-q", "-m", "wip")
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, true), 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	got = decode[removeResp](t, rec)
	if !got.Removed || got.BranchDeleted || got.BranchKeptReason != "não está integrado em main" {
		t.Fatalf("não integrado = %+v", got)
	}
	if _, ok := (gitinfo.Exec{}).BranchTip(main, "feat/wt"); !ok {
		t.Fatal("branch não integrado precisa continuar existindo")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("a pasta deveria ter sido removida")
	}
}

func TestDeleteWorktreeBranchIsBase(t *testing.T) {
	a, p, main, _ := rmSetup(t)
	other := filepath.Join(t.TempDir(), "on-main")
	gitRun(t, main, "checkout", "-q", "-b", "outro")
	gitRun(t, main, "worktree", "add", "-q", other, "main")
	got := decode[removeResp](t, a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, other, false, true), 0))
	if !got.Removed || got.BranchDeleted || got.BranchKeptReason != "é a base do projeto" {
		t.Fatalf("resp = %+v", got)
	}
}

func TestDeleteWorktreePrunable(t *testing.T) {
	a, p, _, wt := rmSetup(t)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if listedPaths(t, a)[wt] {
		t.Fatal("registro deveria ter sido limpo")
	}
}

func TestDeleteWorktreeUnknownPath404(t *testing.T) {
	a, p, _, wt := rmSetup(t)
	other := t.TempDir()
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, other, true, true), 0)
	if rec.Code != http.StatusNotFound || decode[removeResp](t, rec).Error != "worktree não encontrado" {
		t.Fatalf("desconhecido: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("pasta fora da lista não pode ser tocada")
	}
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("1ª: %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0); rec.Code != http.StatusNotFound {
		t.Fatalf("repetido: %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(999, wt, false, false), 0); rec.Code != http.StatusNotFound ||
		decode[removeResp](t, rec).Error != "projeto não encontrado" {
		t.Fatalf("projeto: %d %s", rec.Code, rec.Body)
	}
}

func startLive(t *testing.T, a *testAPI, id string, pid *int64, cwd, worktree string) {
	t.Helper()
	x := domain.Session{ID: id, Agent: "claude", PID: 100, Cwd: cwd, Worktree: worktree,
		ProjectID: pid, StartedAt: testNow.Add(-time.Hour), LastEventAt: testNow.Add(-time.Minute)}
	if err := a.st.StartSession(x); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteWorktreeRefusesNestedWorktree(t *testing.T) {
	a, p, main, wt := rmSetup(t)
	nested := filepath.Join(wt, ".worktrees", "w2")
	gitRun(t, main, "worktree", "add", "-q", "-b", "w2", nested)
	if err := os.WriteFile(filepath.Join(nested, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	startLive(t, a, "claude:nested", &p.ID, nested, nested)
	for _, force := range []bool{false, true} {
		rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, force, true), 0)
		if rec.Code != http.StatusConflict || decode[removeResp](t, rec).Error != "há outro worktree dentro deste ("+nested+"); exclua-o antes" {
			t.Fatalf("force=%v: %d %s", force, rec.Code, rec.Body)
		}
	}
	for _, d := range []string{wt, filepath.Join(nested, "wip.txt")} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("%s deveria continuar: %v", d, err)
		}
	}
}

func TestDeleteWorktreeLiveSessionVariants(t *testing.T) {
	cases := map[string]func(a *testAPI, p *domain.Project, wt string){
		"outro projeto": func(a *testAPI, p *domain.Project, wt string) {
			o, err := a.st.CreateProject("outro", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			startLive(t, a, "claude:x", &o.ID, wt, wt)
		},
		"sem projeto": func(a *testAPI, p *domain.Project, wt string) { startLive(t, a, "claude:x", nil, wt, "") },
		"cwd em subdiretório": func(a *testAPI, p *domain.Project, wt string) {
			startLive(t, a, "claude:x", &p.ID, filepath.Join(wt, "pkg"), wt)
		},
		"legado sem worktree": func(a *testAPI, p *domain.Project, wt string) {
			startLive(t, a, "claude:x", &p.ID, filepath.Join(wt, "pkg"), "")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			a, p, _, wt := rmSetup(t)
			setup(a, p, wt)
			rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0)
			if rec.Code != http.StatusConflict {
				t.Fatalf("code = %d %s", rec.Code, rec.Body)
			}
			if _, err := os.Stat(wt); err != nil {
				t.Fatal("pasta não pode ser removida")
			}
		})
	}
}

func TestDeleteWorktreeProcessesUnknown(t *testing.T) {
	main, wt := gitRepoWithWorktree(t)
	fr := &fakeResources{ready: false, disk: map[string][]string{}}
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &testAPI{st: st, h: New(st, Deps{Now: func() time.Time { return testNow }, Git: gitinfo.Exec{}, Resources: fr})}
	p, err := st.CreateProject("api", main)
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	var got struct {
		Error   string `json:"error"`
		Unknown bool   `json:"processes_unknown"`
		Confirm bool   `json:"needs_confirm"`
	}
	got = decode[struct {
		Error   string `json:"error"`
		Unknown bool   `json:"processes_unknown"`
		Confirm bool   `json:"needs_confirm"`
	}](t, rec)
	if rec.Code != http.StatusConflict || !got.Unknown || !got.Confirm || got.Error != "não consegui verificar os processos agora; confirme para excluir mesmo assim" {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}
}

func TestDeleteWorktreeDetachedKeepsBranchReason(t *testing.T) {
	a, p, main, _ := rmSetup(t)
	det := filepath.Join(t.TempDir(), "det")
	gitRun(t, main, "worktree", "add", "-q", "--detach", det)
	got := decode[removeResp](t, a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, det, false, true), 0))
	if !got.Removed || got.BranchDeleted || got.BranchKeptReason != "o worktree não tem branch (HEAD destacado)" {
		t.Fatalf("resp = %+v", got)
	}
}
