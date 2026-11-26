package main

import (
	"encoding/json"
	"errors"
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
		{[]string{"start", "Novo", "--goal", "g", "--done-when", "d"}, "/api/me/start", `{"cwd":"/w/api","done_when":"d","goal":"g","title":"Novo"}`},
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
		{[]string{"start", "-t", "--goal", "g"}, `/api/me/start {"cwd":"/w/api","done_when":"","goal":"g","title":"-t"}`},
		{[]string{"start", "--goal", "g", "-t"}, `/api/me/start {"cwd":"/w/api","done_when":"","goal":"g","title":"-t"}`},
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

func TestCLIBranchAndProjectBase(t *testing.T) {
	type seen struct{ method, path, body string }
	var calls []seen
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		calls = append(calls, seen{r.Method, r.URL.Path, string(bs)})
		if r.Method == "GET" {
			w.Write([]byte(`[{"id":1,"name":"outro","path":"/w/outro"},{"id":7,"name":"api","path":"/w/api"}]`))
			return
		}
		w.Write([]byte(`{"key":"NAC-1"}`))
	})
	if r := runCLI(t, addr, "", nil, "branch", "feat/x"); r.code != 0 || calls[0] != (seen{"POST", "/api/me/branch", `{"branch":"feat/x"}`}) {
		t.Fatalf("%+v %+v", r, calls)
	}
	calls = nil
	r := runCLI(t, addr, "", nil, "project", "base", "develop", "/w/api")
	if r.code != 0 || len(calls) != 2 || calls[1] != (seen{"PATCH", "/api/projects/7", `{"base_branch":"develop"}`}) {
		t.Fatalf("%+v %+v", r, calls)
	}
	if r := runCLI(t, addr, "", nil, "project", "base", "develop", "/w/nada"); r.code != 1 || !strings.Contains(r.err, "não cadastrado") {
		t.Fatalf("%+v", r)
	}
	if r := runCLI(t, addr, "", nil, "branch"); r.code != 2 {
		t.Fatalf("uso: %+v", r)
	}
}

func TestCLICardAdd(t *testing.T) {
	var body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/cards" {
			t.Errorf("path = %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(201)
		w.Write([]byte(`{"key":"NAC-2","column":"pausado"}`))
	})
	r := runCLI(t, addr, "", nil, "card", "add", "B", "--column", "pausado", "--where", "x")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	want := `{"branch":"","column":"pausado","cwd":"/w/api","done_when":"","goal":"","title":"B","where_stopped":"x"}`
	if body != want {
		t.Fatalf("body = %s", body)
	}
	if r.stdout != "Card NAC-2 criado no pausado (sessão não ligada).\n" {
		t.Fatalf("out = %q", r.stdout)
	}
}

func TestCLIBlock(t *testing.T) {
	var path, body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		path, body = r.URL.Path, string(bs)
		w.Write([]byte(`{"key":"NAC-9","blocked_by":"NAC-2"}`))
	})
	r := runCLI(t, addr, "", nil, "block", "NAC-2", "--why", "x")
	if r.code != 0 || path != "/api/me/block" || body != `{"key":"NAC-2","why":"x"}` || strings.TrimSpace(r.stdout) != "Card NAC-9 bloqueado por NAC-2." {
		t.Fatalf("%+v path=%s body=%s", r, path, body)
	}
	r = runCLI(t, addr, "", nil, "block", "--clear")
	if r.code != 0 || body != `{"key":""}` || strings.TrimSpace(r.stdout) != "Bloqueio removido." {
		t.Fatalf("%+v body=%s", r, body)
	}
	if r := runCLI(t, addr, "", nil, "block"); r.code != 2 {
		t.Fatalf("sem argumento: %+v", r)
	}
}

// I1: o 409 do daemon (card fora de Em andamento) chega ao agente; nada de
// "Card marcado como pronto".
func TestCLIDoneRefusedNotInProgress(t *testing.T) {
	msg := "o card está em Revisão; nobre done só move cards em andamento"
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	})
	r := runCLI(t, addr, "", nil, "done")
	if r.code != 1 || r.stdout != "" || strings.TrimSpace(r.err) != msg {
		t.Fatalf("%+v", r)
	}
}

