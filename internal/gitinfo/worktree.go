package gitinfo

import (
	"path/filepath"
	"strings"
)

// Location diz onde um diretório fica no git: Root é o topo do worktree que o
// contém e Main o topo do repositório principal. Linked indica um worktree
// ligado (criado com git worktree add), com Root diferente de Main. Fora de
// um repositório, todos os campos ficam vazios.
type Location struct {
	Root   string `json:"root"`
	Main   string `json:"main"`
	Linked bool   `json:"linked"`
}

// Worktree é um item de git worktree list. Main marca o worktree principal
// (sempre o primeiro da lista). Branch vem sem o prefixo refs/heads/ e fica
// vazio com HEAD destacado. Prunable: o diretório sumiu e o git pode limpá-lo.
type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"`
	Main     bool   `json:"main"`
	Bare     bool   `json:"bare"`
	Detached bool   `json:"detached"`
	Locked   bool   `json:"locked"`
	Prunable bool   `json:"prunable"`
	// LockReason e PruneReason são os motivos que o git informa (podem ser vazios).
	LockReason  string `json:"lock_reason"`
	PruneReason string `json:"prune_reason"`
}

// WorktreeReader consulta worktrees. Fica fora de GitInfo para que quem não
// precisa (e os fakes de teste) não tenha de implementá-lo; consumidores
// verificam com type assertion.
type WorktreeReader interface {
	Locate(dir string) Location
	Worktrees(repo string) ([]Worktree, error)
}

var _ WorktreeReader = Exec{}

// Locate devolve o worktree e o repositório principal de dir.
func (Exec) Locate(dir string) Location {
	if dir == "" {
		return Location{}
	}
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return Location{}
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		return Location{}
	}
	root, common := lines[0], filepath.Clean(lines[1])
	main := common
	if filepath.Base(common) == ".git" {
		main = filepath.Dir(common)
	}
	return Location{Root: root, Main: main, Linked: root != main}
}

// Worktrees lista os worktrees do repositório de repo (qualquer worktree dele
// serve), começando pelo principal.
func (Exec) Worktrees(repo string) ([]Worktree, error) {
	out, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

// parseWorktrees lê a saída de git worktree list --porcelain: um bloco por
// worktree, separados por linha em branco, com uma chave por linha.
func parseWorktrees(out string) []Worktree {
	var list []Worktree
	for block := range strings.SplitSeq(strings.TrimSpace(out), "\n\n") {
		var w Worktree
		for line := range strings.SplitSeq(block, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				w.Path = val
			case "HEAD":
				w.Head = val
			case "branch":
				w.Branch = strings.TrimPrefix(val, "refs/heads/")
			case "bare":
				w.Bare = true
			case "detached":
				w.Detached = true
			case "locked":
				w.Locked, w.LockReason = true, val
			case "prunable":
				w.Prunable, w.PruneReason = true, val
			}
		}
		if w.Path == "" {
			continue
		}
		w.Main = len(list) == 0
		list = append(list, w)
	}
	return list
}
