package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// launchdLabel identifica o serviço do nobred no launchd do macOS. O Makefile
// usa o mesmo rótulo (LAUNCHD_LABEL).
const launchdLabel = "io.nobretech.nobred"

func (d *setupDeps) launchdPlist() string {
	return filepath.Join(d.AgentsDir, launchdLabel+".plist")
}

func (d *setupDeps) launchdDomain() string { return fmt.Sprintf("gui/%d", d.UID) }

func (d *setupDeps) launchdTarget() string { return d.launchdDomain() + "/" + launchdLabel }

// plistContent monta o LaunchAgent do nobred, o equivalente da unidade do
// systemd: KeepAlive só em saída com erro (Restart=on-failure),
// AbandonProcessGroup para os terminais e agentes abertos pelo "Continuar"
// sobreviverem a um restart (KillMode=process) e o PATH de reserva
// (unitPATH) em EnvironmentVariables.
func plistContent(nobred, path, logFile string) string {
	var env string
	if path != "" {
		env = "\t<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>PATH</key>\n\t\t<string>" + xmlEscape(path) + "</string>\n\t</dict>\n"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + launchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(nobred) + `</string>
	</array>
` + env + `	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>AbandonProcessGroup</key>
	<true/>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(logFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(logFile) + `</string>
</dict>
</plist>
`
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// launchdLoaded diz se o serviço está carregado no domínio do usuário.
func (d *setupDeps) launchdLoaded() bool {
	return d.Run("launchctl", "print", d.launchdTarget()) == nil
}

// launchdBootstrap carrega o plist. Logo depois de um bootout o launchd pode
// recusar (o serviço antigo ainda está saindo): tenta de novo por alguns
// segundos.
func (d *setupDeps) launchdBootstrap() error {
	var err error
	for i := 0; i < 10; i++ {
		if err = d.Run("launchctl", "bootstrap", d.launchdDomain(), d.launchdPlist()); err == nil {
			return nil
		}
		if d.Sleep != nil {
			d.Sleep(500 * time.Millisecond)
		}
	}
	return err
}

// setupLaunchd grava o LaunchAgent e deixa o nobred rodando: carrega o plist
// novo (ou alterado, descarregando o anterior) ou, sem mudança, só garante
// que está de pé (kickstart sem -k não reinicia um que já roda).
func (c *cli) setupLaunchd(d *setupDeps, dry bool, l *checklist) {
	plist := d.launchdPlist()
	want := plistContent(d.NobredBin, unitPATH(c.getenv("PATH"), filepath.Dir(d.NobreBin)), filepath.Join(d.LogDir, "nobred.log"))
	cur, _ := os.ReadFile(plist)
	changed := string(cur) != want
	if dry {
		if changed {
			l.ok("[dry-run] Gravaria %s (ProgramArguments=%s)", plist, d.NobredBin)
		} else {
			l.ok("[dry-run] LaunchAgent %s já está atualizado", plist)
		}
		l.ok("[dry-run] Rodaria: launchctl bootstrap %s %s", d.launchdDomain(), plist)
		return
	}
	if changed {
		for _, dir := range []string{d.AgentsDir, d.LogDir} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				l.bad("LaunchAgent do nobred: %v", err)
				return
			}
		}
		if err := os.WriteFile(plist, []byte(want), 0o644); err != nil {
			l.bad("LaunchAgent do nobred: %v", err)
			return
		}
		l.ok("LaunchAgent gravado em %s", plist)
	} else {
		l.ok("LaunchAgent %s já está atualizado", plist)
	}
	// Um "launchctl disable" anterior impediria o bootstrap.
	_ = d.Run("launchctl", "enable", d.launchdTarget())
	loaded := d.launchdLoaded()
	switch {
	case loaded && !changed:
		if err := d.Run("launchctl", "kickstart", d.launchdTarget()); err != nil {
			l.bad("launchctl kickstart %s: %v", d.launchdTarget(), err)
			return
		}
		l.ok("Daemon nobred ativo (launchd: %s)", d.launchdTarget())
		return
	case loaded:
		if err := d.Run("launchctl", "bootout", d.launchdTarget()); err != nil {
			l.bad("launchctl bootout %s: %v", d.launchdTarget(), err)
			return
		}
	}
	if err := d.launchdBootstrap(); err != nil {
		l.bad("launchctl bootstrap %s %s: %v", d.launchdDomain(), plist, err)
		return
	}
	if loaded {
		l.ok("Daemon nobred reiniciado para usar o LaunchAgent novo")
		return
	}
	l.ok("Daemon nobred ativo (launchd: %s; log em %s)", d.launchdTarget(), filepath.Join(d.LogDir, "nobred.log"))
}

// removeLaunchd para o nobred e apaga o LaunchAgent.
func (c *cli) removeLaunchd(d *setupDeps, dry bool, l *checklist) {
	plist := d.launchdPlist()
	if !isFile(plist) {
		l.ok("Serviço do nobred não instalado; nada a remover")
		return
	}
	if dry {
		l.ok("[dry-run] Rodaria: launchctl bootout %s", d.launchdTarget())
		l.ok("[dry-run] Apagaria %s", plist)
		return
	}
	if d.launchdLoaded() {
		if err := d.Run("launchctl", "bootout", d.launchdTarget()); err != nil {
			l.bad("launchctl bootout %s: %v", d.launchdTarget(), err)
			return
		}
	}
	if err := os.Remove(plist); err != nil {
		l.bad("LaunchAgent do nobred: %v", err)
		return
	}
	l.ok("Serviço do nobred parado e removido (%s)", plist)
}
