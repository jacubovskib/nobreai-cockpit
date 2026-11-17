package gitinfo

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// maxFiles limita a lista de arquivos alterados de Inspection.
const maxFiles = 50

// FileChange é um arquivo no git status; Code é o par XY do porcelain
// ("M ", " M", "??", "UU"…).
type FileChange struct {
	Path string `json:"path"`
	Code string `json:"code"`
}

// Commit é um commit do log.
type Commit struct {
	Hash    string    `json:"hash"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	At      time.Time `json:"at"`
}

// Inspection é o estado git de um worktree: alterações na árvore, últimos
// commits e, com Compared, quantos commits o branch está à frente e atrás da
// base. Files traz no máximo 50 arquivos; TotalFiles conta todos.
type Inspection struct {
	Staged     int          `json:"staged"`
	Modified   int          `json:"modified"`
	Untracked  int          `json:"untracked"`
	Conflicts  int          `json:"conflicts"`
	TotalFiles int          `json:"total_files"`
	Files      []FileChange `json:"files"`
	Commits    []Commit     `json:"commits"`
	Base       string       `json:"base"`
	Compared   bool         `json:"compared"`
	Ahead      int          `json:"ahead"`
	Behind     int          `json:"behind"`
}

// WorktreeInspector lê o estado git detalhado de um worktree. Fica fora de
// GitInfo pelo mesmo motivo de WorktreeReader.
type WorktreeInspector interface {
	Inspect(dir, base, head string, commits int) (Inspection, error)
}

var _ WorktreeInspector = Exec{}

// StatusReader lê as alterações de um worktree vendo sempre os arquivos
// novos, qualquer que seja o status.showUntrackedFiles do usuário (é o que
// decide se excluir o worktree perde algo).
type StatusReader interface {
	FullStatus(dir string) (Inspection, error)
}

var _ StatusReader = Exec{}

// FullStatus roda git status --porcelain=v1 -z -unormal em dir.
func (Exec) FullStatus(dir string) (Inspection, error) {
	in := Inspection{Files: []FileChange{}, Commits: []Commit{}}
	out, err := gitRO(dir, "status", "--porcelain=v1", "-z", "-unormal")
	if err != nil {
		return in, err
	}
	parseStatus(out, &in)
	return in, nil
}

// gitRO roda um comando git só de leitura sem pegar locks opcionais: o
// git status não atualiza o índice (nem disputa index.lock com quem está
// trabalhando no worktree).
func gitRO(dir string, args ...string) (string, error) {
	full := append([]string{"--no-optional-locks", "-C", dir}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// validBranch recusa nomes que o git leria como opção ou que não são branch.
func validBranch(dir, name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "@") {
		return false
	}
	_, err := gitRO(dir, "check-ref-format", "--branch", name)
	return err == nil
}

// Inspect lê o estado git de dir. base e head são nomes de branch; com os
// dois válidos e diferentes, compara head com base. commits é quantos
// commits do log trazer.
func (Exec) Inspect(dir, base, head string, commits int) (Inspection, error) {
	in := Inspection{Files: []FileChange{}, Commits: []Commit{}, Base: base}
	out, err := gitRO(dir, "status", "--porcelain=v1", "-z")
	if err != nil {
		return in, err
	}
	parseStatus(out, &in)

	if commits > 0 {
		if out, err := gitRO(dir, "log", "-n", strconv.Itoa(commits), "--format=%H%x00%s%x00%an%x00%aI"); err == nil {
			for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
				f := strings.Split(line, "\x00")
				if len(f) != 4 {
					continue
				}
				at, _ := time.Parse(time.RFC3339, f[3])
				in.Commits = append(in.Commits, Commit{Hash: f[0], Subject: f[1], Author: f[2], At: at})
			}
		}
	}

	if base != head && validBranch(dir, base) && validBranch(dir, head) {
		out, err := gitRO(dir, "rev-list", "--left-right", "--count", "refs/heads/"+base+"...refs/heads/"+head, "--")
		if f := strings.Fields(out); err == nil && len(f) == 2 {
			in.Behind, _ = strconv.Atoi(f[0])
			in.Ahead, _ = strconv.Atoi(f[1])
			in.Compared = true
		}
	}
	return in, nil
}

// conflictCodes são os pares XY de arquivo com conflito de merge.
var conflictCodes = map[string]bool{"DD": true, "AU": true, "UD": true, "UA": true, "DU": true, "AA": true, "UU": true}

// parseStatus conta a saída de git status --porcelain=v1 -z. Renomeações
// trazem o caminho de origem como entrada extra, que é pulada.
func parseStatus(out string, in *Inspection) {
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		x, y, path := e[0], e[1], e[3:]
		if x == 'R' || x == 'C' {
			i++ // origem da renomeação/cópia
		}
		code := e[:2]
		switch {
		case code == "??":
			in.Untracked++
		case conflictCodes[code]:
			in.Conflicts++
		default:
			if x != ' ' {
				in.Staged++
			}
			if y != ' ' {
				in.Modified++
			}
		}
		in.TotalFiles++
		if len(in.Files) < maxFiles {
			in.Files = append(in.Files, FileChange{Path: path, Code: code})
		}
	}
}

// ReadOnly expõe gitRO a outros pacotes (o levantamento do projeto): só
// leitura, sem locks opcionais, nunca busca na rede.
func ReadOnly(dir string, args ...string) (string, error) { return gitRO(dir, args...) }

// OrphanReader acha o que a remoção de um worktree perderia além dos arquivos:
// commits alcançáveis só pelo HEAD dele (HEAD destacado, rebase parado no
// meio), já que o reflog do worktree some com a pasta administrativa.
type OrphanReader interface {
	OrphanCommits(dir string) (commits []Commit, total int, rebase bool, err error)
}

var _ OrphanReader = Exec{}

// OrphanCommits lista até 20 commits de HEAD que nenhum branch, tag ou
// remoto alcança, o total deles e se há um rebase em andamento.
func (Exec) OrphanCommits(dir string) ([]Commit, int, bool, error) {
	commits := []Commit{}
	rebase := false
	for _, p := range []string{"rebase-merge", "rebase-apply"} {
		if out, err := gitRO(dir, "rev-parse", "--path-format=absolute", "--git-path", p); err == nil {
			if st, err := os.Stat(strings.TrimSpace(out)); err == nil && st.IsDir() {
				rebase = true
			}
		}
	}
	if _, err := gitRO(dir, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return commits, 0, rebase, nil // sem commits ainda
	}
	not := []string{"HEAD", "--not", "--branches", "--tags", "--remotes", "--"}
	out, err := gitRO(dir, append([]string{"rev-list", "--count"}, not...)...)
	if err != nil {
		return commits, 0, rebase, err
	}
	total, _ := strconv.Atoi(strings.TrimSpace(out))
	if total == 0 {
		return commits, 0, rebase, nil
	}
	out, err = gitRO(dir, append([]string{"log", "-n", "20", "--format=%H%x00%s%x00%an%x00%aI"}, not...)...)
	if err != nil {
		return commits, total, rebase, err
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 4 {
			continue
		}
		at, _ := time.Parse(time.RFC3339, f[3])
		commits = append(commits, Commit{Hash: f[0], Subject: f[1], Author: f[2], At: at})
	}
	return commits, total, rebase, nil
}
