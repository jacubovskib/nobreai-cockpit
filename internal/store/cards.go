package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// ErrNotFound indica que o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// CardFilter filtra ListCards. Campos zerados não filtram.
type CardFilter struct {
	ProjectID int64
	Agent     string // cards com alguma sessão desse agente
}

const cardCols = `id, key, title, goal, done_when, where_stopped, where_stopped_at, how_to_test, branch, project_id, col, created_at, updated_at, branch_head, branch_diverged, branch_merged_at, pause_reason, handoff_incomplete, blocked_by`

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func scanCard(sc interface{ Scan(...any) error }) (domain.Card, error) {
	var c domain.Card
	var stopAt sql.NullString
	var created, updated string
	var mergedAt sql.NullString
	err := sc.Scan(&c.ID, &c.Key, &c.Title, &c.Goal, &c.DoneWhen, &c.WhereStopped, &stopAt,
		&c.HowToTest, &c.Branch, &c.ProjectID, &c.Column, &created, &updated, &c.BranchHead, &c.BranchDiverged, &mergedAt, &c.PauseReason, &c.HandoffIncomplete, &c.BlockedBy)
	if err != nil {
		return c, err
	}
	if mergedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, mergedAt.String)
		if err != nil {
			return c, err
		}
		c.BranchMergedAt = &t
	}
	if stopAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, stopAt.String)
		if err != nil {
			return c, err
		}
		c.WhereStoppedAt = &t
	}
	if c.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return c, err
	}
	c.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return c, err
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtTime(*t)
}

