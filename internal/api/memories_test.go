package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

type memJSON struct {
	ID          int64    `json:"id"`
	ProjectID   *int64   `json:"project_id"`
	Category    string   `json:"category"`
	Rule        string   `json:"rule"`
	Do          string   `json:"do"`
	Status      string   `json:"status"`
	Source      string   `json:"source"`
	SourceRef   string   `json:"source_ref"`
	NeedsReview bool     `json:"needs_review"`
	Inherited   bool     `json:"inherited"`
	Scope       string   `json:"scope"`
	Similar     []simRow `json:"similar"`
}

type simRow struct {
	ID   int64  `json:"id"`
	Rule string `json:"rule"`
}

// memSession cria uma sessão viva ligada ao projeto (sem card).
func (a *testAPI) memSession(t *testing.T, id string, pid int, projectID int64) {
	t.Helper()
	if err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", PID: pid, Cwd: "/w", ProjectID: &projectID, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
}

func (a *testAPI) mem(t *testing.T, projectID *int64, rule, status string) domain.Memory {
	t.Helper()
	m, err := a.st.CreateMemory(domain.Memory{ProjectID: projectID, Category: "shell", Rule: rule, Do: "faça", Status: status, Source: "usuario"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMemoriesCRUDAndValidation(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	ch, cancel := a.hub.Subscribe()
	defer cancel()

	rec := a.do(t, "POST", "/api/memories", map[string]any{"project_id": p.ID, "category": "shell", "rule": "Usar rtk", "do": "rtk git status"}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	m := decode[memJSON](t, rec)
	if m.Source != "usuario" || m.Status != "ativa" || m.ID == 0 {
		t.Fatalf("m = %+v", m)
	}
	select {
	case k := <-ch:
		if k != "memories" {
			t.Fatalf("publicou %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("não publicou memories")
	}
	for _, body := range []map[string]any{
		{"rule": "  ", "category": "shell"},
		{"rule": "x", "category": "inexistente"},
		{"rule": "x", "category": "shell", "project_id": 999},
		{"rule": strings.Repeat("a", 301), "category": "shell"},
	} {
		if rec := a.do(t, "POST", "/api/memories", body, 0); rec.Code != http.StatusBadRequest {
			t.Fatalf("%v = %d %s", body, rec.Code, rec.Body)
		}
	}

	path := fmt.Sprintf("/api/memories/%d", m.ID)
	rec = a.do(t, "PATCH", path, map[string]any{"rule": "Usar rtk sempre", "project_id": nil}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	if got := decode[memJSON](t, rec); got.Rule != "Usar rtk sempre" || got.ProjectID != nil || got.Source != "usuario" {
		t.Fatalf("patch = %+v", got)
	}
	if rec := a.do(t, "PATCH", "/api/memories/9999", map[string]any{"rule": "x"}, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH 404 = %d", rec.Code)
	}
	if rec := a.do(t, "PATCH", path, map[string]any{"rule": ""}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH vazio = %d", rec.Code)
	}
	if rec := a.do(t, "PATCH", "/api/memories/abc", map[string]any{"rule": "x"}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH id ruim = %d", rec.Code)
	}

	// Excluir só arquivada, e excluir não apaga: vira descartada.
	if rec := a.do(t, "DELETE", path, nil, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE ativa = %d %s", rec.Code, rec.Body)
	}
	a.do(t, "PATCH", path, map[string]any{"status": "arquivada"}, 0)
	if rec := a.do(t, "DELETE", path, nil, 0); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
	}
	if got, err := a.st.GetMemory(m.ID); err != nil || got.Status != "descartada" {
		t.Fatalf("lápide = %+v %v", got, err)
	}
	if rec := a.do(t, "DELETE", path, nil, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE de descartada = %d", rec.Code)
	}
	if rec := a.do(t, "DELETE", "/api/memories/9999", nil, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE 404 = %d", rec.Code)
	}
	// Ativa exige o faça assim.
	if rec := a.do(t, "POST", "/api/memories", map[string]any{"rule": "sem ação", "category": "shell"}, 0); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "falta o faça assim") {
		t.Fatalf("sem ação = %d %s", rec.Code, rec.Body)
	}
}

func TestMemoriesMove(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	m1, m2 := a.mem(t, &p.ID, "um", "ativa"), a.mem(t, &p.ID, "dois", "ativa")
	if rec := a.do(t, "POST", fmt.Sprintf("/api/memories/%d/move", m2.ID), map[string]int{"dir": -1}, 0); rec.Code != http.StatusOK {
		t.Fatalf("move = %d %s", rec.Code, rec.Body)
	}
	list := decode[[]memJSON](t, a.do(t, "GET", fmt.Sprintf("/api/memories?project=%d&exact=1", p.ID), nil, 0))
	if len(list) != 2 || list[0].ID != m2.ID || list[1].ID != m1.ID {
		t.Fatalf("list = %+v", list)
	}
	for _, c := range []struct {
		path string
		body any
		code int
	}{
		{fmt.Sprintf("/api/memories/%d/move", m1.ID), map[string]int{"dir": 0}, 400},
		{fmt.Sprintf("/api/memories/%d/move", m1.ID), map[string]int{"dir": 2}, 400},
		{"/api/memories/9999/move", map[string]int{"dir": 1}, 404},
	} {
		if rec := a.do(t, "POST", c.path, c.body, 0); rec.Code != c.code {
			t.Fatalf("%s %v = %d", c.path, c.body, rec.Code)
		}
	}
}

func TestListMemoriesScopes(t *testing.T) {
	a := newTestAPI(t)
	root := a.project(t)
	app, _, err := a.st.CreateChild("web", "/w/api/web", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := a.st.CreateProject("outro", "/w/outro")
	a.mem(t, &app.ID, "do app", "ativa")
	a.mem(t, &root.ID, "da raiz", "ativa")
	a.mem(t, nil, "global", "ativa")
	a.mem(t, &other.ID, "de outro", "ativa")
	a.mem(t, &root.ID, "candidata raiz", "candidata")
	a.mem(t, &root.ID, "arquivada raiz", "arquivada")
	d := a.mem(t, &root.ID, "descartada raiz", "ativa")
	a.do(t, "POST", "/api/memories/batch", map[string]any{"ids": []int64{d.ID}, "action": "descartar"}, 0)

	byRule := func(path string) map[string]memJSON {
		rec := a.do(t, "GET", path, nil, 0)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body)
		}
		out := map[string]memJSON{}
		for _, m := range decode[[]memJSON](t, rec) {
			out[m.Rule] = m
		}
		return out
	}
	all := byRule("/api/memories")
	if len(all) != 6 || all["descartada raiz"].ID != 0 || all["de outro"].Inherited {
		t.Fatalf("all = %v", all)
	}
	got := byRule(fmt.Sprintf("/api/memories?project=%d", app.ID))
	if len(got) != 5 || got["do app"].Inherited || !got["da raiz"].Inherited || !got["global"].Inherited || got["de outro"].ID != 0 {
		t.Fatalf("app = %v", got)
	}
	got = byRule(fmt.Sprintf("/api/memories?project=%d&exact=1", app.ID))
	if len(got) != 1 || got["do app"].ID == 0 || got["do app"].Inherited {
		t.Fatalf("exact = %v", got)
	}
	got = byRule(fmt.Sprintf("/api/memories?project=%d&status=candidata", root.ID))
	if len(got) != 1 || got["candidata raiz"].ID == 0 {
		t.Fatalf("status = %v", got)
	}
	got = byRule("/api/memories?status=descartada")
	if len(got) != 1 || got["descartada raiz"].ID == 0 {
		t.Fatalf("descartada = %v", got)
	}
	for path, code := range map[string]int{"/api/memories?project=x": 400, "/api/memories?project=999": 404} {
		if rec := a.do(t, "GET", path, nil, 0); rec.Code != code {
			t.Fatalf("%s = %d", path, rec.Code)
		}
	}
}

func TestBatchMemories(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	ok := a.mem(t, &p.ID, "curta", "candidata")
	long, err := a.st.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: strings.Repeat("a", 350), Do: "x", Status: "candidata", Source: "napkin"})
	if err != nil || !long.NeedsReview {
		t.Fatalf("long = %+v %v", long, err)
	}
	noDo, _ := a.st.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: "sem ação", Status: "candidata", Source: "card"})
	rec := a.do(t, "POST", "/api/memories/batch", map[string]any{"ids": []int64{ok.ID, long.ID, noDo.ID, 9999}, "action": "aprovar"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch = %d %s", rec.Code, rec.Body)
	}
	res := decode[struct {
		Done    []int64 `json:"done"`
		Refused []struct {
			ID    int64  `json:"id"`
			Error string `json:"error"`
		} `json:"refused"`
	}](t, rec)
	if len(res.Done) != 1 || res.Done[0] != ok.ID || len(res.Refused) != 3 ||
		res.Refused[0].ID != long.ID || !strings.Contains(res.Refused[0].Error, "300") ||
		res.Refused[1].ID != noDo.ID || res.Refused[1].Error != "falta o faça assim: edite antes de aprovar" || res.Refused[2].ID != 9999 {
		t.Fatalf("res = %+v", res)
	}
	if m, _ := a.st.GetMemory(ok.ID); m.Status != "ativa" {
		t.Fatalf("m = %+v", m)
	}
	if rec := a.do(t, "POST", "/api/memories/batch", map[string]any{"ids": []int64{ok.ID}, "action": "arquivar"}, 0); rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	if m, _ := a.st.GetMemory(ok.ID); m.Status != "arquivada" {
		t.Fatalf("m = %+v", m)
	}
	a.do(t, "POST", "/api/memories/batch", map[string]any{"ids": []int64{long.ID}, "action": "descartar"}, 0)
	if m, _ := a.st.GetMemory(long.ID); m.Status != "descartada" {
		t.Fatalf("m = %+v", m)
	}
	for _, body := range []map[string]any{{"ids": []int64{1}, "action": "x"}, {"ids": []int64{}, "action": "aprovar"}} {
		if rec := a.do(t, "POST", "/api/memories/batch", body, 0); rec.Code != 400 {
			t.Fatalf("%v = %d", body, rec.Code)
		}
	}
}

func TestPatchApproveOverLimitIs400(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	long, _ := a.st.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: strings.Repeat("b", 350), Status: "candidata", Source: "napkin"})
	if rec := a.do(t, "PATCH", fmt.Sprintf("/api/memories/%d", long.ID), map[string]any{"status": "ativa"}, 0); rec.Code != 400 || !strings.Contains(rec.Body.String(), "300") {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
}

