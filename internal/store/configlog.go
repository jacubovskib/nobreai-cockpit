package store

import (
	"encoding/json"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

const configLogKeep = 500

// AddConfigLog grava uma linha no registro de mudanças de configuração e
// apaga as que passam das 500 mais recentes.
func (s *Store) AddConfigLog(kind string, data any, at time.Time) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO config_log(at, kind, data) VALUES (?, ?, ?)`, fmtTime(at), kind, string(b)); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM config_log WHERE id NOT IN (SELECT id FROM config_log ORDER BY id DESC LIMIT ?)`, configLogKeep); err != nil {
		return err
	}
	return tx.Commit()
}

// RecentConfigLog devolve até limit linhas, a mais recente primeiro.
func (s *Store) RecentConfigLog(limit int) ([]domain.ConfigLogEntry, error) {
	rows, err := s.DB.Query(`SELECT id, at, kind, data FROM config_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ConfigLogEntry
	for rows.Next() {
		var e domain.ConfigLogEntry
		var at, data string
		if err := rows.Scan(&e.ID, &at, &e.Kind, &data); err != nil {
			return nil, err
		}
		if e.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}
