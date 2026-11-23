package kiro

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

// hookEvents são os eventos do kiro-cli que o Cockpit escuta.
var hookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "SessionEnd", "PostCompact"}

const (
	hookTimeout = 10
	hookPrefix  = "nobreai-cockpit-"
	msgNoPaths  = "caminhos não configurados"
	msgNoNobre  = "não achei o binário nobre (coloque-o ao lado do nobred ou no PATH)"
)

// steeringContent é o steering embutido: as instruções do nobre para o Kiro.
//
//go:embed steering.md
var steeringContent string

type hookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type hookEntry struct {
	Name    string     `json:"name"`
	Trigger string     `json:"trigger"`
	Matcher string     `json:"matcher,omitempty"`
	Action  hookAction `json:"action"`
	Timeout int        `json:"timeout"`
}

type hooksFile struct {
	Version string      `json:"version"`
	Hooks   []hookEntry `json:"hooks"`
}

// quoteBin devolve o caminho do nobre pronto para o shell: como está, ou entre
// aspas simples quando tem caracteres especiais.
func quoteBin(b string) string {
	if b != "" && strings.Trim(b, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_/.+-:@,%=~") == "" {
		return b
	}
	return "'" + strings.ReplaceAll(b, "'", `'\''`) + "'"
}

// unquoteBin desfaz quoteBin.
func unquoteBin(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	}
	return s
}

// hooksContent é o conteúdo do arquivo de hooks para o nobre nobreBin.
func hooksContent(nobreBin string) []byte {
	f := hooksFile{Version: "v1"}
	for _, ev := range hookEvents {
		f.Hooks = append(f.Hooks, hookEntry{
			Name: hookPrefix + ev, Trigger: ev,
			Action:  hookAction{Type: "command", Command: quoteBin(nobreBin) + " hook kiro " + ev},
			Timeout: hookTimeout,
		})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(f)
	return buf.Bytes()
}

// inspectHooks lê o arquivo de hooks: eventos nossos que faltam, se há
// entradas a mais ou repetidas e os binários nobre citados (sem repetição).
func inspectHooks(raw []byte) (missing []string, extra bool, bins []string, err error) {
	var f hooksFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, false, nil, err
	}
	seen := map[string]bool{}
	seenBin := map[string]bool{}
	for _, h := range f.Hooks {
		bin, ev, ok := strings.Cut(h.Action.Command, " hook kiro ")
		if !ok || ev != h.Trigger || h.Name != hookPrefix+ev || seen[ev] || !isEvent(ev) {
			extra = true
			continue
		}
		seen[ev] = true
		if b := unquoteBin(bin); !seenBin[b] {
			seenBin[b] = true
			bins = append(bins, b)
		}
	}
	for _, ev := range hookEvents {
		if !seen[ev] {
			missing = append(missing, ev)
		}
	}
	return missing, extra, bins, nil
}

func isEvent(ev string) bool {
	for _, e := range hookEvents {
		if e == ev {
			return true
		}
	}
	return false
}

// resolveNobre acha o nobre agora: um caminho que ainda existe vale como está;
// senão procura de novo (o de quando o daemon subiu pode ter sumido num make
// install). "" = não achou.
func (a *Adapter) resolveNobre() string {
	a.nobreMu.Lock()
	defer a.nobreMu.Unlock()
	if cur := a.paths.NobreBin; cur != "" {
		if st, err := os.Stat(cur); err == nil && !st.IsDir() {
			return cur
		}
	}
	if found := findNobre(a.paths.SearchPATH); found != "" {
		a.paths.NobreBin = found
		return found
	}
	return ""
}

// NobreBinary é o nobre atual em disco ("" = não achou), para o monitor.
func (a *Adapter) NobreBinary() string { return a.resolveNobre() }

func (a *Adapter) installPaths() (Paths, error) {
	nobre := a.resolveNobre()
	a.nobreMu.Lock()
	p := a.paths
	a.nobreMu.Unlock()
	if p.HooksFile == "" || p.SteeringFile == "" {
		return p, errors.New(msgNoPaths)
	}
	if nobre == "" {
		return p, errors.New(msgNoNobre)
	}
	p.NobreBin = nobre
	return p, nil
}

// fileNeeds diz se gravar want em path mudaria algo.
func fileNeeds(path string, want []byte) (bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !bytes.Equal(b, want), nil
}

