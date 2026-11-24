package store

import (
	"errors"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

func ctxTurns(seg, from, to int) []usage.ContextTurn {
	var ts []usage.ContextTurn
	for i := from; i <= to; i++ {
		ts = append(ts, usage.ContextTurn{Turn: i, Segment: seg, MessageID: "m" + string(rune('a'+seg)) + string(rune('0'+i)),
			At: ctxAt.Add(time.Duration(i) * time.Minute), Tokens: int64(100 * (i + 1))})
	}
	return ts
}

func ctxOut(id, tool, key string, tokens, in int64, turn, seg int) usage.ToolOutput {
	return usage.ToolOutput{ItemID: id, Tool: tool, Key: key, Bytes: tokens * 4, Tokens: tokens, InTokens: in,
		Turn: turn, Segment: seg, At: ctxAt, Result: true}
}

// ctxSession cria uma sessão com transcript principal "/main".
func ctxSession(t *testing.T, s *Store, id string, card, project *int64) {
	t.Helper()
	x := newSession(id, 1)
	x.TranscriptPath = "/main"
	x.CardID, x.ProjectID = card, project
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
}

func save(t *testing.T, s *Store, sid, path string, outs []usage.ToolOutput, turns []usage.ContextTurn) {
	t.Helper()
	if err := s.SaveContext(sid, path, false, outs, turns, 1, usage.ContextState{LastTurn: -1}); err != nil {
		t.Fatal(err)
	}
}

func TestContextRereads(t *testing.T) {
	s := newTestStore(t)
	ctxSession(t, s, "s1", nil, nil)
	save(t, s, "s1", "/main", []usage.ToolOutput{
		ctxOut("a", "Bash", "a", 10, 0, 1, 0),
		ctxOut("b", "Bash", "b", 10, 0, 4, 1),
		ctxOut("c", "Bash", "c", 10, 0, -1, 0),
		ctxOut("d", "Bash", "d", 10, 0, 5, 1),
	}, append(ctxTurns(0, 0, 3), ctxTurns(1, 4, 5)...))
	_, top, err := s.ContextSessionDetail("s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, o := range top {
		got[o.Key] = o.Rereads
	}
	want := map[string]int64{"a": 2, "b": 1, "c": 4, "d": 0}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("rereads[%s] = %d, want %d", k, got[k], w)
		}
	}
}

func TestContextRereads_AfterCompaction(t *testing.T) {
	s := newTestStore(t)
	ctxSession(t, s, "s1", nil, nil)
	save(t, s, "s1", "/main", []usage.ToolOutput{ctxOut("h", "hook", "SessionStart · cockpit", 20, 5, 1, 1)},
		append(ctxTurns(0, 0, 1), ctxTurns(1, 2, 4)...))
	_, top, err := s.ContextSessionDetail("s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top[0].Rereads != 3 || top[0].Weight != 75 {
		t.Fatalf("top = %+v", top)
	}
}

