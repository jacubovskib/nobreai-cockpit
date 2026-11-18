package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/integration"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Overview alimenta a sala de controle.
type Overview struct {
	Active []ActiveSession `json:"active"`
	Today  TodayTotals     `json:"today"`
	Alerts []Alert         `json:"alerts"`
	// NoCard: sessões sem card não encerradas e as encerradas nas últimas 24 h
	// (ended_at preenchido), para "Criar card desta sessão".
	NoCard []SessionWithUsage `json:"no_card"`
	Events []domain.Event     `json:"events"`
}

// noCardWindow é até quando uma sessão sem card encerrada aparece em NoCard.
const noCardWindow = 24 * time.Hour

// ActiveSession é uma sessão não encerrada com seu card, atividade recente e
// custos. Com pid_dead_at preenchido o processo já sumiu (carência antes de
// encerrar): aparece como "processo encerrado há N min", não como viva.
type ActiveSession struct {
	domain.Session
	Card         *domain.Card            `json:"card"`
	LastActivity string                  `json:"last_activity"`
	SessionUsage store.Total             `json:"session_usage"`
	CardTotals   store.Total             `json:"card_totals"`
	Readiness    *domain.ReadinessResult `json:"readiness"`
}

// AgentSpend é o gasto de hoje de um agente frente ao limite diário
// (DailyUSD 0 = sem limite). Action é a ação do limite em 100% ("pause" ou
// "warn"; vazio sem limite configurado).
type AgentSpend struct {
	Agent    string  `json:"agent"`
	SpentUSD float64 `json:"spent_usd"`
	DailyUSD float64 `json:"daily_usd"`
	Pct      int     `json:"pct"`
	WarnPct  int     `json:"warn_pct"`
	Action   string  `json:"action"`
	Unpriced bool    `json:"unpriced"`
}

// TodayTotals são os indicadores do dia local (00:00 até 00:00 seguinte).
type TodayTotals struct {
	// ActiveAgents conta agentes distintos com sessão viva (processo existindo).
	ActiveAgents     int `json:"active_agents"`
	CardsInProgress  int `json:"cards_in_progress"`
	CardsWithoutLive int `json:"cards_without_live_session"`
	// CostUSD soma só o uso com preço; CostUnpriced avisa que há uso sem preço.
	CostUSD          float64      `json:"cost_usd"`
	CostUnpriced     bool         `json:"cost_unpriced"`
	DailyLimitUSD    float64      `json:"daily_limit_usd"`
	TokensInput      int64        `json:"tokens_input"`
	TokensOutput     int64        `json:"tokens_output"`
	TokensCacheWrite int64        `json:"tokens_cache_write"`
	TokensCacheRead  int64        `json:"tokens_cache_read"`
	PerAgent         []AgentSpend `json:"per_agent"`
}

// Alert é um item de "Precisa de atenção". Kind: stale, limit_warn,
// incomplete, unpriced, plan_limit ou integration.
type Alert struct {
	Kind    string `json:"kind"`
	CardKey string `json:"card_key"`
	Agent   string `json:"agent"`
	Text    string `json:"text"`
	// ResetsAt (só plan_limit): quando a janela reinicia; a UI esconde o
	// alerta quando o relógio do cliente passa dele.
	ResetsAt *time.Time `json:"resets_at,omitempty"`
	// Integration (só integration): o problema e a ação que o conserta.
	Integration *integration.Problem `json:"integration,omitempty"`
}

func (s *server) overviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/overview", s.overview)
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	o, err := s.buildOverview()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, o)
}

var agentNames = map[string]string{"claude": "Claude Code", "codex": "Codex", "kiro": "Kiro"}

func agentName(id string) string {
	if n, ok := agentNames[id]; ok {
		return n
	}
	return id
}

// usd formata em pt-BR: "US$ 8,20".
func usd(v float64) string {
	return "US$ " + strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1)
}

