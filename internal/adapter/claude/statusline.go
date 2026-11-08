package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

// Status line: o Claude Code passa o JSON da sessão ao comando de
// statusLine.command pelo stdin. O Cockpit entra no pipe:
//
//	<nobre> statusline | { <comando original> ; }   (com status line anterior)
//	<nobre> statusline --resumo                     (sem status line anterior)
//
// "nobre statusline" repassa o stdin intacto e envia os limites do plano ao
// daemon. O comando original continua rodando no shell do Claude Code.

const (
	statusLineArg      = " statusline"
	statusLineSummary  = " --resumo"
	statusLinePipe     = " | { "
	statusLineRefresh  = 60
	statusLineCheckKey = "statusline"
)

// ErrStatusLineShape: o comando é do Cockpit mas não tem a forma que gravamos.
var ErrStatusLineShape = errors.New("o comando da status line é do nobre mas tem um formato desconhecido; nada foi alterado")

var trailingComment = regexp.MustCompile(`(^|\s)#`)

// quoteBin devolve o caminho do nobre pronto para o shell: como está, ou entre
// aspas simples quando tem caracteres especiais.
func quoteBin(b string) string {
	if b != "" && strings.Trim(b, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_/.+-:@,%=~") == "" {
		return b
	}
	return "'" + strings.ReplaceAll(b, "'", `'\''`) + "'"
}

// wrapCommand monta o comando com o wrapper em torno de orig. O fechamento é
// " ; }" salvo quando isso quebraria o original (várias linhas, comentário na
// última linha, ou termina em ; ou &): aí vai "\n}".
func wrapCommand(bin, orig string) string {
	end := " ; }"
	t := strings.TrimRight(orig, " \t\r\n")
	last := t[strings.LastIndex(t, "\n")+1:]
	if strings.Contains(orig, "\n") || trailingComment.MatchString(last) || strings.HasSuffix(t, ";") || strings.HasSuffix(t, "&") {
		end = "\n}"
	}
	return quoteBin(bin) + statusLineArg + statusLinePipe + orig + end
}

func summaryCommand(bin string) string { return quoteBin(bin) + statusLineArg + statusLineSummary }

// slCmd é um comando de status line reconhecido como nosso.
type slCmd struct {
	bin     string // como gravado (sem aspas)
	summary bool   // forma --resumo (sem original)
	orig    string // comando original (forma wrapper)
}

// parseStatusLine reconhece "<...>/nobre statusline ...". ours=false: não é
// nosso; ours=true com err: é nosso mas de formato desconhecido.
func parseStatusLine(cmd string) (sl slCmd, ours bool, err error) {
	var bin, rest string
	switch {
	case strings.HasPrefix(cmd, "'"):
		end := strings.Index(cmd[1:], "'")
		if end < 0 {
			return sl, false, nil
		}
		bin, rest = cmd[1:1+end], cmd[2+end:]
		if strings.HasPrefix(rest, `\''`) { // aspas simples dentro do caminho: não geramos
			return sl, false, nil
		}
	default:
		i := strings.Index(cmd, " ")
		if i < 0 {
			return sl, false, nil
		}
		bin, rest = cmd[:i], cmd[i:]
	}
	if base := path.Base(bin); base != "nobre" || !strings.HasPrefix(rest, statusLineArg) {
		return sl, false, nil
	}
	rest = rest[len(statusLineArg):]
	sl.bin = bin
	switch {
	case rest == statusLineSummary:
		sl.summary = true
		return sl, true, nil
	case strings.HasPrefix(rest, statusLinePipe):
		body := rest[len(statusLinePipe):]
		switch {
		case strings.HasSuffix(body, "\n}"):
			sl.orig = strings.TrimSuffix(body, "\n}")
		case strings.HasSuffix(body, " ; }"):
			sl.orig = strings.TrimSuffix(body, " ; }")
		default:
			return sl, true, ErrStatusLineShape
		}
		return sl, true, nil
	}
	return sl, true, ErrStatusLineShape
}

func (sl slCmd) build(bin string) string {
	if sl.summary {
		return summaryCommand(bin)
	}
	return wrapCommand(bin, sl.orig)
}

// statusLineOf devolve o objeto statusLine de root (nil se ausente).
func statusLineOf(root map[string]any) (map[string]any, error) {
	v, present := root["statusLine"]
	if !present || v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrInvalidSettings
	}
	return m, nil
}

// InstallStatusLine põe o `nobre statusline` na status line de settingsPath,
// preservando o comando existente (ver forma acima). Idempotente; reinstalar
// com outro caminho troca só o caminho. refreshInterval=60 só se ausente.
func InstallStatusLine(settingsPath, nobreBin string) (adapter.InstallReport, error) {
	return installStatusLine(settingsPath, nobreBin, time.Now(), true)
}

