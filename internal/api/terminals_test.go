package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nobretech/nobreai-cockpit/internal/pty"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// binAdapter é um agente falso cujo binário é um executável do sistema.
type binAdapter struct {
	fakeAdapter
	bin string
}

func (b *binAdapter) Binary() string { return b.bin }
func (b *binAdapter) Name() string   { return "claude" }

type termEnv struct {
	srv *httptest.Server
	mgr *pty.Manager
	hub *Hub
}

func newTermEnv(t *testing.T, bin string) *termEnv {
	t.Helper()
	return newTermEnvOpts(t, bin, pty.Options{})
}

func newTermEnvOpts(t *testing.T, bin string, opts pty.Options) *termEnv {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	mgr := pty.NewManager(opts)
	t.Cleanup(mgr.Close)
	srv := httptest.NewServer(New(st, Deps{Hub: hub, Terminals: mgr,
		Agents: agents.New(&binAdapter{bin: bin})}))
	t.Cleanup(srv.Close)
	return &termEnv{srv: srv, mgr: mgr, hub: hub}
}

func (e *termEnv) create(t *testing.T, body map[string]any) (*http.Response, pty.Info) {
	t.Helper()
	if _, ok := body["dir"]; !ok {
		body["dir"] = t.TempDir()
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(e.srv.URL+"/api/terminals", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info pty.Info
	if resp.StatusCode == http.StatusCreated {
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			t.Fatal(err)
		}
	}
	return resp, info
}

func (e *termEnv) dial(t *testing.T, id, query string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	url := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/api/terminals/" + id + "/ws" + query
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: hdr})
	if c != nil {
		t.Cleanup(func() { c.CloseNow() })
	}
	return c, resp, err
}

// readUntil lê mensagens até a saída binária acumulada satisfazer pred (ou o
// socket fechar); devolve a saída e as mensagens de texto.
func readUntil(t *testing.T, c *websocket.Conn, pred func(out string) bool) (string, []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out strings.Builder
	var texts []string
	for !pred(out.String()) {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return out.String(), texts
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
		} else {
			texts = append(texts, string(data))
		}
	}
	return out.String(), texts
}

func TestTerminalWSStreamsAndAcceptsInput(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	resp, info := e.create(t, map[string]any{"cols": 100, "rows": 30})
	if resp.StatusCode != http.StatusCreated || info.Cols != 100 || info.Agent != "claude" || info.Origin != "plataforma" {
		t.Fatalf("resp=%d info=%+v", resp.StatusCode, info)
	}
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(context.Background(), websocket.MessageBinary, []byte("xyz\n")); err != nil {
		t.Fatal(err)
	}
	out, texts := readUntil(t, c, func(o string) bool { return strings.Contains(o, "xyz") })
	if !strings.Contains(out, "xyz") {
		t.Fatalf("saída = %q", out)
	}
	if len(texts) == 0 || !strings.Contains(texts[0], `"type":"info"`) || !strings.Contains(texts[0], `"id":"`+info.ID+`"`) {
		t.Fatalf("info = %v", texts)
	}
}

func TestTerminalWSSnapshotForLateJoiner(t *testing.T) {
	e := newTermEnv(t, "/bin/sh")
	_, info := e.create(t, map[string]any{"args": []string{"-c", "printf antigo; sleep 30"}})
	tm, _ := e.mgr.Get(info.ID)
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, _, cancel := tm.Subscribe()
		cancel()
		if strings.Contains(string(snap), "antigo") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agente não imprimiu")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := readUntil(t, c, func(o string) bool { return strings.Contains(o, "antigo") })
	if !strings.Contains(out, "antigo") {
		t.Fatalf("snapshot = %q", out)
	}
}

func TestTerminalWSExitMessage(t *testing.T) {
	e := newTermEnv(t, "/bin/sh")
	_, info := e.create(t, map[string]any{"args": []string{"-c", "printf fim; exit 3"}})
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Lê tudo até o fechamento: a saída final precede a mensagem exit.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var seq []string
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("fechamento = %v", err)
			}
			break
		}
		if typ == websocket.MessageText {
			seq = append(seq, "T:"+string(data))
		} else {
			seq = append(seq, "B:"+string(data))
		}
	}
	joined := strings.Join(seq, "|")
	last := seq[len(seq)-1]
	if last != `T:{"type":"exit","code":3,"reason":"agent"}` || !strings.Contains(joined, "fim") {
		t.Fatalf("seq = %q", seq)
	}
	if strings.Index(joined, "fim") > strings.Index(joined, `"exit"`) {
		t.Fatalf("exit antes da saída final: %q", seq)
	}
}

