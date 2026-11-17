package launch

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const pathMarker = "__NOBRE_PATH__"

// UserPATH descobre o PATH do usuário perguntando ao shell de login dele na
// hora de usar, em vez de confiar no PATH congelado na unidade do systemd
// (que guardaria o venv, o nvm ou o mise de quando o setup rodou).
//
// Só bash e zsh são consultados (-lic lê o perfil e o rc interativo). Em
// qualquer falha (shell desconhecido, sem marcador, erro, timeout) vale
// Fallback, e a falha não fica em cache. O diretório do nobre sempre entra.
type UserPATH struct {
	Shell    string        // $SHELL do usuário
	Fallback string        // PATH do daemon (o da unidade)
	NobreDir string        // diretório do nobre; entra se faltar
	Timeout  time.Duration // limite do shell; 0 = 3 s

	mu     sync.Mutex
	cached string
	at     time.Time
}

// Get devolve o PATH. maxAge=0 consulta o shell agora; senão aceita o
// resultado da última consulta bem-sucedida feita há menos de maxAge.
func (u *UserPATH) Get(maxAge time.Duration) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if maxAge > 0 && u.cached != "" && time.Since(u.at) < maxAge {
		return u.cached
	}
	login, ok := u.probe()
	if !ok {
		return PathWithDir(u.Fallback, u.NobreDir)
	}
	u.cached = PathWithDir(mergePATH(login, u.Fallback), u.NobreDir)
	u.at = time.Now()
	return u.cached
}

func (u *UserPATH) probe() (string, bool) {
	switch filepath.Base(u.Shell) {
	case "bash", "zsh":
	default:
		return "", false
	}
	timeout := u.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, u.Shell, "-lic", `printf '\n`+pathMarker+`%s\n' "$PATH"`)
	cmd.Stdin = nil // /dev/null
	cmd.Stderr = nil
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	// Sessão própria: o timeout mata o grupo inteiro, inclusive filhos que o
	// rc deixou em segundo plano segurando o stdout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	var last string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), pathMarker); ok {
			last = v
		}
	}
	last = strings.TrimSpace(last)
	if !strings.Contains(last, "/") {
		return "", false
	}
	return last, true
}

// mergePATH põe os diretórios de first e depois os de second que ainda não
// apareceram; entradas vazias e relativas saem.
func mergePATH(first, second string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range []string{first, second} {
		for _, d := range strings.Split(p, ":") {
			if !filepath.IsAbs(d) || seen[d] {
				continue
			}
			seen[d] = true
			out = append(out, d)
		}
	}
	return strings.Join(out, ":")
}

// LookPathIn acha name nos diretórios absolutos de path (sem olhar o PATH do
// processo). Devolve o caminho sem seguir symlinks, ou "".
func LookPathIn(name, path string) string {
	for _, d := range strings.Split(path, ":") {
		if !filepath.IsAbs(d) {
			continue
		}
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// LoginShell devolve o shell de login do usuário: $SHELL (o systemd do
// usuário o define) ou, sem ele, o campo do /etc/passwd. "" = não achou.
func LoginShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	uid := strconv.Itoa(os.Getuid())
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) == 7 && f[2] == uid {
			return f[6]
		}
	}
	return ""
}
