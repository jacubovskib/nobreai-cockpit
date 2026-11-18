package claude

import (
	"context"
	"errors"
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

// Detect monta o checklist de saúde do Claude Code: CLI (com versão), hooks,
// status line, skill e eventos recebidos.
func (a *Adapter) Detect() adapter.Detection {
	d := adapter.Detection{Checks: []adapter.Check{}}
	if a.paths.SettingsPath == "" && a.paths.SkillsDir == "" {
		d.Checks = append(d.Checks, adapter.Check{Key: "paths", Label: "Caminhos", Detail: msgNoPaths})
		return d
	}
	cli := a.cliPath()
	d.Found = cli != ""
	d.Path = cli
	if d.Found && a.paths.Version != nil {
		d.Version = a.paths.Version(cli)
	}
	cs := a.checks(true)
	events := adapter.Check{Key: "events", Label: "Recebendo eventos", Detail: "nenhum evento recebido ainda"}
	if last, ok := a.lastEvent(); ok {
		events.OK = true
		events.Detail = "último " + ago(a.deps.Now().Sub(last))
	}
	d.Checks = append(cs, events)
	return d
}

// Health é o checklist barato do monitor: os checks de Detect sem consultar a
// versão do CLI (que executa um processo) e sem os eventos.
func (a *Adapter) Health() []adapter.Check {
	if a.paths.SettingsPath == "" && a.paths.SkillsDir == "" {
		return nil
	}
	return a.checks(false)
}

// WatchedFiles são os arquivos cuja mudança pede uma conferência completa.
func (a *Adapter) WatchedFiles() []string {
	var fs []string
	if a.paths.SettingsPath != "" {
		fs = append(fs, a.paths.SettingsPath)
	}
	if a.paths.SkillsDir != "" {
		fs = append(fs, SkillFile(a.paths.SkillsDir))
	}
	a.nobreMu.Lock()
	nobre := a.paths.NobreBin
	a.nobreMu.Unlock()
	if nobre != "" {
		fs = append(fs, nobre)
	}
	return fs
}

func (a *Adapter) checks(withVersion bool) []adapter.Check {
	cli := a.cliPath()
	cliCheck := adapter.Check{Key: "cli", Label: "CLI encontrado", OK: cli != "", Detail: "claude não está no PATH nem em ~/.local/bin"}
	if cli == "" {
		cliCheck.Code = "not_found"
	} else {
		version := ""
		if withVersion && a.paths.Version != nil {
			version = a.paths.Version(cli)
		}
		cliCheck.Detail = strings.TrimSpace(cli + " " + version)
	}
	// O State de cada item resume o estado dele para a impressão de "Ignorar".
	cliCheck.State = cli

	hooks := adapter.Check{Key: "hooks", Label: "Hooks instalados"}
	missing, bins, err := installedHooks(a.paths.SettingsPath)
	var absent *settingsMissingError
	switch {
	case errors.As(err, &absent):
		hooks.Detail = err.Error()
		hooks.Code = "missing"
	case err != nil:
		hooks.Detail = err.Error()
		hooks.Code = "unreadable"
	case len(missing) > 0:
		hooks.Detail = "faltam: " + strings.Join(missing, ", ")
		hooks.Code = "missing"
	default:
		hooks.OK = true
		hooks.Detail = strings.Join(hookEvents, ", ")
		for _, b := range bins {
			if !binaryExists(b) {
				hooks.OK = false
				hooks.Code = "dead_binary"
				hooks.Detail = "aponta para um binário que não existe: " + b
				break
			}
		}
	}
	hooks.State = strings.Join(bins, ",")

	sline := adapter.Check{Key: statusLineCheckKey, Label: "Status line integrada", State: "missing"}
	if cmd := statusLineCommand(a.paths.SettingsPath); cmd != "" {
		sline.State = cmd
	}
	if b, detail := statusLineState(a.paths.SettingsPath); b == "" {
		sline.Detail = detail
		sline.Code = "missing"
		if _, _, exists, lerr := loadSettings(a.paths.SettingsPath); lerr != nil {
			sline.Code = "unreadable"
		} else if !exists {
			sline.Code = "no_settings"
		}
	} else if !binaryExists(b) {
		sline.Detail = "aponta para um binário que não existe: " + b
		sline.Code = "dead_binary"
	} else {
		sline.OK = true
		sline.Detail = b + " statusline"
	}

	skill := adapter.Check{Key: "skill", Label: "Skill do Cockpit instalada"}
	skillFile := SkillFile(a.paths.SkillsDir)
	skill.State = "missing"
	if st, err := os.Stat(skillFile); err == nil {
		skill.State = fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
	}
	if b, err := os.ReadFile(skillFile); err == nil {
		skill.OK = true
		skill.Detail = skillFile
		if string(b) != skillContent {
			skill.Detail = skillFile + " (desatualizada: rode nobre setup)"
			skill.Code = "outdated"
		}
	} else {
		skill.Detail = "não encontrada em " + skillFile
		skill.Code = "missing"
	}
	return []adapter.Check{cliCheck, hooks, sline, skill}
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
