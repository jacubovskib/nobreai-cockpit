package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
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

// mainOnlyEvents são os eventos que só o agente principal dispara (o
// subagente termina com SubagentStop): só eles podem mudar o local da sessão.
var mainOnlyEvents = map[string]bool{"SessionStart": true, "UserPromptSubmit": true, "Stop": true}

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

// HandleHook trata um evento de hook. Toda sessão é registrada; sem captura
// completa (projeto cadastrado com capture=true) só sessão e uso são gravados:
// nada de prompts, arquivos, contexto ou bloqueios.
//
// O store não recebe contexto: o trabalho de banco nunca é cancelado junto
// com a requisição (o `nobre hook` desiste em 2 s, mas o daemon termina o que
// começou). A leitura do transcript roda em segundo plano (ver ingestAsync).
func (a *Adapter) HandleHook(_ context.Context, h adapter.HookCall) (adapter.HookReply, error) {
	var p payload
	if err := json.Unmarshal(h.Payload, &p); err != nil {
		return adapter.HookReply{}, fmt.Errorf("payload do hook inválido: %w", err)
	}
	if p.SessionID == "" {
		return adapter.HookReply{}, errors.New("payload do hook sem session_id")
	}
	event := h.Event
	if event == "" {
		event = p.HookEventName
	}
	now := a.deps.Now()
	sess, err := a.session(p, h.AgentPID, event, now)
	if err != nil {
		return adapter.HookReply{}, err
	}
	switch event {
	case "SessionStart":
		return a.sessionStart(sess, p, h.Env, now)
	case "UserPromptSubmit":
		if sess.FullCapture {
			a.event(sess.ID, domain.EvPrompt, map[string]any{"text": p.Prompt}, now)
		} else {
			a.publish("sessions") // última atividade mudou
		}
	case "PreToolUse":
		return a.preToolUse(sess, p, now)
	case "PostToolUse":
		if sess.FullCapture {
			return adapter.HookReply{}, a.postToolUse(sess, p, now)
		}
	case "Stop":
		reply, err := a.stop(sess, p, now)
		a.ingestAsync(sess.ID, transcriptPath(p, sess))
		return reply, err
	case "SessionEnd":
		return a.sessionEnd(sess, p, now)
	}
	return adapter.HookReply{}, nil
}

func transcriptPath(p payload, sess domain.Session) string {
	if p.TranscriptPath != "" {
		return p.TranscriptPath
	}
	return sess.TranscriptPath
}

// session devolve a sessão do evento, criando-a se não existe. Sessão que
// não está viva (encerrada ou com o processo dado como morto) e volta a mandar
// eventos é reaberta, com o PID novo; SessionEnd nunca reabre. As demais
// apenas têm a última atividade atualizada.
func (a *Adapter) session(p payload, pid int, event string, now time.Time) (domain.Session, error) {
	id := "claude:" + p.SessionID
	sess, err := a.st.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		return a.createSession(id, p, pid, now)
	}
	if err != nil {
		return sess, err
	}
	if event != "SessionEnd" && (!sess.Live() || (pid > 0 && pid != sess.PID)) {
		if err := a.st.ReopenSession(id, pid, now); err != nil {
			return sess, err
		}
		a.publish("sessions", "cards")
		sess, err := a.st.GetSession(id)
		if err != nil {
			return sess, err
		}
		// claude --resume depois de um git switch no terminal: o SessionStart
		// (cards pausados do branch) já precisa do branch atual.
		if p.AgentID == "" && mainOnlyEvents[event] {
			if p.Cwd != "" && p.Cwd != sess.Cwd {
				return a.moveSession(sess, p.Cwd)
			}
			return a.followBranch(sess, false), nil
		}
		return sess, nil
	}
	if err := a.st.TouchSession(id, now); err != nil {
		return sess, err
	}
	sess.LastEventAt = now
	// Subagentes com isolation: worktree mandam hooks com o cwd do worktree
	// deles; seguir esses cwds faria a sessão oscilar entre worktrees.
	if p.Cwd != "" && p.Cwd != sess.Cwd && mainOnlyEvents[event] && p.AgentID == "" {
		return a.moveSession(sess, p.Cwd)
	}
	// Mesmo diretório: o branch pode ter sido trocado fora do agente (no
	// terminal do usuário, na IDE), sem nenhum Bash que o PostToolUse visse.
	// Só a sessão acompanha: o usuário pode estar só revisando outro branch, e
	// o card segue o que o próprio agente faz (Bash, nobre branch/start/resume).
	if mainOnlyEvents[event] && p.AgentID == "" {
		return a.followBranch(sess, false), nil
	}
	return sess, nil
}

