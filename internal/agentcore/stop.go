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

// turnEnd decide se o fim do turno é segurado.
//
// Com card, junta (com uma linha em branco entre elas) as partes que valerem:
// o prompt de handoff, quando IncompleteAction é "ask_agent", a regra
// "where_stopped" está ligada e a sessão editou arquivos do card depois do
// último "Onde parei" (o fim de turno vem a cada resposta: os demais itens da
// regra de handoff só aparecem na UI e no fim da sessão); e os lembretes de
// plano e de tarefas (planReminders), que não dependem dessas condições.
//
// Sem card, lembra uma única vez de criar card quando a sessão já editou
// ReminderEditThreshold arquivos ou escreveu um plano (só se o agente cria
// cards por conta própria: AgentCreatesCards). Se o plano vem depois desse
// lembrete, sai um lembrete curto, também uma única vez.
func (c *Core) turnEnd(sess domain.Session, ev Event) (Reply, error) {
	if ev.StopActive || !sess.FullCapture {
		return Reply{}, nil
	}
	set, err := c.st.GetSettings()
	if err != nil {
		return Reply{}, err
	}
	if sess.CardID != nil {
		return c.cardTurnEnd(sess, set, ev)
	}
	if set.ReminderEditThreshold <= 0 || !set.AgentCreatesCards {
		return Reply{}, nil
	}
	reminded, err := c.st.SessionReminded(sess.ID)
	if err != nil {
		return Reply{}, err
	}
	plan, err := c.sessionPlan(sess.ID, set)
	if err != nil {
		return Reply{}, err
	}
	if reminded {
		if plan == "" {
			return Reply{}, nil
		}
		if done, err := c.st.SessionPlanReminded(sess.ID); err != nil || done {
			return Reply{}, err
		}
		if err := c.st.MarkPlanReminded(sess.ID); err != nil {
			return Reply{}, err
		}
		return Reply{HoldTurn: &Decision{Reason: fmt.Sprintf(
			`NobreAI Cockpit: esta sessão escreveu um plano (%s) sem card. Crie o card com nobre start "<título>" e registre as tarefas com nobre plan.`, plan)}}, nil
	}
	n, err := c.st.DistinctEditedFiles(sess.ID)
	if err != nil || (n < set.ReminderEditThreshold && plan == "") {
		return Reply{}, err
	}
	if err := c.st.MarkReminded(sess.ID); err != nil {
		return Reply{}, err
	}
	files := fmt.Sprintf("%d arquivos", n)
	if n == 1 {
		files = "1 arquivo"
	}
	reason := fmt.Sprintf(`NobreAI Cockpit: esta sessão já editou %s sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`, files)
	if plan != "" {
		if err := c.st.MarkPlanReminded(sess.ID); err != nil {
			return Reply{}, err
		}
		reason += fmt.Sprintf("\n"+`Inclui um plano (%s): crie o card com nobre start "<título>" e registre as tarefas com nobre plan.`, plan)
	}
	return Reply{HoldTurn: &Decision{Reason: reason}}, nil
}

// cardTurnEnd é o fim de turno de uma sessão com card (ver turnEnd).
func (c *Core) cardTurnEnd(sess domain.Session, set domain.Settings, ev Event) (Reply, error) {
	card, err := c.st.GetCardByID(*sess.CardID)
	if err != nil {
		return Reply{}, err
	}
	var parts []string
	if set.IncompleteAction == "ask_agent" && set.Rules["where_stopped"] {
		edited, err := c.st.SessionEditedAfter(sess.ID, card.ID, card.WhereStoppedAt)
		if err != nil {
			return Reply{}, err
		}
		if edited {
			reason, err := c.handoffPrompt(card, sess.ID, set, ev.At)
			if err != nil {
				return Reply{}, err
			}
			parts = append(parts, reason)
		}
	}
	ct, err := c.st.CardContent(card.ID)
	if err != nil {
		return Reply{}, err
	}
	more, err := c.planReminders(sess, card, ct, set)
	if err != nil {
		return Reply{}, err
	}
	parts = append(parts, more...)
	if len(parts) == 0 {
		return Reply{}, nil
	}
	return Reply{HoldTurn: &Decision{Reason: strings.Join(parts, "\n\n")}}, nil
}

// planReminders devolve os lembretes de plano e de tarefas do card, na ordem:
// plano escrito sem registro (uma vez por sessão) e tarefas para trás (houve
// commit da sessão desde a última mudança de tarefa; uma vez por mudança).
func (c *Core) planReminders(sess domain.Session, card domain.Card, ct domain.Content, set domain.Settings) ([]string, error) {
	var out []string
	if ct.Plan == nil && len(set.PlanPatterns) > 0 {
		done, err := c.st.SessionPlanReminded(sess.ID)
		if err != nil {
			return nil, err
		}
		if !done {
			plan, err := c.sessionPlan(sess.ID, set)
			if err != nil {
				return nil, err
			}
			if plan != "" {
				if err := c.st.MarkPlanReminded(sess.ID); err != nil {
					return nil, err
				}
				out = append(out, fmt.Sprintf(
					`NobreAI Cockpit: você escreveu um plano (%s) e o card %s ainda não tem as tarefas. Registre com nobre plan %s --task "<tarefa 1>" --task "<tarefa 2>" ...`,
					plan, card.Key, plan))
			}
		}
	}
	if ct.Plan.Open() > 0 {
		last, err := c.st.LastPlanChange(card.ID)
		if err != nil || last == nil {
			return out, err
		}
		committed, err := c.st.SessionCommittedAfter(sess.ID, *last)
		if err != nil || !committed {
			return out, err
		}
		mark := store.FmtTime(*last)
		prev, err := c.st.TaskRemindedFor(sess.ID)
		if err != nil || prev == mark {
			return out, err
		}
		if err := c.st.SetTaskRemindedFor(sess.ID, mark); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf(
			"NobreAI Cockpit: houve commit desde a última atualização das tarefas do card %s. Atualize com nobre task <n> andamento|revisao|concluida.", card.Key))
	}
	return out, nil
}

// sessionPlan devolve o primeiro caminho editado pela sessão que casa com um
// padrão de plano; "" se nenhum (ou sem padrões).
func (c *Core) sessionPlan(sessionID string, set domain.Settings) (string, error) {
	if len(set.PlanPatterns) == 0 {
		return "", nil
	}
	paths, err := c.st.SessionEditedPaths(sessionID)
	if err != nil {
		return "", err
	}
	for _, p := range paths {
		if domain.MatchPlan(set.PlanPatterns, p) {
			return p, nil
		}
	}
	return "", nil
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
