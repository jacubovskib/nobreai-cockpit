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

func TestCheckAddr(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:7420", "127.0.0.2:9000", "localhost:7420"} {
		if err := checkAddr(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{":7420", "0.0.0.0:7420", "192.168.0.10:7420", "[::]:7420", "exemplo.com:7420", "127.0.0.1", ""} {
		if err := checkAddr(bad); err == nil {
			t.Errorf("%q deveria ser recusado", bad)
		}
	}
}
