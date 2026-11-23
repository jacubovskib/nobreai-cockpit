package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

const fakeID = "fake:s1"

// fakeGit responde branch e HEAD fixos.
type fakeGit struct{ branch string }

func (g fakeGit) Branch(string) string           { return g.branch }
func (g fakeGit) Head(string) string             { return "abc123" }
func (g fakeGit) DiffStat(string, string) string { return "" }
func (g fakeGit) Clean(string) bool              { return true }
func (g fakeGit) Checkout(string, string) error  { return nil }

// fakeDriver devolve o próximo evento como tradução e guarda a resposta que
// recebe para formatar.
type fakeDriver struct {
	next    Event
	replies []Reply
}

func (d *fakeDriver) Name() string { return "fake" }

func (d *fakeDriver) Translate(event string, _ []byte) (Event, error) { return d.next, nil }

func (d *fakeDriver) Render(_ Event, r Reply) (adapter.HookReply, error) {
	d.replies = append(d.replies, r)
	return adapter.HookReply{Stdout: strings.Join(r.Context, "|")}, nil
}

// fullDriver tem as capacidades opcionais: anota as leituras do histórico
// pedidas e, nas notas e nas memórias, quantos eventos do tipo já estavam
// gravados quando o núcleo montou a resposta (sondas de ordem).
type fullDriver struct {
	fakeDriver
	st       *store.Store
	ingested []string
	// seen: "memórias session_start=N", "prompt prompt=N".
	seen []string
	// observed: "<kind> <sessão> prompt=N seen=M" de cada Observe (sonda
	// de ordem: prompts gravados e sondas das notas até ali).
	observed []string
}

func (d *fullDriver) Observe(sess domain.Session, ev Event) {
	d.observed = append(d.observed, fmt.Sprintf("%d %s prompt=%d seen=%d", ev.Kind, sess.ID, d.count(domain.EvPrompt), len(d.seen)))
}

func (d *fullDriver) Ingest(sess domain.Session, ev Event) {
	d.ingested = append(d.ingested, sess.ID+" "+ev.TranscriptPath)
}