func TestCLIApp(t *testing.T) {
	var body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte(`[{"id":1,"name":"loja","path":"/w/loja"},{"id":2,"name":"web","path":"/w/loja/web","parent_id":1}]`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = strings.TrimSpace(string(b))
		w.Write([]byte(`{"key":"NAC-1","project_id":2}`))
	})
	r := runCLI(t, addr, "", nil, "app", "web")
	if r.code != 0 || r.stdout != "Card NAC-1 agora em loja › web.\n" || body != `{"app":"web","geral":false}` {
		t.Fatalf("%+v body=%q", r, body)
	}
	if r := runCLI(t, addr, "", nil, "app", "--geral"); r.code != 0 || body != `{"app":"","geral":true}` {
		t.Fatalf("%+v body=%q", r, body)
	}
	if r := runCLI(t, addr, "", nil, "app"); r.code != 2 {
		t.Fatalf("uso: %+v", r)
	}
}

func TestCLILembrar(t *testing.T) {
	var last, body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		bs, _ := io.ReadAll(r.Body)
		last, body = r.Method+" "+r.URL.Path, string(bs)
		w.Write([]byte(`{"id":7,"scope":"raiz","similar":[{"id":3,"rule":"regra parecida"}]}`))
	})
	r := runCLI(t, addr, "", nil, "lembrar", "usar", "rtk", "--faca", "rtk git status", "--cat", "shell", "--escopo", "raiz")
	if r.code != 0 || last != "POST /api/me/remember" {
		t.Fatalf("%+v last=%s", r, last)
	}
	var got map[string]string
	json.Unmarshal([]byte(body), &got)
	if got["rule"] != "usar rtk" || got["do"] != "rtk git status" || got["category"] != "shell" || got["scope"] != "raiz" {
		t.Fatalf("body = %s", body)
	}
	if r.stdout != "Memória #7 gravada (raiz).\nParecida com #3: regra parecida\n" {
		t.Fatalf("stdout = %q", r.stdout)
	}
	runCLI(t, addr, "", nil, "lembrar", "só regra", "--faca", "algo")
	json.Unmarshal([]byte(body), &got)
	if got["category"] != "dominio" || got["scope"] != "projeto" || got["do"] != "algo" {
		t.Fatalf("padrões = %s", body)
	}
	last = ""
	for _, args := range [][]string{{"lembrar"}, {"lembrar", "sem ação"}, {"lembrar", "sem ação", "--faca", "  "}} {
		if r := runCLI(t, addr, "", nil, args...); r.code != 2 || !strings.Contains(r.err, `uso: nobre lembrar "<regra>" --faca`) || last != "" {
			t.Fatalf("%v: %+v last=%s", args, r, last)
		}
	}
}

func TestCLILembrarSemParecida(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":2,"status":"ativa","scope":"raiz","similar":[]}`))
	})
	if r := runCLI(t, addr, "", nil, "lembrar", "x", "--faca", "y", "--escopo", "raiz"); r.stdout != "Memória #2 gravada (raiz).\n" {
		t.Fatalf("%+v", r)
	}
}

func TestCLILembrarGlobalVaiParaAprovacao(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":9,"status":"candidata","scope":"global","similar":[]}`))
	})
	if r := runCLI(t, addr, "", nil, "lembrar", "x", "--faca", "y", "--escopo", "global"); r.stdout != "Memória global #9 enviada para aprovação na página Memórias.\n" {
		t.Fatalf("%+v", r)
	}
}

func TestCLIMemorias(t *testing.T) {
	var query string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(`[{"id":4,"category":"shell","rule":"usar rtk","do":"rtk git","scope":"raiz"},{"id":5,"category":"dominio","rule":"sem ação","do":"","scope":"global"}]`))
	})
	r := runCLI(t, addr, "", nil, "memorias", "rtk")
	want := "#4 [shell] (raiz) usar rtk → rtk git\n#5 [dominio] (global) sem ação\n"
	if r.code != 0 || r.stdout != want || query != "q=rtk" {
		t.Fatalf("%+v query=%q", r, query)
	}
	addr = fake(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
	if r := runCLI(t, addr, "", nil, "memorias"); r.stdout != "Nenhuma memória.\n" {
		t.Fatalf("%+v", r)
	}
	if r := runCLI(t, addr, "", nil, "lembrar", "--help"); !strings.Contains(r.stdout, "--faca") {
		t.Fatalf("help = %q", r.stdout)
	}
}

