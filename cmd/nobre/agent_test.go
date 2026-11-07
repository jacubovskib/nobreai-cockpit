package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAgentCommandsCallAPI(t *testing.T) {
	type seen struct{ method, path, body, pid string }
	var last seen
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		last = seen{r.Method, r.URL.Path, string(bs), r.Header.Get("X-Nobre-Agent-PID")}
		w.Write([]byte(`{"key":"NAC-1","markdown":"# Pacote"}`))
	})
	cases := []struct {
		args       []string
		path, body string
	}{
		{[]string{"note", "parei", "aqui"}, "/api/me/note", `{"text":"parei aqui"}`},
		{[]string{"decide", "PKCE", "--why", "seguro"}, "/api/me/decide", `{"what":"PKCE","why":"seguro"}`},
		{[]string{"file", "a.go", "--why", "núcleo", "--lines", "L1-9"}, "/api/me/file", `{"lines":"L1-9","path":"a.go","why":"núcleo"}`},
		{[]string{"todo", "add", "falta x"}, "/api/me/todo", `{"text":"falta x"}`},
		{[]string{"todo", "done", "5"}, "/api/me/todo-done", `{"id":5}`},
		{[]string{"ask", "como?"}, "/api/me/ask", `{"text":"como?"}`},
		{[]string{"test", "rode go test"}, "/api/me/test", `{"text":"rode go test"}`},
		{[]string{"done"}, "/api/me/done", `{}`},
		{[]string{"start", "Novo", "--goal", "g", "--done-when", "d"}, "/api/me/start", `{"done_when":"d","goal":"g","title":"Novo"}`},
		{[]string{"resume", "NAC-1", "--force"}, "/api/me/resume", `{"force":true,"key":"NAC-1"}`},
	}
	for _, c := range cases {
		r := runCLI(t, addr, "", nil, c.args...)
		if r.code != 0 || last.path != c.path || last.body != c.body || last.pid != "77" {
			t.Fatalf("%v: %+v last=%+v", c.args, r, last)
		}
	}
	if r := runCLI(t, addr, "", nil, "resume", "NAC-1"); !strings.Contains(r.stdout, "# Pacote") {
		t.Fatalf("%+v", r)
	}
}

func TestAPIErrorPrintedToStderrExit1(t *testing.T) {
	msg := `Esta sessão ainda não tem card. Use nobre start "<título>" ou nobre resume <KEY>.`
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	})
	r := runCLI(t, addr, "", nil, "note", "x")
	if r.code != 1 || r.stdout != "" || strings.TrimSpace(r.err) != msg {
		t.Fatalf("%+v", r)
	}
}

func TestStatusPrintsCardAndReadiness(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"session":{},"card":{"key":"NAC-7","title":"Login","column":"em_andamento","where_stopped":"no callback",
		"readiness":{"met":1,"total":3,"items":[{"label":"Objetivo","ok":true},{"label":"Como testar preenchido","ok":false},{"label":"Arquivo essencial","ok":false}]}}}`))
	})
	r := runCLI(t, addr, "", nil, "status")
	for _, want := range []string{"NAC-7", "Login", "em_andamento", "Onde parei: no callback", "1/3", "Como testar preenchido", "Arquivo essencial"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("falta %q em:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "Objetivo") {
		t.Fatalf("item cumprido listado:\n%s", r.stdout)
	}
	addr = fake(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"session":{},"card":null}`)) })
	if r := runCLI(t, addr, "", nil, "status"); r.code != 1 || !strings.Contains(r.err, "ainda não tem card") {
		t.Fatalf("%+v", r)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"note"}, {"resume"}, {"todo", "done", "x"}, {"file", "a.go"}, {"bogus"}} {
		if r := runCLI(t, "127.0.0.1:1", "", nil, args...); r.code != 2 || r.stdout != "" || r.err == "" {
			t.Fatalf("%v: %+v", args, r)
		}
	}
}
