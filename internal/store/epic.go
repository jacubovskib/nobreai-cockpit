package store

import (
	"database/sql"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// recalcEpicTx recalcula a coluna do épico a partir das filhas
// (domain.EpicColumn) e grava se mudou. Card que não é épico fica como está.
func recalcEpicTx(tx *sql.Tx, epicID int64) error {
	var cur domain.Column
	var typ domain.CardType
	if err := tx.QueryRow(`SELECT col, type FROM cards WHERE id=?`, epicID).Scan(&cur, &typ); err != nil {
		return err
	}
	if typ != domain.TypeEpico {
		return nil
	}
	rows, err := tx.Query(`SELECT col FROM cards WHERE parent_id=?`, epicID)
	if err != nil {
		return err
	}
	var children []domain.Column
	for rows.Next() {
		var c domain.Column
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return err
		}
		children = append(children, c)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	next := domain.EpicColumn(cur, children)
	if next == cur {
		return nil
	}
	_, err = tx.Exec(`UPDATE cards SET col=?, updated_at=? WHERE id=?`, string(next), fmtTime(time.Now()), epicID)
	return err
}

// recalcParentTx recalcula o épico pai do card, se houver.
func recalcParentTx(tx *sql.Tx, cardID int64) error {
	var parent sql.NullInt64
	if err := tx.QueryRow(`SELECT parent_id FROM cards WHERE id=?`, cardID).Scan(&parent); err != nil {
		return err
	}
	if !parent.Valid {
		return nil
	}
	return recalcEpicTx(tx, parent.Int64)
}