func TestPreviewRespectsBudget(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	for i := 0; i < 5; i++ {
		a.mem(t, &p.ID, fmt.Sprintf("regra numero %d com texto", i), "ativa")
	}
	path := fmt.Sprintf("/api/memories/preview?project=%d", p.ID)
	type prev struct {
		Block  string `json:"block"`
		Tokens int    `json:"tokens"`
		Chosen int    `json:"chosen"`
		Left   int    `json:"left"`
	}
	got := decode[prev](t, a.do(t, "GET", path, nil, 0))
	if got.Chosen != 5 || got.Left != 0 || !strings.Contains(got.Block, "regra numero 0") || got.Tokens <= 0 {
		t.Fatalf("got = %+v", got)
	}
	st, _ := a.st.GetSettings()
	st.MemoryBudgetTokens = 20
	if err := a.st.PutSettings(st); err != nil {
		t.Fatal(err)
	}
	got = decode[prev](t, a.do(t, "GET", path, nil, 0))
	if got.Chosen+got.Left != 5 || got.Left == 0 || got.Tokens > 20 || !strings.Contains(got.Block, "fora do limite") {
		t.Fatalf("limitado = %+v", got)
	}
	st.MemoryBudgetTokens = 0
	a.st.PutSettings(st)
	if got = decode[prev](t, a.do(t, "GET", path, nil, 0)); got.Block != "" || got.Chosen != 0 {
		t.Fatalf("desligado = %+v", got)
	}
	for p, code := range map[string]int{"/api/memories/preview": 400, "/api/memories/preview?project=999": 404} {
		if rec := a.do(t, "GET", p, nil, 0); rec.Code != code {
			t.Fatalf("%s = %d", p, rec.Code)
		}
	}
}