// lastActivityText resume o evento em texto curto pt-BR. Mapeamento:
// file_edit → "Edit <path>", file_read → "Read <path>", command → "Bash <comando>",
// note → "nota: <texto>"; os demais tipos viram uma frase fixa (kinds
// desconhecidos aparecem como estão). Textos longos são cortados em 80 caracteres.
func lastActivityText(e domain.Event) string {
	var d struct {
		Path    string `json:"path"`
		Command string `json:"command"`
		Text    string `json:"text"`
	}
	_ = json.Unmarshal(e.Data, &d)
	var t string
	switch e.Kind {
	case domain.EvFileEdit:
		t = strings.TrimSpace("Edit " + d.Path)
	case domain.EvFileRead:
		t = strings.TrimSpace("Read " + d.Path)
	case domain.EvCommand:
		t = strings.TrimSpace("Bash " + d.Command)
	case domain.EvNote:
		t = "nota: " + d.Text
	case domain.EvPrompt:
		t = "prompt enviado"
	case domain.EvDecision:
		t = "decisão registrada"
	case domain.EvEssentialFile:
		t = "arquivo essencial registrado"
	case domain.EvTodo:
		t = "tarefa atualizada"
	case domain.EvQuestion:
		t = "pergunta registrada"
	case domain.EvSessionStart:
		t = "sessão iniciada"
	case domain.EvSessionEnd:
		t = "sessão encerrada"
	case domain.EvDone:
		t = "marcado como pronto para revisão"
	case domain.EvHowToTest:
		t = "“como testar” registrado"
	case domain.EvLimitPause:
		t = "pausado por limite de custo"
	default:
		t = e.Kind
	}
	if r := []rune(t); len(r) > 80 {
		t = string(r[:79]) + "…"
	}
	return t
}

