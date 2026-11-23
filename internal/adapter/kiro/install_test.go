package kiro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

type tenv struct {
	a      *Adapter
	home   string
	hooks  string
	steer  string
	nobre  string
	kiro   string
	events []string
}

// newInstEnv monta um HOME temporário com um nobre e um kiro-cli falsos.
func newInstEnv(t *testing.T) *tenv {
	t.Helper()
	home := t.TempDir()
	e := &tenv{home: home,
		hooks: filepath.Join(home, ".kiro", "hooks", "nobreai-cockpit.json"),
		steer: filepath.Join(home, ".kiro", "steering", "nobreai-cockpit.md"),
		nobre: filepath.Join(home, "bin", "nobre"),
		kiro:  filepath.Join(home, "bin", "kiro-cli"),
	}
	for _, f := range []string{e.nobre, e.kiro} {
		os.MkdirAll(filepath.Dir(f), 0o755)
		if err := os.WriteFile(f, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.a = New(nil, adapter.Deps{})
	e.a.SetPaths(Paths{HooksFile: e.hooks, SteeringFile: e.steer, NobreBin: e.nobre, KiroBin: e.kiro,
		Version: func(string) string { return "kiro-cli 3.4.0" }})
	return e
}

func (e *tenv) read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func check(t *testing.T, cs []adapter.Check, key string) adapter.Check {
	t.Helper()
	for _, c := range cs {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("sem check %q em %+v", key, cs)
	return adapter.Check{}
}

func TestInstallWritesBothFiles(t *testing.T) {
	e := newInstEnv(t)
	rep, err := e.a.Install()
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Changed) != 2 {
		t.Fatalf("Changed = %v", rep.Changed)
	}
	var f struct {
		Version string `json:"version"`
		Hooks   []struct {
			Name    string `json:"name"`
			Trigger string `json:"trigger"`
			Matcher string `json:"matcher"`
			Action  struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"action"`
			Timeout int `json:"timeout"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(e.read(t, e.hooks)), &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != "v1" || len(f.Hooks) != 7 {
		t.Fatalf("arquivo = %+v", f)
	}
	for i, ev := range hookEvents {
		h := f.Hooks[i]
		if h.Trigger != ev || h.Name != "nobreai-cockpit-"+ev || h.Matcher != "" || h.Timeout != 10 ||
			h.Action.Type != "command" || h.Action.Command != e.nobre+" hook kiro "+ev {
			t.Fatalf("hook %d = %+v", i, h)
		}
	}
	if got := e.read(t, e.steer); got != steeringContent || !strings.HasPrefix(got, "---\ninclusion: always\n---\n") {
		t.Fatalf("steering = %q", got[:60])
	}
}

func TestInstallIdempotent(t *testing.T) {
	e := newInstEnv(t)
	if _, err := e.a.Install(); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(e.hooks, old, old)
	os.Chtimes(e.steer, old, old)
	plan, err := e.a.PlanInstall()
	if err != nil || len(plan.Changed) != 0 {
		t.Fatalf("plano = %+v, %v", plan, err)
	}
	rep, err := e.a.Install()
	if err != nil || len(rep.Changed) != 0 {
		t.Fatalf("reinstalar = %+v, %v", rep, err)
	}
	for _, p := range []string{e.hooks, e.steer} {
		if st, _ := os.Stat(p); st.ModTime().Sub(old) > time.Second {
			t.Fatalf("%s foi reescrito", p)
		}
	}
}

func TestPlanInstallDoesNotWrite(t *testing.T) {
	e := newInstEnv(t)
	plan, err := e.a.PlanInstall()
	if err != nil || len(plan.Changed) != 2 {
		t.Fatalf("plano = %+v, %v", plan, err)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".kiro")); !os.IsNotExist(err) {
		t.Fatal("o plano criou ~/.kiro")
	}
}

func TestInstallQuotesNobrePathWithSpaces(t *testing.T) {
	e := newInstEnv(t)
	p := filepath.Join(e.home, "my bin", "nobre")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("x"), 0o755)
	e.a.SetPaths(Paths{HooksFile: e.hooks, SteeringFile: e.steer, NobreBin: p})
	if _, err := e.a.Install(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.read(t, e.hooks), "'"+p+"' hook kiro SessionStart") {
		t.Fatalf("sem aspas: %s", e.read(t, e.hooks))
	}
	if c := check(t, e.a.Health(), "hooks"); !c.OK {
		t.Fatalf("hooks = %+v", c)
	}
}

func TestInstallWithoutNobreFails(t *testing.T) {
	e := newInstEnv(t)
	e.a.SetPaths(Paths{HooksFile: e.hooks, SteeringFile: e.steer, NobreBin: filepath.Join(e.home, "sumiu"),
		SearchPATH: func() string { return "" }})
	if _, err := e.a.Install(); err == nil {
		t.Fatal("esperava erro sem nobre")
	}
	if _, err := os.Stat(e.hooks); !os.IsNotExist(err) {
		t.Fatal("gravou com nobre morto")
	}
}

func TestUninstallRemovesOnlyOurFiles(t *testing.T) {
	e := newInstEnv(t)
	e.a.Install()
	mine := filepath.Join(e.home, ".kiro", "hooks", "meu.json")
	other := filepath.Join(e.home, ".kiro", "steering", "meu.md")
	os.WriteFile(mine, []byte("{}"), 0o644)
	os.WriteFile(other, []byte("x"), 0o644)
	rep, err := e.a.Uninstall(true)
	if err != nil || len(rep.Changed) != 2 {
		t.Fatalf("dry = %+v, %v", rep, err)
	}
	if _, err := os.Stat(e.hooks); err != nil {
		t.Fatal("dry-run apagou")
	}
	if rep, err = e.a.Uninstall(false); err != nil || len(rep.Changed) != 2 {
		t.Fatalf("remover = %+v, %v", rep, err)
	}
	for _, p := range []string{e.hooks, e.steer} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s continua", p)
		}
	}
	for _, p := range []string{mine, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("apagou %s", p)
		}
	}
	rep, err = e.a.Uninstall(false)
	if err != nil || len(rep.Changed) != 0 {
		t.Fatalf("segunda remoção = %+v, %v", rep, err)
	}
}