func TestRememberScopesAndSimilar(t *testing.T) {
	a := newTestAPI(t)
	root := a.project(t)
	app, _, _ := a.st.CreateChild("web", "/w/api/web", root.ID)
	a.memSession(t, "claude:app", 4242, app.ID)

	rec := a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "rodar go vet antes do commit", "do": "go vet", "category": "shell", "scope": "raiz"}, 4242)
	if rec.Code != http.StatusCreated {
		t.Fatalf("remember = %d %s", rec.Code, rec.Body)
	}
	m := decode[memJSON](t, rec)
	if m.ProjectID == nil || *m.ProjectID != root.ID || m.Source != "agente" || m.Status != "ativa" || len(m.Similar) != 0 {
		t.Fatalf("m = %+v", m)
	}
	rec = a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "Rodar go vet antes de commit", "do": "go vet", "category": "shell", "scope": "projeto"}, 4242)
	m2 := decode[memJSON](t, rec)
	if m2.ProjectID == nil || *m2.ProjectID != app.ID || len(m2.Similar) != 1 || m2.Similar[0].ID != m.ID || m2.Similar[0].Rule != "rodar go vet antes do commit" {
		t.Fatalf("m2 = %+v", m2)
	}
	// Global do agente vai para a fila de aprovação.
	rec = a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "algo global bem distinto", "do": "x", "scope": "global"}, 4242)
	if g := decode[memJSON](t, rec); g.ProjectID != nil || g.Category != "dominio" || g.Status != "candidata" || g.Scope != "global" {
		t.Fatalf("g = %+v", g)
	}
	// Raiz sobre sessão que já está na raiz: o próprio projeto.
	a.memSession(t, "claude:root", 4343, root.ID)
	rec = a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "outra da raiz", "do": "x", "scope": "raiz"}, 4343)
	if r := decode[memJSON](t, rec); r.ProjectID == nil || *r.ProjectID != root.ID {
		t.Fatalf("r = %+v", r)
	}
}