// moveSession grava o novo diretório da sessão (o agente entrou num worktree,
// por exemplo) com branch, HEAD e worktree de lá. O projeto ligado não muda.
func (a *Adapter) moveSession(sess domain.Session, cwd string) (domain.Session, error) {
	l := a.locate(cwd)
	if err := a.st.UpdateSessionLocation(sess.ID, l); err != nil {
		return sess, err
	}
	a.publish("sessions")
	sess.Cwd, sess.Branch, sess.GitHead, sess.Worktree, sess.WorktreeLinked = l.Cwd, l.Branch, l.GitHead, l.Worktree, l.WorktreeLinked
	a.syncCardBranch(sess)
	return sess, nil
}

// syncCardBranch faz o branch do card da sessão acompanhar o da sessão (a
// base do projeto nunca vira branch do card). Falha só vai para o log.
func (a *Adapter) syncCardBranch(sess domain.Session) {
	if sess.CardID == nil {
		return
	}
	br, _ := a.deps.Git.(gitinfo.BranchReader) // nil quando o Git não lê branches
	changed, err := board.SyncCardBranch(a.st, br, *sess.CardID, sess.Branch)
	if err != nil {
		log.Printf("hook claude: branch do card da sessão %s: %v", sess.ID, err)
		return
	}
	if changed {
		a.publish("cards")
	}
}

// locate consulta o git sobre cwd: branch, HEAD e worktree (este só quando o
// Git sabe ler worktrees).
func (a *Adapter) locate(cwd string) store.SessionLocation {
	l := store.SessionLocation{Cwd: cwd}
	if a.deps.Git == nil || cwd == "" {
		return l
	}
	l.Branch, l.GitHead = a.deps.Git.Branch(cwd), a.deps.Git.Head(cwd)
	if wr, ok := a.deps.Git.(gitinfo.WorktreeReader); ok {
		loc := wr.Locate(cwd)
		l.Worktree, l.WorktreeLinked = loc.Root, loc.Linked
	}
	return l
}

// projectFor acha o projeto de cwd. Num worktree ligado fora do diretório do
// projeto, cwd é traduzido para o mesmo lugar no checkout principal.
func (a *Adapter) projectFor(cwd string) (*domain.Project, error) {
	proj, err := a.st.ProjectForPath(cwd)
	if proj != nil || err != nil || cwd == "" {
		return proj, err
	}
	wr, ok := a.deps.Git.(gitinfo.WorktreeReader)
	if !ok {
		return nil, nil
	}
	loc := wr.Locate(cwd)
	if !loc.Linked {
		return nil, nil
	}
	rel, err := filepath.Rel(loc.Root, cwd)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, nil
	}
	return a.st.ProjectForPath(filepath.Join(loc.Main, rel))
}

// projectRoot é a raiz do projeto vista pela sessão: num worktree ligado, o
// diretório equivalente dentro do worktree.
func (a *Adapter) projectRoot(sess domain.Session, pr domain.Project) string {
	root := filepath.Clean(pr.Path)
	if !sess.WorktreeLinked || sess.Worktree == "" {
		return root
	}
	wr, ok := a.deps.Git.(gitinfo.WorktreeReader)
	if !ok {
		return root
	}
	main, cached := a.repoRoots.Load(root)
	if !cached {
		main = wr.Locate(root).Root
		if main != "" { // falha do git não fica no cache
			a.repoRoots.Store(root, main)
		}
	}
	rel, err := filepath.Rel(main.(string), root)
	if main.(string) == "" || err != nil || strings.HasPrefix(rel, "..") {
		return root
	}
	return filepath.Join(sess.Worktree, rel)
}

func (a *Adapter) createSession(id string, p payload, pid int, now time.Time) (domain.Session, error) {
	l := a.locate(p.Cwd)
	x := domain.Session{ID: id, Agent: a.Name(), ProviderSessionID: p.SessionID, PID: pid, Cwd: p.Cwd,
		Branch: l.Branch, GitHead: l.GitHead, Worktree: l.Worktree, WorktreeLinked: l.WorktreeLinked,
		TranscriptPath: p.TranscriptPath, StartedAt: now, LastEventAt: now}
	proj, err := a.projectFor(p.Cwd)
	if err != nil {
		return x, err
	}
	if proj != nil && proj.Capture {
		x.ProjectID, x.FullCapture = &proj.ID, true
	}
	// Dois hooks simultâneos podem criar a mesma sessão: StartSession ignora o
	// segundo e a releitura devolve a que ficou.
	if err := a.st.StartSession(x); err != nil {
		return x, err
	}
	a.publish("sessions")
	return a.st.GetSession(id)
}

// event grava um evento da sessão e avisa o stream. Falha só vai para o log:
// o hook nunca quebra o agente por causa de um evento.
func (a *Adapter) event(sessionID, kind string, data map[string]any, at time.Time) {
	b, err := json.Marshal(data)
	if err == nil {
		err = a.st.AddEvent(domain.Event{SessionID: sessionID, Kind: kind, Data: b, At: at})
	}
	if err != nil {
		log.Printf("hook claude: evento %s da sessão %s: %v", kind, sessionID, err)
		return
	}
	a.publish("sessions")
}

