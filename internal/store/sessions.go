package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// ErrCardBusy indica que outra sessão viva já é dona do card.
var ErrCardBusy = errors.New("card já tem uma sessão viva")

const sessionCols = `id, agent, provider_session_id, card_id, project_id, pid, cwd, branch, git_head, model, transcript_path, full_capture, started_at, last_event_at, ended_at, end_reason`

func scanSession(sc interface{ Scan(...any) error }) (domain.Session, error) {
	var x domain.Session
	var cardID, projectID sql.NullInt64
	var started, last string
	var ended sql.NullString
	err := sc.Scan(&x.ID, &x.Agent, &x.ProviderSessionID, &cardID, &projectID, &x.PID, &x.Cwd, &x.Branch,
		&x.GitHead, &x.Model, &x.TranscriptPath, &x.FullCapture, &started, &last, &ended, &x.EndReason)
	if err != nil {
		return x, err
	}
	if cardID.Valid {
		x.CardID = &cardID.Int64
	}
	if projectID.Valid {
		x.ProjectID = &projectID.Int64
	}
	if x.StartedAt, err = time.Parse(time.RFC3339Nano, started); err != nil {
		return x, err
	}
	if x.LastEventAt, err = time.Parse(time.RFC3339Nano, last); err != nil {
		return x, err
	}
	if ended.Valid {
		t, err := time.Parse(time.RFC3339Nano, ended.String)
		if err != nil {
			return x, err
		}
		x.EndedAt = &t
	}
	return x, nil
}

func nullableInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func querySessions(db interface {
	Query(string, ...any) (*sql.Rows, error)
}, q string, args ...any) ([]domain.Session, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// StartSession registra a sessão; se o ID já existir, não faz nada.
func (s *Store) StartSession(x domain.Session) error {
	if x.StartedAt.IsZero() {
		x.StartedAt = time.Now()
	}
	if x.LastEventAt.IsZero() {
		x.LastEventAt = x.StartedAt
	}
	_, err := s.DB.Exec(`INSERT INTO sessions(id, agent, provider_session_id, card_id, project_id, pid, cwd, branch, git_head, model, transcript_path, full_capture, started_at, last_event_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		x.ID, x.Agent, x.ProviderSessionID, nullableInt(x.CardID), nullableInt(x.ProjectID), x.PID, x.Cwd, x.Branch,
		x.GitHead, x.Model, x.TranscriptPath, x.FullCapture, fmtTime(x.StartedAt), fmtTime(x.LastEventAt))
	return err
}

// GetSession busca a sessão pelo ID (ErrNotFound se não existir).
func (s *Store) GetSession(id string) (domain.Session, error) {
	x, err := scanSession(s.DB.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// SessionByPID devolve a sessão não encerrada mais recente com esse PID, ou nil.
func (s *Store) SessionByPID(pid int) (*domain.Session, error) {
	x, err := scanSession(s.DB.QueryRow(`SELECT `+sessionCols+` FROM sessions
		WHERE pid=? AND ended_at IS NULL ORDER BY julianday(started_at) DESC LIMIT 1`, pid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// TouchSession atualiza last_event_at.
func (s *Store) TouchSession(id string, at time.Time) error {
	res, err := s.DB.Exec(`UPDATE sessions SET last_event_at=? WHERE id=?`, fmtTime(at), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// MarkReminded marca que a sessão já recebeu o lembrete de handoff.
func (s *Store) MarkReminded(id string) error {
	res, err := s.DB.Exec(`UPDATE sessions SET reminded=1 WHERE id=?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// LiveSessions lista as sessões não encerradas, da mais antiga para a mais nova.
func (s *Store) LiveSessions() ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions WHERE ended_at IS NULL ORDER BY julianday(started_at), id`)
}

// SessionsForCard lista as sessões ligadas ao card, da mais antiga para a mais nova.
func (s *Store) SessionsForCard(cardID int64) ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions WHERE card_id=? ORDER BY julianday(started_at), id`, cardID)
}

// RecentSessions lista as sessões mais recentes (por início), da mais nova para a mais antiga.
func (s *Store) RecentSessions(limit int) ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions ORDER BY julianday(started_at) DESC, id DESC LIMIT ?`, limit)
}

// moveCardTx aplica a transição ao card dentro da transação.
func moveCardTx(tx *sql.Tx, cardID int64, ev domain.Transition) error {
	var cur domain.Column
	if err := tx.QueryRow(`SELECT col FROM cards WHERE id=?`, cardID).Scan(&cur); err != nil {
		return err
	}
	next := domain.NextColumn(cur, ev)
	if next == cur {
		return nil
	}
	_, err := tx.Exec(`UPDATE cards SET col=?, updated_at=? WHERE id=?`, string(next), fmtTime(time.Now()), cardID)
	return err
}

// AttachSession liga a sessão ao card, move o card para em_andamento e copia
// para o card os eventos já capturados da sessão. Se a sessão já terminou, o
// card vai para pausado (nunca em_andamento sem dono vivo). Se a sessão viva
// estava em outro card e ele fica sem dono vivo, esse card também pausa. Se
// outra sessão viva é dona do card devolve ErrCardBusy, a menos que force (a
// outra é desligada).
func (s *Store) AttachSession(sessionID, cardKey string, force bool) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var ended sql.NullString
	var prevCard sql.NullInt64
	if err := tx.QueryRow(`SELECT ended_at, card_id FROM sessions WHERE id=?`, sessionID).Scan(&ended, &prevCard); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var cardID int64
	if err := tx.QueryRow(`SELECT id FROM cards WHERE key=?`, cardKey).Scan(&cardID); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}

	var others int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE card_id=? AND ended_at IS NULL AND id<>?`, cardID, sessionID).Scan(&others); err != nil {
		return err
	}
	if others > 0 {
		if !force {
			return ErrCardBusy
		}
		if _, err := tx.Exec(`UPDATE sessions SET card_id=NULL WHERE card_id=? AND ended_at IS NULL AND id<>?`, cardID, sessionID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE sessions SET card_id=? WHERE id=?`, cardID, sessionID); err != nil {
		return err
	}
	if prevCard.Valid && prevCard.Int64 != cardID && !ended.Valid {
		// A sessão viva saiu do card anterior: sem outro dono vivo, ele pausa.
		var live int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE card_id=? AND ended_at IS NULL`, prevCard.Int64).Scan(&live); err != nil {
			return err
		}
		if live == 0 {
			if err := moveCardTx(tx, prevCard.Int64, domain.SessionEnded); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE events SET card_id=? WHERE session_id=?`, cardID, sessionID); err != nil {
		return err
	}
	if err := moveCardTx(tx, cardID, domain.SessionAttached); err != nil {
		return err
	}
	if ended.Valid {
		// Sessão já encerrada não é dona viva: o card fica pausado.
		if err := moveCardTx(tx, cardID, domain.SessionEnded); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EndSession encerra a sessão e, se ela tem card, aplica a transição
// SessionEnded na mesma transação. Encerrar sessão já encerrada não faz nada.
func (s *Store) EndSession(id, reason string, at time.Time) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var cardID sql.NullInt64
	var ended sql.NullString
	if err := tx.QueryRow(`SELECT card_id, ended_at FROM sessions WHERE id=?`, id).Scan(&cardID, &ended); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if ended.Valid {
		return nil
	}
	if _, err := tx.Exec(`UPDATE sessions SET ended_at=?, end_reason=? WHERE id=?`, fmtTime(at), reason, id); err != nil {
		return err
	}
	if cardID.Valid {
		if err := moveCardTx(tx, cardID.Int64, domain.SessionEnded); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddEvent grava o evento. Sem CardID, usa o card atual da sessão (se houver).
func (s *Store) AddEvent(e domain.Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	data := string(e.Data)
	if data == "" {
		data = "{}"
	}
	_, err := s.DB.Exec(`INSERT INTO events(session_id, card_id, kind, data, at)
		VALUES (?, COALESCE(?, (SELECT card_id FROM sessions WHERE id=?)), ?, ?, ?)`,
		e.SessionID, nullableInt(e.CardID), e.SessionID, e.Kind, data, fmtTime(e.At))
	return err
}

// RecentEvents devolve os últimos eventos, do mais novo para o mais antigo.
func (s *Store) RecentEvents(limit int) ([]domain.Event, error) {
	rows, err := s.DB.Query(`SELECT id, session_id, card_id, kind, data, at FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var cardID sql.NullInt64
		var data, at string
		if err := rows.Scan(&e.ID, &e.SessionID, &cardID, &e.Kind, &data, &at); err != nil {
			return nil, err
		}
		if cardID.Valid {
			e.CardID = &cardID.Int64
		}
		e.Data = json.RawMessage(data)
		if e.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
