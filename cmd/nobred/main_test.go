package main

import (
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func TestResolveDBPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	if _, err := resolveDBPath(store.DefaultPath(), true); err == nil {
		t.Fatal("banco real com --seed-demo deveria falhar")
	}
	if _, err := resolveDBPath("/xdg/nobreai-cockpit/../nobreai-cockpit/cockpit.db", true); err == nil {
		t.Fatal("caminho equivalente ao real deveria falhar")
	}
	if p, err := resolveDBPath("", true); err != nil || p != store.DefaultDemoPath() {
		t.Fatalf("p=%q err=%v", p, err)
	}
	if p, err := resolveDBPath("/tmp/outro.db", true); err != nil || p != "/tmp/outro.db" {
		t.Fatalf("p=%q err=%v", p, err)
	}
	if p, err := resolveDBPath(store.DefaultPath(), false); err != nil || p != store.DefaultPath() {
		t.Fatalf("sem --seed-demo o banco real é permitido: %q %v", p, err)
	}
	if p, _ := resolveDBPath("", false); p != store.DefaultPath() {
		t.Fatalf("p=%q", p)
	}
}
