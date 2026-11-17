package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// ErrCardBusy indica que outra sessão viva já é dona do card.
var ErrCardBusy = errors.New("card já tem uma sessão viva")

// liveCond é a regra de sessão viva: não encerrada e com o processo do agente
// existindo (pid_dead_at marca o início da carência após o PID sumir).
const liveCond = `ended_at IS NULL AND pid_dead_at IS NULL`

const sessionCols = `id, agent, provider_session_id, card_id, project_id, pid, cwd, branch, git_head, model, transcript_path, full_capture, started_at, last_event_at, ended_at, end_reason, pid_dead_at, worktree, worktree_linked`

func scanSession(sc interface{ Scan(...any) error }) (domain.Session, error) {
	var x domain.Session
	var cardID, projectID sql.NullInt64
	var started, last string
	var ended, dead sql.NullString
	err := sc.Scan(&x.ID, &x.Agent, &x.ProviderSessionID, &cardID, &projectID, &x.PID, &x.Cwd, &x.Branch,
		&x.GitHead, &x.Model, &x.TranscriptPath, &x.FullCapture, &started, &last, &ended, &x.EndReason, &dead,
		&x.Worktree, &x.WorktreeLinked)
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
	if dead.Valid {
		t, err := time.Parse(time.RFC3339Nano, dead.String)
		if err != nil {
			return x, err
		}
		x.PIDDeadAt = &t
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
	_, err := s.DB.Exec(`INSERT INTO sessions(id, agent, provider_session_id, card_id, project_id, pid, cwd, branch, git_head, model, transcript_path, full_capture, started_at, last_event_at, worktree, worktree_linked)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		x.ID, x.Agent, x.ProviderSessionID, nullableInt(x.CardID), nullableInt(x.ProjectID), x.PID, x.Cwd, x.Branch,
		x.GitHead, x.Model, x.TranscriptPath, x.FullCapture, fmtTime(x.StartedAt), fmtTime(x.LastEventAt),
		x.Worktree, x.WorktreeLinked)
	return err
}

// SessionLocation é onde a sessão roda: diretório, estado do git e worktree.
type SessionLocation struct {
	Cwd, Branch, GitHead, Worktree string
	WorktreeLinked                 bool
}

// UpdateSessionLocation grava o novo local da sessão (o agente trocou de
// diretório, por exemplo ao entrar num worktree). O projeto ligado não muda.
func (s *Store) UpdateSessionLocation(id string, l SessionLocation) error {
	res, err := s.DB.Exec(`UPDATE sessions SET cwd=?, branch=?, git_head=?, worktree=?, worktree_linked=? WHERE id=?`,
		l.Cwd, l.Branch, l.GitHead, l.Worktree, l.WorktreeLinked, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
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

// MarkPIDDead registra que o PID da sessão sumiu. Só grava na primeira vez:
// o instante original é o início da carência.
func (s *Store) MarkPIDDead(id string, at time.Time) error {
	res, err := s.DB.Exec(`UPDATE sessions SET pid_dead_at=? WHERE id=? AND pid_dead_at IS NULL`, fmtTime(at), id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		_, err := s.GetSession(id) // já marcada, ou inexistente
		return err
	}
	return nil
}

// ClearPIDDead limpa pid_dead_at (o PID voltou a existir).
func (s *Store) ClearPIDDead(id string) error {
	_, err := s.DB.Exec(`UPDATE sessions SET pid_dead_at=NULL WHERE id=? AND pid_dead_at IS NOT NULL`, id)
	return err
}

// MarkReminded marca que a sessão já recebeu o lembrete de handoff.
func (s *Store) MarkReminded(id string) error {
	res, err := s.DB.Exec(`UPDATE sessions SET reminded=1 WHERE id=?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// LiveSessions lista as sessões não encerradas (inclusive as com processo
// morto, ainda na carência), da mais antiga para a mais nova. Para a regra de
// "viva" use domain.Session.Live.
func (s *Store) LiveSessions() ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions WHERE ended_at IS NULL ORDER BY julianday(started_at), id`)
}

// ProjectSessionsSince lista as sessões do projeto não encerradas e as
// encerradas a partir de since, da atividade mais recente para a mais antiga.
func (s *Store) ProjectSessionsSince(projectID int64, since time.Time) ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions
		WHERE project_id=? AND (ended_at IS NULL OR julianday(ended_at) >= julianday(?))
		ORDER BY julianday(last_event_at) DESC, id`, projectID, fmtTime(since))
}

// ProjectsSessionsSince é ProjectSessionsSince para vários projetos (a família
// de um monorepo), no mesmo critério e ordem.
func (s *Store) ProjectsSessionsSince(projectIDs []int64, since time.Time) ([]domain.Session, error) {
	if len(projectIDs) == 0 {
		return nil, nil
	}
	args := []any{}
	for _, id := range projectIDs {
		args = append(args, id)
	}
	args = append(args, fmtTime(since))
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions
		WHERE project_id IN (`+placeholders(len(projectIDs))+`) AND (ended_at IS NULL OR julianday(ended_at) >= julianday(?))
		ORDER BY julianday(last_event_at) DESC, id`, args...)
}

// SessionsForCard lista as sessões do card, da mais antiga para a mais nova:
// as ligadas a ele agora e as que deixaram uso ou eventos nele (histórico de
// uma sessão que trocou de card ou foi deslocada).
func (s *Store) SessionsForCard(cardID int64) ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions
		WHERE card_id=?
			OR id IN (SELECT session_id FROM usage WHERE card_id=?)
			OR id IN (SELECT session_id FROM events WHERE card_id=?)
		ORDER BY julianday(started_at), id`, cardID, cardID, cardID)
}

