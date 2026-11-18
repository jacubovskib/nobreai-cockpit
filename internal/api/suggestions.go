package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

// dismissedKey é a chave livre em settings com as sugestões recusadas:
// mapa "<id do projeto>" → []rel.
const dismissedKey = "subproject_dismissed"

// repoFor devolve o projeto cuja pasta é o repositório de p (o pai, no caso
// de um sub-projeto); nil, depois de responder o erro, quando não há pasta.
func (s *server) repoFor(w http.ResponseWriter, r *http.Request) *domain.Project {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "projeto inválido")
		return nil
	}
	p, err := s.projectByID(id)
	if err != nil {
		writeStoreError(w, err, "")
		return nil
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "projeto não encontrado")
		return nil
	}
	if p.IsGroup() {
		writeError(w, http.StatusBadRequest, "grupo não tem repositório; veja os sub-projetos de cada pasta")
		return nil
	}
	repo, err := s.st.RepoProject(*p)
	if err != nil {
		writeStoreError(w, err, "")
		return nil
	}
	return &repo
}

// openSuggestions são os sub-projetos que o repositório declara e que ainda
// não foram cadastrados nem recusados (nunca nil).
func (s *server) openSuggestions(repo domain.Project) ([]workspaces.Suggestion, error) {
	found, err := workspaces.Detect(repo.Path)
	if err != nil {
		return nil, err
	}
	ps, err := s.st.ListProjects()
	if err != nil {
		return nil, err
	}
	registered := map[string]bool{}
	for _, p := range ps {
		if p.Path != "" {
			registered[filepath.Clean(p.Path)] = true
		}
	}
	dismissed := map[string][]string{}
	if _, err := s.st.GetKV(dismissedKey, &dismissed); err != nil {
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		if !errors.As(err, &syn) && !errors.As(err, &typ) {
			return nil, err
		}
		// Valor corrompido: as recusas se perdem, o resto segue funcionando.
		log.Printf("sugestões: %s ilegível, ignorando as recusas: %v", dismissedKey, err)
		dismissed = map[string][]string{}
	}
	hidden := map[string]bool{}
	for _, rel := range dismissed[strconv.FormatInt(repo.ID, 10)] {
		hidden[rel] = true
	}
	out := []workspaces.Suggestion{}
	for _, sg := range found {
		if !registered[filepath.Clean(sg.Path)] && !hidden[sg.Rel] {
			out = append(out, sg)
		}
	}
	return out, nil
}

// suggestions: GET /api/projects/{id}/suggestions, os sub-projetos que o
// repositório declara e que ainda não foram cadastrados nem recusados.
func (s *server) suggestions(w http.ResponseWriter, r *http.Request) {
	repo := s.repoFor(w, r)
	if repo == nil {
		return
	}
	out, err := s.openSuggestions(*repo)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// dismissSuggestions: POST /api/projects/{id}/suggestions/dismiss {"rels":[...]}
// esconde as sugestões para sempre (a recusa é do repositório).
func (s *server) dismissSuggestions(w http.ResponseWriter, r *http.Request) {
	repo := s.repoFor(w, r)
	if repo == nil {
		return
	}
	var in struct {
		Rels []string `json:"rels"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	dismissed := map[string][]string{}
	if _, err := s.st.GetKV(dismissedKey, &dismissed); err != nil {
		writeStoreError(w, err, "")
		return
	}
	key := strconv.FormatInt(repo.ID, 10)
	have := map[string]bool{}
	for _, rel := range dismissed[key] {
		have[rel] = true
	}
	for _, rel := range in.Rels {
		if rel != "" && !have[rel] {
			have[rel] = true
			dismissed[key] = append(dismissed[key], rel)
		}
	}
	if err := s.st.PutKV(dismissedKey, dismissed); err != nil {
		writeStoreError(w, err, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