func TestHealthEachMissing(t *testing.T) {
	e := newInstEnv(t)
	cs := e.a.Health()
	if c := check(t, cs, "hooks"); c.OK || c.Code != "missing" {
		t.Fatalf("hooks = %+v", c)
	}
	if c := check(t, cs, "steering"); c.OK || c.Code != "missing" {
		t.Fatalf("steering = %+v", c)
	}
	if c := check(t, cs, "binary"); !c.OK {
		t.Fatalf("binary = %+v", c)
	}
	e.a.Install()
	for _, k := range []string{"binary", "hooks", "steering"} {
		if c := check(t, e.a.Health(), k); !c.OK {
			t.Fatalf("%s = %+v", k, c)
		}
	}
	// Detalhe do hooks não afirma que o disparo foi verificado.
	if d := check(t, e.a.Health(), "hooks").Detail; strings.Contains(d, "verificad") {
		t.Fatalf("detail = %q", d)
	}
}

func TestHealthHooksProblems(t *testing.T) {
	e := newInstEnv(t)
	e.a.Install()
	orig := e.read(t, e.hooks)
	cases := []struct{ name, content, code string }{
		{"quebrado", "{nao json", "unreadable"},
		{"faltando", strings.Replace(orig, "SessionEnd", "Outro", -1), "missing"},
		{"nobre antigo", strings.Replace(orig, e.nobre, e.home+"/velho/nobre", -1), "outdated"},
		{"entrada extra", strings.Replace(orig, `"hooks": [`, `"hooks": [{"name":"x","trigger":"Stop","action":{"type":"command","command":"/x/y"},"timeout":1},`, 1), "outdated"},
	}
	for _, c := range cases {
		os.WriteFile(e.hooks, []byte(c.content), 0o644)
		got := check(t, e.a.Health(), "hooks")
		if got.OK || got.Code != c.code {
			t.Errorf("%s: %+v, quer %s", c.name, got, c.code)
		}
	}
	os.WriteFile(e.hooks, []byte(orig), 0o644)
	os.Remove(e.nobre)
	e.a.SetPaths(Paths{HooksFile: e.hooks, SteeringFile: e.steer, NobreBin: e.nobre, SearchPATH: func() string { return "" }})
	if got := check(t, e.a.Health(), "hooks"); got.OK || got.Code != "dead_binary" {
		t.Errorf("nobre sumiu: %+v", got)
	}
}

