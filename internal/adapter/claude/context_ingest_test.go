package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

const ctxSess = "claude:ctx"

func ctxAssistant(id, tool, toolID string) string {
	return `{"type":"assistant","uuid":"a-` + id + `","timestamp":"2026-11-18T10:00:00Z","message":{"id":"` + id +
		`","usage":{"input_tokens":10,"cache_read_input_tokens":100,"output_tokens":5},"content":[{"type":"tool_use","id":"` + toolID +
		`","name":"` + tool + `","input":{"command":"ls"}}]}}` + "\n"
}

func ctxResult(toolID, text string) string {
	return `{"type":"user","uuid":"u-` + toolID + `","timestamp":"2026-11-18T10:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"` +
		toolID + `","content":"` + text + `"}]}}` + "\n"
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func ctxCount(t *testing.T, e *env, table, path string) int {
	t.Helper()
	var n int
	if err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE session_id=? AND path=?`, ctxSess, path).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ctxEnv cria a sessão e dois transcripts (principal e subagente).
func ctxEnv(t *testing.T) (e *env, main, sub string) {
	e = newEnv(t)
	dir := t.TempDir()
	main = filepath.Join(dir, "main.jsonl")
	sub = filepath.Join(dir, "main", "subagents", "agent-a.jsonl")
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		t.Fatal(err)
	}
	appendFile(t, main, ctxAssistant("m1", "Bash", "t1")+ctxResult("t1", "abcdefgh"))
	appendFile(t, sub, ctxAssistant("s1", "Read", "t2")+ctxResult("t2", "abcd"))
	if err := e.st.StartSession(domain.Session{ID: ctxSess, Agent: "claude", ProviderSessionID: "x", TranscriptPath: main}); err != nil {
		t.Fatal(err)
	}
	return e, main, sub
}

func TestReadTranscripts_Context(t *testing.T) {
	e, main, sub := ctxEnv(t)
	if _, err := e.a.readTranscripts(ctxSess, main); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{main, sub} {
		if ctxCount(t, e, "tool_outputs", p) != 1 || ctxCount(t, e, "context_turns", p) != 1 {
			t.Fatalf("%s: saídas/turnos não gravados", p)
		}
	}
	// Segunda leitura sem mudança não duplica.
	if _, err := e.a.readTranscripts(ctxSess, main); err != nil {
		t.Fatal(err)
	}
	if ctxCount(t, e, "tool_outputs", main) != 1 || ctxCount(t, e, "context_turns", main) != 1 {
		t.Fatal("duplicou")
	}
	// Linhas novas: só o novo é lido.
	off, _, _ := e.st.ContextCursor(ctxSess, main)
	// Adultera o turno 0 direto no banco: se o arquivo fosse relido do começo,
	// o upsert o restauraria.
	if _, err := e.st.DB.Exec(`UPDATE context_turns SET tokens=777, at='orig' WHERE session_id=? AND path=? AND turn=0`, ctxSess, main); err != nil {
		t.Fatal(err)
	}
	appendFile(t, main, ctxAssistant("m2", "Grep", "t3")+ctxResult("t3", "xy"))
	if _, err := e.a.readTranscripts(ctxSess, main); err != nil {
		t.Fatal(err)
	}
	if ctxCount(t, e, "tool_outputs", main) != 2 || ctxCount(t, e, "context_turns", main) != 2 {
		t.Fatal("novo não lido")
	}
	if off2, st2, _ := e.st.ContextCursor(ctxSess, main); off2 <= off || st2.LastTurn != 1 || st2.LastMessageID != "m2" {
		t.Fatalf("cursor: %d -> %d, st=%+v", off, off2, st2)
	}
	var tok int64
	var at string
	if err := e.st.DB.QueryRow(`SELECT tokens, at FROM context_turns WHERE session_id=? AND path=? AND turn=0`, ctxSess, main).Scan(&tok, &at); err != nil {
		t.Fatal(err)
	}
	if tok != 777 || at != "orig" {
		t.Fatalf("turno antigo foi relido: tokens=%d at=%s", tok, at)
	}
	var mid string
	if err := e.st.DB.QueryRow(`SELECT message_id FROM context_turns WHERE session_id=? AND path=? AND turn=1`, ctxSess, main).Scan(&mid); err != nil || mid != "m2" {
		t.Fatalf("turno novo: %q %v", mid, err)
	}
}

func TestBackfillContext(t *testing.T) {
	e, main, sub := ctxEnv(t)
	// O uso já foi lido até o fim; a medição de contexto ainda não.
	for _, p := range []string{main, sub} {
		fi, _ := os.Stat(p)
		if err := e.st.SetFileOffset(ctxSess, p, fi.Size()); err != nil {
			t.Fatal(err)
		}
	}
	// Com ctx já cancelado, não lê nenhuma sessão.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	e.a.BackfillContext(cancelled)
	if ctxCount(t, e, "tool_outputs", main) != 0 {
		t.Fatal("leu com ctx cancelado")
	}
	e.a.BackfillContext(context.Background())
	if ctxCount(t, e, "tool_outputs", main) != 1 || ctxCount(t, e, "tool_outputs", sub) != 1 {
		t.Fatal("histórico não gravado")
	}
	fi, _ := os.Stat(main)
	if u, _ := e.st.FileOffset(ctxSess, main); u != fi.Size() {
		t.Fatalf("offset de uso mudou: %d", u)
	}
	// StartBackfill: Wait espera a passada.
	e.a.StartBackfill(context.Background())
	e.a.Wait()
}

func TestReadTranscripts_ContextTruncated(t *testing.T) {
	e, main, _ := ctxEnv(t)
	if _, err := e.a.readTranscripts(ctxSess, main); err != nil {
		t.Fatal(err)
	}
	// O arquivo é reescrito menor, com outro conteúdo.
	short := ctxAssistant("n1", "Edit", "z1")
	if err := os.WriteFile(main, []byte(short), 0o644); err != nil {
		t.Fatal(err)
	}
	if off, _, _ := e.st.ContextCursor(ctxSess, main); off <= int64(len(short)) {
		t.Fatalf("pré-condição: tool_offset %d deveria superar %d", off, len(short))
	}
	if _, err := e.a.readTranscripts(ctxSess, main); err != nil {
		t.Fatal(err)
	}
	var tool string
	if err := e.st.DB.QueryRow(`SELECT tool FROM tool_outputs WHERE session_id=? AND path=?`, ctxSess, main).Scan(&tool); err != nil {
		t.Fatal(err)
	}
	if ctxCount(t, e, "tool_outputs", main) != 1 || tool != "Edit" {
		t.Fatalf("linhas antigas não sumiram: tool=%q", tool)
	}
}