// CreateCard insere o card, atribuindo Key = NAC-<id> na mesma transação.
func (s *Store) CreateCard(c domain.Card) (domain.Card, error) {
	if c.Column == "" {
		c.Column = domain.Backlog
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	tx, err := s.DB.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	c, err = createCardTx(tx, c, now)
	if err != nil {
		return c, err
	}
	return c, tx.Commit()
}

// CreateCardAndAttach cria o card e liga a sessão a ele numa só transação:
// se a ligação falhar, o card não fica órfão.
func (s *Store) CreateCardAndAttach(c domain.Card, sessionID string) (domain.Card, error) {
	if c.Column == "" {
		c.Column = domain.Backlog
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	tx, err := s.DB.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	c, err = createCardTx(tx, c, now)
	if err != nil {
		return c, err
	}
	if err := attachSessionTx(tx, sessionID, c.Key, false); err != nil {
		return c, err
	}
	return c, tx.Commit()
}

func createCardTx(tx *sql.Tx, c domain.Card, now time.Time) (domain.Card, error) {
	res, err := tx.Exec(`INSERT INTO cards(key, project_id, title, goal, done_when, where_stopped, where_stopped_at, how_to_test, branch, col, created_at, updated_at)
		VALUES ('', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ProjectID, c.Title, c.Goal, c.DoneWhen, c.WhereStopped, nullableTime(c.WhereStoppedAt),
		c.HowToTest, c.Branch, string(c.Column), fmtTime(now), fmtTime(now))
	if err != nil {
		return c, fmt.Errorf("criar card: %w", err)
	}
	c.ID, err = res.LastInsertId()
	if err != nil {
		return c, err
	}
	c.Key = "NAC-" + strconv.FormatInt(c.ID, 10)
	if _, err := tx.Exec(`UPDATE cards SET key=? WHERE id=?`, c.Key, c.ID); err != nil {
		return c, err
	}
	return c, nil
}

// GetCard busca um card pela chave (ErrNotFound se não existir).
func (s *Store) GetCard(key string) (domain.Card, error) {
	c, err := scanCard(s.DB.QueryRow(`SELECT `+cardCols+` FROM cards WHERE key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// GetCardByID busca um card pelo id (ErrNotFound se não existir).
func (s *Store) GetCardByID(id int64) (domain.Card, error) {
	c, err := scanCard(s.DB.QueryRow(`SELECT `+cardCols+` FROM cards WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// ListCards lista cards por id, com filtros opcionais.
func (s *Store) ListCards(f CardFilter) ([]domain.Card, error) {
	q := `SELECT ` + cardCols + ` FROM cards WHERE 1=1`
	var args []any
	if f.ProjectID != 0 {
		q += ` AND project_id=?`
		args = append(args, f.ProjectID)
	}
	if f.Agent != "" {
		q += ` AND id IN (SELECT card_id FROM sessions WHERE agent=? AND card_id IS NOT NULL)`
		args = append(args, f.Agent)
	}
	rows, err := s.DB.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListIntegrationCandidates lista os cards com branch que estão em
// em_andamento, pausado ou revisao (inclusive os já integrados: o branch pode
// ganhar commits novos e voltar a ser acompanhado).
func (s *Store) ListIntegrationCandidates() ([]domain.Card, error) {
	rows, err := s.DB.Query(`SELECT ` + cardCols + ` FROM cards WHERE col IN ('em_andamento','pausado','revisao') AND branch<>'' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCard grava os campos editáveis do card (identificado por ID).
func (s *Store) UpdateCard(c domain.Card) error {
	res, err := s.DB.Exec(`UPDATE cards SET title=?, goal=?, done_when=?, where_stopped=?, where_stopped_at=?, how_to_test=?, branch=?, project_id=?, col=?, pause_reason=?, handoff_incomplete=?, blocked_by=?, updated_at=? WHERE id=?`,
		c.Title, c.Goal, c.DoneWhen, c.WhereStopped, nullableTime(c.WhereStoppedAt), c.HowToTest, c.Branch,
		c.ProjectID, string(c.Column), c.PauseReason, c.HandoffIncomplete, c.BlockedBy, fmtTime(time.Now()), c.ID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// SetCardBranch troca o branch do card se for diferente do atual; nesse caso
// zera as marcas do branch anterior (head, divergência, merge). Devolve se mudou.
func (s *Store) SetCardBranch(cardID int64, branch string) (bool, error) {
	res, err := s.DB.Exec(`UPDATE cards SET branch=?, branch_head='', branch_diverged=0, branch_merged_at=NULL, updated_at=? WHERE id=? AND branch<>?`,
		branch, fmtTime(time.Now()), cardID, branch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetBranchHead grava o último commit visto no branch do card e se divergiu,
// e zera a marca de integração (commit novo fora da base reabre o
// acompanhamento). Só grava se o card ainda está em branch (a leitura pode
// estar velha); devolve se gravou.
func (s *Store) SetBranchHead(cardID int64, branch, head string, diverged bool) (bool, error) {
	res, err := s.DB.Exec(`UPDATE cards SET branch_head=?, branch_diverged=?, branch_merged_at=NULL WHERE id=? AND branch=?`,
		head, diverged, cardID, branch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// MarkBranchMerged registra quando o branch do card entrou na base. Só grava se
// o card ainda está em branch e não foi marcado; devolve se gravou.
func (s *Store) MarkBranchMerged(cardID int64, branch string, at time.Time) (bool, error) {
	return markBranchMerged(s.DB, cardID, branch, at)
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func markBranchMerged(db execer, cardID int64, branch string, at time.Time) (bool, error) {
	res, err := db.Exec(`UPDATE cards SET branch_merged_at=? WHERE id=? AND branch=? AND branch_merged_at IS NULL`,
		fmtTime(at), cardID, branch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RecordIntegration registra numa transação que o branch do card entrou na
// base: a marca (MarkBranchMerged), a decisão d e, se o card ainda está em
// revisao, a ida para concluido. Se a marca não pega (o branch do card mudou ou
// já estava marcado), nada é gravado. Devolve se gravou.
func (s *Store) RecordIntegration(cardID int64, branch string, at time.Time, d domain.Decision) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	marked, err := markBranchMerged(tx, cardID, branch, at)
	if err != nil || !marked {
		return false, err
	}
	d.CardID = cardID
	if d.At.IsZero() {
		d.At = at
	}
	if _, err := tx.Exec(`INSERT INTO decisions(card_id, what, why, session_id, at) VALUES (?, ?, ?, ?, ?)`,
		d.CardID, d.What, d.Why, d.SessionID, fmtTime(d.At)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE cards SET col=?, updated_at=? WHERE id=? AND col=?`,
		string(domain.Concluido), fmtTime(time.Now()), cardID, string(domain.Revisao)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// MoveCard move o card manualmente, sem aplicar regras.
func (s *Store) MoveCard(key string, to domain.Column) error {
	res, err := s.DB.Exec(`UPDATE cards SET col=?, updated_at=? WHERE key=?`, string(to), fmtTime(time.Now()), key)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// PauseForLimit move para pausado o card em andamento e grava o motivo
// "limite de custo". Devolve se mudou.
func (s *Store) PauseForLimit(cardID int64, at time.Time) (bool, error) {
	res, err := s.DB.Exec(`UPDATE cards SET col=?, pause_reason=?, updated_at=? WHERE id=? AND col=?`,
		string(domain.Pausado), domain.PauseReasonLimit, fmtTime(at), cardID, string(domain.EmAndamento))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ResumeFromLimit devolve para em andamento o card pausado por limite de
// custo e limpa o motivo, a marca de handoff incompleto e o bloqueio. Devolve se mudou.
func (s *Store) ResumeFromLimit(cardID int64, at time.Time) (bool, error) {
	res, err := s.DB.Exec(`UPDATE cards SET col=?, pause_reason='', handoff_incomplete=0, blocked_by='', updated_at=? WHERE id=? AND col=? AND pause_reason=?`,
		string(domain.EmAndamento), fmtTime(at), cardID, string(domain.Pausado), domain.PauseReasonLimit)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetHandoffIncomplete grava a marca de handoff incompleto do card.
func (s *Store) SetHandoffIncomplete(cardID int64, v bool, at time.Time) error {
	res, err := s.DB.Exec(`UPDATE cards SET handoff_incomplete=?, updated_at=? WHERE id=?`, v, fmtTime(at), cardID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}