func TestHealthSteeringOutdated(t *testing.T) {
	e := newInstEnv(t)
	e.a.Install()
	os.WriteFile(e.steer, []byte("velho"), 0o644)
	if c := check(t, e.a.Health(), "steering"); c.OK || c.Code != "outdated" {
		t.Fatalf("steering = %+v", c)
	}
}

func TestDetectBinaryVersion(t *testing.T) {
	e := newInstEnv(t)
	d := e.a.Detect()
	if !d.Found || d.Path != e.kiro || d.Version != "kiro-cli 3.4.0" {
		t.Fatalf("detect = %+v", d)
	}
	if c := check(t, d.Checks, "binary"); !c.OK || !strings.Contains(c.Detail, "3.4.0") {
		t.Fatalf("binary = %+v", c)
	}
	if c := check(t, d.Checks, "last_event"); c.OK {
		t.Fatalf("last_event = %+v", c)
	}
	e.a.paths.Version = func(string) string { return "kiro-cli 3.0.9" }
	if c := check(t, e.a.Detect().Checks, "binary"); c.OK || c.Code != "outdated" {
		t.Fatalf("versão velha: %+v", c)
	}
	e.a.paths.Version = func(string) string { return "kiro-cli 3.1.0" }
	if c := check(t, e.a.Detect().Checks, "binary"); !c.OK {
		t.Fatalf("3.1.0: %+v", c)
	}
	e.a.paths.KiroBin = filepath.Join(e.home, "nao-existe")
	d = e.a.Detect()
	if d.Found {
		t.Fatal("achou kiro inexistente")
	}
	if c := check(t, d.Checks, "binary"); c.OK || c.Code != "not_found" {
		t.Fatalf("binary = %+v", c)
	}
}

func TestBinaryPathThenLocalBin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := New(nil, adapter.Deps{})
	a.SetPaths(Paths{SearchPATH: func() string { return "" }})
	if a.Binary() != "" {
		t.Fatal("achou do nada")
	}
	lb := filepath.Join(home, ".local", "bin", "kiro-cli")
	os.MkdirAll(filepath.Dir(lb), 0o755)
	os.WriteFile(lb, []byte("x"), 0o755)
	if a.Binary() != lb {
		t.Fatalf("Binary = %q", a.Binary())
	}
	dir := t.TempDir()
	pb := filepath.Join(dir, "kiro-cli")
	os.WriteFile(pb, []byte("x"), 0o755)
	a.SetPaths(Paths{SearchPATH: func() string { return dir }})
	if a.Binary() != pb {
		t.Fatalf("Binary = %q", a.Binary())
	}
}

func TestLaunchCommand(t *testing.T) {
	got := New(nil, adapter.Deps{}).LaunchCommand("/b/kiro-cli", "faça X")
	want := []string{"/b/kiro-cli", "chat", "--v3", "faça X"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got %q", got)
	}
}

func TestWatchedFiles(t *testing.T) {
	e := newInstEnv(t)
	got := e.a.WatchedFiles()
	if len(got) != 3 || got[0] != e.hooks || got[1] != e.steer || got[2] != e.nobre {
		t.Fatalf("watched = %v", got)
	}
}

func TestSteeringContent(t *testing.T) {
	for _, want := range []string{
		"NobreAI Cockpit: esta sessão é registrada no board", "nobre start", "nobre note", "nobre done", "nobre resume",
	} {
		if !strings.Contains(steeringContent, want) {
			t.Errorf("steering sem %q", want)
		}
	}
	for _, bad := range []string{"name: nobre-cockpit", ".claude/", "Claude"} {
		if strings.Contains(steeringContent, bad) {
			t.Errorf("steering com %q", bad)
		}
	}
}
