package store

import (
	"errors"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

var liveNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// liveTurn é um turno com o detalhe de cache; at é relativo a liveNow.
func liveTurn(turn, seg int, ago time.Duration, tokens, cr, cw int64, ttl, uuid string) usage.ContextTurn {
	return usage.ContextTurn{Turn: turn, Segment: seg, MessageID: "m" + uuid, UUID: uuid, At: liveNow.Add(-ago),
		Tokens: tokens, Input: tokens - cr - cw, CacheRead: cr, CacheWrite: cw, TTL: ttl}
}

func liveSession(t *testing.T, s *Store, id, model string, lastEvent time.Duration) {
	t.Helper()
	x := newSession(id, 1)
	x.TranscriptPath = "/main"
	x.Model = model
	x.LastEventAt = liveNow.Add(-lastEvent)
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
}

func liveCtx(t *testing.T, s *Store, id string) ContextLive {
	t.Helper()
	c, err := s.SessionContextAt(id, liveNow)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSessionContextWindows(t *testing.T) {
	cases := []struct {
		model     string
		tokens    int64
		window    int64
		estimated bool
	}{
		{"claude-opus-5-5", 100000, 1000000, false}, // tabela
		{"claude-sonnet-x[1m]", 100000, 1000000, false},
		{"claude-sonnet-x", 100000, 200000, true},   // padrão
		{"", 100000, 200000, true},                  // desconhecido
		{"claude-sonnet-x", 250000, 1000000, false}, // observado acima do padrão
	}
	for _, c := range cases {
		s := newTestStore(t)
		liveSession(t, s, "s", c.model, time.Minute)
		save(t, s, "s", "/main", nil, []usage.ContextTurn{liveTurn(0, 0, time.Minute, c.tokens, 0, 0, "", "u0")})
		got := liveCtx(t, s, "s")
		if got.WindowTokens != c.window || got.WindowEstimated != c.estimated {
			t.Errorf("%q/%d: janela %d est=%v, quer %d est=%v", c.model, c.tokens, got.WindowTokens, got.WindowEstimated, c.window, c.estimated)
		}
	}
}

func TestSessionContextObservedFixed(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-sonnet-x", time.Minute)
	// Passou de 200k uma vez e depois caiu: a janela segue 1.000.000.
	save(t, s, "s", "/main", nil, []usage.ContextTurn{
		liveTurn(0, 0, 3*time.Minute, 250000, 0, 0, "", "a"),
		liveTurn(1, 1, 2*time.Minute, 20000, 0, 0, "", "b"),
	})
	got := liveCtx(t, s, "s")
	if got.WindowTokens != 1000000 || got.Tokens != 20000 || got.Pct != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestSessionContextGrowth(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-sonnet-x", time.Minute)
	// segmento 0 (antes da compactação) e segmento 1 com 4 turnos: 100k, 110k, 120k, 130k.
	save(t, s, "s", "/main", nil, []usage.ContextTurn{
		liveTurn(0, 0, 9*time.Minute, 150000, 0, 0, "", "a"),
		liveTurn(1, 1, 4*time.Minute, 100000, 0, 0, "", "b"),
		liveTurn(2, 1, 3*time.Minute, 110000, 0, 0, "", "c"),
		liveTurn(3, 1, 2*time.Minute, 120000, 0, 0, "", "d"),
		liveTurn(4, 1, 1*time.Minute, 130000, 0, 0, "", "e"),
	})
	got := liveCtx(t, s, "s")
	if got.Tokens != 130000 || got.Pct != 65 || got.Compactions != 1 || got.TurnsSinceCompact != 4 {
		t.Fatalf("%+v", got)
	}
	if got.GrowthPerTurn != 10000 {
		t.Errorf("crescimento = %d", got.GrowthPerTurn)
	}
	// até 95% de 200k = 190k: faltam 60k a 10k/turno.
	if got.TurnsToLimit != 6 {
		t.Errorf("turnos até o limite = %d", got.TurnsToLimit)
	}
}

func TestSessionContextFewTurnsNoGrowth(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-sonnet-x", time.Minute)
	save(t, s, "s", "/main", nil, []usage.ContextTurn{
		liveTurn(0, 0, 2*time.Minute, 10000, 0, 0, "", "a"),
		liveTurn(1, 0, time.Minute, 50000, 0, 0, "", "b"),
	})
	got := liveCtx(t, s, "s")
	if got.GrowthPerTurn != 0 || got.TurnsToLimit != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestSessionContextCacheHit(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-sonnet-x", time.Minute)
	var turns []usage.ContextTurn
	// 25 turnos: os 5 primeiros sem leitura de cache (fora dos últimos 20); os 20 últimos 90% de leitura.
	for i := 0; i < 25; i++ {
		if i < 5 {
			turns = append(turns, liveTurn(i, 0, time.Duration(30-i)*time.Minute, 1000, 0, 1000, "", "x"+string(rune('a'+i))))
		} else {
			turns = append(turns, liveTurn(i, 0, time.Duration(30-i)*time.Minute, 1000, 900, 50, "", "x"+string(rune('a'+i))))
		}
	}
	save(t, s, "s", "/main", nil, turns)
	got := liveCtx(t, s, "s")
	if got.CacheHitPct != 90 {
		t.Fatalf("hit = %v", got.CacheHitPct)
	}
}

func TestSessionContextRecentRewrite(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-sonnet-x", time.Minute)
	save(t, s, "s", "/main", nil, []usage.ContextTurn{
		liveTurn(0, 0, 90*time.Minute, 100000, 0, 90000, "", "a"), // velha demais
		liveTurn(1, 0, 30*time.Minute, 100000, 0, 49999, "", "b"), // abaixo do mínimo
		liveTurn(2, 0, 20*time.Minute, 120000, 0, 60000, "", "c"),
		liveTurn(3, 0, 10*time.Minute, 130000, 0, 70000, "", "d"),
		liveTurn(4, 0, 1*time.Minute, 140000, 100000, 100, "", "e"),
	})
	got := liveCtx(t, s, "s")
	if got.RecentRewrite == nil || got.RecentRewrite.Tokens != 70000 || !got.RecentRewrite.At.Equal(liveNow.Add(-10*time.Minute)) {
		t.Fatalf("regravação = %+v", got.RecentRewrite)
	}
	s2 := newTestStore(t)
	liveSession(t, s2, "s", "claude-sonnet-x", time.Minute)
	save(t, s2, "s", "/main", nil, []usage.ContextTurn{liveTurn(0, 0, 10*time.Minute, 100000, 0, 49999, "", "a")})
	if got := liveCtx(t, s2, "s"); got.RecentRewrite != nil {
		t.Fatalf("regravação = %+v", got.RecentRewrite)
	}
}

func TestSessionContextIdleOverTTL(t *testing.T) {
	cases := []struct {
		name   string
		ttl    string
		idle   time.Duration
		tokens int64
		want   bool
	}{
		{"5m passou", "5m", 6 * time.Minute, 250000, true},
		{"ttl vazio vale 5m", "", 6 * time.Minute, 250000, true},
		{"1h ainda dentro", "1h", 30 * time.Minute, 250000, false},
		{"1h passou", "1h", 61 * time.Minute, 250000, true},
		{"contexto pequeno", "5m", 6 * time.Minute, 200000, false},
		{"dentro de 5m", "5m", 4 * time.Minute, 250000, false},
	}
	for _, c := range cases {
		s := newTestStore(t)
		liveSession(t, s, "s", "claude-sonnet-x", c.idle)
		save(t, s, "s", "/main", nil, []usage.ContextTurn{liveTurn(0, 0, c.idle, c.tokens, 0, 0, c.ttl, "a")})
		got := liveCtx(t, s, "s")
		if got.IdleOverTTL != c.want || got.IdleSec != int64(c.idle.Seconds()) {
			t.Errorf("%s: over=%v idle=%d", c.name, got.IdleOverTTL, got.IdleSec)
		}
		if c.ttl != "" && got.TTL != c.ttl {
			t.Errorf("%s: ttl=%q", c.name, got.TTL)
		}
	}
}

func TestSessionContextTopOutputsPerWindow(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "claude:p", "claude-sonnet-x", time.Minute)
	child := newSession("claude:p@2", 2)
	child.ParentSessionID = "claude:p"
	child.Model = "claude-sonnet-x"
	child.LastEventAt = liveNow.Add(-time.Minute)
	if err := s.StartSession(child); err != nil {
		t.Fatal(err)
	}
	turns := []usage.ContextTurn{
		liveTurn(0, 0, 5*time.Minute, 1000, 0, 0, "", "m0"),
		liveTurn(1, 0, 4*time.Minute, 2000, 0, 0, "", "c0"),
		liveTurn(2, 0, 3*time.Minute, 3000, 0, 0, "", "m1"),
	}
	var outs []usage.ToolOutput
	for i, n := range []int64{10, 70, 20, 30, 40, 50, 60} {
		o := ctxOut("p"+string(rune('a'+i)), "Read", "k"+string(rune('a'+i)), n, 0, 0, 0)
		o.UUID = "m0"
		outs = append(outs, o)
	}
	co := ctxOut("child", "Bash", "kc", 999, 0, 1, 0)
	co.UUID = "c0"
	outs = append(outs, co)
	so := ctxOut("sub", "Bash", "ks", 5000, 0, 1, 0)
	so.UUID = "m0"
	w := ContextWindows{ByUUID: map[string]string{"c0": "claude:p@2"}}
	if err := s.SaveContextWindows("claude:p", "/main", false, outs, turns, 1, usage.ContextState{LastTurn: -1}, w); err != nil {
		t.Fatal(err)
	}
	// saída de subagente (outro path) não entra
	if err := s.SaveContext("claude:p", "/sub", false, []usage.ToolOutput{so}, nil, 1, usage.ContextState{LastTurn: -1}); err != nil {
		t.Fatal(err)
	}
	main := liveCtx(t, s, "claude:p")
	if main.Tokens != 3000 || len(main.TopOutputs) != 5 || main.TopOutputs[0].Tokens != 70 || main.TopOutputs[4].Tokens != 30 {
		t.Fatalf("principal = %+v", main)
	}
	ch := liveCtx(t, s, "claude:p@2")
	if ch.Tokens != 2000 || len(ch.TopOutputs) != 1 || ch.TopOutputs[0].Tokens != 999 {
		t.Fatalf("filha = %+v", ch)
	}
}

func TestSessionContextEmpty(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-opus-5-5", time.Minute)
	got := liveCtx(t, s, "s")
	if got.Tokens != 0 || got.Pct != 0 || got.GrowthPerTurn != 0 || got.TurnsToLimit != 0 || got.CacheHitPct != 0 ||
		got.RecentRewrite != nil || got.IdleOverTTL || got.TopOutputs == nil || len(got.TopOutputs) != 0 || got.WindowTokens != 1000000 {
		t.Fatalf("%+v", got)
	}
	if _, err := s.SessionContextAt("nada", liveNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionContextModelFromUsage(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "", time.Minute)
	if err := s.UpsertUsage([]domain.UsageRow{{SessionID: "s", MessageID: "m1", Model: "claude-opus-5-5", Input: 1, At: liveNow}}); err != nil {
		t.Fatal(err)
	}
	got := liveCtx(t, s, "s")
	if got.Model != "claude-opus-5-5" || got.WindowTokens != 1000000 {
		t.Fatalf("%+v", got)
	}
}

func TestSessionContextTTLFromLastWrite(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-sonnet-x", 30*time.Minute)
	save(t, s, "s", "/main", nil, []usage.ContextTurn{
		liveTurn(0, 0, 40*time.Minute, 250000, 0, 250000, "1h", "a"),
		liveTurn(1, 0, 30*time.Minute, 251000, 250000, 0, "", "b"), // só leitura
	})
	got := liveCtx(t, s, "s")
	if got.TTL != "1h" || got.IdleOverTTL {
		t.Fatalf("%+v", got)
	}
}

func TestSessionContextAtLimit(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "claude-sonnet-x", time.Minute)
	save(t, s, "s", "/main", nil, []usage.ContextTurn{
		liveTurn(0, 0, 3*time.Minute, 170000, 0, 0, "", "a"),
		liveTurn(1, 0, 2*time.Minute, 180000, 0, 0, "", "b"),
		liveTurn(2, 0, time.Minute, 190000, 0, 0, "", "c"),
	})
	got := liveCtx(t, s, "s")
	if !got.AtLimit || got.TurnsToLimit != 0 || got.GrowthPerTurn != 10000 {
		t.Fatalf("%+v", got)
	}
}

func TestGetSettingsNullModelWindowsKeepsDefaults(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutKV("model_windows", nil); err != nil {
		t.Fatal(err)
	}
	st, err := s.GetSettings()
	if err != nil || st.ModelWindows["claude-opus-5-5"] != 1000000 {
		t.Fatalf("%+v %v", st.ModelWindows, err)
	}
}

// Cada janela só conta as próprias compactações (context_compactions); o
// segmento do medidor é o do primeiro turno depois da última delas. reset
// apaga as compactações do path.
func TestSessionContextCompactionsPerWindow(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "claude:p", "claude-sonnet-x", time.Minute)
	child := newSession("claude:p@2", 2)
	child.ParentSessionID = "claude:p"
	child.LastEventAt = liveNow.Add(-time.Minute)
	if err := s.StartSession(child); err != nil {
		t.Fatal(err)
	}
	turns := []usage.ContextTurn{
		liveTurn(0, 0, 9*time.Minute, 1000, 0, 0, "", "m0"),
		liveTurn(1, 0, 8*time.Minute, 2000, 0, 0, "", "c0"),
		// compactação 1: da principal
		liveTurn(2, 1, 7*time.Minute, 1100, 0, 0, "", "m1"),
		liveTurn(3, 1, 6*time.Minute, 2100, 0, 0, "", "c1"),
		// compactação 2: da filha
		liveTurn(4, 2, 5*time.Minute, 300, 0, 0, "", "c2"),
		liveTurn(5, 2, 4*time.Minute, 1200, 0, 0, "", "m2"),
	}
	w := ContextWindows{
		ByUUID:      map[string]string{"c0": "claude:p@2", "c1": "claude:p@2", "c2": "claude:p@2"},
		Compactions: map[int]string{1: "", 2: "claude:p@2"},
	}
	if err := s.SaveContextWindows("claude:p", "/main", false, nil, turns, 1, usage.ContextState{LastTurn: 5, Segment: 2}, w); err != nil {
		t.Fatal(err)
	}
	main := liveCtx(t, s, "claude:p")
	if main.Compactions != 1 || main.TurnsSinceCompact != 2 || main.Segment != 1 {
		t.Fatalf("principal: compactações %d, turnos desde %d, segmento %d", main.Compactions, main.TurnsSinceCompact, main.Segment)
	}
	ch := liveCtx(t, s, "claude:p@2")
	if ch.Compactions != 1 || ch.TurnsSinceCompact != 1 || ch.Segment != 2 {
		t.Fatalf("filha: compactações %d, turnos desde %d, segmento %d", ch.Compactions, ch.TurnsSinceCompact, ch.Segment)
	}
	if err := s.SaveContextWindows("claude:p", "/main", true, nil, nil, 0, usage.ContextState{LastTurn: -1}, ContextWindows{}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM context_compactions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("reset deixou %d compactações (%v)", n, err)
	}
}

func pctTurn(turn int, pct float64) usage.ContextTurn {
	return usage.ContextTurn{Turn: turn, MessageID: "p" + string(rune('a'+turn)), At: liveNow.Add(-time.Duration(10-turn) * time.Minute), Pct: &pct}
}

func TestSessionContextPctOnly(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "auto", time.Minute)
	save(t, s, "s", "/main", nil, []usage.ContextTurn{pctTurn(0, 10), pctTurn(1, 14), pctTurn(2, 18)})
	got := liveCtx(t, s, "s")
	if !got.PctOnly || got.Pct != 18 || got.WindowTokens != 0 || got.Tokens != 0 ||
		got.GrowthPerTurn != 4 || got.TurnsToLimit != 19 || got.AtLimit || got.TurnsSinceCompact != 3 ||
		got.CacheHitPct != 0 || got.TTL != "" || got.IdleOverTTL || got.RecentRewrite != nil || got.WindowEstimated {
		t.Fatalf("%+v", got)
	}
	// Claude segue sem PctOnly (pct NULL).
	liveSession(t, s, "c", "claude-sonnet-x", time.Minute)
	save(t, s, "c", "/main", nil, []usage.ContextTurn{liveTurn(0, 0, time.Minute, 100000, 0, 0, "", "u0")})
	if c := liveCtx(t, s, "c"); c.PctOnly || c.Pct != 50 {
		t.Fatalf("%+v", c)
	}
}

func TestContextTotalsIgnoresPctOnly(t *testing.T) {
	s := newTestStore(t)
	liveSession(t, s, "s", "auto", time.Minute)
	save(t, s, "s", "/main", nil, []usage.ContextTurn{pctTurn(0, 10), liveTurn(1, 0, time.Minute, 500, 0, 0, "", "u1")})
	got, err := s.ContextTotals(ContextFilter{})
	if err != nil || got.ContextTotal != 500 {
		t.Fatalf("%+v %v", got, err)
	}
}
