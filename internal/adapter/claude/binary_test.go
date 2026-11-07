package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

func TestResolveCommandKeepsSymlinkAndFallsBack(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(home, "versions", "2.1.318")
	os.MkdirAll(filepath.Dir(real), 0o755)
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(local, "claude")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	notFound := func(string) (string, error) { return "", errors.New("não está no PATH") }

	// Fora do PATH: cai em ~/.local/bin/claude, sem seguir o symlink (o comm
	// do processo tem de continuar "claude").
	if got := resolveCommand("claude", notFound, home); got != link {
		t.Fatalf("fallback = %q, quero %q", got, link)
	}
	// No PATH: o caminho do PATH, absoluto e sem resolver symlink.
	inPath := func(string) (string, error) { return link, nil }
	if got := resolveCommand("claude", inPath, ""); got != link {
		t.Fatalf("PATH = %q", got)
	}
	// Relativo vira absoluto.
	rel := func(string) (string, error) { return "bin/claude", nil }
	if got := resolveCommand("claude", rel, ""); !filepath.IsAbs(got) || !strings.HasSuffix(got, "/bin/claude") {
		t.Fatalf("relativo = %q", got)
	}
	// Em nenhum lugar.
	if got := resolveCommand("claude", notFound, t.TempDir()); got != "" {
		t.Fatalf("ausente = %q", got)
	}
	if got := resolveCommand("claude", notFound, ""); got != "" {
		t.Fatalf("sem home = %q", got)
	}
}

func TestBinaryAndLaunchCommandUseResolvedPath(t *testing.T) {
	a := New(nil, adapter.Deps{})
	cli := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755)
	a.SetPaths(Paths{ClaudeBin: cli})
	if got := a.Binary(); got != cli {
		t.Fatalf("Binary = %q", got)
	}
	if got := a.LaunchCommand(cli, "oi"); len(got) != 3 || got[0] != cli || got[1] != "--" || got[2] != "oi" {
		t.Fatalf("LaunchCommand = %q", got)
	}
	a.SetPaths(Paths{ClaudeBin: filepath.Join(t.TempDir(), "ausente")})
	if got := a.Binary(); got != "" {
		t.Fatalf("ausente = %q", got)
	}
}

func TestDetectHooksPointingToMissingBinary(t *testing.T) {
	e := newEnv(t)
	p := writeSettings(t, "")
	skills := t.TempDir()
	gone := filepath.Join(t.TempDir(), "sumiu", "nobre")
	e.a.SetPaths(Paths{SettingsPath: p, SkillsDir: skills, NobreBin: gone, ClaudeBin: "/nao/existe"})
	if _, err := e.a.Install(); err != nil {
		t.Fatal(err)
	}
	var hooks adapter.Check
	for _, c := range e.a.Detect().Checks {
		if c.Key == "hooks" {
			hooks = c
		}
	}
	if hooks.OK || hooks.Detail != "aponta para um binário que não existe: "+gone {
		t.Fatalf("%+v", hooks)
	}
}

func TestSkillGatesOnRegisteredPhrase(t *testing.T) {
	if !strings.HasPrefix(instructions, registeredPhrase) {
		t.Fatalf("instructions não começa com %q", registeredPhrase)
	}
	if !strings.Contains(skillContent, registeredPhrase) {
		t.Fatalf("skill.md não cita a frase %q", registeredPhrase)
	}
	if !strings.Contains(skillContent, "não cadastre") {
		t.Fatal("skill.md deveria proibir cadastrar o repositório por conta própria")
	}
}
