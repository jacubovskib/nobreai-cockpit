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

func TestParseSkipsZeroTokenMessages(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	// Mensagens sintéticas do Claude Code (erro de API, "No response
	// requested.") vêm com model "<synthetic>" e uso zerado: não são uso, e
	// sem preço deixariam a sessão inteira "sem preço".
	data := `{"type":"assistant","timestamp":"2026-11-05T16:00:00Z","message":{"id":"msg_s","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-11-05T16:00:01Z","message":{"id":"msg_r","model":"m","usage":{"input_tokens":1,"output_tokens":0}}}` + "\n"
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, _, err := ParseClaudeTranscript(p, 0)
	if err != nil || len(rows) != 1 || rows[0].MessageID != "msg_r" {
		t.Fatalf("%+v %v", rows, err)
	}
}

// Until para no limite dado (a leitura de contexto chegou até ali): com o
// limite no fim da 4ª linha só msg_1 sai (a última das suas linhas) e msg_2,
// depois do limite, fica para a próxima leitura; limite no meio de uma linha
// não lê a linha cortada; limite no offset não lê nada.
func TestParseUntilStopsAtLimit(t *testing.T) {
	p := copyFixture(t)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var nl []int64 // posição logo depois de cada \n
	for i, c := range b {
		if c == '\n' {
			nl = append(nl, int64(i+1))
		}
	}
	limit := nl[3]
	rows, off, err := ParseClaudeTranscriptUntil(p, 0, limit)
	if err != nil {
		t.Fatal(err)
	}
	if off != limit || len(rows) != 1 || rows[0].MessageID != "msg_1" || rows[0].Output != 290 {
		t.Fatalf("até o limite: off=%d (quer %d) rows=%+v", off, limit, rows)
	}
	if rows, off2, err := ParseClaudeTranscriptUntil(p, off, off); err != nil || len(rows) != 0 || off2 != off {
		t.Fatalf("limite no offset: rows=%d off=%d err=%v", len(rows), off2, err)
	}
	mid := nl[len(nl)-2] + 10 // dentro da linha de msg_2
	if rows, off2, err := ParseClaudeTranscriptUntil(p, off, mid); err != nil || len(rows) != 0 || off2 != nl[len(nl)-2] {
		t.Fatalf("limite no meio da linha: rows=%+v off=%d err=%v", rows, off2, err)
	}
	rest, off3, err := ParseClaudeTranscriptUntil(p, off, -1)
	if err != nil || off3 != int64(len(b)) || len(rest) != 1 || rest[0].MessageID != "msg_2" {
		t.Fatalf("resto: rows=%+v off=%d size=%d err=%v", rest, off3, len(b), err)
	}
}