// HasLiveSession diz se alguma sessão viva (não encerrada e com processo
// existindo) é dona do card.
func (s *Store) HasLiveSession(cardID int64) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE card_id=? AND `+liveCond, cardID).Scan(&n)
	return n > 0, err
}

// EndedWithoutCardSince lista as sessões sem card encerradas a partir de
// since, da mais nova para a mais antiga.
func (s *Store) EndedWithoutCardSince(since time.Time) ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions
		WHERE card_id IS NULL AND ended_at IS NOT NULL AND julianday(ended_at) >= julianday(?)
		ORDER BY julianday(ended_at) DESC, id DESC`, fmtTime(since))
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
	if next == cur && next != domain.EmAndamento {
		return nil
	}
	if next == domain.EmAndamento {
		// Novo dono vivo: as marcas da pausa e do handoff anterior caem.
		_, err := tx.Exec(`UPDATE cards SET col=?, pause_reason='', handoff_incomplete=0, blocked_by='', updated_at=? WHERE id=?`, string(next), fmtTime(time.Now()), cardID)
		return err
	}
	_, err := tx.Exec(`UPDATE cards SET col=?, updated_at=? WHERE id=?`, string(next), fmtTime(time.Now()), cardID)
	return err
}

// AttachSession liga a sessão ao card, move o card para em_andamento e copia
// para o card os eventos e o uso da sessão ainda sem card. Se a sessão já
// terminou, o card vai para pausado (nunca em_andamento sem dono vivo). Se a
// sessão viva estava em outro card e ele fica sem dono vivo, esse card também
// pausa. Sessão aberta cujo processo morreu (pid_dead_at) conta como
// encerrada para o card. Se outra sessão viva é dona do card devolve
// ErrCardBusy, a menos que force (a outra é desligada). Outra sessão não
// encerrada cujo processo morreu não é viva: não bloqueia e é desligada do
// card. Quem é desligado perde a posse, mas seus eventos e uso continuam no
// card (histórico).
func (s *Store) AttachSession(sessionID, cardKey string, force bool) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := attachSessionTx(tx, sessionID, cardKey, force); err != nil {
		return err
	}
	return tx.Commit()
}