func (a *Adapter) postToolUse(sess domain.Session, p payload, now time.Time) error {
	in := p.input()
	switch p.ToolName {
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
		path := in.FilePath
		if path == "" {
			path = in.NotebookPath
		}
		if path == "" {
			return nil
		}
		path, err := a.projectRelative(sess, path)
		if err != nil {
			return err
		}
		kind := domain.EvFileEdit
		if p.ToolName == "Read" {
			kind = domain.EvFileRead
		}
		a.event(sess.ID, kind, map[string]any{"path": path}, now)
	case "Bash":
		data := map[string]any{"command": in.Command}
		if code, ok := exitCode(p.ToolResponse); ok {
			data["exit_code"] = code
		}
		a.event(sess.ID, domain.EvCommand, data, now)
		a.followBranch(sess, true)
	}
	return nil
}

// followBranch percebe troca de branch no mesmo diretório (git switch num
// comando Bash, ou fora do agente): grava o branch atual na sessão e devolve
// a sessão atualizada; com syncCard, o card acompanha. Custa um git
// rev-parse por comando, prompt e fim de turno; falhas só vão para o log.
func (a *Adapter) followBranch(sess domain.Session, syncCard bool) domain.Session {
	if a.deps.Git == nil || sess.Cwd == "" {
		return sess
	}
	b := a.deps.Git.Branch(sess.Cwd)
	if b == "" {
		return sess
	}
	if b == sess.Branch {
		// A sessão já acompanhou (uma troca vista no prompt ou no fim de
		// turno, que não move o card): agora que o próprio agente trabalha
		// nesse branch, o card vem junto.
		// Só quando o card estava no branch de antes da troca (seguia a
		// sessão); um nobre branch dado de propósito fica.
		if prev, ok := a.outsideSwitch.Load(sess.ID); ok && syncCard && sess.CardID != nil {
			a.outsideSwitch.Delete(sess.ID)
			if c, err := a.st.GetCardByID(*sess.CardID); err == nil && c.Branch == prev.(string) {
				a.syncCardBranch(sess)
			}
		}
		return sess
	}
	l := a.locate(sess.Cwd)
	// Vale uma leitura só: se a do locate discordar (falhou ou o branch mudou
	// no meio), nada é gravado; o próximo evento tenta de novo.
	if l.Branch != b {
		return sess
	}
	if err := a.st.UpdateSessionLocation(sess.ID, l); err != nil {
		log.Printf("hook claude: branch da sessão %s: %v", sess.ID, err)
		return sess
	}
	a.publish("sessions")
	prev := sess.Branch
	sess.Branch, sess.GitHead, sess.Worktree, sess.WorktreeLinked = l.Branch, l.GitHead, l.Worktree, l.WorktreeLinked
	if syncCard {
		a.outsideSwitch.Delete(sess.ID)
		a.syncCardBranch(sess)
	} else {
		// Guarda o branch de antes da primeira troca de fora (A→B→C: vale A).
		a.outsideSwitch.LoadOrStore(sess.ID, prev)
	}
	return sess
}

// projectRelative devolve o caminho relativo à raiz do projeto da sessão
// quando o arquivo está dentro dela (casa com os arquivos essenciais, que o
// agente registra relativos); fora do projeto, fica absoluto.
func (a *Adapter) projectRelative(sess domain.Session, path string) (string, error) {
	if sess.ProjectID == nil || !filepath.IsAbs(path) {
		return path, nil
	}
	projects, err := a.st.ListProjects()
	if err != nil {
		return "", err
	}
	for _, pr := range projects {
		if pr.ID != *sess.ProjectID {
			continue
		}
		root := a.projectRoot(sess, pr)
		clean := filepath.Clean(path)
		if rel, err := filepath.Rel(root, clean); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
			return rel, nil
		}
	}
	return path, nil
}

// exitCode lê o código de saída do tool_response do Bash, se vier (o Claude
// Code nem sempre o informa; nunca é inventado).
func exitCode(raw json.RawMessage) (int, bool) {
	var r map[string]json.RawMessage
	if json.Unmarshal(raw, &r) != nil {
		return 0, false
	}
	for _, k := range []string{"exit_code", "exitCode"} {
		var code int
		if v, ok := r[k]; ok && json.Unmarshal(v, &code) == nil {
			return code, true
		}
	}
	return 0, false
}

