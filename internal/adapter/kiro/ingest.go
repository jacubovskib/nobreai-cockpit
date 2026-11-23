package kiro

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// Quando o histórico é lido (decisão da Task 8). O núcleo só chama Ingest no
// TurnEnd e no Ended, e o kiro-cli grava o usage_summary DEPOIS do hook Stop:
// os créditos de um turno não estão no arquivo quando o TurnEnd é tratado.
// Por isso o Kiro lê também (1) em cada prompt, pelo Observe (leitura
// barata e incremental, que pega o turno anterior), (2) em duas releituras
// curtas depois do TurnEnd (retries), para o último turno de uma conversa
// aparecer sem esperar o prompt seguinte, e (3) no Ended (final).

// Ingest lê o histórico da sessão em segundo plano (agentcore.Ingester).
// Depois de um fim de turno agenda as releituras de a.retries.
func (a *Adapter) Ingest(sess domain.Session, ev agentcore.Event) {
	a.ingestAsync(sess, 0)
	if ev.Kind != agentcore.TurnEnd {
		return
	}
	for _, d := range a.retries {
		a.ingestAsync(sess, d)
	}
}

// Observe dispara a leitura do histórico a cada prompt
// (agentcore.Observer): o usage_summary do turno anterior já está no arquivo.
func (a *Adapter) Observe(sess domain.Session, ev agentcore.Event) {
	if ev.Kind == agentcore.Prompt {
		a.ingestAsync(sess, 0)
	}
}

// Wait cancela as releituras ainda esperando e espera as em andamento
// (desligamento e testes).
func (a *Adapter) Wait() {
	a.quitOnce.Do(func() { close(a.quit) })
	a.wg.Wait()
}

func (a *Adapter) ingestAsync(sess domain.Session, delay time.Duration) {
	if a.st == nil {
		return
	}
	now := a.now()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if delay > 0 {
			t := time.NewTimer(delay)
			defer t.Stop()
			select {
			case <-t.C:
			case <-a.quit:
				return
			}
		}
		changed, err := a.readLocked(sess, now)
		if err != nil {
			log.Printf("hook kiro: histórico da sessão %s: %v", sess.ID, err)
		}
		if changed && a.publish != nil {
			a.publish("usage")
		}
	}()
}

// readLocked lê com o mutex da sessão. Um pânico não derruba o daemon: vira
// log e a leitura termina sem mudança.
func (a *Adapter) readLocked(sess domain.Session, now time.Time) (changed bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hook kiro: pânico ao ler o histórico da sessão %s: %v\n%s", sess.ID, r, debug.Stack())
			changed, err = false, nil
		}
	}()
	mu, _ := a.ingest.LoadOrStore(sess.ID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	return a.readHistory(sess, now)
}

