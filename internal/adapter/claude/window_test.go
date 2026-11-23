package claude

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// Linhas de transcript para a atribuição por janela. Os horários são
// relativos a t0 (o relógio dos hooks), para casar com os prompts.

func ts(d time.Duration) string { return t0.Add(d).UTC().Format(time.RFC3339Nano) }

func parentField(parent string) string {
	if parent == "" {
		return `"parentUuid":null`
	}
	return `"parentUuid":"` + parent + `"`
}

func wPrompt(uuid, parent, text string, at time.Duration) string {
	return `{"type":"user",` + parentField(parent) + `,"uuid":"` + uuid + `","timestamp":"` + ts(at) +
		`","message":{"role":"user","content":"` + text + `"}}` + "\n"
}

// wAssistant é um turno (message.id msg) com um tool_use toolID (se não vazio).
func wAssistant(uuid, parent, msg, toolID string, at time.Duration) string {
	content := `[]`
	if toolID != "" {
		content = `[{"type":"tool_use","id":"` + toolID + `","name":"Bash","input":{"command":"ls"}}]`
	}
	return `{"type":"assistant",` + parentField(parent) + `,"uuid":"` + uuid + `","timestamp":"` + ts(at) +
		`","message":{"id":"` + msg + `","model":"claude-opus-5-5","usage":{"input_tokens":1000000,"cache_read_input_tokens":100,"output_tokens":5},"content":` +
		content + `}}` + "\n"
}

func wResult(uuid, parent, toolID string, at time.Duration) string {
	return `{"type":"user",` + parentField(parent) + `,"uuid":"` + uuid + `","timestamp":"` + ts(at) +
		`","message":{"content":[{"type":"tool_result","tool_use_id":"` + toolID + `","content":"abcdefgh"}]}}` + "\n"
}

