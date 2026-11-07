package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProjectAddResolvesCwd(t *testing.T) {
	var body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(201)
		w.Write([]byte(`{"name":"api","path":"/w/api"}`))
	})
	if r := runCLI(t, addr, "", nil, "project", "add", "--name", "api"); r.code != 0 || !strings.Contains(r.stdout, "/w/api") {
		t.Fatalf("%+v", r)
	}
	if body != `{"name":"api","path":"/w/api"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestPromptUseRendersWithCard(t *testing.T) {
	var in map[string]any
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/prompts/handoff/render" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&in)
		w.Write([]byte(`{"text":"Retome NAC-2"}`))
	})
	r := runCLI(t, addr, "", nil, "prompt", "use", "handoff", "--card", "NAC-2")
	if r.code != 0 || r.stdout != "Retome NAC-2\n" || in["card"] != "NAC-2" || in["use"] != true {
		t.Fatalf("%+v in=%v", r, in)
	}
}

func TestPromptSaveReadsStdin(t *testing.T) {
	var in map[string]any
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&in)
		w.WriteHeader(201)
		w.Write([]byte(`{"slug":"revisao-rapida"}`))
	})
	r := runCLI(t, addr, "corpo {{card.key}}\n", nil, "prompt", "save", "--name", "Revisão rápida", "--tag", "review")
	if r.code != 0 || in["body"] != "corpo {{card.key}}" || in["tag"] != "review" || in["slug"] != "revis-o-r-pida" && in["slug"] != "revisao-rapida" {
		t.Fatalf("%+v in=%v", r, in)
	}
}

func TestUIUsesInjectedOpener(t *testing.T) {
	var opened string
	var out strings.Builder
	c := &cli{out: &out, err: io.Discard, getenv: func(string) string { return "" },
		opener: func(u string) error { opened = u; return nil }}
	if code := c.run([]string{"ui"}); code != 0 || opened != "http://127.0.0.1:7420" {
		t.Fatalf("code=%d opened=%q", code, opened)
	}
}
