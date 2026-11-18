package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func TestSettingsPutMergesOverCurrent(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "PUT", "/api/settings", map[string]any{"grace_min": 30, "rules": map[string]bool{"how_to_test": false}}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	st, _ := a.st.GetSettings()
	if st.GraceMin != 30 || st.Rules["how_to_test"] || !st.Rules["goal"] || st.WhereStoppedTTLMin != 15 {
		t.Fatalf("settings = %+v", st)
	}
	if rec := a.do(t, "PUT", "/api/settings", map[string]any{"incomplete_action": "x"}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("ação inválida = %d", rec.Code)
	}
}

func TestLimitsPricesProjects(t *testing.T) {
	a := newTestAPI(t)
	if rec := a.do(t, "PUT", "/api/limits", map[string]any{"agent": "claude", "daily_usd": 15, "action": "stop"}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("limite com ação inválida = %d", rec.Code)
	}
	if rec := a.do(t, "PUT", "/api/limits", map[string]any{"agent": "claude", "daily_usd": 15}, 0); rec.Code != http.StatusOK {
		t.Fatalf("limite = %d %s", rec.Code, rec.Body)
	}
	ls := decode[[]domain.Limit](t, a.do(t, "GET", "/api/limits", nil, 0))
	if len(ls) != 1 || ls[0].DailyUSD != 15 || ls[0].WarnPct != 80 || ls[0].Action != "warn" {
		t.Fatalf("limits = %+v", ls)
	}

	for _, body := range []map[string]any{{"model": "m"}, {"model": "m", "input_per_m": 3}, {"model": "m", "output_per_m": 3}} {
		rec := a.do(t, "PUT", "/api/prices", body, 0)
		if rec.Code != http.StatusBadRequest || decode[map[string]string](t, rec)["error"] != "Informe os preços de entrada e saída (maiores que zero)" {
			t.Fatalf("preço sem entrada/saída = %d %s", rec.Code, rec.Body)
		}
	}
	if rec := a.do(t, "PUT", "/api/prices", map[string]any{"model": "m", "input_per_m": 3, "output_per_m": 15}, 0); rec.Code != http.StatusOK {
		t.Fatalf("preço = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "DELETE", "/api/prices?model=m", nil, 0); rec.Code != http.StatusOK {
		t.Fatalf("delete preço = %d", rec.Code)
	}
	if ps := decode[[]domain.Price](t, a.do(t, "GET", "/api/prices", nil, 0)); len(ps) != 0 {
		t.Fatalf("prices = %+v", ps)
	}

	if rec := a.do(t, "POST", "/api/projects", map[string]any{"path": "/w/api"}, 0); rec.Code != http.StatusCreated {
		t.Fatalf("projeto = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "POST", "/api/projects", map[string]any{"path": "/w/api/"}, 0); rec.Code != http.StatusConflict {
		t.Fatalf("projeto duplicado = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "PATCH", "/api/projects/1", map[string]any{"capture": false}, 0); rec.Code != http.StatusOK {
		t.Fatalf("patch projeto = %d %s", rec.Code, rec.Body)
	}
	ps := decode[[]domain.Project](t, a.do(t, "GET", "/api/projects", nil, 0))
	if len(ps) != 1 || ps[0].Name != "api" || ps[0].Capture {
		t.Fatalf("projects = %+v", ps)
	}
	if _, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: 1}); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, "DELETE", "/api/projects/1", nil, 0); rec.Code != http.StatusConflict {
		t.Fatalf("delete projeto com card = %d %s", rec.Code, rec.Body)
	}
}

