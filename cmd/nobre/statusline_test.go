package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const slJSON = `{"session_id":"s1","model":{"id":"x"},"rate_limits":{"five_hour":{"used_percentage":23.4,"resets_at":1790000000},"seven_day":{"used_percentage":41.6,"resets_at":1790500000}}}`

func TestStatuslinePassesBytesWithDaemonDown(t *testing.T) {
	in := "  {\"a\": \"ç\\u0026\"}\n\x00\xff bytes estranhos"
	r := runCLI(t, "127.0.0.1:1", in, nil, "statusline")
	if r.code != 0 || r.stdout != in || r.err != "" || r.dur > 400*time.Millisecond {
		t.Fatalf("%+v", r)
	}
	r = runCLI(t, "127.0.0.1:1", slJSON, nil, "statusline")
	if r.code != 0 || r.stdout != slJSON || r.err != "" || r.dur > 400*time.Millisecond {
		t.Fatalf("com rate_limits e daemon fora: %+v", r)
	}
}

func TestStatuslineHangingDaemonStaysUnder400ms(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(3 * time.Second) })
	r := runCLI(t, addr, slJSON, nil, "statusline")
	if r.code != 0 || r.stdout != slJSON || r.err != "" || r.dur > 400*time.Millisecond {
		t.Fatalf("%+v", r)
	}
}

func TestStatuslinePostsLimitsAfterWritingStdout(t *testing.T) {
	var got atomic.Value
	var outAtRequest atomic.Value
	var out bytes.Buffer
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.Store(r.Method + " " + r.URL.Path + " " + string(b))
		outAtRequest.Store(out.String())
		w.WriteHeader(204)
	})
	c := &cli{in: strings.NewReader(slJSON), out: &out, err: io.Discard,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return ""
		}}
	if code := c.run([]string{"statusline"}); code != 0 {
		t.Fatal(code)
	}
	want := `POST /api/plan-limits {"session_id":"s1","rate_limits":{"five_hour":{"used_percentage":23.4,"resets_at":1790000000},"seven_day":{"used_percentage":41.6,"resets_at":1790500000}}}`
	if got.Load() != want {
		t.Fatalf("POST = %v", got.Load())
	}
	if outAtRequest.Load() != slJSON {
		t.Fatalf("o stdout deveria estar escrito antes da rede: %q", outAtRequest.Load())
	}
}

func TestStatuslineNoRateLimitsNoPost(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { t.Error("não deveria postar") })
	for _, in := range []string{`{"session_id":"s"}`, `{"rate_limits":null}`, `não é json`, ``} {
		r := runCLI(t, addr, in, nil, "statusline")
		if r.code != 0 || r.stdout != in || r.err != "" {
			t.Fatalf("%q: %+v", in, r)
		}
	}
}

func TestStatuslineResumo(t *testing.T) {
	var posted atomic.Bool
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { posted.Store(true); w.WriteHeader(204) })
	r := runCLI(t, addr, slJSON, nil, "statusline", "--resumo")
	if r.code != 0 || r.stdout != "5h 23% · semana 42%\n" || r.err != "" || !posted.Load() {
		t.Fatalf("%+v posted=%v", r, posted.Load())
	}
	r = runCLI(t, addr, `{"rate_limits":{"seven_day":{"used_percentage":7}}}`, nil, "statusline", "--resumo")
	if r.stdout != "semana 7%\n" {
		t.Fatalf("só semana: %q", r.stdout)
	}
	for _, in := range []string{`{"rate_limits":{}}`, `{}`, `lixo`, `{"rate_limits":{"five_hour":{"used_percentage":150}}}`} {
		if r := runCLI(t, addr, in, nil, "statusline", "--resumo"); r.code != 0 || r.stdout != "" || r.err != "" {
			t.Fatalf("%q: %+v", in, r)
		}
	}
}

func TestStatuslineBiggerThanLimitIsStillPassedThrough(t *testing.T) {
	in := strings.Repeat("x", maxStatuslineIn+500)
	r := runCLI(t, "127.0.0.1:1", in, nil, "statusline")
	if r.code != 0 || r.stdout != in {
		t.Fatalf("len=%d code=%d", len(r.stdout), r.code)
	}
}