// historyFile acha o messages.jsonl da sessão: o caminho guardado em
// sessions.transcript_path, se o arquivo existe, senão a busca em
// <SessionsDir>/*/<session_id>/ (o hash do meio é o do projeto), guardando o
// achado. "" = ainda não existe (sem erro, sem log).
func (a *Adapter) historyFile(sess domain.Session) string {
	if sess.TranscriptPath != "" {
		if _, err := os.Stat(sess.TranscriptPath); err == nil {
			return sess.TranscriptPath
		}
	}
	a.nobreMu.Lock()
	dir := a.paths.SessionsDir
	a.nobreMu.Unlock()
	id := sess.ProviderSessionID
	// O id vira parte de um padrão de caminho: nada de separadores, "..",
	// nem de curingas do glob.
	if dir == "" || id == "" || id != filepath.Base(id) || id == ".." || id == "." || strings.ContainsAny(id, `*?[\`) {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*", id, "messages.jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	if matches[0] != sess.TranscriptPath {
		if err := a.st.SetTranscriptPath(sess.ID, matches[0]); err != nil {
			log.Printf("hook kiro: guardar o histórico da sessão %s: %v", sess.ID, err)
		}
	}
	return matches[0]
}

// readHistory lê o que há de novo no messages.jsonl desde o cursor e grava o
// uso (créditos por turno), o contexto (% por turno) e as saídas de
// ferramenta. Tudo é upsert: reler o mesmo trecho não duplica. O cursor
// (tool_offset, mais o uso em transcript_offsets) só avança por linhas
// completas: a última, sem "\n", fica para a próxima leitura.
func (a *Adapter) readHistory(sess domain.Session, now time.Time) (bool, error) {
	path := a.historyFile(sess)
	if path == "" {
		return false, nil
	}
	off, st, err := a.st.ContextCursor(sess.ID, path)
	if err != nil {
		return false, err
	}
	h, err := parseHistory(path, off)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if h.reset {
		off, st = 0, usage.ContextState{LastTurn: -1}
	}
	var known []usage.ContextTurn
	if !h.reset && off > 0 {
		if known, err = a.st.ContextTurnsOf(sess.ID, path); err != nil {
			return false, err
		}
	}
	if h.next == off && !h.reset && len(h.recs) == 0 {
		return false, nil
	}
	turns := mergeTurns(known, h.recs)
	if len(turns) > 0 {
		st.Segment = turns[len(turns)-1].Segment
		st.LastTurn = turns[len(turns)-1].Turn
		st.LastMessageID = turns[len(turns)-1].MessageID
	}
	model := sessionModel(path)
	rows, outs := a.buildRows(sess, h.recs, turns, model, now)
	var saved []usage.ContextTurn // só os que mudaram nesta leitura
	touched := map[string]bool{}
	for _, r := range h.recs {
		if r.pct != nil {
			touched[r.exec] = true
		}
	}
	for _, t := range turns {
		if touched[t.MessageID] {
			saved = append(saved, t)
		}
	}
	// O uso (upsert) vai antes: o SaveContext avança o cursor, e uma falha
	// depois dele perderia os créditos. Falhando o uso, o cursor fica onde
	// estava e a próxima leitura tenta de novo.
	if err := a.st.UpsertUsage(rows); err != nil {
		return false, err
	}
	if err := a.st.SaveContext(sess.ID, path, h.reset, outs, saved, h.next, st); err != nil {
		return false, err
	}
	if err := a.st.SetFileOffset(sess.ID, path, h.next); err != nil {
		return false, err
	}
	if model != "" {
		if err := a.st.SetSessionModel(sess.ID, model); err != nil {
			return false, err
		}
	}
	return len(rows) > 0 || len(saved) > 0, nil
}

// buildRows monta o uso (um por usage_summary) e as saídas de ferramenta.
func (a *Adapter) buildRows(sess domain.Session, recs []record, turns []usage.ContextTurn, model string, now time.Time) ([]domain.UsageRow, []usage.ToolOutput) {
	byExec := map[string]usage.ContextTurn{}
	for _, t := range turns {
		byExec[t.MessageID] = t
	}
	names := map[string]record{} // toolCallId → tool_call
	var rows []domain.UsageRow
	var outs []usage.ToolOutput
	for _, r := range recs {
		switch r.kind {
		case "usage_summary":
			id := r.turnID
			if id == "" {
				id = r.exec
			}
			if id == "" {
				continue
			}
			at := r.at
			if at.IsZero() {
				at = now
			}
			rows = append(rows, domain.UsageRow{SessionID: sess.ID, MessageID: id, Model: model, Credits: r.credits, At: at})
		case "tool_call":
			names[r.callID] = r
		case "tool_result":
			if r.callID == "" {
				continue
			}
			o := usage.ToolOutput{ItemID: r.callID, Bytes: r.bytes, Tokens: usage.EstimateTokens(r.bytes, 0),
				Turn: -1, At: r.at, IsError: !r.success, Result: true}
			if c, ok := names[r.callID]; ok {
				o.Tool, o.Key = c.tool, c.key
			}
			if t, ok := byExec[r.exec]; ok {
				o.Turn, o.Segment = t.Turn, t.Segment
			}
			outs = append(outs, o)
		}
	}
	return rows, outs
}

// mergeTurns junta os turnos já gravados (known) com a % lida agora. Um turno
// é uma execução (executionId); o último contextUsage dela vale (um turno
// pode atravessar duas leituras: a mesma execução atualiza o mesmo turno).
// Ruling 2: a % que cai para menos da metade da do turno anterior é uma nova
// compactação (segmento + 1), para o medidor e os avisos rearmarem.
func mergeTurns(known []usage.ContextTurn, recs []record) []usage.ContextTurn {
	turns := append([]usage.ContextTurn(nil), known...)
	idx := map[string]int{}
	for i, t := range turns {
		idx[t.MessageID] = i
	}
	changed := map[int]bool{}
	for _, r := range recs {
		if r.kind != "contextUsage" || r.pct == nil || r.exec == "" {
			continue
		}
		i, ok := idx[r.exec]
		if !ok {
			i = len(turns)
			idx[r.exec] = i
			turns = append(turns, usage.ContextTurn{Turn: i, MessageID: r.exec, At: r.at})
		}
		v := *r.pct
		turns[i].Pct, turns[i].At = &v, r.at
		changed[i] = true
	}
	for i := range turns {
		if !changed[i] {
			continue
		}
		turns[i].Segment = 0
		if i > 0 {
			turns[i].Segment = turns[i-1].Segment
			if p := turns[i-1].Pct; p != nil && turns[i].Pct != nil && *turns[i].Pct < *p/2 {
				turns[i].Segment++
			}
		}
	}
	return turns
}

// sessionModel é o modelId do session.json ao lado do histórico ("" se não
// há ou não se lê).
func sessionModel(historyPath string) string {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(historyPath), "session.json"))
	if err != nil {
		return ""
	}
	var s struct {
		ModelID string `json:"modelId"`
	}
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.ModelID
}

// record é uma linha do messages.jsonl que o leitor usa.
type record struct {
	kind    string // usage_summary, contextUsage, tool_call, tool_result
	exec    string
	turnID  string
	at      time.Time
	pct     *float64 // contextUsage
	credits float64  // usage_summary
	callID  string   // tool_call, tool_result
	tool    string   // tool_call
	key     string   // tool_call: caminho ou comando
	bytes   int64    // tool_result
	success bool     // tool_result
}

type history struct {
	recs  []record
	next  int64
	reset bool
}

type rawLine struct {
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type        string          `json:"type"`
		Key         string          `json:"key"`
		ExecutionID string          `json:"executionId"`
		TurnID      string          `json:"turnId"`
		ToolCallID  string          `json:"toolCallId"`
		ToolName    string          `json:"toolName"`
		Args        json.RawMessage `json:"args"`
		Value       json.RawMessage `json:"value"`
		Content     json.RawMessage `json:"content"`
		Success     *bool           `json:"success"`
		Summaries   []struct {
			Unit  string  `json:"unit"`
			Usage float64 `json:"usage"`
		} `json:"promptTurnSummaries"`
	} `json:"payload"`
}

// parseHistory lê o arquivo de off até a última linha completa. Arquivo menor
// que o cursor foi truncado ou trocado: lê do começo (reset). Linha que não é
// JSON é pulada; a parcial no fim (sem "\n") não conta nem avança next.
func parseHistory(path string, off int64) (history, error) {
	f, err := os.Open(path)
	if err != nil {
		return history{next: off}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return history{next: off}, err
	}
	h := history{}
	if off > info.Size() {
		off, h.reset = 0, true
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return history{next: off}, err
	}
	h.next = off
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, rerr := r.ReadBytes('\n')
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break // linha parcial (ou nada): fica para a próxima leitura
			}
			return history{next: off}, rerr
		}
		h.next += int64(len(line))
		if rec, ok := parseLine(line); ok {
			h.recs = append(h.recs, rec)
		}
	}
	return h, nil
}

func parseLine(line []byte) (record, bool) {
	var l rawLine
	if json.Unmarshal(line, &l) != nil {
		return record{}, false
	}
	p := l.Payload
	at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	rec := record{exec: p.ExecutionID, turnID: p.TurnID, at: at}
	switch {
	case p.Type == "usage_summary":
		rec.kind = "usage_summary"
		for _, s := range p.Summaries {
			if s.Unit == "credit" {
				rec.credits += s.Usage
			}
		}
	case p.Type == "session_metadata" && p.Key == "contextUsage":
		var v struct {
			Pct *float64 `json:"usagePercentage"`
		}
		if json.Unmarshal(p.Value, &v) != nil || v.Pct == nil {
			return record{}, false
		}
		rec.kind, rec.pct = "contextUsage", v.Pct
	case p.Type == "tool_call":
		rec.kind, rec.callID, rec.tool = "tool_call", p.ToolCallID, p.ToolName
		var a struct {
			Path    string `json:"path"`
			Command string `json:"command"`
		}
		_ = json.Unmarshal(p.Args, &a)
		rec.key = a.Path
		if rec.key == "" {
			rec.key = a.Command
		}
	case p.Type == "tool_result":
		rec.kind, rec.callID = "tool_result", p.ToolCallID
		rec.bytes = contentBytes(p.Content)
		rec.success = p.Success == nil || *p.Success
	default:
		return record{}, false
	}
	return rec, true
}

// contentBytes é o tamanho do texto de um tool_result: a string, ou os textos
// de uma lista de blocos {"text"}; senão o JSON como veio.
func contentBytes(raw json.RawMessage) int64 {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return int64(len(s))
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var n int
		var any bool
		for _, b := range blocks {
			n += len(b.Text)
			any = any || b.Text != ""
		}
		if any {
			return int64(n)
		}
	}
	return int64(len(strings.TrimSpace(string(raw))))
}
