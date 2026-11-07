package board

import (
	"math"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// spend grava uso de 1M tokens de entrada por USD de gasto (preço 1 USD/M).
func spend(t *testing.T, st *store.Store, sessionID string, usd float64, at time.Time) {
	t.Helper()
	if err := st.PutPrice(domain.Price{Model: "m", InputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	err := st.UpsertUsage([]domain.UsageRow{{SessionID: sessionID, MessageID: at.String(), Model: "m", Input: int64(math.Round(usd * 1_000_000)), At: at}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLimitStatus(t *testing.T) {
	st := newStore(t)
	c, x := liveCard(t, st, 1)
	if err := st.PutLimit(domain.Limit{Agent: "claude", DailyUSD: 10, CardUSD: 20, WarnPct: 80, Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 15, 0, 0, 0, time.Local)
	spend(t, st, x.ID, 8.20, now.Add(-time.Hour))
	// Ontem não conta no dia, mas conta no card.
	spend(t, st, x.ID, 5, now.Add(-30*time.Hour))

	ls := LimitStatus(st, "claude", &c.ID, now)
	if ls.Pct != 82 || !ls.Warn || ls.Over || ls.AgentDailyUSD != 10 || ls.Action != "pause" {
		t.Fatalf("8,20: %+v", ls)
	}
	if ls.CardUSD != 20 || ls.CardSpentUSD < 13.19 || ls.CardSpentUSD > 13.21 {
		t.Fatalf("card: %+v", ls)
	}

	spend(t, st, x.ID, 1.80, now.Add(-2*time.Hour))
	ls = LimitStatus(st, "claude", nil, now)
	if !ls.Over || ls.Pct != 100 {
		t.Fatalf("10,00: %+v", ls)
	}

	// Limite 0 = sem limite.
	if err := st.PutLimit(domain.Limit{Agent: "claude", WarnPct: 80, Action: "warn"}); err != nil {
		t.Fatal(err)
	}
	ls = LimitStatus(st, "claude", &c.ID, now)
	if ls.Pct != 0 || ls.Warn || ls.Over {
		t.Fatalf("sem limite: %+v", ls)
	}
	// Agente sem linha de limite também.
	if ls = LimitStatus(st, "kiro", nil, now); ls.Over || ls.Warn {
		t.Fatalf("kiro: %+v", ls)
	}
}