// preToolUse nega a ferramenta quando o limite (diário do agente ou do card)
// foi atingido com ação "pause". Uma chamada simples de um subcomando de
// handoff do nobre sempre passa (o agente precisa conseguir registrar o
// handoff); nobre run, setup ou algo encadeado, não (plainNobreCommand).
func (a *Adapter) preToolUse(sess domain.Session, p payload, now time.Time) (adapter.HookReply, error) {
	if !sess.FullCapture {
		return adapter.HookReply{}, nil
	}
	if p.ToolName == "Bash" && plainNobreCommand(p.input().Command) {
		return adapter.HookReply{}, nil
	}
	ls, err := board.LimitStatus(a.st, a.Name(), sess.CardID, now)
	if err != nil {
		return adapter.HookReply{}, err
	}
	if !ls.Over || ls.Action != "pause" {
		if sess.CardID != nil {
			if ok, err := a.st.ResumeFromLimit(*sess.CardID, now); err != nil {
				log.Printf("hook claude: retomar card após limite: %v", err)
			} else if ok {
				a.publish("cards")
			}
		}
		return adapter.HookReply{}, nil
	}
	detail, err := a.limitDetail(ls, sess.CardID)
	if err != nil {
		return adapter.HookReply{}, err
	}
	if sess.CardID != nil {
		if ok, err := a.st.PauseForLimit(*sess.CardID, now); err != nil {
			log.Printf("hook claude: pausar card por limite: %v", err)
		} else if ok {
			a.event(sess.ID, domain.EvLimitPause, map[string]any{"detail": detail}, now)
			a.publish("cards")
		}
	}
	return jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": "NobreAI Cockpit: limite de custo atingido (" + detail + "). Registre “Onde parei” com nobre note 'texto' (uma chamada só, texto entre aspas simples, sem $, crase, heredoc ou redirecionamento) e encerre.",
	}})
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
func (a *Adapter) stop(sess domain.Session, p payload, now time.Time) (adapter.HookReply, error) {
	if p.StopHookActive || !sess.FullCapture {
		return adapter.HookReply{}, nil
	}
	set, err := a.st.GetSettings()
	if err != nil {
		return adapter.HookReply{}, err
	}
	if sess.CardID != nil {
		if set.IncompleteAction != "ask_agent" || !set.Rules["where_stopped"] {
			return adapter.HookReply{}, nil
		}
		c, err := a.st.GetCardByID(*sess.CardID)
		if err != nil {
			return adapter.HookReply{}, err
		}
		edited, err := a.st.SessionEditedAfter(sess.ID, c.ID, c.WhereStoppedAt)
		if err != nil || !edited {
			return adapter.HookReply{}, err
		}
		reason, err := a.handoffPrompt(c, sess.ID, set, now)
		if err != nil {
			return adapter.HookReply{}, err
		}
		return jsonReply(map[string]any{"decision": "block", "reason": reason})
	}
	if set.ReminderEditThreshold <= 0 || !set.AgentCreatesCards {
		return adapter.HookReply{}, nil
	}
	if reminded, err := a.st.SessionReminded(sess.ID); err != nil || reminded {
		return adapter.HookReply{}, err
	}
	n, err := a.st.DistinctEditedFiles(sess.ID)
	if err != nil || n < set.ReminderEditThreshold {
		return adapter.HookReply{}, err
	}
	if err := a.st.MarkReminded(sess.ID); err != nil {
		return adapter.HookReply{}, err
	}
	return jsonReply(map[string]any{"decision": "block", "reason": fmt.Sprintf(
		`NobreAI Cockpit: esta sessão já editou %d arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`, n)})
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

// endReasons traduz o reason do SessionEnd do Claude Code.
var endReasons = map[string]string{
	"clear":             "conversa limpa (/clear)",
	"logout":            "logout",
	"prompt_input_exit": "usuário saiu",
	"other":             "encerrada pelo agente",
}

// sessionEnd encerra a sessão (o card sai de em_andamento) e só depois lê o
// transcript.
func (a *Adapter) sessionEnd(sess domain.Session, p payload, now time.Time) (adapter.HookReply, error) {
	reason := endReasons[p.Reason]
	if reason == "" {
		reason = p.Reason
	}
	if reason == "" {
		reason = "encerrada pelo agente"
	}
	if err := board.FinishSession(a.st, sess.ID, reason, now); err != nil {
		return adapter.HookReply{}, err
	}
	a.publish("sessions", "cards")
	a.event(sess.ID, domain.EvSessionEnd, map[string]any{"reason": p.Reason}, now)
	a.ingestAsync(sess.ID, transcriptPath(p, sess))
	return adapter.HookReply{}, nil
}

// jsonReply serializa a resposta do hook (sem escapar <, > e &: o texto vai
// para o agente ler).
func jsonReply(v any) (adapter.HookReply, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return adapter.HookReply{}, err
	}
	return adapter.HookReply{Stdout: strings.TrimSuffix(b.String(), "\n")}, nil
}
