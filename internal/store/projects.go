package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// CreateProject cadastra um projeto (captura ligada por padrão).
func (s *Store) CreateProject(name, path string) (domain.Project, error) {
	kind := "repo"
	if path == "" {
		kind = "group" // sem pasta só existe grupo
	}
	return s.insertProject(name, path, kind)
}

func (s *Store) insertProject(name, path, kind string) (domain.Project, error) {
	res, err := s.DB.Exec(`INSERT INTO projects(name, path, kind) VALUES (?, NULLIF(?, ''), ?)`, name, path, kind)
	if err != nil {
		return domain.Project{}, fmt.Errorf("criar projeto: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Project{}, err
	}
	return domain.Project{ID: id, Name: name, Path: path, Capture: true, Kind: kind}, nil
}

// ListProjects lista os projetos por id.
func (s *Store) ListProjects() ([]domain.Project, error) {
	rows, err := s.DB.Query(`SELECT id, name, COALESCE(path, ''), capture, base_branch, parent_id, kind FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Capture, &p.BaseBranch, &p.ParentID, &p.Kind); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject atualiza nome, caminho e captura. Num projeto com
// sub-projetos, a captura nova vale também para eles. O nome continua único na
// família e a pasta de um sub-projeto de monorepo continua dentro da do pai.
func (s *Store) UpdateProject(p domain.Project) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cur, err := getProjectTx(tx, p.ID)
	if err != nil {
		return err
	}
	kids, err := childrenTx(tx, p.ID)
	if err != nil {
		return err
	}
	if cur.ParentID != nil {
		parent, err := getProjectTx(tx, *cur.ParentID)
		if err != nil {
			return err
		}
		if p.Path == "" {
			return familyErr("o sub-projeto precisa de uma pasta")
		}
		if err := checkInside(parent, p.Path); err != nil {
			return err
		}
		p.Capture = parent.Capture // a captura do sub-projeto é a do pai
	} else if cur.IsGroup() {
		// grupo pode ganhar ou trocar de pasta, desde que os filhos fiquem dentro
		if p.Path == "" && cur.Path != "" && len(kids) > 0 {
			return familyErr("um grupo com sub-projetos não pode ficar sem pasta")
		}
		if p.Path != "" && p.Path != cur.Path {
			clean, err := checkGroupFolder(tx, p.Path, cur.ID)
			if err != nil {
				return err
			}
			p.Path = clean
			for _, k := range kids {
				if !inside(p.Path, k.Path) {
					return familyErr("o sub-projeto " + k.Name + " fica fora da pasta nova do grupo")
				}
			}
		}
	} else if p.Path == "" {
		return familyErr("um repositório precisa de uma pasta")
	} else if p.Path != cur.Path {
		for _, k := range kids {
			if !inside(p.Path, k.Path) {
				return familyErr("o sub-projeto " + k.Name + " fica fora da pasta nova do projeto")
			}
		}
	}
	if !strings.EqualFold(cur.Name, p.Name) {
		if err := checkNameFree(tx, cur, p.Name); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE projects SET name=?, path=NULLIF(?, ''), capture=?, base_branch=? WHERE id=?`,
		p.Name, p.Path, p.Capture, p.BaseBranch, p.ID); err != nil {
		return err
	}
	if cur.Capture != p.Capture {
		if _, err := tx.Exec(`UPDATE projects SET capture=? WHERE parent_id=?`, p.Capture, p.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetProjectBase grava o branch base do projeto ("" volta a detectar pelo git).
func (s *Store) SetProjectBase(id int64, base string) error {
	res, err := s.DB.Exec(`UPDATE projects SET base_branch=? WHERE id=?`, base, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// DeleteProject remove o projeto; recusa um projeto que ainda tem sub-projetos.
func (s *Store) DeleteProject(id int64) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM projects WHERE parent_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return familyErr("o projeto tem sub-projetos; desligue-os antes de excluir")
	}
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
		if p.Path == "" { // grupo sem pasta
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

// ProjectForLocation é a regra de dono de uma sessão (a do hook, e a de quem
// adota sessões sem projeto): o projeto mais fundo que contém cwd, de qualquer
// família. Num worktree ligado (l.WorktreeLinked, com l.Main e l.Worktree), cwd
// é traduzido para o mesmo lugar no checkout principal: sem projeto em cwd
// vale o de lá; se o projeto achado é um grupo ou tem sub-projetos, o de lá
// vence quando é um projeto que não é grupo.
func (s *Store) ProjectForLocation(cwd string, l SessionLocation) (*domain.Project, error) {
	proj, err := s.ProjectForPath(cwd)
	if err != nil || cwd == "" {
		return proj, err
	}
	var main, rel string
	linked := l.WorktreeLinked && l.Main != ""
	if linked {
		r, err := filepath.Rel(l.Worktree, cwd)
		linked = err == nil && !strings.HasPrefix(r, "..")
		main, rel = l.Main, r
	}
	if proj == nil {
		if !linked {
			return nil, nil
		}
		return s.ProjectForPath(filepath.Join(main, rel))
	}
	if !linked {
		return proj, nil
	}
	if !proj.IsGroup() {
		kids, err := s.Children(proj.ID)
		if err != nil || len(kids) == 0 {
			return proj, err
		}
	}
	if app, err := s.ProjectForPath(filepath.Join(main, rel)); err == nil && app != nil && !app.IsGroup() {
		return app, nil
	}
	return proj, nil
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
	kids, err := s.Children(projectID)
	if err != nil {
		return 0, err
	}
	parent, err := getProjectTx(s.DB, projectID)
	if err != nil {
		return 0, err
	}
	type target struct {
		rel string
		id  int64
	}
	var apps []target
	for _, k := range kids {
		if r, ok := relUnder(parent.Path, k.Path); ok {
			apps = append(apps, target{r, k.ID})
		}
	}
	base := strings.TrimRight(worktree, "/")
	rows, err := s.DB.Query(`SELECT id, cwd FROM sessions
		WHERE project_id IS NULL AND worktree_linked=1 AND worktree=?`, worktree)
	if err != nil {
		return 0, fmt.Errorf("religar sessões do worktree: %w", err)
	}
	owner := map[string]int64{}
	for rows.Next() {
		var id, cwd string
		if err := rows.Scan(&id, &cwd); err != nil {
			rows.Close()
			return 0, err
		}
		pid, best := projectID, -1
		if r, ok := relUnder(base, cwd); ok {
			for _, a := range apps {
				if (r == a.rel || strings.HasPrefix(r, a.rel+"/")) && len(a.rel) > best {
					pid, best = a.id, len(a.rel)
				}
			}
		}
		owner[id] = pid
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for id, pid := range owner {
		if _, err := tx.Exec(`UPDATE sessions SET project_id=?, full_capture=1 WHERE id=?`, pid, id); err != nil {
			return 0, fmt.Errorf("religar sessões do worktree: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(owner), nil
}

// relUnder devolve path relativo a base, se path está dentro de base.
func relUnder(base, path string) (string, bool) {
	base, path = strings.TrimRight(base, "/"), strings.TrimRight(path, "/")
	if base == "" || !strings.HasPrefix(path, base+"/") {
		return "", false
	}
	return strings.TrimPrefix(path, base+"/"), true
}