// PlanStatusLine é o dry-run de InstallStatusLine.
func PlanStatusLine(settingsPath, nobreBin string) (adapter.InstallReport, error) {
	return installStatusLine(settingsPath, nobreBin, time.Now(), false)
}

func installStatusLine(settingsPath, nobreBin string, now time.Time, write bool) (adapter.InstallReport, error) {
	rep := adapter.InstallReport{Changed: []string{}, Messages: []string{}}
	if settingsPath == "" || nobreBin == "" {
		return rep, errors.New("caminho do settings.json ou do nobre não definido")
	}
	root, raw, exists, err := loadSettings(settingsPath)
	if err != nil {
		return rep, err
	}
	sl, err := statusLineOf(root)
	if err != nil {
		return rep, err
	}
	if sl == nil {
		sl = map[string]any{}
	}
	if t, ok := sl["type"].(string); ok && t != "command" {
		rep.Messages = append(rep.Messages, fmt.Sprintf("Status line do tipo %q não é um comando; nada a alterar.", t))
		return rep, nil
	}
	cur, hasCmd := sl["command"].(string)
	if _, present := sl["command"]; present && !hasCmd {
		return rep, ErrInvalidSettings
	}
	var want string
	cp, ours, perr := parseStatusLine(cur)
	switch {
	case perr != nil:
		return rep, perr
	case ours:
		want = cp.build(nobreBin)
	case strings.TrimSpace(cur) == "":
		want = summaryCommand(nobreBin)
	default:
		want = wrapCommand(nobreBin, cur)
	}
	_, hasRefresh := sl["refreshInterval"]
	if cur == want && hasRefresh && sl["type"] == "command" {
		rep.Messages = append(rep.Messages, "Status line já integrada; nada a alterar.")
		return rep, nil
	}
	sl["type"], sl["command"] = "command", want
	if !hasRefresh {
		sl["refreshInterval"] = json.Number(fmt.Sprint(statusLineRefresh))
	}
	root["statusLine"] = sl
	rep.Changed = []string{"statusLine"}
	if !write {
		rep.Messages = append(rep.Messages, "Integraria a status line ("+settingsPath+"): "+want)
		return rep, nil
	}
	bak, err := commitSettings(settingsPath, root, raw, exists, now)
	if err != nil {
		return rep, err
	}
	rep.Backup = bak
	rep.Messages = append(rep.Messages, "Status line integrada.")
	if bak != "" {
		rep.Messages = append(rep.Messages, "Backup: "+bak)
	}
	return rep, nil
}

// UninstallStatusLine desfaz InstallStatusLine: devolve o comando original;
// na forma --resumo remove a statusLine (era só nossa). O refreshInterval fica.
func UninstallStatusLine(settingsPath, nobreBin string) (adapter.InstallReport, error) {
	rep := adapter.InstallReport{Changed: []string{}, Messages: []string{}}
	if settingsPath == "" {
		return rep, errors.New("caminho do settings.json não definido")
	}
	root, raw, exists, err := loadSettings(settingsPath)
	if err != nil {
		return rep, err
	}
	sl, err := statusLineOf(root)
	if err != nil {
		return rep, err
	}
	cur, _ := sl["command"].(string)
	cp, ours, perr := parseStatusLine(cur)
	if perr != nil {
		return rep, perr
	}
	if !ours {
		rep.Messages = append(rep.Messages, "Status line não é do nobre; nada a alterar.")
		return rep, nil
	}
	if cp.summary {
		delete(root, "statusLine")
	} else {
		sl["command"] = cp.orig
	}
	rep.Changed = []string{"statusLine"}
	bak, err := commitSettings(settingsPath, root, raw, exists, time.Now())
	if err != nil {
		return rep, err
	}
	rep.Backup = bak
	rep.Messages = append(rep.Messages, "Status line restaurada.")
	return rep, nil
}

// statusLineState descreve a status line para o Detect: o binário que o
// wrapper chama ("" = não integrada) e o motivo quando não está.
func statusLineState(settingsPath string) (bin, detail string) {
	root, _, exists, err := loadSettings(settingsPath)
	if err != nil {
		return "", err.Error()
	}
	if !exists {
		return "", settingsPath + " não existe"
	}
	sl, err := statusLineOf(root)
	if err != nil {
		return "", err.Error()
	}
	cur, _ := sl["command"].(string)
	cp, ours, perr := parseStatusLine(cur)
	switch {
	case perr != nil:
		return "", perr.Error()
	case !ours:
		return "", "a status line não passa pelo nobre: rode nobre setup"
	}
	return cp.bin, ""
}
