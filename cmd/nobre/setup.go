package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
)

// setupDeps são os caminhos e comandos do `nobre setup`. Os testes injetam
// diretórios temporários e um executor falso; nada aqui toca o sistema real.
type setupDeps struct {
	SettingsPath string // ~/.claude/settings.json
	SkillsDir    string // ~/.claude/skills
	UnitDir      string // ~/.config/systemd/user
	NobreBin     string // caminho absoluto do nobre (vai nos hooks)
	NobredBin    string // caminho absoluto do nobred; vazio = não encontrado
	Run          func(name string, args ...string) error
}

func defaultSetupDeps() (*setupDeps, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	d := &setupDeps{
		SettingsPath: filepath.Join(home, ".claude", "settings.json"),
		SkillsDir:    filepath.Join(home, ".claude", "skills"),
		UnitDir:      filepath.Join(home, ".config", "systemd", "user"),
		NobreBin:     exe,
		Run: func(name string, args ...string) error {
			out, err := exec.Command(name, args...).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		},
	}
	if sib := filepath.Join(filepath.Dir(exe), "nobred"); isFile(sib) {
		d.NobredBin = sib
	} else if lp, err := exec.LookPath("nobred"); err == nil {
		if abs, err := filepath.Abs(lp); err == nil {
			d.NobredBin = abs
		}
	}
	return d, nil
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func unitContent(nobred string) string {
	return "[Unit]\nDescription=NobreAI Cockpit (nobred)\nAfter=network.target\n\n" +
		"[Service]\nExecStart=" + nobred + "\nRestart=on-failure\n\n" +
		"[Install]\nWantedBy=default.target\n"
}

// checklist imprime os itens ✓/! do setup e lembra se algum falhou.
type checklist struct {
	c    *cli
	warn bool
}

func (l *checklist) ok(format string, a ...any) { fmt.Fprintf(l.c.out, "✓ "+format+"\n", a...) }
func (l *checklist) bad(format string, a ...any) {
	l.warn = true
	fmt.Fprintf(l.c.out, "! "+format+"\n", a...)
}

// setup instala hooks e skill do Claude Code, grava e ativa o serviço do
// daemon e oferece cadastrar o repositório atual como projeto.
func (c *cli) setup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "mostra o que mudaria sem gravar nada")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usagef("uso: nobre setup [--dry-run]")
	}
	d := c.sd
	if d == nil {
		if d, err = defaultSetupDeps(); err != nil {
			return err
		}
	}
	l := &checklist{c: c}
	prefix := ""
	if *dry {
		prefix = "[dry-run] "
		fmt.Fprintln(c.out, "Modo dry-run: nada será gravado.")
	}

	// 1. Hooks do Claude Code.
	hooks := claude.InstallHooks
	if *dry {
		hooks = claude.PlanHooks
	}
	if rep, err := hooks(d.SettingsPath, d.NobreBin); err != nil {
		l.bad("Hooks do Claude Code: %v", err)
	} else {
		for _, m := range rep.Messages {
			l.ok("%s%s", prefix, m)
		}
	}

	// 2. Skill.
	if need, err := claude.PlanSkill(d.SkillsDir); err != nil {
		l.bad("Skill do Cockpit: %v", err)
	} else if !need {
		l.ok("Skill do Cockpit já instalada em %s", claude.SkillFile(d.SkillsDir))
	} else if *dry {
		l.ok("%sInstalaria a skill em %s", prefix, claude.SkillFile(d.SkillsDir))
	} else if err := claude.InstallSkill(d.SkillsDir); err != nil {
		l.bad("Skill do Cockpit: %v", err)
	} else {
		l.ok("Skill do Cockpit instalada em %s", claude.SkillFile(d.SkillsDir))
	}

	// 3. Serviço do daemon.
	c.setupService(d, *dry, l)

	// 4. Projeto atual.
	c.setupProject(*dry, l)

	if l.warn {
		return errors.New("Setup terminou com pendências (itens com !).")
	}
	return nil
}

func (c *cli) setupService(d *setupDeps, dry bool, l *checklist) {
	if d.NobredBin == "" {
		l.bad("Serviço do daemon: não achei o nobred (coloque-o ao lado do nobre ou no PATH)")
		return
	}
	unit := filepath.Join(d.UnitDir, "nobred.service")
	want := unitContent(d.NobredBin)
	cur, _ := os.ReadFile(unit)
	if dry {
		if string(cur) == want {
			l.ok("[dry-run] Unidade %s já está atualizada", unit)
		} else {
			l.ok("[dry-run] Gravaria %s (ExecStart=%s)", unit, d.NobredBin)
		}
		l.ok("[dry-run] Rodaria: systemctl --user daemon-reload")
		l.ok("[dry-run] Rodaria: systemctl --user enable --now nobred")
		return
	}
	if string(cur) == want {
		l.ok("Unidade %s já está atualizada", unit)
	} else if err := os.MkdirAll(d.UnitDir, 0o755); err != nil {
		l.bad("Unidade do systemd: %v", err)
		return
	} else if err := os.WriteFile(unit, []byte(want), 0o644); err != nil {
		l.bad("Unidade do systemd: %v", err)
		return
	} else {
		l.ok("Unidade gravada em %s", unit)
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", "nobred"}} {
		if err := d.Run("systemctl", args...); err != nil {
			l.bad("systemctl %s: %v", strings.Join(args, " "), err)
			return
		}
	}
	l.ok("Daemon nobred ativo (systemctl --user enable --now nobred)")
}

// gitRoot sobe de dir até achar .git.
func gitRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func (c *cli) setupProject(dry bool, l *checklist) {
	wd, err := c.cwd()
	if err != nil {
		return
	}
	root, ok := gitRoot(wd)
	if !ok {
		return
	}
	if dry {
		l.ok("[dry-run] Perguntaria se %s deve ser cadastrado como projeto", root)
		return
	}
	fmt.Fprintf(c.out, "Cadastrar %s como projeto? [s/N] ", root)
	line, _ := bufio.NewReader(c.in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "s" && a != "sim" {
		fmt.Fprintln(c.out)
		l.ok("Projeto não cadastrado (use nobre project add quando quiser)")
		return
	}
	cl := c.client()
	if ps, err := cl.Projects(ctx()); err == nil {
		for _, p := range ps {
			if p.Path == root {
				l.ok("Projeto %s já cadastrado", p.Name)
				return
			}
		}
	}
	p, err := cl.AddProject(ctx(), "", root)
	if err != nil {
		l.bad("Cadastro do projeto: %v (com o daemon no ar, rode nobre project add)", err)
		return
	}
	l.ok("Projeto %s cadastrado: %s", p.Name, p.Path)
}