func TestCLIPlan(t *testing.T) {
	var path, body string
	calls := 0
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		path, body = r.URL.Path, string(bs)
		w.Write([]byte(`{"path":"docs/p.md","tasks":[{"n":1,"title":"A","status":"a_fazer"},{"n":2,"title":"B c","status":"em_revisao"}],"n":2,"status":"em_revisao"}`))
	})
	r := runCLI(t, addr, "", nil, "plan", "docs/p.md", "--task", "A", "--task", "B c")
	if r.code != 0 || path != "/api/me/plan" || body != `{"path":"docs/p.md","tasks":["A","B c"]}` ||
		!strings.Contains(r.stdout, "Plano registrado: 2 tarefas.") {
		t.Fatalf("%+v path=%s body=%s", r, path, body)
	}
	calls = 0
	if r := runCLI(t, addr, "", nil, "plan", "docs/p.md"); r.code != 2 || r.err == "" || calls != 0 {
		t.Fatalf("sem --task: %+v calls=%d", r, calls)
	}
	r = runCLI(t, addr, "", nil, "task", "2", "revisao")
	if r.code != 0 || path != "/api/me/task" || body != `{"n":2,"status":"revisao"}` {
		t.Fatalf("task: %+v path=%s body=%s", r, path, body)
	}
	if r := runCLI(t, addr, "", nil, "task", "x", "revisao"); r.code != 2 || r.err == "" {
		t.Fatalf("task x: %+v", r)
	}
}

func TestStatusListsPlan(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"session":{},"card":{"key":"NAC-7","title":"Login","column":"em_andamento",
		"readiness":{"met":3,"total":3,"items":[]},
		"content":{"plan":{"path":"docs/p.md","tasks":[{"n":1,"title":"A","status":"concluida"},{"n":2,"title":"B","status":"em_andamento"}]}}}}`))
	})
	r := runCLI(t, addr, "", nil, "status")
	for _, want := range []string{"Plano:", "docs/p.md · 1 de 2 concluídas", "- 2. B — em andamento"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("falta %q em:\n%s", want, r.stdout)
		}
	}
}

func TestStartAndCardFlagsSendHierarchyFields(t *testing.T) {
	var path, body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bs, _ := json.Marshal(b)
		path, body = r.URL.Path, string(bs)
		w.Write([]byte(`{"key":"NAC-2","type":"historia","priority":"alta","parent_key":"NAC-1"}`))
	})
	cases := []struct {
		args       []string
		path, body string
	}{
		{[]string{"start", "x", "--type", "historia", "--epic", "NAC-1", "--priority", "alta", "--label", "front", "--label", "api", "--points", "2"},
			"/api/me/start", `{"cwd":"/w/api","done_when":"","goal":"","labels":["front","api"],"parent_key":"NAC-1","points":2,"priority":"alta","title":"x","type":"historia"}`},
		{[]string{"card", "add", "y", "--type", "bug", "--points", "0.5"},
			"/api/me/cards", `{"branch":"","column":"","cwd":"/w/api","done_when":"","goal":"","points":0.5,"title":"y","type":"bug","where_stopped":""}`},
		{[]string{"card", "set", "NAC-2", "--no-epic"}, "/api/me/card-set", `{"key":"NAC-2","parent_key":""}`},
		{[]string{"card", "set", "--no-points"}, "/api/me/card-set", `{"points":null}`},
		{[]string{"card", "set", "--no-labels"}, "/api/me/card-set", `{"labels":[]}`},
		{[]string{"card", "set", "--priority", "baixa", "--label", "x"}, "/api/me/card-set", `{"labels":["x"],"priority":"baixa"}`},
	}
	for _, c := range cases {
		r := runCLI(t, addr, "", nil, c.args...)
		if r.code != 0 || path != c.path || body != c.body {
			t.Fatalf("%v: %+v\npath=%s body=%s", c.args, r, path, body)
		}
	}
}

func TestCardSetOutputAndUsage(t *testing.T) {
	resp := ""
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(resp)) })
	for _, c := range []struct{ resp, want string }{
		{`{"key":"NAC-2","type":"historia","priority":"alta","parent_key":"NAC-1"}`, "Card NAC-2 atualizado: História · Alta · épico NAC-1.\n"},
		{`{"key":"NAC-3","type":"bug","priority":"media","parent_key":""}`, "Card NAC-3 atualizado: Bug.\n"},
	} {
		resp = c.resp
		if r := runCLI(t, addr, "", nil, "card", "set", "NAC-2", "--type", "bug"); r.code != 0 || r.stdout != c.want {
			t.Fatalf("%+v, quer %q", r, c.want)
		}
	}
	for _, args := range [][]string{
		{"card", "set"}, {"card", "set", "NAC-1", "--epic", "NAC-2", "--no-epic"}, {"card", "set", "--points", "1", "--no-points"},
		{"card", "set", "--label", "a", "--no-labels"}, {"card", "set", "--points", "NaN"}, {"card", "set", "--points", "Inf"}, {"card", "set", "--points", "-1"},
	} {
		if r := runCLI(t, "127.0.0.1:1", "", nil, args...); r.code != 2 || r.stdout != "" {
			t.Fatalf("%v: %+v", args, r)
		}
	}
}

func TestStatusOmitsKindLineForPlainTask(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"session":{},"card":{"key":"NAC-7","title":"T","type":"tarefa","priority":"media","readiness":{"met":0,"total":0}}}`))
	})
	if r := runCLI(t, addr, "", nil, "status"); strings.Contains(r.stdout, "Tipo:") {
		t.Fatalf("linha Tipo inesperada:\n%s", r.stdout)
	}
}