func TestTerminalWSRejectsForeignOrigin(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	for _, origin := range []string{"http://evil.example", "http://127.0.0.1:1", "https://127.0.0.1:" + portOf(e.srv.URL), "null",
		"http://10.0.0.1:" + portOf(e.srv.URL), "http://localhost.evil:" + portOf(e.srv.URL), "http://127.0.0.1", "http://127.0.0.1:" + portOf(e.srv.URL) + "/x"} {
		_, resp, err := e.dial(t, info.ID, "", http.Header{"Origin": {origin}})
		if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: err=%v resp=%v", origin, err, resp)
		}
	}
	for _, origin := range []string{"http://127.0.0.1:" + portOf(e.srv.URL), "http://localhost:" + portOf(e.srv.URL), "http://127.0.0.2:" + portOf(e.srv.URL)} {
		if _, _, err := e.dial(t, info.ID, "", http.Header{"Origin": {origin}}); err != nil {
			t.Fatalf("origin %q: %v", origin, err)
		}
	}
}

func portOf(u string) string { return u[strings.LastIndex(u, ":")+1:] }

func TestTerminalWSReadonlyIgnoresInput(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	ro, _, err := e.dial(t, info.ID, "?readonly=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	rw, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = ro.Write(ctx, websocket.MessageBinary, []byte("proibido\n"))
	_ = ro.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":50,"rows":10}`))
	// Mensagens de um mesmo socket são tratadas em ordem; a entrada do rw
	// abaixo só prova algo depois que o sinal "liberado" ecoar. Um segundo
	// round-trip pelo rw garante que o readonly já foi processado.
	_ = rw.Write(ctx, websocket.MessageBinary, []byte("liberado\n"))
	readUntil(t, rw, func(o string) bool { return strings.Contains(o, "liberado") })
	_ = rw.Write(ctx, websocket.MessageBinary, []byte("fim\n"))
	out, _ := readUntil(t, rw, func(o string) bool { return strings.Contains(o, "fim") })
	snap, _, cancel := mustTerm(t, e, info.ID).Subscribe()
	cancel()
	if strings.Contains(string(snap), "proibido") || strings.Contains(out, "proibido") {
		t.Fatalf("entrada do readonly chegou: %q", snap)
	}
	if in := e.mgr.List()[0]; in.Cols == 50 {
		t.Fatalf("resize do readonly aplicado: %+v", in)
	}
}

func mustTerm(t *testing.T, e *termEnv, id string) *pty.Terminal {
	t.Helper()
	tm, ok := e.mgr.Get(id)
	if !ok {
		t.Fatal("terminal sumiu")
	}
	return tm
}

func TestTerminalWSEndedTerminal(t *testing.T) {
	e := newTermEnv(t, "/bin/sh")
	_, info := e.create(t, map[string]any{"args": []string{"-c", "printf antes; exit 2"}})
	select {
	case <-mustTerm(t, e, info.ID).Done():
	case <-time.After(10 * time.Second):
		t.Fatal("não encerrou")
	}
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	var texts []string
	var rerr error
	for {
		typ, data, err := c.Read(context.Background())
		if err != nil {
			rerr = err
			break
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
		} else {
			texts = append(texts, string(data))
		}
	}
	if !strings.Contains(out.String(), "antes") || len(texts) != 2 ||
		!strings.Contains(texts[0], `"type":"info"`) || texts[1] != `{"type":"exit","code":2,"reason":"agent"}` {
		t.Fatalf("out=%q texts=%v", out.String(), texts)
	}
	if websocket.CloseStatus(rerr) != websocket.StatusNormalClosure {
		t.Fatalf("fechamento = %v", rerr)
	}
}

func TestTerminalWSForeignOriginBeforeLookup(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, resp, err := e.dial(t, "t-naoexiste", "", http.Header{"Origin": {"http://evil.example"}})
	if err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("err=%v resp=%v", err, resp)
	}
}