func TestContextByProjectAndTotals(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("a", "/w/a")
	p2, _ := s.CreateProject("b", "/w/b")
	ctxSession(t, s, "s1", nil, &p1.ID)
	ctxSession(t, s, "s2", nil, &p2.ID)
	save(t, s, "s1", "/main", []usage.ToolOutput{ctxOut("1", "Bash", "git status", 10, 0, 0, 0), ctxOut("2", "Read", "Read .go", 50, 0, 0, 0)}, ctxTurns(0, 0, 2))
	save(t, s, "s2", "/main", []usage.ToolOutput{ctxOut("3", "Bash", "go test", 5, 5, 0, 0)}, ctxTurns(0, 0, 1))
	got, err := s.ContextByProject(ContextFilter{})
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// p1: (10+50)*2=120, top Read .go; p2: 10*1=10.
	if got[0].ProjectID != p1.ID || got[0].Weight != 120 || got[0].TopKey != "Read .go" || got[0].TopTool != "Read" || got[1].ProjectID != p2.ID || got[1].Weight != 10 {
		t.Fatalf("got=%+v", got)
	}
	tot, err := s.ContextTotals(ContextFilter{})
	if err != nil || tot.ContextTotal != 100+200+300+100+200 || tot.WeightTotal != 130 {
		t.Fatalf("tot=%+v err=%v", tot, err)
	}
	// Intervalo: só o turno 0 (at = ctxAt) de cada sessão; peso não depende do intervalo de turnos.
	tot, _ = s.ContextTotals(ContextFilter{From: ctxAt, To: ctxAt.Add(30 * time.Second)})
	if tot.ContextTotal != 200 || tot.WeightTotal != 130 {
		t.Fatalf("tot intervalo=%+v", tot)
	}
	tot, _ = s.ContextTotals(ContextFilter{ProjectIDs: []int64{p2.ID}})
	if tot.ContextTotal != 300 || tot.WeightTotal != 10 {
		t.Fatalf("tot projeto=%+v", tot)
	}
}

func TestContextTopKeys(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("a", "/w/a")
	p2, _ := s.CreateProject("b", "/w/b")
	card, _ := s.CreateCard(domain.Card{Title: "x", ProjectID: p2.ID})
	ctxSession(t, s, "s1", nil, &p1.ID)
	ctxSession(t, s, "s2", &card.ID, &p1.ID) // card (p2) vence a sessão (p1)
	late := ctxOut("l", "Bash", "ls", 1000, 0, 0, 0)
	late.At = ctxAt.Add(48 * time.Hour)
	save(t, s, "s1", "/main", []usage.ToolOutput{
		ctxOut("1", "Bash", "ls", 10, 0, 0, 0), ctxOut("2", "Bash", "ls", 30, 0, 0, 0),
		ctxOut("3", "Read", "Read .go", 100, 0, 0, 0), late}, ctxTurns(0, 0, 1))
	save(t, s, "s2", "/main", []usage.ToolOutput{ctxOut("4", "Bash", "ls", 20, 0, 0, 0)}, ctxTurns(0, 0, 1))

	keys, err := s.ContextTopKeys(ContextFilter{To: ctxAt.Add(time.Hour)}, 10)
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys=%+v err=%v", keys, err)
	}
	if keys[0].Key != "Read .go" || keys[0].Weight != 100 || keys[1].Key != "ls" || keys[1].Count != 3 || keys[1].Tokens != 60 || keys[1].AvgTokens != 20 || keys[1].Weight != 60 {
		t.Fatalf("keys=%+v", keys)
	}
	if keys, _ = s.ContextTopKeys(ContextFilter{To: ctxAt.Add(time.Hour)}, 1); len(keys) != 1 {
		t.Fatalf("limit: %+v", keys)
	}
	keys, _ = s.ContextTopKeys(ContextFilter{ProjectIDs: []int64{p1.ID}, To: ctxAt.Add(time.Hour)}, 10)
	if len(keys) != 2 || keys[1].Count != 2 {
		t.Fatalf("p1 (sem card): %+v", keys)
	}
	keys, _ = s.ContextTopKeys(ContextFilter{ProjectIDs: []int64{p2.ID}}, 10)
	if len(keys) != 1 || keys[0].Count != 1 || keys[0].Tokens != 20 {
		t.Fatalf("p2 (pelo card): %+v", keys)
	}
}