func TestStatusShowsKindLine(t *testing.T) {
	card := ""
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"session":{},"card":{"key":"NAC-7","title":"T",` + card + `"readiness":{"met":0,"total":0}}}`))
	})
	for _, c := range []struct{ card, want string }{
		{`"type":"historia","priority":"alta","parent_key":"NAC-1","parent_title":"Checkout novo",`, "Tipo: História · Prioridade: Alta · Épico: NAC-1 · Checkout novo\n"},
		{`"type":"tarefa","priority":"alta",`, "Tipo: Tarefa · Prioridade: Alta\n"},
		{`"type":"epico","priority":"media","children":{"total":7,"done":4},`, "Tipo: Épico · Filhas: 4/7 concluídas\n"},
	} {
		card = c.card
		if r := runCLI(t, addr, "", nil, "status"); !strings.Contains(r.stdout, c.want) {
			t.Fatalf("falta %q em:\n%s", c.want, r.stdout)
		}
	}
}

// nobre start e nobre card add enviam a pasta onde o comando rodou (o daemon
// escolhe o projeto por ela); se a pasta não pode ser lida, o campo não vai e
// vale a regra da sessão.
func TestStartAndCardAddSendCommandFolder(t *testing.T) {
	var got map[string]any
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"key":"NAC-2"}`))
	})
	run := func(cwd func() (string, error), args ...string) {
		t.Helper()
		var out, errb strings.Builder
		c := &cli{in: strings.NewReader(""), out: &out, err: &errb, cwd: cwd,
			getenv: func(k string) string {
				if k == "NOBRE_ADDR" {
					return addr
				}
				return ""
			}}
		if code := c.run(args); code != 0 {
			t.Fatalf("%v: code %d %s", args, code, errb.String())
		}
	}
	t.Setenv("NOBRE_AGENT_PID", "77")
	in := func() (string, error) { return "/w/bravonix/turing-back", nil }
	for _, args := range [][]string{{"start", "x"}, {"card", "add", "y"}} {
		run(in, args...)
		if got["cwd"] != "/w/bravonix/turing-back" {
			t.Fatalf("%v: cwd = %v", args, got["cwd"])
		}
		run(func() (string, error) { return "", errors.New("pasta removida") }, args...)
		if _, ok := got["cwd"]; ok {
			t.Fatalf("%v: cwd enviado sem pasta: %v", args, got)
		}
	}
	// Os demais comandos da sessão não dependem da pasta e não a enviam.
	run(in, "note", "parei")
	if _, ok := got["cwd"]; ok {
		t.Fatalf("note enviou cwd: %v", got)
	}
}

// nobre start e nobre card add dizem o projeto quando ele não é o da sessão e
// avisam, numa linha, do repositório que o comando ligou ao grupo.
func TestStartAndCardAddSayProjectAndLinkedRepo(t *testing.T) {
	reply := `{"key":"NAC-2","column":"backlog"}`
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(reply)) })
	if r := runCLI(t, addr, "", nil, "start", "x"); r.stdout != "Card NAC-2 criado e ligado a esta sessão.\n" {
		t.Fatalf("mesmo projeto: %q", r.stdout)
	}
	reply = `{"key":"NAC-2","column":"backlog","project_label":"Bravonix › turing-back"}`
	if r := runCLI(t, addr, "", nil, "start", "x"); r.stdout != "Card NAC-2 criado em Bravonix › turing-back e ligado a esta sessão.\n" {
		t.Fatalf("outro projeto: %q", r.stdout)
	}
	if r := runCLI(t, addr, "", nil, "card", "add", "x"); r.stdout != "Card NAC-2 criado em Bravonix › turing-back, no backlog (sessão não ligada).\n" {
		t.Fatalf("card add: %q", r.stdout)
	}
	reply = `{"key":"NAC-2","column":"backlog","project_label":"Bravonix › turing-front","linked_repo":"turing-front","linked_group":"Bravonix"}`
	want := "Repositório turing-front cadastrado no grupo Bravonix.\nCard NAC-2 criado em Bravonix › turing-front e ligado a esta sessão.\n"
	if r := runCLI(t, addr, "", nil, "start", "x"); r.stdout != want {
		t.Fatalf("ligado: %q", r.stdout)
	}
}
