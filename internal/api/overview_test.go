package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

type overviewResp struct {
	Active []struct {
		ID           string     `json:"id"`
		PIDDeadAt    *time.Time `json:"pid_dead_at"`
		LastActivity string     `json:"last_activity"`
		Card         *struct {
			Key string `json:"key"`
		} `json:"card"`
		SessionUsage struct {
			Input int64 `json:"input"`
		} `json:"session_usage"`
	} `json:"active"`
	Today struct {
		ActiveAgents     int     `json:"active_agents"`
		CardsInProgress  int     `json:"cards_in_progress"`
		CardsWithoutLive int     `json:"cards_without_live_session"`
		CostUSD          float64 `json:"cost_usd"`
		CostUnpriced     bool    `json:"cost_unpriced"`
		Credits          float64 `json:"credits"`
		DailyLimitUSD    float64 `json:"daily_limit_usd"`
		TokensInput      int64   `json:"tokens_input"`
		PerAgent         []struct {
			Agent    string  `json:"agent"`
			SpentUSD float64 `json:"spent_usd"`
			DailyUSD float64 `json:"daily_usd"`
			Action   string  `json:"action"`
			Credits  float64 `json:"credits"`
		} `json:"per_agent"`
	} `json:"today"`
	Alerts []struct {
		Kind    string `json:"kind"`
		CardKey string `json:"card_key"`
		Agent   string `json:"agent"`
		Text    string `json:"text"`
	} `json:"alerts"`
	NoCard []struct {
		ID      string     `json:"id"`
		EndedAt *time.Time `json:"ended_at"`
	} `json:"no_card"`
	Events []struct {
		Kind string `json:"kind"`
	} `json:"events"`
}

