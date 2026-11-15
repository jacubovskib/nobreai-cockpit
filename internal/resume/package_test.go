package resume

import (
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func fixture() (domain.Card, domain.Content) {
	c := domain.Card{Key: "NAC-42", Title: "Login OAuth2", Goal: "Entrar com Google", DoneWhen: "teste e2e passa",
		WhereStopped: "Implementar o callback", HowToTest: "go test ./auth", Branch: "feat/oauth2"}
	ct := domain.Content{
		Decisions: []domain.Decision{{What: "usar PKCE", Why: "sem segredo no cliente"}},
		Files:     []domain.EssentialFile{{Path: "auth/cb.go", Lines: "10-40", Why: "callback"}, {Path: "auth/x.go", Why: "helpers"}},
		Todos:     []domain.Todo{{Text: "callback", Done: false}, {Text: "config", Done: true}},
	}
	return c, ct
}

func TestBuildPackageOrderAndOmissions(t *testing.T) {
	c, ct := fixture()
	s := []domain.Session{{Cwd: "/w/app", StartedAt: time.Now()}}
	out := Build(c, ct, s, nil, "")
	for _, want := range []string{"# NAC-42 · Login OAuth2", "app · feat/oauth2 · 1 sessões",
		"- usar PKCE — porquê: sem segredo no cliente", "- auth/cb.go:10-40: callback", "- auth/x.go: helpers",
		"- [ ] callback", "- [x] config", "sem mudanças registradas"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em:\n%s", want, out)
		}
	}
	if strings.Index(out, "## Onde parei") > strings.Index(out, "## Decisões") || !strings.Contains(out, "## Onde parei") {
		t.Fatalf("ordem:\n%s", out)
	}
	if strings.Contains(out, "## Perguntas em aberto") {
		t.Fatalf("seção vazia presente:\n%s", out)
	}
	order := []string{"## Onde parei", "## Objetivo", "## Falta", "## Feito", "## Decisões", "## Arquivos essenciais", "## Como testar", "## Desde a última sessão"}
	last := -1
	for _, h := range order {
		i := strings.Index(out, h)
		if i <= last {
			t.Fatalf("%s fora de ordem:\n%s", h, out)
		}
		last = i
	}
}

func TestBuildEmptyWhereStopped(t *testing.T) {
	out := Build(domain.Card{Key: "NAC-1", Title: "T"}, domain.Content{}, nil, []store.Total{}, "2 files changed")
	if !strings.Contains(out, "## Onde parei\n(não registrado)") || !strings.Contains(out, "2 files changed") {
		t.Fatal(out)
	}
	if strings.Contains(out, "## Objetivo") || strings.Contains(out, "## Falta") {
		t.Fatal(out)
	}
}

func TestVars(t *testing.T) {
	c, ct := fixture()
	r := domain.ReadinessResult{Items: []domain.ReadinessItem{{Label: "Objetivo", OK: true}, {Label: "Onde parei", OK: false}, {Label: "Como testar", OK: false}}}
	v := Vars(c, ct, "PKG", r)
	want := map[string]string{"card.id": "NAC-42", "card.titulo": "Login OAuth2", "card.onde_parei": "Implementar o callback",
		"card.pacote": "PKG", "card.arquivos": "auth/cb.go, auth/x.go", "card.como_testar": "go test ./auth",
		"git.branch": "feat/oauth2", "handoff.faltando": "Onde parei; Como testar"}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
}

func TestBuildCostFormatPtBR(t *testing.T) {
	c := domain.Card{Key: "NAC-1", Title: "T"}
	out := Build(c, domain.Content{}, nil, []store.Total{{CostUSD: 4.8}, {CostUSD: 0.02}}, "")
	if !strings.Contains(out, "0 sessões · US$ 4,82\n") {
		t.Fatal(out)
	}
	out = Build(c, domain.Content{}, nil, []store.Total{{CostUSD: 1, Unpriced: true}}, "")
	if strings.Contains(out, "US$") {
		t.Fatal(out)
	}
}

func TestPackageQuestionsShowIDs(t *testing.T) {
	c, ct := fixture()
	ct.Questions = []domain.Question{{ID: 3, Text: "x"}, {ID: 4, Text: "y", Answer: "z"}}
	out := Build(c, ct, nil, nil, "")
	if !strings.Contains(out, "- [3] x") || strings.Contains(out, "- [4]") {
		t.Fatalf("perguntas:\n%s", out)
	}
}
