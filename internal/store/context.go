package store

import (
	"time"

	"database/sql"
	"errors"

	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// ContextCursor devolve até onde o transcript path da sessão já foi lido para
// a medição de contexto (tool_offset) e o estado do leitor: LastTurn e
// LastMessageID vêm do último turno gravado do path, Segment da linha de
// transcript_offsets. Sem nada gravado: off 0 e {LastTurn:-1}. Com off 0 o
// estado é sempre {LastTurn:-1}, mesmo com turnos gravados (releitura depois
// de ResetContextCursors): a leitura recomeça a numeração do zero.
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
	if off == 0 {
		return 0, usage.ContextState{LastTurn: -1}, nil
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
//
// Sem atribuição por janela: tudo fica com a principal (ver SaveContextWindows).
func (s *Store) SaveContext(sessionID, path string, reset bool, outs []usage.ToolOutput, turns []usage.ContextTurn, next int64, st usage.ContextState) error {
	return s.SaveContextWindows(sessionID, path, reset, outs, turns, next, st, ContextWindows{})
}

// SaveContextWindows é SaveContext com a atribuição por janela da leitura, na
// mesma transação: saídas e turnos gravam window_session_id pelo uuid da linha
// que os criou (w.ByUUID; ausente = principal, ""), as linhas de janela filha
// e as raízes da principal (w.MainRoots, com o id da principal) vão para
// window_lines e os prompts casados ficam marcados como usados. A
// janela é sempre a da leitura mais recente (é determinística: a raiz do galho
// fica em window_lines). As compactações (w.Compactions) vão para
// context_compactions por (session_id, path, segment). reset=true apaga
// também as window_lines e as compactações do path; w.Rewrite apaga só os
// turnos e as saídas do path (releitura do começo). Saídas e turnos fora de
// w.ByUUID ficam com w.Default. Os subagentes (w.Subagents) entram em
// subagent_windows na mesma transação (ver ContextWindows).
func (s *Store) SaveContextWindows(sessionID, path string, reset bool, outs []usage.ToolOutput, turns []usage.ContextTurn, next int64, st usage.ContextState, w ContextWindows) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if reset || w.Rewrite {
		if _, err := tx.Exec(`DELETE FROM tool_outputs WHERE session_id=? AND path=?`, sessionID, path); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM context_turns WHERE session_id=? AND path=?`, sessionID, path); err != nil {
			return err
		}
	}
	if reset {
		if _, err := tx.Exec(`DELETE FROM window_lines WHERE session_id=? AND path=?`, sessionID, path); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM context_compactions WHERE session_id=? AND path=?`, sessionID, path); err != nil {
			return err
		}
	}
	for seg, win := range w.Compactions {
		if _, err := tx.Exec(`INSERT INTO context_compactions (session_id, path, segment, window_session_id) VALUES (?,?,?,?)
			ON CONFLICT(session_id, path, segment) DO UPDATE SET window_session_id=excluded.window_session_id`,
			sessionID, path, seg, win); err != nil {
			return err
		}
	}
	lines := make(map[string]string, len(w.ByUUID)+len(w.MainRoots))
	for uuid, win := range w.ByUUID {
		lines[uuid] = win
	}
	for _, uuid := range w.MainRoots {
		lines[uuid] = sessionID
	}
	for uuid, win := range lines {
		if _, err := tx.Exec(`INSERT INTO window_lines (session_id, path, uuid, window_session_id) VALUES (?,?,?,?)
			ON CONFLICT(session_id, path, uuid) DO UPDATE SET window_session_id=excluded.window_session_id`,
			sessionID, path, uuid, win); err != nil {
			return err
		}
	}
	for sub, win := range w.Subagents {
		if err := saveSubagentWindowTx(tx, sessionID, sub, win); err != nil {
			return err
		}
	}
	winOf := func(uuid string) string {
		if win, ok := w.ByUUID[uuid]; ok {
			return win
		}
		return w.Default
	}
	for _, id := range w.UsedPrompts {
		if _, err := tx.Exec(`UPDATE window_prompts SET used=1 WHERE id=?`, id); err != nil {
			return err
		}
	}
	for _, o := range outs {
		res := 0
		if o.Result {
			res = 1
		}
		if _, err := tx.Exec(`INSERT INTO tool_outputs (session_id, path, item_id, tool, key, bytes, tokens, in_tokens, turn, segment, at, is_error, window_session_id)
			VALUES (?,?,?, COALESCE(NULLIF(?,''),'?'), COALESCE(NULLIF(?,''),'?'), ?,?,?,?,?,?,?,?)
			ON CONFLICT(session_id, item_id) DO UPDATE SET
			  window_session_id = excluded.window_session_id,
			  tool = COALESCE(NULLIF(?,''), tool_outputs.tool),
			  key = COALESCE(NULLIF(?,''), tool_outputs.key),
			  in_tokens = CASE WHEN ? > 0 THEN ? ELSE tool_outputs.in_tokens END,
			  bytes = CASE WHEN ?=1 THEN ? ELSE tool_outputs.bytes END,
			  tokens = CASE WHEN ?=1 THEN ? ELSE tool_outputs.tokens END,
			  is_error = CASE WHEN ?=1 THEN ? ELSE tool_outputs.is_error END`,
			sessionID, path, o.ItemID, o.Tool, o.Key, o.Bytes, o.Tokens, o.InTokens, o.Turn, o.Segment, fmtTime(o.At), o.IsError, winOf(o.UUID),
			o.Tool, o.Key,
			o.InTokens, o.InTokens,
			res, o.Bytes,
			res, o.Tokens,
			res, o.IsError); err != nil {
			return err
		}
	}
	for _, t := range turns {
		if _, err := tx.Exec(`INSERT INTO context_turns (session_id, path, turn, segment, message_id, at, tokens, input, cache_read, cache_write, ttl, window_session_id, pct)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(session_id, path, turn) DO UPDATE SET segment=excluded.segment,
			  message_id=excluded.message_id, at=excluded.at, tokens=excluded.tokens,
			  input=excluded.input, cache_read=excluded.cache_read, cache_write=excluded.cache_write, ttl=excluded.ttl,
			  window_session_id=excluded.window_session_id, pct=excluded.pct`,
			sessionID, path, t.Turn, t.Segment, t.MessageID, fmtTime(t.At), t.Tokens, t.Input, t.CacheRead, t.CacheWrite, t.TTL, winOf(t.UUID), t.Pct); err != nil {
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

// saveSubagentWindowTx grava a janela win do arquivo de subagente sub se ele
// ainda não tem uma (a primeira vale) e, sendo filha, passa para ela os
// turnos, as saídas e o uso já gravados do arquivo com a principal; o uso vai
// também para o card atual da filha (única troca de card de uso já gravado). O uso não tem caminho: as linhas são as dos message_id
// dos turnos do arquivo.
func saveSubagentWindowTx(tx *sql.Tx, sessionID, sub, win string) error {
	res, err := tx.Exec(`INSERT INTO subagent_windows (session_id, path, window_session_id) VALUES (?,?,?)
		ON CONFLICT(session_id, path) DO NOTHING`, sessionID, sub, win)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 || win == "" {
		return err
	}
	if _, err := tx.Exec(`UPDATE usage SET window_session_id=?, card_id=(SELECT card_id FROM sessions WHERE id=?)
		WHERE session_id=? AND window_session_id='' AND message_id IN (SELECT message_id FROM context_turns WHERE session_id=? AND path=?)`,
		win, win, sessionID, sessionID, sub); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE context_turns SET window_session_id=? WHERE session_id=? AND path=? AND window_session_id=''`, win, sessionID, sub); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE tool_outputs SET window_session_id=? WHERE session_id=? AND path=? AND window_session_id=''`, win, sessionID, sub)
	return err
}

// ResetContextCursors volta ao começo (tool_offset e segment 0) o cursor de
// contexto de todos os paths das sessões sessionIDs (o transcript principal e
// os subagentes), para a próxima leitura refazer a medição. Quem chama passa
// só as sessões do próprio leitor: o cursor de outro agente não muda. Nada é
// apagado aqui: a leitura do Claude que recomeça do zero apaga os turnos e as
// saídas daquele path na mesma transação em que os regrava
// (ContextWindows.Rewrite); window_lines, compactações e uso ficam. Linhas de
// transcript que não existe mais ficam como estão. O offset de uso não muda.
func (s *Store) ResetContextCursors(sessionIDs []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Um UPDATE por sessão: um IN com milhares de ids estouraria o limite de parâmetros.
	stmt, err := tx.Prepare(`UPDATE transcript_offsets SET tool_offset=0, segment=0 WHERE session_id=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range sessionIDs {
		if _, err := stmt.Exec(id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClaudeTranscripts devolve o transcript_path de cada sessão do Claude que
// tem um (id da sessão → caminho). Sessões filhas ficam de fora: dividem o
// transcript com a principal, que é quem o lê.
func (s *Store) ClaudeTranscripts() (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT id, transcript_path FROM sessions WHERE agent='claude' AND transcript_path<>'' AND parent_session_id=''`)
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

// SetTranscriptPath guarda o caminho do histórico da sessão (agentes que só o
// descobrem depois do início, como o Kiro).
func (s *Store) SetTranscriptPath(sessionID, path string) error {
	_, err := s.DB.Exec(`UPDATE sessions SET transcript_path=? WHERE id=?`, path, sessionID)
	return err
}

// ContextTurnsOf devolve os turnos gravados do transcript path da sessão, em
// ordem (Turn, Segment, MessageID, At e Pct; o resto não é lido).
func (s *Store) ContextTurnsOf(sessionID, path string) ([]usage.ContextTurn, error) {
	rows, err := s.DB.Query(`SELECT turn, segment, message_id, at, pct FROM context_turns WHERE session_id=? AND path=? ORDER BY turn`, sessionID, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usage.ContextTurn
	for rows.Next() {
		var t usage.ContextTurn
		var at string
		var pct sql.NullFloat64
		if err := rows.Scan(&t.Turn, &t.Segment, &t.MessageID, &at, &pct); err != nil {
			return nil, err
		}
		t.At, _ = time.Parse(time.RFC3339Nano, at)
		if pct.Valid {
			v := pct.Float64
			t.Pct = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