func TestTerminalWSResize(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Write(context.Background(), websocket.MessageText, []byte(`{"type":"resize","cols":90,"rows":20}`))
	deadline := time.Now().Add(5 * time.Second)
	for e.mgr.List()[0].Cols != 90 {
		if time.Now().After(deadline) {
			t.Fatal("resize não aplicado")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCreateTerminalValidation(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	for name, body := range map[string]map[string]any{
		"agente desconhecido": {"agent": "codex"},
		"dir inexistente":     {"dir": "/nao/existe/aqui"},
		"dir relativo":        {"dir": "relativo"},
		"origem inválida":     {"origin": "outra"},
	} {
		resp, _ := e.create(t, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status %d", name, resp.StatusCode)
		}
	}
}

func TestCreateTerminalUnknownAgent(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	b := `{"agent":"codex","dir":"` + t.TempDir() + `"}`
	resp, err := http.Post(e.srv.URL+"/api/terminals", "application/json", strings.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusBadRequest || out["error"] != "agente indisponível no piloto" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
}

func TestListTerminalsAndLimit(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	resp, err := http.Get(e.srv.URL + "/api/terminals")
	if err != nil {
		t.Fatal(err)
	}
	var empty []pty.Info
	json.NewDecoder(resp.Body).Decode(&empty)
	resp.Body.Close()
	if empty == nil || len(empty) != 0 {
		t.Fatalf("lista vazia = %#v", empty)
	}
	_, info := e.create(t, map[string]any{"card_key": "NAC-9", "origin": "nobre run"})
	resp, err = http.Get(e.srv.URL + "/api/terminals")
	if err != nil {
		t.Fatal(err)
	}
	var raw []map[string]any
	json.NewDecoder(resp.Body).Decode(&raw)
	resp.Body.Close()
	if len(raw) != 1 || raw[0]["id"] != info.ID || raw[0]["card_key"] != "NAC-9" || raw[0]["origin"] != "nobre run" {
		t.Fatalf("lista = %v", raw)
	}
}

func TestCreateTerminalTooMany(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	e.mgr.Close() // Start após Close = erro; troca por um manager de limite 1
	e.mgr = pty.NewManager(pty.Options{MaxLive: 1})
	t.Cleanup(e.mgr.Close)
	st, _ := store.Open(t.TempDir() + "/c.db")
	t.Cleanup(func() { st.Close() })
	srv := httptest.NewServer(New(st, Deps{Terminals: e.mgr,
		Agents: agents.New(&binAdapter{bin: "/bin/cat"})}))
	t.Cleanup(srv.Close)
	e.srv = srv
	if resp, _ := e.create(t, map[string]any{}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("1º: %d", resp.StatusCode)
	}
	if resp, _ := e.create(t, map[string]any{}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("2º: %d", resp.StatusCode)
	}
}

func TestDeleteTerminalKills(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	req, _ := http.NewRequest(http.MethodDelete, e.srv.URL+"/api/terminals/"+info.ID, nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	tm, _ := e.mgr.Get(info.ID)
	select {
	case <-tm.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("processo não morreu")
	}
	if c := tm.Info().ExitCode; c == nil || *c != 128+15 {
		t.Fatalf("exit = %v", c)
	}
	req, _ = http.NewRequest(http.MethodDelete, e.srv.URL+"/api/terminals/t-naoexiste", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("desconhecido: %d", resp.StatusCode)
	}
}

func TestTerminalPublishesOnCreateAndEnd(t *testing.T) {
	e := newTermEnv(t, "/bin/sh")
	ch, cancel := e.hub.Subscribe()
	defer cancel()
	e.create(t, map[string]any{"args": []string{"-c", "exit 0"}})
	got := 0
	timeout := time.After(10 * time.Second)
	for got < 2 {
		select {
		case k := <-ch:
			if k == "terminals" {
				got++
			}
		case <-timeout:
			t.Fatalf("publicações = %d, quero 2", got)
		}
	}
}

func TestTerminalNilManagerUnavailable(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/c.db")
	t.Cleanup(func() { st.Close() })
	srv := httptest.NewServer(New(st, Deps{}))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/api/terminals")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestCreateTerminalResumeKey(t *testing.T) {
	sh := writeScript(t, "#!/bin/sh\necho \"R=$NOBRE_RESUME\"\n")
	e := newTermEnv(t, sh)
	resp, info := e.create(t, map[string]any{"resume_key": "NAC-12"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("code = %d", resp.StatusCode)
	}
	tm := mustTerm(t, e, info.ID)
	<-tm.Done()
	snap, _, cancel := tm.Subscribe()
	cancel()
	if !strings.Contains(string(snap), "R=NAC-12") {
		t.Fatalf("saída = %q", snap)
	}
	for _, bad := range []string{"NAC-", "nac-1", "NAC-1; rm", "X=1"} {
		if resp, _ := e.create(t, map[string]any{"resume_key": bad}); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("resume_key %q = %d", bad, resp.StatusCode)
		}
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreateTerminalEnvAllowlist(t *testing.T) {
	sh := writeScript(t, "#!/bin/sh\necho \"K=$ANTHROPIC_API_KEY T=$TZ\"\n")
	e := newTermEnv(t, sh)
	resp, info := e.create(t, map[string]any{"env": map[string]string{"ANTHROPIC_API_KEY": "abc", "TZ": "UTC"}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("code = %d", resp.StatusCode)
	}
	tm := mustTerm(t, e, info.ID)
	<-tm.Done()
	snap, _, cancel := tm.Subscribe()
	cancel()
	if !strings.Contains(string(snap), "K=abc T=UTC") {
		t.Fatalf("saída = %q", snap)
	}
	for _, k := range []string{"PATH", "LD_PRELOAD", "NOBRE_RESUME", "HOME", "anthropic_x", "LD_LIBRARY_PATH"} {
		if resp, _ := e.create(t, map[string]any{"env": map[string]string{k: "x"}}); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("env %s = %d", k, resp.StatusCode)
		}
	}
}

// readExit lê até o fechamento e devolve a mensagem exit (se houver) e o erro final.
func readExit(t *testing.T, c *websocket.Conn) (map[string]any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var exit map[string]any
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return exit, err
		}
		if typ == websocket.MessageText {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil && m["type"] == "exit" {
				exit = m
			}
		}
	}
}

func TestTerminalWSDaemonShutdownReason(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, func(string) bool { return true }) // conectado (info)
	go e.mgr.Close()                                   // desligamento do nobred
	exit, rerr := readExit(t, c)
	if exit == nil || exit["reason"] != "daemon" || exit["code"] != float64(128+15) {
		t.Fatalf("exit = %v (%v)", exit, rerr)
	}
	if websocket.CloseStatus(rerr) != websocket.StatusNormalClosure {
		t.Fatalf("fechamento = %v", rerr)
	}
}

func TestTerminalWSKilledReason(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, func(string) bool { return true })
	if err := e.mgr.Kill(info.ID); err != nil {
		t.Fatal(err)
	}
	if exit, rerr := readExit(t, c); exit == nil || exit["reason"] != "killed" {
		t.Fatalf("exit = %v (%v)", exit, rerr)
	}
}

func TestTerminalWSWriteTimeoutKeepsConnection(t *testing.T) {
	e := newTermEnvOpts(t, "/bin/sh", pty.Options{WriteTimeout: 200 * time.Millisecond})
	_, info := e.create(t, map[string]any{"args": []string{"-c", "stty raw -echo; echo pronto; sleep 30"}})
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, func(o string) bool { return strings.Contains(o, "pronto") })
	ctx := context.Background()
	big := bytes.Repeat([]byte("a"), 900<<10) // o agente não lê: a escrita vence o prazo
	for i := 0; i < 2; i++ {
		if err := c.Write(ctx, websocket.MessageBinary, big); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":77,"rows":21}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for mustTerm(t, e, info.ID).Info().Cols != 77 {
		if time.Now().After(deadline) {
			t.Fatal("conexão caiu depois do prazo de escrita: resize não chegou")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := mustTerm(t, e, info.ID).Info().Clients; n != 1 {
		t.Fatalf("clients = %d", n)
	}
}

func TestTerminalWSSlowClientClosesWithReason(t *testing.T) {
	e := newTermEnvOpts(t, "/bin/sh", pty.Options{SubLimit: 16})
	_, info := e.create(t, map[string]any{"args": []string{"-c", "sleep 0.5; printf '%080d' 0; sleep 30"}})
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, rerr := readExit(t, c)
	var ce websocket.CloseError
	if exit != nil || !errors.As(rerr, &ce) || ce.Reason != "lento" {
		t.Fatalf("exit=%v err=%v", exit, rerr)
	}
}

func TestTerminalWSSendsEmptySnapshot(t *testing.T) {
	// O cliente libera a entrada depois do snapshot: ele chega mesmo vazio.
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	c, _, err := e.dial(t, info.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if typ, _, err := c.Read(ctx); err != nil || typ != websocket.MessageText {
		t.Fatalf("1ª = %v %v", typ, err)
	}
	if typ, data, err := c.Read(ctx); err != nil || typ != websocket.MessageBinary || len(data) != 0 {
		t.Fatalf("2ª = %v %q %v", typ, data, err)
	}
}

func TestCreateTerminalCardKeyFromResumeKey(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	if _, info := e.create(t, map[string]any{"resume_key": "NAC-12", "origin": "nobre run"}); info.CardKey != "NAC-12" {
		t.Fatalf("card_key = %q", info.CardKey)
	}
	if _, info := e.create(t, map[string]any{"resume_key": "NAC-12", "card_key": "NAC-3"}); info.CardKey != "NAC-3" {
		t.Fatalf("card_key explícito = %q", info.CardKey)
	}
}

func TestTerminalEnvDropsDaemonNobreVarsAndSetsColorterm(t *testing.T) {
	t.Setenv("NOBRE_RESUME", "NAC-99")
	t.Setenv("NOBRE_AGENT_PID", "4242")
	t.Setenv("COLORTERM", "")
	sh := writeScript(t, "#!/bin/sh\necho \"R=[$NOBRE_RESUME] P=[$NOBRE_AGENT_PID] C=[$COLORTERM] L=[$LANG]\"\n")
	e := newTermEnv(t, sh)
	run := func(body map[string]any) string {
		t.Helper()
		resp, info := e.create(t, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("code = %d", resp.StatusCode)
		}
		tm := mustTerm(t, e, info.ID)
		<-tm.Done()
		snap, _, cancel := tm.Subscribe()
		cancel()
		return string(snap)
	}
	t.Setenv("LANG", "C")
	if out := run(map[string]any{}); !strings.Contains(out, "R=[] P=[] C=[truecolor] L=[C]") {
		t.Fatalf("saída = %q", out)
	}
	out := run(map[string]any{"resume_key": "NAC-5", "env": map[string]string{"COLORTERM": "24bit", "LANG": "pt_BR.UTF-8"}})
	if !strings.Contains(out, "R=[NAC-5] P=[] C=[24bit] L=[pt_BR.UTF-8]") {
		t.Fatalf("saída = %q", out)
	}
}

// drainWS lê até o fechamento e devolve a saída binária concatenada e a
// última mensagem de texto.
func drainWS(t *testing.T, c *websocket.Conn) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out strings.Builder
	var lastText string
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("fechamento = %v", err)
			}
			return out.String(), lastText
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
		} else {
			lastText = string(data)
		}
	}
}

func TestTerminalWSBatchDeliversAllOutputThenExit(t *testing.T) {
	const exitMsg = `{"type":"exit","code":0,"reason":"agent"}`
	script := "printf um; sleep 0.15; printf dois; sleep 0.15; printf tres"
	var got [2]string
	for i, query := range []string{"", "?batch=1"} {
		e := newTermEnv(t, "/bin/sh")
		_, info := e.create(t, map[string]any{"args": []string{"-c", script}})
		c, _, err := e.dial(t, info.ID, query, nil)
		if err != nil {
			t.Fatal(err)
		}
		out, last := drainWS(t, c)
		if last != exitMsg {
			t.Fatalf("query %q: última mensagem = %q", query, last)
		}
		got[i] = out
	}
	if got[0] != "umdoistres" || got[1] != got[0] {
		t.Fatalf("sem batch = %q, com batch = %q", got[0], got[1])
	}
}

// drainWSCounts é como drainWS, mas também conta as mensagens binárias.
func drainWSCounts(t *testing.T, c *websocket.Conn) (out string, binMsgs int, last string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sb strings.Builder
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("fechamento = %v", err)
			}
			return sb.String(), binMsgs, last
		}
		if typ == websocket.MessageBinary {
			sb.Write(data)
			binMsgs++
		} else {
			last = string(data)
		}
	}
}

