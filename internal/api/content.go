package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// Conteúdo do card (decisões, arquivos essenciais, pendências, perguntas).
// Os handlers add* servem a UI (sessionID "") e as rotas do agente em me.go.

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id inválido")
		return 0, false
	}
	return id, true
}

// withCardKey resolve {key} e chama h com o card.
func (s *server) withCardKey(h func(http.ResponseWriter, *http.Request, domain.Card, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := s.st.GetCard(r.PathValue("key"))
		if err != nil {
			writeStoreError(w, err, msgCardNotFound)
			return
		}
		h(w, r, c, "")
	}
}

func (s *server) addDecision(w http.ResponseWriter, r *http.Request, c domain.Card, sessionID string) {
	var in struct{ What, Why string }
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.What) == "" {
		writeError(w, http.StatusBadRequest, "decisão obrigatória")
		return
	}
	d, err := s.st.AddDecision(domain.Decision{CardID: c.ID, What: in.What, Why: in.Why, SessionID: sessionID, At: s.now()})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.recordEvent(sessionID, domain.EvDecision, map[string]any{"what": in.What, "why": in.Why})
	s.publish("cards")
	writeJSON(w, http.StatusCreated, d)
}

func (s *server) addFile(w http.ResponseWriter, r *http.Request, c domain.Card, sessionID string) {
	var in struct{ Path, Why, Lines string }
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Path) == "" {
		writeError(w, http.StatusBadRequest, "caminho obrigatório")
		return
	}
	f := domain.EssentialFile{CardID: c.ID, Path: in.Path, Why: in.Why, Lines: in.Lines}
	if err := s.st.UpsertEssentialFile(f); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.recordEvent(sessionID, domain.EvEssentialFile, map[string]any{"path": in.Path, "why": in.Why, "lines": in.Lines})
	s.publish("cards")
	writeJSON(w, http.StatusCreated, f)
}

func (s *server) addTodo(w http.ResponseWriter, r *http.Request, c domain.Card, sessionID string) {
	var in struct{ Text string }
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "texto obrigatório")
		return
	}
	t, err := s.st.AddTodo(domain.Todo{CardID: c.ID, Text: in.Text, SessionID: sessionID})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.recordEvent(sessionID, domain.EvTodo, map[string]any{"id": t.ID, "text": t.Text})
	s.publish("cards")
	writeJSON(w, http.StatusCreated, t)
}

func (s *server) addQuestion(w http.ResponseWriter, r *http.Request, c domain.Card, sessionID string) {
	var in struct{ Text string }
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "pergunta obrigatória")
		return
	}
	q, err := s.st.AddQuestion(domain.Question{CardID: c.ID, Text: in.Text, SessionID: sessionID})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.recordEvent(sessionID, domain.EvQuestion, map[string]any{"id": q.ID, "text": q.Text})
	s.publish("cards")
	writeJSON(w, http.StatusCreated, q)
}

func (s *server) patchTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Done *bool `json:"done"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Done == nil {
		writeError(w, http.StatusBadRequest, "campo done obrigatório")
		return
	}
	if err := s.st.SetTodoDone(id, *in.Done); err != nil {
		writeStoreError(w, err, "pendência não encontrada")
		return
	}
	s.publish("cards")
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "done": *in.Done})
}

func (s *server) patchQuestion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.AnswerQuestion(id, in.Answer); err != nil {
		writeStoreError(w, err, "pergunta não encontrada")
		return
	}
	s.publish("cards")
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "answer": in.Answer})
}