// PlanInstall calcula o que Install mudaria, sem escrever nada.
func (a *Adapter) PlanInstall() (adapter.InstallReport, error) { return a.install(false) }

// Install grava os dois arquivos inteiros do Cockpit no ~/.kiro: o de hooks e
// o steering. Nada de arquivos do usuário é editado; sem mudança, nada é
// regravado.
func (a *Adapter) Install() (adapter.InstallReport, error) { return a.install(true) }

func (a *Adapter) install(write bool) (adapter.InstallReport, error) {
	rep := adapter.InstallReport{Changed: []string{}, Messages: []string{}}
	p, err := a.installPaths()
	if err != nil {
		return rep, err
	}
	for _, f := range []struct {
		key, label, path string
		want             []byte
	}{
		{"hooks", "Hooks do Kiro", p.HooksFile, hooksContent(p.NobreBin)},
		{"steering", "Steering do Kiro", p.SteeringFile, []byte(steeringContent)},
	} {
		need, err := fileNeeds(f.path, f.want)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", f.label, err)
		}
		switch {
		case !need:
			rep.Messages = append(rep.Messages, f.label+" já instalados em "+f.path+"; nada a alterar.")
		case !write:
			rep.Changed = append(rep.Changed, f.key)
			rep.Messages = append(rep.Messages, f.label+" seriam gravados em "+f.path+".")
		default:
			if err := writeFileAtomic(f.path, f.want); err != nil {
				return rep, fmt.Errorf("%s: %w", f.label, err)
			}
			rep.Changed = append(rep.Changed, f.key)
			rep.Messages = append(rep.Messages, f.label+" gravados em "+f.path+".")
		}
	}
	return rep, nil
}

// SetupInstall é o passo do Kiro no `nobre setup` (adapter.SetupInstaller):
// grava os hooks com o nobre dado e o steering, ou, com dry, só diz o que
// gravaria. Sem kiro-cli na máquina, avisa numa linha e não grava nada.
func (a *Adapter) SetupInstall(nobre string, dry bool) (adapter.InstallReport, error) {
	if nobre != "" {
		a.nobreMu.Lock()
		a.paths.NobreBin = nobre
		a.nobreMu.Unlock()
	}
	if a.Binary() == "" {
		return adapter.InstallReport{Changed: []string{},
			Messages: []string{"Kiro não encontrado (kiro-cli fora do PATH e de ~/.local/bin); pulei"}}, nil
	}
	return a.install(!dry)
}

// SetupUninstall é o passo do Kiro no `nobre setup --remover`
// (adapter.SetupUninstaller): o mesmo que Uninstall.
func (a *Adapter) SetupUninstall(dry bool) (adapter.InstallReport, error) { return a.Uninstall(dry) }

// Uninstall apaga os dois arquivos do Cockpit (e só eles). dry só informa.
func (a *Adapter) Uninstall(dry bool) (adapter.InstallReport, error) {
	rep := adapter.InstallReport{Changed: []string{}, Messages: []string{}}
	a.nobreMu.Lock()
	p := a.paths
	a.nobreMu.Unlock()
	if p.HooksFile == "" || p.SteeringFile == "" {
		return rep, errors.New(msgNoPaths)
	}
	for _, f := range []struct{ key, label, path string }{
		{"hooks", "Hooks do Kiro", p.HooksFile},
		{"steering", "Steering do Kiro", p.SteeringFile},
	} {
		if _, err := os.Lstat(f.path); errors.Is(err, os.ErrNotExist) {
			rep.Messages = append(rep.Messages, f.label+" não instalados; nada a remover.")
			continue
		} else if err != nil {
			return rep, fmt.Errorf("%s: %w", f.label, err)
		}
		if dry {
			rep.Changed = append(rep.Changed, f.key)
			rep.Messages = append(rep.Messages, "Removeria "+f.path+".")
			continue
		}
		if err := os.Remove(f.path); err != nil {
			return rep, fmt.Errorf("%s: %w", f.label, err)
		}
		rep.Changed = append(rep.Changed, f.key)
		rep.Messages = append(rep.Messages, "Removido: "+f.path+".")
	}
	return rep, nil
}

// writeFileAtomic escreve em path (criando as pastas) por arquivo temporário
// e rename; se path for um link simbólico, escreve no destino.
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