func (d *fullDriver) count(kind string) int {
	all, _ := d.st.RecentEvents(1000)
	n := 0
	for _, ev := range all {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

func (d *fullDriver) StartNote(sess domain.Session) string { return "nota do início" }

func (d *fullDriver) PromptNote(sess domain.Session, ev Event) string {
	d.seen = append(d.seen, fmt.Sprintf("prompt %s=%d", domain.EvPrompt, d.count(domain.EvPrompt)))
	return "nota do prompt"
}

func (d *fullDriver) MemoriesBlock(sess domain.Session, set domain.Settings, room int) string {
	d.seen = append(d.seen, fmt.Sprintf("memórias %s=%d", domain.EvSessionStart, d.count(domain.EvSessionStart)))
	return "Memórias deste projeto:\n- x\n"
}

func newFull(t *testing.T) (*env, *fullDriver) {
	d := &fullDriver{}
	e := newEnv(t, d)
	d.st = e.st
	return e, d
}

type env struct {
	st *store.Store
	c  *Core
	d  *fakeDriver
}

func newEnv(t *testing.T, d Driver) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cockpit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{st: st}
	e.c = New(st, adapter.Deps{Now: func() time.Time { return t0 }, Git: fakeGit{branch: "main"}}, d)
	return e
}

func newFake(t *testing.T) (*env, *fakeDriver) {
	d := &fakeDriver{}
	e := newEnv(t, d)
	e.d = d
	return e, d
}

func (e *env) handle(t *testing.T, d *fakeDriver, ev Event) adapter.HookReply {
	t.Helper()
	if ev.ProviderSessionID == "" {
		ev.ProviderSessionID = "s1"
	}
	if ev.Cwd == "" {
		ev.Cwd = "/w/api"
	}
	pid := 4242
	if ev.PID != 0 {
		pid = ev.PID
	}
	d.next = ev
	r, err := e.c.Handle(context.Background(), adapter.HookCall{AgentPID: pid, Env: ev.Env})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *env) events(t *testing.T, kind string) []map[string]any {
	t.Helper()
	all, err := e.st.RecentEvents(1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, ev := range all {
		if ev.Kind != kind {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(ev.Data, &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func (e *env) project(t *testing.T) domain.Project {
	t.Helper()
	p, err := e.st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStartedCreatesSessionWithLocationAndProject(t *testing.T) {
	e, d := newFake(t)
	p := e.project(t)
	e.handle(t, d, Event{Kind: Started, Source: "startup", TranscriptPath: "/t/s1.jsonl"})
	x, err := e.st.GetSession(fakeID)
	if err != nil {
		t.Fatal(err)
	}
	if x.Agent != "fake" || x.ProviderSessionID != "s1" || x.PID != 4242 || x.Cwd != "/w/api" || x.Branch != "main" ||
		x.GitHead != "abc123" || x.TranscriptPath != "/t/s1.jsonl" || !x.StartedAt.Equal(t0) {
		t.Fatalf("sessão = %+v", x)
	}
	if x.ProjectID == nil || *x.ProjectID != p.ID || !x.FullCapture {
		t.Fatalf("projeto = %v, captura = %v", x.ProjectID, x.FullCapture)
	}
	if evs := e.events(t, domain.EvSessionStart); len(evs) != 1 || evs[0]["source"] != "startup" {
		t.Fatalf("eventos = %v", evs)
	}
}

func TestPromptRecordedOnlyWithFullCapture(t *testing.T) {
	e, d := newFake(t)
	e.handle(t, d, Event{Kind: Prompt, Prompt: "sem projeto"})
	if evs := e.events(t, domain.EvPrompt); len(evs) != 0 {
		t.Fatalf("prompt gravado sem captura: %v", evs)
	}

	e, d = newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Prompt, Prompt: "corrija o login"})
	if evs := e.events(t, domain.EvPrompt); len(evs) != 1 || evs[0]["text"] != "corrija o login" {
		t.Fatalf("eventos = %v", evs)
	}
}

func TestToolAfterRecordsFilesAndCommands(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/auth/oauth.go"}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolRead, Path: "/etc/hosts"}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit}}) // sem caminho: nada
	code := 1
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolCommand, Command: "go test ./...", ExitCode: &code}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolCommand, Command: "ls"}})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolOther, Name: "WebFetch"}})

	if evs := e.events(t, domain.EvFileEdit); len(evs) != 1 || evs[0]["path"] != "auth/oauth.go" {
		t.Fatalf("edições = %v", evs)
	}
	if evs := e.events(t, domain.EvFileRead); len(evs) != 1 || evs[0]["path"] != "/etc/hosts" {
		t.Fatalf("leituras = %v", evs)
	}
	cmds := e.events(t, domain.EvCommand)
	if len(cmds) != 2 {
		t.Fatalf("comandos = %v", cmds)
	}
	byCmd := map[string]map[string]any{}
	for _, c := range cmds {
		byCmd[c["command"].(string)] = c
	}
	if c := byCmd["go test ./..."]; c == nil || c["exit_code"] != float64(1) {
		t.Fatalf("comando com código = %v", c)
	}
	if c := byCmd["ls"]; c == nil || c["exit_code"] != nil {
		t.Fatalf("comando sem código = %v", c)
	}
}

func TestToolAfterWithoutCaptureRecordsNothing(t *testing.T) {
	e, d := newFake(t)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/a.go"}})
	if evs := e.events(t, domain.EvFileEdit); len(evs) != 0 {
		t.Fatalf("edições = %v", evs)
	}
}

