package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
)

// Paths são os caminhos que o adaptador lê e escreve. Tudo é injetável: os
// testes apontam para diretórios temporários e nunca para o ~/.claude real.
type Paths struct {
	SettingsPath string // ~/.claude/settings.json
	SkillsDir    string // ~/.claude/skills
	NobreBin     string // caminho absoluto do binário nobre, gravado nos hooks
	ClaudeBin    string // CLI do Claude Code; vazio = procurar no PATH
	// SearchPATH devolve o PATH onde procurar o CLI (o do shell de login do
	// usuário, launch.UserPATH); nil = o PATH do processo.
	SearchPATH func() string
	// Version devolve a versão do CLI; nil = não informar.
	Version func(bin string) string
}

// DefaultPaths usa o diretório do usuário que roda o processo. O nobre fica ao
// lado do executável atual ou no PATH.
func DefaultPaths() Paths {
	p := Paths{Version: cliVersion}
	if home, err := os.UserHomeDir(); err == nil {
		p.SettingsPath = filepath.Join(home, ".claude", "settings.json")
		p.SkillsDir = filepath.Join(home, ".claude", "skills")
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		sib := filepath.Join(filepath.Dir(exe), "nobre")
		if st, err := os.Stat(sib); err == nil && !st.IsDir() {
			p.NobreBin = resolveLinks(sib)
		}
	}
	if p.NobreBin == "" {
		if lp, err := exec.LookPath("nobre"); err == nil {
			if abs, err := filepath.Abs(lp); err == nil {
				p.NobreBin = resolveLinks(abs)
			}
		}
	}
	return p
}

func resolveLinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func cliVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SetPaths troca os caminhos do adaptador (testes e instalação customizada).
func (a *Adapter) SetPaths(p Paths) { a.paths = p }

// cliPath acha o CLI do Claude Code: Paths.ClaudeBin, se definido, vale
// sozinho; senão o PATH do usuário (SearchPATH, ou o do processo) e, por fim,
// ~/.local/bin/claude (onde o instalador do Claude Code o põe).
func (a *Adapter) cliPath() string {
	if a.paths.ClaudeBin != "" {
		if _, err := os.Stat(a.paths.ClaudeBin); err == nil {
			return a.paths.ClaudeBin
		}
		return ""
	}
	home, _ := os.UserHomeDir()
	lookPath := exec.LookPath
	if a.paths.SearchPATH != nil {
		path := a.paths.SearchPATH()
		lookPath = func(name string) (string, error) {
			if p := launch.LookPathIn(name, path); p != "" {
				return p, nil
			}
			return "", exec.ErrNotFound
		}
	}
	return resolveCommand("claude", lookPath, home)
}

// resolveCommand devolve o caminho absoluto de name no PATH (lookPath) ou em
// <home>/.local/bin, sem seguir symlinks: o comm do processo vem do nome do
// arquivo executado e tem de continuar sendo name. "" = não encontrado.
func resolveCommand(name string, lookPath func(string) (string, error), home string) string {
	if p, err := lookPath(name); err == nil && p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	if home == "" {
		return ""
	}
	p := filepath.Join(home, ".local", "bin", name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0 {
		return p
	}
	return ""
}

// binaryExists diz se o comando de um hook existe: caminho com "/" é
// conferido no disco; nome solto, no PATH.
func binaryExists(b string) bool {
	if !strings.Contains(b, "/") {
		_, err := exec.LookPath(b)
		return err == nil
	}
	st, err := os.Stat(b)
	return err == nil && !st.IsDir()
}

// Detect monta o checklist de saúde do Claude Code: CLI, hooks, skill e
// eventos recebidos.
func (a *Adapter) Detect() adapter.Detection {
	d := adapter.Detection{Checks: []adapter.Check{}}
	if a.paths.SettingsPath == "" && a.paths.SkillsDir == "" {
		d.Checks = append(d.Checks, adapter.Check{Key: "paths", Label: "Caminhos", Detail: msgNoPaths})
		return d
	}
	cli := a.cliPath()
	d.Found = cli != ""
	d.Path = cli
	cliCheck := adapter.Check{Key: "cli", Label: "CLI encontrado", OK: d.Found, Detail: "claude não está no PATH nem em ~/.local/bin"}
	if d.Found {
		if a.paths.Version != nil {
			d.Version = a.paths.Version(cli)
		}
		cliCheck.Detail = strings.TrimSpace(cli + " " + d.Version)
	}

	hooks := adapter.Check{Key: "hooks", Label: "Hooks instalados"}
	missing, bins, err := installedHooks(a.paths.SettingsPath)
	switch {
	case err != nil:
		hooks.Detail = err.Error()
	case len(missing) > 0:
		hooks.Detail = "faltam: " + strings.Join(missing, ", ")
	default:
		hooks.OK = true
		hooks.Detail = strings.Join(hookEvents, ", ")
		for _, b := range bins {
			if !binaryExists(b) {
				hooks.OK = false
				hooks.Detail = "aponta para um binário que não existe: " + b
				break
			}
		}
	}

	sline := adapter.Check{Key: statusLineCheckKey, Label: "Status line integrada"}
	if b, detail := statusLineState(a.paths.SettingsPath); b == "" {
		sline.Detail = detail
	} else if !binaryExists(b) {
		sline.Detail = "aponta para um binário que não existe: " + b
	} else {
		sline.OK = true
		sline.Detail = b + " statusline"
	}

	skill := adapter.Check{Key: "skill", Label: "Skill do Cockpit instalada"}
	skillFile := filepath.Join(a.paths.SkillsDir, skillName, "SKILL.md")
	if b, err := os.ReadFile(skillFile); err == nil {
		skill.OK = true
		skill.Detail = skillFile
		if string(b) != skillContent {
			skill.Detail = skillFile + " (desatualizada: rode nobre setup)"
		}
	} else {
		skill.Detail = "não encontrada em " + skillFile
	}

	events := adapter.Check{Key: "events", Label: "Recebendo eventos", Detail: "nenhum evento recebido ainda"}
	if last, ok := a.lastEvent(); ok {
		events.OK = true
		events.Detail = "último " + ago(a.deps.Now().Sub(last))
	}

	d.Checks = []adapter.Check{cliCheck, hooks, sline, skill, events}
	return d
}

func (a *Adapter) lastEvent() (time.Time, bool) {
	if a.st == nil {
		return time.Time{}, false
	}
	ss, err := a.st.RecentSessions(100)
	if err != nil {
		return time.Time{}, false
	}
	var last time.Time
	for _, s := range ss {
		if s.Agent == "claude" && s.LastEventAt.After(last) {
			last = s.LastEventAt
		}
	}
	return last, !last.IsZero()
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "há instantes"
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("há %d h", int(d.Hours()))
	default:
		return fmt.Sprintf("há %d dias", int(d.Hours()/24))
	}
}