// windowEnv abre a principal (PID 100) no card NAC-1 sem prompt nenhum e
// zera o transcript, sem subagente (os testes escrevem o seu). As filhas nascem no primeiro
// prompt de outro PID vivo (ver prompt).
func windowEnv(t *testing.T) (*env, domain.Card) {
	t.Helper()
	e := newEnv(t)
	e.alive[100], e.alive[200], e.alive[300] = true, true, true
	p := e.project(t)
	c, err := e.st.CreateCard(domain.Card{Title: "Principal", ProjectID: p.ID, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	e.call(t, "session_start", withPID(100), withEnv("NOBRE_RESUME", c.Key))
	if err := os.WriteFile(e.transcript, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Sem o subagente do fixture: o uso dele somaria na principal.
	if err := os.Remove(strings.TrimSuffix(e.transcript, ".jsonl") + "/subagents/agent-a1.jsonl"); err != nil {
		t.Fatal(err)
	}
	return e, c
}

// prompt manda o UserPromptSubmit do PID pid com o texto, no horário t0+at.
func (e *env) prompt(t *testing.T, pid int, text string, at time.Duration) {
	t.Helper()
	e.now = t0.Add(at)
	e.call(t, "user_prompt_submit", withPID(pid), with("prompt", text))
}

// childCard liga a filha do PID pid a um card novo.
func (e *env) childCard(t *testing.T, pid int, title string) domain.Card {
	t.Helper()
	c, err := e.st.CreateCard(domain.Card{Title: title, ProjectID: 1, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.AttachSession(childID(pid), c.Key, false); err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *env) read(t *testing.T) {
	t.Helper()
	if _, err := e.a.readTranscripts(sessID, e.transcript); err != nil {
		t.Fatal(err)
	}
}

// turnWindows: message_id → janela dos turnos do transcript principal.
func (e *env) turnWindows(t *testing.T) map[string]string {
	t.Helper()
	return e.pairs(t, `SELECT message_id, window_session_id FROM context_turns WHERE session_id=? AND path=?`, sessID, e.transcript)
}

func (e *env) outputWindows(t *testing.T) map[string]string {
	t.Helper()
	return e.pairs(t, `SELECT item_id, window_session_id FROM tool_outputs WHERE session_id=? AND path=?`, sessID, e.transcript)
}

func (e *env) usageWindows(t *testing.T) map[string]string {
	t.Helper()
	return e.pairs(t, `SELECT message_id, window_session_id FROM usage WHERE session_id=?`, sessID)
}

// usageCards: message_id → key do card do uso ("" sem card).
func (e *env) usageCards(t *testing.T) map[string]string {
	t.Helper()
	return e.pairs(t, `SELECT u.message_id, COALESCE(c.key,'') FROM usage u LEFT JOIN cards c ON c.id=u.card_id WHERE u.session_id=?`, sessID)
}

func (e *env) pairs(t *testing.T, q string, args ...any) map[string]string {
	t.Helper()
	rows, err := e.st.DB.Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func wantWindows(t *testing.T, what string, got map[string]string, want map[string]string) {
	t.Helper()
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s %s: não gravado (tem %v)", what, k, got)
			continue
		}
		if g != w {
			t.Errorf("%s %s: janela %q, quer %q", what, k, g, w)
		}
	}
}

func TestWindow_SingleWindowAllMain(t *testing.T) {
	e, c := windowEnv(t)
	e.prompt(t, 100, "investigue o login", time.Minute)
	appendFile(t, e.transcript, wPrompt("u1", "", "investigue o login", time.Minute)+
		wAssistant("a1", "u1", "m1", "t1", time.Minute+2*time.Second)+
		wResult("r1", "a1", "t1", time.Minute+3*time.Second)+
		wAssistant("a2", "r1", "m2", "", time.Minute+4*time.Second))
	e.read(t)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"m1": "", "m2": ""})
	wantWindows(t, "saída", e.outputWindows(t), map[string]string{"t1": ""})
	wantWindows(t, "uso", e.usageWindows(t), map[string]string{"m1": "", "m2": ""})
	wantWindows(t, "card do uso", e.usageCards(t), map[string]string{"m1": c.Key, "m2": c.Key})
	var n int
	if err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM window_lines`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("window_lines = %d %v", n, err)
	}
}

// twoWindows: a principal pergunta do login, a filha (PID 200, card NAC-2)
// revisa o README. A raiz do galho da filha aponta para uma linha da
// principal (como num --resume): só o prompt do hook a separa.
func twoWindows(t *testing.T) (e *env, mainCard, childCard domain.Card) {
	t.Helper()
	e, mainCard = windowEnv(t)
	e.prompt(t, 100, "investigue o login", time.Minute)
	e.prompt(t, 200, "Revise o README, por favor.", time.Minute+5*time.Second)
	childCard = e.childCard(t, 200, "README")
	appendFile(t, e.transcript, wPrompt("mu1", "", "investigue o login", time.Minute)+
		wPrompt("cu1", "mu1", "revise o readme por favor", time.Minute+6*time.Second)+
		wAssistant("ma1", "mu1", "m1", "tm1", time.Minute+7*time.Second)+
		wAssistant("ca1", "cu1", "c1", "tc1", time.Minute+8*time.Second)+
		wResult("mr1", "ma1", "tm1", time.Minute+9*time.Second)+
		wResult("cr1", "ca1", "tc1", time.Minute+10*time.Second)+
		wAssistant("ma2", "mr1", "m2", "", time.Minute+11*time.Second)+
		wAssistant("ca2", "cr1", "c2", "", time.Minute+12*time.Second))
	return e, mainCard, childCard
}

func TestWindow_TwoWindowsSplitByBranch(t *testing.T) {
	e, mc, cc := twoWindows(t)
	e.read(t)
	child := childID(200)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"m1": "", "m2": "", "c1": child, "c2": child})
	wantWindows(t, "saída", e.outputWindows(t), map[string]string{"tm1": "", "tc1": child})
	wantWindows(t, "uso", e.usageWindows(t), map[string]string{"m1": "", "m2": "", "c1": child, "c2": child})
	wantWindows(t, "card do uso", e.usageCards(t), map[string]string{"m1": mc.Key, "m2": mc.Key, "c1": cc.Key, "c2": cc.Key})
	wl := e.pairs(t, `SELECT uuid, window_session_id FROM window_lines WHERE session_id=? AND path=?`, sessID, e.transcript)
	if len(wl) != 4 {
		t.Errorf("window_lines = %v", wl)
	}
	wantWindows(t, "linha", wl, map[string]string{"cu1": child, "ca1": child, "cr1": child, "ca2": child})
	var unused int
	if err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM window_prompts WHERE used=0`).Scan(&unused); err != nil || unused != 0 {
		t.Fatalf("prompts não usados = %d %v", unused, err)
	}
	// Total por sessão de cada card: a janela dona, com o custo dela.
	zero := 0.0
	if err := e.st.PutPrice(domain.Price{Model: "claude-opus-5-5", InputPerM: 1, CacheWritePerM: &zero, CacheReadPerM: &zero}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		card domain.Card
		sess string
	}{{mc, sessID}, {cc, child}} {
		ts, err := e.st.Totals(store.TotalsFilter{GroupBy: "session", CardID: &c.card.ID})
		if err != nil || len(ts) != 1 || ts[0].Key != c.sess || ts[0].Sessions != 1 || ts[0].Input != 2_000_000 ||
			ts[0].CostUSD < 1.999 || ts[0].CostUSD > 2.001 || ts[0].Unpriced {
			t.Errorf("total do card %s = %+v %v", c.card.Key, ts, err)
		}
	}
	// O card da filha lista só a filha (o uso dela tem o id da principal).
	ss, err := e.st.SessionsForCard(cc.ID)
	if err != nil || len(ss) != 1 || ss[0].ID != child {
		t.Fatalf("sessões do card da filha = %+v %v", ss, err)
	}
}

// Releitura do começo (versão nova do leitor): os prompts já foram usados,
// mas a raiz do galho está em window_lines e a atribuição não muda.
func TestWindow_RereadKeepsWindows(t *testing.T) {
	e, _, _ := twoWindows(t)
	e.read(t)
	if err := e.st.ResetContextCursors(); err != nil {
		t.Fatal(err)
	}
	e.read(t)
	child := childID(200)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"m1": "", "m2": "", "c1": child, "c2": child})
	wantWindows(t, "saída", e.outputWindows(t), map[string]string{"tm1": "", "tc1": child})
}