func TestEndedFinishesWithDriverReason(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	// O driver traduz o motivo; o código bruto vai para o evento.
	e.handle(t, d, Event{Kind: Ended, Reason: "saiu pelo menu", ReasonCode: "menu_exit"})
	x, _ := e.st.GetSession(fakeID)
	if x.EndedAt == nil || x.EndReason != "saiu pelo menu" {
		t.Fatalf("sessão = %+v", x)
	}
	if evs := e.events(t, domain.EvSessionEnd); len(evs) != 1 || evs[0]["reason"] != "menu_exit" {
		t.Fatalf("eventos = %v", evs)
	}

	// Sem motivo, o padrão.
	e, d = newFake(t)
	e.handle(t, d, Event{Kind: Started})
	e.handle(t, d, Event{Kind: Ended})
	if x, _ := e.st.GetSession(fakeID); x.EndReason != "encerrada pelo agente" {
		t.Fatalf("motivo vazio: %q", x.EndReason)
	}
}

func TestEndedNeverCreatesNorReopens(t *testing.T) {
	e, d := newFake(t)
	if r := e.handle(t, d, Event{Kind: Ended}); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	if _, err := e.st.GetSession(fakeID); err == nil {
		t.Fatal("SessionEnd criou a sessão")
	}
	if len(d.replies) != 0 {
		t.Fatal("hook ignorado chegou ao Render")
	}

	e.handle(t, d, Event{Kind: Started})
	e.handle(t, d, Event{Kind: Ended})
	e.handle(t, d, Event{Kind: Ended, PID: 5555}) // repetido, com outro PID
	if x, _ := e.st.GetSession(fakeID); x.EndedAt == nil {
		t.Fatalf("SessionEnd reabriu a sessão: %+v", x)
	}
	e.handle(t, d, Event{Kind: Prompt}) // qualquer outro evento reabre
	if x, _ := e.st.GetSession(fakeID); !x.Live() {
		t.Fatalf("sessão não reaberta: %+v", x)
	}
}

func TestOptionalCapabilities(t *testing.T) {
	e, d := newFull(t)
	e.project(t)
	e.handle(t, &d.fakeDriver, Event{Kind: Started})
	start := lastReply(t, &d.fakeDriver).Context
	// Memórias logo depois das instruções; a nota do driver abre o resto.
	if len(start) != 1 || !strings.HasPrefix(start[0], RegisteredPhrase) ||
		!strings.Contains(start[0], ".\n\nMemórias deste projeto:\n- x\n\nnota do início") {
		t.Fatalf("início = %q", start)
	}
	e.handle(t, &d.fakeDriver, Event{Kind: TurnEnd, TranscriptPath: "/t/a.jsonl"})
	if got := e.handle(t, &d.fakeDriver, Event{Kind: Prompt}); got.Stdout != "nota do prompt||" {
		t.Fatalf("resposta = %+v", got)
	}
	e.handle(t, &d.fakeDriver, Event{Kind: Ended, TranscriptPath: "/t/b.jsonl"})
	if len(d.ingested) != 2 || d.ingested[0] != fakeID+" /t/a.jsonl" || d.ingested[1] != fakeID+" /t/b.jsonl" {
		t.Fatalf("leituras = %v", d.ingested)
	}
}