func TestRememberErrors(t *testing.T) {
	a := newTestAPI(t)
	if rec := a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "x"}, 9999); rec.Code != 404 {
		t.Fatalf("sem sessão = %d", rec.Code)
	}
	if err := a.st.StartSession(domain.Session{ID: "claude:solta", Agent: "claude", PID: 5050, Cwd: "/fora", StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		body map[string]string
		msg  string
	}{
		{map[string]string{"rule": "x", "scope": "projeto"}, "sessão sem projeto"},
		{map[string]string{"rule": "x", "scope": "raiz"}, "sessão sem projeto"},
		{map[string]string{"rule": " ", "scope": "global"}, "regra"},
		{map[string]string{"rule": "x", "scope": "lua"}, "escopo"},
		{map[string]string{"rule": "x", "scope": "global", "category": "zzz"}, "categoria"},
	} {
		rec := a.do(t, "POST", "/api/me/remember", c.body, 5050)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), c.msg) {
			t.Fatalf("%v = %d %s", c.body, rec.Code, rec.Body)
		}
	}
	if rec := a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "global sem projeto", "scope": "global"}, 5050); rec.Code != 201 {
		t.Fatalf("global = %d", rec.Code)
	}
	// Sessão com card usa o projeto do card.
	c := a.card(t)
	a.liveSession(t, "claude:card", 6060, c.Key)
	rec := a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "do card", "do": "x", "scope": "projeto"}, 6060)
	if m := decode[memJSON](t, rec); m.ProjectID == nil || *m.ProjectID != c.ProjectID {
		t.Fatalf("m = %+v", m)
	}
}

func TestMeMemoriesFiltersByTerm(t *testing.T) {
	a := newTestAPI(t)
	root := a.project(t)
	app, _, _ := a.st.CreateChild("web", "/w/api/web", root.ID)
	a.memSession(t, "claude:app", 4242, app.ID)
	a.mem(t, &app.ID, "regra do app", "ativa")
	a.mem(t, &root.ID, "regra da raiz com Ação", "ativa")
	a.mem(t, nil, "regra global", "ativa")
	a.mem(t, &root.ID, "candidata fora", "candidata")
	list := decode[[]memJSON](t, a.do(t, "GET", "/api/me/memories", nil, 4242))
	if len(list) != 3 || list[0].Scope != "app" || list[1].Scope != "raiz" || list[2].Scope != "global" {
		t.Fatalf("list = %+v", list)
	}
	list = decode[[]memJSON](t, a.do(t, "GET", "/api/me/memories?q=RAIZ", nil, 4242))
	if len(list) != 1 || list[0].Rule != "regra da raiz com Ação" {
		t.Fatalf("q = %+v", list)
	}
	if rec := a.do(t, "GET", "/api/me/memories", nil, 9999); rec.Code != 404 {
		t.Fatalf("sem sessão = %d", rec.Code)
	}
}

