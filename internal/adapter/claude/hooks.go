package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/prompts"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
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
// duplas), Ingester (leitura do transcript) e Responder.
func (a *Adapter) HandleHook(ctx context.Context, h adapter.HookCall) (adapter.HookReply, error) {
	return a.core.Handle(ctx, h)
}

// Respond monta a resposta do Claude a cada evento (contexto do
// SessionStart, avisos, limites, fim de turno). Temporário: implementa o
// agentcore.Responder até as respostas irem para o núcleo.
func (a *Adapter) Respond(_ *agentcore.Core, sess domain.Session, ev agentcore.Event) (agentcore.Reply, error) {
	var p payload
	if err := json.Unmarshal(ev.Raw, &p); err != nil {
		return agentcore.Reply{}, fmt.Errorf("payload do hook inválido: %w", err)
	}
	now := ev.At
	switch ev.Kind {
	case agentcore.Started:
		return a.sessionStart(sess, p, ev.Env, now)
	case agentcore.Prompt:
		// Registro para a atribuição por janela (só o resumo do texto, em
		// toda sessão: o uso de cada janela depende dele). Falha só vai para
		// o log.
		if err := a.st.AddWindowPrompt(sess.ID, promptKey(p.Prompt), now); err != nil {
			log.Printf("hook claude: prompt da janela %s: %v", sess.ID, err)
		}
		var child string
		if sess.ParentSessionID != "" {
			if pending, err := a.st.ClaimChildNotice(sess.ID); err != nil {
				log.Printf("hook claude: aviso da filha %s: %v", sess.ID, err)
			} else if pending {
				child = childPhrase // a filha nasceu fora do SessionStart
			}
		}
		return agentcore.Reply{Context: []string{child, a.noticeText(sess, p, now), a.contextWarning(sess, p)}}, nil
	case agentcore.ToolBefore:
		return a.preToolUse(sess, p, now)
	case agentcore.ToolAfter:
		return agentcore.Reply{Context: []string{a.noticeText(sess, p, now), a.bigOutputWarning(sess, p)}}, nil
	case agentcore.TurnEnd:
		return a.stop(sess, p, now)
	}
	return agentcore.Reply{}, nil
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

// preToolUse nega a ferramenta quando o limite (diário do agente ou do card)
// foi atingido com ação "pause". Uma chamada simples de um subcomando de
// handoff do nobre sempre passa (o agente precisa conseguir registrar o
// handoff); nobre run, setup ou algo encadeado, não (plainNobreCommand).
func (a *Adapter) preToolUse(sess domain.Session, p payload, now time.Time) (agentcore.Reply, error) {
	if !sess.FullCapture {
		return agentcore.Reply{}, nil
	}
	if p.ToolName == "Bash" && plainNobreCommand(p.input().Command) {
		return agentcore.Reply{}, nil
	}
	ls, err := board.LimitStatus(a.st, a.Name(), sess.CardID, now)
	if err != nil {
		return agentcore.Reply{}, err
	}
	if !ls.Over || ls.Action != "pause" {
		if sess.CardID != nil {
			if ok, err := a.st.ResumeFromLimit(*sess.CardID, now); err != nil {
				log.Printf("hook claude: retomar card após limite: %v", err)
			} else if ok {
				a.publish("cards")
			}
		}
		return agentcore.Reply{}, nil
	}
	detail, err := a.limitDetail(ls, sess.CardID)
	if err != nil {
		return agentcore.Reply{}, err
	}
	if sess.CardID != nil {
		if ok, err := a.st.PauseForLimit(*sess.CardID, now); err != nil {
			log.Printf("hook claude: pausar card por limite: %v", err)
		} else if ok {
			a.core.Event(sess.ID, domain.EvLimitPause, map[string]any{"detail": detail}, now)
			a.publish("cards")
		}
	}
	return agentcore.Reply{Block: &agentcore.Decision{Reason: "NobreAI Cockpit: limite de custo atingido (" + detail + "). Registre “Onde parei” com nobre note 'texto' (uma chamada só, texto entre aspas simples, sem $, crase, heredoc ou redirecionamento) e encerre."}}, nil
}

// limitDetail descreve o(s) limite(s) atingido(s): "diário: US$ 10,40 de US$
// 10,00" e/ou "card NAC-3: US$ 5,10 de US$ 5,00".
func (a *Adapter) limitDetail(ls board.LimitState, cardID *int64) (string, error) {
	var parts []string
	if ls.AgentDailyUSD > 0 && ls.AgentSpentUSD >= ls.AgentDailyUSD-1e-9 {
		parts = append(parts, fmt.Sprintf("diário: %s de %s", usd(ls.AgentSpentUSD), usd(ls.AgentDailyUSD)))
	}
	if cardID != nil && ls.CardUSD > 0 && ls.CardSpentUSD >= ls.CardUSD-1e-9 {
		c, err := a.st.GetCardByID(*cardID)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("card %s: %s de %s", c.Key, usd(ls.CardSpentUSD), usd(ls.CardUSD)))
	}
	return strings.Join(parts, "; "), nil
}

