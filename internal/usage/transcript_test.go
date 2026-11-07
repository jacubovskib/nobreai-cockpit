package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func copyFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/main.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "main.jsonl")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseDedupesMessageID(t *testing.T) {
	p := copyFixture(t)
	rows, off, err := ParseClaudeTranscript(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if len(rows) != 2 || off != st.Size() {
		t.Fatalf("rows=%d off=%d size=%d", len(rows), off, st.Size())
	}
	m := map[string]int64{}
	for _, r := range rows {
		m[r.MessageID] = r.Output
		if r.Model != "claude-opus-5-5" || r.At.IsZero() {
			t.Fatalf("row %+v", r)
		}
	}
	if m["msg_1"] != 290 || m["msg_2"] != 10 {
		t.Fatalf("%v", m)
	}
	if rows[0].Input != 2 || rows[0].CacheWrite != 27305 || rows[0].CacheRead != 28950 {
		t.Fatalf("%+v", rows[0])
	}
}

func TestParseIncremental(t *testing.T) {
	p := copyFixture(t)
	_, off, _ := ParseClaudeTranscript(p, 0)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-11-05T16:00:00Z","message":{"id":"msg_3","model":"m","usage":{"input_tokens":1,"output_tokens":2}}}` + "\n")
	f.Close()
	rows, off2, err := ParseClaudeTranscript(p, off)
	st, _ := os.Stat(p)
	if err != nil || len(rows) != 1 || rows[0].MessageID != "msg_3" || off2 != st.Size() {
		t.Fatalf("%+v %d %v", rows, off2, err)
	}
}

func TestParseIgnoresPartialLastLine(t *testing.T) {
	p := copyFixture(t)
	_, off, _ := ParseClaudeTranscript(p, 0)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-11-05T16:00:00Z","message":{"id":"msg_4","usage":{"output_tokens":2}}}`)
	f.Close()
	rows, off2, err := ParseClaudeTranscript(p, off)
	if err != nil || len(rows) != 0 || off2 != off {
		t.Fatalf("%+v %d %d %v", rows, off2, off, err)
	}
}

func TestSubagentTranscripts(t *testing.T) {
	got := SubagentTranscripts("testdata/main.jsonl")
	if len(got) != 1 || filepath.Base(got[0]) != "agent-x.jsonl" {
		t.Fatalf("%v", got)
	}
}

func TestParseLineWithoutTimestampUsesMtime(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	line := `{"type":"assistant","sessionId":"s1","message":{"id":"msg_9","model":"m","usage":{"input_tokens":3,"output_tokens":4}}}` + "\n"
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	rows, _, err := ParseClaudeTranscript(p, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("linha sem timestamp descartada: %+v %v", rows, err)
	}
	if rows[0].Output != 4 || !rows[0].At.Equal(mtime) {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestParseOffsetBeyondSizeRestarts(t *testing.T) {
	p := copyFixture(t)
	st, _ := os.Stat(p)
	rows, off, err := ParseClaudeTranscript(p, st.Size()+1000)
	if err != nil || len(rows) != 2 || off != st.Size() {
		t.Fatalf("arquivo truncado/rotacionado deveria ser relido do início: rows=%d off=%d err=%v", len(rows), off, err)
	}
}
