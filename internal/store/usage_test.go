package store

import (
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func usageFixture(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	a := newSession("claude:1", 1)
	a.CardID = &c.ID
	b := newSession("codex:1", 2)
	b.Agent = "codex"
	for _, x := range []domain.Session{a, b} {
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestUpsertUsageIdempotent(t *testing.T) {
	s := usageFixture(t)
	rows := []domain.UsageRow{
		{SessionID: "claude:1", MessageID: "m1", Model: "a", Input: 1, At: time.Now()},
		{SessionID: "claude:1", MessageID: "m2", Model: "a", Input: 2, At: time.Now()},
	}
	for i := 0; i < 2; i++ {
		if err := s.UpsertUsage(rows); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM usage`).Scan(&n)
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
}

func TestTotalsUnpriced(t *testing.T) {
	s := usageFixture(t)
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	rows := []domain.UsageRow{
		{SessionID: "claude:1", MessageID: "m1", Model: "priced", Input: 1_000_000, At: at},
		{SessionID: "claude:1", MessageID: "m2", Model: "nopriced", Input: 500, At: at},
		{SessionID: "codex:1", MessageID: "m3", Model: "nopriced", Output: 7, At: at.Add(time.Hour)},
		{SessionID: "codex:1", MessageID: "m4", Model: "priced", Input: 1_000_000, At: at.AddDate(0, 0, 10)},
	}
	if err := s.UpsertUsage(rows); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPrice(domain.Price{Model: "priced", InputPerM: 3}); err != nil {
		t.Fatal(err)
	}
	f := TotalsFilter{From: at.Add(-time.Hour), To: at.AddDate(0, 0, 1), GroupBy: "agent"}
	got, err := s.Totals(f)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	by := map[string]Total{}
	for _, x := range got {
		by[x.Key] = x
	}
	cl, cx := by["claude"], by["codex"]
	if !cl.Unpriced || cl.CostUSD != 3 || cl.Input != 1_000_500 || cl.Sessions != 1 {
		t.Fatalf("claude %+v", cl)
	}
	if !cx.Unpriced || cx.CostUSD != 0 || cx.Output != 7 {
		t.Fatalf("codex %+v", cx)
	}
	f.GroupBy = "model"
	got, _ = s.Totals(f)
	for _, x := range got {
		if x.Key == "priced" && x.Unpriced || x.Key == "nopriced" && !x.Unpriced {
			t.Fatalf("model %+v", x)
		}
	}
	f.GroupBy = "card"
	got, _ = s.Totals(f)
	hasKey := false
	for _, x := range got {
		if x.Key == "NAC-1" {
			hasKey = true
		}
	}
	if !hasKey {
		t.Fatalf("card %+v", got)
	}
	f.GroupBy = "day_agent"
	got, _ = s.Totals(f)
	for _, x := range got {
		if x.Day == "" || x.Agent == "" {
			t.Fatalf("day_agent %+v", x)
		}
	}
	f.GroupBy = "bogus"
	if _, err := s.Totals(f); err == nil {
		t.Fatal("want error")
	}
}

func TestPricesLimitsAndOffset(t *testing.T) {
	s := usageFixture(t)
	s.PutPrice(domain.Price{Model: "m", InputPerM: 1})
	s.PutPrice(domain.Price{Model: "m", InputPerM: 2})
	p, _ := s.ListPrices()
	if len(p) != 1 || p[0].InputPerM != 2 {
		t.Fatalf("%+v", p)
	}
	s.DeletePrice("m")
	p, _ = s.ListPrices()
	if len(p) != 0 {
		t.Fatal(p)
	}
	if err := s.PutLimit(domain.Limit{Agent: "claude", DailyUSD: 10, WarnPct: 80, Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.ListLimits()
	if len(l) != 1 || l[0].Action != "pause" {
		t.Fatalf("%+v", l)
	}
	if err := s.SetTranscriptOffset("claude:1", 99); err != nil {
		t.Fatal(err)
	}
	var off int64
	s.DB.QueryRow(`SELECT transcript_offset FROM sessions WHERE id='claude:1'`).Scan(&off)
	if off != 99 {
		t.Fatal(off)
	}
}
