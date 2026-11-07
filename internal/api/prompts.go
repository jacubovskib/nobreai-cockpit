package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/prompts"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
)

const msgPromptNotFound = "prompt não encontrado"

var validSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (s *server) promptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/prompts", s.listPrompts)
	mux.HandleFunc("POST /api/prompts", s.createPrompt)
	mux.HandleFunc("PATCH /api/prompts/{slug}", s.patchPrompt)
	mux.HandleFunc("DELETE /api/prompts/{slug}", s.deletePrompt)
	mux.HandleFunc("POST /api/prompts/{slug}/render", s.renderPrompt)
}

func (s *server) listPrompts(w http.ResponseWriter, r *http.Request) {
	ps, err := s.st.ListPrompts()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ps))
}

func validatePrompt(p domain.Prompt) string {
	switch {
	case strings.TrimSpace(p.Name) == "":
		return "nome obrigatório"
	case !validSlug.MatchString(p.Slug):
		return "slug inválido (use letras minúsculas, números e hífen)"
	}
	return ""
}

func (s *server) createPrompt(w http.ResponseWriter, r *http.Request) {
	var p domain.Prompt
	if !readJSON(w, r, &p) {
		return
	}
	if msg := validatePrompt(p); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	_, err := s.st.CreatePrompt(p)
	if isConstraint(err, "UNIQUE") {
		writeError(w, http.StatusConflict, "já existe um prompt com esse slug")
		return
	}
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("prompts")
	s.writePrompt(w, http.StatusCreated, p.Slug)
}

func (s *server) writePrompt(w http.ResponseWriter, code int, slug string) {
	p, err := s.st.GetPrompt(slug)
	if err != nil {
		writeStoreError(w, err, msgPromptNotFound)
		return
	}
	writeJSON(w, code, p)
}

// patchPrompt aplica o corpo sobre o prompt atual; o slug não muda.
func (s *server) patchPrompt(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := s.st.GetPrompt(slug)
	if err != nil {
		writeStoreError(w, err, msgPromptNotFound)
		return
	}
	if !readJSON(w, r, &p) {
		return
	}
	p.Slug = slug
	if msg := validatePrompt(p); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.st.UpdatePrompt(p); err != nil {
		writeStoreError(w, err, msgPromptNotFound)
		return
	}
	s.publish("prompts")
	s.writePrompt(w, http.StatusOK, slug)
}

func (s *server) deletePrompt(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeletePrompt(r.PathValue("slug")); err != nil {
		writeStoreError(w, err, msgPromptNotFound)
		return
	}
	s.publish("prompts")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// renderPrompt preenche o prompt com as variáveis do card (opcional).
// Body, se enviado, substitui o texto salvo (prévia do editor); use=true
// registra o uso. Variável desconhecida → 422 com "unknown".
func (s *server) renderPrompt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Card string `json:"card"`
		Body string `json:"body"`
		Use  bool   `json:"use"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	slug := r.PathValue("slug")
	p, err := s.st.GetPrompt(slug)
	if err != nil {
		writeStoreError(w, err, msgPromptNotFound)
		return
	}
	body := p.Body
	if in.Body != "" {
		body = in.Body
	}
	vars := prompts.Vars{}
	if in.Card != "" {
		c, err := s.st.GetCard(in.Card)
		if err != nil {
			writeStoreError(w, err, msgCardNotFound)
			return
		}
		d, err := s.cardDetail(c)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		pkg, err := s.resumeMarkdown(d)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		vars = resume.Vars(d.Card, d.Content, pkg, d.Readiness)
	}
	text, err := prompts.Render(body, vars)
	var unknown *prompts.UnknownVarsError
	if errors.As(err, &unknown) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": unknown.Error(), "unknown": unknown.Names})
		return
	}
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if in.Use {
		if err := s.st.MarkPromptUsed(slug, s.now()); err != nil {
			writeStoreError(w, err, msgPromptNotFound)
			return
		}
		s.publish("prompts")
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}
