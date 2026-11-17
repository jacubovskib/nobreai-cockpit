package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
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

// sessionPATH é o PATH que o continue exporta no script dos testes.
const sessionPATH = "/usr/bin:/opt/nobre/bin"

// fakeClaude cria um executável "claude" falso e devolve o caminho.
func fakeClaude(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// continueAPI monta a API com lançador e git falsos e o CLI claude em
// claudeBin; devolve o card NAC-1 (branch "feat/x") num projeto em /w/api.
func continueAPI(t *testing.T, g gitinfo.GitInfo, claudeBin ...string) (*testAPI, *fakeLauncher) {
	t.Helper()
	return continueAPIWith(t, g, nil, claudeBin...)
}

// continueAPIWith é continueAPI com um gerenciador de terminais (nil = sem).
func continueAPIWith(t *testing.T, g gitinfo.GitInfo, mgr *pty.Manager, claudeBin ...string) (*testAPI, *fakeLauncher) {
	t.Helper()
	return continueAPIDeps(t, g, mgr, nil, claudeBin...)
}

// continueAPIDeps é continueAPIWith com um ajuste nas Deps antes do New.
func continueAPIDeps(t *testing.T, g gitinfo.GitInfo, mgr *pty.Manager, tweak func(*Deps), claudeBin ...string) (*testAPI, *fakeLauncher) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	ad := claude.New(st, adapter.Deps{Now: func() time.Time { return testNow }})
	bin := ""
	if len(claudeBin) > 0 {
		bin = claudeBin[0]
	} else {
		bin = fakeClaude(t)
	}
	ad.SetPaths(claude.Paths{ClaudeBin: bin})
	l := &fakeLauncher{}
	deps := Deps{Now: func() time.Time { return testNow }, Hub: hub,
		Adapters: map[string]adapter.Adapter{"claude": ad}, Launcher: l, Git: g, Terminals: mgr, SessionPATH: sessionPATH,
		ResolveTerminal: func(n string) (string, error) {
			if n == "x-terminal-emulator" {
				return "/usr/bin/terminator", nil
			}
			return "", errors.New("não achei")
		}}
	if tweak != nil {
		tweak(&deps)
	}
	a := &testAPI{st: st, hub: hub, h: New(st, deps)}
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
		!strings.Contains(last, "NOBRE_RESUME") || !strings.Contains(last, "NAC-1") ||
		!strings.Contains(last, "export PATH=") || !strings.Contains(last, sessionPATH) || !strings.Contains(last, "/claude") || strings.Contains(last, "'claude'") {
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

func TestContinueAgentBinaryNotFound(t *testing.T) {
	g := &fakeGit{branch: "main", clean: true}
	a, l := continueAPI(t, g, filepath.Join(t.TempDir(), "claude"))
	got := errOf(t, a, http.StatusConflict, map[string]any{"agent": "claude"})
	if got != "não encontrei o comando claude; rode nobre setup de um terminal onde `claude` funciona" {
		t.Fatalf("erro = %q", got)
	}
	if len(g.checkouts) != 0 || len(l.calls) != 0 {
		t.Fatalf("checkout %v / lançamentos %d sem o claude", g.checkouts, len(l.calls))
	}
}

func TestContinueRendersPackageEmpty(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	c, err := a.st.GetCard("NAC-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.AddDecision(domain.Decision{CardID: c.ID, What: "Usar PKCE", Why: "app público"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.CreatePrompt(domain.Prompt{Name: "pac", Slug: "pac", Body: "Card {{card.id}}.[{{card.pacote}}]"}); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"prompt_slug": "pac"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	last := l.calls[0][len(l.calls[0])-1]
	if !strings.Contains(last, "Card NAC-1.[]") || strings.Contains(last, "Usar PKCE") {
		t.Fatalf("o pacote deveria ir só pelo SessionStart: %q", last)
	}
	// A prévia do prompt continua com o pacote.
	rec := a.do(t, "POST", "/api/prompts/render", map[string]any{"card": "NAC-1", "body": "{{card.pacote}}"}, 0)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Usar PKCE") {
		t.Fatalf("render = %d %s", rec.Code, rec.Body)
	}
}

// wtFakeGit é o fakeGit com uma lista fixa de worktrees.
type wtFakeGit struct {
	*fakeGit
	list []gitinfo.Worktree
}

func (g wtFakeGit) Locate(string) gitinfo.Location               { return gitinfo.Location{} }
func (g wtFakeGit) Worktrees(string) ([]gitinfo.Worktree, error) { return g.list, nil }

func TestContinueOpensInCardWorktree(t *testing.T) {
	g := wtFakeGit{fakeGit: &fakeGit{branch: "main", clean: false}, list: []gitinfo.Worktree{
		{Path: "/w/api", Branch: "main", Main: true},
		{Path: "/w/api-velho", Branch: "feat/x", Prunable: true},
		{Path: "/w/api-feat", Branch: "feat/x"},
	}}
	a, l := continueAPI(t, g)
	// Árvore suja no principal não importa: nada de checkout lá.
	if rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"prompt_slug": "nenhum"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if len(g.checkouts) != 0 {
		t.Fatalf("não deveria trocar de branch: %v", g.checkouts)
	}
	last := l.calls[0][len(l.calls[0])-1]
	if !strings.Contains(last, "/w/api-feat") || strings.Contains(last, "/w/api-velho") {
		t.Fatalf("deveria abrir no worktree do branch: %q", last)
	}
}

// bgClaude é um "claude" falso que imprime o NOBRE_RESUME, o diretório e o argumento.
func bgClaude(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho \"RESUME=$NOBRE_RESUME PWD=$(pwd)\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestContinueBackgroundStartsTerminal(t *testing.T) {
	proj := t.TempDir()
	g := &fakeGit{branch: "main", clean: true}
	mgr := pty.NewManager(pty.Options{})
	t.Cleanup(mgr.Close)
	a, l := continueAPIWith(t, g, mgr, bgClaude(t))
	p, _ := a.st.CreateProject("real", proj)
	if _, err := a.st.CreateCard(domain.Card{Title: "bg", ProjectID: p.ID, Branch: "feat/y"}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/cards/NAC-2/continue", map[string]any{"agent": "claude", "mode": "background", "prompt_slug": "nenhum"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	resp := decode[map[string]any](t, rec)
	id, _ := resp["terminal_id"].(string)
	if id == "" {
		t.Fatalf("faltou terminal_id: %v", resp)
	}
	if len(l.calls) != 0 {
		t.Fatal("modo background não deve abrir terminal externo")
	}
	if len(g.checkouts) != 1 || g.checkouts[0] != "feat/y" {
		t.Fatalf("checkouts = %v", g.checkouts)
	}
	tm, ok := mgr.Get(id)
	if !ok {
		t.Fatal("terminal não registrado")
	}
	select {
	case <-tm.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("agente não terminou")
	}
	snap, _, cancel := tm.Subscribe()
	cancel()
	info := tm.Info()
	if !strings.Contains(string(snap), "RESUME=NAC-2") || !strings.Contains(string(snap), "PWD="+proj) {
		t.Fatalf("saída = %q", snap)
	}
	if info.CardKey != "NAC-2" || info.Origin != "plataforma" || info.Agent != "claude" {
		t.Fatalf("info = %+v", info)
	}
}

func TestContinueBackgroundFromSettingsDefault(t *testing.T) {
	mgr := pty.NewManager(pty.Options{})
	t.Cleanup(mgr.Close)
	a, l := continueAPIWith(t, &fakeGit{branch: "main", clean: true}, mgr, bgClaude(t))
	p, _ := a.st.CreateProject("real", t.TempDir())
	if _, err := a.st.CreateCard(domain.Card{Title: "bg", ProjectID: p.ID}); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"launch_mode": "background"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("settings = %d %s", rec.Code, rec.Body)
	}
	rec := a.do(t, "POST", "/api/cards/NAC-2/continue", map[string]any{"mode": "platform", "prompt_slug": "nenhum"}, 0)
	if rec.Code != http.StatusOK || len(l.calls) != 0 {
		t.Fatalf("code = %d %s calls=%d", rec.Code, rec.Body, len(l.calls))
	}
	if id, _ := decode[map[string]any](t, rec)["terminal_id"].(string); id == "" {
		t.Fatalf("faltou terminal_id: %s", rec.Body)
	}
}

func TestContinueBackgroundWithoutManager(t *testing.T) {
	a, _ := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	errOf(t, a, http.StatusServiceUnavailable, map[string]any{"mode": "background"})
}

type fakeUserPATH struct {
	path string
	ages []time.Duration
}

func (f *fakeUserPATH) Get(maxAge time.Duration) string {
	f.ages = append(f.ages, maxAge)
	return f.path
}

// Com UserPATH, o "Continuar" consulta o PATH do usuário na hora (maxAge 0)
// e o exporta no script, no lugar do PATH fixo do daemon.
func TestContinueUsesFreshUserPATH(t *testing.T) {
	g := &fakeGit{branch: "main", clean: true}
	up := &fakeUserPATH{path: "/novo/bin:/usr/bin"}
	a, l := continueAPIDeps(t, g, nil, func(d *Deps) { d.UserPATH = up })
	rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"agent": "claude", "mode": "platform", "prompt_slug": ""}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	last := l.calls[0][len(l.calls[0])-1]
	if !strings.Contains(last, "export PATH=") || !strings.Contains(last, "/novo/bin:/usr/bin") || strings.Contains(last, sessionPATH) {
		t.Fatalf("script = %q", last)
	}
	if len(up.ages) == 0 || up.ages[0] != 0 {
		t.Fatalf("consultas = %v (a primeira precisa ser fresca)", up.ages)
	}
}

// A prévia do Continuar devolve exatamente o texto que o Continuar envia:
// sem o pacote (que chega pelo SessionStart) e sem abrir nada.
func TestContinuePreviewMatchesWhatIsSent(t *testing.T) {
	g := &fakeGit{branch: "feat/x", clean: true}
	a, l := continueAPI(t, g)
	c, _ := a.st.GetCard("NAC-1")
	if _, err := a.st.AddDecision(domain.Decision{CardID: c.ID, What: "Usar PKCE", Why: "app público"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.CreatePrompt(domain.Prompt{Name: "pac", Slug: "pac", Body: "Card {{card.id}}.[{{card.pacote}}]\n\n"}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"pac":    "Card NAC-1.[]",
		"nenhum": "Continue o card NAC-1.",
	}
	for slug, want := range cases {
		rec := a.do(t, "POST", "/api/cards/NAC-1/continue/preview", map[string]any{"prompt_slug": slug}, 0)
		var got struct {
			Text string `json:"text"`
		}
		json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != http.StatusOK || got.Text != want {
			t.Fatalf("%s: %d %q, quer %q", slug, rec.Code, got.Text, want)
		}
	}
	// O padrão ("") usa o prompt de handoff padrão, como o Continuar.
	rec := a.do(t, "POST", "/api/cards/NAC-1/continue/preview", map[string]any{"prompt_slug": ""}, 0)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "continuando o card NAC-1") || strings.Contains(rec.Body.String(), "Usar PKCE") {
		t.Fatalf("padrão: %d %s", rec.Code, rec.Body)
	}
	if len(g.checkouts) != 0 || len(l.calls) != 0 {
		t.Fatalf("a prévia não pode abrir nada: checkouts %v, lançamentos %d", g.checkouts, len(l.calls))
	}
	p, _ := a.st.GetPrompt("pac")
	if p.Uses != 0 {
		t.Fatalf("a prévia não conta uso: %d", p.Uses)
	}
	// O enviado é o mesmo texto.
	if rec := a.do(t, "POST", "/api/cards/NAC-1/continue", map[string]any{"prompt_slug": "pac"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("continue: %d %s", rec.Code, rec.Body)
	}
	if last := l.calls[0][len(l.calls[0])-1]; !strings.Contains(last, "Card NAC-1.[]") {
		t.Fatalf("enviado: %q", last)
	}
	for slug, code := range map[string]int{"nao-existe": 404} {
		if rec := a.do(t, "POST", "/api/cards/NAC-1/continue/preview", map[string]any{"prompt_slug": slug}, 0); rec.Code != code {
			t.Fatalf("%s: %d", slug, rec.Code)
		}
	}
	if rec := a.do(t, "POST", "/api/cards/NAC-99/continue/preview", map[string]any{"prompt_slug": ""}, 0); rec.Code != 404 {
		t.Fatalf("card inexistente: %d", rec.Code)
	}
}

// O argumento que o agente recebe é igual, byte a byte, ao texto da prévia;
// e um prompt grande demais é recusado já na prévia, como no envio.
func TestContinuePreviewEqualsAgentArgument(t *testing.T) {
	got := filepath.Join(t.TempDir(), "arg")
	claudeBin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\nprintf '%s' \"$2\" > "+got+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := pty.NewManager(pty.Options{})
	t.Cleanup(mgr.Close)
	a, _ := continueAPIWith(t, &fakeGit{branch: "main", clean: true}, mgr, claudeBin)
	p, _ := a.st.CreateProject("real", t.TempDir())
	c, err := a.st.CreateCard(domain.Card{Title: "Título com 'aspas' e $VAR", ProjectID: p.ID, WhereStopped: "linha 1\nlinha 2  "})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.CreatePrompt(domain.Prompt{Name: "x", Slug: "x", Body: "{{card.id}} {{card.titulo}}\n{{card.onde_parei}}\n{{card.pacote}}\n"}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/cards/"+c.Key+"/continue/preview", map[string]any{"prompt_slug": "x"}, 0)
	preview := decode[map[string]string](t, rec)["text"]
	if rec.Code != http.StatusOK || preview == "" {
		t.Fatalf("prévia: %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "POST", "/api/cards/"+c.Key+"/continue", map[string]any{"mode": "background", "prompt_slug": "x"}, 0)
	id, _ := decode[map[string]any](t, rec)["terminal_id"].(string)
	tm, ok := mgr.Get(id)
	if !ok {
		t.Fatalf("continue: %d %s", rec.Code, rec.Body)
	}
	select {
	case <-tm.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("agente não terminou")
	}
	if b, _ := os.ReadFile(got); string(b) != preview {
		t.Fatalf("enviado %q\nprévia   %q", b, preview)
	}

	if _, err := a.st.CreatePrompt(domain.Prompt{Name: "g", Slug: "g", Body: strings.Repeat("x", 130<<10)}); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/cards/"+c.Key+"/continue/preview", map[string]any{"prompt_slug": "g"}, 0); rec.Code != 422 {
		t.Fatalf("prévia de prompt grande: %d", rec.Code)
	}
}
