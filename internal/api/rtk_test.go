package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/integration"
	"github.com/nobretech/nobreai-cockpit/internal/rtk"
)

const rtkHookSettings = `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/x/rtk hook claude"}]}]}}`

const rtkGain = `{"daily":[` +
	`{"date":"2026-11-05","commands":10,"input_tokens":1000,"saved_tokens":500},` +
	`{"date":"2026-11-06","commands":5,"input_tokens":1000,"saved_tokens":100},` +
	`{"date":"2026-11-07","commands":7,"input_tokens":2000,"saved_tokens":1500}]}`

type rtkEnv struct {
	*testAPI
	dir, settings string
	tool          *rtk.Tool
	mon           *integration.Monitor
}

// novoRTK monta a API com um rtk falso e um settings.json temporário;
// nunca toca o binário nem o ~/.claude reais.
func novoRTK(t *testing.T, settings string, gainFails bool) *rtkEnv {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	bindir := filepath.Join(home, ".cargo", "bin")
	if err := os.MkdirAll(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	e := &rtkEnv{dir: dir, settings: filepath.Join(dir, "claude", "settings.json")}
	if err := os.MkdirAll(filepath.Dir(e.settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(e.settings, []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gain := "echo '" + rtkGain + "'"
	if gainFails {
		gain = "echo boom; exit 1"
	}
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"--version) echo 'rtk 1.2.0';;\n" +
		"gain) " + gain + ";;\n" +
		"init) if [ -f " + filepath.Join(dir, "fail") + " ]; then echo 'falhou'; exit 1; fi; case \"$*\" in\n" +
		"*--dry-run*) echo 'prévia';;\n" +
		"*--uninstall*) echo '{}' > " + e.settings + "; echo 'removido';;\n" +
		"*) echo '" + rtkHookSettings + "' > " + e.settings + "; echo 'feito';;\n" +
		"esac;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bindir, "rtk"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a := newTestAPI(t)
	e.testAPI = a
	e.tool = &rtk.Tool{SettingsPath: e.settings, Home: home, SearchPATH: func() string { return "" },
		Store: a.st, Now: func() time.Time { return testNow }}
	e.mon = &integration.Monitor{
		Health:  func() []adapter.Check { return []adapter.Check{e.tool.Check()} },
		Store:   a.st,
		Now:     func() time.Time { return testNow },
		Publish: a.hub.Publish,
	}
	a.h = New(a.st, Deps{Now: func() time.Time { return testNow }, Hub: a.hub, RTK: e.tool, Integration: e.mon})
	return e
}

func TestRTKStatus(t *testing.T) {
	e := novoRTK(t, rtkHookSettings, false)
	rec := e.do(t, "GET", "/api/rtk", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	st := decode[rtk.Status](t, rec)
	if !st.Installed || !st.Active || st.Version != "1.2.0" {
		t.Errorf("st = %+v", st)
	}
}

func TestRTKPreview(t *testing.T) {
	e := novoRTK(t, rtkHookSettings, false)
	rec := e.do(t, "GET", "/api/rtk/preview?action=activate", nil, 0)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "prévia") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := e.do(t, "GET", "/api/rtk/preview?action=x", nil, 0); rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRTKApply_Activate(t *testing.T) {
	e := novoRTK(t, "{}", false)
	rec := e.do(t, "POST", "/api/rtk", map[string]string{"action": "activate"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	out := decode[struct {
		Output, Backup string
		Status         rtk.Status
	}](t, rec)
	if out.Backup == "" || !out.Status.Active || len(out.Status.Activations) != 1 || out.Status.Activations[0].Off != nil {
		t.Errorf("out = %+v", out)
	}
	if _, err := os.Stat(out.Backup); err != nil {
		t.Errorf("sem cópia: %v", err)
	}
	log, _ := e.st.RecentConfigLog(10)
	found := false
	for _, l := range log {
		if l.Kind == "rtk_on" {
			found = true
		}
	}
	if !found {
		t.Errorf("config_log sem rtk_on: %+v", log)
	}
	if rec := e.do(t, "POST", "/api/rtk", map[string]string{"action": "x"}, 0); rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestRTKDeactivate_NoAlert(t *testing.T) {
	e := novoRTK(t, rtkHookSettings, false)
	if p := e.mon.CheckNow(); len(p) != 0 {
		t.Fatalf("ativo deveria estar ok: %+v", p)
	}
	rec := e.do(t, "POST", "/api/rtk", map[string]string{"action": "deactivate"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if c := e.tool.Check(); c.Code != "missing" {
		t.Fatalf("check = %+v", c)
	}
	if p := e.mon.CheckNow(); len(p) != 0 {
		t.Errorf("desativar de propósito não deve alertar: %+v", p)
	}
	log, _ := e.st.RecentConfigLog(10)
	found := false
	for _, l := range log {
		if l.Kind == "rtk_off" {
			found = true
		}
	}
	if !found {
		t.Errorf("config_log sem rtk_off")
	}
}

func TestRTKSavings(t *testing.T) {
	e := novoRTK(t, rtkHookSettings, false)
	from := time.Date(2026, 11, 6, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	to := time.Date(2026, 11, 7, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	rec := e.do(t, "GET", "/api/rtk/savings?from="+from+"&to="+to, nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	out := decode[struct {
		Installed   bool
		Gain        rtkGainTotals
		Compare     []any
		Activations []any
	}](t, rec)
	if !out.Installed {
		t.Errorf("installed = false: %s", rec.Body.String())
	}
	g := out.Gain
	if !g.Available || g.Commands != 5 || g.InputTokens != 1000 || g.SavedTokens != 100 || g.SavingsPct != 10 || g.OldestDay != "2026-11-05" {
		t.Errorf("gain = %+v", g)
	}
	if out.Compare == nil || out.Activations == nil {
		t.Errorf("compare/activations devem ser listas: %s", rec.Body.String())
	}
	// Sem período: tudo.
	all := decode[struct{ Gain rtkGainTotals }](t, e.do(t, "GET", "/api/rtk/savings", nil, 0)).Gain
	if all.Commands != 22 || all.SavedTokens != 2100 {
		t.Errorf("all = %+v", all)
	}
}

func TestRTKSavings_GainFails(t *testing.T) {
	e := novoRTK(t, rtkHookSettings, true)
	rec := e.do(t, "GET", "/api/rtk/savings", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	out := decode[struct {
		Gain    rtkGainTotals
		Compare []any
	}](t, rec)
	if out.Gain.Available || out.Gain.Commands != 0 || out.Compare == nil {
		t.Errorf("out = %+v", out)
	}
}

func TestRTK_NoTool(t *testing.T) {
	a := newTestAPI(t)
	for _, c := range [][2]string{{"GET", "/api/rtk"}, {"GET", "/api/rtk/preview?action=activate"},
		{"POST", "/api/rtk"}, {"GET", "/api/rtk/savings"}} {
		if rec := a.do(t, c[0], c[1], map[string]string{"action": "activate"}, 0); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%v: code = %d", c, rec.Code)
		}
	}
}

func TestRTKDeactivate_FailedKeepsAlerting(t *testing.T) {
	e := novoRTK(t, rtkHookSettings, false)
	if p := e.mon.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "fail"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(t, "POST", "/api/rtk", map[string]string{"action": "deactivate"}, 0); rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
	// O hook continuou ativo; se sumir depois, não foi de propósito: alerta.
	if err := os.WriteFile(e.settings, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := e.mon.CheckNow(); len(p) != 1 || p[0].Key != "rtk" {
		t.Errorf("problems = %+v", p)
	}
	// Falhar não grava rtk_off nem linha extra além da do Fixed.
	log, _ := e.st.RecentConfigLog(20)
	fixed := 0
	for _, l := range log {
		switch l.Kind {
		case domain.LogRTKOff, domain.LogRTKOn:
			t.Errorf("falha não deveria logar %s", l.Kind)
		case domain.LogIntegrationFixed:
			fixed++
		}
	}
	if fixed != 1 {
		t.Errorf("integration_fixed = %d, quer 1: %+v", fixed, log)
	}
}

func TestRTKApply_NotInstalled(t *testing.T) {
	e := novoRTK(t, rtkHookSettings, false)
	if p := e.mon.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
	if err := os.Remove(filepath.Join(e.dir, "home", ".cargo", "bin", "rtk")); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"deactivate", "activate"} {
		rec := e.do(t, "POST", "/api/rtk", map[string]string{"action": action}, 0)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "RTK não instalado") {
			t.Fatalf("%s: %d %s", action, rec.Code, rec.Body.String())
		}
	}
	log, _ := e.st.RecentConfigLog(10)
	for _, l := range log {
		if l.Kind == domain.LogIntegrationRemoved || l.Kind == domain.LogRTKOff {
			t.Errorf("sem binário não marca remoção nem desativa: %+v", log)
		}
	}
	// O hook segue no arquivo sem binário: alerta, a marca não foi gravada.
	if p := e.mon.CheckNow(); len(p) != 1 || p[0].Key != "rtk" {
		t.Errorf("problems = %+v", p)
	}
	rec := e.do(t, "GET", "/api/rtk/savings", nil, 0)
	if out := decode[struct{ Installed bool }](t, rec); out.Installed {
		t.Errorf("savings deveria dizer não instalado: %s", rec.Body.String())
	}
}