// "ok" nas duas janelas: vence o prompt mais próximo no tempo. A linha da
// filha vem antes no arquivo e o prompt da principal foi registrado antes:
// pegar o primeiro candidato erraria.
func TestWindow_SameTextResolvedByTime(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 100, "ok", 2*time.Minute)
	e.prompt(t, 200, "ok", 2*time.Minute+10*time.Second)
	appendFile(t, e.transcript, wAssistant("a0", "", "m0", "", time.Minute)+
		wPrompt("cu", "a0", "ok", 2*time.Minute+10*time.Second)+
		wPrompt("mu", "a0", "ok", 2*time.Minute)+
		wAssistant("ca", "cu", "c1", "", 2*time.Minute+12*time.Second)+
		wAssistant("ma", "mu", "m1", "", 2*time.Minute+13*time.Second))
	e.read(t)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"m0": "", "m1": "", "c1": childID(200)})
}

// Fora da tolerância (40 s) o prompt não é raiz: a linha herda do pai.
func TestWindow_PromptOutsideToleranceInherits(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 200, "revise o readme", time.Minute)
	appendFile(t, e.transcript, wPrompt("cu", "", "revise o readme", time.Minute+40*time.Second)+
		wAssistant("ca", "cu", "c1", "", time.Minute+41*time.Second))
	e.read(t)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"c1": ""})
}

// O galho continua certo atravessando leituras: o pai numa leitura, o filho
// na seguinte (inclusive o tool_result de um tool_use já gravado).
func TestWindow_BranchAcrossIncrementalReads(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 100, "investigue o login", time.Minute)
	e.prompt(t, 200, "revise o readme", time.Minute+5*time.Second)
	cc := e.childCard(t, 200, "README")
	appendFile(t, e.transcript, wPrompt("mu1", "", "investigue o login", time.Minute)+
		wPrompt("cu1", "mu1", "revise o readme", time.Minute+5*time.Second)+
		wAssistant("ca1", "cu1", "c1", "tc1", time.Minute+8*time.Second))
	e.read(t)
	appendFile(t, e.transcript, wAssistant("ma1", "mu1", "m1", "", time.Minute+9*time.Second)+
		wResult("cr1", "ca1", "tc1", time.Minute+10*time.Second)+
		wAssistant("ca2", "cr1", "c2", "", time.Minute+12*time.Second))
	e.read(t)
	child := childID(200)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"c1": child, "c2": child, "m1": ""})
	wantWindows(t, "saída", e.outputWindows(t), map[string]string{"tc1": child})
	wantWindows(t, "uso", e.usageWindows(t), map[string]string{"c1": child, "c2": child, "m1": ""})
	wantWindows(t, "card do uso", e.usageCards(t), map[string]string{"c2": cc.Key})
}