func writeNapkin(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "napkin.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportNapkins(t *testing.T) {
	a := newTestAPI(t)
	d1, d2, d3 := t.TempDir(), t.TempDir(), t.TempDir()
	p1, _ := a.st.CreateProject("um", d1)
	p2, _ := a.st.CreateProject("dois", d2)
	a.st.CreateProject("sem-napkin", d3)
	a.st.CreateProject("grupo", "") // sem pasta: ignorado
	long := strings.Repeat("palavra ", 60)
	writeNapkin(t, d1, "## Shell & Commands\n1. **[2026-01-01] Rodar go vet antes do commit**\n   Do instead: go vet ./...\n"+
		"2. **[2026-01-02] "+long+"**\n   Do instead: ok\n3. **[2026-01-03] Só do um**\n   Do instead: x\n")
	writeNapkin(t, d2, "## Shell & Commands\n1. **[2026-02-01] rodar GO VET antes do commit**\n   Do instead: vet\n")
	before, _ := os.ReadFile(filepath.Join(d1, ".claude", "napkin.md"))

	rec := a.do(t, "POST", "/api/memories/import", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d %s", rec.Code, rec.Body)
	}
	res := decode[struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
		Merged  int `json:"merged"`
		Files   []struct {
			ProjectID int64  `json:"project_id"`
			Path      string `json:"path"`
			Items     int    `json:"items"`
		} `json:"files"`
	}](t, rec)
	if res.Created != 3 || res.Skipped != 0 || res.Merged != 2 || len(res.Files) != 2 || res.Files[0].ProjectID != p1.ID || res.Files[0].Items != 3 || res.Files[1].ProjectID != p2.ID {
		t.Fatalf("res = %+v", res)
	}
	list := decode[[]memJSON](t, a.do(t, "GET", "/api/memories?status=candidata", nil, 0))
	if len(list) != 3 {
		t.Fatalf("list = %+v", list)
	}
	var globals, longOnes int
	for _, m := range list {
		if m.Source != "napkin" {
			t.Fatalf("m = %+v", m)
		}
		if m.ProjectID == nil {
			globals++
			if m.Rule != "Rodar go vet antes do commit" || !strings.Contains(m.SourceRef, filepath.Join(d1, ".claude", "napkin.md")) || !strings.Contains(m.SourceRef, filepath.Join(d2, ".claude", "napkin.md")) {
				t.Fatalf("global = %+v", m)
			}
			// A data do item do napkin vira a criação (meia-noite local).
			if g, _ := a.st.GetMemory(m.ID); !g.CreatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)) {
				t.Fatalf("created_at = %v", g.CreatedAt)
			}
		}
		if len([]rune(m.Rule)) > 300 {
			longOnes++
			if !m.NeedsReview || strings.Contains(m.Rule, "…") {
				t.Fatalf("longa = %+v", m)
			}
		}
	}
	if globals != 1 || longOnes != 1 {
		t.Fatalf("globals=%d long=%d", globals, longOnes)
	}
	// Reimportar não cria nada.
	res2 := decode[struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
	}](t, a.do(t, "POST", "/api/memories/import", nil, 0))
	if res2.Created != 0 || res2.Skipped != 3 {
		t.Fatalf("res2 = %+v", res2)
	}
	after, _ := os.ReadFile(filepath.Join(d1, ".claude", "napkin.md"))
	if string(before) != string(after) {
		t.Fatal("o napkin foi alterado")
	}
}

func TestCardConcluidoCreatesCandidates(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	if _, err := a.st.AddDecision(domain.Decision{CardID: c.ID, What: "Usar PKCE", Why: "mais seguro", At: testNow}); err != nil {
		t.Fatal(err)
	}
	ch, cancel := a.hub.Subscribe()
	defer cancel()
	// Mover para revisão não cria.
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "revisao"}, 0); rec.Code != 200 {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body)
	}
	if l, _ := a.st.ListMemories(storeFilterAll()); len(l) != 0 {
		t.Fatalf("revisao criou %+v", l)
	}
	if rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "concluido"}, 0); rec.Code != 200 {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body)
	}
	l, _ := a.st.ListMemories(storeFilterAll())
	if len(l) != 1 || l[0].Status != "candidata" || l[0].Source != "card" || l[0].SourceRef != c.Key || l[0].Rule != "Usar PKCE" {
		t.Fatalf("l = %+v", l)
	}
	var kinds []string