// Rajada sem pausas: quem assiste com ?batch=1 recebe os mesmos bytes em
// menos mensagens binárias, e o exit vem por último nos dois casos.
func TestTerminalWSBatchCoalescesBurst(t *testing.T) {
	const exitMsg = `{"type":"exit","code":0,"reason":"agent"}`
	// A pausa inicial dá tempo de o cliente assistir antes da rajada (sem
	// ela o snapshot entregaria tudo numa mensagem só).
	script := "sleep 0.5; i=0; while [ $i -lt 400 ]; do echo linha$i; i=$((i+1)); done"
	var outs [2]string
	var msgs [2]int
	for i, query := range []string{"", "?batch=1"} {
		e := newTermEnv(t, "/bin/sh")
		_, info := e.create(t, map[string]any{"args": []string{"-c", script}})
		c, _, err := e.dial(t, info.ID, query, nil)
		if err != nil {
			t.Fatal(err)
		}
		var last string
		outs[i], msgs[i], last = drainWSCounts(t, c)
		if last != exitMsg {
			t.Fatalf("query %q: última mensagem = %q", query, last)
		}
	}
	if !strings.Contains(outs[0], "linha399") || outs[1] != outs[0] {
		t.Fatalf("bytes diferem: sem batch %d, com batch %d", len(outs[0]), len(outs[1]))
	}
	if msgs[1] >= msgs[0] {
		t.Fatalf("mensagens binárias: sem batch %d, com batch %d (queria menos)", msgs[0], msgs[1])
	}
	t.Logf("mensagens binárias: sem batch %d, com batch %d", msgs[0], msgs[1])
}