func TestOverviewAlerts(t *testing.T) {
	a := newTestAPI(t) // testNow = 2026-11-07 12:00 UTC
	p := a.project(t)
	mk := func(title string, col domain.Column) domain.Card {
		c, err := a.st.CreateCard(domain.Card{Title: title, ProjectID: p.ID})
		if err != nil {
			t.Fatal(err)
		}
		if col != domain.Backlog {
			if err := a.st.MoveCard(c.Key, col); err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	// stale: em andamento, sessão viva com PID morto desde 7 min atrás.
	stale := mk("a", domain.Backlog)
	start := func(id, agent string, pid int, at time.Time) {
		if err := a.st.StartSession(domain.Session{ID: id, Agent: agent, PID: pid, StartedAt: at, LastEventAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	start("claude:1", "claude", 111, testNow.Add(-time.Hour))
	if err := a.st.AttachSession("claude:1", stale.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := a.st.MarkPIDDead("claude:1", testNow.Add(-7*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// incomplete: pausado, sem handoff e marcado (IncompleteAction = mark).
	paused := mk("b", domain.Pausado)
	setIncompleteAction(t, a, "mark")
	if err := a.st.SetHandoffIncomplete(paused.ID, true, testNow); err != nil {
		t.Fatal(err)
	}
	// limit_warn + unpriced: codex gastou 8,20 de 10,00 hoje; modelo "x" sem preço.
	if err := a.st.PutLimit(domain.Limit{Agent: "codex", DailyUSD: 10, WarnPct: 80, Action: "warn"}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.PutPrice(domain.Price{Model: "m", InputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	start("codex:1", "codex", 0, testNow.Add(-time.Hour))
	if err := a.st.UpsertUsage([]domain.UsageRow{
		{SessionID: "codex:1", MessageID: "1", Model: "m", Input: 8_200_000, At: testNow.Add(-time.Minute)},
		{SessionID: "codex:1", MessageID: "2", Model: "x", Input: 5, At: testNow.Add(-time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.AddEvent(domain.Event{SessionID: "claude:1", Kind: domain.EvFileEdit, Data: []byte(`{"path":"internal/auth/oauth.go"}`), At: testNow.Add(-2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	rec := a.do(t, "GET", "/api/overview", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	o := decode[overviewResp](t, rec)
	want := []struct{ kind, key, agent, text string }{
		{"stale", stale.Key, "claude", "NAC-1 está em “Em andamento” sem sessão viva há 7 min."},
		{"limit_warn", "", "codex", "Codex usou 82% do limite diário (US$ 8,20 de US$ 10,00)."},
		{"incomplete", paused.Key, "", "NAC-2 foi pausado com handoff incompleto."},
		{"unpriced", "", "", "Há uso de modelos sem preço configurado: x."},
	}
	if len(o.Alerts) != len(want) {
		t.Fatalf("alertas = %+v", o.Alerts)
	}
	for i, w := range want {
		g := o.Alerts[i]
		if g.Kind != w.kind || g.CardKey != w.key || g.Agent != w.agent || g.Text != w.text {
			t.Errorf("alerta %d = %+v, quer %+v", i, g, w)
		}
	}

	// claude:1 continua na lista (processo encerrado, na carência), marcada
	// com pid_dead_at, mas não conta como viva.
	if len(o.Active) != 2 || o.Active[0].ID != "claude:1" || o.Active[0].Card == nil || o.Active[0].LastActivity != "Edit internal/auth/oauth.go" ||
		o.Active[0].PIDDeadAt == nil || o.Active[1].PIDDeadAt != nil {
		t.Errorf("active = %+v", o.Active)
	}
	if len(o.NoCard) != 1 || o.NoCard[0].ID != "codex:1" {
		t.Errorf("no_card = %+v", o.NoCard)
	}
	td := o.Today
	if td.ActiveAgents != 1 || td.CardsInProgress != 1 || td.CardsWithoutLive != 1 || td.DailyLimitUSD != 10 || td.TokensInput != 8_200_005 || td.CostUSD < 8.19 || td.CostUSD > 8.21 {
		t.Errorf("today = %+v", td)
	}
	if len(td.PerAgent) != 1 || td.PerAgent[0].Agent != "codex" || td.PerAgent[0].DailyUSD != 10 || td.PerAgent[0].Action != "warn" {
		t.Errorf("per_agent = %+v", td.PerAgent)
	}
	if len(o.Events) != 1 {
		t.Errorf("events = %+v", o.Events)
	}
}

// Sessões sem card encerradas nas últimas 24 h aparecem (com ended_at), para
// permitir "Criar card desta sessão"; mais antigas ou com card, não.
func TestOverviewNoCardIncludesRecentlyEnded(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	mk := func(id string, endedAgo time.Duration, card bool) {
		x := domain.Session{ID: id, Agent: "claude", PID: 1, StartedAt: testNow.Add(-48 * time.Hour)}
		if card {
			x.CardID = &c.ID
		}
		if err := a.st.StartSession(x); err != nil {
			t.Fatal(err)
		}
		if endedAgo > 0 {
			if err := a.st.EndSession(id, "exit", testNow.Add(-endedAgo)); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("claude:open", 0, false)
	mk("claude:recent", 2*time.Hour, false)
	mk("claude:old", 30*time.Hour, false)
	mk("claude:card", time.Hour, true)
	o := decode[overviewResp](t, a.do(t, "GET", "/api/overview", nil, 0))
	if len(o.NoCard) != 2 || o.NoCard[0].ID != "claude:open" || o.NoCard[0].EndedAt != nil ||
		o.NoCard[1].ID != "claude:recent" || o.NoCard[1].EndedAt == nil {
		t.Fatalf("no_card = %+v", o.NoCard)
	}
}

func TestOverviewEmptyIsArrays(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "GET", "/api/overview", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	m := decode[map[string]any](t, rec)
	for _, k := range []string{"active", "alerts", "no_card", "events"} {
		if v, ok := m[k].([]any); !ok || len(v) != 0 {
			t.Errorf("%s = %v", k, m[k])
		}
	}
}

func TestLastActivityText(t *testing.T) {
	cases := []struct{ kind, data, want string }{
		{domain.EvFileEdit, `{"path":"a.go"}`, "Edit a.go"},
		{domain.EvFileRead, `{"path":"a.go"}`, "Read a.go"},
		{domain.EvCommand, `{"command":"go test ./..."}`, "Bash go test ./..."},
		{domain.EvNote, `{"text":"olhei o token"}`, "nota: olhei o token"},
		{domain.EvPrompt, `{}`, "prompt enviado"},
		{domain.EvDecision, `{}`, "decisão registrada"},
		{domain.EvSessionStart, `{}`, "sessão iniciada"},
		{domain.EvTodo, `{}`, "tarefa atualizada"},
		{domain.EvDone, `{}`, "marcado como pronto para revisão"},
		{domain.EvHowToTest, `{"text":"go test"}`, "“como testar” registrado"},
		{domain.EvLimitPause, `{}`, "pausado por limite de custo"},
	}
	for _, c := range cases {
		got := lastActivityText(domain.Event{Kind: c.kind, Data: []byte(c.data)})
		if got != c.want {
			t.Errorf("%s: %q, quer %q", c.kind, got, c.want)
		}
	}
}

func TestOverviewPlanLimitAlerts(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("BRT", -3*3600)
	defer func() { time.Local = old }()
	a := newTestAPI(t) // testNow = 2026-11-07 12:00 UTC (sábado 09:00 local)
	post := func(five, seven float64, fiveReset, sevenReset time.Time) {
		t.Helper()
		rec := a.do(t, "POST", "/api/plan-limits", map[string]any{"session_id": "s", "rate_limits": map[string]any{
			"five_hour": map[string]any{"used_percentage": five, "resets_at": fiveReset.Unix()},
			"seven_day": map[string]any{"used_percentage": seven, "resets_at": sevenReset.Unix()},
		}}, 0)
		if rec.Code != 204 {
			t.Fatalf("POST = %d", rec.Code)
		}
	}
	alerts := func() []string {
		t.Helper()
		var out []string
		for _, al := range decode[overviewResp](t, a.do(t, "GET", "/api/overview", nil, 0)).Alerts {
			if al.Kind == "plan_limit" {
				out = append(out, al.Text)
			}
		}
		return out
	}
	// 5h reinicia 14:30Z = 11:30 local (sáb); semana reinicia 2026-11-10 22:05Z = ter 19:05 local.
	five := time.Date(2026, 11, 7, 14, 30, 0, 0, time.UTC)
	week := time.Date(2026, 11, 10, 22, 5, 0, 0, time.UTC)
	post(79.9, 50, five, week)
	if got := alerts(); len(got) != 0 {
		t.Fatalf("abaixo do aviso = %v", got)
	}
	post(80, 91.4, five, week) // 80 = limite exato; mesmo minuto: coalesce com o maior
	got := alerts()
	// O alerta leva o resets_at: a UI o esconde quando o relógio do cliente passa dele.
	raw := a.do(t, "GET", "/api/overview", nil, 0).Body.String()
	if !strings.Contains(raw, `"resets_at":"2026-11-07T14:30:00Z"`) || !strings.Contains(raw, `"resets_at":"2026-11-10T22:05:00Z"`) {
		t.Fatalf("alerta sem resets_at: %s", raw)
	}
	want := []string{
		"Sessão de 5 h do plano em 80% (reinicia às 11:30).",
		"Limite semanal do plano em 91% (reinicia ter às 19:05).",
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("alertas = %q, quer %q", got, want)
	}
	// Janela expirada não alerta. (Numa API nova: com a janela de 5 h ainda
	// vigente, uma leitura com reinício no passado seria descartada.)
	a = newTestAPI(t)
	post(99, 99, testNow.Add(-time.Minute), week)
	got = alerts()
	if len(got) != 1 || got[0] != "Limite semanal do plano em 99% (reinicia ter às 19:05)." {
		t.Fatalf("expirada = %q", got)
	}
	// plan_warn_pct configurável.
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"plan_warn_pct": 100}, 0); rec.Code != 200 {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	if got = alerts(); len(got) != 0 {
		t.Fatalf("warn 100 = %q", got)
	}
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"plan_warn_pct": 0}, 0); rec.Code != 400 {
		t.Fatalf("plan_warn_pct 0 = %d", rec.Code)
	}
}

func setIncompleteAction(t *testing.T, a *testAPI, action string) {
	t.Helper()
	set, err := a.st.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	set.IncompleteAction = action
	if err := a.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewIncompleteAlertDependsOnSetting(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	c, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.MoveCard(c.Key, domain.Pausado); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		rec := a.do(t, "GET", "/api/overview", nil, 0)
		n := 0
		for _, al := range decode[overviewResp](t, rec).Alerts {
			if al.Kind == "incomplete" {
				n++
			}
		}
		return n
	}
	setIncompleteAction(t, a, "mark")
	if n := count(); n != 0 {
		t.Fatalf("pausado incompleto sem marca: %d alertas", n)
	}
	if err := a.st.SetHandoffIncomplete(c.ID, true, testNow); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("marcado + mark: %d alertas", n)
	}
	setIncompleteAction(t, a, "ask_agent")
	if n := count(); n != 0 {
		t.Fatalf("marcado + ask_agent: %d alertas", n)
	}
	got, _ := a.st.GetCard(c.Key)
	if !got.HandoffIncomplete {
		t.Fatal("a marca deveria continuar no banco")
	}
}

func TestOverviewTodayCredits(t *testing.T) {
	a := newTestAPI(t)
	if err := a.st.StartSession(domain.Session{ID: "kiro:1", Agent: "kiro", PID: 1, StartedAt: testNow.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.UpsertUsage([]domain.UsageRow{
		{SessionID: "kiro:1", MessageID: "k1", Model: "auto", Credits: 0.25, At: testNow.Add(-30 * time.Minute)},
		{SessionID: "kiro:1", MessageID: "k2", Model: "auto", Credits: 0.5, At: testNow.Add(-20 * time.Minute)},
		// Turno sem gasto (0 créditos, sem tokens): nada a precificar.
		{SessionID: "kiro:1", MessageID: "k3", Model: "auto", Credits: 0, At: testNow.Add(-10 * time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	o := decode[overviewResp](t, a.do(t, "GET", "/api/overview", nil, 0))
	if o.Today.Credits != 0.75 || o.Today.CostUSD != 0 {
		t.Fatalf("today = credits %v, usd %v", o.Today.Credits, o.Today.CostUSD)
	}
	if o.Today.CostUnpriced {
		t.Fatal("custo de hoje sem preço por turno sem tokens")
	}
	if len(o.Today.PerAgent) != 1 || o.Today.PerAgent[0].Agent != "kiro" || o.Today.PerAgent[0].Credits != 0.75 || o.Today.PerAgent[0].SpentUSD != 0 {
		t.Fatalf("por agente = %+v", o.Today.PerAgent)
	}
	for _, al := range o.Alerts {
		if al.Kind == "unpriced" {
			t.Fatalf("alerta de modelo sem preço: %+v", al)
		}
	}
}
