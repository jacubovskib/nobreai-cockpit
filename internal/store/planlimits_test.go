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
	if _, err := s.RecordPlanLimits(domain.PlanReading{SessionID: "a", At: t0, FiveHour: win(23.5, rs)}); err != nil {
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
	if f.UsedPct != 23.5 || f.ResetsAt == nil || !f.ResetsAt.Equal(rs) || !f.ReceivedAt.Equal(t0) {
		t.Fatalf("five_hour = %+v", f)
	}
}

func TestPlanLimitsCoalescePerClockMinute(t *testing.T) {
	s := newTestStore(t)
	rs := t0.Add(2 * time.Hour)
	rec := func(at time.Time, pct float64, resets time.Time) {
		t.Helper()
		if _, err := s.RecordPlanLimits(domain.PlanReading{SessionID: "a", At: at, SevenDay: win(pct, resets)}); err != nil {
			t.Fatal(err)
		}
	}
	rec(t0.Add(5*time.Second), 10, rs)
	rec(t0.Add(30*time.Second), 12, rs)                // mesmo minuto: coalesce
	rec(t0.Add(50*time.Second), 11, rs)                // menor que o máximo: descartada
	rec(t0.Add(70*time.Second), 13, rs)                // minuto seguinte: nova linha
	rec(t0.Add(80*time.Second), 14, rs.Add(time.Hour)) // resets_at mudou: nova linha
	h, err := s.PlanLimitsHistory("seven_day", t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 3 || h[0].UsedPct != 12 || h[1].UsedPct != 13 || h[2].UsedPct != 14 {
		t.Fatalf("history = %+v", h)
	}
	if !h[0].At.Equal(t0.Add(30 * time.Second)) {
		t.Fatalf("received_at da linha coalescida = %v", h[0].At)
	}
}

func TestPlanLimitsSameMinuteKeepsMax(t *testing.T) {
	s := newTestStore(t)
	rs := t0.Add(2 * time.Hour)
	// Leituras de sessões diferentes no mesmo minuto: uma linha com o maior.
	s.RecordPlanLimits(domain.PlanReading{SessionID: "a", At: t0.Add(10 * time.Second), FiveHour: win(30, rs)})
	s.RecordPlanLimits(domain.PlanReading{SessionID: "b", At: t0.Add(40 * time.Second), FiveHour: win(30, rs)})
	var n int
	var pct float64
	s.DB.QueryRow(`SELECT COUNT(*), MAX(used_pct) FROM plan_limits`).Scan(&n, &pct)
	if n != 1 || pct != 30 {
		t.Fatalf("n=%d pct=%v", n, pct)
	}
}

func TestPlanLimitsActiveUseOneRowPerMinute(t *testing.T) {
	s := newTestStore(t)
	rs := t0.Add(5 * time.Hour)
	// POST a cada 20 s por 30 min com uso crescente.
	for i := 0; i < 90; i++ {
		at := t0.Add(time.Duration(i*20) * time.Second)
		if _, err := s.RecordPlanLimits(domain.PlanReading{SessionID: "a", At: at, FiveHour: win(float64(i)/2, rs)}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM plan_limits`).Scan(&n)
	if n != 30 {
		t.Fatalf("linhas = %d, quero 30", n)
	}
	m, _ := s.LatestPlanLimits()
	if m["five_hour"].UsedPct != 44.5 {
		t.Fatalf("vigente = %+v", m["five_hour"])
	}
}

func TestPlanLimitsIdleSessionsDoNotFlap(t *testing.T) {
	s := newTestStore(t)
	rs := t0.Add(3 * time.Hour)
	// Duas sessões ociosas reenviam o rate_limits cacheado a cada 61 s:
	// uma com 85 %, a outra (antiga) com 40 %, mesmo resets_at.
	for i := 0; i < 20; i++ {
		at := t0.Add(time.Duration(i*61) * time.Second)
		pct, sid := 85.0, "nova"
		if i%2 == 1 {
			pct, sid = 40, "velha"
		}
		if _, err := s.RecordPlanLimits(domain.PlanReading{SessionID: sid, At: at, FiveHour: win(pct, rs)}); err != nil {
			t.Fatal(err)
		}
		m, _ := s.LatestPlanLimits()
		if i > 0 && m["five_hour"].UsedPct != 85 {
			t.Fatalf("passo %d: vigente = %+v", i, m["five_hour"])
		}
	}
	h, _ := s.PlanLimitsHistory("five_hour", t0.Add(-time.Hour))
	for _, p := range h {
		if p.UsedPct != 85 {
			t.Fatalf("histórico com a leitura velha: %+v", h)
		}
	}
}

func TestPlanLimitsStaleResetsAtDoesNotOverride(t *testing.T) {
	s := newTestStore(t)
	cur := t0.Add(3 * time.Hour)
	past := t0.Add(-2 * time.Hour)
	s.RecordPlanLimits(domain.PlanReading{SessionID: "nova", At: t0, FiveHour: win(20, cur)})
	// Sessão parada desde a janela anterior (resets_at no passado, % alto).
	changed, err := s.RecordPlanLimits(domain.PlanReading{SessionID: "velha", At: t0.Add(2 * time.Minute), FiveHour: win(95, past)})
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	m, _ := s.LatestPlanLimits()
	if f := m["five_hour"]; f.UsedPct != 20 || !f.ResetsAt.Equal(cur) {
		t.Fatalf("vigente = %+v", f)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM plan_limits`).Scan(&n)
	if n != 1 {
		t.Fatalf("a leitura velha não deveria ser gravada: %d linhas", n)
	}
	// Nova janela (resets_at mais novo) vira a vigente mesmo com % menor.
	next := cur.Add(5 * time.Hour)
	if changed, _ := s.RecordPlanLimits(domain.PlanReading{At: t0.Add(4 * time.Hour), FiveHour: win(3, next)}); !changed {
		t.Fatal("nova janela deveria mudar a vigente")
	}
	m, _ = s.LatestPlanLimits()
	if f := m["five_hour"]; f.UsedPct != 3 || !f.ResetsAt.Equal(next) {
		t.Fatalf("vigente = %+v", f)
	}
}

func TestPlanLimitsChangedOnlyWhenVigenteChanges(t *testing.T) {
	s := newTestStore(t)
	rs := t0.Add(3 * time.Hour)
	rec := func(at time.Time, r domain.PlanReading) bool {
		t.Helper()
		r.At = at
		c, err := s.RecordPlanLimits(r)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if !rec(t0, domain.PlanReading{FiveHour: win(10, rs)}) {
		t.Fatal("primeira leitura muda")
	}
	if rec(t0.Add(2*time.Minute), domain.PlanReading{FiveHour: win(10, rs)}) {
		t.Fatal("mesmo valor não muda")
	}
	if rec(t0.Add(3*time.Minute), domain.PlanReading{FiveHour: win(5, rs)}) {
		t.Fatal("leitura descartada não muda")
	}
	if !rec(t0.Add(4*time.Minute), domain.PlanReading{FiveHour: win(10, rs), SevenDay: win(1, rs.Add(48*time.Hour))}) {
		t.Fatal("janela nova muda")
	}
	if !rec(t0.Add(5*time.Minute), domain.PlanReading{FiveHour: win(11, rs)}) {
		t.Fatal("% maior muda")
	}
}

func TestPlanLimitsNullResetsAtOnlyWithoutOthers(t *testing.T) {
	s := newTestStore(t)
	// Só leituras sem resets_at: vale a mais recente (por received_at), mesmo menor.
	s.RecordPlanLimits(domain.PlanReading{At: t0, SevenDay: &domain.PlanWindow{UsedPct: 50}})
	s.RecordPlanLimits(domain.PlanReading{At: t0.Add(5 * time.Minute), SevenDay: &domain.PlanWindow{UsedPct: 30}})
	m, _ := s.LatestPlanLimits()
	if m["seven_day"].UsedPct != 30 {
		t.Fatalf("vigente = %+v", m["seven_day"])
	}
	// Com uma leitura com resets_at, a nula deixa de valer.
	rs := t0.Add(48 * time.Hour)
	s.RecordPlanLimits(domain.PlanReading{At: t0.Add(6 * time.Minute), SevenDay: win(20, rs)})
	s.RecordPlanLimits(domain.PlanReading{At: t0.Add(8 * time.Minute), SevenDay: &domain.PlanWindow{UsedPct: 90}})
	m, _ = s.LatestPlanLimits()
	if f := m["seven_day"]; f.UsedPct != 20 || f.ResetsAt == nil {
		t.Fatalf("vigente = %+v", f)
	}
}

func TestPlanLimitsLatestByResetsAtAcrossFormats(t *testing.T) {
	s := newTestStore(t)
	// Tamanhos diferentes de fração de segundo em resets_at: comparação
	// textual erraria a ordem.
	// ("...:01Z" > "...:01.5Z" como texto, mas 01.5 s vem depois de 01 s.)
	older := t0.Add(time.Hour + time.Second)
	newer := t0.Add(time.Hour + 1500*time.Millisecond)
	s.RecordPlanLimits(domain.PlanReading{SessionID: "y", At: t0, FiveHour: win(60, older)})
	s.RecordPlanLimits(domain.PlanReading{SessionID: "x", At: t0.Add(30 * time.Minute), FiveHour: win(50, newer)})
	m, _ := s.LatestPlanLimits()
	if f := m["five_hour"]; f.UsedPct != 50 || !f.ResetsAt.Equal(newer) {
		t.Fatalf("latest = %+v", f)
	}
}

func TestPlanLimitsHistorySince(t *testing.T) {
	s := newTestStore(t)
	past := time.Now().Add(-time.Hour)
	s.RecordPlanLimits(domain.PlanReading{At: time.Now().Add(-2 * time.Hour), FiveHour: win(90, past)})
	s.RecordPlanLimits(domain.PlanReading{At: time.Now().Add(-30 * time.Hour), SevenDay: &domain.PlanWindow{UsedPct: 5}})
	m, _ := s.LatestPlanLimits()
	if m["five_hour"].Expired || m["seven_day"].ResetsAt != nil {
		t.Fatalf("o store não calcula Expired: %+v", m)
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

func TestPlanLimitsRetention30Days(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s.RecordPlanLimits(domain.PlanReading{At: now.Add(-31 * 24 * time.Hour), FiveHour: &domain.PlanWindow{UsedPct: 1}})
	s.RecordPlanLimits(domain.PlanReading{At: now.Add(-29 * 24 * time.Hour), FiveHour: &domain.PlanWindow{UsedPct: 2}})
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM plan_limits`).Scan(&n)
	if n != 2 {
		t.Fatalf("antes: %d", n)
	}
	s.RecordPlanLimits(domain.PlanReading{At: now, FiveHour: &domain.PlanWindow{UsedPct: 3}})
	s.DB.QueryRow(`SELECT COUNT(*) FROM plan_limits`).Scan(&n)
	if n != 2 {
		t.Fatalf("a de 31 dias deveria sair: %d", n)
	}
}