// attachSessionTx é o corpo de AttachSession dentro de uma transação do chamador.
func attachSessionTx(tx *sql.Tx, sessionID, cardKey string, force bool) error {
	var ended, dead sql.NullString
	var prevCard sql.NullInt64
	if err := tx.QueryRow(`SELECT ended_at, pid_dead_at, card_id FROM sessions WHERE id=?`, sessionID).Scan(&ended, &dead, &prevCard); errors.Is(err, sql.ErrNoRows) {
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
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE card_id=? AND `+liveCond+` AND id<>?`, cardID, sessionID).Scan(&others); err != nil {
		return err
	}
	if others > 0 && !force {
		return ErrCardBusy
	}
	// Desliga as outras sessões não encerradas: com force, todas; sem force,
	// só as de processo morto (senão o reconciliador, ao encerrá-las, pausaria
	// o card da nova dona).
	detach := `UPDATE sessions SET card_id=NULL WHERE card_id=? AND ended_at IS NULL AND id<>?`
	if !force {
		detach += ` AND pid_dead_at IS NOT NULL`
	}
	if _, err := tx.Exec(detach, cardID, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sessions SET card_id=? WHERE id=?`, cardID, sessionID); err != nil {
		return err
	}
	if prevCard.Valid && prevCard.Int64 != cardID && !ended.Valid {
		// A sessão viva saiu do card anterior: sem outro dono vivo, ele pausa.
		var live int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE card_id=? AND `+liveCond, prevCard.Int64).Scan(&live); err != nil {
			return err
		}
		if live == 0 {
			if err := moveCardTx(tx, prevCard.Int64, domain.SessionEnded); err != nil {
				return err
			}
		}
	}
	// Só os eventos e o uso ainda sem card vêm junto: o que a sessão fez num
	// card anterior continua no histórico (e no custo) dele.
	if _, err := tx.Exec(`UPDATE events SET card_id=? WHERE session_id=? AND card_id IS NULL`, cardID, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE usage SET card_id=? WHERE session_id=? AND card_id IS NULL`, cardID, sessionID); err != nil {
		return err
	}
	if err := moveCardTx(tx, cardID, domain.SessionAttached); err != nil {
		return err
	}
	if ended.Valid || dead.Valid {
		// Sessão encerrada (ou com o processo morto) não é dona viva: o card
		// fica pausado.
		if err := moveCardTx(tx, cardID, domain.SessionEnded); err != nil {
			return err
		}
	}
	return nil
}

