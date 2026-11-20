package claude

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// childID é o id da sessão filha do processo pid na conversa de sessID.
func childID(pid int) string { return sessID + "@" + strconv.Itoa(pid) }

// sharedEnv abre a principal com o PID 100 (vivo) ligada ao card NAC-1 e
// manda um prompt do PID 200 (também vivo): a filha claude:S@200 nasce.
func sharedEnv(t *testing.T) (*env, domain.Card) {
	t.Helper()
	e := newEnv(t)
	e.alive[100], e.alive[200] = true, true
	p := e.project(t)
	c, err := e.st.CreateCard(domain.Card{Title: "Migrar OAuth2", ProjectID: p.ID, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	e.call(t, "session_start", withPID(100), withEnv("NOBRE_RESUME", c.Key))
	e.now = t0.Add(time.Minute)
	e.call(t, "user_prompt_submit", withPID(200))
	return e, c
}

func (e *env) sessionCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSession_SecondLiveProcessGetsChild(t *testing.T) {
	e, c := sharedEnv(t)
	main, err := e.st.GetSession(sessID)
	if err != nil {
		t.Fatal(err)
	}
	if main.PID != 100 || main.CardID == nil || *main.CardID != c.ID || main.ParentSessionID != "" {
		t.Fatalf("principal = %+v", main)
	}
	child, err := e.st.GetSession(childID(200))
	if err != nil {
		t.Fatalf("filha não criada: %v", err)
	}
	if child.ParentSessionID != sessID || child.PID != 200 || child.CardID != nil || !child.Live() {
		t.Fatalf("filha = %+v", child)
	}
	if child.ProjectID == nil || *child.ProjectID != *main.ProjectID || !child.FullCapture ||
		child.Cwd != main.Cwd || child.Branch != main.Branch || child.GitHead != main.GitHead ||
		child.TranscriptPath != main.TranscriptPath || child.ProviderSessionID != main.ProviderSessionID {
		t.Fatalf("filha não herdou da principal: filha %+v principal %+v", child, main)
	}
	// O prompt do PID 200 é da filha.
	evs := e.events(t, domain.EvPrompt)
	if len(evs) != 1 || evs[0].SessionID != childID(200) || evs[0].CardID != nil {
		t.Fatalf("prompts = %+v", evs)
	}
	if got, _ := e.st.GetCard(c.Key); got.Column != domain.EmAndamento {
		t.Fatalf("col = %s", got.Column)
	}
}

func TestSession_AlternatingHooksRouteToOwnSession(t *testing.T) {
	e, _ := sharedEnv(t)
	for i, pid := range []int{100, 200, 100, 200} {
		e.now = t0.Add(time.Duration(i+2) * time.Minute)
		e.call(t, "user_prompt_submit", withPID(pid))
		id := sessID
		if pid == 200 {
			id = childID(200)
		}
		x, err := e.st.GetSession(id)
		if err != nil || x.PID != pid || !x.LastEventAt.Equal(e.now) {
			t.Fatalf("hook %d do PID %d: sessão %+v %v", i, pid, x, err)
		}
	}
	if m, _ := e.st.GetSession(sessID); m.PID != 100 {
		t.Fatalf("PID da principal trocou: %d", m.PID)
	}
	if n := e.sessionCount(t); n != 2 {
		t.Fatalf("sessões = %d", n)
	}
}

func TestSession_ResumeAfterDeathReusesMain(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start", withPID(100)) // 100 não está em e.alive: morto
	e.call(t, "session_start_resume", withPID(300))
	m, _ := e.st.GetSession(sessID)
	if m.PID != 300 || !m.Live() {
		t.Fatalf("principal = %+v", m)
	}
	if n := e.sessionCount(t); n != 1 {
		t.Fatalf("sessões = %d", n)
	}
}

func TestSession_MainEndedReopensMainEvenWithChildPID(t *testing.T) {
	e, _ := sharedEnv(t)
	e.call(t, "session_end", withPID(200)) // filha encerra
	e.call(t, "session_end", withPID(100)) // principal encerra
	e.call(t, "session_start_resume", withPID(200))
	m, _ := e.st.GetSession(sessID)
	if m.PID != 200 || !m.Live() {
		t.Fatalf("principal = %+v", m)
	}
	if ch, _ := e.st.GetSession(childID(200)); ch.Live() {
		t.Fatalf("filha reaberta com a principal encerrada: %+v", ch)
	}
}

func TestSession_SubagentNeverForks(t *testing.T) {
	e := newEnv(t)
	e.alive[100] = true
	e.project(t)
	e.call(t, "session_start", withPID(100))
	e.call(t, "post_tool_use_read", withPID(100), with("agent_id", "a1"))
	if n := e.sessionCount(t); n != 1 {
		t.Fatalf("sessões = %d", n)
	}
	if m, _ := e.st.GetSession(sessID); m.PID != 100 {
		t.Fatalf("principal = %+v", m)
	}
}

// O subagente roda dentro do processo que o chamou: o hook dele traz o PID
// desse processo e segue o mesmo roteamento, mesmo sendo o primeiro hook visto
// do processo.
func TestSession_SubagentFromOtherProcessRoutesToChild(t *testing.T) {
	e := newEnv(t)
	e.alive[100], e.alive[200] = true, true
	e.project(t)
	e.call(t, "session_start", withPID(100))
	e.call(t, "post_tool_use_read", withPID(200), with("agent_id", "a1"))
	ch, err := e.st.GetSession(childID(200))
	if err != nil || ch.ParentSessionID != sessID || ch.PID != 200 || !ch.Live() {
		t.Fatalf("filha = %+v %v", ch, err)
	}
	if m, _ := e.st.GetSession(sessID); m.PID != 100 || !m.Live() {
		t.Fatalf("PID da principal trocou: %+v", m)
	}
	evs := e.events(t, domain.EvFileRead)
	if len(evs) != 1 || evs[0].SessionID != childID(200) {
		t.Fatalf("leituras = %+v", evs)
	}
	// Outro hook de subagente do 200: a mesma filha.
	e.call(t, "post_tool_use_read", withPID(200), with("agent_id", "a1"))
	if n := e.sessionCount(t); n != 2 {
		t.Fatalf("sessões = %d", n)
	}
}

func TestSession_ChildReopensItself(t *testing.T) {
	e, c := sharedEnv(t)
	e.call(t, "session_end", withPID(200))
	if ch, _ := e.st.GetSession(childID(200)); ch.EndedAt == nil {
		t.Fatalf("filha não encerrou: %+v", ch)
	}
	if m, _ := e.st.GetSession(sessID); !m.Live() {
		t.Fatalf("SessionEnd da filha encerrou a principal: %+v", m)
	}
	if got, _ := e.st.GetCard(c.Key); got.Column != domain.EmAndamento {
		t.Fatalf("col = %s", got.Column)
	}
	// SessionEnd repetido da filha encerrada não encerra a principal.
	e.call(t, "session_end", withPID(200))
	if m, _ := e.st.GetSession(sessID); !m.Live() {
		t.Fatalf("SessionEnd repetido encerrou a principal: %+v", m)
	}
	e.now = t0.Add(time.Hour)
	e.call(t, "user_prompt_submit", withPID(200))
	ch, _ := e.st.GetSession(childID(200))
	if !ch.Live() || ch.PID != 200 {
		t.Fatalf("filha não reaberta: %+v", ch)
	}
	if m, _ := e.st.GetSession(sessID); m.PID != 100 {
		t.Fatalf("principal = %+v", m)
	}
	if n := e.sessionCount(t); n != 2 {
		t.Fatalf("sessões = %d", n)
	}
}

func TestIngest_ChildUsesMainSession(t *testing.T) {
	e, _ := sharedEnv(t)
	e.call(t, "stop", withPID(200))
	e.a.Wait()
	e.call(t, "session_end", withPID(200))
	e.a.Wait()
	e.a.BackfillContext(context.Background())
	for _, q := range []string{
		`SELECT COUNT(*) FROM usage WHERE session_id=?`,
		`SELECT COUNT(*) FROM transcript_offsets WHERE session_id=?`,
		`SELECT COUNT(*) FROM tool_outputs WHERE session_id=?`,
		`SELECT COUNT(*) FROM context_turns WHERE session_id=?`,
	} {
		var child int
		if err := e.st.DB.QueryRow(q, childID(200)).Scan(&child); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if child != 0 {
			t.Fatalf("%s da filha = %d", q, child)
		}
	}
	var main int
	if err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM usage WHERE session_id=?`, sessID).Scan(&main); err != nil || main == 0 {
		t.Fatalf("uso da principal = %d %v", main, err)
	}
}

const childLine = "Esta conversa também está aberta em outra janela, com sessão própria no Cockpit; os comandos nobre desta janela valem só para ela."

func TestSessionStart_ChildLine(t *testing.T) {
	e := newEnv(t)
	e.alive[100], e.alive[200] = true, true
	e.project(t)
	mainCtx := additionalContext(t, e.call(t, "session_start", withPID(100)))
	if strings.Contains(mainCtx, childLine) {
		t.Fatalf("principal recebeu a linha da filha: %q", mainCtx)
	}
	childCtx := additionalContext(t, e.call(t, "session_start_resume", withPID(200)))
	if !strings.Contains(childCtx, childLine) {
		t.Fatalf("contexto da filha sem a linha: %q", childCtx)
	}
	if !strings.HasPrefix(childCtx, instructionsFor(mustSettings(t, e))) {
		t.Fatalf("instruções não vêm primeiro: %q", childCtx)
	}
}

func mustSettings(t *testing.T, e *env) domain.Settings {
	t.Helper()
	set, err := e.st.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	return set
}
