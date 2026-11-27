package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
)

func restartErr(t *testing.T, a *testAPI, id string, code int, body map[string]any) string {
	t.Helper()
	rec := a.do(t, "POST", "/api/sessions/"+id+"/restart-clean", body, 0)
	if rec.Code != code {
		t.Fatalf("code = %d (quero %d) %s", rec.Code, code, rec.Body)
	}
	return decode[map[string]string](t, rec)["error"]
}

func TestRestartCleanErrors(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	restartErr(t, a, "claude:nada", http.StatusNotFound, map[string]any{})
	a.liveSession(t, "claude:sem", 1, "")
	if got := restartErr(t, a, "claude:sem", 400, map[string]any{}); got != "sessão sem card" {
		t.Fatalf("erro = %q", got)
	}
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	if got := restartErr(t, a, "claude:s", 400, map[string]any{}); got != "registre onde parou antes de recomeçar" {
		t.Fatalf("erro = %q", got)
	}
	old := testNow.Add(-31 * time.Minute)
	c, _ := a.st.GetCard("NAC-1")
	if err := a.st.SetWhereStopped(c.ID, "velho", old); err != nil {
		t.Fatal(err)
	}
	restartErr(t, a, "claude:s", 400, map[string]any{"where_stopped": "  "})
	if len(l.calls) != 0 {
		t.Fatal("não deveria lançar")
	}
	s, _ := a.st.GetSession("claude:s")
	if s.CardID == nil {
		t.Fatal("desligou apesar do erro")
	}
}

func TestRestartCleanWithTextSavesDetachesContinues(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	rec := a.do(t, "POST", "/api/sessions/claude:s/restart-clean", map[string]any{"where_stopped": "no handler"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	c, _ := a.st.GetCard("NAC-1")
	if c.WhereStopped != "no handler" || c.WhereStoppedAt == nil || !c.WhereStoppedAt.Equal(testNow) {
		t.Fatalf("onde parei = %q %v", c.WhereStopped, c.WhereStoppedAt)
	}
	if c.Column != domain.Pausado {
		t.Fatalf("coluna = %s", c.Column)
	}
	s, _ := a.st.GetSession("claude:s")
	if s.CardID != nil || !s.Live() {
		t.Fatalf("sessão = card %v viva %v", s.CardID, s.Live())
	}
	if len(l.calls) != 1 {
		t.Fatalf("lançamentos = %d", len(l.calls))
	}
}

func TestRestartCleanRecentWithoutText(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	c, _ := a.st.GetCard("NAC-1")
	if err := a.st.SetWhereStopped(c.ID, "ontem não", testNow.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "POST", "/api/sessions/claude:s/restart-clean", map[string]any{}, 0); rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	c, _ = a.st.GetCard("NAC-1")
	if c.WhereStopped != "ontem não" || c.Column != domain.Pausado || len(l.calls) != 1 {
		t.Fatalf("card = %+v lançamentos %d", c, len(l.calls))
	}
}

func TestRestartCleanChildSessionOwnCard(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	if _, err := a.st.CreateCard(domain.Card{Title: "Filha", ProjectID: 1}); err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:main", 1, "NAC-1")
	a.liveSession(t, "claude:filha", 2, "NAC-2")
	rec := a.do(t, "POST", "/api/sessions/claude:filha/restart-clean", map[string]any{"where_stopped": "aqui"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	c2, _ := a.st.GetCard("NAC-2")
	c1, _ := a.st.GetCard("NAC-1")
	f, _ := a.st.GetSession("claude:filha")
	m, _ := a.st.GetSession("claude:main")
	if c2.Column != domain.Pausado || c1.Column != domain.EmAndamento || f.CardID != nil || m.CardID == nil || len(l.calls) != 1 {
		t.Fatalf("c1=%s c2=%s filha=%v main=%v calls=%d", c1.Column, c2.Column, f.CardID, m.CardID, len(l.calls))
	}
}

type failLauncher struct{}

func (failLauncher) Start([]string) error { return errors.New("boom") }

func TestRestartCleanValidationFailureChangesNothing(t *testing.T) {
	a, _ := continueAPIDeps(t, &fakeGit{branch: "feat/x", clean: true}, nil, func(d *Deps) { d.Launcher = nil })
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	got := restartErr(t, a, "claude:s", http.StatusServiceUnavailable, map[string]any{"where_stopped": "aqui"})
	if got != "lançador indisponível" {
		t.Fatalf("erro = %q", got)
	}
	s, _ := a.st.GetSession("claude:s")
	c, _ := a.st.GetCard("NAC-1")
	if s.CardID == nil || c.Column != domain.EmAndamento {
		t.Fatalf("mudou algo: card %v coluna %s", s.CardID, c.Column)
	}
}

func TestRestartCleanDirtyTreeChangesNothing(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "main", clean: false})
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	restartErr(t, a, "claude:s", http.StatusConflict, map[string]any{"where_stopped": "aqui"})
	s, _ := a.st.GetSession("claude:s")
	if s.CardID == nil || len(l.calls) != 0 {
		t.Fatal("mudou algo")
	}
}

func TestRestartCleanLaunchFailureAfterDetach(t *testing.T) {
	a, _ := continueAPIDeps(t, &fakeGit{branch: "feat/x", clean: true}, nil, func(d *Deps) { d.Launcher = failLauncher{} })
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	got := restartErr(t, a, "claude:s", http.StatusInternalServerError, map[string]any{"where_stopped": "aqui"})
	if !strings.HasSuffix(got, " — o card ficou pausado; use Continuar no card para abrir de novo.") {
		t.Fatalf("erro = %q", got)
	}
	s, _ := a.st.GetSession("claude:s")
	c, _ := a.st.GetCard("NAC-1")
	if s.CardID != nil || c.Column != domain.Pausado {
		t.Fatalf("card %v coluna %s", s.CardID, c.Column)
	}
}

// Sessão encerrada não recomeça: o card pode já ser de outra sessão viva.
// Nada muda (nem o "onde parei").
func TestRestartCleanEndedSessionConflict(t *testing.T) {
	a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
	a.liveSession(t, "claude:s", 4242, "NAC-1")
	if err := a.st.EndSession("claude:s", "exit", testNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/sessions/claude:s/restart-clean", map[string]any{"where_stopped": "no handler"}, 0)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	body := decode[map[string]string](t, rec)
	if body["code"] != "session_ended" || body["error"] == "" {
		t.Fatalf("corpo = %v", body)
	}
	if len(l.calls) != 0 {
		t.Fatal("não deveria lançar")
	}
	if c, _ := a.st.GetCard("NAC-1"); c.WhereStopped == "no handler" {
		t.Fatal("gravou onde parei apesar do erro")
	}
	if s, _ := a.st.GetSession("claude:s"); s.CardID == nil {
		t.Fatal("desligou apesar do erro")
	}
}

// mode "background" abre o agente novo no gerenciador de terminais, mesmo com
// o padrão das configurações em terminal.
func TestRestartCleanModeBackgroundOpensOnPlatform(t *testing.T) {
	proj := realTempDir(t)
	mgr := pty.NewManager(pty.Options{})
	t.Cleanup(mgr.Close)
	a, l := continueAPIWith(t, &fakeGit{branch: "main", clean: true}, mgr, bgClaude(t))
	p, _ := a.st.CreateProject("real", proj)
	if _, err := a.st.CreateCard(domain.Card{Title: "bg", ProjectID: p.ID}); err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, "NAC-2")
	rec := a.do(t, "POST", "/api/sessions/claude:s/restart-clean", map[string]any{"where_stopped": "aqui", "mode": "background"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	id, _ := decode[map[string]any](t, rec)["terminal_id"].(string)
	if id == "" {
		t.Fatalf("faltou terminal_id: %s", rec.Body)
	}
	if _, ok := mgr.Get(id); !ok {
		t.Fatal("terminal não registrado")
	}
	if len(l.calls) != 0 {
		t.Fatal("modo background não deve abrir terminal externo")
	}
	s, _ := a.st.GetSession("claude:s")
	c, _ := a.st.GetCard("NAC-2")
	if s.CardID != nil || !s.Live() || c.Column != domain.Pausado {
		t.Fatalf("card %v viva %v coluna %s", s.CardID, s.Live(), c.Column)
	}
}

// mode inválido, ou background sem gerenciador, recusa antes de desligar o card.
func TestRestartCleanInvalidModeChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		mgr  *pty.Manager
		mode string
		code int
		want string
	}{
		{"invalido", nil, "xyz", http.StatusBadRequest, "modo ainda não disponível no piloto"},
		{"sem gerenciador", nil, "background", http.StatusServiceUnavailable, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, l := continueAPI(t, &fakeGit{branch: "feat/x", clean: true})
			a.liveSession(t, "claude:s", 4242, "NAC-1")
			got := restartErr(t, a, "claude:s", tc.code, map[string]any{"where_stopped": "aqui", "mode": tc.mode})
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("erro = %q", got)
			}
			s, _ := a.st.GetSession("claude:s")
			c, _ := a.st.GetCard("NAC-1")
			if s.CardID == nil || c.Column != domain.EmAndamento || len(l.calls) != 0 {
				t.Fatalf("mudou algo: card %v coluna %s lançamentos %d", s.CardID, c.Column, len(l.calls))
			}
		})
	}
}

