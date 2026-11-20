package store

import (
	"database/sql"
	"errors"
)

// SessionAlerts é o estado dos avisos ao agente de uma sessão (sobrevive a
// reinício do daemon). Sem linha: nada avisado (WarnedSegment -1).
type SessionAlerts struct {
	WarnedPct     int // maior faixa de contexto já avisada no segmento
	WarnedSegment int // segmento de compactação do último aviso; -1 = nunca
	BigOutputs    int // avisos de saída grande já dados
}

// GetSessionAlerts lê o estado dos avisos da sessão.
func (s *Store) GetSessionAlerts(sessionID string) (SessionAlerts, error) {
	a := SessionAlerts{WarnedSegment: -1}
	err := s.DB.QueryRow(`SELECT warned_pct, warned_segment, big_outputs FROM session_alerts WHERE session_id=?`, sessionID).
		Scan(&a.WarnedPct, &a.WarnedSegment, &a.BigOutputs)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionAlerts{WarnedSegment: -1}, nil
	}
	return a, err
}

// SetContextWarned grava a faixa e o segmento do aviso de contexto.
func (s *Store) SetContextWarned(sessionID string, pct, segment int) error {
	_, err := s.DB.Exec(`INSERT INTO session_alerts(session_id, warned_pct, warned_segment) VALUES (?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET warned_pct=excluded.warned_pct, warned_segment=excluded.warned_segment`,
		sessionID, pct, segment)
	return err
}

// ClaimBigOutputWarning conta mais um aviso de saída grande, se a sessão ainda
// não chegou a max; false quando o limite já foi atingido. Atômico: hooks
// simultâneos não passam do limite.
func (s *Store) ClaimBigOutputWarning(sessionID string, max int) (bool, error) {
	if max <= 0 {
		return false, nil
	}
	res, err := s.DB.Exec(`INSERT INTO session_alerts(session_id, big_outputs) VALUES (?,1)
		ON CONFLICT(session_id) DO UPDATE SET big_outputs=big_outputs+1 WHERE big_outputs < ?`, sessionID, max)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// MarkChildNoticePending marca que a sessão filha, criada no meio da conversa,
// ainda deve receber a linha de "outra janela" (child_notice_sent=1 = devida).
func (s *Store) MarkChildNoticePending(sessionID string) error {
	_, err := s.DB.Exec(`INSERT INTO session_alerts(session_id, child_notice_sent) VALUES (?,1)
		ON CONFLICT(session_id) DO UPDATE SET child_notice_sent=1`, sessionID)
	return err
}

// ClaimChildNotice entrega a linha de "outra janela" pendente: true uma única
// vez (atômico), depois a marca volta a 0.
func (s *Store) ClaimChildNotice(sessionID string) (bool, error) {
	res, err := s.DB.Exec(`UPDATE session_alerts SET child_notice_sent=0 WHERE session_id=? AND child_notice_sent=1`, sessionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
