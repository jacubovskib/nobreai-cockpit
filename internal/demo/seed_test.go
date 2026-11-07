package demo

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// testNow fixa "agora" às 15:00 locais de hoje, para que as sessões "de hoje"
// não cruzem a meia-noite quando o teste roda de madrugada.
func testNow() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 15, 0, 0, 0, time.Local)
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seeded(t *testing.T, now time.Time) *store.Store {
	t.Helper()
	st := openStore(t)
	if err := Seed(st, now, os.Getpid()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	return st
}

func cardByTitle(t *testing.T, st *store.Store, title string) domain.Card {
	t.Helper()
	cs, err := st.ListCards(store.CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Title == title {
			return c
		}
	}
	t.Fatalf("card %q não encontrado", title)
	return domain.Card{}
}

func TestSeedRefusesNonEmpty(t *testing.T) {
	st := openStore(t)
	p, err := st.CreateProject("x", "/tmp/x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(domain.Card{Title: "antes", ProjectID: p.ID}); err != nil {
		t.Fatal(err)
	}
	err = Seed(st, time.Now(), 1)
	if err == nil || err.Error() != "o banco não está vazio; use outro --db" {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("esperava ErrNotEmpty, veio %v", err)
	}
}

func TestSeedHeroCard(t *testing.T) {
	now := testNow()
	st := seeded(t, now)
	hero := cardByTitle(t, st, heroTitle)

	sess, err := st.SessionsForCard(hero.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess) != 3 {
		t.Fatalf("sessões = %d", len(sess))
	}
	agents := []string{sess[0].Agent, sess[1].Agent, sess[2].Agent}
	if agents[0] != "claude" || agents[1] != "codex" || agents[2] != "claude" {
		t.Fatalf("agentes = %v", agents)
	}
	if sess[2].EndedAt != nil || sess[0].EndedAt == nil || sess[1].EndedAt == nil {
		t.Fatal("só a sessão 3 deve estar viva")
	}
	if !sess[0].StartedAt.Before(now.Add(-12*time.Hour)) || !sess[1].StartedAt.Before(sess[2].StartedAt) {
		t.Fatalf("as sessões 1 e 2 deveriam ser de dias anteriores: %v, %v", sess[0].StartedAt, sess[1].StartedAt)
	}

	ct, err := st.CardContent(hero.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, td := range ct.Todos {
		if td.Done {
			done++
		}
	}
	if len(ct.Todos) != 6 || done != 3 || len(ct.Decisions) != 3 || len(ct.Files) != 3 || len(ct.Questions) != 1 {
		t.Fatalf("conteúdo: todos=%d feitos=%d decisões=%d arquivos=%d perguntas=%d",
			len(ct.Todos), done, len(ct.Decisions), len(ct.Files), len(ct.Questions))
	}
	if hero.Goal == "" || hero.DoneWhen == "" || hero.Column != domain.EmAndamento {
		t.Fatalf("card = %+v", hero)
	}
	touched, err := st.TouchedFiles(hero.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(touched) != 14 {
		t.Fatalf("arquivos tocados = %d", len(touched))
	}

	set, err := st.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !set.Demo {
		t.Fatal("setting demo deveria ser true")
	}
	lastEdit, err := st.LastEventAt(hero.ID, domain.EvFileEdit)
	if err != nil {
		t.Fatal(err)
	}
	for _, live := range []bool{true, false} {
		r := domain.Readiness(hero, ct, live, lastEdit, set, now)
		if r.Met != 4 || r.Total != 5 {
			t.Fatalf("live=%v readiness = %+v", live, r)
		}
	}

	totals, err := st.Totals(store.TotalsFilter{GroupBy: "card"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range totals {
		if tt.Key != hero.Key {
			continue
		}
		if tt.Input != 820000 || tt.Output != 121000 || tt.CacheWrite+tt.CacheRead != 141000 {
			t.Fatalf("totais = %+v", tt)
		}
		if tt.Unpriced || tt.CostUSD <= 0 {
			t.Fatalf("custo = %+v", tt)
		}
		return
	}
	t.Fatal("sem totais do card")
}

func TestSeedBoardColumns(t *testing.T) {
	st := seeded(t, testNow())
	cs, err := st.ListCards(store.CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 11 {
		t.Fatalf("cards = %d", len(cs))
	}
	count := map[domain.Column]int{}
	for _, c := range cs {
		count[c.Column]++
	}
	want := map[domain.Column]int{domain.Backlog: 2, domain.EmAndamento: 4, domain.Pausado: 2, domain.Revisao: 1, domain.Concluido: 2}
	for col, n := range want {
		if count[col] != n {
			t.Errorf("coluna %s: %d, esperava %d", col, count[col], n)
		}
	}
}

func TestSeedLiveSessionsSurviveReconcile(t *testing.T) {
	now := testNow()
	st := seeded(t, now)
	before, err := st.LiveSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 4 {
		t.Fatalf("sessões vivas = %d, esperava 3 com card + 1 sem card", len(before))
	}
	noCard, withPID := 0, 0
	for _, x := range before {
		if x.CardID == nil {
			noCard++
		}
		if x.PID == os.Getpid() {
			withPID++
		}
	}
	if noCard != 1 || withPID != 3 {
		t.Fatalf("sem card = %d, com PID vivo = %d", noCard, withPID)
	}
	cardsBefore, _ := st.ListCards(store.CardFilter{})

	set, _ := st.GetSettings()
	// Duas passadas, a segunda depois da carência: nada pode ser encerrado.
	for _, at := range []time.Time{now, now.Add(time.Hour)} {
		if _, err := board.Reconcile(st, "/proc", at, set); err != nil {
			t.Fatal(err)
		}
	}
	after, err := st.LiveSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("sessões vivas: %d -> %d", len(before), len(after))
	}
	cardsAfter, _ := st.ListCards(store.CardFilter{})
	for i := range cardsBefore {
		if cardsBefore[i].Column != cardsAfter[i].Column {
			t.Fatalf("card %s mudou de coluna", cardsBefore[i].Key)
		}
	}
}

func TestSeedLimitsPricesAndToday(t *testing.T) {
	now := testNow()
	st := seeded(t, now)
	limits, err := st.ListLimits()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, l := range limits {
		got[l.Agent] = l.DailyUSD
	}
	if got["claude"] != 15 || got["codex"] != 10 || got["kiro"] != 8 {
		t.Fatalf("limites = %v", got)
	}

	models, err := st.Totals(store.TotalsFilter{GroupBy: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 4 {
		t.Fatalf("modelos = %+v", models)
	}
	for _, m := range models {
		if m.Unpriced {
			t.Errorf("modelo %q sem preço", m.Key)
		}
	}

	from, to := board.DayBounds(now)
	today, err := st.Totals(store.TotalsFilter{From: from, To: to, GroupBy: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"claude": 5.73, "codex": 8.20, "kiro": 0.69}
	for _, a := range today {
		if math.Abs(a.CostUSD-want[a.Key]) > 0.01 {
			t.Errorf("hoje %s = %.3f, esperava %.2f", a.Key, a.CostUSD, want[a.Key])
		}
		delete(want, a.Key)
	}
	if len(want) != 0 {
		t.Fatalf("agentes sem uso hoje: %v", want)
	}

	days, err := st.Totals(store.TotalsFilter{GroupBy: "day_agent"})
	if err != nil {
		t.Fatal(err)
	}
	distinct := map[string]bool{}
	for _, d := range days {
		distinct[d.Day] = true
	}
	if len(distinct) != 7 {
		t.Fatalf("dias com uso = %d, esperava 7", len(distinct))
	}
}

func TestSeedPrompts(t *testing.T) {
	st := seeded(t, testNow())
	evs, err := st.RecentEvents(500)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == domain.EvPrompt {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("prompts capturados = %d", n)
	}
}

func TestRefreshLivePID(t *testing.T) {
	st := seeded(t, testNow())
	if err := RefreshLivePID(st, 4242); err != nil {
		t.Fatal(err)
	}
	live, _ := st.LiveSessions()
	n := 0
	for _, x := range live {
		if x.PID == 4242 {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("sessões com o PID novo = %d", n)
	}
}

func TestSeedRefusesDBWithPriceOrLimit(t *testing.T) {
	st := openStore(t)
	if err := st.PutPrice(domain.Price{Model: "m", InputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := Seed(st, testNow(), 1); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("preço: err = %v", err)
	}
	st2 := openStore(t)
	if err := st2.PutLimit(domain.Limit{Agent: "claude", DailyUSD: 1, WarnPct: 80, Action: "warn"}); err != nil {
		t.Fatal(err)
	}
	if err := Seed(st2, testNow(), 1); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("limite: err = %v", err)
	}
}
