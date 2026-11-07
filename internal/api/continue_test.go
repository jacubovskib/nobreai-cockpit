package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

type fakeLauncher struct{ calls [][]string }

func (f *fakeLauncher) Start(argv []string) error { f.calls = append(f.calls, argv); return nil }

type fakeGit struct {
	branch    string
	clean     bool
	checkouts []string
}

func (g *fakeGit) Branch(string) string        { return g.branch }
func (g *fakeGit) Head(string) string          { return "" }
func (g *fakeGit) DiffStat(_, _ string) string { return "" }
func (g *fakeGit) Clean(string) bool           { return g.clean }
func (g *fakeGit) Checkout(_, b string) error {
	g.checkouts = append(g.checkouts, b)
	g.branch = b
	return nil
}

// continueAPI monta a API com lançador e git falsos; devolve o card NAC-1
// (branch "feat/x") num projeto em /w/api.
func continueAPI(t *testing.T, g *fakeGit) (*testAPI, *fakeLauncher) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	ad := claude.New(st, adapter.Deps{Now: func() time.Time { return testNow }})
	l := &fakeLauncher{}
	a := &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub,
		Adapters: map[string]adapter.Adapter{"claude": ad}, Launcher: l, Git: g,
		ResolveTerminal: func(n string) (string, error) {
			if n == "x-terminal-emulator" {
				return "/usr/bin/terminator", nil
			}
			return "", errors.New("não achei")
		}})}
	p, err := st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(domain.Card{Title: "OAuth2", ProjectID: p.ID, Branch: "feat/x"}); err != nil {
		t.Fatal(err)
	}
	return a, l
}

func errOf(t *testing.T, a *testAPI, code int, body map[string]any) string {
	t.Helper()
	rec := a.do(t, "POST", "/api/cards/NAC-1/continue", body, 0)
	if rec.Code != code {
		t.Fatalf("code = %d (quero %d) %s", rec.Code, code, rec.Body)
	}
	return decode[map[string]string](t, rec)["error"]
}

func TestContinueRefusesDirtyTree(t *testing.T) {
	g := &fakeGit{branch: "main", clean: false}
	a, l := continueAPI(t, g)
	got := errOf(t, a, http.StatusConflict, map[string]any{"agent": "claude"})
	if got != "O repositório tem mudanças não salvas; faça commit ou stash antes de continuar." {
		t.Fatalf("erro = %q", got)
	}
	if len(l.calls) != 0 || len(g.checkouts) != 0 {
		t.Fatal("não deveria lançar nem trocar de branch")
	}
}

func TestContinueRefusesLiveSessionUnlessForced(t *testing.T) {
	g := &fakeGit{branch: "feat/x", clean: true}
	a, l := continueAPI(t, g)
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	got := errOf(t, a, http.StatusConflict, map[string]any{"agent": "claude"})
	if got != "NAC-1 está com uma sessão viva de Claude Code. Encerre-a ou use forçar." {
		t.Fatalf("erro = %q", got)
	}
	rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"agent": "claude"}, 0)
	if decode[map[string]string](t, rec)["code"] != "live_session" {
		t.Fatalf("faltou code: %s", rec.Body)
	}
	if len(l.calls) != 0 {
		t.Fatal("lançou apesar da sessão viva")
	}
	if rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"agent": "claude", "force": true}, 0); rec.Code != http.StatusOK {
		t.Fatalf("force = %d %s", rec.Code, rec.Body)
	}
	if len(l.calls) != 1 {
		t.Fatalf("lançamentos = %d", len(l.calls))
	}
}

func TestContinueStartsWithEnv(t *testing.T) {
	g := &fakeGit{branch: "main", clean: true}
	a, l := continueAPI(t, g)
	rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"agent": "claude", "mode": "platform", "prompt_slug": ""}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if len(g.checkouts) != 1 || g.checkouts[0] != "feat/x" {
		t.Fatalf("checkouts = %v", g.checkouts)
	}
	if len(l.calls) != 1 {
		t.Fatalf("lançamentos = %d", len(l.calls))
	}
	argv := l.calls[0]
	last := argv[len(argv)-1]
	if argv[0] != "/usr/bin/terminator" || argv[1] != "-e" || len(argv) != 3 ||
		!strings.Contains(last, "cd ") || !strings.Contains(last, "/w/api") ||
		!strings.Contains(last, "NOBRE_RESUME") || !strings.Contains(last, "NAC-1") || !strings.Contains(last, "'claude'") {
		t.Fatalf("argv = %q", argv)
	}
	p, err := a.st.GetPrompt("retomar-card")
	if err != nil || p.Uses != 1 {
		t.Fatalf("uso do prompt = %+v %v", p, err)
	}
}