loop:
	for {
		select {
		case k := <-ch:
			kinds = append(kinds, k)
		case <-time.After(200 * time.Millisecond):
			break loop
		}
	}
	if !strings.Contains(strings.Join(kinds, ","), "memories") {
		t.Fatalf("kinds = %v", kinds)
	}
	// Salvar de novo na mesma coluna não duplica.
	a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"title": "Novo"}, 0)
	// Sair e voltar: dedupe.
	a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "revisao"}, 0)
	a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"column": "concluido"}, 0)
	if l, _ := a.st.ListMemories(storeFilterAll()); len(l) != 1 {
		t.Fatalf("duplicou: %+v", l)
	}
}

func TestNobreDoneDoesNotCreateCandidates(t *testing.T) {
	a := newTestAPI(t)
	c := a.card(t)
	a.liveSession(t, "claude:s", 4242, c.Key)
	a.st.AddDecision(domain.Decision{CardID: c.ID, What: "Decisão", Why: "porque", SessionID: "claude:s", At: testNow})
	a.st.TouchSession("claude:s", testNow.Add(-time.Hour))
	a.do(t, "POST", "/api/me/test", map[string]string{"text": "go test"}, 4242)
	if rec := a.do(t, "POST", "/api/me/done", nil, 4242); rec.Code != 200 {
		t.Fatalf("done = %d %s", rec.Code, rec.Body)
	}
	if l, _ := a.st.ListMemories(storeFilterAll()); len(l) != 0 {
		t.Fatalf("done criou %+v", l)
	}
}

func storeFilterAll() store.MemoryFilter { return store.MemoryFilter{} }

type importRes struct {
	Created int `json:"created"`
	Skipped int `json:"skipped"`
	Files   []struct {
		ProjectID int64  `json:"project_id"`
		Path      string `json:"path"`
		Items     int    `json:"items"`
		Error     string `json:"error"`
	} `json:"files"`
}

func TestImportAfterEditDoesNotRecreate(t *testing.T) {
	a := newTestAPI(t)
	d := t.TempDir()
	a.st.CreateProject("um", d)
	writeNapkin(t, d, "## Shell\n1. **[2026-01-01] Rodar go vet antes do commit**\n   Do instead: go vet\n")
	if r := decode[importRes](t, a.do(t, "POST", "/api/memories/import", nil, 0)); r.Created != 1 {
		t.Fatalf("r = %+v", r)
	}
	l, _ := a.st.ListMemories(store.MemoryFilter{})
	if rec := a.do(t, "PATCH", fmt.Sprintf("/api/memories/%d", l[0].ID), map[string]any{"rule": "Vet sempre"}, 0); rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	if r := decode[importRes](t, a.do(t, "POST", "/api/memories/import", nil, 0)); r.Created != 0 || r.Skipped != 1 {
		t.Fatalf("reimport = %+v", r)
	}
}

func TestConcurrentImportsNoDuplicates(t *testing.T) {
	a := newTestAPI(t)
	d := t.TempDir()
	a.st.CreateProject("um", d)
	writeNapkin(t, d, "## Shell\n1. **Regra A**\n   Do instead: a\n2. **Regra B**\n   Do instead: b\n")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.do(t, "POST", "/api/memories/import", nil, 0)
		}()
	}
	wg.Wait()
	if l, _ := a.st.ListMemories(store.MemoryFilter{}); len(l) != 2 {
		t.Fatalf("l = %+v", l)
	}
}

func TestImportReadErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lê qualquer arquivo")
	}
	a := newTestAPI(t)
	d1, d2 := t.TempDir(), t.TempDir()
	p1, _ := a.st.CreateProject("fechado", d1)
	a.st.CreateProject("bom", d2)
	writeNapkin(t, d1, "## Shell\n1. **X**\n")
	file := filepath.Join(d1, ".claude", "napkin.md")
	os.Chmod(file, 0)
	defer os.Chmod(file, 0o644)
	writeNapkin(t, d2, "## Shell\n1. **Y**\n   Do instead: y\n")
	r := decode[importRes](t, a.do(t, "POST", "/api/memories/import", nil, 0))
	if r.Created != 1 || len(r.Files) != 2 || r.Files[0].ProjectID != p1.ID || r.Files[0].Error == "" || r.Files[1].Error != "" {
		t.Fatalf("r = %+v", r)
	}
}

