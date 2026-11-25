package api

import (
	"net/http"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// hierCard é o card como as respostas da API o devolvem, só com os campos da
// hierarquia.
type hierCard struct {
	Key          string   `json:"key"`
	Title        string   `json:"title"`
	Type         string   `json:"type"`
	Priority     string   `json:"priority"`
	Points       *float64 `json:"points"`
	Labels       []string `json:"labels"`
	Column       string   `json:"column"`
	ParentKey    string   `json:"parent_key"`
	ParentTitle  string   `json:"parent_title"`
	Branch       string   `json:"branch"`
	BlockedBy    string   `json:"blocked_by"`
	PauseReason  string   `json:"pause_reason"`
	WaitingOnYou bool     `json:"waiting_on_you"`
	WaitingWhy   string   `json:"waiting_reason"`
	Children     *struct {
		Total int `json:"total"`
		Done  int `json:"done"`
	} `json:"children"`
	ChildrenTotals *struct {
		CostUSD float64 `json:"cost_usd"`
	} `json:"children_totals"`
	TreeTotals *struct {
		CostUSD float64 `json:"cost_usd"`
	} `json:"tree_totals"`
	Readiness struct {
		Items []any `json:"items"`
		Ready bool  `json:"ready"`
	} `json:"readiness"`
	ChildCards []hierCard `json:"child_cards"`
}

// wantErr confere o status e a mensagem de erro da resposta.
func (a *testAPI) wantErr(t *testing.T, method, path string, body any, code int, msg string) {
	t.Helper()
	rec := a.do(t, method, path, body, 0)
	if rec.Code != code || decode[map[string]string](t, rec)["error"] != msg {
		t.Fatalf("%s %s %v = %d %s, want %d %q", method, path, body, rec.Code, rec.Body, code, msg)
	}
}

func (a *testAPI) postCard(t *testing.T, body map[string]any) hierCard {
	t.Helper()
	rec := a.do(t, "POST", "/api/cards", body, 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %v = %d %s", body, rec.Code, rec.Body)
	}
	return decode[hierCard](t, rec)
}

func (a *testAPI) getHier(t *testing.T, key string) hierCard {
	t.Helper()
	rec := a.do(t, "GET", "/api/cards/"+key, nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", key, rec.Code, rec.Body)
	}
	return decode[hierCard](t, rec)
}

// boardCard é o card como aparece em GET /api/cards.
func (a *testAPI) boardCard(t *testing.T, key string) hierCard {
	t.Helper()
	rec := a.do(t, "GET", "/api/cards", nil, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/cards = %d %s", rec.Code, rec.Body)
	}
	for _, c := range decode[[]hierCard](t, rec) {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("%s fora do board", key)
	return hierCard{}
}

func TestCreateEpicAndChild(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	epic := a.postCard(t, map[string]any{"title": "Login", "project_id": p.ID, "type": "epico"})
	if epic.Type != "epico" || epic.Labels == nil {
		t.Fatalf("épico = %+v", epic)
	}
	child := a.postCard(t, map[string]any{"title": "Tela", "project_id": p.ID, "type": "historia",
		"parent_key": " " + epic.Key + " ", "priority": "alta", "labels": []string{"front", "front"}, "points": 3})
	got := a.getHier(t, child.Key)
	if got.Type != "historia" || got.Priority != "alta" || got.Points == nil || *got.Points != 3 ||
		len(got.Labels) != 1 || got.Labels[0] != "front" || got.ParentKey != epic.Key || got.ParentTitle != "Login" {
		t.Fatalf("filha = %+v", got)
	}
	board := a.boardCard(t, child.Key)
	if board.ParentKey != epic.Key || board.ParentTitle != "Login" {
		t.Fatalf("filha no board = %+v", board)
	}
	// points null tira a estimativa; parent_key "" tira o pai; ausentes não mudam.
	rec := a.do(t, "PATCH", "/api/cards/"+child.Key, map[string]any{"points": nil, "parent_key": ""}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	got = a.getHier(t, child.Key)
	if got.Points != nil || got.ParentKey != "" || got.Priority != "alta" || len(got.Labels) != 1 {
		t.Fatalf("depois do PATCH = %+v", got)
	}
	// Card comum continua nascendo tarefa/media sem labels.
	plain := a.postCard(t, map[string]any{"title": "x", "project_id": p.ID})
	if plain.Type != "tarefa" || plain.Priority != "media" || plain.Labels == nil {
		t.Fatalf("card comum = %+v", plain)
	}
	if d := a.getHier(t, plain.Key); d.Labels == nil || d.ChildCards != nil || d.Children != nil {
		t.Fatalf("detalhe do card comum = %+v", d)
	}
}

func TestEpicOnGroupWithoutFolder(t *testing.T) {
	a := newTestAPI(t)
	g, err := a.st.CreateGroup("loja")
	if err != nil {
		t.Fatal(err)
	}
	epic := a.postCard(t, map[string]any{"title": "Épico", "project_id": g.ID, "type": "epico"})
	a.wantErr(t, "POST", "/api/cards", map[string]any{"title": "x", "project_id": g.ID, "type": "tarefa"},
		http.StatusBadRequest, msgGroupNoCards)
	a.wantErr(t, "POST", "/api/cards", map[string]any{"title": "x", "project_id": g.ID, "type": "epico", "branch": "feat/x"},
		http.StatusBadRequest, msgGroupNoBranch)
	// O épico no grupo sem pasta não pode virar card comum lá.
	a.wantErr(t, "PATCH", "/api/cards/"+epic.Key, map[string]any{"type": "tarefa"},
		http.StatusBadRequest, msgGroupNoCards)
	// Épico de um repositório do grupo pode ir para o grupo sem pasta.
	repo, _, err := a.st.CreateChild("api", "/r/loja/api", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	moved := a.postCard(t, map[string]any{"title": "Épico do repo", "project_id": repo.ID, "type": "epico"})
	if rec := a.do(t, "PATCH", "/api/cards/"+moved.Key, map[string]any{"project_id": g.ID}, 0); rec.Code != http.StatusOK {
		t.Fatalf("mover épico para o grupo = %d %s", rec.Code, rec.Body)
	}
	if got, _ := a.st.GetCard(moved.Key); got.ProjectID != g.ID {
		t.Fatalf("épico ficou no projeto %d", got.ProjectID)
	}
}

func TestHierarchyValidation(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	epic := a.postCard(t, map[string]any{"title": "E", "project_id": p.ID, "type": "epico"})
	plain := a.postCard(t, map[string]any{"title": "T", "project_id": p.ID})
	child := a.postCard(t, map[string]any{"title": "F", "project_id": p.ID, "parent_key": epic.Key})
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"title": "x", "project_id": p.ID}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	cases := []struct {
		body map[string]any
		code int
		msg  string
	}{
		{base(map[string]any{"labels": []string{"a b"}}), 400, `label sem espaços: "a b"`},
		{base(map[string]any{"points": -1}), 400, "points não pode ser negativo"},
		{base(map[string]any{"points": "três"}), 400, "points inválido"},
		{base(map[string]any{"type": "xyz"}), 400, "tipo inválido (use epico, historia, tarefa ou bug)"},
		{base(map[string]any{"priority": "urgente"}), 400, "prioridade inválida (use mais_alta, alta, media, baixa ou mais_baixa)"},
		{base(map[string]any{"parent_key": "nac-999"}), 404, "card NAC-999 não encontrado"},
		{base(map[string]any{"parent_key": plain.Key}), 400, "o pai precisa ser um épico"},
		{base(map[string]any{"type": "epico", "parent_key": epic.Key}), 400, "um épico não pode ter pai"},
	}
	for _, c := range cases {
		a.wantErr(t, "POST", "/api/cards", c.body, c.code, c.msg)
	}
	a.wantErr(t, "PATCH", "/api/cards/"+plain.Key, map[string]any{"parent_key": plain.Key},
		http.StatusBadRequest, "um card não pode ser pai de si mesmo")
	a.wantErr(t, "PATCH", "/api/cards/"+child.Key, map[string]any{"type": "epico"},
		http.StatusBadRequest, "tire o card do épico antes")
	a.wantErr(t, "PATCH", "/api/cards/"+epic.Key, map[string]any{"type": "tarefa"},
		http.StatusBadRequest, "remova as 1 filhas do épico antes")
	a.wantErr(t, "PATCH", "/api/cards/"+plain.Key, map[string]any{"labels": []string{""}},
		http.StatusBadRequest, "label vazia")
	if got := a.getHier(t, plain.Key); got.ParentKey != "" || len(got.Labels) != 0 {
		t.Fatalf("PATCH recusado gravou: %+v", got)
	}
}

func TestBecomeEpicRules(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	live := a.postCard(t, map[string]any{"title": "vivo", "project_id": p.ID})
	a.liveSession(t, "claude:vivo", 1, live.Key)
	a.wantErr(t, "PATCH", "/api/cards/"+live.Key, map[string]any{"type": "epico"}, http.StatusConflict,
		"o card tem uma sessão ao vivo; encerre a sessão antes de transformá-lo em épico")

	blocker := a.postCard(t, map[string]any{"title": "bloqueador", "project_id": p.ID})
	c := a.postCard(t, map[string]any{"title": "pausado", "project_id": p.ID, "branch": "feat/x"})
	card, err := a.st.GetCard(c.Key)
	if err != nil {
		t.Fatal(err)
	}
	card.Column, card.BlockedBy, card.PauseReason, card.HandoffIncomplete = domain.Pausado, blocker.Key, "outro", true
	if err := a.st.UpdateCard(card); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "PATCH", "/api/cards/"+c.Key, map[string]any{"type": "epico"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("virar épico = %d %s", rec.Code, rec.Body)
	}
	got := a.getHier(t, c.Key)
	if got.Type != "epico" || got.Column != "backlog" || got.BlockedBy != "" || got.PauseReason != "" || got.Branch != "" {
		t.Fatalf("épico = %+v", got)
	}
	if after, _ := a.st.GetCard(c.Key); after.HandoffIncomplete {
		t.Fatalf("handoff_incomplete ficou: %+v", after)
	}
}

func TestEpicManualMoveAPI(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	epic := a.postCard(t, map[string]any{"title": "E", "project_id": p.ID, "type": "epico"})
	child := a.postCard(t, map[string]any{"title": "F", "project_id": p.ID, "parent_key": epic.Key})
	if err := a.st.MoveCard(child.Key, domain.EmAndamento); err != nil {
		t.Fatal(err)
	}
	a.wantErr(t, "PATCH", "/api/cards/"+epic.Key, map[string]any{"column": "concluido"},
		http.StatusConflict, "a coluna do épico segue as filhas")
	// Reenviar a coluna atual não é mudança.
	if rec := a.do(t, "PATCH", "/api/cards/"+epic.Key, map[string]any{"column": "em_andamento", "title": "E2"}, 0); rec.Code != http.StatusOK {
		t.Fatalf("mesma coluna = %d %s", rec.Code, rec.Body)
	}
	if err := a.st.MoveCard(child.Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "PATCH", "/api/cards/"+epic.Key, map[string]any{"column": "concluido"}, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("concluir = %d %s", rec.Code, rec.Body)
	}
	if got := a.getHier(t, epic.Key); got.Column != "concluido" {
		t.Fatalf("épico = %+v", got)
	}
}

func TestEpicSummary(t *testing.T) {
	a := newTestAPI(t)
	p1 := a.project(t)
	p2, err := a.st.CreateProject("web", "/w/web")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	epic := a.postCard(t, map[string]any{"title": "E", "project_id": p1.ID, "type": "epico"})
	var kids []hierCard
	for i, pid := range []int64{p1.ID, p2.ID, p2.ID} {
		kids = append(kids, a.postCard(t, map[string]any{"title": "F" + string(rune('1'+i)), "project_id": pid, "parent_key": epic.Key}))
	}
	usage := func(sid, key string, input int64) {
		t.Helper()
		if err := a.st.StartSession(domain.Session{ID: sid, Agent: "claude", PID: 1, StartedAt: testNow}); err != nil {
			t.Fatal(err)
		}
		if err := a.st.AttachSession(sid, key, false); err != nil {
			t.Fatal(err)
		}
		if err := a.st.UpsertUsage([]domain.UsageRow{{SessionID: sid, MessageID: sid + "-m", Model: "m", Input: input, At: testNow}}); err != nil {
			t.Fatal(err)
		}
	}
	usage("claude:f1", kids[0].Key, 1_000_000)
	usage("claude:f2", kids[1].Key, 500_000)
	usage("claude:e", epic.Key, 2_000_000)
	if err := a.st.MoveCard(kids[0].Key, domain.Concluido); err != nil {
		t.Fatal(err)
	}
	b := a.boardCard(t, epic.Key)
	if b.Children == nil || b.Children.Total != 3 || b.Children.Done != 1 {
		t.Fatalf("children = %+v", b.Children)
	}
	if b.ChildrenTotals == nil || b.ChildrenTotals.CostUSD != 1.5 || b.TreeTotals == nil || b.TreeTotals.CostUSD != 3.5 {
		t.Fatalf("totais = %+v %+v", b.ChildrenTotals, b.TreeTotals)
	}
	if b.Readiness.Items == nil || len(b.Readiness.Items) != 0 || b.Readiness.Ready {
		t.Fatalf("readiness do épico = %+v", b.Readiness)
	}
	if k := a.boardCard(t, kids[1].Key); k.Children != nil || k.ChildrenTotals != nil || k.TreeTotals != nil {
		t.Fatalf("filha com resumo de épico: %+v", k)
	}
	d := a.getHier(t, epic.Key)
	if len(d.ChildCards) != 3 || d.Children == nil || d.Children.Done != 1 || d.TreeTotals == nil || d.TreeTotals.CostUSD != 3.5 {
		t.Fatalf("detalhe = %+v", d)
	}
	if d.ChildCards[1].ParentKey != epic.Key || d.ChildCards[1].Labels == nil {
		t.Fatalf("child_cards[1] = %+v", d.ChildCards[1])
	}
	empty := a.postCard(t, map[string]any{"title": "E2", "project_id": p1.ID, "type": "epico"})
	if d := a.getHier(t, empty.Key); d.ChildCards == nil || len(d.ChildCards) != 0 {
		t.Fatalf("épico sem filhas = %+v", d)
	}
}

func TestWaitingOnYouAPI(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	asking := a.postCard(t, map[string]any{"title": "pergunta", "project_id": p.ID})
	a.liveSession(t, "claude:q", 1, asking.Key)
	if err := a.st.MoveCard(asking.Key, domain.EmAndamento); err != nil {
		t.Fatal(err)
	}
	card, _ := a.st.GetCard(asking.Key)
	if _, err := a.st.AddQuestion(domain.Question{CardID: card.ID, Text: "qual banco?"}); err != nil {
		t.Fatal(err)
	}
	review := a.postCard(t, map[string]any{"title": "revisão", "project_id": p.ID})
	if err := a.st.MoveCard(review.Key, domain.Revisao); err != nil {
		t.Fatal(err)
	}
	idle := a.postCard(t, map[string]any{"title": "parado", "project_id": p.ID})
	for key, want := range map[string]string{asking.Key: "pergunta aberta", review.Key: "revisão", idle.Key: ""} {
		got := a.boardCard(t, key)
		if got.WaitingOnYou != (want != "") || got.WaitingWhy != want {
			t.Fatalf("%s: waiting = %v %q, want %q", key, got.WaitingOnYou, got.WaitingWhy, want)
		}
	}
}
