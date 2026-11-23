package claude

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// payload é o JSON que o Claude Code entrega aos hooks. Todo evento tem
// session_id, transcript_path, cwd e hook_event_name; os demais campos
// dependem do evento.
type payload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	// SessionStart: startup | resume | clear | compact.
	Source string `json:"source"`
	// UserPromptSubmit.
	Prompt string `json:"prompt"`
	// PreToolUse / PostToolUse. tool_input e tool_response variam por
	// ferramenta: são lidos sob demanda.
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
	// Stop.
	StopHookActive bool `json:"stop_hook_active"`
	// SessionEnd.
	Reason string `json:"reason"`
	// AgentID vem preenchido quando o hook dispara dentro de um subagente
	// (que usa o session_id do agente principal).
	AgentID string `json:"agent_id"`
}

type toolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Command      string `json:"command"`
}

func (p payload) input() toolInput {
	var in toolInput
	_ = json.Unmarshal(p.ToolInput, &in)
	return in
}

// HandleHook trata um evento de hook pelo núcleo de sessões (agentcore): o
// Claude é o driver (Translate, Render) com as capacidades Router (janelas
// duplas), Observer (prompt de cada janela), Ingester (leitura do
// transcript), Noter (aviso da sessão filha), Memories e Guide (a skill).
func (a *Adapter) HandleHook(ctx context.Context, h adapter.HookCall) (adapter.HookReply, error) {
	return a.core.Handle(ctx, h)
}

// Ingest lê o transcript da sessão em segundo plano (agentcore.Ingester).
func (a *Adapter) Ingest(sess domain.Session, ev agentcore.Event) {
	path := ev.TranscriptPath
	if path == "" {
		path = sess.TranscriptPath
	}
	a.ingestAsync(ingestID(sess), path)
}

// Route devolve a sessão do evento (agentcore.Router). Cada processo vivo da
// conversa tem a sua (ver route): o segundo processo ganha uma sessão filha.
// A sessão escolhida segue a regra do núcleo (Continue): reabre se não está
// viva, senão só tem a última atividade atualizada. Conversa que o Cockpit
// ainda não conhece é criada por qualquer evento, SessionEnd inclusive (o
// daemon esteve fora a sessão inteira: o uso do transcript não se perde).
func (a *Adapter) Route(c *agentcore.Core, ev agentcore.Event) (domain.Session, bool, error) {
	main, err := a.st.GetSession(c.SessionID(ev))
	if errors.Is(err, store.ErrNotFound) {
		sess, err := c.CreateSession(ev)
		return sess, true, err
	}
	if err != nil {
		return main, true, err
	}
	sess, child, err := a.route(main, ev.ProviderSessionID, ev.PID, ev.Kind)
	if err != nil {
		return sess, true, err
	}
	if child {
		if ev.Kind == agentcore.Ended {
			// Fim de um processo que nunca mandou outro hook: não há sessão
			// dele para encerrar, e a principal é de outro processo vivo.
			return sess, true, agentcore.ErrIgnore
		}
		sess, err := a.createChild(c, main, ev)
		return sess, true, err
	}
	sess, err = c.Continue(sess, ev)
	return sess, true, err
}

