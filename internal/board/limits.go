// Package board reúne as regras que mantêm o board coerente com o mundo real:
// reconciliação de sessões cujo processo morreu e limites de gasto.
package board

import (
	"math"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// LimitState é o gasto de um agente (e de um card) frente aos limites.
// Limite 0 = sem limite. Pct é o inteiro (piso) de gasto diário / limite diário.
type LimitState struct {
	AgentSpentUSD float64
	AgentDailyUSD float64
	CardSpentUSD  float64
	CardUSD       float64
	Pct           int
	// Warn: o gasto diário atingiu warn_pct do limite sem estourá-lo.
	Warn bool
	// Over: o gasto diário atingiu o limite diário, ou o card atingiu o limite do card.
	Over   bool
	Action string // "pause" ou "warn" (vazio sem limite configurado)
}

// DayBounds devolve o dia civil local de now: [00:00, 00:00 do dia seguinte).
func DayBounds(now time.Time) (from, to time.Time) {
	n := now.In(time.Local)
	from = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
	return from, from.AddDate(0, 0, 1)
}

// LimitStatus calcula o estado dos limites do agente hoje e, se cardID não for
// nil, do card (gasto acumulado do card, sem recorte de dia). Erros de leitura
// resultam em estado vazio (sem aviso), nunca em bloqueio.
func LimitStatus(st *store.Store, agent string, cardID *int64, now time.Time) LimitState {
	var ls LimitState
	limits, err := st.ListLimits()
	if err != nil {
		return ls
	}
	for _, l := range limits {
		if l.Agent == agent {
			ls.AgentDailyUSD, ls.CardUSD, ls.Action = l.DailyUSD, l.CardUSD, l.Action
			from, to := DayBounds(now)
			if ts, err := st.Totals(store.TotalsFilter{From: from, To: to, GroupBy: "agent"}); err == nil {
				for _, t := range ts {
					if t.Key == agent {
						ls.AgentSpentUSD = t.CostUSD
					}
				}
			}
			if l.DailyUSD > 0 {
				// +1e-9 absorve o erro binário (8,2/10*100 = 81,99999…).
				ls.Pct = int(math.Floor(ls.AgentSpentUSD*100/l.DailyUSD + 1e-9))
				ls.Over = ls.AgentSpentUSD >= l.DailyUSD-1e-9
				ls.Warn = !ls.Over && ls.Pct >= l.WarnPct
			}
			if cardID != nil && l.CardUSD > 0 {
				if c, err := st.GetCardByID(*cardID); err == nil {
					if ts, err := st.Totals(store.TotalsFilter{GroupBy: "card"}); err == nil {
						for _, t := range ts {
							if t.Key == c.Key {
								ls.CardSpentUSD = t.CostUSD
							}
						}
					}
					if ls.CardSpentUSD >= l.CardUSD-1e-9 {
						ls.Over = true
					}
				}
			}
			break
		}
	}
	return ls
}
