package store

import (
	"database/sql"
	"encoding/json"
	"errors"

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

// GetKV lê uma chave livre da tabela settings (valor em JSON) para v. Devolve
// false, sem tocar em v, quando a chave não existe. GetSettings ignora as
// chaves que não são campos de domain.Settings.
func (s *Store) GetKV(key string, v any) (bool, error) {
	var raw string
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

// PutKV grava (ou troca) uma chave livre da tabela settings como JSON.
func (s *Store) PutKV(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(b))
	return err
}
