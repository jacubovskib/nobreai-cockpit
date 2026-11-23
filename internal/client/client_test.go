package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/agents"
)

func TestClientSendsAgentPID(t *testing.T) {
	t.Setenv("NOBRE_AGENT_PID", "77")
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(AgentPIDHeader)
		w.Write([]byte(`{"session":{},"card":null}`))
	}))
	defer srv.Close()
	c := New(strings.TrimPrefix(srv.URL, "http://"))
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "77" {
		t.Fatalf("header = %q", got)
	}
}

func TestClientAPIErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"sem card"}`))
	}))
	defer srv.Close()
	_, err := New(strings.TrimPrefix(srv.URL, "http://")).Status(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 409 || ae.Message != "sem card" {
		t.Fatalf("err = %v", err)
	}
}

func TestClientRenderPromptSendsUse(t *testing.T) {
	var path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b := make([]byte, 200)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
		w.Write([]byte(`{"text":"oi"}`))
	}))
	defer srv.Close()
	txt, err := New(strings.TrimPrefix(srv.URL, "http://")).RenderPrompt(context.Background(), "handoff", "NAC-1")
	if err != nil || txt != "oi" || path != "/api/prompts/handoff/render" || !strings.Contains(body, `"use":true`) || !strings.Contains(body, `"card":"NAC-1"`) {
		t.Fatalf("txt=%q err=%v path=%s body=%s", txt, err, path, body)
	}
}

func fakeProc(t *testing.T, root string, pid, ppid int, comm string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%d (%s) S %d 1 1 0 -1 0\n", pid, comm, ppid)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Os nomes vêm do registro: acrescentar um driver (aqui, o kiro-cli) basta
// para achar o ancestral pelo mesmo mecanismo.
func TestFindAgentPIDAchaAncestralDoRegistro(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, 100, 1, "kiro-cli")
	fakeProc(t, root, 150, 100, "claude")
	fakeProc(t, root, 200, 100, "bash")
	fakeProc(t, root, 300, 200, "nobre")
	if got := findAgentPID(root, 300, []string{"claude"}); got != 0 {
		t.Fatalf("sem o nome do kiro-cli, got %d", got)
	}
	if got := findAgentPID(root, 300, agents.Static()); got != 100 {
		t.Fatalf("kiro-cli: got %d, want 100", got)
	}
	if got := findAgentPID(root, 150, agents.Static()); got != 150 {
		t.Fatalf("claude: got %d, want 150", got)
	}
}