func TestContextTopSessions(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("a", "/w/a")
	card, _ := s.CreateCard(domain.Card{Title: "titulo", ProjectID: p.ID})
	ctxSession(t, s, "s1", &card.ID, nil)
	ctxSession(t, s, "s2", nil, &p.ID)
	save(t, s, "s1", "/main", []usage.ToolOutput{ctxOut("1", "Bash", "ls", 10, 0, 0, 0)}, append(ctxTurns(0, 0, 1), ctxTurns(1, 2, 3)...))
	// Subagente: turnos e segmentos maiores não contam para pico/compactações.
	save(t, s, "s1", "/sub", []usage.ToolOutput{ctxOut("2", "Read", "Read .go", 1, 0, 0, 5)}, []usage.ContextTurn{
		{Turn: 0, Segment: 5, MessageID: "x", At: ctxAt, Tokens: 99999}, {Turn: 1, Segment: 5, MessageID: "y", At: ctxAt, Tokens: 99999}})
	save(t, s, "s2", "/main", []usage.ToolOutput{ctxOut("3", "Bash", "ls", 1, 0, 0, 0)}, ctxTurns(0, 0, 1))
	got, err := s.ContextTopSessions(ContextFilter{}, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	a := got[0]
	// Bash: segmento 0 tem turnos 0..1 -> 1 releitura; Read sub: seg 5, max 1, turn 0 -> 1.
	if a.SessionID != "s1" || a.PeakContext != 400 || a.Compactions != 1 || a.Weight != 11 || a.CardKey != card.Key || a.CardTitle != "titulo" || a.StartedAt == "" {
		t.Fatalf("a=%+v", a)
	}
	if got[1].SessionID != "s2" || got[1].CardKey != "" || got[1].Weight != 1 {
		t.Fatalf("b=%+v", got[1])
	}
	if got, _ = s.ContextTopSessions(ContextFilter{}, 1); len(got) != 1 {
		t.Fatalf("limit: %+v", got)
	}
	if got, _ = s.ContextTopSessions(ContextFilter{}, 0); len(got) != 2 {
		t.Fatalf("limit 0 deve devolver tudo: %+v", got)
	}
	if keys, _ := s.ContextTopKeys(ContextFilter{}, -1); len(keys) != 2 {
		t.Fatalf("limit -1 deve devolver tudo: %+v", keys)
	}
	// Intervalo: as saídas ficam em ctxAt; fora do intervalo nenhuma sessão.
	if got, _ = s.ContextTopSessions(ContextFilter{From: ctxAt.Add(time.Hour)}, 10); len(got) != 0 {
		t.Fatalf("From: %+v", got)
	}
	if got, _ = s.ContextTopSessions(ContextFilter{To: ctxAt}, 10); len(got) != 0 {
		t.Fatalf("To exclusivo: %+v", got)
	}
	if got, _ = s.ContextTopSessions(ContextFilter{From: ctxAt, To: ctxAt.Add(time.Hour)}, 10); len(got) != 2 {
		t.Fatalf("intervalo: %+v", got)
	}
}

func TestContextSessionDetail(t *testing.T) {
	s := newTestStore(t)
	ctxSession(t, s, "s1", nil, nil)
	save(t, s, "s1", "/main", []usage.ToolOutput{ctxOut("1", "Bash", "ls", 10, 0, 0, 0), ctxOut("2", "Bash", "go test", 5, 0, 0, 0)},
		append(append(ctxTurns(0, 0, 1), ctxTurns(1, 2, 3)...), ctxTurns(2, 4, 4)...))
	save(t, s, "s1", "/sub", []usage.ToolOutput{ctxOut("3", "Agent", "Agent geral", 1000, 40, 0, 0)}, ctxTurns(0, 0, 3))
	turns, top, err := s.ContextSessionDetail("s1", 2)
	if err != nil || len(turns) != 5 {
		t.Fatalf("turns=%+v err=%v", turns, err)
	}
	for i, tp := range turns {
		if tp.Turn != i || tp.AfterCompact != (i == 2 || i == 4) {
			t.Fatalf("turns[%d]=%+v", i, tp)
		}
	}
	if len(top) != 2 || top[0].Key != "Agent geral" || !top[0].Subagent || top[0].Rereads != 3 || top[0].InTokens != 40 || top[0].Weight != 3120 || top[1].Subagent {
		t.Fatalf("top=%+v", top)
	}
	if _, _, err := s.ContextSessionDetail("nada", 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestContextEmpty(t *testing.T) {
	s := newTestStore(t)
	ctxSession(t, s, "s1", nil, nil)
	f := ContextFilter{}
	keys, e1 := s.ContextTopKeys(f, 5)
	sess, e2 := s.ContextTopSessions(f, 5)
	proj, e3 := s.ContextByProject(f)
	tot, e4 := s.ContextTotals(f)
	turns, top, e5 := s.ContextSessionDetail("s1", 5)
	for _, e := range []error{e1, e2, e3, e4, e5} {
		if e != nil {
			t.Fatal(e)
		}
	}
	if keys == nil || sess == nil || proj == nil || turns == nil || top == nil || tot != (ContextTotals{}) {
		t.Fatalf("nil: %v %v %v %v %v %+v", keys == nil, sess == nil, proj == nil, turns == nil, top == nil, tot)
	}
}

// detailWindows cria a principal claude:p e a filha claude:p@2 com turnos e
// saídas intercalados (m* = principal, c* = filha).
func detailWindows(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	ctxSession(t, s, "claude:p", nil, nil)
	child := newSession("claude:p@2", 2)
	child.ParentSessionID = "claude:p"
	child.TranscriptPath = "/main"
	if err := s.StartSession(child); err != nil {
		t.Fatal(err)
	}
	turns := []usage.ContextTurn{
		liveTurn(0, 0, 5*time.Minute, 1000, 0, 0, "", "m0"),
		liveTurn(1, 0, 4*time.Minute, 2000, 0, 0, "", "c0"),
		liveTurn(2, 0, 3*time.Minute, 3000, 0, 0, "", "m1"),
		liveTurn(3, 0, 2*time.Minute, 4000, 0, 0, "", "c1"),
	}
	om := ctxOut("om", "Read", "main-key", 10, 0, 0, 0)
	om.UUID = "m0"
	oc := ctxOut("oc", "Bash", "child-key", 20, 0, 1, 0)
	oc.UUID = "c0"
	w := ContextWindows{ByUUID: map[string]string{"c0": "claude:p@2", "c1": "claude:p@2"}}
	if err := s.SaveContextWindows("claude:p", "/main", false, []usage.ToolOutput{om, oc}, turns, 1, usage.ContextState{LastTurn: -1}, w); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestContextSessionDetailPerWindow(t *testing.T) {
	s := detailWindows(t)
	main, mtop, err := s.ContextSessionDetail("claude:p", 10)
	if err != nil || len(main) != 2 || main[0].Tokens != 1000 || main[1].Tokens != 3000 || len(mtop) != 1 || mtop[0].Key != "main-key" {
		t.Fatalf("principal turns=%+v top=%+v err=%v", main, mtop, err)
	}
	ch, ctop, err := s.ContextSessionDetail("claude:p@2", 10)
	if err != nil || len(ch) != 2 || ch[0].Tokens != 2000 || ch[1].Tokens != 4000 || len(ctop) != 1 || ctop[0].Key != "child-key" {
		t.Fatalf("filha turns=%+v top=%+v err=%v", ch, ctop, err)
	}
	for _, p := range append(main, ch...) {
		if p.AfterCompact || p.PctOnly {
			t.Fatalf("ponto %+v", p)
		}
	}
}

func TestContextSessionDetailPctOnly(t *testing.T) {
	s := newTestStore(t)
	ctxSession(t, s, "k", nil, nil)
	save(t, s, "k", "/main", nil, []usage.ContextTurn{pctTurn(0, 10), pctTurn(1, 14), pctTurn(2, 18)})
	turns, _, err := s.ContextSessionDetail("k", 5)
	if err != nil || len(turns) != 3 {
		t.Fatalf("%+v %v", turns, err)
	}
	for i, p := range turns {
		if !p.PctOnly || p.Tokens != 0 || p.Pct != []float64{10, 14, 18}[i] {
			t.Fatalf("turns[%d]=%+v", i, p)
		}
	}
	// Claude (pct NULL) não é só %.
	ctxSession(t, s, "c", nil, nil)
	save(t, s, "c", "/main", nil, ctxTurns(0, 0, 0))
	ct, _, _ := s.ContextSessionDetail("c", 5)
	if len(ct) != 1 || ct[0].PctOnly || ct[0].Pct != 0 {
		t.Fatalf("%+v", ct)
	}
}

// compactWindows cria a principal claude:p e a filha claude:p@2 no mesmo
// transcript: a filha compacta (segmento 1) e depois a principal (segmento 2).
// Turnos: m* da principal, c* da filha; a principal tem também um turno só no
// subagente /sub com tokens enormes (não conta para o pico).
func compactWindows(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	ctxSession(t, s, "claude:p", nil, nil)
	child := newSession("claude:p@2", 2)
	child.ParentSessionID = "claude:p"
	child.TranscriptPath = "/main"
	if err := s.StartSession(child); err != nil {
		t.Fatal(err)
	}
	turns := []usage.ContextTurn{
		liveTurn(0, 0, 9*time.Minute, 1000, 0, 0, "", "m0"),
		liveTurn(1, 0, 8*time.Minute, 5000, 0, 0, "", "c0"),
		liveTurn(2, 1, 7*time.Minute, 800, 0, 0, "", "c1"),
		liveTurn(3, 1, 6*time.Minute, 2000, 0, 0, "", "m1"),
		liveTurn(4, 2, 5*time.Minute, 900, 0, 0, "", "m2"),
		liveTurn(5, 2, 4*time.Minute, 1200, 0, 0, "", "c2"),
	}
	byUUID := map[string]string{"c0": "claude:p@2", "c1": "claude:p@2", "c2": "claude:p@2"}
	w := ContextWindows{ByUUID: byUUID, Compactions: map[int]string{1: "claude:p@2", 2: ""}}
	if err := s.SaveContextWindows("claude:p", "/main", false, nil, turns, 1, usage.ContextState{LastTurn: -1}, w); err != nil {
		t.Fatal(err)
	}
	return s
}

// A curva de cada janela só marca as compactações da própria janela.
func TestContextSessionDetailCompactionPerWindow(t *testing.T) {
	s := compactWindows(t)
	for _, c := range []struct {
		id   string
		want []bool
	}{
		{"claude:p", []bool{false, false, true}},   // m0, m1 (compactação da filha), m2 (dela)
		{"claude:p@2", []bool{false, true, false}}, // c0, c1 (dela), c2 (compactação da principal)
	} {
		turns, _, err := s.ContextSessionDetail(c.id, 10)
		if err != nil || len(turns) != len(c.want) {
			t.Fatalf("%s: turns=%+v err=%v", c.id, turns, err)
		}
		for i, p := range turns {
			if p.AfterCompact != c.want[i] {
				t.Errorf("%s: turns[%d]=%+v, quer after_compact=%v", c.id, i, p, c.want[i])
			}
		}
	}
}

// A lista de sessões é por janela: a filha aparece com o próprio card, pico
// e compactações; a principal não conta os turnos nem as compactações da
// filha. O projeto (filtro e totais) é o da janela dona.
func TestContextTopSessionsPerWindow(t *testing.T) {
	s := compactWindows(t)
	p1, _ := s.CreateProject("a", "/w/a")
	p2, _ := s.CreateProject("b", "/w/b")
	mc, _ := s.CreateCard(domain.Card{Title: "principal", ProjectID: p1.ID})
	cc, _ := s.CreateCard(domain.Card{Title: "filha", ProjectID: p2.ID})
	if _, err := s.DB.Exec(`UPDATE sessions SET card_id=? WHERE id='claude:p'`, mc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE sessions SET card_id=? WHERE id='claude:p@2'`, cc.ID); err != nil {
		t.Fatal(err)
	}
	// Saídas: a da principal no turno 0 (segmento 0), a da filha no turno 2
	// (segmento 1); uma de subagente da principal com turno enorme em /sub.
	om := ctxOut("om", "Read", "main-key", 10, 0, 0, 0)
	om.UUID = "m0"
	oc := ctxOut("oc", "Bash", "child-key", 20, 0, 2, 1)
	oc.UUID = "c1"
	w := ContextWindows{ByUUID: map[string]string{"c0": "claude:p@2", "c1": "claude:p@2", "c2": "claude:p@2"}}
	if err := s.SaveContextWindows("claude:p", "/main", false, []usage.ToolOutput{om, oc}, nil, 1, usage.ContextState{LastTurn: -1}, w); err != nil {
		t.Fatal(err)
	}
	save(t, s, "claude:p", "/sub", nil, []usage.ContextTurn{{Turn: 0, Segment: 7, MessageID: "x", At: ctxAt, Tokens: 99999}})

	got, err := s.ContextTopSessions(ContextFilter{}, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	by := map[string]ContextSession{}
	for _, x := range got {
		by[x.SessionID] = x
	}
	m, c := by["claude:p"], by["claude:p@2"]
	if m.PeakContext != 2000 || m.Compactions != 1 || m.CardKey != mc.Key || m.CardTitle != "principal" || m.StartedAt == "" {
		t.Errorf("principal = %+v", m)
	}
	if c.PeakContext != 5000 || c.Compactions != 1 || c.CardKey != cc.Key || c.CardTitle != "filha" || c.StartedAt == "" || c.Weight == 0 {
		t.Errorf("filha = %+v", c)
	}
	// Filtro de projeto pela janela dona.
	if got, _ := s.ContextTopSessions(ContextFilter{ProjectIDs: []int64{p2.ID}}, 10); len(got) != 1 || got[0].SessionID != "claude:p@2" {
		t.Errorf("projeto da filha = %+v", got)
	}
	if got, _ := s.ContextTopSessions(ContextFilter{ProjectIDs: []int64{p1.ID}}, 10); len(got) != 1 || got[0].SessionID != "claude:p" {
		t.Errorf("projeto da principal = %+v", got)
	}
	if keys, _ := s.ContextTopKeys(ContextFilter{ProjectIDs: []int64{p2.ID}}, 10); len(keys) != 1 || keys[0].Key != "child-key" {
		t.Errorf("chaves do projeto da filha = %+v", keys)
	}
	tot, err := s.ContextTotals(ContextFilter{ProjectIDs: []int64{p2.ID}})
	if err != nil || tot.ContextTotal != 5000+800+1200 || tot.WeightTotal != c.Weight {
		t.Errorf("totais do projeto da filha = %+v err=%v", tot, err)
	}
}

// Sessão só com % (Kiro) e sem saídas entra na lista pelos turnos, com o pico em %.
func TestContextTopSessionsPctOnlyWithoutOutputs(t *testing.T) {
	s := newTestStore(t)
	ctxSession(t, s, "k", nil, nil)
	save(t, s, "k", "/main", nil, []usage.ContextTurn{pctTurn(0, 10), pctTurn(1, 34), pctTurn(2, 18)})
	got, err := s.ContextTopSessions(ContextFilter{}, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got[0].SessionID != "k" || got[0].PeakPct != 34 || got[0].PeakContext != 0 || got[0].Weight != 0 || got[0].Compactions != 0 {
		t.Fatalf("kiro = %+v", got[0])
	}
	// Fora do intervalo dos turnos, nada.
	if got, _ := s.ContextTopSessions(ContextFilter{From: liveNow}, 10); len(got) != 0 {
		t.Fatalf("fora do intervalo = %+v", got)
	}
}
