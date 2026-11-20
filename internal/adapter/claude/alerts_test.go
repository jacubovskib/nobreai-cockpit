package claude

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

func TestFmtTokens(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0", 999: "999", 1000: "1 mil", 22000: "22 mil", 22400: "22 mil", 22600: "23 mil", 820000: "820 mil",
		999600: "1 milhão", 1000000: "1 milhão", 1200000: "1,2 milhão", 1960000: "2 milhões", 2500000: "2,5 milhões",
	} {
		if got := fmtTokens(n); got != want {
			t.Errorf("fmtTokens(%d) = %q, quer %q", n, got, want)
		}
	}
}

func TestContextWarningText(t *testing.T) {
	got := contextWarningText(82, 164000)
	want := "Contexto em 82% da janela (164 mil tokens). Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card."
	if got != want {
		t.Fatalf("texto = %q", got)
	}
}

// alertsEnv abre a sessão principal com captura ligada.
func alertsEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start")
	return e
}

// turn grava um turno da janela principal (janela de 200.000 tokens).
func (e *env) turn(t *testing.T, n, segment int, tokens int64) {
	t.Helper()
	err := e.st.SaveContext(sessID, e.transcript, false, nil, []usage.ContextTurn{{
		Turn: n, Segment: segment, MessageID: fmt.Sprintf("m%d", n), At: e.now, Tokens: tokens, Input: tokens,
	}}, 0, usage.ContextState{LastTurn: n, LastMessageID: fmt.Sprintf("m%d", n), Segment: segment})
	if err != nil {
		t.Fatal(err)
	}
}

func ctxWarn(pct, tokens string) string {
	return "Contexto em " + pct + "% da janela (" + tokens + " tokens). Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card."
}

func TestContextWarning_Bands(t *testing.T) {
	e := alertsEnv(t)
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != "" {
		t.Fatalf("sem turnos = %q", got)
	}
	e.turn(t, 0, 0, 100000) // 50%
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != "" {
		t.Fatalf("50%% = %q", got)
	}
	e.turn(t, 1, 0, 124000) // 62%
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != ctxWarn("62", "124 mil") {
		t.Fatalf("62%% = %q", got)
	}
	e.turn(t, 2, 0, 130000) // 65%
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != "" {
		t.Fatalf("65%% repetiu = %q", got)
	}
	e.turn(t, 3, 0, 164000) // 82%
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != ctxWarn("82", "164 mil") {
		t.Fatalf("82%% = %q", got)
	}
	// Nova compactação: as faixas recomeçam.
	e.turn(t, 4, 1, 40000)
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != "" {
		t.Fatalf("pós-compactação 20%% = %q", got)
	}
	e.turn(t, 5, 1, 122000) // 61%
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != ctxWarn("61", "122 mil") {
		t.Fatalf("61%% no segmento novo = %q", got)
	}
}

func TestContextWarning_SkipsBandsAtOnce(t *testing.T) {
	e := alertsEnv(t)
	e.turn(t, 0, 0, 170000) // 85%: só a faixa de 80
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != ctxWarn("85", "170 mil") {
		t.Fatalf("got %q", got)
	}
	e.turn(t, 1, 0, 150000)
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != "" {
		t.Fatalf("faixa de 60 depois da de 80 = %q", got)
	}
}

func TestContextWarning_Off(t *testing.T) {
	e := alertsEnv(t)
	set := mustSettings(t, e)
	set.AgentContextWarnings = false
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	e.turn(t, 0, 0, 170000)
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != "" {
		t.Fatalf("desligado = %q", got)
	}
}

func TestContextWarning_NoCapture(t *testing.T) {
	e := newEnv(t) // sem projeto: sem captura completa
	e.call(t, "session_start")
	e.turn(t, 0, 0, 170000)
	if got := promptContext(t, e.call(t, "user_prompt_submit")); got != "" {
		t.Fatalf("sem captura = %q", got)
	}
}

func TestContextWarning_CombinedWithConfigNotice(t *testing.T) {
	e := alertsEnv(t)
	set := mustSettings(t, e)
	set.AgentCreatesCards = !set.AgentCreatesCards
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	e.turn(t, 0, 0, 170000)
	got := promptContext(t, e.call(t, "user_prompt_submit"))
	if !strings.Contains(got, "a configuração mudou") || !strings.HasSuffix(got, ctxWarn("85", "170 mil")) || !strings.Contains(got, "\n\n") {
		t.Fatalf("contexto = %q", got)
	}
}

