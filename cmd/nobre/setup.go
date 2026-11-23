package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/kiro"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// setupDeps são os caminhos e comandos do `nobre setup`. Os testes injetam
// diretórios temporários e um executor falso; nada aqui toca o sistema real.
type setupDeps struct {
	SettingsPath string        // ~/.claude/settings.json
	SkillsDir    string        // ~/.claude/skills
	UnitDir      string        // ~/.config/systemd/user
	NobreBin     string        // caminho absoluto do nobre (vai nos hooks)
	Kiro         *kiro.Adapter // instalador do Kiro; nil = não mexer no Kiro
	NobredBin    string        // caminho absoluto do nobred; vazio = não encontrado
	DaemonWait   time.Duration // quanto esperar o nobred responder antes de cadastrar o projeto
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
		Kiro:         newKiro(exe),
		DaemonWait:   10 * time.Second,
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

// newKiro monta o instalador do Kiro com o nobre que está rodando.
func newKiro(nobre string) *kiro.Adapter {
	a := kiro.New(nil, adapter.Deps{})
	p := kiro.DefaultPaths()
	p.NobreBin = nobre
	a.SetPaths(p)
	return a
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// unitContent monta a unidade do nobred. path (unitPATH) vai em
// Environment=PATH: é a reserva do daemon quando o shell de login do usuário
// não responde (launch.UserPATH).
func unitContent(nobred, path string) string {
	env := ""
	if q := systemdPATH(path); q != "" {
		env = "Environment=" + q + "\n"
	}
	return "[Unit]\nDescription=NobreAI Cockpit (nobred)\nAfter=network.target\n\n" +
		"[Service]\n" + env + "ExecStart=" + nobred + "\nRestart=on-failure\nKillMode=process\n\n" +
		"[Install]\nWantedBy=default.target\n"
}

// unitPATH é o PATH de reserva da unidade: o diretório do nobre, o do claude
// achado no PATH deste terminal, o resto do PATH deste terminal e os do
// sistema. Com bash ou zsh o daemon põe na frente o PATH do shell de login,
// lido na hora de usar (launch.UserPATH); esta reserva só decide sozinha com
// outros shells (fish, por exemplo) ou quando a consulta falha.
func unitPATH(termPATH, nobreDir string) string {
	dirs := []string{nobreDir}
	if p := launch.LookPathIn("claude", termPATH); p != "" {
		dirs = append(dirs, filepath.Dir(p))
	}
	for _, d := range strings.Split(termPATH, ":") {
		if filepath.IsAbs(d) && !strings.ContainsAny(d, "\n\r") {
			dirs = append(dirs, d)
		}
	}
	dirs = append(dirs, "/usr/local/bin", "/usr/bin", "/bin")
	seen := map[string]bool{}
	out := dirs[:0]
	for _, d := range dirs {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return strings.Join(out, ":")
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

// setup instala hooks, status line e skill do Claude Code (e hooks e steering
// do Kiro, se houver kiro-cli), grava e ativa o serviço do
// daemon e oferece cadastrar o repositório atual como projeto. Com
// --remover-statusline só desfaz a integração da status line.
func (c *cli) setup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "mostra o que mudaria sem gravar nada")
	noSL := fs.Bool("sem-statusline", false, "não integra a status line do Claude Code")
	rmSL := fs.Bool("remover-statusline", false, "só remove a integração da status line (devolve o comando original)")
	rmAll := fs.Bool("remover", false, "desfaz o setup: hooks, status line, skill e serviço do nobred")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*noSL && *rmSL) || (*rmAll && (*noSL || *rmSL)) {
		return usagef("uso: nobre setup [--dry-run] [--sem-statusline | --remover-statusline | --remover]")
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

	if *rmSL {
		return c.removeStatusLine(d, *dry, l)
	}
	if *rmAll {
		return c.removeAll(d, *dry, l)
	}

	// 1. Hooks e status line do Claude Code (uma só leitura/gravação do
	// settings.json). A status line entra no pipe e envia os limites do plano.
	rep, slErr, err := claude.InstallSettings(d.SettingsPath, d.NobreBin, !*noSL, *dry)
	if err != nil {
		l.bad("Hooks do Claude Code: %v", err)
	} else {
		for _, m := range rep.Messages {
			l.ok("%s%s", prefix, m)
		}
		if *noSL {
			l.ok("Status line do Claude Code: pulada (--sem-statusline)")
		}
		if slErr != nil {
			l.bad("Status line do Claude Code: %v", slErr)
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

	// 3. Kiro (só se o kiro-cli existir).
	c.setupKiro(d, *dry, l)

	// 4. Serviço do daemon.
	c.setupService(d, *dry, l)

	// 5. Projeto atual.
	c.setupProject(d, *dry, l)

	if l.warn {
		return errors.New("Setup terminou com pendências (itens com !).")
	}
	return nil
}

// setupKiro instala hooks e steering do Kiro quando o kiro-cli existe; sem
// ele, avisa numa linha e segue.
func (c *cli) setupKiro(d *setupDeps, dry bool, l *checklist) {
	if d.Kiro == nil {
		return
	}
	if d.Kiro.Binary() == "" {
		l.ok("Kiro não encontrado (kiro-cli fora do PATH e de ~/.local/bin); pulei")
		return
	}
	prefix := ""
	var rep adapter.InstallReport
	var err error
	if dry {
		prefix = "[dry-run] "
		rep, err = d.Kiro.PlanInstall()
	} else {
		rep, err = d.Kiro.Install()
	}
	if err != nil {
		l.bad("Kiro: %v", err)
		return
	}
	for _, m := range rep.Messages {
		l.ok("%s%s", prefix, m)
	}
}

// removeStatusLine é o "nobre setup --remover-statusline": só desfaz a
// integração da status line; hooks, skill, serviço e projeto ficam como estão.
func (c *cli) removeStatusLine(d *setupDeps, dry bool, l *checklist) error {
	var rep adapter.InstallReport
	var err error
	prefix := ""
	if dry {
		prefix = "[dry-run] "
		rep, err = claude.PlanUninstallStatusLine(d.SettingsPath)
	} else {
		c.tellRemoved("statusline")
		rep, err = claude.UninstallStatusLine(d.SettingsPath, d.NobreBin)
	}
	if err != nil {
		l.bad("Status line do Claude Code: %v", err)
		return errors.New("Setup terminou com pendências (itens com !).")
	}
	for _, m := range rep.Messages {
		l.ok("%s%s", prefix, m)
	}
	return nil
}

// tellRemoved avisa o daemon, antes de escrever, que a remoção é de propósito.
// Melhor esforço: daemon fora do ar não impede a remoção.
func (c *cli) tellRemoved(keys ...string) {
	cx, cancel := context.WithTimeout(ctx(), 2*time.Second)
	defer cancel()
	_ = c.client().IntegrationRemoved(cx, keys, "cli")
}

// removeAll é o "nobre setup --remover": avisa o daemon que hooks, status
// line e skill saem de propósito (tira-os da linha de base, para um setup
// futuro não alertar a remoção), para o serviço e depois desfaz os três.
// Binários e banco ficam.
func (c *cli) removeAll(d *setupDeps, dry bool, l *checklist) error {
	prefix := ""
	if dry {
		prefix = "[dry-run] "
	} else {
		c.tellRemoved("hooks", "statusline", "skill")
	}
	c.removeService(d, dry, l)

	rep, slErr, err := claude.UninstallSettings(d.SettingsPath, dry)
	if err != nil {
		l.bad("Hooks do Claude Code: %v", err)
	} else {
		for _, m := range rep.Messages {
			l.ok("%s%s", prefix, m)
		}
	}
	if slErr != nil {
		l.bad("Status line do Claude Code: %v", slErr)
	}

	skill := claude.SkillFile(d.SkillsDir)
	switch {
	case !isFile(skill):
		l.ok("Skill do Cockpit não instalada; nada a remover")
	case dry:
		l.ok("%sRemoveria a skill %s", prefix, skill)
	default:
		if _, err := claude.UninstallSkill(d.SkillsDir); err != nil {
			l.bad("Skill do Cockpit: %v", err)
		} else {
			l.ok("Skill removida: %s", skill)
		}
	}

	if d.Kiro != nil {
		rep, err := d.Kiro.Uninstall(dry)
		if err != nil {
			l.bad("Kiro: %v", err)
		}
		for _, m := range rep.Messages {
			l.ok("%s%s", prefix, m)
		}
	}

	if dir := filepath.Dir(d.NobreBin); !dry {
		fmt.Fprintf(c.out, "\nOs binários em %s e o banco em %s não foram tocados (o make uninstall também apaga os binários).\n"+
			"Reinicie as sessões do Claude Code abertas: elas guardam os hooks de quando começaram.\n", dir, filepath.Dir(store.DefaultPath()))
	}
	if l.warn {
		return errors.New("Remoção terminou com pendências (itens com !).")
	}
	return nil
}

func (c *cli) removeService(d *setupDeps, dry bool, l *checklist) {
	unit := filepath.Join(d.UnitDir, "nobred.service")
	if !isFile(unit) {
		l.ok("Serviço do nobred não instalado; nada a remover")
		return
	}
	if dry {
		l.ok("[dry-run] Rodaria: systemctl --user disable --now nobred")
		l.ok("[dry-run] Apagaria %s", unit)
		l.ok("[dry-run] Rodaria: systemctl --user daemon-reload")
		return
	}
	if err := d.Run("systemctl", "--user", "disable", "--now", "nobred"); err != nil {
		l.bad("systemctl --user disable --now nobred: %v", err)
		return
	}
	if err := os.Remove(unit); err != nil {
		l.bad("Unidade do systemd: %v", err)
		return
	}
	if err := d.Run("systemctl", "--user", "daemon-reload"); err != nil {
		l.bad("systemctl --user daemon-reload: %v", err)
		return
	}
	l.ok("Serviço do nobred parado e removido (%s)", unit)
}

const sessionEnvVars = "DISPLAY WAYLAND_DISPLAY XAUTHORITY DBUS_SESSION_BUS_ADDRESS"

func (c *cli) setupService(d *setupDeps, dry bool, l *checklist) {
	if d.NobredBin == "" {
		l.bad("Serviço do daemon: não achei o nobred (coloque-o ao lado do nobre ou no PATH)")
		return
	}
	unit := filepath.Join(d.UnitDir, "nobred.service")
	want := unitContent(d.NobredBin, unitPATH(c.getenv("PATH"), filepath.Dir(d.NobreBin)))
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

func (c *cli) setupProject(d *setupDeps, dry bool, l *checklist) {
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
	// O enable --now volta antes de o nobred ouvir: espera ele responder.
	ps, err := cl.Projects(ctx())
	for deadline := time.Now().Add(d.DaemonWait); err != nil && time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
		ps, err = cl.Projects(ctx())
	}
	if err != nil {
		l.bad("Cadastro do projeto: %v (com o daemon no ar, rode nobre project add)", err)
		return
	}
	for _, p := range ps {
		if p.Path == root {
			l.ok("Projeto %s já cadastrado", p.Name)
			return
		}
	}
	p, err := cl.AddProject(ctx(), "", root)
	if err != nil {
		l.bad("Cadastro do projeto: %v (com o daemon no ar, rode nobre project add)", err)
		return
	}
	l.ok("Projeto %s cadastrado: %s", p.Name, p.Path)
}
