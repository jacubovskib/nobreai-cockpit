package kiro

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
)

// Paths são os caminhos que o adaptador lê e escreve. Tudo é injetável: os
// testes apontam para diretórios temporários e nunca para o ~/.kiro real.
type Paths struct {
	HooksFile    string // ~/.kiro/hooks/nobreai-cockpit.json
	SteeringFile string // ~/.kiro/steering/nobreai-cockpit.md
	SessionsDir  string // ~/.kiro/sessions: onde o kiro-cli grava o histórico das sessões
	NobreBin     string // caminho absoluto do nobre, gravado nos hooks
	KiroBin      string // kiro-cli; vazio = procurar no PATH e em ~/.local/bin
	// SearchPATH devolve o PATH onde procurar o CLI; nil = o PATH do processo.
	SearchPATH func() string
	// Version devolve a saída de `kiro-cli --version`; nil = não informar.
	Version func(bin string) string
}

// DefaultPaths usa o diretório do usuário que roda o processo. O nobre fica ao
// lado do executável atual ou no PATH.
func DefaultPaths() Paths {
	p := Paths{Version: cliVersion}
	if home, err := os.UserHomeDir(); err == nil {
		p.HooksFile = filepath.Join(home, ".kiro", "hooks", "nobreai-cockpit.json")
		p.SteeringFile = filepath.Join(home, ".kiro", "steering", "nobreai-cockpit.md")
		p.SessionsDir = filepath.Join(home, ".kiro", "sessions")
	}
	p.NobreBin = findNobre(nil)
	return p
}

// findNobre acha o nobre ao lado do executável atual e, depois, no PATH
// (searchPATH nil = o do processo). "" = não achou.
func findNobre(searchPATH func() string) string {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		sib := filepath.Join(filepath.Dir(exe), "nobre")
		if st, err := os.Stat(sib); err == nil && !st.IsDir() {
			return resolveLinks(sib)
		}
	}
	path := os.Getenv("PATH")
	if searchPATH != nil {
		path = searchPATH()
	}
	if lp := launch.LookPathIn("nobre", path); lp != "" {
		if abs, err := filepath.Abs(lp); err == nil {
			return resolveLinks(abs)
		}
	}
	return ""
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
func (a *Adapter) SetPaths(p Paths) {
	a.nobreMu.Lock()
	defer a.nobreMu.Unlock()
	a.paths = p
}

// Binary é o kiro-cli: Paths.KiroBin, se definido, vale sozinho; senão o PATH
// e, por fim, ~/.local/bin/kiro-cli. Sem resolver symlinks: o nome do
// processo tem de continuar sendo kiro-cli. "" = não encontrado.
func (a *Adapter) Binary() string {
	a.nobreMu.Lock()
	p := a.paths
	a.nobreMu.Unlock()
	if p.KiroBin != "" {
		if st, err := os.Stat(p.KiroBin); err == nil && !st.IsDir() {
			return p.KiroBin
		}
		return ""
	}
	path := os.Getenv("PATH")
	if p.SearchPATH != nil {
		path = p.SearchPATH()
	}
	if lp := launch.LookPathIn("kiro-cli", path); lp != "" {
		if abs, err := filepath.Abs(lp); err == nil {
			return abs
		}
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	lb := filepath.Join(home, ".local", "bin", "kiro-cli")
	if st, err := os.Stat(lb); err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0 {
		return lb
	}
	return ""
}

// minVersion é o primeiro kiro-cli com o harness V3.
var minVersion = [3]int{3, 1, 0}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// versionOutdated diz se a saída de --version traz uma versão abaixo de
// minVersion. Saída sem versão reconhecível não conta como defasada.
func versionOutdated(out string) bool {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		n, _ := strconv.Atoi(m[i+1])
		if n != minVersion[i] {
			return n < minVersion[i]
		}
	}
	return false
}

func binaryExists(b string) bool {
	if !strings.Contains(b, "/") {
		_, err := exec.LookPath(b)
		return err == nil
	}
	st, err := os.Stat(b)
	return err == nil && !st.IsDir()
}