// Gerenciador de terminais no limite: recusa antes de desligar o card, com o
// mesmo erro da abertura e sem o sufixo de card pausado.
func TestRestartCleanBackgroundAtTerminalLimitChangesNothing(t *testing.T) {
	proj := realTempDir(t)
	mgr := pty.NewManager(pty.Options{MaxLive: 1})
	t.Cleanup(mgr.Close)
	if _, err := mgr.Start(pty.StartSpec{Argv: []string{"cat"}}); err != nil {
		t.Fatal(err)
	}
	a, l := continueAPIWith(t, &fakeGit{branch: "main", clean: true}, mgr, bgClaude(t))
	p, _ := a.st.CreateProject("real", proj)
	if _, err := a.st.CreateCard(domain.Card{Title: "bg", ProjectID: p.ID}); err != nil {
		t.Fatal(err)
	}
	a.liveSession(t, "claude:s", 4242, "NAC-2")
	before, _ := a.st.GetCard("NAC-2")
	got := restartErr(t, a, "claude:s", http.StatusConflict, map[string]any{"where_stopped": "aqui", "mode": "background"})
	if got != pty.ErrTooMany.Error() {
		t.Fatalf("erro = %q", got)
	}
	s, _ := a.st.GetSession("claude:s")
	c, _ := a.st.GetCard("NAC-2")
	if s.CardID == nil || c.Column != before.Column || len(l.calls) != 0 {
		t.Fatalf("mudou algo: card %v coluna %s (antes %s) lançamentos %d", s.CardID, c.Column, before.Column, len(l.calls))
	}
	if n := len(mgr.List()); n != 1 {
		t.Fatalf("terminais = %d", n)
	}
}
