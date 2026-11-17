package store

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// CreateProject cadastra um projeto (captura ligada por padrão).
func (s *Store) CreateProject(name, path string) (domain.Project, error) {
	res, err := s.DB.Exec(`INSERT INTO projects(name, path) VALUES (?, ?)`, name, path)
	if err != nil {
		return domain.Project{}, fmt.Errorf("criar projeto: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Project{}, err
	}
	return domain.Project{ID: id, Name: name, Path: path, Capture: true}, nil
}

// ListProjects lista os projetos por id.
func (s *Store) ListProjects() ([]domain.Project, error) {
	rows, err := s.DB.Query(`SELECT id, name, COALESCE(path, ''), capture, base_branch, parent_id FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Capture, &p.BaseBranch, &p.ParentID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject atualiza nome, caminho e captura.
func (s *Store) UpdateProject(p domain.Project) error {
	res, err := s.DB.Exec(`UPDATE projects SET name=?, path=?, capture=?, base_branch=? WHERE id=?`, p.Name, p.Path, p.Capture, p.BaseBranch, p.ID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// SetProjectBase grava o branch base do projeto ("" volta a detectar pelo git).
func (s *Store) SetProjectBase(id int64, base string) error {
	res, err := s.DB.Exec(`UPDATE projects SET base_branch=? WHERE id=?`, base, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// DeleteProject remove o projeto.
func (s *Store) DeleteProject(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ProjectForPath retorna o projeto cujo path é o prefixo mais longo de cwd
// (respeitando limite de diretório), ou nil se nenhum casar.
func (s *Store) ProjectForPath(cwd string) (*domain.Project, error) {
	ps, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	var best *domain.Project
	for i := range ps {
		p := ps[i]
		if p.Path == "" { // grupo: não tem pasta
			continue
		}
		root := strings.TrimRight(p.Path, "/")
		if cwd != root && !strings.HasPrefix(cwd, root+"/") {
			continue
		}
		if best == nil || len(p.Path) > len(best.Path) {
			best = &ps[i]
		}
	}
	return best, nil
}

// ReassignSessions liga ao projeto as sessões sem projeto cujo cwd é root ou
// fica abaixo dele (mesmo limite de diretório de ProjectForPath) e liga a
// captura completa. Devolve quantas mudaram.
func (s *Store) ReassignSessions(projectID int64, root string) (int, error) {
	root = strings.TrimRight(root, "/")
	prefix := root + "/"
	res, err := s.DB.Exec(`UPDATE sessions SET project_id=?, full_capture=1
		WHERE project_id IS NULL AND (cwd=? OR substr(cwd, 1, ?)=?)`,
		projectID, root, utf8.RuneCountInString(prefix), prefix)
	if err != nil {
		return 0, fmt.Errorf("religar sessões: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// UnassignedLinkedWorktrees lista os worktrees (distintos) das sessões sem
// projeto que rodam num worktree ligado.
func (s *Store) UnassignedLinkedWorktrees() ([]string, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT worktree FROM sessions
		WHERE project_id IS NULL AND worktree_linked=1 AND worktree<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AssignSessionsInWorktree liga ao projeto as sessões sem projeto que rodam
// no worktree e liga a captura completa. Devolve quantas mudaram.
func (s *Store) AssignSessionsInWorktree(projectID int64, worktree string) (int, error) {
	res, err := s.DB.Exec(`UPDATE sessions SET project_id=?, full_capture=1
		WHERE project_id IS NULL AND worktree_linked=1 AND worktree=?`, projectID, worktree)
	if err != nil {
		return 0, fmt.Errorf("religar sessões do worktree: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
