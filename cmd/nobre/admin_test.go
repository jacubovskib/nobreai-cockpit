package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

// fakeWT é um git que sempre acha o mesmo repositório principal.
type fakeWT struct{ main string }

func (g fakeWT) Locate(dir string) gitinfo.Location {
	return gitinfo.Location{Root: dir, Main: g.main, Linked: dir != g.main}
}
func (g fakeWT) Worktrees(string) ([]gitinfo.Worktree, error) { return nil, nil }

func TestCLIProjectAddFromLinkedWorktree(t *testing.T) {
	var body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(201)
		w.Write([]byte(`{"name":"api","path":"/w/api","base_branch":"develop","reassigned":2}`))
	})
	var out, errb bytes.Buffer
	c := &cli{in: strings.NewReader(""), out: &out, err: &errb,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return ""
		},
		cwd: func() (string, error) { return "/w/api-feat", nil },
		git: fakeWT{main: "/w/api"}}
	if code := c.run([]string{"project", "add"}); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if body != `{"name":"","path":"/w/api"}` {
		t.Fatalf("body = %s", body)
	}
	if want := "Projeto api cadastrado: /w/api (base develop, 2 sessões religadas)\n"; out.String() != want {
		t.Fatalf("out = %q", out.String())
	}
}

func TestProjectAddResolvesCwd(t *testing.T) {
	var body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(201)
		w.Write([]byte(`{"name":"api","path":"/w/api"}`))
	})
	if r := runCLI(t, addr, "", nil, "project", "add", "--name", "api"); r.code != 0 || !strings.Contains(r.stdout, "/w/api") {
		t.Fatalf("%+v", r)
	}
	if body != `{"name":"api","path":"/w/api"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestPromptUseRendersWithCard(t *testing.T) {
	var in map[string]any
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/prompts/handoff/render" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&in)
		w.Write([]byte(`{"text":"Retome NAC-2"}`))
	})
	r := runCLI(t, addr, "", nil, "prompt", "use", "handoff", "--card", "NAC-2")
	if r.code != 0 || r.stdout != "Retome NAC-2\n" || in["card"] != "NAC-2" || in["use"] != true {
		t.Fatalf("%+v in=%v", r, in)
	}
}

func TestPromptSaveReadsStdin(t *testing.T) {
	var in map[string]any
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&in)
		w.WriteHeader(201)
		w.Write([]byte(`{"slug":"revisao-rapida"}`))
	})
	r := runCLI(t, addr, "corpo {{card.key}}\n", nil, "prompt", "save", "--name", "Revisão rápida", "--tag", "review")
	if r.code != 0 || in["body"] != "corpo {{card.key}}" || in["tag"] != "review" || in["slug"] != "revis-o-r-pida" && in["slug"] != "revisao-rapida" {
		t.Fatalf("%+v in=%v", r, in)
	}
}

func TestUIUsesInjectedOpener(t *testing.T) {
	var opened string
	var out strings.Builder
	c := &cli{out: &out, err: io.Discard, getenv: func(string) string { return "" },
		opener: func(u string) error { opened = u; return nil }}
	if code := c.run([]string{"ui"}); code != 0 || opened != "http://127.0.0.1:7420" {
		t.Fatalf("code=%d opened=%q", code, opened)
	}
}

func TestCLIProjectScanPrintsSections(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects":
			w.Write([]byte(`[{"id":3,"name":"api","path":"/w/api"}]`))
		case "/api/projects/3/scan":
			w.Write([]byte(`{"repo":"/w/api","base":"main",
			 "branches":[{"name":"feat/a","subject":"faz a","worktree":"/w/api-a","last_commit":"2026-11-01T00:00:00Z","ahead":3,"recent":true}],
			 "plans":[{"path":"docs/plan-x.md","open":2,"done":1,"modified":"2026-11-01T00:00:00Z"}],
			 "sessions":[{"id":"claude:1","started_at":"2026-11-01T00:00:00Z","ended_at":null,"branch":"feat/a","cwd":"/w/api","cost_usd":1.5}],
			 "cards":[{"key":"NAC-1","title":"Existente","column":"backlog","branch":""}]}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	})
	r := runCLI(t, addr, "", nil, "project", "scan", "/w/api")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{"Base: main", "Branches com trabalho fora da base", "feat/a (recente)", "3 à frente", "faz a", "/w/api-a",
		"Planos com tarefas abertas", "docs/plan-x.md", "Sessões recentes com custo", "Cards que já existem", "NAC-1"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("faltou %q em:\n%s", want, r.stdout)
		}
	}
}

