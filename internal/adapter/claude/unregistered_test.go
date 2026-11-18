package claude

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não instalado")
	}
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", d, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "init", "-q", "-b", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return d
}

// realGit troca o git do env por um que lê worktrees de verdade.
func (e *env) realGit() {
	e.a.deps.Git = gitinfo.Exec{}
}

func TestSessionStartUnregisteredRepoWarns(t *testing.T) {
	e := newEnv(t)
	e.realGit()
	repo := gitRepo(t)
	e.project(t)
	ctx := additionalContext(t, e.call(t, "session_start", with("cwd", repo)))
	if !strings.HasPrefix(ctx, unregisteredPhrase) || !strings.Contains(ctx, "nobre project add "+repo) {
		t.Fatalf("contexto = %q", ctx)
	}
	if !strings.HasSuffix(ctx, guidance) {
		t.Fatalf("contexto não termina nas instruções: %q", ctx)
	}
	if strings.Contains(ctx, registeredPhrase) {
		t.Fatalf("aviso de não cadastrado não pode dizer que a sessão é registrada: %q", ctx)
	}
}

func TestSessionStartHomeNoWarning(t *testing.T) {
	e := newEnv(t)
	e.realGit()
	repo := gitRepo(t)
	t.Setenv("HOME", repo)
	if r := e.call(t, "session_start", with("cwd", repo)); r.Stdout != "" {
		t.Fatalf("HOME não avisa: %q", r.Stdout)
	}
}

func TestSessionStartNonGitNoWarning(t *testing.T) {
	e := newEnv(t)
	e.realGit()
	dir := t.TempDir()
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Fatal("inesperado")
	}
	if r := e.call(t, "session_start", with("cwd", dir)); r.Stdout != "" {
		t.Fatalf("fora de repo não avisa: %q", r.Stdout)
	}
}

func TestSessionStartIgnoredProjectNoWarning(t *testing.T) {
	e := newEnv(t)
	e.realGit()
	repo := gitRepo(t)
	p, err := e.st.CreateProject("r", repo)
	if err != nil {
		t.Fatal(err)
	}
	p.Capture = false
	if err := e.st.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	if r := e.call(t, "session_start", with("cwd", repo)); r.Stdout != "" {
		t.Fatalf("projeto ignorado não avisa: %q", r.Stdout)
	}
}

// I2: com agent_creates_cards desligado, o aviso de não cadastrado mantém o
// cadastro e o levantamento, mas só lista os cards sugeridos ao usuário.
func TestSessionStartUnregisteredWithoutAgentCreatesCards(t *testing.T) {
	e := newEnv(t)
	e.realGit()
	repo := gitRepo(t)
	e.project(t)
	set := domain.DefaultSettings()
	set.AgentCreatesCards = false
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start", with("cwd", repo)))
	for _, want := range []string{unregisteredPhrase, "nobre project add " + repo, "nobre project scan",
		"liste ao usuário os cards sugeridos (não crie sem ele pedir)", "Só crie card quando o usuário pedir"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("contexto sem %q: %q", want, ctx)
		}
	}
	if strings.Contains(ctx, "crie os cards importantes") || strings.Contains(ctx, "quando começar algo que precisa de histórico") {
		t.Fatalf("contexto manda criar cards: %q", ctx)
	}
}

func TestUnregisteredNonGitFolderWithRepos(t *testing.T) {
	e := newEnv(t)
	e.realGit()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "api", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start", with("cwd", dir)))
	if !strings.HasPrefix(ctx, unregisteredFolderPhrase) || !strings.Contains(ctx, "nobre project add "+dir) {
		t.Fatalf("contexto = %q", ctx)
	}
	if strings.Contains(ctx, "nobre project scan") {
		t.Fatalf("grupo não tem levantamento: %q", ctx)
	}

	// Sem repositórios no primeiro nível: sem aviso.
	empty, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(empty, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := e.call(t, "session_start", with("cwd", empty)); r.Stdout != "" {
		t.Fatalf("pasta sem repositórios não avisa: %q", r.Stdout)
	}

	// A home nunca recebe aviso, mesmo com repositórios dentro.
	t.Setenv("HOME", dir)
	if r := e.call(t, "session_start", with("cwd", dir)); r.Stdout != "" {
		t.Fatalf("HOME não avisa: %q", r.Stdout)
	}
}

func TestUnregisteredRepoInsideGroupSuggestsPai(t *testing.T) {
	e := newEnv(t)
	e.realGit()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := e.st.CreateGroupAt("meus", dir)
	if err != nil {
		t.Fatal(err)
	}
	novo := filepath.Join(dir, "novo")
	if err := os.MkdirAll(novo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", novo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	ctx := additionalContext(t, e.call(t, "session_start", with("cwd", novo), with("session_id", "s1")))
	if !strings.Contains(ctx, "--pai") || !strings.Contains(ctx, registeredPhrase) {
		t.Fatalf("contexto = %q", ctx)
	}
	sess, err := e.st.GetSession("claude:s1")
	if err != nil || sess.ProjectID == nil || *sess.ProjectID != g.ID {
		t.Fatalf("sessão = %+v err=%v", sess, err)
	}

	if err := e.st.PutKV("subproject_dismissed", map[string][]string{strconv.FormatInt(g.ID, 10): {"novo"}}); err != nil {
		t.Fatal(err)
	}
	ctx = additionalContext(t, e.call(t, "session_start", with("cwd", novo), with("session_id", "s2")))
	if strings.Contains(ctx, "--pai") {
		t.Fatalf("recusado não avisa: %q", ctx)
	}
}