// ReopenSession reabre a sessão que voltou a mandar eventos (retomada com o
// mesmo ID do provedor, ou processo dado como morto que segue vivo): limpa
// ended_at, end_reason e pid_dead_at e grava o PID e a última atividade. Se a
// sessão tem card: com outra sessão viva dona dele, a reaberta é desligada;
// senão, um card pausado volta para em_andamento (as demais colunas ficam).
func (s *Store) ReopenSession(id string, pid int, at time.Time) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var cardID sql.NullInt64
	if err := tx.QueryRow(`SELECT card_id FROM sessions WHERE id=?`, id).Scan(&cardID); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sessions SET ended_at=NULL, end_reason='', pid_dead_at=NULL, pid=?, last_event_at=? WHERE id=?`,
		pid, fmtTime(at), id); err != nil {
		return err
	}
	if cardID.Valid {
		var others int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE card_id=? AND `+liveCond+` AND id<>?`, cardID.Int64, id).Scan(&others); err != nil {
			return err
		}
		if others > 0 {
			if _, err := tx.Exec(`UPDATE sessions SET card_id=NULL WHERE id=?`, id); err != nil {
				return err
			}
		} else {
			var col domain.Column
			if err := tx.QueryRow(`SELECT col FROM cards WHERE id=?`, cardID.Int64).Scan(&col); err != nil {
				return err
			}
			if col == domain.Pausado {
				if err := moveCardTx(tx, cardID.Int64, domain.SessionAttached); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// SessionReminded diz se a sessão já recebeu o lembrete de criar card.
func (s *Store) SessionReminded(id string) (bool, error) {
	var r bool
	err := s.DB.QueryRow(`SELECT reminded FROM sessions WHERE id=?`, id).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return r, err
}

// SessionEditedAfter diz se a sessão registrou file_edit no card depois de
// after (nil = qualquer file_edit).
func (s *Store) SessionEditedAfter(sessionID string, cardID int64, after *time.Time) (bool, error) {
	q := `SELECT EXISTS(SELECT 1 FROM events WHERE session_id=? AND card_id=? AND kind=?`
	args := []any{sessionID, cardID, domain.EvFileEdit}
	if after != nil {
		q += ` AND julianday(at) > julianday(?)`
		args = append(args, fmtTime(*after))
	}
	var has bool
	err := s.DB.QueryRow(q+`)`, args...).Scan(&has)
	return has, err
}

// DistinctEditedFiles conta os arquivos distintos editados pela sessão.
func (s *Store) DistinctEditedFiles(sessionID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(DISTINCT json_extract(data, '$.path')) FROM events
		WHERE session_id=? AND kind=? AND COALESCE(json_extract(data, '$.path'), '') <> ''`,
		sessionID, domain.EvFileEdit).Scan(&n)
	return n, err
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
	return scanEvents(rows)
}

// SessionEvents devolve os eventos mais recentes das sessões dadas, do mais
// novo para o mais antigo.
func (s *Store) SessionEvents(ids []string, limit int) ([]domain.Event, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := s.DB.Query(`SELECT id, session_id, card_id, kind, data, at FROM events
		WHERE session_id IN (?`+strings.Repeat(",?", len(ids)-1)+`) ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]domain.Event, error) {
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var cardID sql.NullInt64
		var data, at string
		var err error
		if err = rows.Scan(&e.ID, &e.SessionID, &cardID, &e.Kind, &data, &at); err != nil {
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

// LastSessionEvent devolve o evento mais recente da sessão, ignorando
// heartbeats; nil se não houver.
func (s *Store) LastSessionEvent(sessionID string) (*domain.Event, error) {
	var e domain.Event
	var cardID sql.NullInt64
	var data, at string
	err := s.DB.QueryRow(`SELECT id, session_id, card_id, kind, data, at FROM events
		WHERE session_id=? AND kind<>? ORDER BY id DESC LIMIT 1`, sessionID, domain.EvHeartbeat).
		Scan(&e.ID, &e.SessionID, &cardID, &e.Kind, &data, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cardID.Valid {
		e.CardID = &cardID.Int64
	}
	e.Data = json.RawMessage(data)
	if e.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
		return nil, err
	}
	return &e, nil
}

// EventWithContext é um evento com o agente da sessão e a chave do card.
type EventWithContext struct {
	domain.Event
	Agent   string `json:"agent"`
	CardKey string `json:"card_key"`
}

// EventsByKind devolve os últimos eventos do tipo kind, do mais novo para o
// mais antigo, com o agente da sessão e a chave do card ("" sem card).
func (s *Store) EventsByKind(kind string, limit int) ([]EventWithContext, error) {
	rows, err := s.DB.Query(`SELECT e.id, e.session_id, e.card_id, e.kind, e.data, e.at,
			COALESCE(s.agent, ''), COALESCE(c.key, '')
		FROM events e
		LEFT JOIN sessions s ON s.id = e.session_id
		LEFT JOIN cards c ON c.id = e.card_id
		WHERE e.kind = ? ORDER BY e.id DESC LIMIT ?`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventWithContext
	for rows.Next() {
		var e EventWithContext
		var cardID sql.NullInt64
		var data, at string
		if err := rows.Scan(&e.ID, &e.SessionID, &cardID, &e.Kind, &data, &at, &e.Agent, &e.CardKey); err != nil {
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
