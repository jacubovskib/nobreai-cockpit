package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Revisão final do NAC-10: cada teste cobre um achado.

type rmConfirmResp struct {
	Error            string `json:"error"`
	NeedsConfirm     bool   `json:"needs_confirm"`
	GitUnknown       bool   `json:"git_unknown"`
	ProcessesUnknown bool   `json:"processes_unknown"`
	TotalFiles       int    `json:"total_files"`
	Files            []struct {
		Path string `json:"path"`
		Code string `json:"code"`
	} `json:"files"`
}

func mustExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s deveria continuar existindo: %v", p, err)
		}
	}
}

func commitIn(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", file)
}

func drainKinds(ch <-chan string) map[string]bool {
	got := map[string]bool{}
	for {
		select {
		case k := <-ch:
			got[k] = true
		default:
			return got
		}
	}
}

// I1: status.showUntrackedFiles=no não pode esconder arquivos novos.
func TestDeleteWorktreeSeesUntrackedWithShowUntrackedNo(t *testing.T) {
	a, p, main, wt := rmSetup(t)
	gitRun(t, main, "config", "status.showUntrackedFiles", "no")
	novo := filepath.Join(wt, "novo.txt")
	if err := os.WriteFile(novo, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	got := decode[rmConfirmResp](t, rec)
	if rec.Code != http.StatusConflict || !got.NeedsConfirm || got.TotalFiles != 1 || got.Files[0].Path != "novo.txt" || got.Files[0].Code != "??" {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	mustExist(t, novo)
}

// I2: integrado pela regra do Cockpit, o branch sai por compare-and-delete,
// mesmo quando git branch -d recusaria.
func TestDeleteWorktreeBranchIntegratedCases(t *testing.T) {
	t.Run("integrado só em origin/main, sem upstream", func(t *testing.T) {
		a, p, main, wt := rmSetup(t)
		commitIn(t, wt, "b.txt")
		tip, _ := (gitinfo.Exec{}).BranchTip(main, "feat/wt")
		gitRun(t, main, "update-ref", "refs/remotes/origin/main", tip)
		ch, cancel := a.hub.Subscribe()
		defer cancel()
		got := decode[removeResp](t, a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, true), 0))
		if !got.Removed || !got.BranchDeleted || got.BranchKeptReason != "" {
			t.Fatalf("resp = %+v", got)
		}
		if _, ok := (gitinfo.Exec{}).BranchTip(main, "feat/wt"); ok {
			t.Fatal("branch deveria ter sido apagado")
		}
		if k := drainKinds(ch); !k["cards"] || !k["sessions"] {
			t.Fatalf("publicou %v; esperava cards e sessions", k)
		}
	})
	t.Run("integrado em main local com o principal em outro branch", func(t *testing.T) {
		a, p, main, wt := rmSetup(t)
		commitIn(t, wt, "b.txt")
		gitRun(t, main, "checkout", "-q", "-b", "outro")
		gitRun(t, main, "branch", "-f", "main", "feat/wt")
		got := decode[removeResp](t, a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, true), 0))
		if !got.Removed || !got.BranchDeleted || got.BranchKeptReason != "" {
			t.Fatalf("resp = %+v", got)
		}
		if _, ok := (gitinfo.Exec{}).BranchTip(main, "feat/wt"); ok {
			t.Fatal("branch deveria ter sido apagado")
		}
	})
	t.Run("branch em uso em outro worktree", func(t *testing.T) {
		a, p, main, wt := rmSetup(t)
		other := filepath.Join(realTempDir(t), "outro")
		gitRun(t, main, "worktree", "add", "-q", "--force", other, "feat/wt")
		ch, cancel := a.hub.Subscribe()
		defer cancel()
		got := decode[removeResp](t, a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, true), 0))
		if !got.Removed || got.BranchDeleted || got.BranchKeptReason != "o branch está em uso em outro worktree ("+other+")" {
			t.Fatalf("resp = %+v", got)
		}
		if _, ok := (gitinfo.Exec{}).BranchTip(main, "feat/wt"); !ok {
			t.Fatal("branch em uso precisa continuar")
		}
		if k := drainKinds(ch); k["cards"] {
			t.Fatal("sem branch apagado não publica cards")
		}
	})
}