// route escolhe a sessão do processo pid na conversa da principal main:
//   - a sessão viva da conversa com esse PID (a principal ou uma filha);
//   - senão, se outro processo ainda vivo é dono da principal viva e ela
//     segue sendo a sessão atual desse processo, a filha deste processo
//     (child = true quando ela ainda não existe; uma filha encerrada é
//     devolvida para ser reaberta);
//   - senão (PID desconhecido, principal encerrada, dono morto ou que já
//     passou para outra conversa), a principal: é o --resume normal, que a
//     reabre com o PID novo.
//
// Assim o PID da principal nunca muda enquanto o dono dela está vivo. Hooks de
// subagente seguem a mesma regra: o subagente roda dentro do processo que o
// chamou e o hook traz o PID dele (com o PID do dono, fica na principal).
// SessionEnd nunca reabre: vai para a sessão desse PID mesmo encerrada.
func (a *Adapter) route(main domain.Session, sid string, pid int, kind agentcore.Kind) (sess domain.Session, child bool, err error) {
	if pid <= 0 {
		return main, false, nil
	}
	own, err := a.st.SessionByProviderPID(a.Name(), sid, pid)
	if err != nil {
		return main, false, err
	}
	if own != nil && (own.Live() || kind == agentcore.Ended) {
		return *own, false, nil
	}
	shared, err := a.ownedElsewhere(main, pid)
	if err != nil || !shared {
		return main, false, err
	}
	if own != nil && own.ParentSessionID == main.ID {
		return *own, false, nil
	}
	return main, true, nil
}

// ownedElsewhere diz se a principal viva pertence a outro processo que ainda
// existe e cuja sessão atual é ela (um dono que passou para outra conversa
// sem SessionEnd não segura mais a principal).
func (a *Adapter) ownedElsewhere(main domain.Session, pid int) (bool, error) {
	if !main.Live() || main.PID <= 0 || main.PID == pid || !a.alive(main.PID) {
		return false, nil
	}
	cur, err := a.st.SessionByPID(main.PID)
	if err != nil {
		return false, err
	}
	return cur != nil && cur.ID == main.ID, nil
}

// createChild cria a sessão filha do processo do evento: herda da principal
// o projeto, a captura, o local e o transcript, sem card.
func (a *Adapter) createChild(c *agentcore.Core, main domain.Session, ev agentcore.Event) (domain.Session, error) {
	id := main.ID + "@" + strconv.Itoa(ev.PID)
	tp := main.TranscriptPath
	if tp == "" {
		tp = ev.TranscriptPath
	}
	now := ev.At
	x := domain.Session{ID: id, Agent: main.Agent, ProviderSessionID: main.ProviderSessionID, ParentSessionID: main.ID,
		PID: ev.PID, ProjectID: main.ProjectID, FullCapture: main.FullCapture, Cwd: main.Cwd, Branch: main.Branch,
		GitHead: main.GitHead, Worktree: main.Worktree, WorktreeLinked: main.WorktreeLinked, TranscriptPath: tp,
		StartedAt: now, LastEventAt: now}
	// A outra janela pode estar noutro diretório (outro worktree): a filha
	// nasce no cwd do próprio hook, como uma sessão nova. Hook de subagente
	// manda o cwd do worktree dele e não conta. O projeto é o da principal.
	if ev.SubagentID == "" && ev.Cwd != "" && ev.Cwd != main.Cwd {
		l := c.Locate(ev.Cwd)
		x.Cwd, x.Branch, x.GitHead, x.Worktree, x.WorktreeLinked = l.Cwd, l.Branch, l.GitHead, l.Worktree, l.WorktreeLinked
	}
	// Dois hooks simultâneos do mesmo processo: o segundo é ignorado e a
	// releitura devolve a que ficou.
	if err := a.st.StartSession(x); err != nil {
		return x, err
	}
	if ev.Kind != agentcore.Started {
		// Sem SessionStart (o daemon foi instalado ou voltou com a conversa
		// aberta): a linha da filha vai no próximo prompt.
		if err := a.st.MarkChildNoticePending(id); err != nil {
			log.Printf("hook claude: aviso da filha %s: %v", id, err)
		}
	}
	a.publish("sessions")
	sess, err := a.st.GetSession(id)
	if err != nil {
		return sess, err
	}
	return c.FollowMain(sess, ev)
}

// ingestID é a sessão em que o uso e o contexto do transcript são gravados:
// a filha divide o transcript com a principal e lê com o id dela (mesmos
// offsets e linhas, sem contar em dobro).
func ingestID(sess domain.Session) string {
	if sess.ParentSessionID != "" {
		return sess.ParentSessionID
	}
	return sess.ID
}