// mapWT é um git falso: mapeia o topo de cada worktree para o checkout
// principal; Locate escolhe o topo mais profundo que contém o diretório.
type mapWT map[string]string

func (g mapWT) Locate(dir string) gitinfo.Location {
	best := ""
	for root := range g {
		if (dir == root || strings.HasPrefix(dir, root+"/")) && len(root) > len(best) {
			best = root
		}
	}
	if best == "" {
		return gitinfo.Location{}
	}
	return gitinfo.Location{Root: best, Main: g[best], Linked: best != g[best]}
}
func (g mapWT) Worktrees(string) ([]gitinfo.Worktree, error) { return nil, nil }

var testRepos = mapWT{
	"/w/api":                     "/w/api",
	"/w/api/.claude/worktrees/x": "/w/api",
	"/w/api-feat":                "/w/api",
}

// projectServer é o daemon falso com o projeto 3 em /w/api: grava o corpo do
// POST /api/projects (respondendo reply) e o caminho de cada requisição.
func projectServer(t *testing.T, reply string, body, paths *[]string) string {
	return fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*paths = append(*paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/projects":
			*body = append(*body, string(b))
			w.WriteHeader(201)
			w.Write([]byte(reply))
		case r.URL.Path == "/api/projects":
			w.Write([]byte(`[{"id":3,"name":"api","path":"/w/api"},{"id":4,"name":"outro","path":"/w/api-outro"}]`))
		case r.URL.Path == "/api/projects/3/scan":
			w.Write([]byte(`{"repo":"/w/api","base":"main","branches":[],"plans":[],"sessions":[],"cards":[]}`))
		case r.URL.Path == "/api/projects/3":
			w.Write([]byte(`{}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	})
}

func runProjectCLI(t *testing.T, addr, cwd string, git gitinfo.WorktreeReader, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	c := &cli{in: strings.NewReader(""), out: &out, err: &errb,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return ""
		},
		cwd: func() (string, error) { return cwd, nil },
		git: git}
	code := c.run(args)
	return result{code: code, stdout: out.String(), err: errb.String()}
}

func TestCLIProjectAddRefusesNonGit(t *testing.T) {
	var posts []string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts = append(posts, r.URL.Path)
		}
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"a pasta não é um repositório git nem tem repositórios dentro"}`))
	})
	r := runProjectCLI(t, addr, "/w/solto", testRepos, "project", "add")
	if r.code == 0 || !strings.Contains(r.err, "não é um repositório git") || len(posts) != 0 {
		t.Fatalf("%+v posts=%v", r, posts)
	}
}