// Ordem de que um driver sem Router (Kiro) depende: o início é gravado antes
// de a resposta ser montada; o prompt e a ferramenta, depois.
func TestRecordOrderAroundResponses(t *testing.T) {
	e, d := newFull(t)
	e.project(t)
	e.handle(t, &d.fakeDriver, Event{Kind: Started})
	e.handle(t, &d.fakeDriver, Event{Kind: Prompt, Prompt: "corrija o login"})
	want := []string{"memórias session_start=1", "prompt prompt=0"}
	if strings.Join(d.seen, ",") != strings.Join(want, ",") {
		t.Fatalf("sondas = %v, quer %v", d.seen, want)
	}
	if evs := e.events(t, domain.EvPrompt); len(evs) != 1 {
		t.Fatalf("prompt não gravado depois: %v", evs)
	}

	// ToolAfter: o aviso de saída grande (session_alerts) é decidido antes da
	// edição ser gravada.
	for _, q := range []string{
		`CREATE TABLE probe(n INTEGER)`,
		`CREATE TRIGGER probe_ins AFTER INSERT ON session_alerts BEGIN
			INSERT INTO probe SELECT count(*) FROM events WHERE kind='` + domain.EvFileEdit + `'; END`,
		`CREATE TRIGGER probe_upd AFTER UPDATE ON session_alerts BEGIN
			INSERT INTO probe SELECT count(*) FROM events WHERE kind='` + domain.EvFileEdit + `'; END`,
	} {
		if _, err := e.st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	e.handle(t, &d.fakeDriver, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/a.go", OutputBytes: 400_000}})
	if got := lastReply(t, &d.fakeDriver).Context; !strings.Contains(strings.Join(got, "|"), "Essa saída teve ~100 mil tokens") {
		t.Fatalf("sem aviso de saída grande: %q", got)
	}
	var probes []int
	rows, err := e.st.DB.Query(`SELECT n FROM probe`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		probes = append(probes, n)
	}
	if len(probes) != 1 || probes[0] != 0 {
		t.Fatalf("edições gravadas quando o aviso foi decidido = %v, quer [0]", probes)
	}
	if evs := e.events(t, domain.EvFileEdit); len(evs) != 1 {
		t.Fatalf("edição não gravada depois: %v", evs)
	}
}

// failOn faz o banco recusar a atualização da coluna col de sessions.
func (e *env) failOn(t *testing.T, col string) {
	t.Helper()
	q := `CREATE TRIGGER fail_` + col + ` BEFORE UPDATE OF ` + col + ` ON sessions BEGIN SELECT RAISE(ABORT, 'falha de teste'); END`
	if _, err := e.st.DB.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func (e *env) handleErr(d *fakeDriver, ev Event) error {
	ev.ProviderSessionID, ev.Cwd = "s1", "/w/api"
	d.next = ev
	_, err := e.c.Handle(context.Background(), adapter.HookCall{AgentPID: 4242})
	return err
}

// O fim de turno lê o histórico mesmo quando a resposta falha.
func TestTurnEndIngestsEvenIfResponseFails(t *testing.T) {
	e, d := newFull(t)
	e.project(t)
	e.handle(t, &d.fakeDriver, Event{Kind: Started})
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		e.handle(t, &d.fakeDriver, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + f}})
	}
	e.failOn(t, "reminded") // o lembrete sem card não consegue se marcar
	if err := e.handleErr(&d.fakeDriver, Event{Kind: TurnEnd, TranscriptPath: "/t/a.jsonl"}); err == nil {
		t.Fatal("esperava o erro da resposta")
	}
	if len(d.ingested) != 1 || d.ingested[0] != fakeID+" /t/a.jsonl" {
		t.Fatalf("leituras = %v", d.ingested)
	}
}

// brokenRender falha ao formatar a resposta.
type brokenRender struct{ fullDriver }

func (d *brokenRender) Render(Event, Reply) (adapter.HookReply, error) {
	return adapter.HookReply{}, errors.New("falha do render")
}

// O erro da regra não some atrás do erro do Render.
func TestRenderErrorKeepsRuleError(t *testing.T) {
	d := &brokenRender{}
	e := newEnv(t, d)
	d.st = e.st
	e.project(t)
	_ = e.handleErr(&d.fakeDriver, Event{Kind: Started})
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		_ = e.handleErr(&d.fakeDriver, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + f}})
	}
	e.failOn(t, "reminded")
	err := e.handleErr(&d.fakeDriver, Event{Kind: TurnEnd})
	if err == nil || !strings.Contains(err.Error(), "falha de teste") || !strings.Contains(err.Error(), "falha do render") {
		t.Fatalf("erro = %v", err)
	}
}

