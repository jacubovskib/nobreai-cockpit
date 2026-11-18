package claude

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
)

// skillContent é o SKILL.md instalado em ~/.claude/skills/nobre-cockpit.
//
//go:embed skill.md
var skillContent string

const skillName = "nobre-cockpit"

// hookEvents são os eventos do Claude Code que o Cockpit escuta.
var hookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "SessionEnd"}

const hookTimeout = 10

const msgNoPaths = "caminhos não configurados"

// ErrInvalidSettings é devolvido quando settings.json existe mas não é JSON
// válido: nada é escrito.
var ErrInvalidSettings = errors.New("~/.claude/settings.json não é um JSON válido; nada foi alterado")

// isOurs reconhece um comando de hook do Cockpit: "<...>/nobre hook claude <Evento>".
func isOurs(cmd string) bool {
	i := strings.Index(cmd, " hook claude ")
	if i < 0 {
		return false
	}
	prefix := cmd[:i]
	return prefix == "nobre" || strings.HasSuffix(prefix, "/nobre")
}

// loadSettings lê settings.json. Arquivo ausente ou vazio vira {}.
func loadSettings(path string) (root map[string]any, raw []byte, exists bool, err error) {
	raw, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, raw, true, nil
	}
	if !json.Valid(raw) {
		return nil, raw, true, ErrInvalidSettings
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil || root == nil {
		return nil, raw, true, ErrInvalidSettings
	}
	if dec.More() {
		return nil, raw, true, ErrInvalidSettings
	}
	return root, raw, true, nil
}

func wantMatcher(event string) string {
	if event == "PreToolUse" || event == "PostToolUse" {
		return "*"
	}
	return ""
}

func desiredEntry(event, nobreBin string) map[string]any {
	e := map[string]any{"hooks": []any{map[string]any{
		"type": "command", "command": nobreBin + " hook claude " + event, "timeout": json.Number(fmt.Sprint(hookTimeout)),
	}}}
	if m := wantMatcher(event); m != "" {
		e["matcher"] = m
	}
	return e
}

// stripOurs remove os hooks nossos de uma lista de entradas. Entradas que
// ficam sem hooks somem; entradas de terceiros ficam como estavam.
func stripOurs(entries []any) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			out = append(out, e)
			continue
		}
		hs, ok := m["hooks"].([]any)
		if !ok {
			out = append(out, e)
			continue
		}
		kept := make([]any, 0, len(hs))
		for _, h := range hs {
			if hm, ok := h.(map[string]any); ok {
				if c, _ := hm["command"].(string); isOurs(c) {
					continue
				}
			}
			kept = append(kept, h)
		}
		if len(kept) == len(hs) {
			out = append(out, e)
			continue
		}
		if len(kept) == 0 {
			continue
		}
		cp := map[string]any{}
		for k, v := range m {
			cp[k] = v
		}
		cp["hooks"] = kept
		out = append(out, cp)
	}
	return out
}

// mergeHooks devolve os eventos alterados; root é modificado.
func mergeHooks(root map[string]any, nobreBin string) ([]string, error) {
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		if root["hooks"] != nil {
			return nil, ErrInvalidSettings
		}
		hooks = map[string]any{}
	}
	var changed []string
	for _, ev := range hookEvents {
		var entries []any
		if cur, present := hooks[ev]; present {
			if entries, ok = cur.([]any); !ok {
				return nil, ErrInvalidSettings
			}
		}
		want := desiredEntry(ev, nobreBin)
		// Já instalado exatamente como queremos (e uma única vez): nada a fazer.
		if countOurs(entries) == 1 && containsEntry(entries, want) {
			continue
		}
		hooks[ev] = append(stripOurs(entries), want)
		changed = append(changed, ev)
	}
	root["hooks"] = hooks
	return changed, nil
}

