package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const sessID = "claude:5f1c2d9e-8b7a-4c3d-9e2f-1a2b3c4d5e6f"

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// fakeGit responde branch/head fixos.
type fakeGit struct{ branch string }

func (g fakeGit) Branch(string) string           { return g.branch }
func (g fakeGit) Head(string) string             { return "abc123" }
func (g fakeGit) DiffStat(string, string) string { return " auth/oauth.go | 4 ++--" }
func (g fakeGit) Clean(string) bool              { return true }
func (g fakeGit) Checkout(string, string) error  { return nil }

type env struct {
	st         *store.Store
	a          *Adapter
	now        time.Time
	transcript string

	mu   sync.Mutex
	pubs []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cockpit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{st: st, now: t0}
	e.a = New(st, adapter.Deps{
		Now: func() time.Time { return e.now },
		Git: fakeGit{branch: "main"},
		Publish: func(kind string) {
			e.mu.Lock()
			e.pubs = append(e.pubs, kind)
			e.mu.Unlock()
		},
	})
	t.Cleanup(e.a.Wait)
	e.transcript = copyTranscript(t)
	return e
}

// copyTranscript copia o transcript de testdata (com o de subagente) para um
// diretório temporário e devolve o caminho do principal.
func copyTranscript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "5f1c2d9e.jsonl")
	sub := filepath.Join(dir, "5f1c2d9e", "subagents", "agent-a1.jsonl")
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		t.Fatal(err)
	}
	for src, dst := range map[string]string{"testdata/transcript/main.jsonl": main, "testdata/transcript/subagents/agent-a1.jsonl": sub} {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return main
}

func (e *env) published(kind string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Contains(e.pubs, kind)
}

// project cadastra /w/api (captura ligada).
func (e *env) project(t *testing.T) domain.Project {
	t.Helper()
	p, err := e.st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type callOpt func(*adapter.HookCall, map[string]any)

func withEnv(k, v string) callOpt {
	return func(h *adapter.HookCall, _ map[string]any) { h.Env = map[string]string{k: v} }
}

func with(k string, v any) callOpt {
	return func(_ *adapter.HookCall, p map[string]any) { p[k] = v }
}

func withPID(pid int) callOpt {
	return func(h *adapter.HookCall, _ map[string]any) { h.AgentPID = pid }
}

// call envia o fixture testdata/<fixture>.json como hook (transcript_path
// trocado pelo transcript temporário, PID 4242).
func (e *env) call(t *testing.T, fixture string, opts ...callOpt) adapter.HookReply {
	t.Helper()
	return e.callCtx(t, context.Background(), fixture, opts...)
}

func (e *env) callCtx(t *testing.T, ctx context.Context, fixture string, opts ...callOpt) adapter.HookReply {
	t.Helper()
	b, err := os.ReadFile("testdata/" + fixture + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	p["transcript_path"] = e.transcript
	h := adapter.HookCall{Event: p["hook_event_name"].(string), AgentPID: 4242}
	for _, o := range opts {
		o(&h, p)
	}
	if h.Payload, err = json.Marshal(p); err != nil {
		t.Fatal(err)
	}
	r, err := e.a.HandleHook(ctx, h)
	if err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	if r.Exit != 0 {
		t.Fatalf("%s: exit = %d", fixture, r.Exit)
	}
	return r
}

func additionalContext(t *testing.T, r adapter.HookReply) string {
	t.Helper()
	var out struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("stdout não é JSON: %q", r.Stdout)
	}
	if out.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Fatalf("stdout = %s", r.Stdout)
	}
	return out.HookSpecificOutput.AdditionalContext
}

func blockReason(t *testing.T, r adapter.HookReply) string {
	t.Helper()
	var out struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil || out.Decision != "block" {
		t.Fatalf("esperava bloqueio, stdout = %q", r.Stdout)
	}
	return out.Reason
}

