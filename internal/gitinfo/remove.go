package gitinfo

import (
	"errors"
	"os/exec"
	"strings"
)

// WorktreeRemover são as únicas escritas do git do Cockpit sobre worktrees:
// explícitas, pedidas pelo usuário e nunca destrutivas além do necessário
// (o branch só sai com -d, nunca -D). Fica fora de GitInfo pelo mesmo motivo
// de WorktreeReader.
type WorktreeRemover interface {
	RemoveWorktree(repo, path string, force bool) error // git worktree remove [--force] -- path
	PruneWorktrees(repo string) error                   // git worktree prune
	DeleteBranch(repo, branch string) error             // git branch -d -- branch (valida o nome com validBranch)
}

var _ WorktreeRemover = Exec{}

// gitWrite roda um git que escreve e devolve a mensagem do git como erro.
func gitWrite(repo string, args ...string) error {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// RemoveWorktree roda git worktree remove [--force] -- path em repo.
func (Exec) RemoveWorktree(repo, path string, force bool) error {
	if path == "" || strings.HasPrefix(path, "-") {
		return errors.New("caminho inválido")
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	return gitWrite(repo, append(args, "--", path)...)
}

// PruneWorktrees roda git worktree prune em repo.
func (Exec) PruneWorktrees(repo string) error {
	return gitWrite(repo, "worktree", "prune")
}

// DeleteBranch roda git branch -d -- branch em repo: o git recusa o branch
// que não está integrado. Nunca usa -D.
func (Exec) DeleteBranch(repo, branch string) error {
	if !validBranch(repo, branch) {
		return errors.New("nome de branch inválido")
	}
	return gitWrite(repo, "branch", "-d", "--", branch)
}
