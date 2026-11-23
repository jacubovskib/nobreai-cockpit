package kiro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// histEnv é um adaptador com sessão criada e um ~/.kiro/sessions temporário.
type histEnv struct {
	*env
	dir  string // <SessionsDir>/<hash>/<session_id>
	file string // messages.jsonl
}

func newHist(t *testing.T) *histEnv {
	t.Helper()
	e := newEnv(t)
	root := t.TempDir()
	e.a.SetPaths(Paths{SessionsDir: root})
	e.a.retries = nil
	e.call(t, "SessionStart", "session_start", 4242)
	dir := filepath.Join(root, "0123456789abcdef", providerID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &histEnv{env: e, dir: dir, file: filepath.Join(dir, "messages.jsonl")}
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (h *histEnv) write(t *testing.T, b []byte) {
	t.Helper()
	if err := os.WriteFile(h.file, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *histEnv) append(t *testing.T, b []byte) {
	t.Helper()
	f, err := os.OpenFile(h.file, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (h *histEnv) sess(t *testing.T) domain.Session {
	t.Helper()
	s, err := h.st.GetSession(sessID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// read faz uma leitura síncrona, como a goroutine do Ingest.
func (h *histEnv) read(t *testing.T) bool {
	t.Helper()
	changed, err := h.a.readLocked(h.sess(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

type usageRow struct {
	msg, model string
	credits    float64
}

func (h *histEnv) usage(t *testing.T) []usageRow {
	t.Helper()
	rs, err := h.st.DB.Query(`SELECT message_id, model, credits FROM usage WHERE session_id=? ORDER BY at, message_id`, sessID)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []usageRow
	for rs.Next() {
		var u usageRow
		if err := rs.Scan(&u.msg, &u.model, &u.credits); err != nil {
			t.Fatal(err)
		}
		out = append(out, u)
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

const (
	turn1 = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	turn2 = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func TestIngestCreditsAndContext(t *testing.T) {
	h := newHist(t)
	h.write(t, fixtureBytes(t, "messages.jsonl"))
	// O session.json mora ao lado do histórico.
	if err := os.WriteFile(filepath.Join(h.dir, "session.json"), fixtureBytes(t, "session.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !h.read(t) {
		t.Fatal("a primeira leitura não viu mudança")
	}
	u := h.usage(t)
	if len(u) != 2 || u[0].msg != turn1 || u[1].msg != turn2 {
		t.Fatalf("uso = %+v", u)
	}
	if !near(u[0].credits, 0.30) || !near(u[1].credits, 0.5) {
		t.Fatalf("créditos = %v e %v, quer 0.30 (0.25+0.05) e 0.5", u[0].credits, u[1].credits)
	}
	if u[0].model != "claude-sonnet-4.5" || h.sess(t).Model != "claude-sonnet-4.5" {
		t.Fatalf("modelo = %q / sessão %q", u[0].model, h.sess(t).Model)
	}
	turns, err := h.st.ContextTurnsOf(sessID, h.file)
	if err != nil || len(turns) != 2 {
		t.Fatalf("turnos = %+v err %v", turns, err)
	}
	// Vale o último contextUsage de cada execução (10 → 12.5), e o turno nasce
	// por executionId.
	if turns[0].Pct == nil || !near(*turns[0].Pct, 12.5) || turns[1].Pct == nil || !near(*turns[1].Pct, 14) {
		t.Fatalf("pct = %v e %v", turns[0].Pct, turns[1].Pct)
	}
	if turns[0].MessageID != "11111111-1111-4111-8111-111111111111" || turns[0].Segment != 0 || turns[1].Segment != 0 {
		t.Fatalf("turnos = %+v", turns)
	}
	var n int
	var tool, key string
	var tok int64
	var isErr bool
	if err := h.st.DB.QueryRow(`SELECT COUNT(*) FROM tool_outputs WHERE session_id=?`, sessID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("saídas = %d err %v", n, err)
	}
	if err := h.st.DB.QueryRow(`SELECT tool, key, tokens FROM tool_outputs WHERE item_id='call_1'`).Scan(&tool, &key, &tok); err != nil ||
		tool != "read_file" || key != "/w/api/a.txt" || tok != 100 {
		t.Fatalf("saída 1 = %q %q %d err %v", tool, key, tok, err)
	}
	if err := h.st.DB.QueryRow(`SELECT tool, is_error FROM tool_outputs WHERE item_id='call_2'`).Scan(&tool, &isErr); err != nil || tool != "execute_bash" || !isErr {
		t.Fatalf("saída 2 = %q erro %v err %v", tool, isErr, err)
	}
	if h.sess(t).TranscriptPath != h.file {
		t.Fatalf("transcript_path = %q, quer %q", h.sess(t).TranscriptPath, h.file)
	}
	live, err := h.st.SessionContext(sessID)
	if err != nil || !live.PctOnly || !near(live.Pct, 14) {
		t.Fatalf("contexto ao vivo = %+v err %v", live, err)
	}
}

func TestIngestRereadDoesNotDuplicate(t *testing.T) {
	h := newHist(t)
	h.write(t, fixtureBytes(t, "messages.jsonl"))
	h.read(t)
	if h.read(t) {
		t.Fatal("a releitura sem linhas novas disse que mudou")
	}
	if u := h.usage(t); len(u) != 2 {
		t.Fatalf("uso duplicado: %+v", u)
	}
	off, err := h.st.FileOffset(sessID, h.file)
	if err != nil || off != int64(len(fixtureBytes(t, "messages.jsonl"))) {
		t.Fatalf("cursor = %d err %v", off, err)
	}
	// Cursor zerado (releitura forçada): upsert, nada duplica.
	if err := h.st.SetFileOffset(sessID, h.file, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.DB.Exec(`UPDATE transcript_offsets SET tool_offset=0`); err != nil {
		t.Fatal(err)
	}
	h.read(t)
	if u := h.usage(t); len(u) != 2 {
		t.Fatalf("uso duplicado depois do reset: %+v", u)
	}
	if turns, _ := h.st.ContextTurnsOf(sessID, h.file); len(turns) != 2 {
		t.Fatalf("turnos duplicados: %+v", turns)
	}
}

func TestIngestMissingFileIsSilent(t *testing.T) {
	h := newHist(t)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	for i := 0; i < 3; i++ {
		if h.read(t) {
			t.Fatal("sem arquivo não há mudança")
		}
	}
	// Caminho guardado que sumiu (e nada novo no glob): também silencioso.
	if err := h.st.SetTranscriptPath(sessID, filepath.Join(h.dir, "sumiu.jsonl")); err != nil {
		t.Fatal(err)
	}
	h.read(t)
	if buf.Len() != 0 {
		t.Fatalf("log inesperado: %q", buf.String())
	}
	if len(h.usage(t)) != 0 {
		t.Fatal("uso sem arquivo")
	}
}

func TestIngestPartialLastLineKeepsCursor(t *testing.T) {
	h := newHist(t)
	full := fixtureBytes(t, "messages.jsonl")
	lines := bytes.SplitAfter(full, []byte("\n"))
	// Tudo até o usage_summary do turno 2 (linha 12), mais metade da turn_end.
	var head []byte
	for _, l := range lines[:11] {
		head = append(head, l...)
	}
	partial := lines[11][:len(lines[11])/2]
	h.write(t, append(append([]byte{}, head...), partial...))
	h.read(t)
	off, _ := h.st.FileOffset(sessID, h.file)
	if off != int64(len(head)) {
		t.Fatalf("cursor = %d, quer %d (sem a linha parcial)", off, len(head))
	}
	// Uma linha parcial de usage_summary nunca vira crédito pela metade.
	h.write(t, append(append([]byte{}, head[:len(head)-len(lines[10])]...), lines[10][:len(lines[10])-5]...))
	if err := h.st.SetFileOffset(sessID, h.file, 0); err != nil {
		t.Fatal(err)
	}
	h.st.DB.Exec(`UPDATE transcript_offsets SET tool_offset=0`)
	h.read(t)
	// O arquivo termina completo: o resto entra.
	h.write(t, full)
	h.read(t)
	if u := h.usage(t); len(u) != 2 || !near(u[1].credits, 0.5) {
		t.Fatalf("uso = %+v", u)
	}
	if off, _ := h.st.FileOffset(sessID, h.file); off != int64(len(full)) {
		t.Fatalf("cursor final = %d, quer %d", off, len(full))
	}
}

// Um turno pode atravessar duas leituras: o contextUsage de antes do Stop e o
// usage_summary de depois. O mesmo executionId atualiza o mesmo turno.
func TestIngestTurnAcrossReads(t *testing.T) {
	h := newHist(t)
	lines := bytes.SplitAfter(fixtureBytes(t, "messages.jsonl"), []byte("\n"))
	var first, rest []byte
	for i, l := range lines {
		if i < 9 { // até o contextUsage 14.0 do turno 2
			first = append(first, l...)
		} else {
			rest = append(rest, l...)
		}
	}
	h.write(t, first)
	h.read(t)
	if u := h.usage(t); len(u) != 1 || u[0].msg != turn1 {
		t.Fatalf("uso depois da 1ª leitura = %+v (o turno 2 ainda não tem usage_summary)", u)
	}
	if turns, _ := h.st.ContextTurnsOf(sessID, h.file); len(turns) != 2 {
		t.Fatalf("turnos = %+v", turns)
	}
	h.append(t, rest)
	h.read(t)
	if u := h.usage(t); len(u) != 2 || u[1].msg != turn2 || !near(u[1].credits, 0.5) {
		t.Fatalf("uso depois da 2ª leitura = %+v", u)
	}
	if turns, _ := h.st.ContextTurnsOf(sessID, h.file); len(turns) != 2 || *turns[1].Pct != 14 {
		t.Fatalf("turnos = %+v", turns)
	}
}

func ctxLine(exec string, pct float64) []byte {
	b, _ := json.Marshal(map[string]any{"id": "x", "timestamp": "2026-11-23T12:00:00.000Z", "payload": map[string]any{
		"type": "session_metadata", "key": "contextUsage", "value": map[string]any{"usagePercentage": pct}, "executionId": exec}})
	return append(b, '\n')
}

// Ruling 2: a % que cai para menos da metade da do turno anterior é uma nova
// compactação; os turnos seguintes ficam no segmento novo.
func TestIngestCompactionSegments(t *testing.T) {
	h := newHist(t)
	var b []byte
	for i, p := range []float64{40, 70, 30, 34, 10} {
		b = append(b, ctxLine(fmt.Sprintf("e%d", i), p)...)
	}
	h.write(t, b[:len(ctxLine("e0", 40))+len(ctxLine("e1", 70))])
	h.read(t)
	h.write(t, b) // as outras três chegam numa 2ª leitura
	h.read(t)
	turns, err := h.st.ContextTurnsOf(sessID, h.file)
	if err != nil || len(turns) != 5 {
		t.Fatalf("turnos = %+v err %v", turns, err)
	}
	// 70 → 30 é queda para menos da metade (35); 30 → 34 sobe; 34 → 10 cai de novo.
	want := []int{0, 0, 1, 1, 2}
	for i, tu := range turns {
		if tu.Segment != want[i] {
			t.Fatalf("segmentos = %v, quer %v", segs(turns), want)
		}
	}
	live, err := h.st.SessionContext(sessID)
	if err != nil || live.Compactions != 2 {
		t.Fatalf("contexto ao vivo = %+v err %v", live, err)
	}
}

func TestIngestTruncatedFileRestarts(t *testing.T) {
	h := newHist(t)
	h.write(t, fixtureBytes(t, "messages.jsonl"))
	h.read(t)
	h.write(t, ctxLine("novo", 5)) // arquivo menor que o cursor: trocado
	h.read(t)
	turns, _ := h.st.ContextTurnsOf(sessID, h.file)
	if len(turns) != 1 || turns[0].MessageID != "novo" {
		t.Fatalf("turnos = %+v", turns)
	}
}

func TestIngestUsageKeyFallsBackToExecutionID(t *testing.T) {
	h := newHist(t)
	line := `{"id":"u","timestamp":"2026-11-23T12:00:00.000Z","payload":{"type":"usage_summary","promptTurnSummaries":[{"unit":"credit","usage":1.5},{"unit":"bonus","usage":9}],"executionId":"exec-1"}}` + "\n"
	h.write(t, []byte(line))
	h.read(t)
	if u := h.usage(t); len(u) != 1 || u[0].msg != "exec-1" || !near(u[0].credits, 1.5) {
		t.Fatalf("uso = %+v", u)
	}
}

// O núcleo só chama o Ingest no TurnEnd e no Ended, e o usage_summary chega
// depois do Stop: o Kiro relê depois do TurnEnd, no prompt e no fim.
func TestIngestTriggers(t *testing.T) {
	lines := bytes.SplitAfter(fixtureBytes(t, "messages.jsonl"), []byte("\n"))
	upTo := func(n int) []byte { return bytes.Join(lines[:n], nil) }
	rest := func(n int) []byte { return bytes.Join(lines[n:], nil) }

	t.Run("TurnEnd relê depois de um atraso", func(t *testing.T) {
		h := newHist(t)
		h.a.retries = []time.Duration{30 * time.Millisecond}
		h.write(t, upTo(11)) // turno 2 sem o usage_summary
		h.a.Ingest(h.sess(t), agentcore.Event{Kind: agentcore.TurnEnd})
		h.append(t, rest(11)) // o kiro-cli grava o usage_summary depois do Stop
		waitFor(t, func() bool { return len(h.usage(t)) == 2 })
		h.a.Wait()
	})
	t.Run("Prompt pega o turno anterior", func(t *testing.T) {
		h := newHist(t)
		h.write(t, fixtureBytes(t, "messages.jsonl"))
		if got := h.a.PromptNote(h.sess(t), agentcore.Event{Kind: agentcore.Prompt}); got != "" {
			t.Fatalf("PromptNote = %q, não deve escrever no contexto", got)
		}
		h.a.Wait()
		if len(h.usage(t)) != 2 {
			t.Fatalf("uso = %+v", h.usage(t))
		}
	})
	t.Run("Ended lê o final", func(t *testing.T) {
		h := newHist(t)
		h.write(t, fixtureBytes(t, "messages.jsonl"))
		h.a.Ingest(h.sess(t), agentcore.Event{Kind: agentcore.Ended})
		h.a.Wait()
		if len(h.usage(t)) != 2 {
			t.Fatalf("uso = %+v", h.usage(t))
		}
	})
	t.Run("pela fiação do núcleo, no hook Stop", func(t *testing.T) {
		h := newHist(t)
		h.write(t, fixtureBytes(t, "messages.jsonl"))
		h.call(t, "Stop", "stop", 4242)
		h.a.Wait()
		if len(h.usage(t)) != 2 {
			t.Fatalf("uso = %+v", h.usage(t))
		}
	})
	t.Run("Wait cancela as releituras que esperam", func(t *testing.T) {
		h := newHist(t)
		h.a.retries = []time.Duration{time.Hour}
		h.write(t, fixtureBytes(t, "messages.jsonl"))
		h.a.Ingest(h.sess(t), agentcore.Event{Kind: agentcore.TurnEnd})
		done := make(chan struct{})
		go func() { h.a.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Wait ficou preso na espera da releitura")
		}
	})
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a condição não aconteceu a tempo")
}

func segs(ts []usage.ContextTurn) []int {
	var out []int
	for _, t := range ts {
		out = append(out, t.Segment)
	}
	return out
}

// O id da sessão vira parte do glob: nada sai da pasta nem casa com outra
// sessão.
func TestHistoryFileRejectsPathyIDs(t *testing.T) {
	h := newHist(t)
	h.write(t, fixtureBytes(t, "messages.jsonl"))
	s := h.sess(t)
	s.TranscriptPath = ""
	if h.a.historyFile(s) != h.file {
		t.Fatal("o id certo tem de achar o arquivo")
	}
	for _, id := range []string{"", ".", "..", "../x", "a/b", "*", "sess_*", "[a-z]*", "?"} {
		s.ProviderSessionID = id
		if p := h.a.historyFile(s); p != "" {
			t.Fatalf("id %q achou %q", id, p)
		}
	}
}
