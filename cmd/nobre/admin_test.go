package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

// fakeWT é um git que sempre acha o mesmo repositório principal.
type fakeWT struct{ main string }

func (g fakeWT) Locate(dir string) gitinfo.Location {
	return gitinfo.Location{Root: dir, Main: g.main, Linked: dir != g.main}
}
func (g fakeWT) Worktrees(string) ([]gitinfo.Worktree, error) { return nil, nil }

func TestCLIProjectAddFromLinkedWorktree(t *testing.T) {
	var body string
	addr := fake(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(201)
		w.Write([]byte(`{"name":"api","path":"/w/api","base_branch":"develop","reassigned":2}`))
	})
	var out, errb bytes.Buffer
	c := &cli{in: strings.NewReader(""), out: &out, err: &errb,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return ""
		},
		cwd: func() (string, error) { return "/w/api-feat", nil },
		git: fakeWT{main: "/w/api"}}
	if code := c.run([]string{"project", "add"}); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if body != `{"name":"","path":"/w/api"}` {
		t.Fatalf("body = %s", body)
	}
	if want := "Projeto api cadastrado: /w/api (base develop, 2 sessões religadas)\n"; out.String() != want {
		t.Fatalf("out = %q", out.String())
	}
}

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
