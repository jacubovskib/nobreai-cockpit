package store

import (
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

var ctxAt = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

type outRow struct {
	Tool, Key         string
	Bytes, Tokens, In int64
	Turn, Segment     int
	IsError           bool
}

func getOut(t *testing.T, s *Store, session, item string) (outRow, int) {
	t.Helper()
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM tool_outputs WHERE session_id=? AND item_id=?`, session, item).Scan(&n)
	var r outRow
	s.DB.QueryRow(`SELECT tool, key, bytes, tokens, in_tokens, turn, segment, is_error FROM tool_outputs WHERE session_id=? AND item_id=?`,
		session, item).Scan(&r.Tool, &r.Key, &r.Bytes, &r.Tokens, &r.In, &r.Turn, &r.Segment, &r.IsError)
	return r, n
}

func TestContextCursor_Empty(t *testing.T) {
	s := usageFixture(t)
	off, st, err := s.ContextCursor("claude:1", "/p.jsonl")
	if err != nil || off != 0 || st != (usage.ContextState{LastTurn: -1}) {
		t.Fatalf("off=%d st=%+v err=%v", off, st, err)
	}
}

func TestSaveContext_RoundTrip(t *testing.T) {
	s := usageFixture(t)
	if err := s.SetFileOffset("claude:1", "/p.jsonl", 999); err != nil {
		t.Fatal(err)
	}
	turns := []usage.ContextTurn{{Turn: 0, MessageID: "m0", At: ctxAt, Tokens: 10}, {Turn: 1, Segment: 1, MessageID: "m1", At: ctxAt, Tokens: 20}}
	outs := []usage.ToolOutput{{ItemID: "t1", Tool: "Bash", Key: "ls", Bytes: 8, Tokens: 2, Turn: 0, At: ctxAt, Result: true}}
	if err := s.SaveContext("claude:1", "/p.jsonl", false, outs, turns, 500, usage.ContextState{LastTurn: 1, LastMessageID: "m1", Segment: 1}); err != nil {
		t.Fatal(err)
	}
	off, st, err := s.ContextCursor("claude:1", "/p.jsonl")
	if err != nil || off != 500 || st.LastTurn != 1 || st.LastMessageID != "m1" || st.Segment != 1 {
		t.Fatalf("off=%d st=%+v err=%v", off, st, err)
	}
	if u, _ := s.FileOffset("claude:1", "/p.jsonl"); u != 999 {
		t.Fatalf("offset de uso mudou: %d", u)
	}
	// Sem linha prévia, o offset de uso nasce 0.
	if err := s.SaveContext("claude:1", "/q.jsonl", false, nil, nil, 7, usage.ContextState{LastTurn: -1}); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.FileOffset("claude:1", "/q.jsonl"); u != 0 {
		t.Fatalf("u=%d", u)
	}
	// SetFileOffset não apaga o cursor de contexto.
	if err := s.SetFileOffset("claude:1", "/p.jsonl", 1000); err != nil {
		t.Fatal(err)
	}
	if off, _, _ := s.ContextCursor("claude:1", "/p.jsonl"); off != 500 {
		t.Fatalf("tool_offset=%d", off)
	}
}

func TestSaveContext_UpsertTurn(t *testing.T) {
	s := usageFixture(t)
	st := usage.ContextState{LastTurn: 1}
	if err := s.SaveContext("claude:1", "/p", false, nil, []usage.ContextTurn{{Turn: 1, MessageID: "m1", At: ctxAt, Tokens: 10}}, 1, st); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContext("claude:1", "/p", false, nil, []usage.ContextTurn{{Turn: 1, MessageID: "m1", At: ctxAt, Tokens: 99}}, 2, st); err != nil {
		t.Fatal(err)
	}
	var n int
	var tok int64
	s.DB.QueryRow(`SELECT COUNT(*), MAX(tokens) FROM context_turns`).Scan(&n, &tok)
	if n != 1 || tok != 99 {
		t.Fatalf("n=%d tok=%d", n, tok)
	}
}

func TestSaveContext_KeepCompleted(t *testing.T) {
	s := usageFixture(t)
	st := usage.ContextState{LastTurn: -1}
	full := usage.ToolOutput{ItemID: "t1", Tool: "Read", Key: "a.go", Bytes: 400, Tokens: 100, Turn: 0, At: ctxAt, Result: true}
	use := usage.ToolOutput{ItemID: "t1", Tool: "Read", Key: "a.go", Turn: 0, At: ctxAt}
	for _, o := range []usage.ToolOutput{full, use} {
		if err := s.SaveContext("claude:1", "/p", false, []usage.ToolOutput{o}, nil, 1, st); err != nil {
			t.Fatal(err)
		}
	}
	if r, n := getOut(t, s, "claude:1", "t1"); n != 1 || r.Bytes != 400 || r.Tokens != 100 {
		t.Fatalf("%+v n=%d", r, n)
	}
}

func TestSaveContext_MergeUseAndResult(t *testing.T) {
	s := usageFixture(t)
	st := usage.ContextState{LastTurn: -1}
	use := usage.ToolOutput{ItemID: "t1", Tool: "Bash", Key: "git status", InTokens: 5, Turn: 2, At: ctxAt}
	res := usage.ToolOutput{ItemID: "t1", Bytes: 400, Tokens: 100, Turn: 3, At: ctxAt.Add(time.Hour), Result: true, IsError: true}
	for _, o := range []usage.ToolOutput{use, res} {
		if err := s.SaveContext("claude:1", "/p", false, []usage.ToolOutput{o}, nil, 1, st); err != nil {
			t.Fatal(err)
		}
	}
	r, n := getOut(t, s, "claude:1", "t1")
	want := outRow{Tool: "Bash", Key: "git status", Bytes: 400, Tokens: 100, In: 5, Turn: 2, IsError: true}
	if n != 1 || r != want {
		t.Fatalf("%+v n=%d", r, n)
	}
	var at string
	s.DB.QueryRow(`SELECT at FROM tool_outputs WHERE item_id='t1'`).Scan(&at)
	if at != fmtTime(ctxAt) {
		t.Fatalf("at=%s", at)
	}
	// Resultado sozinho de item novo: "?".
	if err := s.SaveContext("claude:1", "/p", false, []usage.ToolOutput{{ItemID: "t2", Bytes: 4, Tokens: 1, Turn: 1, At: ctxAt, Result: true}}, nil, 1, st); err != nil {
		t.Fatal(err)
	}
	if r, _ := getOut(t, s, "claude:1", "t2"); r.Tool != "?" || r.Key != "?" {
		t.Fatalf("%+v", r)
	}
}

func TestSaveContext_Reset(t *testing.T) {
	s := usageFixture(t)
	st := usage.ContextState{LastTurn: 0}
	for _, p := range []string{"/a", "/b"} {
		o := usage.ToolOutput{ItemID: "i" + p, Tool: "Bash", Key: "x", Bytes: 4, Tokens: 1, At: ctxAt, Result: true}
		if err := s.SaveContext("claude:1", p, false, []usage.ToolOutput{o}, []usage.ContextTurn{{Turn: 0, MessageID: "m", At: ctxAt, Tokens: 1}}, 10, st); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveContext("claude:1", "/a", true, nil, nil, 0, usage.ContextState{LastTurn: -1}); err != nil {
		t.Fatal(err)
	}
	var o, tu int
	s.DB.QueryRow(`SELECT COUNT(*) FROM tool_outputs WHERE path='/a'`).Scan(&o)
	s.DB.QueryRow(`SELECT COUNT(*) FROM context_turns WHERE path='/a'`).Scan(&tu)
	if o != 0 || tu != 0 {
		t.Fatalf("/a o=%d tu=%d", o, tu)
	}
	s.DB.QueryRow(`SELECT COUNT(*) FROM tool_outputs WHERE path='/b'`).Scan(&o)
	s.DB.QueryRow(`SELECT COUNT(*) FROM context_turns WHERE path='/b'`).Scan(&tu)
	if o != 1 || tu != 1 {
		t.Fatalf("/b o=%d tu=%d", o, tu)
	}
}

func TestClaudeTranscripts(t *testing.T) {
	s := newTestStore(t)
	for _, x := range []struct{ id, agent, path string }{{"claude:1", "claude", "/a.jsonl"}, {"claude:2", "claude", ""}, {"codex:1", "codex", "/c.jsonl"}} {
		d := newSession(x.id, 1)
		d.Agent, d.TranscriptPath = x.agent, x.path
		if err := s.StartSession(d); err != nil {
			t.Fatal(err)
		}
	}
	m, err := s.ClaudeTranscripts()
	if err != nil || len(m) != 1 || m["claude:1"] != "/a.jsonl" {
		t.Fatalf("%v %v", m, err)
	}
}

func TestSaveContext_CacheColumns(t *testing.T) {
	s := usageFixture(t)
	st := usage.ContextState{LastTurn: 0}
	first := usage.ContextTurn{Turn: 0, MessageID: "m", At: ctxAt, Tokens: 6, Input: 1, CacheRead: 2, CacheWrite: 3, TTL: "5m", UUID: "a1"}
	// A mesma mensagem continuada numa leitura seguinte troca o detalhe.
	second := usage.ContextTurn{Turn: 0, MessageID: "m", At: ctxAt, Tokens: 60, Input: 10, CacheRead: 20, CacheWrite: 30, TTL: "1h", UUID: "a2"}
	for _, tu := range []usage.ContextTurn{first, second} {
		if err := s.SaveContext("claude:1", "/p", false, nil, []usage.ContextTurn{tu}, 1, st); err != nil {
			t.Fatal(err)
		}
	}
	var in, cr, cw int64
	var ttl, win string
	if err := s.DB.QueryRow(`SELECT input, cache_read, cache_write, ttl, window_session_id FROM context_turns WHERE session_id='claude:1' AND path='/p' AND turn=0`).
		Scan(&in, &cr, &cw, &ttl, &win); err != nil {
		t.Fatal(err)
	}
	if in != 10 || cr != 20 || cw != 30 || ttl != "1h" || win != "" {
		t.Fatalf("in=%d cr=%d cw=%d ttl=%q win=%q", in, cr, cw, ttl, win)
	}
}

func TestResetContextCursors(t *testing.T) {
	s := usageFixture(t)
	if err := s.SetFileOffset("claude:1", "/p", 999); err != nil {
		t.Fatal(err)
	}
	o := usage.ToolOutput{ItemID: "t1", Tool: "Bash", Key: "x", Bytes: 4, Tokens: 1, At: ctxAt, Result: true}
	if err := s.SaveContext("claude:1", "/p", false, []usage.ToolOutput{o}, []usage.ContextTurn{{Turn: 3, MessageID: "m", At: ctxAt, Tokens: 1}},
		500, usage.ContextState{LastTurn: 3, LastMessageID: "m", Segment: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetContextCursors(); err != nil {
		t.Fatal(err)
	}
	// Nada é apagado: a releitura faz upsert por cima.
	var o2, tu int
	s.DB.QueryRow(`SELECT COUNT(*) FROM tool_outputs`).Scan(&o2)
	s.DB.QueryRow(`SELECT COUNT(*) FROM context_turns`).Scan(&tu)
	if o2 != 1 || tu != 1 {
		t.Fatalf("o=%d tu=%d", o2, tu)
	}
	// Cursor no começo: o estado também volta ao começo, mesmo com turnos
	// gravados (senão a releitura numeraria os turnos depois do último).
	off, st, err := s.ContextCursor("claude:1", "/p")
	if err != nil || off != 0 || st != (usage.ContextState{LastTurn: -1}) {
		t.Fatalf("off=%d st=%+v err=%v", off, st, err)
	}
	// O offset de uso fica.
	if u, _ := s.FileOffset("claude:1", "/p"); u != 999 {
		t.Fatalf("offset de uso mudou: %d", u)
	}
}

func TestMigration017_Schema(t *testing.T) {
	s := usageFixture(t)
	for _, q := range []string{
		`SELECT input, cache_read, cache_write, ttl, window_session_id FROM context_turns`,
		`SELECT window_session_id FROM tool_outputs`,
		`SELECT window_session_id FROM usage`,
		`SELECT session_id, path, uuid, window_session_id FROM window_lines`,
		`SELECT id, session_id, text_hash, at, used FROM window_prompts`,
		`SELECT session_id, warned_pct, warned_segment, big_outputs, child_notice_sent FROM session_alerts`,
	} {
		rows, err := s.DB.Query(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		rows.Close()
	}
	// Padrões de session_alerts e window_prompts.
	if _, err := s.DB.Exec(`INSERT INTO session_alerts(session_id) VALUES ('x')`); err != nil {
		t.Fatal(err)
	}
	var pct, seg, big, child int
	s.DB.QueryRow(`SELECT warned_pct, warned_segment, big_outputs, child_notice_sent FROM session_alerts WHERE session_id='x'`).Scan(&pct, &seg, &big, &child)
	if pct != 0 || seg != -1 || big != 0 || child != 0 {
		t.Fatalf("padrões: %d %d %d %d", pct, seg, big, child)
	}
	if _, err := s.DB.Exec(`INSERT INTO window_lines(session_id, path, uuid, window_session_id) VALUES ('a','/p','u','b')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO window_lines(session_id, path, uuid, window_session_id) VALUES ('a','/p','u','c')`); err == nil {
		t.Fatal("chave de window_lines não é (session_id, path, uuid)")
	}
}

func TestMigration018_Schema(t *testing.T) {
	s := usageFixture(t)
	if _, err := s.DB.Exec(`INSERT INTO context_compactions(session_id, path, segment) VALUES ('a','/p',1)`); err != nil {
		t.Fatal(err)
	}
	var win string
	if err := s.DB.QueryRow(`SELECT window_session_id FROM context_compactions`).Scan(&win); err != nil || win != "" {
		t.Fatalf("padrão de window_session_id: %q %v", win, err)
	}
	if _, err := s.DB.Exec(`INSERT INTO context_compactions(session_id, path, segment, window_session_id) VALUES ('a','/p',1,'b')`); err == nil {
		t.Fatal("chave de context_compactions não é (session_id, path, segment)")
	}
}

// A janela de um subagente: a primeira gravação vale (uma releitura do
// principal com a linha do resultado na principal não devolve o subagente da
// filha para a principal), e só a janela filha corrige o que já foi gravado.
func TestSaveContext_SubagentWindowFirstWins(t *testing.T) {
	s := usageFixture(t)
	sub := "/p/subagents/agent-a1.jsonl"
	if _, ok, err := s.SubagentWindow("claude:1", sub); err != nil || ok {
		t.Fatalf("antes: ok=%v err=%v", ok, err)
	}
	save := func(subs map[string]string) {
		t.Helper()
		if err := s.SaveContextWindows("claude:1", "/p.jsonl", false, nil, nil, 10, usage.ContextState{LastTurn: -1},
			ContextWindows{Subagents: subs}); err != nil {
			t.Fatal(err)
		}
	}
	save(map[string]string{sub: "claude:1@200"})
	save(map[string]string{sub: ""})
	if w, ok, err := s.SubagentWindow("claude:1", sub); err != nil || !ok || w != "claude:1@200" {
		t.Fatalf("janela = %q ok=%v err=%v", w, ok, err)
	}
}