func countOurs(entries []any) int {
	n := 0
	for _, e := range entries {
		m, _ := e.(map[string]any)
		hs, _ := m["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if c, _ := hm["command"].(string); isOurs(c) {
				n++
			}
		}
	}
	return n
}

func containsEntry(entries []any, want map[string]any) bool {
	for _, e := range entries {
		if reflect.DeepEqual(e, any(want)) {
			return true
		}
	}
	return false
}

// settingsMissingError distingue "settings.json não existe" de JSON inválido.
type settingsMissingError struct{ path string }

func (e *settingsMissingError) Error() string { return e.path + " não existe" }

// installedHooks lista os eventos sem hook nosso em settingsPath e os
// binários (sem repetição, na ordem) que os hooks nossos chamam.
func installedHooks(settingsPath string) (missing, bins []string, err error) {
	root, _, exists, err := loadSettings(settingsPath)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, &settingsMissingError{settingsPath}
	}
	hooks, _ := root["hooks"].(map[string]any)
	seen := map[string]bool{}
	for _, ev := range hookEvents {
		entries, _ := hooks[ev].([]any)
		if countOurs(entries) == 0 {
			missing = append(missing, ev)
		}
		for _, e := range entries {
			m, _ := e.(map[string]any)
			hs, _ := m["hooks"].([]any)
			for _, h := range hs {
				hm, _ := h.(map[string]any)
				if c, _ := hm["command"].(string); isOurs(c) {
					b := c[:strings.Index(c, " hook claude ")]
					if !seen[b] {
						seen[b] = true
						bins = append(bins, b)
					}
				}
			}
		}
	}
	return missing, bins, nil
}

// InstallHooks mescla os hooks do Cockpit em settingsPath, preservando o que já
// existe. É idempotente: sem mudanças não grava nem faz backup. Antes de
// alterar um arquivo existente grava settings.json.bak-<AAAAMMDDHHMMSS> ao lado.
func InstallHooks(settingsPath, nobreBin string) (adapter.InstallReport, error) {
	return installHooks(settingsPath, nobreBin, time.Now(), true)
}

// PlanHooks faz o mesmo cálculo de InstallHooks sem escrever nada (dry-run).
func PlanHooks(settingsPath, nobreBin string) (adapter.InstallReport, error) {
	return installHooks(settingsPath, nobreBin, time.Now(), false)
}

func installHooks(settingsPath, nobreBin string, now time.Time, write bool) (adapter.InstallReport, error) {
	rep, _, err := installSettings(settingsPath, nobreBin, now, write, false)
	return rep, err
}

// InstallSettings instala hooks e, com statusLine, a status line num único
// ler-alterar-gravar de settings.json (um só backup). Erro da status line vem
// em slErr e não impede os hooks; err é fatal (nada gravado). dry=true só calcula.
func InstallSettings(settingsPath, nobreBin string, statusLine, dry bool) (rep adapter.InstallReport, slErr, err error) {
	return installSettings(settingsPath, nobreBin, time.Now(), !dry, statusLine)
}

