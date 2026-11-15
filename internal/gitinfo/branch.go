package gitinfo

import "strings"

// BranchReader consulta branches de um repositório. Fica fora de GitInfo
// pelo mesmo motivo de WorktreeReader: consumidores usam type assertion.
type BranchReader interface {
	// DetectBase acha o branch base: develop (local ou origin/develop), o
	// alvo de origin/HEAD, main, master; "" se nenhum existe.
	DetectBase(repo string) string
	// BranchTip devolve o sha completo de refs/heads/<branch>; false se não existe.
	BranchTip(repo, branch string) (string, bool)
	// IsAncestor diz se commit está contido em ref; false se ref não existe.
	IsAncestor(repo, commit, ref string) bool
}

var _ BranchReader = Exec{}

func refExists(repo, ref string) bool {
	_, err := gitRO(repo, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// DetectBase devolve o branch base de repo, ou "".
func (Exec) DetectBase(repo string) string {
	if refExists(repo, "refs/heads/develop") || refExists(repo, "refs/remotes/origin/develop") {
		return "develop"
	}
	if out, err := gitRO(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		name := strings.TrimPrefix(strings.TrimSpace(out), "origin/")
		if name != "" && (refExists(repo, "refs/heads/"+name) || refExists(repo, "refs/remotes/origin/"+name)) {
			return name
		}
	}
	for _, b := range []string{"main", "master"} {
		if refExists(repo, "refs/heads/"+b) {
			return b
		}
	}
	return ""
}

// BranchTip devolve o commit ponta de refs/heads/<branch>.
func (Exec) BranchTip(repo, branch string) (string, bool) {
	if !validBranch(repo, branch) {
		return "", false
	}
	out, err := gitRO(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	sha := strings.TrimSpace(out)
	return sha, err == nil && sha != ""
}

// IsAncestor roda git merge-base --is-ancestor commit ref.
func (Exec) IsAncestor(repo, commit, ref string) bool {
	if commit == "" || ref == "" || strings.HasPrefix(commit, "-") || strings.HasPrefix(ref, "-") {
		return false
	}
	_, err := gitRO(repo, "merge-base", "--is-ancestor", commit, ref)
	return err == nil
}