func TestContinuePromptNone(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	if rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"prompt_slug": "nenhum"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	last := l.calls[0][len(l.calls[0])-1]
	if !strings.Contains(last, "'Continue o card NAC-1.'") {
		t.Fatalf("cmd = %q", last)
	}
	if p, _ := a.st.GetPrompt("retomar-card"); p.Uses != 0 {
		t.Fatal("nenhum não deveria registrar uso")
	}
}

func TestContinueValidation(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	if got := errOf(t, a, 400, map[string]any{"mode": "tmux"}); got != "modo ainda não disponível no piloto" {
		t.Fatalf("modo: %q", got)
	}
	if got := errOf(t, a, 400, map[string]any{"agent": "codex"}); got != "agente indisponível no piloto" {
		t.Fatalf("agente: %q", got)
	}
	if rec := a.do(t, "POST", "/api/cards/NAC-9/continue", map[string]any{}, 0); rec.Code != 404 {
		t.Fatalf("card inexistente = %d", rec.Code)
	}
	if len(l.calls) != 0 {
		t.Fatal("lançou")
	}
}

func TestPutSettingsValidatesLaunch(t *testing.T) {
	a := newTestAPI(t)
	bad := []map[string]any{
		{"launch_mode": "nope"},
		{"terminal_cmd": []string{}},
		{"terminal_cmd": []string{"sh;rm", "-e"}},
		{"terminal_cmd": []string{"rel/path"}},
		{"terminal_cmd": []string{"x", ""}},
		{"terminal_cmd": []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
	}
	for _, b := range bad {
		if rec := a.do(t, "PUT", "/api/settings", b, 0); rec.Code != 400 {
			t.Fatalf("%v = %d %s", b, rec.Code, rec.Body)
		}
	}
	// null / omitido mantém o atual
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"terminal_cmd": nil, "launch_mode": "tmux"}, 0); rec.Code != 200 {
		t.Fatalf("null = %d %s", rec.Code, rec.Body)
	}
	st, _ := a.st.GetSettings()
	if len(st.TerminalCmd) != 2 || st.LaunchMode != "tmux" {
		t.Fatalf("settings = %+v", st)
	}
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"terminal_cmd": []string{"/usr/bin/gnome-terminal", "--"}}, 0); rec.Code != 200 {
		t.Fatalf("ok = %d %s", rec.Code, rec.Body)
	}
}

func TestContinueValidatesPromptBeforeCheckout(t *testing.T) {
	g := &fakeGit{branch: "main", clean: true}
	a, l := continueAPI(t, g)
	if rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"prompt_slug": "nao-existe"}, 0); rec.Code != 404 {
		t.Fatalf("slug inexistente = %d %s", rec.Code, rec.Body)
	}
	if _, err := a.st.CreatePrompt(domain.Prompt{Name: "ruim", Slug: "ruim", Body: "{{ var.nada }}"}); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"prompt_slug": "ruim"}, 0); rec.Code != 422 {
		t.Fatalf("variável desconhecida = %d %s", rec.Code, rec.Body)
	}
	if _, err := a.st.CreatePrompt(domain.Prompt{Name: "grande", Slug: "grande", Body: strings.Repeat("'", 40<<10)}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"prompt_slug": "grande"}, 0)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), "grande demais") {
		t.Fatalf("grande = %d %s", rec.Code, rec.Body)
	}
	if len(g.checkouts) != 0 || len(l.calls) != 0 {
		t.Fatalf("checkout %v / lançamentos %d antes de validar", g.checkouts, len(l.calls))
	}
}

func TestPromptSlugNenhumReserved(t *testing.T) {
	a := newTestAPI(t)
	if rec := a.do(t, "POST", "/api/prompts", map[string]any{"name": "x", "slug": "nenhum", "body": "b"}, 0); rec.Code != 400 {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
}
