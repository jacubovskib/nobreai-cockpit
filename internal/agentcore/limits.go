package agentcore

import (
	"fmt"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// toolBefore nega a ferramenta quando o limite (diário do agente ou do card)
// foi atingido com ação "pause". Uma chamada simples de um subcomando de
// handoff do nobre sempre passa (o agente precisa conseguir registrar o
// handoff); nobre run, setup ou algo encadeado, não (plainNobreCommand).
// Os limites são em dólares: o driver que mede o gasto em créditos
// (SpendUnit) nunca é bloqueado nem mexe na pausa por limite do card.
func (c *Core) toolBefore(sess domain.Session, ev Event) (Reply, error) {
	if !sess.FullCapture {
		return Reply{}, nil
	}
	if su, ok := c.d.(SpendUnit); ok && su.SpendsCredits() {
		return Reply{}, nil
	}
	if ev.Tool.Kind == ToolCommand && plainNobreCommand(ev.Tool.Command) {
		return Reply{}, nil
	}
	now := ev.At
	ls, err := board.LimitStatus(c.st, c.d.Name(), sess.CardID, now)
	if err != nil {
		return Reply{}, err
	}
	if !ls.Over || ls.Action != "pause" {
		if sess.CardID != nil {
			if ok, err := c.st.ResumeFromLimit(*sess.CardID, now); err != nil {
				c.logf("retomar card após limite: %v", err)
			} else if ok {
				c.publish("cards")
			}
		}
		return Reply{}, nil
	}
	detail, err := c.limitDetail(ls, sess.CardID)
	if err != nil {
		return Reply{}, err
	}
	if sess.CardID != nil {
		if ok, err := c.st.PauseForLimit(*sess.CardID, now); err != nil {
			c.logf("pausar card por limite: %v", err)
		} else if ok {
			c.Event(sess.ID, domain.EvLimitPause, map[string]any{"detail": detail}, now)
			c.publish("cards")
		}
	}
	return Reply{Block: &Decision{Reason: "NobreAI Cockpit: limite de custo atingido (" + detail + "). Registre “Onde parei” com nobre note 'texto' (uma chamada só, texto entre aspas simples, sem $, crase, heredoc ou redirecionamento) e encerre."}}, nil
}

// limitDetail descreve o(s) limite(s) atingido(s): "diário: US$ 10,40 de US$
// 10,00" e/ou "card NAC-3: US$ 5,10 de US$ 5,00".
func (c *Core) limitDetail(ls board.LimitState, cardID *int64) (string, error) {
	var parts []string
	if ls.AgentDailyUSD > 0 && ls.AgentSpentUSD >= ls.AgentDailyUSD-1e-9 {
		parts = append(parts, fmt.Sprintf("diário: %s de %s", usd(ls.AgentSpentUSD), usd(ls.AgentDailyUSD)))
	}
	if cardID != nil && ls.CardUSD > 0 && ls.CardSpentUSD >= ls.CardUSD-1e-9 {
		card, err := c.st.GetCardByID(*cardID)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("card %s: %s de %s", card.Key, usd(ls.CardSpentUSD), usd(ls.CardUSD)))
	}
	return strings.Join(parts, "; "), nil
}

// usd formata em pt-BR: "US$ 8,20".
func usd(v float64) string {
	return "US$ " + strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1)
}
