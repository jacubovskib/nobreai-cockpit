package store

import (
	"encoding/json"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// GetSettings lê as preferências; chaves ausentes assumem o padrão.
func (s *Store) GetSettings() (domain.Settings, error) {
	st := domain.DefaultSettings()
	rows, err := s.DB.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	raw := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		raw[k] = json.RawMessage(v)
	}
	if err := rows.Err(); err != nil {
		return st, err
	}
	// Decodifica sobre os padrões: só as chaves presentes sobrescrevem.
	// As regras são mescladas para que regras novas mantenham o padrão.
	rules := st.Rules
	if v, ok := raw["rules"]; ok {
		var stored map[string]bool
		if err := json.Unmarshal(v, &stored); err != nil {
			return st, err
		}
		for k, b := range stored {
			rules[k] = b
		}
		delete(raw, "rules")
	}
	obj := make(map[string]json.RawMessage, len(raw))
	for k, v := range raw {
		obj[k] = v
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	st.Rules = rules
	return st, nil
}

// PutSettings grava cada campo como uma linha chave → JSON.
func (s *Store) PutSettings(st domain.Settings) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range fields {
		if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, string(v)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
