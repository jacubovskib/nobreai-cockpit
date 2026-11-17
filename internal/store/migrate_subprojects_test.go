package store

import (
	"database/sql"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("contar %s: %v", table, err)
	}
	return n
}

func TestMigrationKeepsProjectsAndRefs(t *testing.T) {
	path := t.TempDir() + "/cockpit.db"
	s, err := openUpTo(path, 10)
	if err != nil {
		t.Fatalf("abrir v10: %v", err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`INSERT INTO projects(name, path) VALUES ('a','/a'), ('b','/b')`)
	for i, pid := range []int{1, 1, 2} {
		mustExec(`INSERT INTO cards(key, project_id, title, created_at, updated_at) VALUES (?,?,?,?,?)`,
			"NAC-"+string(rune('1'+i)), pid, "c", "2026-01-01", "2026-01-01")
	}
	for _, id := range []string{"s1", "s2"} {
		mustExec(`INSERT INTO sessions(id, agent, project_id, started_at, last_event_at) VALUES (?, 'claude', 1, 'x', 'x')`, id)
	}
	mustExec(`INSERT INTO prompts(name, slug, project_id) VALUES ('p','p',2)`)
	mustExec(`INSERT INTO usage(session_id, message_id, at) VALUES ('s1','m1','x'), ('s1','m2','x')`)

	if err := s.migrateUpTo(1 << 30); err != nil {
		t.Fatalf("migrar: %v", err)
	}
	defer s.Close()

	for table, want := range map[string]int{"projects": 2, "cards": 3, "sessions": 2, "prompts": 1, "usage": 2} {
		if got := count(t, s.DB, table); got != want {
			t.Errorf("%s: %d linhas, esperava %d", table, got, want)
		}
	}
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM cards WHERE project_id=1`).Scan(&n); err != nil || n != 2 {
		t.Errorf("cards do projeto 1 = %d, err = %v", n, err)
	}
	rows, err := s.DB.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Error("foreign_key_check devolveu linhas")
	}
	rows.Close()
	var fk int
	if err := s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, err = %v", fk, err)
	}
	// o índice de FK segue valendo: projeto com card não pode ser apagado
	if _, err := s.DB.Exec(`DELETE FROM projects WHERE id=1`); err == nil {
		t.Error("esperava erro de FK ao apagar projeto com cards")
	}
}

func TestGroupProjectHasNoPath(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"loja", "outro"} {
		if _, err := s.DB.Exec(`INSERT INTO projects(name, path) VALUES (?, NULL)`, name); err != nil {
			t.Fatalf("inserir grupo %s: %v", name, err)
		}
	}
	ps, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("projetos = %d", len(ps))
	}
	for _, p := range ps {
		if p.Path != "" || p.ParentID != nil {
			t.Errorf("grupo %+v deveria ter Path vazio e sem pai", p)
		}
	}
	got, err := s.ProjectForPath("/qualquer")
	if err != nil || got != nil {
		t.Errorf("ProjectForPath = %v, err = %v", got, err)
	}
	// parent_id é lido e gravado
	parent := ps[0].ID
	if _, err := s.DB.Exec(`INSERT INTO projects(name, path, parent_id) VALUES ('filho','/f',?)`, parent); err != nil {
		t.Fatal(err)
	}
	ps, _ = s.ListProjects()
	var child domain.Project = ps[len(ps)-1]
	if child.ParentID == nil || *child.ParentID != parent {
		t.Errorf("ParentID = %v", child.ParentID)
	}
}
