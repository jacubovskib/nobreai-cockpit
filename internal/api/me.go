package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Rotas do agente: a sessão é a sessão viva do processo informado em
// X-Nobre-Agent-PID (o CLI `nobre` descobre o PID do agente ancestral).

const (
	agentPIDHeader = "X-Nobre-Agent-PID"
	msgNoSession   = "nenhuma sessão de agente encontrada para este processo"
	msgNoCard      = `Esta sessão ainda não tem card. Use nobre start "<título>" ou nobre resume <KEY>.`
	msgNoProject   = "Este repositório não está cadastrado no NobreAI Cockpit. Cadastre com nobre project add e faça o levantamento com nobre project scan (veja a skill nobre-cockpit)."
)

func (s *server) meRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/me/start", s.withSession(s.meStart))
	mux.HandleFunc("POST /api/me/cards", s.withSession(s.meCards))
	mux.HandleFunc("POST /api/me/resume", s.withSession(s.meResume))
	mux.HandleFunc("GET /api/me/status", s.withSession(s.meStatus))
	mux.HandleFunc("POST /api/me/note", s.withSessionCard(s.meNote))
	mux.HandleFunc("POST /api/me/decide", s.withSessionCard(s.meContent(s.addDecision)))
	mux.HandleFunc("POST /api/me/file", s.withSessionCard(s.meContent(s.addFile)))
	mux.HandleFunc("POST /api/me/todo", s.withSessionCard(s.meContent(s.addTodo)))
	mux.HandleFunc("POST /api/me/ask", s.withSessionCard(s.meContent(s.addQuestion)))
	mux.HandleFunc("POST /api/me/answer", s.withSessionCard(s.meAnswer))
	mux.HandleFunc("POST /api/me/todo-done", s.withSessionCard(s.meTodoDone))
	mux.HandleFunc("POST /api/me/test", s.withSessionCard(s.meTest))
	mux.HandleFunc("POST /api/me/branch", s.withSessionCard(s.meBranch))
	mux.HandleFunc("POST /api/me/done", s.withSessionCard(s.meDone))
}

type sessionHandler func(http.ResponseWriter, *http.Request, domain.Session)
type sessionCardHandler func(http.ResponseWriter, *http.Request, domain.Session, domain.Card)

func (s *server) withSession(h sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid, err := strconv.Atoi(r.Header.Get(agentPIDHeader))
		if err != nil || pid <= 0 {
			writeError(w, http.StatusBadRequest, "cabeçalho "+agentPIDHeader+" ausente ou inválido")
			return
		}
		sess, err := s.st.SessionByPID(pid)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, msgNoSession)
			return
		}
		h(w, r, *sess)
	}
}

func (s *server) withSessionCard(h sessionCardHandler) http.HandlerFunc {
	return s.withSession(func(w http.ResponseWriter, r *http.Request, sess domain.Session) {
		c, ok := s.sessionCard(w, sess)
		if ok {
			h(w, r, sess, c)
		}
	})
}

// sessionCard devolve o card da sessão ou responde 409.
func (s *server) sessionCard(w http.ResponseWriter, sess domain.Session) (domain.Card, bool) {
	if sess.CardID == nil {
		writeError(w, http.StatusConflict, msgNoCard)
		return domain.Card{}, false
	}
	c, err := s.st.GetCardByID(*sess.CardID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusConflict, msgNoCard)
		return c, false
	}
	if err != nil {
		writeStoreError(w, err, "")
		return c, false
	}
	return c, true
}

// meContent adapta um handler de conteúdo do card para a sessão do agente.
func (s *server) meContent(h func(http.ResponseWriter, *http.Request, domain.Card, string)) sessionCardHandler {
	return func(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
		h(w, r, c, sess.ID)
	}
}

// recordEvent grava um evento da sessão (no card atual dela) e atualiza a
// última atividade. Sem sessão (escrita do humano) não faz nada. Falha aqui
// não desfaz a escrita principal: só é registrada no log.
func (s *server) recordEvent(sessionID, kind string, data map[string]any) {
	if sessionID == "" {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		log.Printf("evento %s: %v", kind, err)
		return
	}
	now := s.now()
	if err := s.st.AddEvent(domain.Event{SessionID: sessionID, Kind: kind, Data: b, At: now}); err != nil {
		log.Printf("evento %s da sessão %s: %v", kind, sessionID, err)
		return
	}
	if err := s.st.TouchSession(sessionID, now); err != nil {
		log.Printf("atualizar sessão %s: %v", sessionID, err)
	}
	s.publish("sessions")
}