func TestImportLongLine(t *testing.T) {
	a := newTestAPI(t)
	d := t.TempDir()
	a.st.CreateProject("um", d)
	writeNapkin(t, d, "## Shell\n1. **"+strings.Repeat("a", 2<<20)+"**\n   Do instead: x\n")
	r := decode[importRes](t, a.do(t, "POST", "/api/memories/import", nil, 0))
	// 2 MB passa do teto de 4000: o clamp de segurança corta, mas a linha lê.
	if len(r.Files) != 1 || r.Files[0].Error != "" || r.Files[0].Items != 1 || r.Created != 1 {
		t.Fatalf("r = %+v", r)
	}
}

func TestImportSharedPathRootClaims(t *testing.T) {
	a := newTestAPI(t)
	d := t.TempDir()
	// O app é cadastrado primeiro (menor id) mas a raiz tem a pasta.
	root, _ := a.st.CreateProject("raiz", d)
	app, _, err := a.st.CreateChild("app", d+"/sub", root.ID)
	if err != nil {
		t.Skip("filho em pasta própria:", err)
	}
	_ = app
	writeNapkin(t, d, "## Shell\n1. **Só uma**\n   Do instead: x\n")
	r := decode[importRes](t, a.do(t, "POST", "/api/memories/import", nil, 0))
	if len(r.Files) != 1 || r.Files[0].ProjectID != root.ID {
		t.Fatalf("r = %+v", r)
	}
}

func TestNapkinOwnersPrefersRoot(t *testing.T) {
	pid := int64(1)
	got := napkinOwners([]domain.Project{
		{ID: 2, Path: "/x", ParentID: &pid},
		{ID: 3, Path: "/x"},
	})
	if got["/x/.claude/napkin.md"] != 3 {
		t.Fatalf("got = %v", got)
	}
}

func TestSessionWithoutProjectGetsGlobals(t *testing.T) {
	a := newTestAPI(t)
	g := a.mem(t, nil, "regra global", "ativa")
	if err := a.st.StartSession(domain.Session{ID: "claude:solta", Agent: "claude", PID: 5050, Cwd: "/fora", StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	list := decode[[]memJSON](t, a.do(t, "GET", "/api/me/memories", nil, 5050))
	if len(list) != 1 || list[0].ID != g.ID || list[0].Scope != "global" {
		t.Fatalf("list = %+v", list)
	}
	rec := a.do(t, "POST", "/api/me/remember", map[string]string{"rule": "regra global", "scope": "global"}, 5050)
	m := decode[memJSON](t, rec)
	if rec.Code != 201 || len(m.Similar) != 1 || m.Scope != "global" {
		t.Fatalf("m = %+v %s", m, rec.Body)
	}
}

func TestPatchClearsNeedsReview(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	long, _ := a.st.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: strings.Repeat("c", 350), Do: "faça", Status: "candidata", Source: "napkin"})
	path := fmt.Sprintf("/api/memories/%d", long.ID)
	if m := decode[memJSON](t, a.do(t, "PATCH", path, map[string]any{"rule": "curta agora"}, 0)); m.NeedsReview || m.Status != "candidata" {
		t.Fatalf("m = %+v", m)
	}
	noDo, _ := a.st.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: "sem acao", Status: "candidata", Source: "napkin", NeedsReview: true})
	path = fmt.Sprintf("/api/memories/%d", noDo.ID)
	if m := decode[memJSON](t, a.do(t, "PATCH", path, map[string]any{"rule": "sem acao editada"}, 0)); !m.NeedsReview {
		t.Fatalf("sem ação deve manter a marca: %+v", m)
	}
	if m := decode[memJSON](t, a.do(t, "PATCH", path, map[string]any{"do": "agora tem"}, 0)); m.NeedsReview {
		t.Fatalf("com ação limpa: %+v", m)
	}
	flagged, _ := a.st.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: "aprovar", Do: "x", Status: "candidata", Source: "napkin", NeedsReview: true})
	if m := decode[memJSON](t, a.do(t, "PATCH", fmt.Sprintf("/api/memories/%d", flagged.ID), map[string]any{"status": "ativa"}, 0)); m.NeedsReview || m.Status != "ativa" {
		t.Fatalf("aprovar: %+v", m)
	}
}

func TestListMemoriesInvalidStatus(t *testing.T) {
	a := newTestAPI(t)
	if rec := a.do(t, "GET", "/api/memories?status=xx", nil, 0); rec.Code != 400 {
		t.Fatalf("= %d", rec.Code)
	}
}

