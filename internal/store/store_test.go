package store

import "testing"

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	path := t.TempDir() + "/x/cockpit.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM cards`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("cards count = %d, err = %v", n, err)
	}
	var fk int
	if err := s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, err = %v", fk, err)
	}
	var v int
	if err := s.DB.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 20 {
		t.Fatalf("user_version = %d, err = %v", v, err)
	}
}

func TestDefaultDemoPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	if got := DefaultDemoPath(); got != "/xdg/nobreai-cockpit/cockpit-demo.db" {
		t.Fatalf("DefaultDemoPath = %q", got)
	}
	if got := DefaultPath(); got != "/xdg/nobreai-cockpit/cockpit.db" {
		t.Fatalf("DefaultPath = %q", got)
	}
}
