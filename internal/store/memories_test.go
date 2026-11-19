package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func mem(pid *int64, cat, rule string) domain.Memory {
	return domain.Memory{ProjectID: pid, Category: cat, Rule: rule, Status: "ativa", Source: "usuario"}
}

func TestMemorySettingsDefault(t *testing.T) {
	s := newTestStore(t)
	st, err := s.GetSettings()
	if err != nil || st.MemoryBudgetTokens != 1500 {
		t.Fatalf("budget = %d, err = %v", st.MemoryBudgetTokens, err)
	}
}

func TestCreateMemoryCleansAndValidates(t *testing.T) {
	s := newTestStore(t)
	m, err := s.CreateMemory(domain.Memory{Category: "shell", Rule: "  usar\nrg  ", Do: "x\ny", Status: "ativa", Source: "usuario"})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == 0 || m.Rule != "usar rg" || m.Do != "x y" || m.CreatedAt.IsZero() {
		t.Fatalf("m = %+v", m)
	}
	for name, bad := range map[string]domain.Memory{
		"vazia":     mem(nil, "shell", "  \n "),
		"categoria": mem(nil, "xx", "regra"),
		"longa":     mem(nil, "shell", strings.Repeat("a", 301)),
		"acao":      {Category: "shell", Rule: "r", Do: strings.Repeat("a", 401), Status: "ativa", Source: "usuario"},
		"cand4001":  {Category: "shell", Rule: strings.Repeat("a", 4001), Status: "candidata", Source: "napkin"},
		"status":    {Category: "shell", Rule: "r", Status: "zzz", Source: "usuario"},
	} {
		if _, err := s.CreateMemory(bad); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
	if _, err := s.CreateMemory(mem(nil, "shell", strings.Repeat("a", 300))); err != nil {
		t.Errorf("300 deveria passar: %v", err)
	}
}

func TestUpdateAndDeleteMemory(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	m, _ := s.CreateMemory(mem(nil, "shell", "uma"))
	m.Rule, m.Category, m.ProjectID, m.NeedsReview = "outra\nlinha", "dominio", &p.ID, true
	u, err := s.UpdateMemory(m)
	if err != nil {
		t.Fatal(err)
	}
	if u.Rule != "outra linha" || u.Category != "dominio" || u.ProjectID == nil || !u.NeedsReview {
		t.Fatalf("u = %+v", u)
	}
	u.Rule = ""
	if _, err := s.UpdateMemory(u); err == nil {
		t.Error("regra vazia deveria falhar")
	}
	if _, err := s.UpdateMemory(domain.Memory{ID: 999, Category: "shell", Rule: "x", Status: "ativa", Source: "usuario"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	if err := s.DeleteMemory(u.ID); err == nil {
		t.Error("ativa não pode ser excluída")
	}
	u.Rule, u.Status = "outra linha", "arquivada"
	if _, err := s.UpdateMemory(u); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMemory(u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMemory(u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestMoveMemoryStaysInGroup(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateMemory(mem(nil, "shell", "a"))
	b, _ := s.CreateMemory(mem(nil, "shell", "b"))
	c, _ := s.CreateMemory(mem(nil, "shell", "c"))
	o, _ := s.CreateMemory(mem(nil, "dominio", "outra categoria"))
	order := func() string {
		ms, _ := s.ListMemories(MemoryFilter{Global: true})
		var out []string
		for _, m := range ms {
			if m.Category == "shell" {
				out = append(out, m.Rule)
			}
		}
		return strings.Join(out, "")
	}
	if order() != "abc" {
		t.Fatalf("order = %s", order())
	}
	if err := s.MoveMemory(b.ID, -1); err != nil || order() != "bac" {
		t.Fatalf("subir: %v %s", err, order())
	}
	if err := s.MoveMemory(b.ID, -1); err != nil || order() != "bac" {
		t.Fatalf("topo: %v %s", err, order())
	}
	if err := s.MoveMemory(a.ID, 1); err != nil || order() != "bca" {
		t.Fatalf("descer: %v %s", err, order())
	}
	_ = c
	if err := s.MoveMemory(o.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveMemory(999, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestListMemoriesFilters(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	q, _ := s.CreateProject("web", "/w/web")
	s.CreateMemory(mem(nil, "shell", "Usar ripgrep"))
	s.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: "Preço em centavos", Do: "nunca float", Status: "ativa", Source: "usuario"})
	s.CreateMemory(mem(&q.ID, "dominio", "regra da web"))
	d, _ := s.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "dominio", Rule: "lixo", Status: "descartada", Source: "card"})
	_ = d
	n := func(f MemoryFilter) int {
		ms, err := s.ListMemories(f)
		if err != nil {
			t.Fatal(err)
		}
		return len(ms)
	}
	if got := n(MemoryFilter{}); got != 3 {
		t.Errorf("todos (sem descartada) = %d", got)
	}
	if got := n(MemoryFilter{Status: "descartada"}); got != 1 {
		t.Errorf("descartada = %d", got)
	}
	if got := n(MemoryFilter{Global: true}); got != 1 {
		t.Errorf("global = %d", got)
	}
	if got := n(MemoryFilter{ProjectIDs: []int64{p.ID}, Global: true}); got != 2 {
		t.Errorf("p+global = %d", got)
	}
	if got := n(MemoryFilter{ProjectIDs: []int64{p.ID}}); got != 1 {
		t.Errorf("p = %d", got)
	}
	if got := n(MemoryFilter{Term: "preco"}); got != 1 {
		t.Errorf("term regra = %d", got)
	}
	if got := n(MemoryFilter{Term: "FLOAT"}); got != 1 {
		t.Errorf("term ação = %d", got)
	}
	ms, err := s.ListMemories(MemoryFilter{Term: "zzz"})
	if err != nil || ms == nil || len(ms) != 0 {
		t.Errorf("vazio não nulo: %v %v", ms, err)
	}
}

func TestMemoryExistsSeesDiscarded(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	s.CreateMemory(domain.Memory{ProjectID: &p.ID, Category: "shell", Rule: "Usar RG!", Status: "descartada", Source: "card"})
	if ok, err := s.MemoryExists(&p.ID, "usar rg"); err != nil || !ok {
		t.Errorf("ok = %v err = %v", ok, err)
	}
	if ok, _ := s.MemoryExists(nil, "usar rg"); ok {
		t.Error("escopo global não deveria achar")
	}
}

func TestCardMemoryCandidates(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("api", "/w/api")
	c, _ := s.CreateCard(domain.Card{Title: "t", ProjectID: p.ID})
	s.AddDecision(domain.Decision{CardID: c.ID, What: "Usar WAL", Why: "leituras concorrentes"})
	s.AddDecision(domain.Decision{CardID: c.ID, What: "Chaves NAC-N", Why: "estável"})
	s.AddDecision(domain.Decision{CardID: c.ID, What: "Merge feita", Why: "detectado pelo Cockpit"})
	n, err := s.CardMemoryCandidates(c.ID)
	if err != nil || n != 2 {
		t.Fatalf("n = %d err = %v", n, err)
	}
	ms, _ := s.ListMemories(MemoryFilter{Status: "candidata"})
	if len(ms) != 2 {
		t.Fatalf("len = %d", len(ms))
	}
	for _, m := range ms {
		if m.Source != "card" || m.SourceRef != c.Key || m.Category != "dominio" || m.ProjectID == nil || *m.ProjectID != p.ID {
			t.Errorf("m = %+v", m)
		}
	}
	if ms[0].Rule != "Usar WAL" || ms[0].Do != "leituras concorrentes" {
		t.Errorf("m0 = %+v", ms[0])
	}
	if n, _ := s.CardMemoryCandidates(c.ID); n != 0 {
		t.Errorf("segunda chamada = %d", n)
	}
	if _, err := s.UpdateMemory(func() domain.Memory { m := ms[0]; m.Status = "descartada"; return m }()); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CardMemoryCandidates(c.ID); n != 0 {
		t.Errorf("após descartar = %d", n)
	}
	if _, err := s.CardMemoryCandidates(9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestMemoriesForSession(t *testing.T) {
	s := newTestStore(t)
	root, _ := s.CreateProject("mono", "/w/mono")
	app, _, err := s.CreateChild("app", "/w/mono/app", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	solo, _ := s.CreateProject("solo", "/w/solo")
	s.CreateMemory(mem(nil, "shell", "global"))
	s.CreateMemory(mem(&root.ID, "shell", "raiz2"))
	s.CreateMemory(mem(&root.ID, "shell", "raiz1"))
	s.CreateMemory(mem(&app.ID, "shell", "app"))
	s.CreateMemory(mem(&solo.ID, "shell", "solta"))
	s.CreateMemory(domain.Memory{ProjectID: &app.ID, Category: "shell", Rule: "cand", Status: "candidata", Source: "card"})
	rules := func(ms []domain.Memory) string {
		var o []string
		for _, m := range ms {
			o = append(o, m.Rule)
		}
		return strings.Join(o, ",")
	}

	card, _ := s.CreateCard(domain.Card{Title: "t", ProjectID: app.ID})
	a, r, g, err := s.MemoriesForSession(domain.Session{CardID: &card.ID, ProjectID: &solo.ID})
	if err != nil || rules(a) != "app" || rules(r) != "raiz2,raiz1" || rules(g) != "global" {
		t.Fatalf("card: %s | %s | %s | %v", rules(a), rules(r), rules(g), err)
	}
	a, r, g, _ = s.MemoriesForSession(domain.Session{ProjectID: &solo.ID})
	if len(a) != 0 || rules(r) != "solta" || rules(g) != "global" || a == nil {
		t.Fatalf("solto: %v | %s | %s", a, rules(r), rules(g))
	}
	a, r, g, err = s.MemoriesForSession(domain.Session{})
	if err != nil || a == nil || r == nil || g == nil || len(a)+len(r) != 0 || rules(g) != "global" {
		t.Fatalf("vazio: %v %v %v %v", a, r, g, err)
	}
}

func TestLongCandidateAndApproval(t *testing.T) {
	s := newTestStore(t)
	long := strings.Repeat("b", 1000)
	c, err := s.CreateMemory(domain.Memory{Category: "shell", Rule: strings.Repeat("a", 301), Do: long, Status: "candidata", Source: "napkin"})
	if err != nil || !c.NeedsReview {
		t.Fatalf("candidata longa: %+v %v", c, err)
	}
	ok, _ := s.CreateMemory(domain.Memory{Category: "shell", Rule: "curta", Status: "candidata", Source: "napkin"})
	if ok.NeedsReview {
		t.Error("curta não precisa de revisão")
	}
	c.Status = "ativa"
	_, err = s.UpdateMemory(c)
	if err == nil || !strings.Contains(err.Error(), "regra acima de 300 caracteres: edite antes de aprovar") {
		t.Fatalf("err = %v", err)
	}
	c.Rule = "regra curta"
	_, err = s.UpdateMemory(c)
	if err == nil || !strings.Contains(err.Error(), "ação acima de 400 caracteres: edite antes de aprovar") {
		t.Fatalf("err = %v", err)
	}
	c.Do = "ação curta"
	u, err := s.UpdateMemory(c)
	if err != nil || u.NeedsReview || u.Status != "ativa" {
		t.Fatalf("u = %+v err = %v", u, err)
	}
}

func TestDeleteProjectCascadesMemories(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("um", "/w/um")
	p2, _ := s.CreateProject("dois", "/w/dois")
	s.CreateMemory(mem(&p1.ID, "shell", "do um"))
	s.CreateMemory(mem(&p2.ID, "shell", "do dois"))
	s.CreateMemory(mem(nil, "shell", "global"))
	if err := s.DeleteProject(p1.ID); err != nil {
		t.Fatal(err)
	}
	l, _ := s.ListMemories(MemoryFilter{})
	if len(l) != 2 || l[0].Rule != "do dois" || l[1].Rule != "global" {
		t.Fatalf("l = %+v", l)
	}
}

func TestMemoryExistsMatchesOriginAfterEdit(t *testing.T) {
	s := newTestStore(t)
	m, _ := s.CreateMemory(mem(nil, "shell", "Regra Original!"))
	m.Rule = "outra coisa"
	if _, err := s.UpdateMemory(m); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"regra original", "OUTRA coisa"} {
		if ok, _ := s.MemoryExists(nil, r); !ok {
			t.Fatalf("%q deveria existir", r)
		}
	}
}

func TestMemoriesForSessionWithoutProjectGetsGlobals(t *testing.T) {
	s := newTestStore(t)
	s.CreateMemory(mem(nil, "shell", "global"))
	app, root, global, err := s.MemoriesForSession(domain.Session{})
	if err != nil || len(app) != 0 || len(root) != 0 || len(global) != 1 {
		t.Fatalf("%v %v %v %v", app, root, global, err)
	}
}

func TestImportMemoriesRollsBack(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.ImportMemories([]domain.Memory{
		{Category: "shell", Rule: "ok", Status: "candidata", Source: "napkin"},
		{Category: "zzz", Rule: "ruim", Status: "candidata", Source: "napkin"},
	})
	if err == nil {
		t.Fatal("esperava erro")
	}
	if l, _ := s.ListMemories(MemoryFilter{}); len(l) != 0 {
		t.Fatalf("não desfez: %+v", l)
	}
}