func TestListMemoriesFamily(t *testing.T) {
	a := newTestAPI(t)
	root := a.project(t)
	web, _, _ := a.st.CreateChild("web", "/w/api/web", root.ID)
	other, _ := a.st.CreateProject("outro", "/w/outro")
	a.mem(t, &web.ID, "candidata do app", "candidata")
	a.mem(t, &web.ID, "ativa do app", "ativa")
	a.mem(t, &root.ID, "da raiz", "ativa")
	a.mem(t, nil, "global", "ativa")
	a.mem(t, &other.ID, "de outro", "candidata")
	byRule := func(path string) map[string]memJSON {
		out := map[string]memJSON{}
		for _, m := range decode[[]memJSON](t, a.do(t, "GET", path, nil, 0)) {
			out[m.Rule] = m
		}
		return out
	}
	got := byRule(fmt.Sprintf("/api/memories?project=%d&family=1", root.ID))
	if len(got) != 4 || got["candidata do app"].ID == 0 || got["candidata do app"].Inherited || got["ativa do app"].Inherited ||
		got["da raiz"].Inherited || !got["global"].Inherited || got["de outro"].ID != 0 {
		t.Fatalf("família = %v", got)
	}
	// Sem family, a raiz continua só com o que é dela e o global.
	if got := byRule(fmt.Sprintf("/api/memories?project=%d", root.ID)); len(got) != 2 {
		t.Fatalf("sem família = %v", got)
	}
	// family num app não muda nada.
	if got := byRule(fmt.Sprintf("/api/memories?project=%d&family=1", web.ID)); len(got) != 4 || !got["da raiz"].Inherited {
		t.Fatalf("app = %v", got)
	}
}

// Itens parecidos do mesmo projeto: só um entra na global, o outro fica local.
func TestImportMergedAndSameProjectStaysLocal(t *testing.T) {
	a := newTestAPI(t)
	d1, d2 := t.TempDir(), t.TempDir()
	a.st.CreateProject("um", d1)
	a.st.CreateProject("dois", d2)
	writeNapkin(t, d1, "## Execução\n1. rode go test antes do commit\n   Do instead: go test ./...\n2. rode go test antes do push\n   Do instead: go test ./...\n")
	writeNapkin(t, d2, "## Execução\n1. rode go test antes de commitar\n   Do instead: go test\n")
	res := decode[struct {
		Created int `json:"created"`
		Merged  int `json:"merged"`
	}](t, a.do(t, "POST", "/api/memories/import", nil, 0))
	if res.Created != 2 || res.Merged != 2 {
		t.Fatalf("res = %+v", res)
	}
	list := decode[[]memJSON](t, a.do(t, "GET", "/api/memories?status=candidata", nil, 0))
	rules := map[string]bool{}
	for _, m := range list {
		rules[fmt.Sprintf("%v:%s", m.ProjectID == nil, m.Rule)] = true
	}
	if !rules["true:rode go test antes do commit"] || !rules["false:rode go test antes do push"] {
		t.Fatalf("list = %v", rules)
	}
}

// Repro (a) pela rota: regra importada em um, descartada, e depois presente
// nos dois napkin.md: a reimportação não cria nada.
func TestImportDiscardedLocalDoesNotReturnAsGlobal(t *testing.T) {
	a := newTestAPI(t)
	d1, d2 := t.TempDir(), t.TempDir()
	a.st.CreateProject("um", d1)
	a.st.CreateProject("dois", d2)
	writeNapkin(t, d1, "## Shell\n1. use rg em vez de grep\n   Do instead: rg\n")
	type res struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
	}
	if r := decode[res](t, a.do(t, "POST", "/api/memories/import", nil, 0)); r.Created != 1 {
		t.Fatalf("1ª = %+v", r)
	}
	list := decode[[]memJSON](t, a.do(t, "GET", "/api/memories?status=candidata", nil, 0))
	a.do(t, "POST", "/api/memories/batch", map[string]any{"ids": []int64{list[0].ID}, "action": "descartar"}, 0)
	writeNapkin(t, d2, "## Shell\n1. Use rg em vez de grep!\n   Do instead: rg\n")
	if r := decode[res](t, a.do(t, "POST", "/api/memories/import", nil, 0)); r.Created != 0 || r.Skipped != 1 {
		t.Fatalf("reimportação = %+v", r)
	}
}