// M3: a pasta de um projeto cadastrado não pode ir junto.
func TestDeleteWorktreeRefusesRegisteredProjectInside(t *testing.T) {
	for _, sub := range []string{"", "pkg"} {
		t.Run("sub="+sub, func(t *testing.T) {
			a, p, _, wt := rmSetup(t)
			dir := filepath.Join(wt, sub)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := a.st.CreateProject("dentro", dir); err != nil {
				t.Fatal(err)
			}
			rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, true), 0)
			if rec.Code != http.StatusConflict || decode[removeResp](t, rec).Error != "a pasta de um projeto cadastrado está aqui (dentro); remova o projeto antes" {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			mustExist(t, dir)
		})
	}
}

// failStatus é o git real, mas sem conseguir ler o status.
type failStatus struct{ gitinfo.Exec }

func (failStatus) FullStatus(string) (gitinfo.Inspection, error) {
	return gitinfo.Inspection{}, errors.New("index corrompido")
}
func (failStatus) Inspect(string, string, string, int) (gitinfo.Inspection, error) {
	return gitinfo.Inspection{}, errors.New("index corrompido")
}

// M4: sem conseguir ler o git, pede confirmação em vez de seguir.
func TestDeleteWorktreeGitUnknown(t *testing.T) {
	main, wt := gitRepoWithWorktree(t)
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &testAPI{st: st, h: New(st, Deps{Now: func() time.Time { return testNow }, Git: failStatus{}})}
	p, err := st.CreateProject("api", main)
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	got := decode[rmConfirmResp](t, rec)
	if rec.Code != http.StatusConflict || !got.NeedsConfirm || !got.GitUnknown || got.Error != "não consegui ler o estado do git agora; confirme para excluir mesmo assim" {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	mustExist(t, wt)
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}
}

// M5: worktree sem pasta limpa só o próprio registro, não todos os prunable.
func TestDeleteWorktreeMissingOnlyThatRecord(t *testing.T) {
	a, p, main, wt := rmSetup(t)
	wt2 := filepath.Join(filepath.Dir(wt), "api-feat2")
	gitRun(t, main, "worktree", "add", "-q", "-b", "feat/wt2", wt2)
	os.RemoveAll(wt)
	os.RemoveAll(wt2)
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	l := listedPaths(t, a)
	if l[wt] || !l[wt2] {
		t.Fatalf("lista = %v", l)
	}
}

// M6: prunable com a pasta ainda lá (sem .git) não é "pasta sumiu": nada de
// prune silencioso, e a pasta com os dados fica.
func TestDeleteWorktreePrunableFolderStillThere(t *testing.T) {
	a, p, _, wt := rmSetup(t)
	if err := os.Remove(filepath.Join(wt, ".git")); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(wt, "dados.txt")
	if err := os.WriteFile(data, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	got := decode[rmConfirmResp](t, rec)
	if rec.Code != http.StatusConflict || !got.NeedsConfirm || !got.GitUnknown {
		t.Fatalf("sem force: %d %+v", rec.Code, got)
	}
	rec = a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0)
	e := decode[removeResp](t, rec).Error
	if rec.Code != http.StatusConflict || e != "o git não reconhece mais esta pasta como worktree; confira e apague à mão se for o caso" {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}
	mustExist(t, data)
	if !listedPaths(t, a)[wt] {
		t.Fatal("o registro não pode sumir com a pasta ainda lá")
	}
}

// M7: submódulos pedem confirmação, não 500.
func TestDeleteWorktreeSubmodulesNeedConfirm(t *testing.T) {
	a, p, _, wt := rmSetup(t)
	sub, _ := gitRepoWithWorktree(t)
	gitRun(t, wt, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	gitRun(t, wt, "commit", "-q", "-m", "sub")
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	got := decode[rmConfirmResp](t, rec)
	if rec.Code != http.StatusConflict || !got.NeedsConfirm || got.Error != "o worktree tem submódulos; confirme para excluir mesmo assim" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	mustExist(t, wt)
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}
}

// Lista do git ilegível: 502 com o motivo, não 404.
func TestDeleteWorktreeListFailure502(t *testing.T) {
	a := worktreesAPI(t)
	dir := t.TempDir()
	p, err := a.st.CreateProject("solto", dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, filepath.Join(dir, "x"), false, false), 0)
	e := decode[removeResp](t, rec).Error
	if rec.Code != http.StatusBadGateway || !strings.HasPrefix(e, "não consegui listar os worktrees de ") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
