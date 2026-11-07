package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/prompts"
)

const promptCols = `id, name, slug, tag, description, body, agents, project_id, is_default_handoff, uses, last_used_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanPrompt(r rowScanner) (domain.Prompt, error) {
	var p domain.Prompt
	var agents string
	var pid sql.NullInt64
	var last sql.NullString
	if err := r.Scan(&p.ID, &p.Name, &p.Slug, &p.Tag, &p.Description, &p.Body, &agents, &pid, &p.IsDefaultHandoff, &p.Uses, &last); err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(agents), &p.Agents); err != nil {
		return p, fmt.Errorf("agents do prompt %s: %w", p.Slug, err)
	}
	if pid.Valid {
		p.ProjectID = &pid.Int64
	}
	if last.Valid {
		t, err := time.Parse(time.RFC3339Nano, last.String)
		if err != nil {
			return p, err
		}
		p.LastUsedAt = &t
	}
	return p, nil
}

// ListPrompts lista os prompts por id.
func (s *Store) ListPrompts() ([]domain.Prompt, error) {
	rows, err := s.DB.Query(`SELECT ` + promptCols + ` FROM prompts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Prompt
	for rows.Next() {
		p, err := scanPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPrompt busca pelo slug.
func (s *Store) GetPrompt(slug string) (domain.Prompt, error) {
	p, err := scanPrompt(s.DB.QueryRow(`SELECT `+promptCols+` FROM prompts WHERE slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// DefaultHandoffPrompt devolve o prompt marcado como padrão de handoff.
func (s *Store) DefaultHandoffPrompt() (domain.Prompt, error) {
	p, err := scanPrompt(s.DB.QueryRow(`SELECT ` + promptCols + ` FROM prompts WHERE is_default_handoff = 1 ORDER BY id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func agentsJSON(a []string) (string, error) {
	if a == nil {
		a = []string{}
	}
	b, err := json.Marshal(a)
	return string(b), err
}

// CreatePrompt insere o prompt; se IsDefaultHandoff, os demais perdem a marca.
func (s *Store) CreatePrompt(p domain.Prompt) (domain.Prompt, error) {
	agents, err := agentsJSON(p.Agents)
	if err != nil {
		return p, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if p.IsDefaultHandoff {
		if _, err := tx.Exec(`UPDATE prompts SET is_default_handoff = 0`); err != nil {
			return p, err
		}
	}
	res, err := tx.Exec(`INSERT INTO prompts(name, slug, tag, description, body, agents, project_id, is_default_handoff)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.Name, p.Slug, p.Tag, p.Description, p.Body, agents, p.ProjectID, p.IsDefaultHandoff)
	if err != nil {
		return p, fmt.Errorf("criar prompt: %w", err)
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

// UpdatePrompt atualiza pelo slug (nome, tag, descrição, corpo, agentes,
// projeto, padrão de handoff); uso e último uso não mudam.
func (s *Store) UpdatePrompt(p domain.Prompt) error {
	agents, err := agentsJSON(p.Agents)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if p.IsDefaultHandoff {
		if _, err := tx.Exec(`UPDATE prompts SET is_default_handoff = 0 WHERE slug <> ?`, p.Slug); err != nil {
			return err
		}
	}
	res, err := tx.Exec(`UPDATE prompts SET name=?, tag=?, description=?, body=?, agents=?, project_id=?, is_default_handoff=? WHERE slug=?`,
		p.Name, p.Tag, p.Description, p.Body, agents, p.ProjectID, p.IsDefaultHandoff, p.Slug)
	if err != nil {
		return err
	}
	if err := requireAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePrompt remove pelo slug.
func (s *Store) DeletePrompt(slug string) error {
	res, err := s.DB.Exec(`DELETE FROM prompts WHERE slug = ?`, slug)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// MarkPromptUsed incrementa o uso e registra o instante.
func (s *Store) MarkPromptUsed(slug string, at time.Time) error {
	res, err := s.DB.Exec(`UPDATE prompts SET uses = uses + 1, last_used_at = ? WHERE slug = ?`,
		at.UTC().Format(time.RFC3339Nano), slug)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// seedPrompts insere prompts.Seeds() apenas se a tabela está vazia.
func (s *Store) seedPrompts() error {
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM prompts`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, p := range prompts.Seeds() {
		if _, err := s.CreatePrompt(p); err != nil {
			return fmt.Errorf("seed %s: %w", p.Slug, err)
		}
	}
	return nil
}