// groupServer é o daemon falso de uma pasta de repositórios /w/grupo: a
// varredura acha a (novo), b (projeto solto), c (já filho, só no grupo já cadastrado) e d
// (bloqueado); o lote devolve os filhos pedidos. registered diz se o grupo já
// estava cadastrado.
func groupServer(t *testing.T, registered bool, batch *string) string {
	return fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/fs/scan":
			if r.URL.Query().Get("path") != "/w/grupo" {
				t.Errorf("path = %q", r.URL.Query().Get("path"))
			}
			self, c := "null", ""
			if registered {
				self = `{"id":1,"name":"grupo","path":"/w/grupo","kind":"group"}`
				c = `{"path":"/w/grupo/c","rel":"c","name":"c","source":"git","registered":{"id":9,"name":"c","parent_id":1},"blocked":""},`
			}
			w.Write([]byte(`{"path":"/w/grupo","git":false,"kind":"group","name":"grupo","registered":` + self + `,"suggestions":[
				{"path":"/w/grupo/a","rel":"a","name":"a","source":"git","registered":null,"blocked":""},
				{"path":"/w/grupo/b","rel":"b","name":"b","source":"git","registered":{"id":8,"name":"b"},"blocked":""},
				` + c + `
				{"path":"/w/grupo/d","rel":"d","name":"d","source":"git","registered":{"id":7,"name":"d","parent_id":5},"blocked":"pertence a outro"}]}`))
		case r.URL.Path == "/api/projects/batch" && r.Method == "POST":
			b, _ := io.ReadAll(r.Body)
			*batch = string(b)
			var in struct {
				Children []map[string]string `json:"children"`
			}
			json.Unmarshal(b, &in)
			for _, c := range in.Children {
				if c["name"] == "" { // sem nome vale o da pasta
					c["name"] = filepath.Base(c["path"])
				}
			}
			kids, _ := json.Marshal(in.Children)
			if registered {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(201)
			}
			w.Write([]byte(`{"project":{"id":1,"name":"grupo","path":"/w/grupo","kind":"group"},"children":` + string(kids) + `,"moved":{"sessions":0,"cards":0}}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	})
}

// nobre project add numa pasta de repositórios cadastra o grupo e liga os
// repositórios que a varredura acha (os novos e os projetos soltos; o que é de
// outro projeto fica de fora).
func TestProjectAddGroupFolderRegistersRepos(t *testing.T) {
	var batch string
	r := runProjectCLI(t, groupServer(t, false, &batch), "/w/grupo", testRepos, "project", "add")
	want := "Grupo grupo cadastrado: /w/grupo\n" +
		"Repositórios ligados ao grupo: a, b\n" +
		"Fora do grupo: d (pertence a outro)\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("%+v\nquer %q", r, want)
	}
	// Sem nome: é o daemon que escolhe (o da pasta ou, se já existe, outro).
	if !strings.Contains(batch, `"children":[{"name":"","path":"/w/grupo/a"},{"name":"","path":"/w/grupo/b"}]`) {
		t.Fatalf("batch = %s", batch)
	}
}

// De novo num grupo já cadastrado: só os repositórios novos entram.
func TestProjectAddGroupFolderAgainAddsOnlyNewRepos(t *testing.T) {
	var batch string
	r := runProjectCLI(t, groupServer(t, true, &batch), "/w/grupo", testRepos, "project", "add")
	want := "Grupo grupo já cadastrado: /w/grupo\n" +
		"Repositórios ligados ao grupo: a, b\n" +
		"Fora do grupo: d (pertence a outro)\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("%+v\nquer %q", r, want)
	}
}

// --sem-filhos cadastra só o grupo e lista os repositórios como sugestão.
func TestProjectAddGroupFolderSemFilhos(t *testing.T) {
	var batch string
	r := runProjectCLI(t, groupServer(t, false, &batch), "/w/grupo", testRepos, "project", "add", "--sem-filhos")
	want := "Grupo grupo cadastrado: /w/grupo\n" +
		"Sub-projetos sugeridos (liste ao usuário, não cadastre sem ele pedir):\n" +
		"  a  (git)\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("%+v\nquer %q", r, want)
	}
	if !strings.Contains(batch, `"children":[]`) {
		t.Fatalf("batch = %s", batch)
	}
}

// Nome repetido não impede o cadastro do grupo: a saída diz o nome que o
// daemon deu ao repositório e cita o que ficou de fora (o projeto solto cujo
// nome já existia no grupo).
func TestProjectAddGroupFolderSaysRenamedAndSkipped(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/scan":
			w.Write([]byte(`{"path":"/w/grupo","git":false,"kind":"group","name":"grupo","registered":null,"suggestions":[
				{"path":"/w/grupo/a/api","rel":"a/api","name":"api","source":"git","registered":null,"blocked":""},
				{"path":"/w/grupo/b/api","rel":"b/api","name":"api","source":"git","registered":null,"blocked":""},
				{"path":"/w/grupo/grupo","rel":"grupo","name":"grupo","source":"git","registered":null,"blocked":""},
				{"path":"/w/grupo/solto","rel":"solto","name":"solto","source":"git","registered":{"id":8,"name":"API"},"blocked":""}]}`))
		case "/api/projects/batch":
			w.WriteHeader(201)
			w.Write([]byte(`{"project":{"id":1,"name":"grupo","path":"/w/grupo","kind":"group"},"children":[
				{"id":2,"name":"api","path":"/w/grupo/a/api"},{"id":3,"name":"b/api","path":"/w/grupo/b/api"},
				{"id":4,"name":"grupo-2","path":"/w/grupo/grupo"}],"moved":{"sessions":0,"cards":0}}`))
		}
	})
	r := runProjectCLI(t, addr, "/w/grupo", testRepos, "project", "add")
	want := "Grupo grupo cadastrado: /w/grupo\n" +
		"Repositórios ligados ao grupo: a/api, b/api (como b/api), grupo (como grupo-2)\n" +
		"Não ligados, já existe um projeto com esse nome no grupo: solto\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("%+v\nquer %q", r, want)
	}
}

// nobre project add <repo> com --name que já existe no grupo da pasta: o
// projeto nasce solto e a saída diz que não foi ligado.
func TestCLIProjectAddInsideGroupSaysNotAttached(t *testing.T) {
	var body, paths []string
	addr := projectServer(t, `{"id":8,"name":"api","path":"/w/api","not_attached":"Bravonix","reassigned":0}`, &body, &paths)
	r := runProjectCLI(t, addr, "/w/api", testRepos, "project", "add", "--name", "api")
	if r.code != 0 || !strings.Contains(r.stdout, "Não ligado ao grupo Bravonix: já existe um projeto com esse nome nele.\n") {
		t.Fatalf("%+v", r)
	}
}

// nobre project add <repo> sem --pai dentro de um grupo: a saída diz que o
// repositório foi ligado ao grupo.
func TestCLIProjectAddInsideGroupSaysAttached(t *testing.T) {
	var body, paths []string
	addr := projectServer(t, `{"id":8,"name":"api","path":"/w/api","parent_id":7,"attached_to":"Bravonix","reassigned":0}`, &body, &paths)
	r := runProjectCLI(t, addr, "/w/api", testRepos, "project", "add")
	if r.code != 0 || !strings.Contains(r.stdout, "Projeto api cadastrado: /w/api") || !strings.Contains(r.stdout, "Ligado ao grupo Bravonix.\n") {
		t.Fatalf("%+v", r)
	}
}

func TestCLIProjectAddResolvesLikeTheHook(t *testing.T) {
	for _, c := range []struct{ cwd, arg, want string }{
		{"/w/api", "packages/api", "/w/api/packages/api"},       // subpasta fica subpasta
		{"/w/api/packages", "", "/w/api/packages"},              // idem pelo cwd
		{"/w/api-feat", "", "/w/api"},                           // worktree ligado externo
		{"/w/api-feat/packages/api", "", "/w/api/packages/api"}, // mesma subpasta no principal
		{"/w/api/.claude/worktrees/x", "", "/w/api"},            // worktree aninhado
	} {
		var body, paths []string
		addr := projectServer(t, `{"name":"api","path":"`+c.want+`"}`, &body, &paths)
		args := []string{"project", "add"}
		if c.arg != "" {
			args = append(args, c.arg)
		}
		r := runProjectCLI(t, addr, c.cwd, testRepos, args...)
		if r.code != 0 || len(body) != 1 || body[0] != `{"name":"","path":"`+c.want+`"}` {
			t.Fatalf("cwd %s arg %q: %+v body=%v", c.cwd, c.arg, r, body)
		}
	}
}

func TestCLIProjectAddOutput(t *testing.T) {
	for _, c := range []struct{ reply, want string }{
		{`{"name":"api","path":"/w/api","reassigned":1}`, "Projeto api cadastrado: /w/api (1 sessão religada)\n"},
		{`{"name":"api","path":"/w/api","base_branch":"main","reassigned":0}`, "Projeto api cadastrado: /w/api (base main, 0 sessões religadas)\n"},
		{`{"name":"api","path":"/w/api","base_branch":"main","reassigned":3}`, "Projeto api cadastrado: /w/api (base main, 3 sessões religadas)\n"},
	} {
		var body, paths []string
		addr := projectServer(t, c.reply, &body, &paths)
		if r := runProjectCLI(t, addr, "/w/api", testRepos, "project", "add"); r.code != 0 || r.stdout != c.want {
			t.Fatalf("%+v, quer %q", r, c.want)
		}
	}
}

// scan e base acham o projeto de uma subpasta e de um worktree ligado, como o hook.
func TestCLIProjectScanAndBaseFindProjectFromSubdirAndWorktree(t *testing.T) {
	for _, cwd := range []string{"/w/api/internal/api", "/w/api-feat", "/w/api-feat/internal", "/w/api/.claude/worktrees/x"} {
		var body, paths []string
		addr := projectServer(t, `{}`, &body, &paths)
		if r := runProjectCLI(t, addr, cwd, testRepos, "project", "scan"); r.code != 0 || !strings.Contains(r.stdout, "Projeto: api") {
			t.Fatalf("scan em %s: %+v", cwd, r)
		}
		if r := runProjectCLI(t, addr, cwd, testRepos, "project", "base", "develop"); r.code != 0 || r.stdout != "Base do projeto api: develop\n" {
			t.Fatalf("base em %s: %+v", cwd, r)
		}
		if !slices.Contains(paths, "PATCH /api/projects/3") {
			t.Fatalf("base em %s não chamou o projeto 3: %v", cwd, paths)
		}
	}
}

func TestProjectScanPrintsSuggestions(t *testing.T) {
	var dismissed []string
	var order []string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		order = append(order, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/projects":
			w.Write([]byte(`[{"id":3,"name":"api","path":"/w/api"}]`))
		case r.URL.Path == "/api/projects/3/suggestions/dismiss":
			b, _ := io.ReadAll(r.Body)
			dismissed = append(dismissed, string(b))
			w.WriteHeader(204)
		case r.URL.Path == "/api/projects/3/scan":
			w.Write([]byte(`{"repo":"/w/api","base":"main","branches":[],"plans":[],"sessions":[],"cards":[],
			 "suggestions":[{"path":"/w/api/apps/web","rel":"apps/web","name":"web","source":"pnpm-workspace.yaml"}]}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	})
	r := runCLI(t, addr, "", nil, "project", "scan", "/w/api")
	if r.code != 0 || !strings.Contains(r.stdout, "Sub-projetos sugeridos (1)") || !strings.Contains(r.stdout, "apps/web") || len(dismissed) != 0 {
		t.Fatalf("%+v", r)
	}
	r = runCLI(t, addr, "", nil, "project", "scan", "/w/api", "--nao-sugerir", "apps/web, apps/api")
	if r.code != 0 || len(dismissed) != 1 || dismissed[0] != `{"rels":["apps/web","apps/api"]}` {
		t.Fatalf("%+v dismissed=%q", r, dismissed)
	}
}

