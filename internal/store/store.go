// Package store implementa a persistência SQLite do NobreAI Cockpit.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store encapsula a conexão com o banco.
type Store struct {
	DB *sql.DB
}

// DefaultPath retorna $XDG_DATA_HOME/nobreai-cockpit/cockpit.db
// (padrão ~/.local/share/nobreai-cockpit/cockpit.db).
func DefaultPath() string { return dataDir("cockpit.db") }

// DefaultDemoPath retorna $XDG_DATA_HOME/nobreai-cockpit/cockpit-demo.db: o
// banco de demonstração, que nunca se mistura com o real.
func DefaultDemoPath() string { return dataDir("cockpit-demo.db") }

func dataDir(file string) string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "nobreai-cockpit", file)
}

// Open cria o diretório, abre o banco e aplica as migrações pendentes.
func Open(path string) (*Store, error) {
	s, err := openUpTo(path, math.MaxInt)
	if err != nil {
		return nil, err
	}
	if err := s.seedPrompts(); err != nil {
		s.DB.Close()
		return nil, err
	}
	return s, nil
}

// openUpTo abre o banco e aplica as migrações até a versão limit (inclusive).
func openUpTo(path string, limit int) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("criar diretório do banco: %w", err)
	}
	// _txlock=immediate: toda transação pega o lock de escrita no BEGIN. Hooks
	// concorrentes não esbarram em SQLITE_BUSY ao promover leitura a escrita
	// (o que o busy_timeout não cobre).
	dsn := "file:" + path + "?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}
	s := &Store{DB: db}
	if err := s.migrateUpTo(limit); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close fecha a conexão.
func (s *Store) Close() error { return s.DB.Close() }

type migration struct {
	version int
	name    string
}

// noForeignKeysMarker na primeira linha pede que a migração rode sem FK
// (reconstrução de tabela referenciada por outras).
const noForeignKeysMarker = "-- nobre:foreign_keys=off"

func (s *Store) migrateUpTo(limit int) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	var ms []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(prefix)
		if !ok || err != nil {
			return fmt.Errorf("migração com nome inválido: %s", name)
		}
		ms = append(ms, migration{v, name})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })

	var current int
	if err := s.DB.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("ler user_version: %w", err)
	}
	for _, m := range ms {
		if m.version <= current || m.version > limit {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(body), noForeignKeysMarker) {
			if err := s.applyWithoutFK(m, string(body)); err != nil {
				return err
			}
			continue
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migração %s: %w", m.name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migração %s: user_version: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migração %s: %w", m.name, err)
		}
	}
	return nil
}

// applyWithoutFK aplica uma migração com foreign_keys=OFF. O PRAGMA só vale
// fora de transação, então tudo roda numa conexão dedicada: PRAGMA, BEGIN, o
// corpo, foreign_key_check e COMMIT. Se a checagem achar violações, aborta.
func (s *Store) applyWithoutFK(m migration, body string) (err error) {
	ctx := context.Background()
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return fmt.Errorf("migração %s: desligar FK: %w", m.name, err)
	}
	// Religa a FK em qualquer saída: a conexão volta ao pool.
	defer func() {
		if _, e := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); e != nil && err == nil {
			err = fmt.Errorf("migração %s: religar FK: %w", m.name, e)
		}
	}()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("migração %s: %w", m.name, err)
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	if _, err := conn.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("migração %s: %w", m.name, err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return fmt.Errorf("migração %s: user_version: %w", m.name, err)
	}
	rows, err := conn.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("migração %s: foreign_key_check: %w", m.name, err)
	}
	violations := rows.Next()
	rows.Close()
	if violations {
		return fmt.Errorf("migração %s: foreign_key_check encontrou violações", m.name)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("migração %s: %w", m.name, err)
	}
	committed = true
	return nil
}
