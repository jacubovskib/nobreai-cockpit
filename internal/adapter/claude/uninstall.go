package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

// UninstallSettings desfaz InstallSettings num único ler-alterar-gravar de
// settingsPath (um só backup): tira os hooks do Cockpit de todos os eventos,
// apaga eventos e "hooks" que ficarem vazios e devolve a status line original
// (a forma --resumo sai inteira). O refreshInterval sai quando tem o valor que
// o setup grava (60). Hooks e status line de terceiros ficam. Erro da status
// line vem em slErr e não impede tirar os hooks; err é fatal (nada gravado).
// dry=true só calcula.
func UninstallSettings(settingsPath string, dry bool) (rep adapter.InstallReport, slErr, err error) {
	return uninstallSettings(settingsPath, time.Now(), !dry)
}

func uninstallSettings(settingsPath string, now time.Time, write bool) (adapter.InstallReport, error, error) {
	rep := adapter.InstallReport{Changed: []string{}, Messages: []string{}}
	if settingsPath == "" {
		return rep, nil, errors.New("caminho do settings.json não definido")
	}
	root, raw, exists, err := loadSettings(settingsPath)
	if err != nil {
		return rep, nil, err
	}
	if !exists {
		rep.Messages = append(rep.Messages, settingsPath+" não existe; nada a remover.")
		return rep, nil, nil
	}
	events, err := removeHooks(root)
	if err != nil {
		return rep, nil, err
	}
	slChanged, slMsg, slErr := removeStatusLine(root)
	rep.Changed = events
	if slChanged {
		rep.Changed = append(rep.Changed, "statusLine")
	}
	if len(rep.Changed) == 0 {
		rep.Messages = append(rep.Messages, "Nenhum hook nem status line do nobre em "+settingsPath+"; nada a remover.")
		return rep, slErr, nil
	}
	if !write {
		if len(events) > 0 {
			rep.Messages = append(rep.Messages, "Removeria os hooks de "+strings.Join(events, ", ")+" ("+settingsPath+").")
		}
		if slChanged {
			rep.Messages = append(rep.Messages, "Removeria a integração da status line: "+slMsg)
		}
		return rep, slErr, nil
	}
	bak, err := commitSettings(settingsPath, root, raw, exists, now)
	if err != nil {
		return rep, slErr, err
	}
	rep.Backup = bak
	if len(events) > 0 {
		rep.Messages = append(rep.Messages, "Hooks removidos de "+strings.Join(events, ", ")+".")
	}
	if slChanged {
		rep.Messages = append(rep.Messages, "Status line restaurada: "+slMsg)
	}
	if bak != "" {
		rep.Messages = append(rep.Messages, "Backup: "+bak)
	}
	return rep, slErr, nil
}

// removeHooks tira os hooks nossos de todos os eventos de root["hooks"] e
// devolve os eventos alterados, em ordem. root é modificado.
func removeHooks(root map[string]any) ([]string, error) {
	v, present := root["hooks"]
	if !present || v == nil {
		return nil, nil
	}
	hooks, ok := v.(map[string]any)
	if !ok {
		return nil, ErrInvalidSettings
	}
	var changed []string
	for ev, cur := range hooks {
		entries, ok := cur.([]any)
		if !ok || countOurs(entries) == 0 {
			continue
		}
		changed = append(changed, ev)
		if kept := stripOurs(entries); len(kept) > 0 {
			hooks[ev] = kept
		} else {
			delete(hooks, ev)
		}
	}
	sort.Strings(changed)
	if len(changed) > 0 && len(hooks) == 0 {
		delete(root, "hooks")
	}
	return changed, nil
}

// removeStatusLine devolve a status line de antes do setup. msg descreve o
// que fica. root é modificado só quando changed.
func removeStatusLine(root map[string]any) (changed bool, msg string, err error) {
	sl, err := statusLineOf(root)
	if err != nil || sl == nil {
		return false, "", err
	}
	cur, _ := sl["command"].(string)
	cp, ours, err := parseStatusLine(cur)
	if err != nil || !ours {
		return false, "", err
	}
	if cp.summary {
		delete(root, "statusLine")
		return true, "sem status line anterior, a statusLine sai.", nil
	}
	sl["command"] = cp.orig
	if n, ok := sl["refreshInterval"].(json.Number); ok && n.String() == fmt.Sprint(statusLineRefresh) {
		delete(sl, "refreshInterval")
	}
	return true, "volta a " + cp.orig, nil
}

// UninstallSkill apaga <dir>/nobre-cockpit/SKILL.md e a pasta, se ficar vazia.
// removed=false quando não havia skill instalada.
func UninstallSkill(dir string) (removed bool, err error) {
	f := SkillFile(dir)
	if err := os.Remove(f); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.Remove(filepath.Dir(f)); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
		return true, err
	}
	return true, nil
}