func TestProjectAddListsAtMostFiveSuggestions(t *testing.T) {
	var sg []string
	for i := 0; i < 7; i++ {
		sg = append(sg, fmt.Sprintf(`{"rel":"apps/a%d","name":"a%d","source":"go.work"}`, i, i))
	}
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte(`{"id":3,"name":"api","path":"/w/api","suggestions":[` + strings.Join(sg, ",") + `]}`))
	})
	r := runCLI(t, addr, "", nil, "project", "add")
	if r.code != 0 || !strings.Contains(r.stdout, "Sub-projetos sugeridos") || !strings.Contains(r.stdout, "apps/a4") ||
		strings.Contains(r.stdout, "apps/a5") || !strings.Contains(r.stdout, "mais 2 na Configuração → Projetos") {
		t.Fatalf("%+v", r)
	}
}

func TestCLIProjectPaiWarnsCapture(t *testing.T) {
	var patched string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			w.Write([]byte(`[{"id":1,"name":"loja","path":"/r/loja","capture":true},{"id":2,"name":"web","path":"/r/loja/web","capture":false}]`))
		case r.Method == "PATCH" && r.URL.Path == "/api/projects/2":
			b, _ := io.ReadAll(r.Body)
			patched = string(b)
			w.Write([]byte(`{"id":2,"moved":{"sessions":3,"cards":1}}`))
		default:
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
	})
	r := runProjectCLI(t, addr, "/r", testRepos, "project", "pai", "2", "1")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if patched != `{"parent_id":1}` {
		t.Fatalf("corpo = %s", patched)
	}
	for _, want := range []string{
		"3 sessões e 1 card passaram para loja › web.",
		"A captura de web passa a seguir a de loja (ligada).",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("faltou %q em %q", want, r.stdout)
		}
	}
	if r := runProjectCLI(t, addr, "/r", testRepos, "project", "pai", "2", "--nenhum"); r.code != 0 || patched != `{"parent_id":null}` {
		t.Fatalf("--nenhum: %+v corpo=%s", r, patched)
	}
}