func (s *server) buildOverview() (Overview, error) {
	now := s.now()
	o := Overview{}
	live, err := s.st.LiveSessions()
	if err != nil {
		return o, err
	}
	cards, err := s.st.ListCards(store.CardFilter{})
	if err != nil {
		return o, err
	}
	byID := make(map[int64]domain.Card, len(cards))
	for _, c := range cards {
		byID[c.ID] = c
	}
	bySession, err := s.totalsBy("session")
	if err != nil {
		return o, err
	}
	byCard, err := s.totalsBy("card")
	if err != nil {
		return o, err
	}

	// Sessões não encerradas: ativas, sem card, e quais cards têm dono vivo.
	// Só conta como viva a sessão cujo processo existe (Session.Live).
	o.Active, o.NoCard = []ActiveSession{}, []SessionWithUsage{}
	liveCards := map[int64]bool{}
	agents := map[string]bool{}
	for _, x := range live {
		if x.Live() {
			agents[x.Agent] = true
		}
		if x.CardID == nil {
			o.NoCard = append(o.NoCard, SessionWithUsage{Session: x, Usage: bySession[x.ID], Unpriced: bySession[x.ID].Unpriced})
		}
		a := ActiveSession{Session: x, SessionUsage: bySession[x.ID]}
		if ev, err := s.st.LastSessionEvent(x.ID); err != nil {
			return o, err
		} else if ev != nil {
			a.LastActivity = lastActivityText(*ev)
		}
		if x.CardID != nil {
			if x.Live() {
				liveCards[*x.CardID] = true
			}
			if c, ok := byID[*x.CardID]; ok {
				a.Card = &c
				a.CardTotals = byCard[c.Key]
				ct, err := s.st.CardContent(c.ID)
				if err != nil {
					return o, err
				}
				rd, err := s.readiness(c, ct, x.Live())
				if err != nil {
					return o, err
				}
				a.Readiness = &rd
			}
		}
		o.Active = append(o.Active, a)
	}
	ended, err := s.st.EndedWithoutCardSince(now.Add(-noCardWindow))
	if err != nil {
		return o, err
	}
	o.NoCard = append(o.NoCard, withUsage(ended, bySession)...)

	// Alertas, na ordem stale, limit_warn, incomplete, unpriced.
	o.Alerts = []Alert{}
	staleSeen := map[int64]bool{}
	for _, x := range live {
		if x.CardID == nil || x.PIDDeadAt == nil || staleSeen[*x.CardID] {
			continue
		}
		c, ok := byID[*x.CardID]
		if !ok || c.Column != domain.EmAndamento {
			continue
		}
		staleSeen[c.ID] = true
		// Minutos desde que o PID sumiu (pid_dead_at), não desde o último evento.
		min := max(int(now.Sub(*x.PIDDeadAt)/time.Minute), 0)
		o.Alerts = append(o.Alerts, Alert{Kind: "stale", CardKey: c.Key, Agent: x.Agent,
			Text: fmt.Sprintf("%s está em “Em andamento” sem sessão viva há %d min.", c.Key, min)})
	}

	// Hoje.
	from, to := board.DayBounds(now)
	todayAgents, err := s.st.Totals(store.TotalsFilter{From: from, To: to, GroupBy: "agent"})
	if err != nil {
		return o, err
	}
	limits, err := s.st.ListLimits()
	if err != nil {
		return o, err
	}
	t := &o.Today
	t.ActiveAgents = len(agents)
	spent := map[string]store.Total{}
	for _, ta := range todayAgents {
		spent[ta.Key] = ta
		t.CostUSD += ta.CostUSD
		t.CostUnpriced = t.CostUnpriced || ta.Unpriced
		t.TokensInput += ta.Input
		t.TokensOutput += ta.Output
		t.TokensCacheWrite += ta.CacheWrite
		t.TokensCacheRead += ta.CacheRead
	}
	t.PerAgent = []AgentSpend{}
	seen := map[string]bool{}
	for _, l := range limits {
		t.DailyLimitUSD += l.DailyUSD
		seen[l.Agent] = true
		ls, err := board.LimitStatus(s.st, l.Agent, nil, now)
		if err != nil {
			return o, err
		}
		t.PerAgent = append(t.PerAgent, AgentSpend{Agent: l.Agent, SpentUSD: ls.AgentSpentUSD, DailyUSD: l.DailyUSD,
			Pct: ls.Pct, WarnPct: l.WarnPct, Action: l.Action, Unpriced: spent[l.Agent].Unpriced})
		if ls.Warn || ls.Over {
			o.Alerts = append(o.Alerts, Alert{Kind: "limit_warn", Agent: l.Agent,
				Text: fmt.Sprintf("%s usou %d%% do limite diário (%s de %s).", agentName(l.Agent), ls.Pct, usd(ls.AgentSpentUSD), usd(l.DailyUSD))})
		}
	}
	for _, ta := range todayAgents {
		if !seen[ta.Key] {
			t.PerAgent = append(t.PerAgent, AgentSpend{Agent: ta.Key, SpentUSD: ta.CostUSD, Unpriced: ta.Unpriced})
		}
	}
	sort.Slice(t.PerAgent, func(i, j int) bool { return t.PerAgent[i].Agent < t.PerAgent[j].Agent })

	set, err := s.st.GetSettings()
	if err != nil {
		return o, err
	}
	for _, c := range cards {
		switch c.Column {
		case domain.EmAndamento:
			t.CardsInProgress++
			if !liveCards[c.ID] {
				t.CardsWithoutLive++
			}
		case domain.Pausado:
			ct, err := s.st.CardContent(c.ID)
			if err != nil {
				return o, err
			}
			rd, err := s.readiness(c, ct, liveCards[c.ID])
			if err != nil {
				return o, err
			}
			if set.IncompleteAction == "mark" && c.HandoffIncomplete && !rd.Ready {
				o.Alerts = append(o.Alerts, Alert{Kind: "incomplete", CardKey: c.Key,
					Text: fmt.Sprintf("%s foi pausado com handoff incompleto.", c.Key)})
			}
		}
	}

	models, err := s.st.Totals(store.TotalsFilter{GroupBy: "model"})
	if err != nil {
		return o, err
	}
	var unpriced []string
	for _, m := range models {
		if m.Unpriced {
			unpriced = append(unpriced, m.Key)
		}
	}
	if len(unpriced) > 0 {
		o.Alerts = append(o.Alerts, Alert{Kind: "unpriced",
			Text: "Há uso de modelos sem preço configurado: " + strings.Join(unpriced, ", ") + "."})
	}

	planAlerts, err := s.planAlerts(now)
	if err != nil {
		return o, err
	}
	o.Alerts = append(o.Alerts, planAlerts...)
	if s.deps.Integration != nil {
		for _, p := range s.deps.Integration.Problems() {
			p := p
			o.Alerts = append(o.Alerts, Alert{Kind: "integration", Agent: "claude", Text: p.Text, Integration: &p})
		}
	}

	if o.Events, err = s.st.RecentEvents(30); err != nil {
		return o, err
	}
	o.Events = nonNil(o.Events)
	return o, nil
}
