// Package workspaces sugere sub-projetos a partir das listas de workspaces
// que o próprio repositório já declara (pnpm, npm, Go, Cargo).
package workspaces

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Suggestion é uma pasta que o repositório declara como workspace.
type Suggestion struct {
	Path   string `json:"path"`   // absoluto
	Rel    string `json:"rel"`    // relativo à raiz, ex.: "apps/web"
	Name   string `json:"name"`   // último pedaço
	Source string `json:"source"` // "pnpm-workspace.yaml", "package.json", "go.work", "Cargo.toml"
}

// Detect lê as listas de workspaces de root. O resultado vem ordenado por Rel,
// sem duplicatas (vale a primeira fonte na ordem pnpm, package.json, go.work,
// Cargo.toml).
func Detect(root string) ([]Suggestion, error) {
	root = filepath.Clean(root)
	readers := []struct {
		file  string
		parse func(string) []string
	}{
		{"pnpm-workspace.yaml", parsePnpm},
		{"package.json", parsePackageJSON},
		{"go.work", parseGoWork},
		{"Cargo.toml", parseCargo},
	}
	seen := map[string]bool{}
	out := []Suggestion{}
	for _, r := range readers {
		b, err := os.ReadFile(filepath.Join(root, r.file))
		if err != nil {
			continue
		}
		for _, pat := range r.parse(string(b)) {
			for _, rel := range expand(root, pat) {
				if seen[rel] {
					continue
				}
				seen[rel] = true
				out = append(out, Suggestion{Path: filepath.Join(root, rel), Rel: rel, Name: filepath.Base(rel), Source: r.file})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// expand resolve um padrão em pastas existentes (relativas, com "/"). O "*"
// só vale no último pedaço; "!" e "**" são ignorados, e a própria raiz também.
func expand(root, pat string) []string {
	pat = strings.TrimSpace(pat)
	if pat == "" || strings.HasPrefix(pat, "!") || strings.Contains(pat, "**") {
		return nil
	}
	pat = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimPrefix(pat, "./"))))
	if pat == "." || filepath.IsAbs(pat) || pat == ".." || strings.HasPrefix(pat, "../") {
		return nil
	}
	if i := strings.LastIndex(pat, "/"); strings.ContainsAny(pat[:i+1], "*?[") {
		return nil // curinga fora do último pedaço
	}
	matches := []string{pat}
	if strings.ContainsAny(pat, "*?[") {
		var err error
		matches, err = filepath.Glob(filepath.Join(root, filepath.FromSlash(pat)))
		if err != nil {
			return nil
		}
		for i, m := range matches {
			if rel, err := filepath.Rel(root, m); err == nil {
				matches[i] = filepath.ToSlash(rel)
			}
		}
	}
	var out []string
	for _, m := range matches {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(m))); err == nil && fi.IsDir() {
			out = append(out, m)
		}
	}
	return out
}

// unquote tira aspas e comentário de fim de linha de um item.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'' || s[0] == '`') {
		if end := strings.IndexByte(s[1:], s[0]); end >= 0 {
			return s[1 : end+1]
		}
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func parsePnpm(src string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(trim, "-") {
			in = strings.HasPrefix(trim, "packages:")
			continue
		}
		if in && strings.HasPrefix(trim, "-") {
			out = append(out, unquote(strings.TrimPrefix(trim, "-")))
		}
	}
	return out
}

func parsePackageJSON(src string) []string {
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal([]byte(src), &pkg) != nil || len(pkg.Workspaces) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(pkg.Workspaces, &list) == nil {
		return list
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(pkg.Workspaces, &obj) == nil {
		return obj.Packages
	}
	return nil
}

func parseGoWork(src string) []string {
	var out []string
	block := false
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if i := strings.Index(trim, "//"); i >= 0 {
			trim = strings.TrimSpace(trim[:i])
		}
		switch {
		case block:
			if trim == ")" {
				block = false
			} else if trim != "" {
				out = append(out, unquote(trim))
			}
		case strings.HasPrefix(trim, "use") && strings.TrimSpace(strings.TrimPrefix(trim, "use")) == "(":
			block = true
		case strings.HasPrefix(trim, "use ") || strings.HasPrefix(trim, "use\t"):
			if rest := strings.TrimSpace(trim[3:]); rest != "" {
				out = append(out, unquote(rest))
			}
		}
	}
	return out
}

func parseCargo(src string) []string {
	var out []string
	inWorkspace, inMembers := false, false
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") && !inMembers {
			inWorkspace = trim == "[workspace]"
			continue
		}
		if !inWorkspace {
			continue
		}
		if !inMembers {
			rest, ok := strings.CutPrefix(trim, "members")
			if !ok {
				continue
			}
			rest = strings.TrimSpace(rest)
			if !strings.HasPrefix(rest, "=") {
				continue
			}
			trim = strings.TrimSpace(strings.TrimPrefix(rest, "="))
			if !strings.HasPrefix(trim, "[") {
				continue
			}
			trim = trim[1:]
			inMembers = true
		}
		// Itens entre aspas na linha; "]" fora de aspas fecha o array.
		for trim != "" {
			trim = strings.TrimLeft(trim, " \t,")
			if trim == "" || strings.HasPrefix(trim, "#") {
				break
			}
			if trim[0] == ']' {
				inMembers = false
				break
			}
			if trim[0] == '"' || trim[0] == '\'' {
				end := strings.IndexByte(trim[1:], trim[0])
				if end < 0 {
					break
				}
				out = append(out, trim[1:end+1])
				trim = trim[end+2:]
				continue
			}
			break
		}
	}
	return out
}