func TestCLIProjectGrupoAndAddPai(t *testing.T) {
	var bodies []string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/projects":
			bodies = append(bodies, string(b))
			w.WriteHeader(201)
			w.Write([]byte(`{"id":7,"name":"loja","path":"","moved":{"sessions":2,"cards":0}}`))
		case r.URL.Path == "/api/projects":
			w.Write([]byte(`[{"id":7,"name":"loja","path":"","capture":true,"kind":"group"}]`))
		default:
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
	})
	if r := runProjectCLI(t, addr, "/r", testRepos, "project", "grupo", "loja"); r.code != 0 || !strings.Contains(r.stdout, "Grupo loja cadastrado (id 7)") {
		t.Fatalf("grupo: %+v", r)
	}
	r := runProjectCLI(t, addr, "/w/api", testRepos, "project", "add", "--pai", "loja")
	if r.code != 0 || !strings.Contains(r.stdout, "2 sessões e 0 cards passaram para") {
		t.Fatalf("add --pai: %+v", r)
	}
	if len(bodies) != 2 || bodies[0] != `{"name":"loja"}` || !strings.Contains(bodies[1], `"parent_id":7`) {
		t.Fatalf("corpos = %v", bodies)
	}
}

func TestCLIProjectBaseOnAppPrintsServerRefusal(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			w.Write([]byte(`[{"id":1,"name":"loja","path":"/r/loja","capture":true},{"id":2,"name":"web","path":"/r/loja/web","capture":true,"parent_id":1}]`))
		case r.Method == "PATCH" && r.URL.Path == "/api/projects/2":
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"a base de um app é a do projeto principal loja"}`))
		default:
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
	})
	r := runProjectCLI(t, addr, "/r/loja/web", testRepos, "project", "base", "develop")
	if r.code == 0 || !strings.Contains(r.err, "a base de um app é a do projeto principal loja") || strings.Contains(r.stdout, "Base do projeto") {
		t.Fatalf("%+v", r)
	}
}

func TestCLIProjectPaiUnknownNumericIDIsNotAPath(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/projects" {
			w.Write([]byte(`[{"id":1,"name":"loja","path":"/r/loja","capture":true}]`))
			return
		}
		t.Errorf("%s %s", r.Method, r.URL.Path)
	})
	r := runProjectCLI(t, addr, "/r", testRepos, "project", "pai", "99", "1")
	if r.code == 0 || !strings.Contains(r.err, "projeto 99 não encontrado") {
		t.Fatalf("%+v", r)
	}
}

// --pai pelo nome acha o grupo com pasta (o kind manda, não a pasta vazia).
func TestCLIProjectAddPaiByNameFindsGroupWithFolder(t *testing.T) {
	var bodies []string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/projects":
			bodies = append(bodies, string(b))
			w.WriteHeader(201)
			w.Write([]byte(`{"id":8,"name":"api","path":"/w/api","moved":{"sessions":0,"cards":0}}`))
		case r.URL.Path == "/api/projects":
			w.Write([]byte(`[{"id":7,"name":"bravonix","path":"/w/bravonix","capture":true,"kind":"group"}]`))
		default:
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
	})
	r := runProjectCLI(t, addr, "/w/api", testRepos, "project", "add", "--pai", "bravonix")
	if r.code != 0 || len(bodies) != 1 || !strings.Contains(bodies[0], `"parent_id":7`) {
		t.Fatalf("add --pai: %+v corpos=%v", r, bodies)
	}
}

// Recusa da varredura por outro motivo (a home, fora da home): a CLI mostra a
// mensagem do daemon, não "não é um repositório git".
func TestProjectAddGroupFolderPrintsServerRefusal(t *testing.T) {
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"pasta fora da home do usuário"}`))
	})
	r := runProjectCLI(t, addr, "/w/solto", testRepos, "project", "add")
	if r.code == 0 || !strings.Contains(r.err, "pasta fora da home do usuário") || strings.Contains(r.err, "não é um repositório git") {
		t.Fatalf("%+v", r)
	}
}
