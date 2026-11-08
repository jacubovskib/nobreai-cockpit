package store

import (
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func win(pct float64, resets time.Time) *domain.PlanWindow {
	return &domain.PlanWindow{UsedPct: pct, ResetsAt: &resets}
}

func TestPlanLimitsRecordAndLatest(t *testing.T) {
	s := newTestStore(t)
	if m, err := s.LatestPlanLimits(); err != nil || len(m) != 0 {
		t.Fatalf("vazio = %v, %v", m, err)
	}
	rs := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	if err := s.RecordPlanLimits(domain.PlanReading{SessionID: "a", At: t0, FiveHour: win(23.5, rs)}); err != nil {
		t.Fatal(err)
	}
	m, err := s.LatestPlanLimits()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["seven_day"].UsedPct != 0 {
		t.Fatalf("só five_hour: %+v", m)
	}
	f := m["five_hour"]
	if f.UsedPct != 23.5 || f.ResetsAt == nil || !f.ResetsAt.Equal(rs) || !f.ReceivedAt.Equal(t0) || f.Expired {
		t.Fatalf("five_hour = %+v", f)
	}
}

func TestPlanLimitsCoalesce(t *testing.T) {
	s := newTestStore(t)
	rs := t0.Add(2 * time.Hour)
	rec := func(at time.Time, pct float64, resets time.Time) {
		t.Helper()
		if err := s.RecordPlanLimits(domain.PlanReading{SessionID: "a", At: at, SevenDay: win(pct, resets)}); err != nil {
			t.Fatal(err)
		}
	}
	rec(t0, 10, rs)
	rec(t0.Add(30*time.Second), 11, rs)                              // coalesce
	rec(t0.Add(50*time.Second), 12, rs)                              // coalesce com a anterior (20 s)
	rec(t0.Add(3*time.Minute), 13, rs)                               // > 60 s: nova linha
	rec(t0.Add(3*time.Minute+10*time.Second), 14, rs.Add(time.Hour)) // resets_at mudou: nova linha
	h, err := s.PlanLimitsHistory("seven_day", t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 3 || h[0].UsedPct != 12 || h[1].UsedPct != 13 || h[2].UsedPct != 14 {
		t.Fatalf("history = %+v", h)
	}
	if !h[0].At.Equal(t0.Add(50 * time.Second)) {
		t.Fatalf("received_at da linha coalescida = %v", h[0].At)
	}
}

func TestPlanLimitsLatestByReceivedAtAcrossFormats(t *testing.T) {
	s := newTestStore(t)
	rs := t0.Add(time.Hour)
	// Tamanhos diferentes de fração de segundo: comparação textual erraria.
	a := t0.Add(1500 * time.Millisecond)
	b := t0.Add(10 * time.Minute)
	s.RecordPlanLimits(domain.PlanReading{SessionID: "x", At: b, FiveHour: win(50, rs)})
	s.RecordPlanLimits(domain.PlanReading{SessionID: "y", At: a, FiveHour: win(40, rs)})
	m, _ := s.LatestPlanLimits()
	if m["five_hour"].UsedPct != 50 {
		t.Fatalf("latest = %+v", m["five_hour"])
	}
}

func TestPlanLimitsExpiredAndHistorySince(t *testing.T) {
	s := newTestStore(t)
	past := time.Now().Add(-time.Hour)
	s.RecordPlanLimits(domain.PlanReading{At: time.Now().Add(-2 * time.Hour), FiveHour: win(90, past)})
	s.RecordPlanLimits(domain.PlanReading{At: time.Now().Add(-30 * time.Hour), SevenDay: &domain.PlanWindow{UsedPct: 5}})
	m, _ := s.LatestPlanLimits()
	if !m["five_hour"].Expired {
		t.Fatalf("deveria estar expirada: %+v", m["five_hour"])
	}
	if sd := m["seven_day"]; sd.Expired || sd.ResetsAt != nil {
		t.Fatalf("sem resets_at nunca expira: %+v", sd)
	}
	h, _ := s.PlanLimitsHistory("seven_day", time.Now().Add(-24*time.Hour))
	if len(h) != 0 {
		t.Fatalf("since deveria excluir: %+v", h)
	}
	h, _ = s.PlanLimitsHistory("seven_day", time.Now().Add(-48*time.Hour))
	if len(h) != 1 {
		t.Fatalf("history = %+v", h)
	}
}
