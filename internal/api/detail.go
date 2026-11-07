package api

import (
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// CardDetail é o documento de handoff completo de um card.
type CardDetail struct {
	domain.Card
	Content   domain.Content         `json:"content"`
	Sessions  []SessionWithUsage     `json:"sessions"`
	Readiness domain.ReadinessResult `json:"readiness"`
	Totals    store.Total            `json:"totals"`
	Live      *domain.Session        `json:"live"`
	// TouchedFiles são os arquivos lidos/editados (eventos dos hooks).
	TouchedFiles []store.TouchedFile `json:"touched_files"`
}

// CardSummary é o card como aparece no board.
type CardSummary struct {
	domain.Card
	// Agent é o agente da sessão mais recente ("" se nunca houve sessão).
	Agent        string `json:"agent"`
	SessionCount int    `json:"session_count"`
	Live         bool   `json:"live"`
	// LastActivityAt é o último sinal de qualquer sessão do card (fim ou
	// último evento), para "sem sessão há <n> min".
	LastActivityAt *time.Time             `json:"last_activity_at"`
	Totals         store.Total            `json:"totals"`
	Readiness      domain.ReadinessResult `json:"readiness"`
}

// cardDetail carrega conteúdo, sessões (com uso), sessão viva e prontidão do card.
func (s *server) cardDetail(c domain.Card) (CardDetail, error) {
	d := CardDetail{Card: c}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		return d, err
	}
	d.Content = domain.Content{
		Decisions: nonNil(ct.Decisions), Files: nonNil(ct.Files),
		Todos: nonNil(ct.Todos), Questions: nonNil(ct.Questions),
	}
	sessions, err := s.st.SessionsForCard(c.ID)
	if err != nil {
		return d, err
	}
	bySession, err := s.totalsBy("session")
	if err != nil {
		return d, err
	}
	byCard, err := s.totalsBy("card")
	if err != nil {
		return d, err
	}
	touched, err := s.st.TouchedFiles(c.ID)
	if err != nil {
		return d, err
	}
	d.TouchedFiles = touched
	d.Sessions = withUsage(sessions, bySession)
	d.Totals = byCard[c.Key]
	for i := range sessions {
		if sessions[i].EndedAt == nil {
			d.Live = &sessions[i]
		}
	}
	d.Readiness, err = s.readiness(c, d.Content, d.Live != nil)
	return d, err
}

func (s *server) readiness(c domain.Card, ct domain.Content, live bool) (domain.ReadinessResult, error) {
	set, err := s.st.GetSettings()
	if err != nil {
		return domain.ReadinessResult{}, err
	}
	lastEdit, err := s.st.LastEventAt(c.ID, domain.EvFileEdit)
	if err != nil {
		return domain.ReadinessResult{}, err
	}
	r := domain.Readiness(c, ct, live, lastEdit, set, s.now())
	r.Items = nonNil(r.Items)
	return r, nil
}

// resumeMarkdown monta o pacote de retomada; o diff é desde o HEAD da sessão
// mais recente, no diretório do projeto.
func (s *server) resumeMarkdown(d CardDetail) (string, error) {
	var sessions []domain.Session
	var totals []store.Total
	for _, x := range d.Sessions {
		sessions = append(sessions, x.Session)
		if x.Usage.Key != "" {
			totals = append(totals, x.Usage)
		}
	}
	diff := ""
	if s.deps.Git != nil {
		p, err := s.projectByID(d.ProjectID)
		if err != nil {
			return "", err
		}
		if p != nil && p.Path != "" {
			since := ""
			if n := len(sessions); n > 0 {
				since = sessions[n-1].GitHead
			}
			diff = s.deps.Git.DiffStat(p.Path, since)
		}
	}
	return resume.Build(d.Card, d.Content, sessions, totals, diff), nil
}

func (s *server) cardSummary(c domain.Card, totals store.Total) (CardSummary, error) {
	sum := CardSummary{Card: c, Totals: totals}
	sessions, err := s.st.SessionsForCard(c.ID)
	if err != nil {
		return sum, err
	}
	sum.SessionCount = len(sessions)
	for _, x := range sessions {
		sum.Agent = x.Agent
		if x.EndedAt == nil {
			sum.Live = true
		}
		last := x.LastEventAt
		if x.EndedAt != nil && x.EndedAt.After(last) {
			last = *x.EndedAt
		}
		if sum.LastActivityAt == nil || last.After(*sum.LastActivityAt) {
			sum.LastActivityAt = &last
		}
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		return sum, err
	}
	sum.Readiness, err = s.readiness(c, ct, sum.Live)
	return sum, err
}
