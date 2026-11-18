package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const (
	msgCardNotFound        = "card não encontrado"
	msgInProgressNeedsLive = "Em andamento só com uma sessão viva — use Continuar com… ou nobre resume"
)

func (s *server) cardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cards", s.listCards)
	mux.HandleFunc("POST /api/cards", s.createCard)
	mux.HandleFunc("GET /api/cards/{key}", s.getCard)
	mux.HandleFunc("PATCH /api/cards/{key}", s.patchCard)
	mux.HandleFunc("POST /api/cards/{key}/attach", s.attachCard)
	mux.HandleFunc("POST /api/cards/{key}/continue", s.continueCard)
	mux.HandleFunc("POST /api/cards/{key}/continue/preview", s.continuePreview)
	mux.HandleFunc("GET /api/cards/{key}/resume", s.cardResume)
	mux.HandleFunc("POST /api/cards/{key}/decisions", s.withCardKey(s.addDecision))
	mux.HandleFunc("POST /api/cards/{key}/files", s.withCardKey(s.addFile))
	mux.HandleFunc("POST /api/cards/{key}/todos", s.withCardKey(s.addTodo))
	mux.HandleFunc("POST /api/cards/{key}/questions", s.withCardKey(s.addQuestion))
	mux.HandleFunc("PATCH /api/todos/{id}", s.patchTodo)
	mux.HandleFunc("PATCH /api/questions/{id}", s.patchQuestion)
}

func validColumn(c domain.Column) bool {
	for _, x := range domain.Columns {
		if x == c {
			return true
		}
	}
	return false
}

// msgGroupNoBranch recusa branch num card de grupo.
const msgGroupNoBranch = "grupo não tem branch"

// msgGroupNoCards recusa um grupo sem pasta como destino de card.
const msgGroupNoCards = "um grupo não recebe cards; escolha um dos repositórios"

func (s *server) projectByID(id int64) (*domain.Project, error) {
	ps, err := s.st.ListProjects()
	if err != nil {
		return nil, err
	}
	for i := range ps {
		if ps[i].ID == id {
			return &ps[i], nil
		}
	}
	return nil, nil
}

// projectLabel é o nome exibido do projeto: "pai › filho" num sub-projeto.
func (s *server) projectLabel(id int64) (string, error) {
	p, err := s.projectByID(id)
	if err != nil || p == nil {
		return "", err
	}
	var parent *domain.Project
	if p.ParentID != nil {
		if parent, err = s.projectByID(*p.ParentID); err != nil {
			return "", err
		}
	}
	return store.DisplayName(*p, parent), nil
}

func (s *server) listCards(w http.ResponseWriter, r *http.Request) {
	var f store.CardFilter
	if p := r.URL.Query().Get("project"); p != "" {
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "projeto inválido")
			return
		}
		f.ProjectID = id
	}
	f.Agent = r.URL.Query().Get("agent")
	cards, err := s.st.ListCards(f)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	byCard, err := s.totalsBy("card")
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	out := make([]CardSummary, 0, len(cards))
	for _, c := range cards {
		sum, err := s.cardSummary(c, byCard[c.Key])
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		out = append(out, sum)
	}
	writeJSON(w, http.StatusOK, out)
}

type cardInput struct {
	Title     string `json:"title"`
	Goal      string `json:"goal"`
	DoneWhen  string `json:"done_when"`
	HowToTest string `json:"how_to_test"`
	Branch    string `json:"branch"`
	ProjectID int64  `json:"project_id"`
}

func (s *server) createCard(w http.ResponseWriter, r *http.Request) {
	var in cardInput
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		writeError(w, http.StatusBadRequest, "título obrigatório")
		return
	}
	if in.ProjectID == 0 {
		writeError(w, http.StatusBadRequest, "projeto obrigatório")
		return
	}
	p, err := s.projectByID(in.ProjectID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if p == nil {
		writeError(w, http.StatusBadRequest, "projeto não encontrado")
		return
	}
	if p.IsGroup() && p.Path == "" {
		writeError(w, http.StatusBadRequest, msgGroupNoCards)
		return
	}
	if p.IsGroup() && strings.TrimSpace(in.Branch) != "" {
		writeError(w, http.StatusBadRequest, msgGroupNoBranch)
		return
	}
	c, err := s.st.CreateCard(domain.Card{Title: in.Title, Goal: in.Goal, DoneWhen: in.DoneWhen,
		HowToTest: in.HowToTest, Branch: in.Branch, ProjectID: in.ProjectID})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("cards")
	writeJSON(w, http.StatusCreated, c)
}

