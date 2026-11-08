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
	"github.com/nobretech/nobreai-cockpit/internal/launch"
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

// unitContent monta a unidade do nobred. path vai em Environment=PATH: o
// systemd do usuário só tem /usr/local/bin e /usr/bin, e o daemon precisa
// achar o claude e passar o PATH ao terminal do "Continuar".
func unitContent(nobred, path string) string {
	env := ""
	if q := systemdPATH(path); q != "" {
		env = "Environment=" + q + "\n"
	}
	return "[Unit]\nDescription=NobreAI Cockpit (nobred)\nAfter=network.target\n\n" +
		"[Service]\n" + env + "ExecStart=" + nobred + "\nRestart=on-failure\nKillMode=process\n\n" +
		"[Install]\nWantedBy=default.target\n"
}

// systemdPATH devolve "PATH=<path>" entre aspas duplas para o Environment=
// do systemd: \ e " escapados e % dobrado (especificadores). Entradas com
// quebra de linha são descartadas. "" = nada a gravar.
func systemdPATH(path string) string {
	var keep []string
	for _, d := range strings.Split(path, ":") {
		if d != "" && !strings.ContainsAny(d, "\n\r") {
			keep = append(keep, d)
		}
	}
	if len(keep) == 0 {
		return ""
	}
	v := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(strings.Join(keep, ":"))
	return `"PATH=` + v + `"`
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

// setup instala hooks, status line e skill do Claude Code, grava e ativa o serviço do
// daemon e oferece cadastrar o repositório atual como projeto.
func (c *cli) setup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "mostra o que mudaria sem gravar nada")
	noSL := fs.Bool("sem-statusline", false, "não integra a status line do Claude Code")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usagef("uso: nobre setup [--dry-run] [--sem-statusline]")
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

	// 1b. Status line: entra no pipe e envia os limites do plano ao daemon.
	if *noSL {
		l.ok("Status line do Claude Code: pulada (--sem-statusline)")
	} else {
		sline := claude.InstallStatusLine
		if *dry {
			sline = claude.PlanStatusLine
		}
		if rep, err := sline(d.SettingsPath, d.NobreBin); err != nil {
			l.bad("Status line do Claude Code: %v", err)
		} else {
			for _, m := range rep.Messages {
				l.ok("%s%s", prefix, m)
			}
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

const sessionEnvVars = "DISPLAY WAYLAND_DISPLAY XAUTHORITY DBUS_SESSION_BUS_ADDRESS"

func (c *cli) setupService(d *setupDeps, dry bool, l *checklist) {
	if d.NobredBin == "" {
		l.bad("Serviço do daemon: não achei o nobred (coloque-o ao lado do nobre ou no PATH)")
		return
	}
	unit := filepath.Join(d.UnitDir, "nobred.service")
	// O PATH deste terminal (onde o claude funciona) mais o diretório do nobre.
	want := unitContent(d.NobredBin, launch.PathWithDir(c.getenv("PATH"), filepath.Dir(d.NobreBin)))
	cur, _ := os.ReadFile(unit)
	if dry {
		if string(cur) == want {
			l.ok("[dry-run] Unidade %s já está atualizada", unit)
		} else {
			l.ok("[dry-run] Gravaria %s (ExecStart=%s)", unit, d.NobredBin)
		}
		l.ok("[dry-run] Rodaria: systemctl --user import-environment %s", sessionEnvVars)
		l.ok("[dry-run] Rodaria: systemctl --user daemon-reload")
		l.ok("[dry-run] Rodaria: systemctl --user enable --now nobred")
		return
	}
	changed := string(cur) != want
	if !changed {
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
	// O daemon precisa do ambiente gráfico para abrir janelas de terminal.
	if err := d.Run("systemctl", append([]string{"--user", "import-environment"}, strings.Fields(sessionEnvVars)...)...); err != nil {
		l.bad("systemctl --user import-environment: %v", err)
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", "nobred"}} {
		if err := d.Run("systemctl", args...); err != nil {
			l.bad("systemctl %s: %v", strings.Join(args, " "), err)
			return
		}
	}
	// Unidade alterada com o daemon já rodando: enable --now não o reinicia.
	if changed && len(cur) > 0 {
		if err := d.Run("systemctl", "--user", "restart", "nobred"); err != nil {
			l.bad("systemctl --user restart nobred: %v", err)
			return
		}
		l.ok("Daemon nobred reiniciado para usar a unidade nova")
		return
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