// Detect monta o checklist de saúde do Kiro: binário (com versão), hooks,
// steering e último evento recebido.
func (a *Adapter) Detect() adapter.Detection {
	d := adapter.Detection{Checks: []adapter.Check{}}
	bin := a.Binary()
	d.Found = bin != ""
	d.Path = bin
	a.nobreMu.Lock()
	version := a.paths.Version
	a.nobreMu.Unlock()
	if d.Found && version != nil {
		d.Version = version(bin)
	}
	cs := a.checks(bin, d.Version)
	last := adapter.Check{Key: "last_event", Label: "Recebendo eventos", Detail: "nenhum evento recebido ainda"}
	if t, ok := a.lastEvent(); ok {
		last.OK = true
		last.Detail = "último " + ago(a.now().Sub(t))
	}
	d.Checks = append(cs, last)
	return d
}

// Health é o checklist barato do monitor: sem consultar a versão (que executa
// um processo) e sem os eventos.
func (a *Adapter) Health() []adapter.Check { return a.checks(a.Binary(), "") }

// WatchedFiles são os arquivos cuja mudança pede uma conferência completa.
func (a *Adapter) WatchedFiles() []string {
	a.nobreMu.Lock()
	p := a.paths
	a.nobreMu.Unlock()
	var fs []string
	for _, f := range []string{p.HooksFile, p.SteeringFile, p.NobreBin} {
		if f != "" {
			fs = append(fs, f)
		}
	}
	return fs
}

func (a *Adapter) checks(bin, version string) []adapter.Check {
	bc := adapter.Check{Key: "binary", Label: "kiro-cli encontrado", State: bin}
	switch {
	case bin == "":
		bc.Code = "not_found"
		bc.Detail = "kiro-cli não está no PATH nem em ~/.local/bin"
	case versionOutdated(version):
		bc.Code = "outdated"
		bc.Detail = strings.TrimSpace(bin+" "+version) + " (o Cockpit precisa do kiro-cli 3.1 ou mais novo)"
	default:
		bc.OK = true
		bc.Detail = strings.TrimSpace(bin + " " + version)
	}

	a.nobreMu.Lock()
	p := a.paths
	a.nobreMu.Unlock()
	return []adapter.Check{bc, a.hooksCheck(p), a.steeringCheck(p)}
}

func (a *Adapter) hooksCheck(p Paths) adapter.Check {
	c := adapter.Check{Key: "hooks", Label: "Hooks instalados"}
	b, err := os.ReadFile(p.HooksFile)
	if errors.Is(err, os.ErrNotExist) {
		c.Code = "missing"
		c.Detail = "não encontrados em " + p.HooksFile
		c.State = "missing"
		return c
	}
	if err != nil {
		c.Code = "unreadable"
		c.Detail = err.Error()
		return c
	}
	missing, extra, bins, err := inspectHooks(b)
	c.State = strings.Join(bins, ",")
	switch {
	case err != nil:
		c.Code = "unreadable"
		c.Detail = p.HooksFile + " não é um JSON válido de hooks: " + err.Error()
		return c
	case len(missing) > 0:
		c.Code = "missing"
		c.Detail = "faltam: " + strings.Join(missing, ", ")
		return c
	case extra:
		c.Code = "outdated"
		c.Detail = "o arquivo tem entradas a mais ou repetidas (desatualizado: rode nobre setup)"
		return c
	}
	cur := a.resolveNobre()
	for _, bin := range bins {
		switch {
		case cur != "" && bin != cur:
			c.Code = "outdated"
			c.Detail = "aponta para outro nobre: " + bin + " (atual: " + cur + ")"
			return c
		case !binaryExists(bin):
			c.Code = "dead_binary"
			c.Detail = "aponta para um binário que não existe: " + bin
			return c
		}
	}
	c.OK = true
	c.Detail = strings.Join(hookEvents, ", ") + " (arquivo conferido)"
	return c
}

func (a *Adapter) steeringCheck(p Paths) adapter.Check {
	c := adapter.Check{Key: "steering", Label: "Steering do Cockpit instalado", State: "missing"}
	if st, err := os.Stat(p.SteeringFile); err == nil {
		c.State = fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
	}
	b, err := os.ReadFile(p.SteeringFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.Code = "missing"
		c.Detail = "não encontrado em " + p.SteeringFile
	case err != nil:
		c.Code = "unreadable"
		c.Detail = err.Error()
	case string(b) != steeringContent:
		c.Code = "outdated"
		c.Detail = p.SteeringFile + " (desatualizado: rode nobre setup)"
	default:
		c.OK = true
		c.Detail = p.SteeringFile
	}
	return c
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
		if s.Agent == "kiro" && s.LastEventAt.After(last) {
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
