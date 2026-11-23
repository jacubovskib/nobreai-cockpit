package agentcore

import (
	"errors"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// ErrIgnore marca o hook que não pertence a nenhuma sessão: é respondido
// vazio, sem gravar nada.
var ErrIgnore = errors.New("hook ignorado")

// SessionID é o id no Cockpit da sessão principal do evento:
// "<agente>:<id da sessão no agente>".
func (c *Core) SessionID(ev Event) string { return c.d.Name() + ":" + ev.ProviderSessionID }

// session devolve a sessão do evento: a do Router, quando o driver tem um e
// resolve; senão a principal, criada se não existe. O fim de uma sessão que
// não existe é ignorado (nunca cria).
func (c *Core) session(ev Event) (domain.Session, error) {
	if r, ok := c.d.(Router); ok {
		sess, handled, err := r.Route(c, ev)
		if handled || err != nil {
			return sess, err
		}
	}
	main, err := c.st.GetSession(c.SessionID(ev))
	if errors.Is(err, store.ErrNotFound) {
		if ev.Kind == Ended {
			return main, ErrIgnore
		}
		return c.CreateSession(ev)
	}
	if err != nil {
		return main, err
	}
	return c.Continue(main, ev)
}

// CreateSession cria a sessão principal do evento, no local do cwd e ligada
// ao projeto desse local quando ele tem captura completa.
func (c *Core) CreateSession(ev Event) (domain.Session, error) {
	id := c.SessionID(ev)
	l := c.Locate(ev.Cwd)
	x := domain.Session{ID: id, Agent: c.d.Name(), ProviderSessionID: ev.ProviderSessionID, PID: ev.PID, Cwd: ev.Cwd,
		Branch: l.Branch, GitHead: l.GitHead, Worktree: l.Worktree, WorktreeLinked: l.WorktreeLinked,
		TranscriptPath: ev.TranscriptPath, StartedAt: ev.At, LastEventAt: ev.At}
	proj, err := c.st.ProjectForLocation(ev.Cwd, l)
	if err != nil {
		return x, err
	}
	if proj != nil && proj.Capture {
		x.ProjectID, x.FullCapture = &proj.ID, true
	}
	// Dois hooks simultâneos podem criar a mesma sessão: StartSession ignora o
	// segundo e a releitura devolve a que ficou.
	if err := c.st.StartSession(x); err != nil {
		return x, err
	}
	c.publish("sessions")
	return c.st.GetSession(id)
}

// Continue registra o evento numa sessão que já existe. Sessão que não está
// viva (encerrada ou com o processo dado como morto) ou que chega com outro
// PID é reaberta com o PID novo; o fim da sessão nunca reabre. As demais
// apenas têm a última atividade atualizada. Depois, a sessão acompanha o
// local do agente principal (FollowMain).
func (c *Core) Continue(sess domain.Session, ev Event) (domain.Session, error) {
	id := sess.ID
	if ev.Kind != Ended && (!sess.Live() || (ev.PID > 0 && ev.PID != sess.PID)) {
		if err := c.st.ReopenSession(id, ev.PID, ev.At); err != nil {
			return sess, err
		}
		c.publish("sessions", "cards")
		sess, err := c.st.GetSession(id)
		if err != nil {
			return sess, err
		}
		// Retomada depois de um git switch no terminal: o início da sessão
		// (cards pausados do branch) já precisa do branch atual.
		return c.FollowMain(sess, ev)
	}
	if err := c.st.TouchSession(id, ev.At); err != nil {
		return sess, err
	}
	sess.LastEventAt = ev.At
	return c.FollowMain(sess, ev)
}

// mainOnly diz se o evento é dos que só o agente principal dispara: só eles
// podem mudar o local da sessão.
func mainOnly(k Kind) bool { return k == Started || k == Prompt || k == TurnEnd }

// FollowMain faz a sessão acompanhar o local do agente principal nos eventos
// que só ele dispara. Subagentes isolados em worktrees mandam hooks com o cwd
// deles; seguir esses cwds faria a sessão oscilar entre worktrees.
func (c *Core) FollowMain(sess domain.Session, ev Event) (domain.Session, error) {
	if ev.SubagentID != "" || !mainOnly(ev.Kind) {
		return sess, nil
	}
	if ev.Cwd != "" && ev.Cwd != sess.Cwd {
		return c.moveSession(sess, ev.Cwd)
	}
	// Mesmo diretório: o branch pode ter sido trocado fora do agente (no
	// terminal do usuário, na IDE), sem nenhum comando que o ToolAfter visse.
	// Só a sessão acompanha: o usuário pode estar só revisando outro branch, e
	// o card segue o que o próprio agente faz (comandos, nobre branch/start/resume).
	return c.followBranch(sess, false), nil
}

// end encerra a sessão (o card sai de em_andamento) e só depois pede a
// leitura do histórico.
func (c *Core) end(sess domain.Session, ev Event) error {
	reason := ev.Reason
	if reason == "" {
		reason = "encerrada pelo agente"
	}
	if err := board.FinishSession(c.st, sess.ID, reason, ev.At); err != nil {
		return err
	}
	c.publish("sessions", "cards")
	c.Event(sess.ID, domain.EvSessionEnd, map[string]any{"reason": ev.ReasonCode}, ev.At)
	c.ingest(sess, ev)
	return nil
}