func TestSessionsWithUsage(t *testing.T) {
	a := newTestAPI(t)
	if err := a.st.StartSession(domain.Session{ID: "claude:s", Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := a.st.UpsertUsage([]domain.UsageRow{{SessionID: "claude:s", MessageID: "m1", Model: "sem-preco", Input: 10, Output: 5, At: testNow}}); err != nil {
		t.Fatal(err)
	}
	type sessionJSON struct {
		ID       string `json:"id"`
		Unpriced bool   `json:"unpriced"`
		Usage    struct {
			Input int64 `json:"input"`
		} `json:"usage"`
	}
	got := decode[[]sessionJSON](t, a.do(t, "GET", "/api/sessions?limit=5", nil, 0))
	if len(got) != 1 || got[0].ID != "claude:s" || !got[0].Unpriced || got[0].Usage.Input != 10 {
		t.Fatalf("sessions = %+v", got)
	}
	if rec := a.do(t, "GET", "/api/usage/totals?group=nada", nil, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("grupo inválido = %d", rec.Code)
	}
	if rec := a.do(t, "GET", "/api/usage/totals?group=agent&from=2026-11-01&to=2026-11-08", nil, 0); rec.Code != http.StatusOK {
		t.Fatalf("totals = %d %s", rec.Code, rec.Body)
	}
}

// Preço de cache em branco (null) fica desconhecido; 0 explícito fica 0.
func TestPutPriceCacheNull(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "PUT", "/api/prices", map[string]any{"model": "m", "input_per_m": 3, "output_per_m": 15, "cache_write_per_m": nil, "cache_read_per_m": 0}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	got := decode[[]map[string]any](t, a.do(t, "GET", "/api/prices", nil, 0))
	if len(got) != 1 || got[0]["cache_write_per_m"] != nil || got[0]["cache_read_per_m"] != float64(0) {
		t.Fatalf("prices = %v", got)
	}
	if rec := a.do(t, "PUT", "/api/prices", map[string]any{"model": "m", "input_per_m": 3, "output_per_m": 15, "cache_read_per_m": -1}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("negativo = %d", rec.Code)
	}
}

func TestCreateProjectReassignsAndSetsBase(t *testing.T) {
	main, _ := gitRepoWithWorktree(t)
	gitRun(t, main, "branch", "develop")
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	a := &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Git: gitinfo.Exec{}})}
	now := testNow
	if err := st.StartSession(domain.Session{ID: "claude:1", Agent: "claude", ProviderSessionID: "1", PID: 5, Cwd: main, StartedAt: now, LastEventAt: now}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/projects", map[string]any{"path": main}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("projeto = %d %s", rec.Code, rec.Body)
	}
	out := decode[map[string]any](t, rec)
	if out["reassigned"] != float64(1) || out["base_branch"] != "develop" {
		t.Fatalf("resposta = %v", out)
	}
	x, err := st.GetSession("claude:1")
	if err != nil || x.ProjectID == nil || !x.FullCapture {
		t.Fatalf("sessão = %+v, %v", x, err)
	}
}

func TestCreateProjectReassignsLinkedWorktreeSession(t *testing.T) {
	main, wt := gitRepoWithWorktree(t)
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	a := &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Git: gitinfo.Exec{}})}
	now := testNow
	if err := st.StartSession(domain.Session{ID: "claude:1", Agent: "claude", ProviderSessionID: "1", PID: 5, Cwd: wt,
		Worktree: wt, WorktreeLinked: true, Branch: "feat/wt", StartedAt: now, LastEventAt: now}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/projects", map[string]any{"path": main}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("projeto = %d %s", rec.Code, rec.Body)
	}
	if out := decode[map[string]any](t, rec); out["reassigned"] != float64(1) {
		t.Fatalf("resposta = %v", out)
	}
	x, err := st.GetSession("claude:1")
	if err != nil || x.ProjectID == nil || !x.FullCapture {
		t.Fatalf("sessão = %+v, %v", x, err)
	}
	if rec := a.do(t, "POST", "/api/me/start", map[string]any{"title": "Novo"}, 5); rec.Code != http.StatusCreated {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
}

