// Package rtk embrulha tudo o que toca o binário do RTK (Rust Token Killer) e
// o settings.json do Claude Code: achar o binário, saber se o hook está ativo,
// ativar/desativar com cópia de segurança e ler o ganho de tokens.
// O Cockpit nunca baixa nem instala o RTK; só roda o que já está instalado.
package rtk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

const (
	kvActivations = "rtk_activations"
	hookCommand   = "rtk hook claude"
	backupPrefix  = "settings.json.bak-rtk-"
	keepBackups   = 3
	gainTTL       = 60 * time.Second
	shortTimeout  = 5 * time.Second
	initTimeout   = 20 * time.Second
)

// KV é o armazenamento chave-valor (config do store).
type KV interface {
	GetKV(key string, v any) (bool, error)
	PutKV(key string, v any) error
}

// Interval é um período em que o hook esteve ativo.
type Interval struct {
	On  time.Time `json:"on"`
	Off time.Time `json:"off"` // zero = ativo
}

// Status é o retrato do RTK neste momento.
type Status struct {
	Installed   bool       `json:"installed"`
	Path        string     `json:"path"`
	Version     string     `json:"version"` // "1.2.0" (sem o prefixo "rtk ")
	Active      bool       `json:"active"`  // hook no settings.json
	Detail      string     `json:"detail"`  // motivo quando inativo/ilegível
	Activations []Interval `json:"activations"`
}

