package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

type result struct {
	code        int
	stdout, err string
	dur         time.Duration
}

func runCLI(t *testing.T, addr, stdin string, env map[string]string, args ...string) result {
	t.Helper()
	return runCLIReader(t, addr, strings.NewReader(stdin), env, args...)
}

func runCLIReader(t *testing.T, addr string, in io.Reader, env map[string]string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	c := &cli{in: in, out: &out, err: &errb,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return env[k]
		},
		cwd:    func() (string, error) { return "/w/api", nil },
		opener: func(string) error { t.Fatal("opener não deveria rodar"); return nil }}
	t.Setenv("NOBRE_AGENT_PID", "77")
	start := time.Now()
	code := c.run(args)
	return result{code, out.String(), errb.String(), time.Since(start)}
}

func fake(t *testing.T, h http.HandlerFunc) string {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestHookFailOpenWhenDaemonDown(t *testing.T) {
	r := runCLI(t, "127.0.0.1:1", `{"a":1}`, nil, "hook", "claude", "Stop")
	if r.code != 0 || r.stdout != "" || r.err != "" || r.dur > 2500*time.Millisecond {
		t.Fatalf("%+v", r)
	}
}

func TestHookPassesStdoutAndExit(t *testing.T) {
	var call adapter.HookCall
	addr := fake(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/hooks/claude" {
			t.Errorf("path = %s", req.URL.Path)
		}
		json.NewDecoder(req.Body).Decode(&call)
		w.Write([]byte(`{"stdout":"X","exit":0}`))
	})
	r := runCLI(t, addr, `{"a":1}`, map[string]string{"NOBRE_RESUME": "NAC-3"}, "hook", "claude", "SessionStart")
	if r.code != 0 || r.stdout != "X" || r.err != "" {
		t.Fatalf("%+v", r)
	}
	if call.Event != "SessionStart" || string(call.Payload) != `{"a":1}` || call.AgentPID != 77 || call.Env["NOBRE_RESUME"] != "NAC-3" {
		t.Fatalf("call = %+v", call)
	}
	r = runCLI(t, fake(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"stdout":"bloqueado\n","exit":2}`)) }),
		"{}", nil, "hook", "claude", "Stop")
	if r.code != 2 || r.stdout != "bloqueado\n" {
		t.Fatalf("%+v", r)
	}
}

func TestHookFailOpenOnBadReplies(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"400": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400); w.Write([]byte(`{"error":"x"}`)) },
		"404": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) },
		"413": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(413) },
		"500": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte(`{"stdout":"X","exit":2}`))
		},
		"badjson": func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`nao json`)) },
		"slow":    func(w http.ResponseWriter, _ *http.Request) { time.Sleep(3 * time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			r := runCLI(t, fake(t, h), `{}`, nil, "hook", "claude", "Stop")
			if r.code != 0 || r.stdout != "" || r.err != "" || r.dur > 2500*time.Millisecond {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestHookInvalidStdinAndNeverStderr(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"stdout":"X","exit":0}`)) })
	for _, in := range []string{"", "nao json"} {
		if r := runCLI(t, addr, in, nil, "hook", "claude", "Stop"); r.code != 0 || r.stdout != "" || r.err != "" {
			t.Fatalf("stdin %q: %+v", in, r)
		}
	}
	if r := runCLI(t, addr, "{}", nil, "hook", "claude"); r.code != 0 || r.stdout != "" || r.err != "" {
		t.Fatalf("%+v", r)
	}
}

func TestHookDeadlineHoldsWithStdinThatNeverCloses(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	go pw.Write([]byte(`{"a":`)) // escreve o começo e nunca fecha
	r := runCLIReader(t, "127.0.0.1:1", pr, nil, "hook", "claude", "Stop")
	if r.code != 0 || r.stdout != "" || r.dur > 2500*time.Millisecond {
		t.Fatalf("%+v", r)
	}
}

func TestHookLargeStdinWithinBudget(t *testing.T) {
	big := `{"x":"` + strings.Repeat("a", 31<<20) + `"}`
	var got int
	addr := fake(t, func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		got = len(b)
		w.Write([]byte(`{"stdout":"ok","exit":0}`))
	})
	r := runCLI(t, addr, big, nil, "hook", "claude", "PostToolUse")
	if r.stdout != "ok" || got < len(big) || r.dur > 2*time.Second {
		t.Fatalf("stdout=%q got=%d dur=%v", r.stdout, got, r.dur)
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("leitura quebrada") }

func TestHookRecoversFromPanic(t *testing.T) {
	r := runCLIReader(t, "127.0.0.1:1", panicReader{}, nil, "hook", "claude", "Stop")
	if r.code != 0 || r.stdout != "" || r.err != "" {
		t.Fatalf("%+v", r)
	}
	// Pânico fora da goroutine de leitura (getenv nil) também sai com 0.
	var out bytes.Buffer
	c := &cli{in: strings.NewReader("{}"), out: &out, err: &out}
	if code := c.hook([]string{"claude", "Stop"}); code != 0 || out.Len() != 0 {
		t.Fatalf("code = %d, saída %q", code, out.String())
	}
}

// O driver que bloqueia com código de saída (Kiro) manda o motivo no stderr.
func TestHookPassesStderr(t *testing.T) {
	r := runCLI(t, fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"stdout":"","stderr":"limite atingido","exit":2}`))
	}), "{}", nil, "hook", "kiro", "PreToolUse")
	if r.code != 2 || r.stdout != "" || r.err != "limite atingido" {
		t.Fatalf("%+v", r)
	}
}
