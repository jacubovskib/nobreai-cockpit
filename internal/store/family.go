package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// ErrFamily é a base dos erros das regras de sub-projeto; vem envolvida com a
// mensagem específica.
var ErrFamily = errors.New("regra de sub-projeto")

func familyErr(msg string) error { return fmt.Errorf("%w: %s", ErrFamily, msg) }

// Moved diz quanto histórico mudou de projeto ao ligar um sub-projeto.
type Moved struct {
	Sessions int `json:"sessions"`
	Cards    int `json:"cards"`
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

func getProjectTx(q querier, id int64) (domain.Project, error) {
	var p domain.Project
	err := q.QueryRow(`SELECT id, name, COALESCE(path, ''), capture, base_branch, parent_id, kind FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Path, &p.Capture, &p.BaseBranch, &p.ParentID, &p.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func childrenTx(q querier, id int64) ([]domain.Project, error) {
	rows, err := q.Query(`SELECT id, name, COALESCE(path, ''), capture, base_branch, parent_id, kind FROM projects WHERE parent_id=? ORDER BY id`, id)
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

// inside diz se path fica estritamente abaixo de root (limite de diretório).
func inside(root, path string) bool {
	root = strings.TrimRight(root, "/")
	return strings.HasPrefix(path, root+"/")
}

// checkInside vale para pai com pasta (monorepo ou grupo com pasta); grupo sem
// pasta aceita qualquer pasta.
func checkInside(parent domain.Project, path string) error {
	if parent.Path != "" && !inside(parent.Path, path) {
		return familyErr("a pasta do sub-projeto precisa ficar dentro da pasta do projeto principal")
	}
	return nil
}

// checkNameFree recusa name se já existe na família de p (o pai, os irmãos e
// os filhos de p), ignorando o próprio p.
func checkNameFree(q querier, p domain.Project, name string) error {
	root := p.ID
	if p.ParentID != nil {
		root = *p.ParentID
	}
	var taken bool
	err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM projects
		WHERE (id=? OR parent_id=?) AND id<>? AND lower(name)=lower(?))`, root, root, p.ID, name).Scan(&taken)
	if err != nil {
		return err
	}
	if taken {
		return familyErr("já existe um projeto com esse nome nesta família")
	}
	return nil
}

// CreateGroup cria um grupo de repositórios sem pasta.
func (s *Store) CreateGroup(name string) (domain.Project, error) {
	return s.insertProject(name, "", "group")
}

// CreateGroupAt cria um grupo com pasta. A pasta é absoluta, não é a home nem
// ancestral dela, e não fica dentro de um projeto cadastrado.
func (s *Store) CreateGroupAt(name, path string) (domain.Project, error) {
	path, err := checkGroupFolder(s.DB, path, 0)
	if err != nil {
		return domain.Project{}, err
	}
	return s.insertProject(name, path, "group")
}

// resolveExisting resolve symlinks de path; se o fim ainda não existe, resolve
// o ancestral mais fundo que existe e reanexa o resto.
func resolveExisting(path string) (string, error) {
	rest := ""
	for p := path; ; p = filepath.Dir(p) {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest), nil
		}
		if p == "/" || p == "." {
			return "", fmt.Errorf("resolver %s", path)
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

// checkGroupFolder valida a pasta de um grupo e devolve o caminho limpo: absoluta,
// não é a home nem ancestral dela, e não fica dentro (nem é a pasta) de outro
// projeto cadastrado. O grupo selfID e os filhos dele não contam.
func checkGroupFolder(q querier, path string, selfID int64) (string, error) {
	if !filepath.IsAbs(path) {
		return "", familyErr("a pasta do grupo precisa ser um caminho absoluto")
	}
	path = filepath.Clean(path)
	home, err := os.UserHomeDir()
	if err != nil {
		return "", familyErr("não foi possível resolver a home do usuário")
	}
	home, err = resolveExisting(filepath.Clean(home))
	if err != nil {
		return "", familyErr("não foi possível resolver a home do usuário")
	}
	real, err := resolveExisting(path)
	if err != nil {
		return "", familyErr("não foi possível resolver a pasta do grupo")
	}
	if real == home || real == "/" || inside(real, home) {
		return "", familyErr("a pasta do grupo não pode ser a home nem uma pasta que a contém")
	}
	rows, err := q.Query(`SELECT name, path FROM projects
		WHERE path IS NOT NULL AND id<>? AND parent_id IS NOT ?`, selfID, selfID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var n, p string
		if err := rows.Scan(&n, &p); err != nil {
			return "", err
		}
		if path == p || inside(p, path) {
			return "", familyErr("a pasta do grupo fica dentro do projeto " + n)
		}
	}
	return path, rows.Err()
}

// checkAttach valida ligar o projeto child (path e nome já definidos) ao pai.
func checkAttach(q querier, child, parent domain.Project) error {
	if parent.ParentID != nil {
		return familyErr("o projeto principal não pode ser sub-projeto de outro")
	}
	if child.ID != 0 {
		kids, err := childrenTx(q, child.ID)
		if err != nil {
			return err
		}
		if len(kids) > 0 {
			return familyErr("um projeto com sub-projetos não pode virar sub-projeto")
		}
	}
	if child.IsGroup() {
		return familyErr("um grupo não pode ser sub-projeto")
	}
	if child.Path == "" {
		return familyErr("o sub-projeto precisa de uma pasta")
	}
	if err := checkInside(parent, child.Path); err != nil {
		return err
	}
	probe := child
	probe.ParentID = &parent.ID
	return checkNameFree(q, probe, child.Name)
}

// CreateChild cria um sub-projeto de parentID, adota a captura do pai e traz
// do pai o histórico que pertence à pasta do filho.
func (s *Store) CreateChild(name, path string, parentID int64) (domain.Project, Moved, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return domain.Project{}, Moved{}, err
	}
	defer tx.Rollback()
	parent, err := getProjectTx(tx, parentID)
	if err != nil {
		return domain.Project{}, Moved{}, err
	}
	child := domain.Project{Name: name, Path: path, Capture: parent.Capture, ParentID: &parentID}
	if err := checkAttach(tx, child, parent); err != nil {
		return domain.Project{}, Moved{}, err
	}
	res, err := tx.Exec(`INSERT INTO projects(name, path, capture, parent_id) VALUES (?, ?, ?, ?)`,
		name, path, parent.Capture, parentID)
	if err != nil {
		return domain.Project{}, Moved{}, fmt.Errorf("criar projeto: %w", err)
	}
	if child.ID, err = res.LastInsertId(); err != nil {
		return domain.Project{}, Moved{}, err
	}
	m, err := moveHistory(tx, parent, child)
	if err != nil {
		return domain.Project{}, Moved{}, err
	}
	return child, m, tx.Commit()
}

// SetParent liga o projeto id ao pai parentID (ou desliga, com nil). Ligar
// adota a captura do pai e traz o histórico da pasta; desligar não devolve
// nada ao pai e mantém a captura do filho.
func (s *Store) SetParent(id int64, parentID *int64) (Moved, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return Moved{}, err
	}
	defer tx.Rollback()
	child, err := getProjectTx(tx, id)
	if err != nil {
		return Moved{}, err
	}
	if parentID == nil {
		if _, err := tx.Exec(`UPDATE projects SET parent_id=NULL WHERE id=?`, id); err != nil {
			return Moved{}, err
		}
		return Moved{}, tx.Commit()
	}
	if *parentID == id {
		return Moved{}, familyErr("um projeto não pode ser sub-projeto de si mesmo")
	}
	parent, err := getProjectTx(tx, *parentID)
	if err != nil {
		return Moved{}, err
	}
	if err := checkAttach(tx, child, parent); err != nil {
		return Moved{}, err
	}
	if _, err := tx.Exec(`UPDATE projects SET parent_id=?, capture=? WHERE id=?`, parent.ID, parent.Capture, id); err != nil {
		return Moved{}, err
	}
	m, err := moveHistory(tx, parent, child)
	if err != nil {
		return Moved{}, err
	}
	return m, tx.Commit()
}