// usd formata em pt-BR: "US$ 8,20".
func usd(v float64) string {
	return "US$ " + strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1)
}

// stop decide se o turno é bloqueado. Com card e IncompleteAction
// "ask_agent", bloqueia quando a sessão editou arquivos do card depois do
// último "Onde parei" (o Stop dispara a cada resposta: os demais itens da regra
// de handoff só aparecem na UI e no fim da sessão); com a regra "where_stopped"
// desligada, não bloqueia, pois esse é o único critério por turno. Sem card, lembra uma única
// vez de criar card quando a sessão já editou ReminderEditThreshold arquivos (só se o agente
// cria cards por conta própria: AgentCreatesCards).
func (a *Adapter) stop(sess domain.Session, p payload, now time.Time) (agentcore.Reply, error) {
	if p.StopHookActive || !sess.FullCapture {
		return agentcore.Reply{}, nil
	}
	set, err := a.st.GetSettings()
	if err != nil {
		return agentcore.Reply{}, err
	}
	if sess.CardID != nil {
		if set.IncompleteAction != "ask_agent" || !set.Rules["where_stopped"] {
			return agentcore.Reply{}, nil
		}
		c, err := a.st.GetCardByID(*sess.CardID)
		if err != nil {
			return agentcore.Reply{}, err
		}
		edited, err := a.st.SessionEditedAfter(sess.ID, c.ID, c.WhereStoppedAt)
		if err != nil || !edited {
			return agentcore.Reply{}, err
		}
		reason, err := a.handoffPrompt(c, sess.ID, set, now)
		if err != nil {
			return agentcore.Reply{}, err
		}
		return agentcore.Reply{HoldTurn: &agentcore.Decision{Reason: reason}}, nil
	}
	if set.ReminderEditThreshold <= 0 || !set.AgentCreatesCards {
		return agentcore.Reply{}, nil
	}
	if reminded, err := a.st.SessionReminded(sess.ID); err != nil || reminded {
		return agentcore.Reply{}, err
	}
	n, err := a.st.DistinctEditedFiles(sess.ID)
	if err != nil || n < set.ReminderEditThreshold {
		return agentcore.Reply{}, err
	}
	if err := a.st.MarkReminded(sess.ID); err != nil {
		return agentcore.Reply{}, err
	}
	return agentcore.Reply{HoldTurn: &agentcore.Decision{Reason: fmt.Sprintf(
		`NobreAI Cockpit: esta sessão já editou %d arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`, n)}}, nil
}

const handoffSlug = "fechar-handoff"

// handoffPrompt renderiza o prompt "fechar-handoff" para o card. Se o prompt
// salvo foi apagado ou tem variáveis inválidas, usa o corpo original.
func (a *Adapter) handoffPrompt(c domain.Card, sessionID string, set domain.Settings, now time.Time) (string, error) {
	var seed string
	for _, sp := range prompts.Seeds() {
		if sp.Slug == handoffSlug {
			seed = sp.Body
		}
	}
	body := seed
	p, err := a.st.GetPrompt(handoffSlug)
	switch {
	case err == nil:
		body = p.Body
	case !errors.Is(err, store.ErrNotFound):
		return "", err
	}
	ct, err := a.st.CardContent(c.ID)
	if err != nil {
		return "", err
	}
	lastEdit, err := a.st.LastEventAt(c.ID, domain.EvFileEdit)
	if err != nil {
		return "", err
	}
	readiness := domain.Readiness(c, ct, true, lastEdit, set, now)
	var pkg string
	if strings.Contains(body, "card.pacote") {
		if pkg, err = a.resumePackage(c, sessionID); err != nil {
			return "", err
		}
	}
	vars := resume.Vars(c, ct, pkg, readiness)
	out, err := prompts.Render(body, vars)
	if err != nil {
		if out, err = prompts.Render(seed, vars); err != nil {
			return "", err
		}
	}
	if p.Slug != "" {
		if err := a.st.MarkPromptUsed(handoffSlug, now); err != nil {
			log.Printf("hook claude: marcar uso do prompt %s: %v", handoffSlug, err)
		}
		a.publish("prompts")
	}
	return out, nil
}
