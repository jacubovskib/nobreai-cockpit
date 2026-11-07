// Package gitinfo consulta o estado do git de um diretório.
package gitinfo

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var hexSHA = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// GitInfo consulta e altera o estado do git de um diretório. As consultas
// devolvem "" (ou false) quando o diretório não é um repositório.
type GitInfo interface {
	Branch(dir string) string
	Head(dir string) string
	DiffStat(dir, since string) string
	Clean(dir string) bool
	Checkout(dir, branch string) error
}

// Exec implementa GitInfo executando o binário git.
type Exec struct{}

var _ GitInfo = Exec{}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Branch devolve o branch atual.
func (Exec) Branch(dir string) string {
	out, _ := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	return out
}

// Head devolve o hash do commit atual.
func (Exec) Head(dir string) string {
	out, _ := git(dir, "rev-parse", "HEAD")
	return out
}

// DiffStat resume as mudanças da árvore de trabalho desde o commit since
// (HEAD se vazio). since precisa ser um hash hexadecimal; outro valor
// (inclusive opção ou nome de branch) devolve "".
func (Exec) DiffStat(dir, since string) string {
	if since == "" {
		since = "HEAD"
	} else if !hexSHA.MatchString(since) {
		return ""
	}
	out, _ := git(dir, "diff", "--stat", since, "--")
	return out
}

// Clean diz se a árvore de trabalho não tem mudanças (nem arquivos novos).
func (Exec) Clean(dir string) bool {
	out, err := git(dir, "status", "--porcelain")
	return err == nil && out == ""
}

// Checkout troca para o branch. O nome precisa ser um branch válido para o
// git e não pode começar com "-" (seria lido como opção).
func (Exec) Checkout(dir, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "@") {
		return fmt.Errorf("nome de branch inválido: %q", branch)
	}
	if _, err := git(dir, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("nome de branch inválido: %q", branch)
	}
	_, err := git(dir, "checkout", "-q", branch, "--")
	return err
}