// O resumo de compactação não tem pai: fica com a principal, mesmo depois de
// um galho da filha.
func TestWindow_CompactSummaryGoesToMain(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 200, "revise o readme", time.Minute)
	appendFile(t, e.transcript, wPrompt("cu1", "", "revise o readme", time.Minute)+
		wAssistant("ca1", "cu1", "c1", "", time.Minute+2*time.Second)+
		`{"type":"system","subtype":"compact_boundary",`+parentField("")+`,"logicalParentUuid":"ca1","uuid":"b1","timestamp":"`+ts(2*time.Minute)+`"}`+"\n"+
		`{"type":"user",`+parentField("b1")+`,"uuid":"s1","isCompactSummary":true,"timestamp":"`+ts(2*time.Minute)+`","message":{"role":"user","content":"resumo"}}`+"\n"+
		wAssistant("k1", "s1", "k1", "", 2*time.Minute+5*time.Second))
	e.read(t)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"c1": childID(200), "k1": ""})
}

// Ligar card depois da leitura: o uso sem card de cada janela vai para o
// card da própria janela (a filha grava o uso com o id da principal).
func TestWindow_AttachAfterIngestFollowsWindow(t *testing.T) {
	e, _ := windowEnv(t)
	if _, err := e.st.DB.Exec(`UPDATE sessions SET card_id=NULL`); err != nil {
		t.Fatal(err)
	}
	e.prompt(t, 100, "investigue o login", time.Minute)
	e.prompt(t, 200, "revise o readme", time.Minute+5*time.Second)
	appendFile(t, e.transcript, wPrompt("mu1", "", "investigue o login", time.Minute)+
		wPrompt("cu1", "", "revise o readme", time.Minute+5*time.Second)+
		wAssistant("ma1", "mu1", "m1", "", time.Minute+7*time.Second)+
		wAssistant("ca1", "cu1", "c1", "", time.Minute+8*time.Second))
	e.read(t)
	wantWindows(t, "card do uso", e.usageCards(t), map[string]string{"m1": "", "c1": ""})
	m, err := e.st.CreateCard(domain.Card{Title: "Login", ProjectID: 1, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.AttachSession(sessID, m.Key, false); err != nil {
		t.Fatal(err)
	}
	wantWindows(t, "card do uso", e.usageCards(t), map[string]string{"m1": m.Key, "c1": ""})
	cc := e.childCard(t, 200, "README")
	wantWindows(t, "card do uso", e.usageCards(t), map[string]string{"m1": m.Key, "c1": cc.Key})
}

// O hook registra o prompt de cada janela (a sessão roteada) só como resumo
// do texto normalizado, nunca o texto, num insert só; a leitura do transcript
// apaga os registros com mais de 24 h.
func TestWindow_HookRegistersPromptDigest(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 100, "Investigue o LOGIN!", time.Minute)
	e.prompt(t, 200, "revise o readme", 25*time.Hour)
	var n int
	if err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM window_prompts`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("hook apagou ou não gravou: %d %v", n, err)
	}
	e.read(t)
	rows, err := e.st.DB.Query(`SELECT session_id, text_hash, at FROM window_prompts ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type wp struct{ sess, norm, at string }
	var got []wp
	for rows.Next() {
		var x wp
		if err := rows.Scan(&x.sess, &x.norm, &x.at); err != nil {
			t.Fatal(err)
		}
		got = append(got, x)
	}
	if len(got) != 1 || got[0].sess != childID(200) || got[0].norm != promptKey("Revise o README.") ||
		strings.Contains(got[0].norm, "readme") || got[0].at != t0.Add(25*time.Hour).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("window_prompts = %+v", got)
	}
	if promptKey("Investigue o LOGIN!") != promptKey("investigue  o login") || promptKey("?!") != "" {
		t.Fatal("promptKey não normaliza")
	}
}

// O prompt de um subagente nunca consome o prompt do hook: só o transcript
// principal casa prompts (aqui o do subagente é lido numa leitura anterior).
func TestWindow_SubagentDoesNotConsumePrompt(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 200, "revise o readme", time.Minute)
	sub := strings.TrimSuffix(e.transcript, ".jsonl") + "/subagents/agent-a1.jsonl"
	if err := os.WriteFile(sub, []byte(wPrompt("su", "", "revise o readme", time.Minute)+
		wAssistant("sa", "su", "s1", "", time.Minute+time.Second)), 0o644); err != nil {
		t.Fatal(err)
	}
	e.read(t)
	appendFile(t, e.transcript, wPrompt("cu", "", "revise o readme", time.Minute+2*time.Second)+
		wAssistant("ca", "cu", "c1", "", time.Minute+3*time.Second))
	e.read(t)
	wantWindows(t, "turno", e.turnWindows(t), map[string]string{"c1": childID(200)})
}