func (s *server) meStart(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Title    string `json:"title"`
		Goal     string `json:"goal"`
		DoneWhen string `json:"done_when"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		writeError(w, http.StatusBadRequest, "título obrigatório")
		return
	}
	var projectID int64
	if sess.ProjectID != nil {
		projectID = *sess.ProjectID
	} else {
		p, err := s.st.ProjectForPath(sess.Cwd)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if p == nil {
			writeError(w, http.StatusConflict, msgNoProject)
			return
		}
		projectID = p.ID
	}
	c, err := s.st.CreateCardAndAttach(domain.Card{Title: in.Title, Goal: in.Goal, DoneWhen: in.DoneWhen,
		ProjectID: projectID}, sess.ID) // o branch vem do syncBranch: a base nunca vira branch do card
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.syncBranch(c.ID, sess.Branch)
	s.publish("cards", "sessions")
	s.writeCard(w, http.StatusCreated, c.Key)
}

// meCards cria um card no projeto da sessão sem ligar a sessão a ele (o
// agente cadastra o histórico do repositório sem sair do card atual).
func (s *server) meCards(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Title        string `json:"title"`
		Goal         string `json:"goal"`
		DoneWhen     string `json:"done_when"`
		Branch       string `json:"branch"`
		WhereStopped string `json:"where_stopped"`
		Column       string `json:"column"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		writeError(w, http.StatusBadRequest, "título obrigatório")
		return
	}
	col := domain.Column(in.Column)
	switch col {
	case "":
		col = domain.Backlog
	case domain.Backlog, domain.Pausado:
	default:
		writeError(w, http.StatusBadRequest, "coluna inválida (use backlog ou pausado)")
		return
	}
	var projectID int64
	if sess.ProjectID != nil {
		projectID = *sess.ProjectID
	} else {
		p, err := s.st.ProjectForPath(sess.Cwd)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if p == nil {
			writeError(w, http.StatusConflict, msgNoProject)
			return
		}
		projectID = p.ID
	}
	card := domain.Card{Title: in.Title, Goal: in.Goal, DoneWhen: in.DoneWhen, Branch: strings.TrimSpace(in.Branch),
		ProjectID: projectID, Column: col}
	if w := strings.TrimSpace(in.WhereStopped); w != "" {
		now := s.now()
		card.WhereStopped, card.WhereStoppedAt = w, &now
	}
	c, err := s.st.CreateCard(card)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("cards")
	s.writeCard(w, http.StatusCreated, c.Key)
}

