package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func (s *server) configRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("GET /api/limits", s.listLimits)
	mux.HandleFunc("PUT /api/limits", s.putLimit)
	mux.HandleFunc("GET /api/prices", s.listPrices)
	mux.HandleFunc("PUT /api/prices", s.putPrice)
	mux.HandleFunc("DELETE /api/prices", s.deletePrice)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("PATCH /api/projects/{id}", s.patchProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// putSettings aplica o corpo sobre as preferências atuais: chaves ausentes
// mantêm o valor, e "rules" é mesclado regra a regra.
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if !readJSON(w, r, &st) {
		return
	}
	switch {
	case st.IncompleteAction != "ask_agent" && st.IncompleteAction != "mark":
		writeError(w, http.StatusBadRequest, "incomplete_action inválida (use ask_agent ou mark)")
		return
	case st.Currency != "USD" && st.Currency != "BRL":
		writeError(w, http.StatusBadRequest, "moeda inválida (use USD ou BRL)")
		return
	case st.WhereStoppedTTLMin < 0 || st.GraceMin < 0 || st.ReminderEditThreshold < 0 || st.BRLRate < 0:
		writeError(w, http.StatusBadRequest, "valores numéricos não podem ser negativos")
		return
	}
	if err := s.st.PutSettings(st); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config")
	s.getSettings(w, r)
}

func (s *server) listLimits(w http.ResponseWriter, r *http.Request) {
	ls, err := s.st.ListLimits()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ls))
}

// putLimit grava (cria ou atualiza) o limite de um agente.
func (s *server) putLimit(w http.ResponseWriter, r *http.Request) {
	l := domain.Limit{WarnPct: 80, Action: "warn"}
	if !readJSON(w, r, &l) {
		return
	}
	switch {
	case strings.TrimSpace(l.Agent) == "":
		writeError(w, http.StatusBadRequest, "agente obrigatório")
		return
	case l.Action != "pause" && l.Action != "warn":
		writeError(w, http.StatusBadRequest, "ação inválida (use pause ou warn)")
		return
	case l.DailyUSD < 0 || l.CardUSD < 0 || l.WarnPct < 0 || l.WarnPct > 100:
		writeError(w, http.StatusBadRequest, "valores do limite fora do intervalo")
		return
	}
	if err := s.st.PutLimit(l); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config")
	writeJSON(w, http.StatusOK, l)
}

func (s *server) listPrices(w http.ResponseWriter, r *http.Request) {
	ps, err := s.st.ListPrices()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ps))
}

// putPrice grava (cria ou atualiza) o preço de um modelo. Preço muda custo,
// por isso publica também "usage".
func (s *server) putPrice(w http.ResponseWriter, r *http.Request) {
	var p domain.Price
	if !readJSON(w, r, &p) {
		return
	}
	if strings.TrimSpace(p.Model) == "" {
		writeError(w, http.StatusBadRequest, "modelo obrigatório")
		return
	}
	if p.InputPerM < 0 || p.OutputPerM < 0 || p.CacheWritePerM < 0 || p.CacheReadPerM < 0 {
		writeError(w, http.StatusBadRequest, "preço não pode ser negativo")
		return
	}
	if err := s.st.PutPrice(p); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config", "usage")
	writeJSON(w, http.StatusOK, p)
}

// deletePrice remove o preço de ?model=.
func (s *server) deletePrice(w http.ResponseWriter, r *http.Request) {
	model := r.URL.Query().Get("model")
	if model == "" {
		writeError(w, http.StatusBadRequest, "modelo obrigatório")
		return
	}
	if err := s.st.DeletePrice(model); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config", "usage")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ps))
}

func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !filepath.IsAbs(in.Path) {
		writeError(w, http.StatusBadRequest, "caminho do projeto deve ser absoluto")
		return
	}
	in.Path = filepath.Clean(in.Path)
	if strings.TrimSpace(in.Name) == "" {
		in.Name = filepath.Base(in.Path)
	}
	p, err := s.st.CreateProject(in.Name, in.Path)
	if isConstraint(err, "UNIQUE") {
		writeError(w, http.StatusConflict, "já existe um projeto com esse caminho")
		return
	}
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config")
	writeJSON(w, http.StatusCreated, p)
}

func (s *server) patchProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.projectByID(id)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "projeto não encontrado")
		return
	}
	if !readJSON(w, r, p) {
		return
	}
	p.ID = id
	if strings.TrimSpace(p.Name) == "" {
		writeError(w, http.StatusBadRequest, "nome obrigatório")
		return
	}
	if !filepath.IsAbs(p.Path) {
		writeError(w, http.StatusBadRequest, "caminho do projeto deve ser absoluto")
		return
	}
	p.Path = filepath.Clean(p.Path)
	err = s.st.UpdateProject(*p)
	if isConstraint(err, "UNIQUE") {
		writeError(w, http.StatusConflict, "já existe um projeto com esse caminho")
		return
	}
	if err != nil {
		writeStoreError(w, err, "projeto não encontrado")
		return
	}
	s.publish("config")
	writeJSON(w, http.StatusOK, p)
}

func (s *server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.st.DeleteProject(id)
	if isConstraint(err, "FOREIGN KEY") {
		writeError(w, http.StatusConflict, "o projeto tem cards, sessões ou prompts ligados e não pode ser removido")
		return
	}
	if err != nil {
		writeStoreError(w, err, "projeto não encontrado")
		return
	}
	s.publish("config")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