func TestContextWarning_StateInDB(t *testing.T) {
	e := alertsEnv(t)
	e.turn(t, 0, 0, 130000)
	e.call(t, "user_prompt_submit")
	a, err := e.st.GetSessionAlerts(sessID)
	if err != nil || a.WarnedPct != 60 || a.WarnedSegment != 0 {
		t.Fatalf("alerts = %+v %v", a, err)
	}
}

// postWithResponse manda um PostToolUse de Read com a resposta do tamanho dado (bytes).
func (e *env) postWithResponse(t *testing.T, bytes int, opts ...callOpt) adapter.HookReply {
	t.Helper()
	resp := map[string]any{"type": "text", "content": strings.Repeat("x", bytes)}
	return e.call(t, "post_tool_use_read", append([]callOpt{with("tool_response", resp)}, opts...)...)
}

func postContext(t *testing.T, r adapter.HookReply) string {
	t.Helper()
	if r.Stdout == "" {
		return ""
	}
	var out struct {
		H struct {
			Name string `json:"hookEventName"`
			Ctx  string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil || out.H.Name != "PostToolUse" {
		t.Fatalf("stdout = %q (%v)", r.Stdout, err)
	}
	return out.H.Ctx
}

func bigWarn(tokens string) string {
	return "Essa saída teve ~" + tokens + " tokens e fica sendo relida a cada turno. Prefira ler trechos (offset/limit, grep) em vez do arquivo inteiro."
}

func TestBigOutputWarning(t *testing.T) {
	e := alertsEnv(t)
	if got := postContext(t, e.postWithResponse(t, 40000)); got != "" { // ~10 mil
		t.Fatalf("saída pequena = %q", got)
	}
	// 88.000 bytes de conteúdo + moldura JSON ~ 22 mil tokens.
	for i := 0; i < 3; i++ {
		if got := postContext(t, e.postWithResponse(t, 88000)); got != bigWarn("22 mil") {
			t.Fatalf("grande %d = %q", i, got)
		}
	}
	if got := postContext(t, e.postWithResponse(t, 88000)); got != "" {
		t.Fatalf("quarta saída grande = %q", got)
	}
}

func TestBigOutputWarning_RespectsSettings(t *testing.T) {
	e := alertsEnv(t)
	set := mustSettings(t, e)
	set.BigOutputTokens, set.BigOutputMax = 1000, 1
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	if got := postContext(t, e.postWithResponse(t, 3000)); got != "" { // ~750
		t.Fatalf("abaixo do limite = %q", got)
	}
	if got := postContext(t, e.postWithResponse(t, 5000)); got != bigWarn("1 mil") { // ~1250
		t.Fatalf("acima do limite = %q", got)
	}
	if got := postContext(t, e.postWithResponse(t, 5000)); got != "" {
		t.Fatalf("além do máximo = %q", got)
	}
}

func TestBigOutputWarning_Off(t *testing.T) {
	e := alertsEnv(t)
	set := mustSettings(t, e)
	set.AgentContextWarnings = false
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	if got := postContext(t, e.postWithResponse(t, 88000)); got != "" {
		t.Fatalf("desligado = %q", got)
	}
}

func TestBigOutputWarning_SubagentIgnored(t *testing.T) {
	e := alertsEnv(t)
	if got := postContext(t, e.postWithResponse(t, 88000, with("agent_id", "a1"))); got != "" {
		t.Fatalf("subagente = %q", got)
	}
}

// O aviso de outra janela vive no banco: sobrevive a um novo Adapter.
func TestChildNoticeSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	e.alive[100], e.alive[200] = true, true
	e.project(t)
	e.call(t, "session_start", withPID(100))
	e.call(t, "post_tool_use_read", withPID(200))
	e.a = New(e.st, adapter.Deps{Now: func() time.Time { return e.now }, Git: fakeGit{branch: "main"}})
	e.a.SetAlive(func(pid int) bool { return e.alive[pid] })
	if got := promptContext(t, e.call(t, "user_prompt_submit", withPID(200))); got != childLine {
		t.Fatalf("depois do reinício = %q", got)
	}
}