func (s *server) meResume(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Key   string `json:"key"`
		Force bool   `json:"force"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Key == "" {
		writeError(w, http.StatusBadRequest, "chave do card obrigatória")
		return
	}
	if err := s.st.AttachSession(sess.ID, in.Key, in.Force); err != nil {
		if errors.Is(err, store.ErrCardBusy) {
			writeError(w, http.StatusConflict, busyMessage(in.Key))
			return
		}
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.publish("cards", "sessions")
	c, err := s.st.GetCard(in.Key)
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	// O card passa a acompanhar o branch de trabalho desta sessão.
	if s.syncBranch(c.ID, sess.Branch) {
		if c, err = s.st.GetCard(in.Key); err != nil {
			writeStoreError(w, err, msgCardNotFound)
			return
		}
	}
	// A base do diff é a sessão anterior: a desta chamada acabou de entrar.
	md, err := resume.ForCard(s.st, s.deps.Git, c, sess.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": c.Key, "markdown": md})
}

// branchReader é o Git do daemon como BranchReader (nil se não souber ler branches).
func (s *server) branchReader() gitinfo.BranchReader {
	br, _ := s.deps.Git.(gitinfo.BranchReader)
	return br
}

// syncBranch faz o branch do card acompanhar branch (ver board.SyncCardBranch).
// Falha só vai para o log; devolve se o branch do card mudou.
func (s *server) syncBranch(cardID int64, branch string) bool {
	changed, err := board.SyncCardBranch(s.st, s.branchReader(), cardID, branch)
	if err != nil {
		log.Printf("branch do card %d: %v", cardID, err)
		return false
	}
	if changed {
		s.publish("cards")
	}
	return changed
}

// meBranch troca o branch do card (o agente informa o branch de trabalho). A
// base do projeto é recusada: o card guarda o branch de trabalho.
func (s *server) meBranch(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Branch string `json:"branch"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	b := strings.TrimSpace(in.Branch)
	if b == "" {
		writeError(w, http.StatusBadRequest, "branch obrigatório")
		return
	}
	ps, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	for _, p := range ps {
		if p.ID == c.ProjectID && board.ProjectBase(s.branchReader(), p) == b {
			writeError(w, http.StatusConflict, fmt.Sprintf("%q é a base do projeto; o card fica com o branch de trabalho", b))
			return
		}
	}
	if _, err := board.SyncCardBranch(s.st, s.branchReader(), c.ID, b); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

func (s *server) meStatus(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	out := struct {
		Session domain.Session `json:"session"`
		Card    *CardDetail    `json:"card"`
	}{Session: sess}
	if sess.CardID != nil {
		c, ok := s.sessionCard(w, sess)
		if !ok {
			return
		}
		d, err := s.cardDetail(c)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		out.Card = &d
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) meNote(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "texto obrigatório")
		return
	}
	if err := s.st.SetWhereStopped(c.ID, in.Text, s.now()); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.recordEvent(sess.ID, domain.EvNote, map[string]any{"text": in.Text})
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

func (s *server) meTodoDone(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		ID int64 `json:"id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	found := false
	for _, t := range ct.Todos {
		found = found || t.ID == in.ID
	}
	if !found {
		writeError(w, http.StatusNotFound, "pendência não encontrada neste card")
		return
	}
	if err := s.st.SetTodoDone(in.ID, true); err != nil {
		writeStoreError(w, err, "pendência não encontrada")
		return
	}
	s.recordEvent(sess.ID, domain.EvTodo, map[string]any{"id": in.ID, "done": true})
	s.publish("cards")
	writeJSON(w, http.StatusOK, map[string]any{"id": in.ID, "done": true})
}

func (s *server) meAnswer(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		ID     int64  `json:"id"`
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Answer) == "" {
		writeError(w, http.StatusBadRequest, "resposta obrigatória")
		return
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	found := false
	for _, q := range ct.Questions {
		found = found || q.ID == in.ID
	}
	if !found {
		writeError(w, http.StatusNotFound, "pergunta não encontrada neste card")
		return
	}
	if err := s.st.AnswerQuestion(in.ID, in.Answer); err != nil {
		writeStoreError(w, err, "pergunta não encontrada")
		return
	}
	s.recordEvent(sess.ID, domain.EvAnswer, map[string]any{"id": in.ID, "answer": in.Answer})
	s.publish("cards")
	writeJSON(w, http.StatusOK, map[string]any{"id": in.ID, "answer": in.Answer})
}

func (s *server) meTest(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "texto obrigatório")
		return
	}
	c.HowToTest = in.Text
	if err := s.st.UpdateCard(c); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.recordEvent(sess.ID, domain.EvHowToTest, map[string]any{"text": in.Text})
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

// doneProblems lista o que impede o done: "Como testar" vazio ou anterior à
// última edição de arquivo, e perguntas sem resposta.
func (s *server) doneProblems(c domain.Card) ([]string, error) {
	var problems []string
	if strings.TrimSpace(c.HowToTest) == "" {
		problems = append(problems, `"Como testar" vazio: registre com nobre test`)
	} else {
		edit, err := s.st.LastEventAt(c.ID, domain.EvFileEdit)
		if err != nil {
			return nil, err
		}
		test, err := s.st.LastEventAt(c.ID, domain.EvHowToTest)
		if err != nil {
			return nil, err
		}
		if edit != nil && test != nil && edit.After(*test) {
			problems = append(problems, `"Como testar" é anterior à última edição de arquivo: atualize com nobre test`)
		}
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		return nil, err
	}
	open := 0
	for _, q := range ct.Questions {
		if strings.TrimSpace(q.Answer) == "" {
			open++
		}
	}
	if open > 0 {
		problems = append(problems, fmt.Sprintf("%d pergunta(s) sem resposta: responda com nobre answer <id>", open))
	}
	return problems, nil
}

// meDone aplica a transição AgentDone (em_andamento → revisao) e registra o
// evento done (mesmo se o card já não estava em andamento).
func (s *server) meDone(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Force bool `json:"force"`
	}
	if !readJSON(w, r, &in) { // corpo vazio é aceito (force=false)
		return
	}
	if !in.Force {
		problems, err := s.doneProblems(c)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if len(problems) > 0 {
			writeError(w, http.StatusConflict, strings.Join(problems, "; ")+" (ou nobre done --force)")
			return
		}
	}
	if next := domain.NextColumn(c.Column, domain.AgentDone); next != c.Column {
		if err := s.st.MoveCard(c.Key, next); err != nil {
			writeStoreError(w, err, msgCardNotFound)
			return
		}
		s.publish("cards")
	}
	s.recordEvent(sess.ID, domain.EvDone, map[string]any{})
	s.writeCard(w, http.StatusOK, c.Key)
}