// A principal volta a escrever depois da filha com o pai na folha da filha
// (o dono saiu e um --resume novo continuou do fim do arquivo): o prompt casa
// com a principal e a releitura do começo não pode passar o galho para a filha.
func TestWindow_MainRootUnderChildLeafSurvivesReread(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 200, "revise o readme", time.Minute)
	appendFile(t, e.transcript, wPrompt("cu1", "", "revise o readme", time.Minute)+
		wAssistant("ca1", "cu1", "c1", "", time.Minute+2*time.Second))
	e.read(t)
	e.prompt(t, 100, "investigue o login", 2*time.Minute)
	appendFile(t, e.transcript, wPrompt("mu1", "ca1", "investigue o login", 2*time.Minute)+
		wAssistant("ma1", "mu1", "m1", "", 2*time.Minute+2*time.Second))
	e.read(t)
	want := map[string]string{"c1": childID(200), "m1": ""}
	wantWindows(t, "turno", e.turnWindows(t), want)
	if err := e.st.ResetContextCursors(); err != nil {
		t.Fatal(err)
	}
	e.read(t)
	wantWindows(t, "turno (releitura)", e.turnWindows(t), want)
}

// O modelo de cada sessão é o do último uso da própria janela.
func TestWindow_ModelPerWindow(t *testing.T) {
	e, _ := windowEnv(t)
	e.prompt(t, 100, "investigue o login", time.Minute)
	e.prompt(t, 200, "revise o readme", time.Minute+5*time.Second)
	sonnet := strings.Replace(wAssistant("ca1", "cu1", "c1", "", time.Minute+8*time.Second), "claude-opus-5-5", "claude-sonnet-4-5", 1)
	appendFile(t, e.transcript, wPrompt("mu1", "", "investigue o login", time.Minute)+
		wPrompt("cu1", "", "revise o readme", time.Minute+5*time.Second)+
		wAssistant("ma1", "mu1", "m1", "", time.Minute+7*time.Second)+sonnet)
	e.read(t)
	m, _ := e.st.GetSession(sessID)
	c, _ := e.st.GetSession(childID(200))
	if m.Model != "claude-opus-5-5" || c.Model != "claude-sonnet-4-5" {
		t.Fatalf("modelos: principal %q, filha %q", m.Model, c.Model)
	}
}

// Falha (erro ou pânico) na leitura de contexto não pula o uso: ele é lido
// até o fim e fica com a principal.
func TestWindow_ContextFailureStillReadsUsage(t *testing.T) {
	for _, mode := range []string{"erro", "pânico"} {
		t.Run(mode, func(t *testing.T) {
			e, c := windowEnv(t)
			e.prompt(t, 200, "revise o readme", time.Minute)
			orig := parseContext
			t.Cleanup(func() { parseContext = orig })
			parseContext = func(path string, off int64, st usage.ContextState) ([]usage.ToolOutput, []usage.ContextTurn, []usage.Prompt, []usage.LineRef, int64, usage.ContextState, bool, error) {
				if mode == "pânico" {
					panic("leitor quebrado")
				}
				return nil, nil, nil, nil, off, st, false, errors.New("leitor quebrado")
			}
			appendFile(t, e.transcript, wPrompt("cu1", "", "revise o readme", time.Minute)+
				wAssistant("ca1", "cu1", "c1", "", time.Minute+2*time.Second)+
				wAssistant("ca2", "ca1", "c2", "", time.Minute+3*time.Second))
			e.read(t)
			wantWindows(t, "uso", e.usageWindows(t), map[string]string{"c1": "", "c2": ""})
			wantWindows(t, "card do uso", e.usageCards(t), map[string]string{"c1": c.Key, "c2": c.Key})
			info, _ := os.Stat(e.transcript)
			if off, _ := e.st.FileOffset(sessID, e.transcript); off != info.Size() {
				t.Fatalf("offset do uso = %d, quer %d", off, info.Size())
			}
		})
	}
}

// wCompact é a compactação de uma janela: a linha compact_boundary (sem pai,
// com logicalParentUuid na última linha da janela) e o resumo pendurado nela.
func wCompact(uuid, logicalParent, summary string, at time.Duration) string {
	return `{"type":"system","subtype":"compact_boundary",` + parentField("") + `,"logicalParentUuid":"` + logicalParent +
		`","uuid":"` + uuid + `","timestamp":"` + ts(at) + `"}` + "\n" +
		`{"type":"user",` + parentField(uuid) + `,"uuid":"` + summary + `","isCompactSummary":true,"timestamp":"` + ts(at) +
		`","message":{"role":"user","content":"resumo"}}` + "\n"
}

