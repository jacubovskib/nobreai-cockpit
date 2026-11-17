package launch

import (
	"bytes"
	"context"
	"errors"
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
// Só bash e zsh são consultados (-lic lê o perfil e o rc interativo). Uma
// consulta que falha (shell desconhecido, sem marcador, erro, timeout) fica
// guardada por FailTTL e devolve o último PATH bom, ou Fallback se nunca
// houve um. O diretório do nobre sempre entra. Chamadas simultâneas esperam
// uma única consulta, sem prender quem só quer o cache.
type UserPATH struct {
	Shell    string        // $SHELL do usuário
	Fallback string        // PATH do daemon (o da unidade)
	NobreDir string        // diretório do nobre; entra se faltar
	Timeout  time.Duration // limite do shell; 0 = 3 s
	FailTTL  time.Duration // quanto uma falha fica guardada; 0 = 30 s
	// OnChange recebe o PATH novo quando ele muda (o daemon atualiza o
	// próprio PATH, que o git, o terminal e o claude --version usam).
	OnChange func(path string)
	// Logf registra as falhas da consulta (nil = não registra).
	Logf func(format string, args ...any)

	mu       sync.Mutex
	good     string // último PATH bom
	goodAt   time.Time
	failedAt time.Time
	inflight chan struct{}
}

// Get devolve o PATH. maxAge=0 consulta o shell agora (salvo falha recente);
// senão aceita o resultado bom de até maxAge.
func (u *UserPATH) Get(maxAge time.Duration) string {
	for {
		u.mu.Lock()
		switch {
		case maxAge > 0 && u.good != "" && time.Since(u.goodAt) < maxAge:
			p := u.good
			u.mu.Unlock()
			return p
		case !u.failedAt.IsZero() && time.Since(u.failedAt) < u.failTTL():
			p := u.current()
			u.mu.Unlock()
			return p
		case u.inflight != nil:
			ch := u.inflight
			u.mu.Unlock()
			<-ch
			u.mu.Lock()
			p := u.current()
			u.mu.Unlock()
			return p
		}
		ch := make(chan struct{})
		u.inflight = ch
		u.mu.Unlock()

		login, ok, why := u.probe()

		u.mu.Lock()
		var changed string
		if ok {
			p := PathWithDir(mergePATH(login, u.Fallback), u.NobreDir)
			if p != u.good {
				changed = p
			}
			u.good, u.goodAt, u.failedAt = p, time.Now(), time.Time{}
		} else {
			if u.failedAt.IsZero() && u.Logf != nil && why != "" {
				u.Logf("PATH do shell de login (%s): %s; usando o PATH de reserva", u.Shell, why)
			}
			u.failedAt = time.Now()
		}
		p := u.current()
		u.inflight = nil
		u.mu.Unlock()
		close(ch)
		if changed != "" && u.OnChange != nil {
			u.OnChange(changed)
		}
		return p
	}
}

// current: o último PATH bom, ou o de reserva. Chamado com mu preso.
func (u *UserPATH) current() string {
	if u.good != "" {
		return u.good
	}
	return PathWithDir(u.Fallback, u.NobreDir)
}

func (u *UserPATH) failTTL() time.Duration {
	if u.FailTTL > 0 {
		return u.FailTTL
	}
	return 30 * time.Second
}

// probe roda o shell e devolve o PATH dele. why explica a falha ("" quando o
// shell não é consultável, o que não é erro).
func (u *UserPATH) probe() (path string, ok bool, why string) {
	switch filepath.Base(u.Shell) {
	case "bash", "zsh":
	default:
		return "", false, ""
	}
	timeout := u.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, u.Shell, "-lic", `printf '\n`+pathMarker+`%s\n' "$PATH"`)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stdin = nil  // /dev/null
	cmd.Stderr = nil // /dev/null: um filho do rc com o stderr herdado não segura pipe
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	// Sessão própria: o grupo inteiro morre no fim, inclusive filhos que o rc
	// deixou em segundo plano segurando o stdout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 300 * time.Millisecond
	err := cmd.Run()
	if cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// ErrWaitDelay: o shell saiu bem, mas um filho dele segurava o stdout; o
	// que ele escreveu antes de sair vale.
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return "", false, err.Error()
	}
	var last string
	for _, line := range strings.Split(out.String(), "\n") {
		if v, ok := strings.CutPrefix(line, pathMarker); ok {
			last = v
		}
	}
	last = strings.TrimSpace(last)
	if !strings.Contains(last, "/") {
		return "", false, "a saída do shell não trouxe o PATH"
	}
	return last, true, ""
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
