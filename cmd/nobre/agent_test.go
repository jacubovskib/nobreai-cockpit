package main

import (
	"encoding/json"
	"io"
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
		{[]string{"done"}, "/api/me/done", `{"force":false}`},
		{[]string{"done", "--force"}, "/api/me/done", `{"force":true}`},
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

func TestDashLeadingTextIsNotAFlag(t *testing.T) {
	var last string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		last = r.URL.Path + " " + string(bs)
		w.Write([]byte(`{"key":"NAC-1"}`))
	})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"note", "- falta testar"}, `/api/me/note {"text":"- falta testar"}`},
		{[]string{"note", "--", "--x e -y"}, `/api/me/note {"text":"--x e -y"}`},
		{[]string{"note", "--", "-a"}, `/api/me/note {"text":"-a"}`},
		{[]string{"decide", "-x", "--why", "y"}, `/api/me/decide {"what":"-x","why":"y"}`},
		{[]string{"decide", "--why", "y", "--", "-x"}, `/api/me/decide {"what":"-x","why":"y"}`},
		{[]string{"decide", "-x", "--why=y"}, `/api/me/decide {"what":"-x","why":"y"}`},
		{[]string{"start", "-t", "--goal", "g"}, `/api/me/start {"done_when":"","goal":"g","title":"-t"}`},
		{[]string{"start", "--goal", "g", "-t"}, `/api/me/start {"done_when":"","goal":"g","title":"-t"}`},
		{[]string{"file", "-weird.go", "--why", "w"}, `/api/me/file {"lines":"","path":"-weird.go","why":"w"}`},
	}
	for _, c := range cases {
		last = ""
		if r := runCLI(t, addr, "", nil, c.args...); r.code != 0 || last != c.want {
			t.Fatalf("%v: %+v last=%q", c.args, r, last)
		}
	}
	// flag desconhecida com "--" é erro de uso; nada é enviado.
	last = ""
	if r := runCLI(t, addr, "", nil, "decide", "x", "--wy", "y"); r.code != 2 || last != "" || !strings.Contains(r.err, "--wy") {
		t.Fatalf("%+v", r)
	}
	if r := runCLI(t, addr, "", nil, "decide", "x", "--why"); r.code != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestHelpFlagPrintsCommandUsage(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("API não deveria ser chamada") })
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"start", "-h"}, "nobre start"},
		{[]string{"note", "--help"}, "nobre note"},
		{[]string{"decide", "x", "-h"}, "nobre decide"},
		{[]string{"todo", "--help"}, "nobre todo add"},
		{[]string{"setup", "-h"}, "nobre setup"},
	} {
		r := runCLI(t, addr, "", nil, c.args...)
		if r.code != 0 || !strings.Contains(r.stdout, "Uso:") || !strings.Contains(r.stdout, c.want) || strings.Contains(r.stdout, "nobre ui") {
			t.Fatalf("%v: %+v", c.args, r)
		}
	}
}

func TestOnlyLeadingDoubleDashConsumedInSimple(t *testing.T) {
	var last string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		last = string(bs)
		w.Write([]byte(`{}`))
	})
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"note", "--", "-- x"}, `{"text":"-- x"}`},
		{[]string{"note", "antes", "--", "depois"}, `{"text":"antes -- depois"}`},
		{[]string{"note", "--", "-h"}, `{"text":"-h"}`},
	} {
		if r := runCLI(t, addr, "", nil, c.args...); r.code != 0 || last != c.want {
			t.Fatalf("%v: %+v last=%q", c.args, r, last)
		}
	}
}

func TestCLIAnswer(t *testing.T) {
	var path, body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		path, body = r.URL.Path, string(bs)
		w.Write([]byte(`{"id":3,"answer":"cookie"}`))
	})
	r := runCLI(t, addr, "", nil, "answer", "3", "cookie")
	if r.code != 0 || path != "/api/me/answer" || body != `{"answer":"cookie","id":3}` || strings.TrimSpace(r.stdout) != "Resposta registrada." {
		t.Fatalf("%+v path=%s body=%s", r, path, body)
	}
}

func TestCLIAnswerUsage(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { t.Error("não deveria chamar a API") })
	for _, args := range [][]string{{"answer", "abc", "x"}, {"answer", "3"}} {
		if r := runCLI(t, addr, "", nil, args...); r.code != 2 {
			t.Fatalf("%v: %+v", args, r)
		}
	}
}

func TestCLIAskPrintsID(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte(`{"id":7,"text":"como?"}`))
	})
	if r := runCLI(t, addr, "", nil, "ask", "como?"); r.code != 0 || strings.TrimSpace(r.stdout) != "Pergunta 7 registrada." {
		t.Fatalf("%+v", r)
	}
}

func TestStatusListsOpenQuestions(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"session":{},"card":{"key":"NAC-7","title":"L","column":"em_andamento","readiness":{"met":0,"total":0,"items":[]},
		"content":{"questions":[{"id":5,"text":"qual?","answer":""},{"id":6,"text":"feita","answer":"ok"}]}}}`))
	})
	r := runCLI(t, addr, "", nil, "status")
	if !strings.Contains(r.stdout, "Perguntas em aberto:\n- [5] qual?") || strings.Contains(r.stdout, "feita") {
		t.Fatalf("%s", r.stdout)
	}
}

func TestCLIDoneForce(t *testing.T) {
	var body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Write([]byte(`{}`))
	})
	if r := runCLI(t, addr, "", nil, "done", "--force"); r.code != 0 || strings.TrimSpace(body) != `{"force":true}` {
		t.Fatalf("%+v body=%q", r, body)
	}
}