func TestScanEndpoint(t *testing.T) {
	main, _ := gitRepoWithWorktree(t)
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	a := &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Git: gitinfo.Exec{}})}
	p, err := st.CreateProject("api", main)
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "checkout", "-q", "-b", "feat/z")
	if err := os.WriteFile(filepath.Join(main, "z.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "add", ".")
	gitRun(t, main, "commit", "-q", "-m", "z")
	gitRun(t, main, "checkout", "-q", "main")
	if _, err := st.CreateCard(domain.Card{Title: "Existente", ProjectID: p.ID, Branch: "feat/z"}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10)+"/scan", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("scan = %d %s", rec.Code, rec.Body)
	}
	out := decode[struct {
		Base     string
		Branches []struct{ Name string }
		Sessions []map[string]any
		Cards    []struct{ Key, Title, Column, Branch string }
	}](t, rec)
	if out.Base != "main" || len(out.Branches) < 1 || out.Branches[0].Name != "feat/z" && out.Branches[0].Name != "feat/wt" {
		t.Fatalf("scan = %s", rec.Body)
	}
	if len(out.Cards) != 1 || out.Cards[0].Key != "NAC-1" || out.Cards[0].Column != "backlog" || out.Cards[0].Branch != "feat/z" {
		t.Fatalf("cards = %+v", out.Cards)
	}
	if rec := a.do(t, "GET", "/api/projects/999/scan", nil, 0); rec.Code != http.StatusNotFound {
		t.Fatalf("inexistente = %d", rec.Code)
	}
}

func TestScanEndpointMissingBase409(t *testing.T) {
	main, _ := gitRepoWithWorktree(t)
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	a := &testAPI{st: st, hub: hub, h: New(st, Deps{Now: func() time.Time { return testNow }, Hub: hub, Git: gitinfo.Exec{}})}
	p, _ := st.CreateProject("api", main)
	if err := st.SetProjectBase(p.ID, "develop"); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10)+"/scan", nil, 0)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "nobre project base") {
		t.Fatalf("scan = %d %s", rec.Code, rec.Body)
	}
}

// A pasta pessoal, a raiz e qualquer pasta acima da pessoal nunca viram projeto
// (cadastro irreversível que engoliria todas as sessões).
func TestCreateProjectRefusesHomeAndAncestors(t *testing.T) {
	a := newTestAPI(t)
	home := filepath.Join(t.TempDir(), "casa", "nobre")
	t.Setenv("HOME", home)
	for _, path := range []string{home, filepath.Dir(home), "/", home + "/"} {
		rec := a.do(t, "POST", "/api/projects", map[string]any{"path": path}, 0)
		if rec.Code != http.StatusBadRequest || !strings.Contains(decode[map[string]string](t, rec)["error"], "pasta pessoal") {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if ps, _ := a.st.ListProjects(); len(ps) != 0 {
		t.Fatalf("projetos = %+v", ps)
	}
	if rec := a.do(t, "POST", "/api/projects", map[string]any{"path": filepath.Join(home, "proj")}, 0); rec.Code != http.StatusCreated {
		t.Fatalf("subpasta do HOME = %d %s", rec.Code, rec.Body)
	}
	p, _ := a.st.ListProjects()
	rec := a.do(t, "PATCH", "/api/projects/"+strconv.FormatInt(p[0].ID, 10), map[string]any{"path": home}, 0)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH para o HOME = %d %s", rec.Code, rec.Body)
	}
}

func TestScanEndpointNoBase409(t *testing.T) {
	a := newTestAPI(t) // sem Git: nenhuma base detectável
	p := a.project(t)
	rec := a.do(t, "GET", "/api/projects/"+strconv.FormatInt(p.ID, 10)+"/scan", nil, 0)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "nobre project base") {
		t.Fatalf("scan = %d %s", rec.Code, rec.Body)
	}
}

func TestScanGroupRefused(t *testing.T) {
	a := newTestAPI(t)
	grp, err := a.st.CreateGroup("grupo")
	if err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/projects/"+strconv.FormatInt(grp.ID, 10)+"/scan", nil, 0)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("scan = %d %s", rec.Code, rec.Body)
	}
	want := "grupo não tem repositório; rode o levantamento em cada sub-projeto"
	if got := decode[map[string]string](t, rec)["error"]; got != want {
		t.Fatalf("erro = %q", got)
	}
}

