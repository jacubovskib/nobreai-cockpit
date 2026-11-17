package api

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

type confirmExtra struct {
	NeedsConfirm bool `json:"needs_confirm"`
	Commits      []struct {
		Hash    string `json:"hash"`
		Subject string `json:"subject"`
	} `json:"commits"`
	TotalCommits int  `json:"total_commits"`
	Rebase       bool `json:"rebase_in_progress"`
	RepoSessions int  `json:"repo_sessions"`
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// Commits que só existem no HEAD destacado do worktree se perderiam com a
// pasta (o reflog dele vai junto): exigem confirmação, listados.
func TestDeleteWorktreeDetachedCommitsNeedConfirm(t *testing.T) {
	a, p, main, _ := rmSetup(t)
	wd := filepath.Join(filepath.Dir(main), "solto-"+filepath.Base(main))
	gitRun(t, main, "worktree", "add", "-q", "--detach", wd)
	os.WriteFile(filepath.Join(wd, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, wd, "add", ".")
	gitRun(t, wd, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "trabalho solto")

	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wd, false, false), 0)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	var got confirmExtra
	json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.NeedsConfirm || got.TotalCommits != 1 || len(got.Commits) != 1 || got.Commits[0].Subject != "trabalho solto" {
		t.Fatalf("resp = %s", rec.Body)
	}
	if _, err := os.Stat(wd); err != nil {
		t.Fatal("pasta não pode ser removida sem confirmação")
	}
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wd, true, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}
}

// Um worktree em branch não tem commits soltos: nada a confirmar por isso.
func TestDeleteWorktreeBranchCommitsAreNotOrphans(t *testing.T) {
	a, p, _, wt := rmSetup(t)
	os.WriteFile(filepath.Join(wt, "c.txt"), []byte("c\n"), 0o644)
	gitRun(t, wt, "add", ".")
	gitRun(t, wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "no branch")
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}

// Sessão viva em outro lugar do mesmo repositório (um subagente dela pode
// estar neste worktree sem que o registro mostre): pede confirmação.
func TestDeleteWorktreeLiveSessionInSameRepoNeedsConfirm(t *testing.T) {
	a, p, main, wt := rmSetup(t)
	x := domain.Session{ID: "claude:principal", Agent: "claude", PID: os.Getpid(), Cwd: main, Worktree: main,
		ProjectID: &p.ID, StartedAt: testNow.Add(-time.Hour), LastEventAt: testNow.Add(-time.Minute)}
	if err := a.st.StartSession(x); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, false), 0)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	var got confirmExtra
	json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.NeedsConfirm || got.RepoSessions != 1 {
		t.Fatalf("resp = %s", rec.Body)
	}
	if rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, true, false), 0); rec.Code != http.StatusOK {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}
}

// Apagar o branch integrado também tira a seção [branch "x"] da config: um
// branch novo com o mesmo nome não herda o rastreamento antigo.
func TestDeleteWorktreeBranchRemovesConfigSection(t *testing.T) {
	a, p, main, wt := rmSetup(t)
	gitRun(t, main, "config", "branch.feat/wt.remote", "origin")
	gitRun(t, main, "config", "branch.feat/wt.merge", "refs/heads/feat/wt")
	rec := a.do(t, "DELETE", "/api/worktrees", rmBody(p.ID, wt, false, true), 0)
	if rec.Code != http.StatusOK || !decode[removeResp](t, rec).BranchDeleted {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if out, _ := exec.Command("git", "-C", main, "config", "--get-regexp", `^branch\.feat/wt\.`).Output(); len(out) != 0 {
		t.Fatalf("config ficou: %s", out)
	}
	_ = gitOut
}