func (s *server) getCard(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetCard(r.PathValue("key"))
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	d, err := s.cardDetail(c)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) patchCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title        *string        `json:"title"`
		Goal         *string        `json:"goal"`
		DoneWhen     *string        `json:"done_when"`
		HowToTest    *string        `json:"how_to_test"`
		WhereStopped *string        `json:"where_stopped"`
		Column       *domain.Column `json:"column"`
		BlockedBy    *string        `json:"blocked_by"`
		ProjectID    *int64         `json:"project_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	c, err := s.st.GetCard(r.PathValue("key"))
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	if in.Column != nil {
		if !validColumn(*in.Column) {
			writeError(w, http.StatusBadRequest, "coluna inválida")
			return
		}
		// Em andamento é "uma sessão viva é dona do card": mover para lá à mão
		// sem sessão viva deixaria o card preso, sem nada que o reconcilie.
		if *in.Column == domain.EmAndamento && c.Column != domain.EmAndamento {
			live, err := s.st.HasLiveSession(c.ID)
			if err != nil {
				writeStoreError(w, err, "")
				return
			}
			if !live {
				writeError(w, http.StatusConflict, msgInProgressNeedsLive)
				return
			}
		}
		// Mudar de coluna derruba o motivo da pausa e, saindo de Pausado, a
		// marca de handoff incompleto (ela só vale enquanto o card está pausado).
		if *in.Column != c.Column {
			c.PauseReason, c.HandoffIncomplete = "", false
			if *in.Column == domain.EmAndamento {
				c.BlockedBy = ""
			}
		}
		c.Column = *in.Column
	}
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			writeError(w, http.StatusBadRequest, "título obrigatório")
			return
		}
		c.Title = *in.Title
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	if in.BlockedBy != nil {
		key := normKey(*in.BlockedBy)
		if key != "" {
			if key == normKey(c.Key) {
				writeError(w, http.StatusBadRequest, "um card não pode bloquear a si mesmo")
				return
			}
			b, err := s.st.GetCard(key)
			if err != nil {
				writeStoreError(w, err, "card "+key+" não encontrado")
				return
			}
			key = b.Key
		}
		c.BlockedBy = key
	}
	set(&c.Goal, in.Goal)
	set(&c.DoneWhen, in.DoneWhen)
	set(&c.HowToTest, in.HowToTest)
	if in.WhereStopped != nil {
		now := s.now()
		c.WhereStopped, c.WhereStoppedAt = *in.WhereStopped, &now
	}
	if in.ProjectID != nil && *in.ProjectID != c.ProjectID {
		if dest, err := s.projectByID(*in.ProjectID); err != nil {
			writeStoreError(w, err, "")
			return
		} else if dest != nil && dest.IsGroup() && dest.Path == "" {
			writeError(w, http.StatusBadRequest, msgGroupNoCards)
			return
		}
		err := s.st.SetCardProject(c.ID, *in.ProjectID)
		switch {
		case errors.Is(err, store.ErrFamily):
			writeError(w, http.StatusBadRequest, "o card só pode ir para um projeto da mesma família")
			return
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "projeto não encontrado")
			return
		case err != nil:
			writeStoreError(w, err, "")
			return
		}
		c.ProjectID = *in.ProjectID
	}
	if err := s.st.UpdateCard(c); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

// normKey normaliza a chave de card digitada (" nac-2 " → "NAC-2").
func normKey(k string) string { return strings.ToUpper(strings.TrimSpace(k)) }

// writeCard responde com o card atual.
func (s *server) writeCard(w http.ResponseWriter, code int, key string) {
	c, err := s.st.GetCard(key)
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	writeJSON(w, code, c)
}

func busyMessage(key string) string {
	return key + " já tem uma sessão viva. Encerre-a ou use forçar para assumir o card."
}

func (s *server) attachCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SessionID string `json:"session_id"`
		Force     bool   `json:"force"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	key := r.PathValue("key")
	if in.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id obrigatório")
		return
	}
	if err := s.st.AttachSession(in.SessionID, key, in.Force); err != nil {
		if errors.Is(err, store.ErrCardBusy) {
			writeError(w, http.StatusConflict, busyMessage(key))
			return
		}
		writeStoreError(w, err, "sessão ou card não encontrado")
		return
	}
	s.publish("cards", "sessions")
	s.writeCard(w, http.StatusOK, key)
}

func (s *server) cardResume(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetCard(r.PathValue("key"))
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	md, err := resume.ForCard(s.st, s.deps.Git, c, "")
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"markdown": md})
}
