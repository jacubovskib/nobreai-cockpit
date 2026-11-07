package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
