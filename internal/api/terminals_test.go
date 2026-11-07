package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// binAdapter é um agente falso cujo binário é um executável do sistema.
type binAdapter struct {
	fakeAdapter
	bin string
}

func (b *binAdapter) Binary() string { return b.bin }

type termEnv struct {
	srv *httptest.Server
	mgr *pty.Manager
	hub *Hub
}

func newTermEnv(t *testing.T, bin string) *termEnv {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	mgr := pty.NewManager(pty.Options{})
	t.Cleanup(mgr.Close)
	srv := httptest.NewServer(New(st, Deps{Hub: hub, Terminals: mgr,
		Adapters: map[string]adapter.Adapter{"claude": &binAdapter{bin: bin}}}))
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
	if last != `T:{"type":"exit","code":3}` || !strings.Contains(joined, "fim") {
		t.Fatalf("seq = %q", seq)
	}
	if strings.Index(joined, "fim") > strings.Index(joined, `"exit"`) {
		t.Fatalf("exit antes da saída final: %q", seq)
	}
}

func TestTerminalWSRejectsForeignOrigin(t *testing.T) {
	e := newTermEnv(t, "/bin/cat")
	_, info := e.create(t, map[string]any{})
	for _, origin := range []string{"http://evil.example", "http://127.0.0.1:1", "https://127.0.0.1:" + portOf(e.srv.URL), "null"} {
		_, resp, err := e.dial(t, info.ID, "", http.Header{"Origin": {origin}})
		if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: err=%v resp=%v", origin, err, resp)
		}
	}
	for _, origin := range []string{"http://127.0.0.1:" + portOf(e.srv.URL), "http://localhost:" + portOf(e.srv.URL)} {
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
		!strings.Contains(texts[0], `"type":"info"`) || texts[1] != `{"type":"exit","code":2}` {
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
		Adapters: map[string]adapter.Adapter{"claude": &binAdapter{bin: "/bin/cat"}}}))
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
