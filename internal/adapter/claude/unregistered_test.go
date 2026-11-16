package claude

import (
	"os"
	"os/exec"
	"path/filepath"
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