func installSettings(settingsPath, nobreBin string, now time.Time, write, withSL bool) (adapter.InstallReport, error, error) {
	rep := adapter.InstallReport{Changed: []string{}, Messages: []string{}}
	if settingsPath == "" || nobreBin == "" {
		return rep, nil, errors.New("caminho do settings.json ou do nobre não definido")
	}
	root, raw, exists, err := loadSettings(settingsPath)
	if err != nil {
		return rep, nil, err
	}
	changed, err := mergeHooks(root, nobreBin)
	if err != nil {
		return rep, nil, err
	}
	var sl slResult
	var slErr error
	if withSL {
		sl, slErr = applyStatusLine(root, nobreBin)
	}
	if len(changed) == 0 {
		rep.Messages = append(rep.Messages, "Hooks já instalados; nada a alterar.")
	} else if !write {
		rep.Messages = append(rep.Messages, "Instalaria hooks em "+strings.Join(changed, ", ")+" ("+settingsPath+").")
	}
	if withSL && slErr == nil && !sl.changed {
		rep.Messages = append(rep.Messages, sl.note)
	} else if sl.changed && !write {
		rep.Messages = append(rep.Messages, "Integraria a status line ("+settingsPath+"): "+sl.command)
	}
	rep.Changed = changed
	if sl.changed {
		rep.Changed = append(rep.Changed, "statusLine")
	}
	if len(rep.Changed) == 0 || !write {
		return rep, slErr, nil
	}
	bak, err := commitSettings(settingsPath, root, raw, exists, now)
	if err != nil {
		return rep, slErr, err
	}
	rep.Backup = bak
	if len(changed) > 0 {
		rep.Messages = append(rep.Messages, "Hooks instalados em "+strings.Join(changed, ", ")+".")
	}
	if sl.changed {
		rep.Messages = append(rep.Messages, "Status line integrada.")
	}
	if bak != "" {
		rep.Messages = append(rep.Messages, "Backup: "+bak)
	}
	return rep, slErr, nil
}

