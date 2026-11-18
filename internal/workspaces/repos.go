package workspaces

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MsgNoRepos é a recusa da varredura de uma pasta que não é git e não tem
// repositórios dentro (o daemon responde, a CLI reconhece).
const MsgNoRepos = "a pasta não é um repositório git nem tem repositórios dentro"

// scanDepth é quantos níveis abaixo da raiz a varredura de repositórios olha.
const scanDepth = 2

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
}

// SkipDir diz se a varredura pula a pasta name: oculta ou de dependências/build.
func SkipDir(name string) bool { return strings.HasPrefix(name, ".") || skipDirs[name] }

// InScan diz se a pasta rel (relativa à raiz da varredura) está ao alcance de
// ScanRepos: até 2 níveis e nenhuma pasta do caminho que a varredura pula.
func InScan(rel string) bool {
	segs := strings.Split(filepath.ToSlash(rel), "/")
	if len(segs) > scanDepth {
		return false
	}
	for _, s := range segs {
		if s == "" || SkipDir(s) {
			return false
		}
	}
	return true
}

// ScanRepos procura repositórios git até 2 níveis abaixo de root. Uma pasta
// com .git (pasta ou arquivo, como nos worktrees) é um repositório e a
// varredura não desce nela. Pula pastas ocultas e de dependências/build e não
// segue symlinks. O resultado vem ordenado por Rel, com Source "git".
func ScanRepos(root string) ([]Suggestion, error) {
	root = filepath.Clean(root)
	out := []Suggestion{}
	if err := scanRepos(root, "", 1, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

func scanRepos(root, rel string, depth int, out *[]Suggestion) error {
	entries, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		if rel == "" {
			return err
		}
		return nil // subpasta ilegível: segue sem ela
	}
	for _, e := range entries {
		name := e.Name()
		// DirEntry.IsDir é falso para symlink: não seguimos.
		if !e.IsDir() || SkipDir(name) {
			continue
		}
		sub := filepath.ToSlash(filepath.Join(rel, name))
		abs := filepath.Join(root, sub)
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			*out = append(*out, Suggestion{Path: abs, Rel: sub, Name: name, Source: "git"})
			continue
		}
		if depth < scanDepth {
			if err := scanRepos(root, sub, depth+1, out); err != nil {
				return err
			}
		}
	}
	return nil
}