// moveHistory passa do pai para o filho as sessões da pasta do filho (pelo cwd
// ou, em worktree ligado, pelo caminho relativo à raiz do worktree) e os cards
// do pai cujas sessões todas passaram. Cards sem sessões ficam.
func moveHistory(tx querier, parent, child domain.Project) (Moved, error) {
	var m Moved
	if parent.Path == "" {
		return m, nil // grupo sem pasta: não tem de onde puxar histórico
	}
	rel := strings.TrimPrefix(strings.TrimRight(child.Path, "/"), strings.TrimRight(parent.Path, "/")+"/")
	root := strings.TrimRight(child.Path, "/")

	rows, err := tx.Query(`SELECT id, cwd, worktree, worktree_linked FROM sessions WHERE project_id=?`, parent.ID)
	if err != nil {
		return m, err
	}
	var move []string
	for rows.Next() {
		var id, cwd, wt string
		var linked bool
		if err := rows.Scan(&id, &cwd, &wt, &linked); err != nil {
			rows.Close()
			return m, err
		}
		ok := cwd == root || strings.HasPrefix(cwd, root+"/")
		if !ok && linked && wt != "" {
			base := strings.TrimRight(wt, "/")
			if strings.HasPrefix(cwd, base+"/") {
				r := strings.TrimPrefix(cwd, base+"/")
				ok = r == rel || strings.HasPrefix(r, rel+"/")
			}
		}
		if ok {
			move = append(move, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return m, err
	}
	rows.Close()
	for _, id := range move {
		if _, err := tx.Exec(`UPDATE sessions SET project_id=? WHERE id=?`, child.ID, id); err != nil {
			return m, err
		}
	}
	m.Sessions = len(move)

	// Cards do pai com sessões e nenhuma delas ainda no pai.
	res, err := tx.Exec(`UPDATE cards SET project_id=? WHERE project_id=?
		AND EXISTS (SELECT 1 FROM sessions WHERE card_id=cards.id)
		AND NOT EXISTS (SELECT 1 FROM sessions WHERE card_id=cards.id AND project_id IS NOT ?)`,
		child.ID, parent.ID, child.ID)
	if err != nil {
		return m, err
	}
	n, err := res.RowsAffected()
	m.Cards = int(n)
	return m, err
}

// FamilyIDs: para um pai, ele e os filhos; para um filho ou projeto solto, só ele.
func (s *Store) FamilyIDs(id int64) ([]int64, error) {
	rows, err := s.DB.Query(`SELECT id FROM projects WHERE id=? OR parent_id=? ORDER BY (id<>?), id`, id, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Children lista os sub-projetos de id.
func (s *Store) Children(id int64) ([]domain.Project, error) { return childrenTx(s.DB, id) }

// RepoProject devolve o pai quando p é app de monorepo (pai repositório); senão
// p, inclusive para filho de grupo, com ou sem pasta.
func (s *Store) RepoProject(p domain.Project) (domain.Project, error) {
	if p.ParentID == nil {
		return p, nil
	}
	parent, err := getProjectTx(s.DB, *p.ParentID)
	if err != nil {
		return p, err
	}
	if parent.IsGroup() {
		return p, nil
	}
	return parent, nil
}

// SetCardProject move o card para outro projeto da mesma família.
func (s *Store) SetCardProject(cardID, projectID int64) error {
	var cur int64
	if err := s.DB.QueryRow(`SELECT project_id FROM cards WHERE id=?`, cardID).Scan(&cur); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	from, err := getProjectTx(s.DB, cur)
	if err != nil {
		return err
	}
	to, err := getProjectTx(s.DB, projectID)
	if err != nil {
		return err
	}
	rootOf := func(p domain.Project) int64 {
		if p.ParentID != nil {
			return *p.ParentID
		}
		return p.ID
	}
	if rootOf(from) != rootOf(to) {
		return familyErr("o card só pode mudar para um projeto da mesma família")
	}
	_, err = s.DB.Exec(`UPDATE cards SET project_id=? WHERE id=?`, projectID, cardID)
	return err
}

// DisplayName é o nome exibido: "pai › filho" para um sub-projeto.
func DisplayName(p domain.Project, parent *domain.Project) string {
	if parent == nil {
		return p.Name
	}
	return parent.Name + " › " + p.Name
}

// ChildSpec é um sub-projeto a cadastrar ou ligar em CreateFamily.
type ChildSpec struct {
	Name string
	Path string
}

func projectAtTx(q querier, path string) (*domain.Project, error) {
	var id int64
	err := q.QueryRow(`SELECT id FROM projects WHERE path=?`, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p, err := getProjectTx(q, id)
	return &p, err
}

func insertProjectTx(q querier, name, path, kind string, capture bool, parentID *int64) (domain.Project, error) {
	res, err := q.Exec(`INSERT INTO projects(name, path, kind, capture, parent_id) VALUES (?, NULLIF(?, ''), ?, ?, ?)`,
		name, path, kind, capture, parentID)
	if err != nil {
		return domain.Project{}, fmt.Errorf("criar projeto: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Project{}, err
	}
	return getProjectTx(q, id)
}

// CreateFamily cadastra, numa transação só, o projeto principal (parent.ID 0
// cria: grupo com pasta se parent.Kind é "group", repositório senão) e liga
// os filhos: um projeto solto já cadastrado na pasta é ligado, os demais são
// criados. Qualquer item inválido desfaz tudo. Os filhos voltam na ordem dada.
func (s *Store) CreateFamily(parent domain.Project, children []ChildSpec) (domain.Project, []domain.Project, Moved, error) {
	var total Moved
	tx, err := s.DB.Begin()
	if err != nil {
		return domain.Project{}, nil, total, err
	}
	defer tx.Rollback()
	if parent.ID != 0 {
		if parent, err = getProjectTx(tx, parent.ID); err != nil {
			return domain.Project{}, nil, total, err
		}
	} else if parent.IsGroup() {
		path, err := checkGroupFolder(tx, parent.Path, 0)
		if err != nil {
			return domain.Project{}, nil, total, err
		}
		if parent, err = insertProjectTx(tx, parent.Name, path, "group", true, nil); err != nil {
			return domain.Project{}, nil, total, err
		}
	} else if parent, err = insertProjectTx(tx, parent.Name, parent.Path, "repo", true, nil); err != nil {
		return domain.Project{}, nil, total, err
	}
	kids := make([]domain.Project, 0, len(children))
	for _, c := range children {
		cur, err := projectAtTx(tx, c.Path)
		if err != nil {
			return domain.Project{}, nil, total, err
		}
		switch {
		case cur == nil:
			child := domain.Project{Name: c.Name, Path: c.Path, Capture: parent.Capture, ParentID: &parent.ID}
			if err := checkAttach(tx, child, parent); err != nil {
				return domain.Project{}, nil, total, err
			}
			if child, err = insertProjectTx(tx, c.Name, c.Path, "repo", parent.Capture, &parent.ID); err != nil {
				return domain.Project{}, nil, total, err
			}
			m, err := moveHistory(tx, parent, child)
			if err != nil {
				return domain.Project{}, nil, total, err
			}
			total.Sessions += m.Sessions
			total.Cards += m.Cards
			kids = append(kids, child)
		case cur.ParentID != nil && *cur.ParentID == parent.ID:
			kids = append(kids, *cur) // já é filho deste principal
		case cur.ParentID != nil:
			owner, err := getProjectTx(tx, *cur.ParentID)
			if err != nil {
				return domain.Project{}, nil, total, err
			}
			return domain.Project{}, nil, total, familyErr(cur.Name + " pertence a " + owner.Name)
		default:
			sub, err := childrenTx(tx, cur.ID)
			if err != nil {
				return domain.Project{}, nil, total, err
			}
			if len(sub) > 0 {
				return domain.Project{}, nil, total, familyErr(cur.Name + " tem sub-projetos")
			}
			if err := checkAttach(tx, *cur, parent); err != nil {
				return domain.Project{}, nil, total, err
			}
			if _, err := tx.Exec(`UPDATE projects SET parent_id=?, capture=? WHERE id=?`, parent.ID, parent.Capture, cur.ID); err != nil {
				return domain.Project{}, nil, total, err
			}
			m, err := moveHistory(tx, parent, *cur)
			if err != nil {
				return domain.Project{}, nil, total, err
			}
			total.Sessions += m.Sessions
			total.Cards += m.Cards
			cur.ParentID, cur.Capture = &parent.ID, parent.Capture
			kids = append(kids, *cur)
		}
	}
	return parent, kids, total, tx.Commit()
}

// UnassignedSessionsUnder lista as sessões sem projeto (vivas e encerradas)
// cujo cwd é root ou fica dentro dele.
func (s *Store) UnassignedSessionsUnder(root string) ([]domain.Session, error) {
	root = strings.TrimRight(root, "/")
	prefix := root + "/"
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions
		WHERE project_id IS NULL AND (cwd=? OR substr(cwd, 1, ?)=?) ORDER BY julianday(started_at), id`,
		root, utf8.RuneCountInString(prefix), prefix)
}

// UnassignedLinkedSessions lista as sessões sem projeto (vivas e encerradas)
// que rodam num worktree ligado, na ordem em que começaram.
func (s *Store) UnassignedLinkedSessions() ([]domain.Session, error) {
	return querySessions(s.DB, `SELECT `+sessionCols+` FROM sessions
		WHERE project_id IS NULL AND worktree_linked=1 AND worktree<>'' ORDER BY julianday(started_at), id`)
}