// commitSettings grava root em settingsPath: backup O_EXCL do conteúdo atual
// (se havia), JSON sem escapar &/</>, escrita atômica que preserva symlink.
func commitSettings(settingsPath string, root map[string]any, raw []byte, exists bool, now time.Time) (backup string, err error) {
	if exists && len(bytes.TrimSpace(raw)) > 0 {
		if backup, err = writeBackup(settingsPath+".bak-"+now.Format("20060102150405"), raw); err != nil {
			return "", err
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return backup, err
	}
	return backup, writeFileAtomic(settingsPath, buf.Bytes())
}

// writeBackup cria o backup com O_EXCL; se o nome existe, acrescenta -1, -2...
func writeBackup(name string, data []byte) (string, error) {
	for i := 0; ; i++ {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s-%d", name, i)
		}
		f, err := os.OpenFile(n, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return n, werr
	}
}

// writeFileAtomic escreve em path; se for um link simbólico, escreve no
// destino e o link sobrevive.
func writeFileAtomic(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nobre-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SkillFile é o caminho do SKILL.md dentro de dir.
func SkillFile(dir string) string { return filepath.Join(dir, skillName, "SKILL.md") }

// PlanSkill diz se InstallSkill escreveria algo (arquivo ausente ou diferente).
func PlanSkill(dir string) (bool, error) {
	b, err := os.ReadFile(SkillFile(dir))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return string(b) != skillContent, nil
}

// InstallSkill grava <dir>/nobre-cockpit/SKILL.md a partir do skill.md embutido.
func InstallSkill(dir string) error {
	need, err := PlanSkill(dir)
	if err != nil || !need {
		return err
	}
	return writeFileAtomic(SkillFile(dir), []byte(skillContent))
}

const msgNoNobre = "não achei o binário nobre (coloque-o ao lado do nobred ou no PATH)"

// resolveNobre acha o nobre agora (ao lado do nobred, depois no PATH do
// usuário), porque o caminho de quando o daemon subiu pode ter sumido num
// make install. Atualiza paths.NobreBin quando acha; "" = não achou (e mantém
// o último caminho conhecido). Um caminho que ainda existe vale como está.
func (a *Adapter) resolveNobre() string {
	a.nobreMu.Lock()
	defer a.nobreMu.Unlock()
	if cur := a.paths.NobreBin; cur != "" {
		if st, err := os.Stat(cur); err == nil && !st.IsDir() {
			return cur
		}
	}
	found := ""
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		sib := filepath.Join(filepath.Dir(exe), "nobre")
		if st, err := os.Stat(sib); err == nil && !st.IsDir() {
			found = resolveLinks(sib)
		}
	}
	if found == "" {
		path := os.Getenv("PATH")
		if a.paths.SearchPATH != nil {
			path = a.paths.SearchPATH()
		}
		if lp := launch.LookPathIn("nobre", path); lp != "" {
			if abs, err := filepath.Abs(lp); err == nil {
				found = resolveLinks(abs)
			}
		}
	}
	if found != "" {
		a.paths.NobreBin = found
	}
	return found
}

// NobreBinary é o nobre atual em disco ("" = não achou), para o monitor.
func (a *Adapter) NobreBinary() string { return a.resolveNobre() }

// currentPaths devolve uma cópia dos caminhos, com o nobre re-resolvido: o
// caminho antigo só vale se ainda existir. Sem nenhum nobre em disco, NobreBin
// vem vazio e quem instala falha com msgNoNobre em vez de gravar um caminho
// morto.
func (a *Adapter) currentPaths() Paths {
	found := a.resolveNobre()
	a.nobreMu.Lock()
	defer a.nobreMu.Unlock()
	p := a.paths
	p.NobreBin = found
	return p
}

// PlanInstall calcula o que Install mudaria, sem escrever nada.
func (a *Adapter) PlanInstall() (adapter.InstallReport, error) {
	p := a.currentPaths()
	if p.SettingsPath == "" || p.SkillsDir == "" {
		return adapter.InstallReport{}, errors.New(msgNoPaths)
	}
	if p.NobreBin == "" {
		return adapter.InstallReport{}, errors.New(msgNoNobre)
	}
	rep, err := PlanHooks(p.SettingsPath, p.NobreBin)
	if err != nil {
		return rep, err
	}
	need, err := PlanSkill(p.SkillsDir)
	if err != nil {
		return rep, err
	}
	if need {
		rep.Changed = append(rep.Changed, "skill")
		rep.Messages = append(rep.Messages, "Skill seria instalada em "+SkillFile(p.SkillsDir)+".")
	}
	return rep, nil
}

// Install instala hooks e skill com os caminhos do adaptador. A status line
// não entra: é a ação explícita InstallStatusLine.
func (a *Adapter) Install() (adapter.InstallReport, error) {
	p := a.currentPaths()
	if p.SettingsPath == "" || p.SkillsDir == "" {
		return adapter.InstallReport{}, errors.New(msgNoPaths)
	}
	if p.NobreBin == "" {
		return adapter.InstallReport{}, errors.New(msgNoNobre)
	}
	rep, _, err := installSettings(p.SettingsPath, p.NobreBin, time.Now(), true, false)
	if err != nil {
		return rep, err
	}
	need, err := PlanSkill(p.SkillsDir)
	if err != nil {
		return rep, err
	}
	if need {
		if err := InstallSkill(p.SkillsDir); err != nil {
			return rep, err
		}
		rep.Changed = append(rep.Changed, "skill")
		rep.Messages = append(rep.Messages, "Skill instalada em "+SkillFile(p.SkillsDir)+".")
	} else {
		rep.Messages = append(rep.Messages, "Skill já instalada; nada a alterar.")
	}
	return rep, nil
}

// InstallStatusLine integra a status line com os caminhos do adaptador.
func (a *Adapter) InstallStatusLine() (adapter.InstallReport, error) {
	p := a.currentPaths()
	if p.SettingsPath == "" {
		return adapter.InstallReport{}, errors.New(msgNoPaths)
	}
	if p.NobreBin == "" {
		return adapter.InstallReport{}, errors.New(msgNoNobre)
	}
	return InstallStatusLine(p.SettingsPath, p.NobreBin)
}

// UninstallStatusLine remove a integração da status line (devolve o original).
func (a *Adapter) UninstallStatusLine() (adapter.InstallReport, error) {
	if a.paths.SettingsPath == "" {
		return adapter.InstallReport{}, errors.New(msgNoPaths)
	}
	a.nobreMu.Lock()
	p := a.paths
	a.nobreMu.Unlock()
	return UninstallStatusLine(p.SettingsPath, p.NobreBin)
}