// Fim de sessão que não grava: nem evento, nem leitura.
func TestEndedFailureRecordsNothing(t *testing.T) {
	e, d := newFull(t)
	e.project(t)
	e.handle(t, &d.fakeDriver, Event{Kind: Started})
	e.failOn(t, "ended_at")
	if err := e.handleErr(&d.fakeDriver, Event{Kind: Ended, TranscriptPath: "/t/a.jsonl"}); err == nil {
		t.Fatal("esperava o erro do fim da sessão")
	}
	if evs := e.events(t, domain.EvSessionEnd); len(evs) != 0 {
		t.Fatalf("eventos = %v", evs)
	}
	if len(d.ingested) != 0 {
		t.Fatalf("leituras = %v", d.ingested)
	}
}

// routerDriver resolve a sessão por conta própria: ignora tudo.
type routerDriver struct{ fakeDriver }

func (d *routerDriver) Route(*Core, Event) (domain.Session, bool, error) {
	return domain.Session{}, true, ErrIgnore
}

func TestRouterIgnoreRepliesEmpty(t *testing.T) {
	d := &routerDriver{}
	e := newEnv(t, d)
	if r := e.handle(t, &d.fakeDriver, Event{Kind: Started}); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	if _, err := e.st.GetSession(fakeID); err == nil {
		t.Fatal("sessão criada apesar do Router")
	}
}

func TestMissingSessionIDFails(t *testing.T) {
	e, d := newFake(t)
	d.next = Event{Kind: Started}
	if _, err := e.c.Handle(context.Background(), adapter.HookCall{}); err == nil {
		t.Fatal("esperava erro sem id de sessão")
	}
}

func TestOtherEventOnlyTouchesSession(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	e.c.Deps().Now = func() time.Time { return t0.Add(time.Minute) }
	if r := e.handle(t, d, Event{Kind: Other}); r != (adapter.HookReply{}) {
		t.Fatalf("resposta = %+v", r)
	}
	if x, _ := e.st.GetSession(fakeID); !x.LastEventAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("sessão = %+v", x)
	}
}

// O Observer vê todo evento, já com a sessão resolvida, antes das regras: o
// prompt ainda não foi gravado e a nota do prompt ainda não foi pedida.
func TestObserverSeesEveryEventBeforeRules(t *testing.T) {
	e, d := newFull(t)
	e.project(t)
	for _, k := range []Kind{Started, Prompt, ToolBefore, ToolAfter, TurnEnd, Compacted, Other, Ended} {
		e.handle(t, &d.fakeDriver, Event{Kind: k, Prompt: "corrija o login"})
	}
	want := []string{
		fmt.Sprintf("%d %s prompt=0 seen=0", Started, fakeID),
		fmt.Sprintf("%d %s prompt=0 seen=1", Prompt, fakeID), // memórias já sondadas; nota do prompt ainda não
		fmt.Sprintf("%d %s prompt=1 seen=2", ToolBefore, fakeID),
		fmt.Sprintf("%d %s prompt=1 seen=2", ToolAfter, fakeID),
		fmt.Sprintf("%d %s prompt=1 seen=2", TurnEnd, fakeID),
		fmt.Sprintf("%d %s prompt=1 seen=2", Compacted, fakeID),
		fmt.Sprintf("%d %s prompt=1 seen=2", Other, fakeID),
		fmt.Sprintf("%d %s prompt=1 seen=2", Ended, fakeID),
	}
	if strings.Join(d.observed, ",") != strings.Join(want, ",") {
		t.Fatalf("observados = %v, quer %v", d.observed, want)
	}
}

// Hook ignorado (sem sessão) não chega ao Observer.
func TestObserverSkipsIgnoredHook(t *testing.T) {
	e, d := newFull(t)
	e.handle(t, &d.fakeDriver, Event{Kind: Ended})
	if len(d.observed) != 0 {
		t.Fatalf("observados = %v", d.observed)
	}
}
