package agentcore

import (
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

// repoGit diz que todo diretório é o repositório /w/repo (lê worktrees).
type repoGit struct{ fakeGit }

func (repoGit) Locate(string) gitinfo.Location {
	return gitinfo.Location{Root: "/w/repo", Main: "/w/repo"}
}
func (repoGit) Worktrees(string) ([]gitinfo.Worktree, error) { return nil, nil }

// guideDriver cita o guia do nobre instalado no agente (Guide).
type guideDriver struct{ fakeDriver }

func (d *guideDriver) GuideName() string { return "o guia nobre" }

// unregisteredText é o aviso do início de uma sessão num repositório que não
// está no board.
func unregisteredText(t *testing.T, d Driver, fd *fakeDriver) string {
	t.Helper()
	e := newEnv(t, d)
	e.c.Deps().Git = repoGit{fakeGit{branch: "main"}}
	e.handle(t, fd, Event{Kind: Started, Cwd: "/w/repo"})
	ctx := lastReply(t, fd).Context
	if len(ctx) != 1 || !strings.HasPrefix(ctx[0], UnregisteredPhrase+" (/w/repo).") {
		t.Fatalf("contexto = %q", ctx)
	}
	return ctx[0]
}

func TestUnregisteredCitesDriverGuide(t *testing.T) {
	g := &guideDriver{}
	if got := unregisteredText(t, g, &g.fakeDriver); !strings.Contains(got, "nobre card add (veja o guia nobre); se o cadastro") {
		t.Fatalf("com Guide = %q", got)
	}
	f := &fakeDriver{}
	got := unregisteredText(t, f, f)
	if !strings.Contains(got, "nobre card add; se o cadastro") || strings.Contains(got, "(veja") {
		t.Fatalf("sem Guide = %q", got)
	}
}
