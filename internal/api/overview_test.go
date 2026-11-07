package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

type overviewResp struct {
	Active []struct {
		ID           string `json:"id"`
		LastActivity string `json:"last_activity"`
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
		DailyLimitUSD    float64 `json:"daily_limit_usd"`
		TokensInput      int64   `json:"tokens_input"`
		PerAgent         []struct {
			Agent    string  `json:"agent"`
			SpentUSD float64 `json:"spent_usd"`
			DailyUSD float64 `json:"daily_usd"`
		} `json:"per_agent"`
	} `json:"today"`
	Alerts []struct {
		Kind    string `json:"kind"`
		CardKey string `json:"card_key"`
		Agent   string `json:"agent"`
		Text    string `json:"text"`
	} `json:"alerts"`
	NoCard []struct {
		ID string `json:"id"`
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
	// incomplete: pausado e sem handoff.
	paused := mk("b", domain.Pausado)
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

	if len(o.Active) != 2 || o.Active[0].ID != "claude:1" || o.Active[0].Card == nil || o.Active[0].LastActivity != "Edit internal/auth/oauth.go" {
		t.Errorf("active = %+v", o.Active)
	}
	if len(o.NoCard) != 1 || o.NoCard[0].ID != "codex:1" {
		t.Errorf("no_card = %+v", o.NoCard)
	}
	td := o.Today
	if td.ActiveAgents != 2 || td.CardsInProgress != 1 || td.CardsWithoutLive != 0 || td.DailyLimitUSD != 10 || td.TokensInput != 8_200_005 || td.CostUSD < 8.19 || td.CostUSD > 8.21 {
		t.Errorf("today = %+v", td)
	}
	if len(td.PerAgent) != 1 || td.PerAgent[0].Agent != "codex" || td.PerAgent[0].DailyUSD != 10 {
		t.Errorf("per_agent = %+v", td.PerAgent)
	}
	if len(o.Events) != 1 {
		t.Errorf("events = %+v", o.Events)
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
	}
	for _, c := range cases {
		got := lastActivityText(domain.Event{Kind: c.kind, Data: []byte(c.data)})
		if got != c.want {
			t.Errorf("%s: %q, quer %q", c.kind, got, c.want)
		}
	}
}