func (e *env) events(t *testing.T, kind string) []domain.Event {
	t.Helper()
	all, err := e.st.RecentEvents(1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Event
	for _, ev := range all {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func eventPath(t *testing.T, ev domain.Event) string {
	t.Helper()
	var d struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		t.Fatal(err)
	}
	return d.Path
}

func TestSessionStartInjectsPausedCards(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	if _, err := e.st.CreateCard(domain.Card{Title: "Migrar OAuth2", ProjectID: p.ID, Branch: "main",
		Column: domain.Pausado, WhereStopped: "falta o teste do refresh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateCard(domain.Card{Title: "Outro branch", ProjectID: p.ID, Branch: "feat/x", Column: domain.Pausado}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateCard(domain.Card{Title: "No backlog", ProjectID: p.ID, Branch: "main"}); err != nil {
		t.Fatal(err)
	}

	ctx := additionalContext(t, e.call(t, "session_start"))
	if !strings.Contains(ctx, "NAC-1 · Migrar OAuth2 · falta o teste do refresh") {
		t.Fatalf("contexto sem o card pausado:\n%s", ctx)
	}
	if strings.Contains(ctx, "NAC-2") || strings.Contains(ctx, "NAC-3") {
		t.Fatalf("contexto com card de outro branch/coluna:\n%s", ctx)
	}
	for _, want := range []string{"nobre start", "nobre resume", "nobre note", `nobre ask "<pergunta>"`, "várias etapas"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("contexto sem %q:\n%s", want, ctx)
		}
	}
	x, err := e.st.GetSession(sessID)
	if err != nil {
		t.Fatal(err)
	}
	if x.PID != 4242 || x.Branch != "main" || x.GitHead != "abc123" || !x.FullCapture || x.ProjectID == nil || *x.ProjectID != p.ID ||
		x.ProviderSessionID != "5f1c2d9e-8b7a-4c3d-9e2f-1a2b3c4d5e6f" || x.TranscriptPath != e.transcript || x.Cwd != "/w/api" {
		t.Fatalf("sessão = %+v", x)
	}
	if len(e.events(t, domain.EvSessionStart)) != 1 || !e.published("sessions") {
		t.Fatal("session_start não registrado/publicado")
	}
}

func TestSessionStartResumeEnv(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	c, err := e.st.CreateCard(domain.Card{Title: "Migrar OAuth2", ProjectID: p.ID, Branch: "main",
		Column: domain.Pausado, WhereStopped: "falta o teste do refresh"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start", withEnv("NOBRE_RESUME", "NAC-1")))
	x, _ := e.st.GetSession(sessID)
	if x.CardID == nil || *x.CardID != c.ID {
		t.Fatalf("sessão não ligada: %+v", x)
	}
	if got, _ := e.st.GetCard("NAC-1"); got.Column != domain.EmAndamento {
		t.Fatalf("col = %s", got.Column)
	}
	if !strings.Contains(ctx, "## Onde parei") || !strings.Contains(ctx, "falta o teste do refresh") {
		t.Fatalf("contexto sem o pacote:\n%s", ctx)
	}
	if !e.published("cards") {
		t.Fatal("cards não publicado")
	}
}

func TestResumeEnvUnknownCardStillReplies(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	ctx := additionalContext(t, e.call(t, "session_start", withEnv("NOBRE_RESUME", "NAC-99")))
	if !strings.Contains(ctx, "NAC-99 não foi encontrado") {
		t.Fatalf("contexto:\n%s", ctx)
	}
}

func TestPostToolUseEditRecordsFile(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start")
	for _, f := range []string{"post_tool_use_edit", "post_tool_use_write", "post_tool_use_multiedit", "post_tool_use_notebook", "post_tool_use_read", "post_tool_use_bash"} {
		e.call(t, f)
	}
	var edits []string
	for _, ev := range e.events(t, domain.EvFileEdit) {
		edits = append(edits, eventPath(t, ev))
	}
	slices.Sort(edits)
	// Dentro do projeto: caminho relativo à raiz (casa com os arquivos essenciais).
	want := []string{"auth/oauth.go", "auth/refresh.go", "auth/token.go", "notas/analise.ipynb"}
	if !slices.Equal(edits, want) {
		t.Fatalf("file_edit = %v", edits)
	}
	reads := e.events(t, domain.EvFileRead)
	if len(reads) != 1 || eventPath(t, reads[0]) != "auth/oauth_test.go" {
		t.Fatalf("file_read = %+v", reads)
	}
	cmds := e.events(t, domain.EvCommand)
	if len(cmds) != 1 || string(cmds[0].Data) != `{"command":"go test ./auth/..."}` {
		t.Fatalf("command = %+v", cmds)
	}
	if e.call(t, "post_tool_use_edit").Stdout != "" {
		t.Fatal("PostToolUse não deve responder nada")
	}
}

func TestFilePathsOutsideProjectStayAbsolute(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	// cwd num subdiretório: a raiz continua sendo a do projeto.
	sub := with("cwd", "/w/api/auth")
	e.call(t, "post_tool_use_edit", sub)
	e.call(t, "post_tool_use_edit", sub, with("tool_input", map[string]any{"file_path": "/etc/hosts"}))
	e.call(t, "post_tool_use_edit", sub, with("tool_input", map[string]any{"file_path": "/w/api-outro/x.go"}))
	e.call(t, "post_tool_use_read", sub, with("tool_input", map[string]any{"file_path": "/home/u/notas.md"}))
	var edits []string
	for _, ev := range e.events(t, domain.EvFileEdit) {
		edits = append(edits, eventPath(t, ev))
	}
	slices.Sort(edits)
	if want := []string{"/etc/hosts", "/w/api-outro/x.go", "auth/oauth.go"}; !slices.Equal(edits, want) {
		t.Fatalf("file_edit = %v", edits)
	}
	if reads := e.events(t, domain.EvFileRead); len(reads) != 1 || eventPath(t, reads[0]) != "/home/u/notas.md" {
		t.Fatalf("file_read = %+v", reads)
	}
}

func TestPostToolUseBashExitCode(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "post_tool_use_bash", with("tool_response", map[string]any{"stdout": "", "exit_code": 2}))
	cmds := e.events(t, domain.EvCommand)
	if len(cmds) != 1 || string(cmds[0].Data) != `{"command":"go test ./auth/...","exit_code":2}` {
		t.Fatalf("command = %+v", cmds)
	}
}

func TestUserPromptSubmitRecordsPrompt(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "user_prompt_submit")
	evs := e.events(t, domain.EvPrompt)
	if len(evs) != 1 || string(evs[0].Data) != `{"text":"corrija o refresh do token OAuth"}` {
		t.Fatalf("prompt = %+v", evs)
	}
	if !e.published("sessions") {
		t.Fatal("sessions não publicado")
	}
	e.now = t0.Add(time.Minute)
	e.call(t, "user_prompt_submit")
	if x, _ := e.st.GetSession(sessID); !x.LastEventAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("last_event_at = %v", x.LastEventAt)
	}
}

func TestUnregisteredProjectRecordsOnlyUsage(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	other := with("cwd", "/home/u/outro")
	if r := e.call(t, "session_start", other); r.Stdout != "" {
		t.Fatalf("sem projeto não injeta contexto: %q", r.Stdout)
	}
	e.call(t, "user_prompt_submit", other)
	for _, f := range []string{"post_tool_use_edit", "post_tool_use_write", "post_tool_use_multiedit"} {
		e.call(t, f, other)
	}
	if r := e.call(t, "stop", other); r.Stdout != "" {
		t.Fatalf("sem captura nunca bloqueia: %q", r.Stdout)
	}
	e.a.Wait()
	x, err := e.st.GetSession(sessID)
	if err != nil {
		t.Fatal(err)
	}
	if x.FullCapture || x.ProjectID != nil {
		t.Fatalf("sessão = %+v", x)
	}
	for _, k := range []string{domain.EvFileEdit, domain.EvPrompt} {
		if evs := e.events(t, k); len(evs) != 0 {
			t.Fatalf("%s registrado sem captura: %+v", k, evs)
		}
	}
	ts, _ := e.st.Totals(store.TotalsFilter{GroupBy: "session"})
	if len(ts) != 1 || ts[0].Output == 0 {
		t.Fatalf("uso = %+v", ts)
	}
}

func TestProjectWithCaptureOffRecordsOnlyUsage(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	p.Capture = false
	if err := e.st.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	if r := e.call(t, "session_start"); r.Stdout != "" {
		t.Fatalf("captura desligada não injeta contexto: %q", r.Stdout)
	}
	e.call(t, "post_tool_use_edit")
	if evs := e.events(t, domain.EvFileEdit); len(evs) != 0 {
		t.Fatalf("file_edit sem captura: %+v", evs)
	}
}

// cardWithSession cria o card NAC-1 e liga a sessão a ele pelo NOBRE_RESUME.
func (e *env) cardWithSession(t *testing.T, c domain.Card) domain.Card {
	t.Helper()
	p := e.project(t)
	c.ProjectID, c.Branch = p.ID, "main"
	c, err := e.st.CreateCard(c)
	if err != nil {
		t.Fatal(err)
	}
	e.call(t, "session_start", withEnv("NOBRE_RESUME", c.Key))
	return c
}

func TestStopBlocksWhenEditsAfterWhereStopped(t *testing.T) {
	e := newEnv(t)
	at := t0
	c := e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2", WhereStopped: "lendo o oauth.go", WhereStoppedAt: &at})
	e.now = t0.Add(time.Minute)
	e.call(t, "post_tool_use_edit")
	e.now = t0.Add(2 * time.Minute)
	reason := blockReason(t, e.call(t, "stop"))
	if !strings.Contains(reason, "Antes de encerrar, atualize o card "+c.Key) || !strings.Contains(reason, "Itens pendentes da regra:") {
		t.Fatalf("reason = %q", reason)
	}
	if strings.Contains(reason, "{{") {
		t.Fatalf("prompt não renderizado: %q", reason)
	}
}

func TestStopBlocksWhenWhereStoppedNeverSet(t *testing.T) {
	e := newEnv(t)
	e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	if r := e.call(t, "stop"); r.Stdout != "" {
		t.Fatalf("sem edits não bloqueia: %q", r.Stdout)
	}
	e.call(t, "post_tool_use_edit")
	blockReason(t, e.call(t, "stop"))
}

func TestStopDoesNotBlockWhenOnlyHowToTestMissing(t *testing.T) {
	e := newEnv(t)
	c := e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2", Goal: "g", DoneWhen: "d"})
	if err := e.st.UpsertEssentialFile(domain.EssentialFile{CardID: c.ID, Path: "auth/oauth.go", Why: "fluxo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddTodo(domain.Todo{CardID: c.ID, Text: "teste do refresh"}); err != nil {
		t.Fatal(err)
	}
	e.now = t0.Add(time.Minute)
	e.call(t, "post_tool_use_edit")
	// "Onde parei" registrado depois do último edit: só falta "como testar".
	if err := e.st.SetWhereStopped(c.ID, "falta o teste do refresh", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	e.now = t0.Add(3 * time.Minute)
	if r := e.call(t, "stop"); r.Stdout != "" {
		t.Fatalf("não deveria bloquear: %q", r.Stdout)
	}
}

func TestStopDoesNotBlockWhenIncompleteActionIsMark(t *testing.T) {
	e := newEnv(t)
	set := domain.DefaultSettings()
	set.IncompleteAction = "mark"
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.call(t, "post_tool_use_edit")
	if r := e.call(t, "stop"); r.Stdout != "" {
		t.Fatalf("não deveria bloquear: %q", r.Stdout)
	}
}

func TestStopNeverBlocksWhenStopHookActive(t *testing.T) {
	e := newEnv(t)
	e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.call(t, "post_tool_use_edit")
	if r := e.call(t, "stop", with("stop_hook_active", true)); r.Stdout != "" {
		t.Fatalf("stop_hook_active nunca bloqueia: %q", r.Stdout)
	}
	e.a.Wait()
	if ts, _ := e.st.Totals(store.TotalsFilter{GroupBy: "session"}); len(ts) != 1 {
		t.Fatalf("uso não lido com stop_hook_active: %+v", ts)
	}
}

func TestStopReminderOnlyOnce(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "post_tool_use_edit")
	e.call(t, "post_tool_use_edit") // mesmo arquivo de novo: não conta duas vezes
	e.call(t, "post_tool_use_write")
	if r := e.call(t, "stop"); r.Stdout != "" {
		t.Fatalf("2 arquivos (< 3) não lembra: %q", r.Stdout)
	}
	e.call(t, "post_tool_use_multiedit")
	want := `NobreAI Cockpit: esta sessão já editou 3 arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`
	if got := blockReason(t, e.call(t, "stop")); got != want {
		t.Fatalf("reason = %q", got)
	}
	if r := e.call(t, "stop"); r.Stdout != "" {
		t.Fatalf("lembrete repetido: %q", r.Stdout)
	}
}

func TestSessionEndSurvivesCancelledRequest(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.now = t0.Add(time.Hour)
	e.callCtx(t, ctx, "session_end")
	e.a.Wait()
	x, _ := e.st.GetSession(sessID)
	if x.EndedAt == nil || !x.EndedAt.Equal(t0.Add(time.Hour)) || x.EndReason == "" {
		t.Fatalf("sessão = %+v", x)
	}
	if ts, _ := e.st.Totals(store.TotalsFilter{GroupBy: "session"}); len(ts) != 1 || ts[0].Output == 0 {
		t.Fatalf("uso = %+v", ts)
	}
	if len(e.events(t, domain.EvSessionEnd)) != 1 {
		t.Fatal("session_end não registrado")
	}
}

func TestSessionEndPausesCard(t *testing.T) {
	e := newEnv(t)
	e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.call(t, "session_end")
	if got, _ := e.st.GetCard("NAC-1"); got.Column != domain.Pausado {
		t.Fatalf("col = %s", got.Column)
	}
	if !e.published("cards") {
		t.Fatal("cards não publicado")
	}
}

func TestResumedEndedSessionReopens(t *testing.T) {
	e := newEnv(t)
	e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.call(t, "session_end")
	e.now = t0.Add(time.Hour)
	e.call(t, "session_start_resume", withPID(5555))
	x, _ := e.st.GetSession(sessID)
	if x.EndedAt != nil || x.EndReason != "" || x.PIDDeadAt != nil || x.PID != 5555 || !x.Live() {
		t.Fatalf("sessão não reaberta: %+v", x)
	}
	if got, _ := e.st.GetCard("NAC-1"); got.Column != domain.EmAndamento {
		t.Fatalf("col = %s", got.Column)
	}
	// SessionEnd repetido de uma sessão encerrada não a reabre.
	e.call(t, "session_end", withPID(5555))
	e.call(t, "session_end", withPID(5555))
	if x, _ := e.st.GetSession(sessID); x.EndedAt == nil {
		t.Fatalf("SessionEnd reabriu a sessão: %+v", x)
	}
}

func TestAnyEventReopensDeadSession(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start")
	if err := e.st.MarkPIDDead(sessID, t0); err != nil {
		t.Fatal(err)
	}
	e.call(t, "user_prompt_submit", withPID(4243))
	x, _ := e.st.GetSession(sessID)
	if !x.Live() || x.PID != 4243 {
		t.Fatalf("sessão = %+v", x)
	}
}

// overLimit grava preço, limite (pause) e uso de hoje acima do limite diário.
func (e *env) overLimit(t *testing.T, action string) {
	t.Helper()
	if err := e.st.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.PutLimit(domain.Limit{Agent: "claude", DailyUSD: 1, WarnPct: 80, Action: action}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.UpsertUsage([]domain.UsageRow{{SessionID: sessID, MessageID: "x", Model: "m", Input: 2_000_000, At: e.now}}); err != nil {
		t.Fatal(err)
	}
}

func TestPreToolUseDeniesOverLimitButAllowsNobre(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start")
	if r := e.call(t, "pre_tool_use_bash"); r.Stdout != "" {
		t.Fatalf("sem limite não nega: %q", r.Stdout)
	}
	e.overLimit(t, "pause")
	for _, f := range []string{"pre_tool_use_bash", "pre_tool_use_edit"} {
		r := e.call(t, f)
		var out struct {
			HookSpecificOutput struct {
				HookEventName            string `json:"hookEventName"`
				PermissionDecision       string `json:"permissionDecision"`
				PermissionDecisionReason string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
			t.Fatalf("%s: stdout = %q", f, r.Stdout)
		}
		h := out.HookSpecificOutput
		want := "NobreAI Cockpit: limite de custo atingido (diário: US$ 2,00 de US$ 1,00). Registre “Onde parei” com nobre note e encerre."
		if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || h.PermissionDecisionReason != want {
			t.Fatalf("%s: %+v", f, h)
		}
	}
	if r := e.call(t, "pre_tool_use_bash_nobre"); r.Stdout != "" {
		t.Fatalf("nobre precisa passar: %q", r.Stdout)
	}
}

func TestPreToolUseWarnOnlyDoesNotDeny(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start")
	e.overLimit(t, "warn")
	if r := e.call(t, "pre_tool_use_bash"); r.Stdout != "" {
		t.Fatalf("ação warn não nega: %q", r.Stdout)
	}
}

func TestPreToolUseDeniesOverCardLimit(t *testing.T) {
	e := newEnv(t)
	c := e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	if err := e.st.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.PutLimit(domain.Limit{Agent: "claude", CardUSD: 1, Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.UpsertUsage([]domain.UsageRow{{SessionID: sessID, MessageID: "x", Model: "m", Input: 3_000_000, At: t0.Add(-48 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, "pre_tool_use_bash")
	if !strings.Contains(r.Stdout, `"permissionDecision":"deny"`) || !strings.Contains(r.Stdout, "card "+c.Key+": US$ 3,00 de US$ 1,00") {
		t.Fatalf("stdout = %s", r.Stdout)
	}
}

func TestStopReadsUsage(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "session_start")
	e.call(t, "stop")
	e.a.Wait()
	ts, err := e.st.Totals(store.TotalsFilter{GroupBy: "session"})
	if err != nil {
		t.Fatal(err)
	}
	// principal: msg_01 + msg_02; subagente: msg_sub_01.
	if len(ts) != 1 || ts[0].Key != sessID || ts[0].Input != 12+8+50 || ts[0].Output != 300+120+40 || ts[0].CacheRead != 32000 {
		t.Fatalf("totais = %+v", ts)
	}
	if !e.published("usage") {
		t.Fatal("usage não publicado")
	}
	x, _ := e.st.GetSession(sessID)
	if x.Model != "claude-opus-5-5" {
		t.Fatalf("model = %q", x.Model)
	}
	if off, _ := e.st.FileOffset(sessID, e.transcript); off == 0 {
		t.Fatal("offset do principal não gravado")
	}

	// Nova mensagem no subagente: só ela é lida (offset por arquivo).
	sub := filepath.Join(strings.TrimSuffix(e.transcript, ".jsonl"), "subagents", "agent-a1.jsonl")
	f, err := os.OpenFile(sub, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"assistant","sessionId":"x","timestamp":"2026-11-07T12:01:00Z","message":{"id":"msg_sub_02","model":"claude-haiku-4-5","usage":{"input_tokens":5,"output_tokens":5}}}` + "\n")
	f.Close()
	e.call(t, "stop")
	e.a.Wait()
	ts, _ = e.st.Totals(store.TotalsFilter{GroupBy: "session"})
	if len(ts) != 1 || ts[0].Input != 12+8+50+5 {
		t.Fatalf("totais = %+v", ts)
	}
	if off, _ := e.st.FileOffset(sessID, sub); off == 0 {
		t.Fatal("offset do subagente não gravado")
	}
}

func TestTranscriptOffsetIsPerPath(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "stop")
	e.a.Wait()
	// Outro transcript (maior que o primeiro) para a mesma sessão: é lido do
	// começo, não do offset do anterior.
	other := filepath.Join(t.TempDir(), "outro.jsonl")
	var lines []string
	for _, id := range []string{"msg_b1", "msg_b2", "msg_b3", "msg_b4", "msg_b5", "msg_b6", "msg_b7", "msg_b8"} {
		lines = append(lines, `{"type":"assistant","sessionId":"x","timestamp":"2026-11-07T12:05:00Z","message":{"id":"`+id+`","model":"claude-opus-5-5","usage":{"input_tokens":1000,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},"padding":"`+strings.Repeat("p", 200)+`"}`)
	}
	if err := os.WriteFile(other, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.call(t, "stop", with("transcript_path", other))
	e.a.Wait()
	ts, _ := e.st.Totals(store.TotalsFilter{GroupBy: "session"})
	if len(ts) != 1 || ts[0].Input != 12+8+50+8*1000 {
		t.Fatalf("totais = %+v", ts)
	}
	first, _ := e.st.FileOffset(sessID, e.transcript)
	second, _ := e.st.FileOffset(sessID, other)
	if first == 0 || second == 0 || first == second {
		t.Fatalf("offsets = %d, %d", first, second)
	}
}

func TestIngestPanicIsRecovered(t *testing.T) {
	a := New(nil, adapter.Deps{}) // store nil: a leitura entra em pânico
	a.ingestAsync("claude:x", "/qualquer.jsonl")
	a.Wait() // o daemon segue de pé
}

func TestMissingTranscriptIsIgnored(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	e.call(t, "stop", with("transcript_path", filepath.Join(t.TempDir(), "nao-existe.jsonl")))
	e.a.Wait()
	if ts, _ := e.st.Totals(store.TotalsFilter{GroupBy: "session"}); len(ts) != 0 {
		t.Fatalf("totais = %+v", ts)
	}
}

func TestAdapterIdentity(t *testing.T) {
	a := New(nil, adapter.Deps{})
	var _ adapter.Adapter = a
	if a.Name() != "claude" || !slices.Equal(a.ProcessNames(), []string{"claude"}) ||
		!slices.Equal(a.LaunchCommand("/x/claude", "oi"), []string{"/x/claude", "--", "oi"}) {
		t.Fatal("identidade do adaptador")
	}
}

func TestSessionStartCompactReinjectsResumePackage(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	if _, err := e.st.CreateCard(domain.Card{Title: "Migrar OAuth2", ProjectID: p.ID, Branch: "main",
		WhereStopped: "falta o teste do refresh"}); err != nil {
		t.Fatal(err)
	}
	e.call(t, "session_start")
	if err := e.st.AttachSession(sessID, "NAC-1", true); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start", with("source", "compact")))
	for _, want := range []string{"NAC-1", "## Onde parei", "falta o teste do refresh"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("compact sem %q:\n%s", want, ctx)
		}
	}
	// Num resume comum segue só a linha do card.
	ctx = additionalContext(t, e.call(t, "session_start", with("source", "resume")))
	if !strings.Contains(ctx, "Esta sessão está no card NAC-1") || strings.Contains(ctx, "## Onde parei") {
		t.Fatalf("resume:\n%s", ctx)
	}
}

// wtGit é o fakeGit que também conhece worktrees: locs mapeia o topo de cada
// worktree para o repositório principal.
type wtGit struct {
	fakeGit
	locs map[string]string
}

// Locate escolhe a raiz mais profunda que contém dir, como o git faz com
// worktrees aninhados (.claude/worktrees dentro do checkout principal).
func (g wtGit) Locate(dir string) gitinfo.Location {
	best := ""
	for root := range g.locs {
		if (dir == root || strings.HasPrefix(dir, root+"/")) && len(root) > len(best) {
			best = root
		}
	}
	if best == "" {
		return gitinfo.Location{}
	}
	return gitinfo.Location{Root: best, Main: g.locs[best], Linked: best != g.locs[best]}
}

func (g wtGit) Worktrees(string) ([]gitinfo.Worktree, error) { return nil, nil }

// wtGit também é BranchReader: a base detectada é sempre main.
func (g wtGit) DetectBase(string) string                { return "main" }
func (g wtGit) BranchTip(string, string) (string, bool) { return "", false }
func (g wtGit) IsAncestor(string, string, string) bool  { return false }

func newWorktreeEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.a.deps.Git = wtGit{fakeGit: fakeGit{branch: "feat/x"}, locs: map[string]string{
		"/w/api":                     "/w/api",
		"/w/api/.claude/worktrees/x": "/w/api",
		"/w/api-feat":                "/w/api",
	}}
	return e
}

func TestSessionInExternalWorktreeJoinsProject(t *testing.T) {
	e := newWorktreeEnv(t)
	p := e.project(t)
	wt := with("cwd", "/w/api-feat/auth")
	e.call(t, "session_start", wt)
	s, err := e.st.GetSession(sessID)
	if err != nil {
		t.Fatal(err)
	}
	if s.ProjectID == nil || *s.ProjectID != p.ID || !s.FullCapture {
		t.Fatalf("sessão em worktree fora do projeto não foi ligada: %+v", s)
	}
	if s.Worktree != "/w/api-feat" || !s.WorktreeLinked || s.Branch != "feat/x" {
		t.Fatalf("worktree = %q linked=%v branch=%q", s.Worktree, s.WorktreeLinked, s.Branch)
	}
	// Caminhos do worktree ficam relativos à raiz do projeto dentro dele.
	e.call(t, "post_tool_use_edit", wt, with("tool_input", map[string]any{"file_path": "/w/api-feat/auth/oauth.go"}))
	e.call(t, "post_tool_use_edit", wt, with("tool_input", map[string]any{"file_path": "/w/api/auth/main.go"}))
	var edits []string
	for _, ev := range e.events(t, domain.EvFileEdit) {
		edits = append(edits, eventPath(t, ev))
	}
	slices.Sort(edits)
	if want := []string{"/w/api/auth/main.go", "auth/oauth.go"}; !slices.Equal(edits, want) {
		t.Fatalf("file_edit = %v", edits)
	}
}

func TestSessionInMainCheckoutIsNotLinked(t *testing.T) {
	e := newWorktreeEnv(t)
	e.project(t)
	e.call(t, "session_start")
	s, _ := e.st.GetSession(sessID)
	if s.Worktree != "/w/api" || s.WorktreeLinked || s.ProjectID == nil {
		t.Fatalf("sessão no checkout principal = %+v", s)
	}
}

func TestSessionEnteringWorktreeUpdatesLocation(t *testing.T) {
	e := newWorktreeEnv(t)
	p := e.project(t)
	e.call(t, "session_start")
	e.mu.Lock()
	e.pubs = nil
	e.mu.Unlock()
	// O agente entrou num worktree (EnterWorktree): o cwd dos hooks muda e a
	// sessão acompanha no próximo evento só do agente principal.
	wt := with("cwd", "/w/api/.claude/worktrees/x")
	e.call(t, "user_prompt_submit", wt)
	e.call(t, "post_tool_use_edit", wt, with("tool_input", map[string]any{"file_path": "/w/api/.claude/worktrees/x/auth/oauth.go"}))
	s, _ := e.st.GetSession(sessID)
	if s.Cwd != "/w/api/.claude/worktrees/x" || s.Worktree != "/w/api/.claude/worktrees/x" || !s.WorktreeLinked {
		t.Fatalf("local não atualizado: %+v", s)
	}
	if s.ProjectID == nil || *s.ProjectID != p.ID {
		t.Fatal("projeto deveria continuar o mesmo")
	}
	if !e.published("sessions") {
		t.Fatal("troca de worktree deveria avisar o stream")
	}
	if got := eventPath(t, e.events(t, domain.EvFileEdit)[0]); got != "auth/oauth.go" {
		t.Fatalf("file_edit no worktree = %q", got)
	}
}

func TestSubagentWorktreeDoesNotMoveSession(t *testing.T) {
	e := newWorktreeEnv(t)
	e.project(t)
	e.call(t, "session_start")
	// Subagente com isolation: worktree manda hooks com o session_id do pai e
	// o cwd do worktree dele, intercalados com os do agente principal.
	sub := with("cwd", "/w/api/.claude/worktrees/x")
	for range 3 {
		e.call(t, "post_tool_use_edit", sub, with("agent_id", "a1"), with("agent_type", "general-purpose"))
		e.call(t, "pre_tool_use_edit", sub)
		if s, _ := e.st.GetSession(sessID); s.Cwd != "/w/api" || s.Worktree != "/w/api" || s.WorktreeLinked {
			t.Fatalf("sessão principal mudou de local por causa do subagente: %+v", s)
		}
		e.call(t, "post_tool_use_edit")
	}
	// Mesmo um evento só do principal vindo de subagente (agent_id) não move.
	e.call(t, "stop", sub, with("agent_id", "a1"))
	if s, _ := e.st.GetSession(sessID); s.WorktreeLinked {
		t.Fatalf("evento com agent_id moveu a sessão: %+v", s)
	}
}

func TestMoveSessionSyncsCardBranch(t *testing.T) {
	e := newWorktreeEnv(t)
	p := e.project(t)
	c, err := e.st.CreateCard(domain.Card{Title: "x", ProjectID: p.ID, Branch: "main", Column: domain.EmAndamento})
	if err != nil {
		t.Fatal(err)
	}
	e.call(t, "session_start")
	if err := e.st.AttachSession(sessID, c.Key, true); err != nil {
		t.Fatal(err)
	}
	// Ainda no checkout principal, em main (a base): nada muda.
	e.a.deps.Git = wtGit{fakeGit: fakeGit{branch: "main"}, locs: e.a.deps.Git.(wtGit).locs}
	e.call(t, "user_prompt_submit")
	if got, _ := e.st.GetCardByID(c.ID); got.Branch != "main" {
		t.Fatalf("branch = %q", got.Branch)
	}
	// O agente entra num worktree em feat/x: o card acompanha.
	e.a.deps.Git = wtGit{fakeGit: fakeGit{branch: "feat/x"}, locs: e.a.deps.Git.(wtGit).locs}
	e.call(t, "user_prompt_submit", with("cwd", "/w/api/.claude/worktrees/x"))
	if got, _ := e.st.GetCardByID(c.ID); got.Branch != "feat/x" {
		t.Fatalf("branch = %q", got.Branch)
	}
	if !e.published("cards") {
		t.Fatal("cards deveria ser publicado")
	}
}

func TestSessionStartResumeEnvSyncsBranch(t *testing.T) {
	e := newWorktreeEnv(t)
	p := e.project(t)
	if _, err := e.st.CreateCard(domain.Card{Title: "x", ProjectID: p.ID, Branch: "main", Column: domain.Pausado}); err != nil {
		t.Fatal(err)
	}
	e.call(t, "session_start", withEnv("NOBRE_RESUME", "NAC-1"))
	if got, _ := e.st.GetCard("NAC-1"); got.Branch != "feat/x" {
		t.Fatalf("branch = %q", got.Branch)
	}
}

// git switch no mesmo diretório: o PostToolUse do Bash percebe o branch novo,
// grava na sessão e o card acompanha.
func TestPostToolUseBashFollowsBranchSwitch(t *testing.T) {
	e := newEnv(t)
	c := e.cardWithSession(t, domain.Card{Title: "x", Column: domain.Pausado})
	if err := e.st.SetProjectBase(c.ProjectID, "main"); err != nil {
		t.Fatal(err)
	}
	e.a.deps.Git = fakeGit{branch: "feat/new"}
	e.call(t, "post_tool_use_bash")
	if s, _ := e.st.GetSession(sessID); s.Branch != "feat/new" || s.Cwd != "/w/api" {
		t.Fatalf("sessão = %+v", s)
	}
	if got, _ := e.st.GetCardByID(c.ID); got.Branch != "feat/new" {
		t.Fatalf("branch do card = %q", got.Branch)
	}
	if len(e.events(t, domain.EvCommand)) != 1 {
		t.Fatal("o comando deveria continuar registrado")
	}
}

// pausedFixture cadastra /w/api com base main e cards pausados em feat/a,
// feat/x e sem branch.
func (e *env) pausedFixture(t *testing.T) {
	t.Helper()
	p := e.project(t)
	if err := e.st.SetProjectBase(p.ID, "main"); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"feat/a", "feat/x", ""} {
		if _, err := e.st.CreateCard(domain.Card{Title: "T" + b, ProjectID: p.ID, Branch: b, Column: domain.Pausado}); err != nil {
			t.Fatal(err)
		}
	}
}

// Na base (o card nunca guarda a base), a sessão vê todos os pausados do projeto.
func TestSessionStartOnBaseListsAllPausedCards(t *testing.T) {
	e := newEnv(t)
	e.pausedFixture(t)
	ctx := additionalContext(t, e.call(t, "session_start"))
	for _, k := range []string{"NAC-1", "NAC-2", "NAC-3"} {
		if !strings.Contains(ctx, k+" · ") {
			t.Fatalf("contexto sem %s:\n%s", k, ctx)
		}
	}
}

// Num branch de trabalho, só os pausados desse branch e os sem branch.
func TestSessionStartOnWorkBranchListsBranchAndUnbranchedPaused(t *testing.T) {
	e := newEnv(t)
	e.a.deps.Git = fakeGit{branch: "feat/x"}
	e.pausedFixture(t)
	ctx := additionalContext(t, e.call(t, "session_start"))
	if !strings.Contains(ctx, "NAC-2 · ") || !strings.Contains(ctx, "NAC-3 · ") || strings.Contains(ctx, "NAC-1 · ") {
		t.Fatalf("contexto:\n%s", ctx)
	}
}

func TestSessionStartOmitsPausedCardsWhenDisabled(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	set := domain.DefaultSettings()
	set.InjectPausedCards = false
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateCard(domain.Card{Title: "Migrar OAuth2", ProjectID: p.ID, Branch: "main",
		Column: domain.Pausado, WhereStopped: "falta o teste do refresh"}); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start"))
	if strings.Contains(ctx, "Cards pausados neste repositório") || strings.Contains(ctx, "NAC-1") {
		t.Fatalf("contexto com cards pausados:\n%s", ctx)
	}
}

func TestSessionStartWithoutAgentCreatesCards(t *testing.T) {
	e := newEnv(t)
	e.project(t)
	set := domain.DefaultSettings()
	set.AgentCreatesCards = false
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start"))
	if !strings.HasPrefix(ctx, registeredPhrase) || !strings.Contains(ctx, `Só crie card quando o usuário pedir ("crie um card para isso"); se ele citar uma chave NAC-, use nobre resume <KEY>.`) ||
		strings.Contains(ctx, "quando começar algo que precisa de histórico") || !strings.Contains(ctx, "nobre note") {
		t.Fatalf("contexto:\n%s", ctx)
	}
}

func TestStopIgnoresWhereStoppedWhenRuleDisabled(t *testing.T) {
	e := newEnv(t)
	set := domain.DefaultSettings()
	set.Rules["where_stopped"] = false
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.call(t, "post_tool_use_edit")
	if r := e.call(t, "stop"); r.Stdout != "" {
		t.Fatalf("não deveria bloquear: %q", r.Stdout)
	}
}

func TestPreToolUsePauseMovesCardToPaused(t *testing.T) {
	e := newEnv(t)
	c := e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.overLimit(t, "pause")
	r := e.call(t, "pre_tool_use_bash")
	if !strings.Contains(r.Stdout, `"permissionDecision":"deny"`) {
		t.Fatalf("stdout = %s", r.Stdout)
	}
	got, _ := e.st.GetCard(c.Key)
	if got.Column != domain.Pausado || got.PauseReason != "limite de custo" {
		t.Fatalf("card = %+v", got)
	}
	evs, err := e.st.EventsByKind(domain.EvLimitPause, 10)
	if err != nil || len(evs) != 1 {
		t.Fatalf("eventos = %+v, err = %v", evs, err)
	}
	if !e.published("cards") {
		t.Fatal("cards não publicado")
	}
	e.call(t, "pre_tool_use_bash")
	if evs, _ := e.st.EventsByKind(domain.EvLimitPause, 10); len(evs) != 1 {
		t.Fatalf("evento duplicado: %d", len(evs))
	}
}

func TestPreToolUseWarnKeepsCardInProgress(t *testing.T) {
	e := newEnv(t)
	c := e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.overLimit(t, "warn")
	e.call(t, "pre_tool_use_bash")
	if got, _ := e.st.GetCard(c.Key); got.Column != domain.EmAndamento {
		t.Fatalf("column = %s", got.Column)
	}
}

func TestPreToolUseResumesCardWhenLimitRaised(t *testing.T) {
	e := newEnv(t)
	c := e.cardWithSession(t, domain.Card{Title: "Migrar OAuth2"})
	e.overLimit(t, "pause")
	e.call(t, "pre_tool_use_bash")
	if got, _ := e.st.GetCard(c.Key); got.Column != domain.Pausado {
		t.Fatalf("column = %s", got.Column)
	}
	if err := e.st.PutLimit(domain.Limit{Agent: "claude", DailyUSD: 100, WarnPct: 80, Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	if r := e.call(t, "pre_tool_use_bash"); r.Stdout != "" {
		t.Fatalf("não deveria negar: %s", r.Stdout)
	}
	got, _ := e.st.GetCard(c.Key)
	if got.Column != domain.EmAndamento || got.PauseReason != "" {
		t.Fatalf("card = %+v", got)
	}
}

func TestSessionStartPausedListShowsReason(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	blocker, err := e.st.CreateCard(domain.Card{Title: "Bloqueador", ProjectID: p.ID, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(title string, mod func(*domain.Card)) domain.Card {
		c, err := e.st.CreateCard(domain.Card{Title: title, ProjectID: p.ID, Branch: "main", Column: domain.Pausado})
		if err != nil {
			t.Fatal(err)
		}
		mod(&c)
		if err := e.st.UpdateCard(c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	mk("Bloqueado", func(c *domain.Card) { c.BlockedBy = blocker.Key })
	mk("Limite", func(c *domain.Card) { c.PauseReason = domain.PauseReasonLimit })
	q := mk("Pergunta", func(*domain.Card) {})
	if _, err := e.st.AddQuestion(domain.Question{CardID: q.ID, Text: "qual?"}); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start"))
	for _, want := range []string{" · bloqueado por " + blocker.Key, " · pausado por limite de custo", " · aguardando você (1 pergunta(s))", "nobre block <KEY>"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("contexto sem %q:\n%s", want, ctx)
		}
	}
	if err := e.st.MoveCard(blocker.Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	ctx = additionalContext(t, e.call(t, "session_start"))
	if !strings.Contains(ctx, " · pronto para retomar ("+blocker.Key+" concluído)") {
		t.Fatalf("contexto sem pronto para retomar:\n%s", ctx)
	}
}