// GainDay é a linha diária de `rtk gain --daily --format json`.
type GainDay struct {
	Date         string `json:"date"`
	Commands     int64  `json:"commands"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	SavedTokens  int64  `json:"saved_tokens"`
}

// Tool reúne as dependências; os testes injetam um rtk falso e arquivos temporários.
type Tool struct {
	SettingsPath string        // ~/.claude/settings.json
	Home         string        // para os fallbacks ~/.cargo/bin e ~/.local/bin
	SearchPATH   func() string // nil = PATH do processo
	Store        KV
	Now          func() time.Time
	// Run executa o binário; nil = os/exec com timeout.
	Run func(ctx context.Context, bin string, args ...string) ([]byte, error)

	mu     sync.Mutex // protege o cache do gain e o KV de intervalos
	gain   []GainDay
	gainAt time.Time
	gainOK bool
}

func (t *Tool) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Find devolve o caminho do rtk: PATH do usuário, ~/.cargo/bin, ~/.local/bin.
// Vazio quando não está instalado.
func (t *Tool) Find() string {
	path := os.Getenv("PATH")
	if t.SearchPATH != nil {
		path = t.SearchPATH()
	}
	for _, d := range filepath.SplitList(path) {
		if d == "" {
			continue
		}
		if p := filepath.Join(d, "rtk"); executable(p) {
			return p
		}
	}
	if t.Home != "" {
		for _, rel := range []string{".cargo/bin/rtk", ".local/bin/rtk"} {
			if p := filepath.Join(t.Home, rel); executable(p) {
				return p
			}
		}
	}
	return ""
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

// run executa o rtk, pelo Run injetado ou por os/exec.
func (t *Tool) run(bin string, args ...string) ([]byte, error) {
	if t.Run != nil {
		return t.Run(context.Background(), bin, args...)
	}
	timeout := shortTimeout
	if len(args) > 0 && args[0] == "init" && !contains(args, "--dry-run") {
		timeout = initTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()
	if t.Home != "" {
		cmd.Env = append(cmd.Env, "HOME="+t.Home)
	}
	if t.SearchPATH != nil {
		cmd.Env = append(cmd.Env, "PATH="+t.SearchPATH())
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// hookState lê o settings.json: ativo, motivo, código do Check e erro.
type hookState struct {
	active bool
	detail string
	code   string // "", "missing", "unreadable", "no_settings"
}

func (t *Tool) hook() hookState {
	raw, err := os.ReadFile(t.SettingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return hookState{detail: "settings.json do Claude Code não existe", code: "no_settings"}
		}
		return hookState{detail: "não consegui ler o settings.json: " + err.Error(), code: "unreadable"}
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return hookState{detail: "settings.json ilegível: " + err.Error(), code: "unreadable"}
	}
	hooks, _ := doc["hooks"].(map[string]any)
	entries, _ := hooks["PreToolUse"].([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		matcher, _ := entry["matcher"].(string)
		if !matchesBash(matcher) {
			continue
		}
		cmds, _ := entry["hooks"].([]any)
		for _, c := range cmds {
			m, _ := c.(map[string]any)
			if cmd, _ := m["command"].(string); strings.Contains(cmd, hookCommand) {
				return hookState{active: true}
			}
		}
	}
	return hookState{detail: "hook do RTK não está no settings.json", code: "missing"}
}

// matchesBash diz se o matcher do hook casa a ferramenta Bash.
func matchesBash(m string) bool {
	if m == "" || m == "*" {
		return true
	}
	for _, alt := range strings.Split(m, "|") {
		if strings.TrimSpace(alt) == "Bash" {
			return true
		}
	}
	return false
}

// Status devolve o retrato atual; withVersion roda `rtk --version`.
func (t *Tool) Status(withVersion bool) Status {
	s := Status{Path: t.Find()}
	s.Installed = s.Path != ""
	if s.Installed && withVersion {
		if out, err := t.run(s.Path, "--version"); err == nil {
			s.Version = strings.TrimPrefix(strings.TrimSpace(string(out)), "rtk ")
		}
	}
	h := t.hook()
	s.Active, s.Detail = h.active, h.detail
	s.Activations, _ = t.Activations()
	return s
}

// Check é o item do checklist de integrações. Se o hook está ativo sem
// intervalo aberto (ativação manual), abre um; só o Apply fecha.
func (t *Tool) Check() adapter.Check {
	c := adapter.Check{Key: "rtk", Label: "RTK ativo no Claude Code"}
	bin := t.Find()
	h := t.hook()
	c.State = bin + "|" + h.code + "|" + fmt.Sprint(h.active)
	switch {
	case h.code == "no_settings" || h.code == "unreadable":
		c.Code, c.Detail = h.code, h.detail
	case !h.active:
		c.Code = "missing"
		c.Detail = "hook do RTK não está ativo"
		if bin == "" {
			c.Detail = "RTK não instalado"
		}
	case bin == "":
		c.Code, c.Detail = "dead_binary", "hook ativo, mas o binário rtk não foi encontrado"
		t.openIfNone()
	default:
		c.OK = true
		c.Detail = bin
		t.openIfNone()
	}
	return c
}

// openIfNone abre um intervalo quando o hook está ativo e nenhum está aberto.
func (t *Tool) openIfNone() {
	t.mu.Lock()
	defer t.mu.Unlock()
	iv, err := t.loadLocked()
	if err != nil {
		return
	}
	if n := len(iv); n > 0 && iv[n-1].Off.IsZero() {
		return
	}
	_ = t.saveLocked(append(iv, Interval{On: t.now()}))
}

func (t *Tool) loadLocked() ([]Interval, error) {
	if t.Store == nil {
		return nil, nil
	}
	var iv []Interval
	if _, err := t.Store.GetKV(kvActivations, &iv); err != nil {
		return nil, err
	}
	return iv, nil
}

func (t *Tool) saveLocked(iv []Interval) error {
	if t.Store == nil {
		return nil
	}
	return t.Store.PutKV(kvActivations, iv)
}

// Activations devolve os intervalos de ativação registrados.
func (t *Tool) Activations() ([]Interval, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loadLocked()
}

func initArgs(action string) ([]string, error) {
	switch action {
	case "activate":
		return []string{"init", "-g", "--hook-only", "--auto-patch"}, nil
	case "deactivate":
		return []string{"init", "-g", "--uninstall"}, nil
	}
	return nil, fmt.Errorf("ação inválida: %q", action)
}

// Preview roda o init com --dry-run e devolve o que ele faria.
func (t *Tool) Preview(action string) (string, error) {
	args, err := initArgs(action)
	if err != nil {
		return "", err
	}
	bin := t.Find()
	if bin == "" {
		return "", errors.New("RTK não instalado")
	}
	out, err := t.run(bin, append(args, "--dry-run")...)
	if err != nil {
		return string(out), fmt.Errorf("rtk falhou: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Apply ativa ou desativa o hook: copia o settings.json (guarda as 3 cópias
// mais recentes), roda o init e atualiza os intervalos. Não restaura sozinho.
func (t *Tool) Apply(action string) (out string, backup string, err error) {
	args, err := initArgs(action)
	if err != nil {
		return "", "", err
	}
	bin := t.Find()
	if bin == "" {
		return "", "", errors.New("RTK não instalado")
	}
	if _, serr := os.Stat(t.SettingsPath); serr != nil {
		if action == "activate" || !errors.Is(serr, os.ErrNotExist) {
			return "", "", fmt.Errorf("settings.json indisponível: %v", serr)
		}
	} else if backup, err = t.backup(); err != nil {
		return "", "", fmt.Errorf("não consegui copiar o settings.json: %v", err)
	}
	raw, rerr := t.run(bin, args...)
	out = string(raw)
	if rerr != nil {
		return out, backup, fmt.Errorf("rtk falhou: %v: %s", rerr, strings.TrimSpace(out))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	iv, lerr := t.loadLocked()
	if lerr != nil {
		return out, backup, lerr
	}
	open := len(iv) > 0 && iv[len(iv)-1].Off.IsZero()
	switch {
	case action == "activate" && !open:
		iv = append(iv, Interval{On: t.now()})
	case action == "deactivate" && open:
		iv[len(iv)-1].Off = t.now()
	default:
		return out, backup, nil
	}
	return out, backup, t.saveLocked(iv)
}

// backup copia o settings.json para settings.json.bak-rtk-<AAAAMMDD-HHMMSS>
// e apaga as cópias além das 3 mais recentes.
func (t *Tool) backup() (string, error) {
	data, err := os.ReadFile(t.SettingsPath)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(t.SettingsPath)
	dst := filepath.Join(dir, backupPrefix+t.now().Format("20060102-150405"))
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", err
	}
	olds, _ := filepath.Glob(filepath.Join(dir, backupPrefix+"*"))
	sort.Strings(olds) // o carimbo ordena cronologicamente
	for len(olds) > keepBackups {
		_ = os.Remove(olds[0])
		olds = olds[1:]
	}
	return dst, nil
}

// Gain devolve os dias de `rtk gain --daily --format json`, com cache de 60 s.
func (t *Tool) Gain() ([]GainDay, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if t.gainOK && now.Sub(t.gainAt) < gainTTL && now.Sub(t.gainAt) >= 0 {
		return t.gain, nil
	}
	bin := t.Find()
	if bin == "" {
		return nil, errors.New("RTK não instalado")
	}
	out, err := t.run(bin, "gain", "--daily", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("rtk gain indisponível: %v", err)
	}
	var doc struct {
		Daily []GainDay `json:"daily"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("saída do rtk gain inválida: %v", err)
	}
	t.gain, t.gainAt, t.gainOK = doc.Daily, now, true
	return t.gain, nil
}