// warnOf é o aviso de contexto do próximo prompt da sessão id ("" sem aviso).
func (e *env) warnOf(t *testing.T, id string) string {
	t.Helper()
	sess, err := e.st.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	return e.a.contextWarning(sess, payload{})
}

func (e *env) liveOf(t *testing.T, id string) store.ContextLive {
	t.Helper()
	c, err := e.st.SessionContextAt(id, e.now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// windowsWarned abre as duas janelas, com um turno de cada (100% da janela),
// e consome o aviso de contexto das duas.
func windowsWarned(t *testing.T) *env {
	t.Helper()
	e, _ := windowEnv(t)
	e.prompt(t, 100, "investigue o login", time.Minute)
	e.prompt(t, 200, "revise o readme", time.Minute+5*time.Second)
	e.childCard(t, 200, "README")
	appendFile(t, e.transcript, wPrompt("mu1", "", "investigue o login", time.Minute)+
		wAssistant("ma1", "mu1", "m1", "", time.Minute+7*time.Second)+
		wPrompt("cu1", "", "revise o readme", time.Minute+5*time.Second)+
		wAssistant("ca1", "cu1", "c1", "", time.Minute+8*time.Second))
	e.read(t)
	for _, id := range []string{sessID, childID(200)} {
		if e.warnOf(t, id) == "" {
			t.Fatalf("%s sem o primeiro aviso", id)
		}
		if got := e.warnOf(t, id); got != "" {
			t.Fatalf("%s avisou de novo: %q", id, got)
		}
	}
	return e
}

// A compactação é da janela que compactou (logicalParentUuid): a da principal
// não conta para a filha, que segue sem compactação e sem aviso repetido; a
// principal volta a ser avisada.
func TestWindow_MainCompactionIsNotChilds(t *testing.T) {
	e := windowsWarned(t)
	appendFile(t, e.transcript, wCompact("b1", "ma1", "s1", 2*time.Minute)+
		wAssistant("ma2", "s1", "m2", "", 2*time.Minute+5*time.Second)+
		wAssistant("ca2", "ca1", "c2", "", 2*time.Minute+6*time.Second))
	e.read(t)
	child := e.liveOf(t, childID(200))
	if child.Compactions != 0 || child.TurnsSinceCompact != 2 {
		t.Fatalf("filha: compactações %d, turnos desde %d", child.Compactions, child.TurnsSinceCompact)
	}
	if got := e.warnOf(t, childID(200)); got != "" {
		t.Fatalf("filha avisada de novo: %q", got)
	}
	main := e.liveOf(t, sessID)
	if main.Compactions != 1 || main.TurnsSinceCompact != 1 {
		t.Fatalf("principal: compactações %d, turnos desde %d", main.Compactions, main.TurnsSinceCompact)
	}
	if e.warnOf(t, sessID) == "" {
		t.Fatal("principal compactou e não foi avisada de novo")
	}
}

// O espelho: a filha compacta (o pai lógico da compactação está numa leitura
// anterior e a compactação fica no fim da leitura); a principal não conta.
func TestWindow_ChildCompactionIsNotMains(t *testing.T) {
	e := windowsWarned(t)
	appendFile(t, e.transcript, wCompact("b1", "ca1", "s1", 2*time.Minute))
	e.read(t)
	e.prompt(t, 200, "continue", 3*time.Minute)
	appendFile(t, e.transcript, wAssistant("ma2", "ma1", "m2", "", 2*time.Minute+5*time.Second)+
		wPrompt("cu2", "s1", "continue", 3*time.Minute)+
		wAssistant("ca2", "cu2", "c2", "", 3*time.Minute+2*time.Second))
	e.read(t)
	main := e.liveOf(t, sessID)
	if main.Compactions != 0 || main.TurnsSinceCompact != 2 {
		t.Fatalf("principal: compactações %d, turnos desde %d", main.Compactions, main.TurnsSinceCompact)
	}
	if got := e.warnOf(t, sessID); got != "" {
		t.Fatalf("principal avisada de novo: %q", got)
	}
	child := e.liveOf(t, childID(200))
	if child.Compactions != 1 || child.TurnsSinceCompact != 1 {
		t.Fatalf("filha: compactações %d, turnos desde %d", child.Compactions, child.TurnsSinceCompact)
	}
	if e.warnOf(t, childID(200)) == "" {
		t.Fatal("filha compactou e não foi avisada de novo")
	}
}
