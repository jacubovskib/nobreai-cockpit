package gitinfo

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// WorktreeRemover são as únicas escritas do git do Cockpit sobre worktrees:
// explícitas, pedidas pelo usuário e nunca destrutivas além do necessário
// (o branch só sai se ainda estiver na ponta verificada; nunca branch -D).
// Fica fora de GitInfo pelo mesmo motivo de WorktreeReader.
type WorktreeRemover interface {
	// RemoveWorktree roda git -c status.showUntrackedFiles=normal worktree
	// remove [--force] -- path. Sem force, worktree com submódulos devolve
	// ErrSubmodules.
	RemoveWorktree(repo, path string, force bool) error
	PruneWorktrees(repo string) error // git worktree prune
	// DeleteBranchAt roda git update-ref -d -- refs/heads/<branch> <tip>: só
	// apaga se o branch ainda aponta para tip (sha completo).
	DeleteBranchAt(repo, branch, tip string) error
}

var _ WorktreeRemover = Exec{}

// ErrSubmodules: o git não remove sem --force um worktree com submódulos.
var ErrSubmodules = errors.New("o worktree tem submódulos")

// fullSHA aceita só o sha completo em hex minúsculo (SHA-1 ou SHA-256).
var fullSHA = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// gitWrite roda um git que escreve. Devolve a saída completa do git (em C,
// para dar para reconhecer as mensagens) e um erro de uma linha só.
func gitWrite(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), errors.New(shortGitMsg(string(out), err))
	}
	return string(out), nil
}

// shortGitMsg tira da saída do git a primeira linha útil, sem o prefixo
// fatal:/error: e sem as dicas (hint:) de várias linhas.
func shortGitMsg(out string, err error) string {
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "hint:") {
			continue
		}
		for _, p := range []string{"fatal: ", "error: "} {
			line = strings.TrimPrefix(line, p)
		}
		return line
	}
	return err.Error()
}

// RemoveWorktree roda git -c status.showUntrackedFiles=normal worktree remove
// [--force] -- path em repo: a checagem de limpeza do próprio git vê arquivos
// novos mesmo com status.showUntrackedFiles=no na configuração.
func (Exec) RemoveWorktree(repo, path string, force bool) error {
	if path == "" || strings.HasPrefix(path, "-") {
		return errors.New("caminho inválido")
	}
	args := []string{"-c", "status.showUntrackedFiles=normal", "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	out, err := gitWrite(repo, append(args, "--", path)...)
	if err != nil && !force && strings.Contains(out, "containing submodules") {
		return ErrSubmodules
	}
	return err
}

// PruneWorktrees roda git worktree prune em repo.
func (Exec) PruneWorktrees(repo string) error {
	_, err := gitWrite(repo, "worktree", "prune")
	return err
}

// DeleteBranchAt apaga refs/heads/<branch> só se ele ainda aponta para tip
// (compare-and-delete do update-ref). Nunca usa branch -D.
func (Exec) DeleteBranchAt(repo, branch, tip string) error {
	if !validBranch(repo, branch) {
		return errors.New("nome de branch inválido")
	}
	if !fullSHA.MatchString(tip) {
		return errors.New("ponta do branch inválida")
	}
	out, err := gitWrite(repo, "update-ref", "-d", "--", "refs/heads/"+branch, tip)
	if err != nil && strings.Contains(out, "but expected") {
		return errors.New("o branch mudou depois da verificação")
	}
	return err
}
