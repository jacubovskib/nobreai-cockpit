package usage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

type transcriptLine struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ParseClaudeTranscript lê o transcript a partir de offset, só até a última
// linha completa, e devolve uma linha por message.id (a última vence). Offset
// maior que o arquivo (truncado ou trocado) recomeça do início: reler é seguro
// porque o uso é gravado por message.id. Linha sem timestamp válido usa o mtime
// do arquivo, para os tokens não se perderem. Mensagens sem nenhum token ficam
// de fora.
func ParseClaudeTranscript(path string, offset int64) ([]domain.UsageRow, int64, error) {
	return ParseClaudeTranscriptUntil(path, offset, -1)
}

// ParseClaudeTranscriptUntil é ParseClaudeTranscript lendo só até o byte limit
// (exclusivo; limit < 0 = até o fim): a atribuição por janela lê o uso só até
// onde a leitura de contexto já atribuiu. limit menor ou igual ao offset (depois
// do recomeço por truncamento) não lê nada.
func ParseClaudeTranscriptUntil(path string, offset, limit int64) ([]domain.UsageRow, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if offset > info.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	var src io.Reader = f
	if limit >= 0 {
		if limit <= offset {
			return nil, offset, nil
		}
		src = io.LimitReader(f, limit-offset)
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, offset, err
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, offset, nil
	}
	data = data[:end+1]

	var rows []domain.UsageRow
	idx := map[string]int{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var l transcriptLine
		if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil {
			at = info.ModTime()
		}
		u := l.Message.Usage
		row := domain.UsageRow{SessionID: l.SessionID, MessageID: l.Message.ID, Model: l.Message.Model,
			Input: u.Input, Output: u.Output, CacheWrite: u.CacheWrite, CacheRead: u.CacheRead, At: at}
		if i, ok := idx[row.MessageID]; ok && row.MessageID != "" {
			rows[i] = row
			continue
		}
		idx[row.MessageID] = len(rows)
		rows = append(rows, row)
	}
	// Mensagem sem nenhum token (as sintéticas do Claude Code) não é uso.
	out := rows[:0]
	for _, r := range rows {
		if r.Input+r.Output+r.CacheWrite+r.CacheRead > 0 {
			out = append(out, r)
		}
	}
	return out, offset + int64(len(data)), nil
}

// SubagentTranscript é o arquivo do subagente agentID do transcript
// principal mainPath: <main sem .jsonl>/subagents/agent-<agentID>.jsonl.
func SubagentTranscript(mainPath, agentID string) string {
	return filepath.Join(strings.TrimSuffix(mainPath, ".jsonl"), "subagents", "agent-"+agentID+".jsonl")
}

// SubagentTranscripts lista <main sem .jsonl>/subagents/agent-*.jsonl.
func SubagentTranscripts(mainPath string) []string {
	m, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(mainPath, ".jsonl"), "subagents", "agent-*.jsonl"))
	return m
}
