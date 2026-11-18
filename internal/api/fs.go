package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

const (
	msgOutsideHome = "pasta fora da home do usuário"
	msgNotDir      = "não é uma pasta"
	msgNothingHere = workspaces.MsgNoRepos
)

func (s *server) fsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fs/dirs", s.fsDirs)
	mux.HandleFunc("GET /api/fs/scan", s.fsScan)
}

// homeDir é a home resolvida (sem symlinks); Deps.Home vazio = home do usuário.
func (s *server) homeDir() (string, error) {
	home := s.deps.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = h
	}
	return filepath.EvalSymlinks(filepath.Clean(home))
}

func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// resolveDir valida ?path= (vazio = home): absoluto, existente, diretório e
// dentro da home depois de Clean e EvalSymlinks. Em erro já respondeu.
func (s *server) resolveDir(w http.ResponseWriter, r *http.Request) (real, home string, ok bool) {
	home, err := s.homeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "não foi possível resolver a home do usuário")
		return "", "", false
	}
	p := r.URL.Query().Get("path")
	if p == "" {
		p = home
	}
	if !filepath.IsAbs(p) {
		writeError(w, http.StatusBadRequest, msgNotDir)
		return "", "", false
	}
	real, err = filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		writeError(w, http.StatusBadRequest, msgNotDir)
		return "", "", false
	}
	if !within(real, home) {
		writeError(w, http.StatusBadRequest, msgOutsideHome)
		return "", "", false
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		writeError(w, http.StatusBadRequest, msgNotDir)
		return "", "", false
	}
	return real, home, true
}

func isGitDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func projectByPath(ps []domain.Project) map[string]*domain.Project {
	m := map[string]*domain.Project{}
	for i := range ps {
		if ps[i].Path != "" {
			m[filepath.Clean(ps[i].Path)] = &ps[i]
		}
	}
	return m
}

// fsDirs: GET /api/fs/dirs?path=, só nomes de pastas (sem as ocultas), nunca
// arquivos nem conteúdo.
func (s *server) fsDirs(w http.ResponseWriter, r *http.Request) {
	real, home, ok := s.resolveDir(w, r)
	if !ok {
		return
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		writeError(w, http.StatusBadRequest, msgNotDir)
		return
	}
	ps, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	reg := projectByPath(ps)
	type dirOut struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		Git        bool   `json:"git"`
		Registered bool   `json:"registered"`
	}
	dirs := []dirOut{}
	for _, e := range entries {
		// IsDir é falso para symlink: links não aparecem nem são seguidos.
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(real, e.Name())
		dirs = append(dirs, dirOut{Name: e.Name(), Path: p, Git: isGitDir(p), Registered: reg[p] != nil})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	parent := ""
	if real != home {
		parent = filepath.Dir(real)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": real, "parent": parent, "dirs": dirs})
}

type scanSuggestion struct {
	workspaces.Suggestion
	Registered *domain.Project `json:"registered"`
	Blocked    string          `json:"blocked"`
}

// dismissedFor lê as sugestões recusadas do projeto id; KV corrompido vira vazio.
func (s *server) dismissedFor(id int64) (map[string]bool, error) {
	dismissed := map[string][]string{}
	if _, err := s.st.GetKV(dismissedKey, &dismissed); err != nil {
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		if !errors.As(err, &syn) && !errors.As(err, &typ) {
			return nil, err
		}
		log.Printf("varredura: %s ilegível, ignorando as recusas: %v", dismissedKey, err)
		dismissed = map[string][]string{}
	}
	hidden := map[string]bool{}
	for _, rel := range dismissed[strconv.FormatInt(id, 10)] {
		hidden[rel] = true
	}
	return hidden, nil
}

// fsScan: GET /api/fs/scan?path=, o que a pasta é (repositório, monorepo ou
// grupo de repositórios) e as sugestões de sub-projetos dentro dela. A home
// não pode ser cadastrada: responde a recusa do cadastro, sem varrer.
func (s *server) fsScan(w http.ResponseWriter, r *http.Request) {
	real, home, ok := s.resolveDir(w, r)
	if !ok {
		return
	}
	if real == home {
		writeError(w, http.StatusBadRequest, msgHomeProject)
		return
	}
	git := isGitDir(real)
	var found []workspaces.Suggestion
	var err error
	kind := "repo"
	if git {
		found, err = workspaces.Detect(real)
		if err == nil && len(found) > 0 {
			kind = "monorepo"
		}
	} else {
		found, err = workspaces.ScanRepos(real)
		kind = "group"
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, msgNotDir)
		return
	}
	if !git && len(found) == 0 {
		writeError(w, http.StatusBadRequest, msgNothingHere)
		return
	}
	ps, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	byPath := projectByPath(ps)
	byID := map[int64]*domain.Project{}
	hasKids := map[int64]bool{}
	for i := range ps {
		byID[ps[i].ID] = &ps[i]
		if ps[i].ParentID != nil {
			hasKids[*ps[i].ParentID] = true
		}
	}
	self := byPath[real]
	hidden := map[string]bool{}
	if self != nil {
		if hidden, err = s.dismissedFor(self.ID); err != nil {
			writeStoreError(w, err, "")
			return
		}
	}
	sugs := []scanSuggestion{}
	for _, sg := range found {
		if hidden[sg.Rel] {
			continue
		}
		out := scanSuggestion{Suggestion: sg, Registered: byPath[filepath.Clean(sg.Path)]}
		if reg := out.Registered; reg != nil {
			switch {
			case reg.ParentID != nil && (self == nil || *reg.ParentID != self.ID):
				name := ""
				if par := byID[*reg.ParentID]; par != nil {
					name = par.Name
				}
				out.Blocked = "pertence a " + name
			case reg.ParentID == nil && hasKids[reg.ID]:
				out.Blocked = "tem sub-projetos"
			}
		}
		sugs = append(sugs, out)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": real, "git": git, "kind": kind, "name": filepath.Base(real),
		"registered": self, "suggestions": sugs,
	})
}
