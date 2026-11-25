// Package e2e testa o handoff de ponta a ponta contra um servidor real em
// httptest: hooks via HTTP, comandos do agente via internal/client e /proc
// falso. Nada toca ~/.claude, terminal ou agente de verdade.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/api"
	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/client"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const (
	sessA = "11111111-aaaa-4aaa-8aaa-111111111111"
	sessB = "22222222-bbbb-4bbb-8bbb-222222222222"
	model = "claude-opus-5-5"
)

type fakeLauncher struct{ calls [][]string }

func (f *fakeLauncher) Start(argv []string) error { f.calls = append(f.calls, argv); return nil }

// env é o ambiente do teste: servidor, relógio manual, /proc falso e adaptador.
type env struct {
	t       *testing.T
	st      *store.Store
	ad      *claude.Adapter
	srv     *httptest.Server
	launch  *fakeLauncher
	procDir string
	project string
	// claudeBin é o CLI falso que o continue põe no comando.
	claudeBin string
	now       atomic.Int64 // UnixNano
}

func (e *env) clock() time.Time { return time.Unix(0, e.now.Load()).UTC() }

// tick avança o relógio (eventos ficam em ordem estrita).
func (e *env) tick(d time.Duration) { e.now.Add(int64(d)) }

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, launch: &fakeLauncher{}}
	e.now.Store(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC).UnixNano())
	var err error
	e.st, err = store.Open(filepath.Join(t.TempDir(), "cockpit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	e.project = filepath.Join(t.TempDir(), "api")
	e.procDir = t.TempDir()
	if err := os.MkdirAll(e.project, 0o755); err != nil {
		t.Fatal(err)
	}
	e.fakeProc(100, "claude")
	e.fakeProc(200, "claude")

	hub := api.NewHub()
	t.Cleanup(hub.Close)
	e.ad = claude.New(e.st, adapter.Deps{Now: e.clock, Publish: hub.Publish})
	// CLI falso: o continue resolve o claude para um caminho absoluto.
	e.claudeBin = filepath.Join(t.TempDir(), "claude")
	must(t, os.WriteFile(e.claudeBin, []byte("#!/bin/sh\n"), 0o755))
	e.ad.SetPaths(claude.Paths{ClaudeBin: e.claudeBin})
	e.srv = httptest.NewServer(api.New(e.st, api.Deps{
		Now: e.clock, Hub: hub, ProcRoot: e.procDir,
		Agents:      agents.New(e.ad),
		Launcher:    e.launch,
		SessionPATH: "/usr/bin:/opt/nobre/bin",
		ResolveTerminal: func(string) (string, error) {
			return "/usr/bin/xterm", nil
		},
	}))
	t.Cleanup(e.srv.Close)
	t.Cleanup(e.ad.Wait)
	return e
}

// fakeProc cria <proc>/<pid>/{comm,stat} como o Linux (ppid 1).
func (e *env) fakeProc(pid int, comm string) {
	dir := filepath.Join(e.procDir, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	must(e.t, os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644))
	must(e.t, os.WriteFile(filepath.Join(dir, "stat"), []byte(fmt.Sprintf("%d (%s) S 1 %d 0 0", pid, comm, pid)), 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (e *env) addr() string { return strings.TrimPrefix(e.srv.URL, "http://") }

// agent devolve o cliente do `nobre` rodando sob o processo pid, descoberto
// pelo NOBRE_AGENT_PID como no uso real.
func (e *env) agent(pid int) *client.Client {
	e.t.Setenv("NOBRE_AGENT_PID", fmt.Sprint(pid))
	return client.New(e.addr())
}

// api faz uma chamada JSON ao servidor e devolve status e corpo.
func (e *env) api(method, path string, in any) (int, []byte) {
	e.t.Helper()
	var body *bytes.Reader
	if in != nil {
		b, err := json.Marshal(in)
		must(e.t, err)
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	must(e.t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	must(e.t, err)
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func (e *env) get(path string, out any) {
	e.t.Helper()
	code, b := e.api("GET", path, nil)
	if code != 200 {
		e.t.Fatalf("GET %s = %d %s", path, code, b)
	}
	must(e.t, json.Unmarshal(b, out))
}

// hook posta um HookCall em /api/hooks/claude (o caminho do `nobre hook`).
func (e *env) hook(pid int, session, event string, extra map[string]any, hookEnv map[string]string) adapter.HookReply {
	e.t.Helper()
	e.tick(time.Second)
	p := map[string]any{
		"session_id": session, "cwd": e.project, "hook_event_name": event,
		"transcript_path": e.transcript(session),
	}
	for k, v := range extra {
		p[k] = v
	}
	raw, err := json.Marshal(p)
	must(e.t, err)
	reply, err := e.agent(pid).Hook(context.Background(), "claude",
		adapter.HookCall{Event: event, Payload: raw, AgentPID: pid, Env: hookEnv})
	must(e.t, err)
	return reply
}

func (e *env) transcript(session string) string {
	name := "session_a.jsonl"
	if session == sessB {
		name = "session_b.jsonl"
	}
	p, err := filepath.Abs(filepath.Join("testdata", name))
	must(e.t, err)
	return p
}

type cardSummary struct {
	Key          string     `json:"key"`
	Column       string     `json:"column"`
	Live         bool       `json:"live"`
	PIDDeadAt    *time.Time `json:"pid_dead_at"`
	SessionCount int        `json:"session_count"`
	Totals       total      `json:"totals"`
}

type total struct {
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheWrite int64   `json:"cache_write"`
	CacheRead  int64   `json:"cache_read"`
	CostUSD    float64 `json:"cost_usd"`
	Unpriced   bool    `json:"unpriced"`
}

func (e *env) board() map[string]cardSummary {
	var cs []cardSummary
	e.get("/api/cards", &cs)
	m := map[string]cardSummary{}
	for _, c := range cs {
		m[c.Key] = c
	}
	return m
}

func (e *env) column(key string) string { return e.board()[key].Column }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func (e *env) expectTotal(label string, got total, in, out, cw, cr int64, cost float64) {
	e.t.Helper()
	if got.Input != in || got.Output != out || got.CacheWrite != cw || got.CacheRead != cr || got.Unpriced || !near(got.CostUSD, cost) {
		e.t.Fatalf("%s = %+v, quero in=%d out=%d cw=%d cr=%d US$ %.6f", label, got, in, out, cw, cr, cost)
	}
}

// Custos com preço 15/75/18,75/1,5 por milhão.
const (
	costA = (20*15 + 420*75 + 2000*18.75 + 32000*1.5) / 1e6
	costB = (15*15 + 300*75 + 1000*18.75 + 42000*1.5) / 1e6
)

func TestHandoffEndToEnd(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// Preço do modelo do transcript e projeto registrado, pela API.
	cw, cr := 18.75, 1.5
	if code, b := e.api("PUT", "/api/prices", map[string]any{"model": model, "input_per_m": 15.0, "output_per_m": 75.0,
		"cache_write_per_m": cw, "cache_read_per_m": cr}); code != 200 {
		t.Fatalf("preço = %d %s", code, b)
	}
	if _, err := e.agent(100).AddProject(ctx, "api", e.project); err != nil {
		t.Fatal(err)
	}

	// --- Sessão A (PID 100) ---
	e.hook(100, sessA, "SessionStart", map[string]any{"source": "startup"}, nil)
	a := e.agent(100)
	card, err := a.Start(ctx, "Refresh de token OAuth", "Renovar o token sem logout", "Refresh automático passa nos testes", client.CardFields{})
	must(t, err)
	if card.Key != "NAC-1" {
		t.Fatalf("chave = %q", card.Key)
	}
	if got := e.column("NAC-1"); got != "em_andamento" {
		t.Fatalf("A viva: coluna = %q", got)
	}

	e.hook(100, sessA, "PostToolUse", map[string]any{"tool_name": "Read",
		"tool_input": map[string]any{"file_path": filepath.Join(e.project, "auth/oauth.go")}}, nil)
	for _, f := range []string{"auth/oauth.go", "auth/refresh.go"} {
		e.hook(100, sessA, "PostToolUse", map[string]any{"tool_name": "Edit",
			"tool_input": map[string]any{"file_path": filepath.Join(e.project, f)}}, nil)
	}
	e.tick(time.Second)
	must(t, a.Me(ctx, "note", map[string]string{"text": "Refresh pronto em auth/refresh.go; falta tratar o callback expirado"}))
	e.tick(time.Second)
	must(t, a.Me(ctx, "decide", map[string]string{"what": "Renovar 60 s antes de expirar", "why": "evita corrida com requisições em voo"}))
	must(t, a.Me(ctx, "file", map[string]string{"path": "auth/refresh.go", "why": "lógica de renovação", "lines": "10-80"}))
	must(t, a.Me(ctx, "todo", map[string]string{"text": "Tratar callback expirado"}))
	must(t, a.Me(ctx, "test", map[string]string{"text": "go test ./auth/... -run Refresh"}))

	if r := e.hook(100, sessA, "Stop", map[string]any{"stop_hook_active": false}, nil); r.Stdout != "" {
		t.Fatalf("Stop não deveria bloquear (handoff completo): %s", r.Stdout)
	}
	e.ad.Wait()
	e.hook(100, sessA, "SessionEnd", map[string]any{"reason": "prompt_input_exit"}, nil)
	e.ad.Wait()

	if got := e.column("NAC-1"); got != "pausado" {
		t.Fatalf("A encerrada: coluna = %q", got)
	}
	var det struct {
		Readiness struct{ Ready bool } `json:"readiness"`
		Totals    total                `json:"totals"`
		Sessions  []struct {
			ID    string `json:"id"`
			Usage total  `json:"usage"`
		} `json:"sessions"`
	}
	e.get("/api/cards/NAC-1", &det)
	if !det.Readiness.Ready {
		t.Fatalf("handoff não está pronto: %+v", det.Readiness)
	}
	if len(det.Sessions) != 1 {
		t.Fatalf("sessões = %d", len(det.Sessions))
	}
	e.expectTotal("sessão A", det.Sessions[0].Usage, 20, 420, 2000, 32000, costA)

	// --- Handoff pela UI: "Continuar com Claude Code" ---
	if code, b := e.api("POST", "/api/cards/NAC-1/continue", map[string]any{"agent": "claude", "mode": "terminal", "prompt_slug": "retomar-card"}); code != 200 {
		t.Fatalf("continue = %d %s", code, b)
	}
	if len(e.launch.calls) != 1 {
		t.Fatalf("lançamentos = %d", len(e.launch.calls))
	}
	script := e.launch.calls[0][len(e.launch.calls[0])-1]
	for _, want := range []string{"export PATH='/usr/bin:/opt/nobre/bin';", "cd '" + e.project + "'", "NOBRE_RESUME='NAC-1'",
		"'" + e.claudeBin + "' '--' ",
		// Prompt inicial padrão renderizado (o que o painel da UI envia).
		"Você está continuando o card NAC-1 (Refresh de token OAuth).",
		"falta tratar o callback expirado"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script do terminal sem %q:\n%s", want, script)
		}
	}
	// O pacote de retomada não vai no argv: chega no SessionStart de B.
	for _, pkgOnly := range []string{"Renovar 60 s antes de expirar", "lógica de renovação", "go test ./auth/... -run Refresh"} {
		if strings.Contains(script, pkgOnly) {
			t.Fatalf("script do terminal repete o pacote (%q):\n%s", pkgOnly, script)
		}
	}

	// --- Sessão B (PID 200) abre com NOBRE_RESUME, como o terminal faria ---
	r := e.hook(200, sessB, "SessionStart", map[string]any{"source": "startup"}, map[string]string{"NOBRE_RESUME": "NAC-1"})
	var out struct {
		Hook struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	must(t, json.Unmarshal([]byte(r.Stdout), &out))
	// Critério 1: o agente recebe o que precisa sem perguntar.
	for _, want := range []string{
		"NAC-1", "Onde parei", "falta tratar o callback expirado",
		"Renovar 60 s antes de expirar", "evita corrida com requisições em voo",
		"auth/refresh.go", "lógica de renovação",
		"Tratar callback expirado",
		"Como testar", "go test ./auth/... -run Refresh",
	} {
		if !strings.Contains(out.Hook.Context, want) {
			t.Fatalf("contexto de B sem %q:\n%s", want, out.Hook.Context)
		}
	}
	// Critério 3: B viva → em andamento, com 2 sessões.
	if b := e.board()["NAC-1"]; b.Column != "em_andamento" || !b.Live || b.SessionCount != 2 {
		t.Fatalf("B viva: %+v", b)
	}
	// B também fala com a API pelo PID 200 e já está no card.
	st, err := e.agent(200).Status(ctx)
	must(t, err)
	if st.Card == nil || st.Card.Key != "NAC-1" {
		t.Fatalf("status de B = %+v", st)
	}

	// Com sessão viva, mover à mão para fora e de volta a em andamento é aceito.
	if code, b := e.api("PATCH", "/api/cards/NAC-1", map[string]string{"column": "pausado"}); code != 200 {
		t.Fatalf("PATCH pausado = %d %s", code, b)
	}
	if code, b := e.api("PATCH", "/api/cards/NAC-1", map[string]string{"column": "em_andamento"}); code != 200 {
		t.Fatalf("PATCH em_andamento com sessão viva = %d %s", code, b)
	}
	if got := e.column("NAC-1"); got != "em_andamento" {
		t.Fatalf("após PATCH com sessão viva: coluna = %q", got)
	}

	e.hook(200, sessB, "Stop", map[string]any{"stop_hook_active": false}, nil)
	e.ad.Wait()

	// Critério 2: tokens e US$ por sessão e somados no card (usage.card_id).
	e.get("/api/cards/NAC-1", &det)
	if len(det.Sessions) != 2 {
		t.Fatalf("sessões = %d", len(det.Sessions))
	}
	per := map[string]total{}
	for _, s := range det.Sessions {
		per[s.ID] = s.Usage
	}
	e.expectTotal("sessão A", per["claude:"+sessA], 20, 420, 2000, 32000, costA)
	e.expectTotal("sessão B", per["claude:"+sessB], 15, 300, 1000, 42000, costB)
	e.expectTotal("card (detalhe)", det.Totals, 35, 720, 3000, 74000, costA+costB)
	e.expectTotal("card (board)", e.board()["NAC-1"].Totals, 35, 720, 3000, 74000, costA+costB)
	var byCard []struct {
		Key string `json:"key"`
		total
	}
	e.get("/api/usage/totals?group=card", &byCard)
	if len(byCard) != 1 || byCard[0].Key != "NAC-1" {
		t.Fatalf("totais por card = %+v", byCard)
	}
	e.expectTotal("card (usage/totals)", byCard[0].total, 35, 720, 3000, 74000, costA+costB)

	// --- Critério 3: processo morto não é sessão viva ---
	must(t, os.RemoveAll(filepath.Join(e.procDir, "200")))
	e.tick(time.Minute)
	reconcile := func() {
		set, err := e.st.GetSettings()
		must(t, err)
		_, err = board.Reconcile(e.st, board.Liveness{ProcRoot: e.procDir}, e.clock(), set)
		must(t, err)
	}
	reconcile()
	b := e.board()["NAC-1"]
	if b.Live || b.PIDDeadAt == nil {
		t.Fatalf("PID morto na carência: live=%v pid_dead_at=%v", b.Live, b.PIDDeadAt)
	}
	if b.Column != "em_andamento" {
		t.Fatalf("dentro da carência o card segue em andamento, veio %q", b.Column)
	}
	e.tick(16 * time.Minute) // passa da carência de 15 min
	reconcile()
	if got := e.column("NAC-1"); got != "pausado" {
		t.Fatalf("após a carência: coluna = %q", got)
	}
	// Sem sessão viva, mover à mão para em andamento é recusado.
	code, body := e.api("PATCH", "/api/cards/NAC-1", map[string]string{"column": "em_andamento"})
	if code != http.StatusConflict {
		t.Fatalf("PATCH em_andamento sem sessão viva = %d %s", code, body)
	}
}