func TestCreateGroupViaAPI(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "POST", "/api/projects", map[string]any{"name": "loja"}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("grupo = %d %s", rec.Code, rec.Body)
	}
	out := decode[map[string]any](t, rec)
	if out["path"] != "" || out["name"] != "loja" || out["parent_id"] != nil {
		t.Fatalf("resposta = %v", out)
	}
	if rec := a.do(t, "POST", "/api/projects", map[string]any{}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("sem nome = %d", rec.Code)
	}
}

func TestCreateChildViaAPIReportsMoved(t *testing.T) {
	a := newTestAPI(t)
	pai := decode[domain.Project](t, a.do(t, "POST", "/api/projects", map[string]any{"name": "loja", "path": "/r/loja", "base_branch": "main"}, 0))
	now := testNow
	if err := a.st.StartSession(domain.Session{ID: "claude:w", Agent: "claude", ProviderSessionID: "w", PID: 7, Cwd: "/r/loja/web", ProjectID: &pai.ID, FullCapture: true, StartedAt: now, LastEventAt: now}); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "POST", "/api/projects", map[string]any{"name": "web", "path": "/r/loja/web", "parent_id": pai.ID}, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("filho = %d %s", rec.Code, rec.Body)
	}
	out := decode[map[string]any](t, rec)
	moved, _ := out["moved"].(map[string]any)
	if moved["sessions"] != float64(1) || moved["cards"] != float64(0) || out["parent_id"] != float64(pai.ID) {
		t.Fatalf("resposta = %v", out)
	}
	if out["base_branch"] != "" {
		t.Fatalf("app não tem base própria: %v", out["base_branch"])
	}
	if rec := a.do(t, "POST", "/api/projects", map[string]any{"name": "x", "path": "/fora/x", "parent_id": pai.ID}, 0); rec.Code != http.StatusBadRequest {
		t.Fatalf("pasta fora do pai = %d", rec.Code)
	}
}

func TestPatchParentErrorsAre400(t *testing.T) {
	a := newTestAPI(t)
	pai, _ := a.st.CreateProject("loja", "/r/loja")
	fora, _ := a.st.CreateProject("fora", "/outro/fora")
	rec := a.do(t, "PATCH", "/api/projects/"+strconv.FormatInt(fora.ID, 10), map[string]any{"parent_id": pai.ID}, 0)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pasta fora do pai = %d %s", rec.Code, rec.Body)
	}
	if msg := decode[map[string]string](t, rec)["error"]; strings.Contains(msg, "regra de sub-projeto") || msg == "" {
		t.Fatalf("mensagem = %q", msg)
	}
	filho, _, _ := a.st.CreateChild("web", "/r/loja/web", pai.ID)
	// Liga, move e desliga pelo PATCH; ausente não mexe no pai.
	if rec := a.do(t, "PATCH", "/api/projects/"+strconv.FormatInt(filho.ID, 10), map[string]any{"name": "web2"}, 0); rec.Code != 200 ||
		decode[map[string]any](t, rec)["parent_id"] != float64(pai.ID) {
		t.Fatalf("renomear mexeu no pai: %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "PATCH", "/api/projects/"+strconv.FormatInt(filho.ID, 10), map[string]any{"parent_id": nil}, 0)
	if rec.Code != 200 || decode[map[string]any](t, rec)["parent_id"] != nil {
		t.Fatalf("desligar = %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, "PATCH", "/api/projects/"+strconv.FormatInt(filho.ID, 10), map[string]any{"parent_id": pai.ID}, 0)
	if rec.Code != 200 || decode[map[string]any](t, rec)["parent_id"] != float64(pai.ID) {
		t.Fatalf("religar = %d %s", rec.Code, rec.Body)
	}
}
