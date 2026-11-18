package store

import (
	"database/sql"
	"errors"

	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// ContextCursor devolve até onde o transcript path da sessão já foi lido para
// a medição de contexto (tool_offset) e o estado do leitor: LastTurn e
// LastMessageID vêm do último turno gravado do path, Segment da linha de
// transcript_offsets. Sem nada gravado: off 0 e {LastTurn:-1}.
func (s *Store) ContextCursor(sessionID, path string) (off int64, st usage.ContextState, err error) {
	st = usage.ContextState{LastTurn: -1}
	err = s.DB.QueryRow(`SELECT tool_offset, segment FROM transcript_offsets WHERE session_id=? AND path=?`,
		sessionID, path).Scan(&off, &st.Segment)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return 0, usage.ContextState{LastTurn: -1}, err
	}
	err = s.DB.QueryRow(`SELECT turn, message_id FROM context_turns WHERE session_id=? AND path=? ORDER BY turn DESC LIMIT 1`,
		sessionID, path).Scan(&st.LastTurn, &st.LastMessageID)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return 0, usage.ContextState{LastTurn: -1}, err
	}
	return off, st, nil
}

// SaveContext grava saídas (upsert por session_id,item_id), turnos (upsert por
// session_id,path,turn) e o cursor, numa transação, sem tocar no offset de uso.
// reset=true apaga antes as linhas de (session_id, path).
//
// Mescla de saídas: o tool_use e o tool_result do mesmo item podem chegar em
// leituras diferentes. Em linha nova, tool/key vazios viram "?". Em conflito,
// tool/key só mudam se o novo não for vazio; in_tokens só se o novo for > 0;
// bytes/tokens/is_error só se o registro tiver Result; turn/segment/at ficam
// os da primeira gravação.
func (s *Store) SaveContext(sessionID, path string, reset bool, outs []usage.ToolOutput, turns []usage.ContextTurn, next int64, st usage.ContextState) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if reset {
		if _, err := tx.Exec(`DELETE FROM tool_outputs WHERE session_id=? AND path=?`, sessionID, path); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM context_turns WHERE session_id=? AND path=?`, sessionID, path); err != nil {
			return err
		}
	}
	for _, o := range outs {
		res := 0
		if o.Result {
			res = 1
		}
		if _, err := tx.Exec(`INSERT INTO tool_outputs (session_id, path, item_id, tool, key, bytes, tokens, in_tokens, turn, segment, at, is_error)
			VALUES (?,?,?, COALESCE(NULLIF(?,''),'?'), COALESCE(NULLIF(?,''),'?'), ?,?,?,?,?,?,?)
			ON CONFLICT(session_id, item_id) DO UPDATE SET
			  tool = COALESCE(NULLIF(?,''), tool_outputs.tool),
			  key = COALESCE(NULLIF(?,''), tool_outputs.key),
			  in_tokens = CASE WHEN ? > 0 THEN ? ELSE tool_outputs.in_tokens END,
			  bytes = CASE WHEN ?=1 THEN ? ELSE tool_outputs.bytes END,
			  tokens = CASE WHEN ?=1 THEN ? ELSE tool_outputs.tokens END,
			  is_error = CASE WHEN ?=1 THEN ? ELSE tool_outputs.is_error END`,
			sessionID, path, o.ItemID, o.Tool, o.Key, o.Bytes, o.Tokens, o.InTokens, o.Turn, o.Segment, fmtTime(o.At), o.IsError,
			o.Tool, o.Key,
			o.InTokens, o.InTokens,
			res, o.Bytes,
			res, o.Tokens,
			res, o.IsError); err != nil {
			return err
		}
	}
	for _, t := range turns {
		if _, err := tx.Exec(`INSERT INTO context_turns (session_id, path, turn, segment, message_id, at, tokens)
			VALUES (?,?,?,?,?,?,?)
			ON CONFLICT(session_id, path, turn) DO UPDATE SET segment=excluded.segment,
			  message_id=excluded.message_id, at=excluded.at, tokens=excluded.tokens`,
			sessionID, path, t.Turn, t.Segment, t.MessageID, fmtTime(t.At), t.Tokens); err != nil {
			return err
		}
	}
	// A linha pode não existir ainda; o offset de uso (offset) nunca é alterado.
	if _, err := tx.Exec(`INSERT INTO transcript_offsets(session_id, path, offset, tool_offset, segment) VALUES (?,?,0,?,?)
		ON CONFLICT(session_id, path) DO UPDATE SET tool_offset=excluded.tool_offset, segment=excluded.segment`,
		sessionID, path, next, st.Segment); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaudeTranscripts devolve o transcript_path de cada sessão do Claude que
// tem um (id da sessão → caminho).
func (s *Store) ClaudeTranscripts() (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT id, transcript_path FROM sessions WHERE agent='claude' AND transcript_path<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}
