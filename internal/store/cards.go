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

const cardCols = `id, key, title, goal, done_when, where_stopped, where_stopped_at, how_to_test, branch, project_id, col, created_at, updated_at, branch_head, branch_diverged, branch_merged_at`

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
		&c.HowToTest, &c.Branch, &c.ProjectID, &c.Column, &created, &updated, &c.BranchHead, &c.BranchDiverged, &mergedAt)
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

// ListIntegrationCandidates lista os cards com branch ainda não integrado que
// estão em em_andamento, pausado ou revisao.
func (s *Store) ListIntegrationCandidates() ([]domain.Card, error) {
	rows, err := s.DB.Query(`SELECT ` + cardCols + ` FROM cards WHERE col IN ('em_andamento','pausado','revisao') AND branch<>'' AND branch_merged_at IS NULL ORDER BY id`)
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
	res, err := s.DB.Exec(`UPDATE cards SET title=?, goal=?, done_when=?, where_stopped=?, where_stopped_at=?, how_to_test=?, branch=?, project_id=?, col=?, updated_at=? WHERE id=?`,
		c.Title, c.Goal, c.DoneWhen, c.WhereStopped, nullableTime(c.WhereStoppedAt), c.HowToTest, c.Branch,
		c.ProjectID, string(c.Column), fmtTime(time.Now()), c.ID)
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

// SetBranchHead grava o último commit visto no branch do card e se divergiu.
func (s *Store) SetBranchHead(cardID int64, head string, diverged bool) error {
	res, err := s.DB.Exec(`UPDATE cards SET branch_head=?, branch_diverged=? WHERE id=?`, head, diverged, cardID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// MarkBranchMerged registra quando o branch do card entrou na base.
func (s *Store) MarkBranchMerged(cardID int64, at time.Time) error {
	res, err := s.DB.Exec(`UPDATE cards SET branch_merged_at=? WHERE id=?`, fmtTime(at), cardID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// MoveCard move o card manualmente, sem aplicar regras.
func (s *Store) MoveCard(key string, to domain.Column) error {
	res, err := s.DB.Exec(`UPDATE cards SET col=?, updated_at=? WHERE key=?`, string(to), fmtTime(time.Now()), key)
	if err != nil {
		return err
	}
	return requireAffected(res)
}
