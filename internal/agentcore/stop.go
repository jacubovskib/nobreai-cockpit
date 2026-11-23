package agentcore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/prompts"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// turnEnd decide se o fim do turno é segurado. Com card e IncompleteAction
// "ask_agent", segura quando a sessão editou arquivos do card depois do
// último "Onde parei" (o fim de turno vem a cada resposta: os demais itens da
// regra de handoff só aparecem na UI e no fim da sessão); com a regra
// "where_stopped" desligada, não segura, pois esse é o único critério por
// turno. Sem card, lembra uma única vez de criar card quando a sessão já
// editou ReminderEditThreshold arquivos (só se o agente cria cards por conta
// própria: AgentCreatesCards).
func (c *Core) turnEnd(sess domain.Session, ev Event) (Reply, error) {
	if ev.StopActive || !sess.FullCapture {
		return Reply{}, nil
	}
	set, err := c.st.GetSettings()
	if err != nil {
		return Reply{}, err
	}
	if sess.CardID != nil {
		if set.IncompleteAction != "ask_agent" || !set.Rules["where_stopped"] {
			return Reply{}, nil
		}
		card, err := c.st.GetCardByID(*sess.CardID)
		if err != nil {
			return Reply{}, err
		}
		edited, err := c.st.SessionEditedAfter(sess.ID, card.ID, card.WhereStoppedAt)
		if err != nil || !edited {
			return Reply{}, err
		}
		reason, err := c.handoffPrompt(card, sess.ID, set, ev.At)
		if err != nil {
			return Reply{}, err
		}
		return Reply{HoldTurn: &Decision{Reason: reason}}, nil
	}
	if set.ReminderEditThreshold <= 0 || !set.AgentCreatesCards {
		return Reply{}, nil
	}
	if reminded, err := c.st.SessionReminded(sess.ID); err != nil || reminded {
		return Reply{}, err
	}
	n, err := c.st.DistinctEditedFiles(sess.ID)
	if err != nil || n < set.ReminderEditThreshold {
		return Reply{}, err
	}
	if err := c.st.MarkReminded(sess.ID); err != nil {
		return Reply{}, err
	}
	return Reply{HoldTurn: &Decision{Reason: fmt.Sprintf(
		`NobreAI Cockpit: esta sessão já editou %d arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`, n)}}, nil
}

const handoffSlug = "fechar-handoff"

// handoffPrompt renderiza o prompt "fechar-handoff" para o card. Se o prompt
// salvo foi apagado ou tem variáveis inválidas, usa o corpo original.
func (c *Core) handoffPrompt(card domain.Card, sessionID string, set domain.Settings, now time.Time) (string, error) {
	var seed string
	for _, sp := range prompts.Seeds() {
		if sp.Slug == handoffSlug {
			seed = sp.Body
		}
	}
	body := seed
	p, err := c.st.GetPrompt(handoffSlug)
	switch {
	case err == nil:
		body = p.Body
	case !errors.Is(err, store.ErrNotFound):
		return "", err
	}
	ct, err := c.st.CardContent(card.ID)
	if err != nil {
		return "", err
	}
	lastEdit, err := c.st.LastEventAt(card.ID, domain.EvFileEdit)
	if err != nil {
		return "", err
	}
	readiness := domain.Readiness(card, ct, true, lastEdit, set, now)
	var pkg string
	if strings.Contains(body, "card.pacote") {
		if pkg, err = c.resumePackage(card, sessionID); err != nil {
			return "", err
		}
	}
	vars := resume.Vars(card, ct, pkg, readiness)
	out, err := prompts.Render(body, vars)
	if err != nil {
		if out, err = prompts.Render(seed, vars); err != nil {
			return "", err
		}
	}
	if p.Slug != "" {
		if err := c.st.MarkPromptUsed(handoffSlug, now); err != nil {
			c.logf("marcar uso do prompt %s: %v", handoffSlug, err)
		}
		c.publish("prompts")
	}
	return out, nil
}
