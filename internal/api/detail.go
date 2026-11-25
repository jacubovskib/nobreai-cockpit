package api

import (
	"errors"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// CardDetail é o documento de handoff completo de um card.
type CardDetail struct {
	domain.Card
	// ProjectName é o nome exibido do projeto ("loja › web" num sub-projeto).
	ProjectName string                 `json:"project_name"`
	Content     domain.Content         `json:"content"`
	Sessions    []SessionWithUsage     `json:"sessions"`
	Readiness   domain.ReadinessResult `json:"readiness"`
	Totals      store.Total            `json:"totals"`
	// Live é a sessão viva dona do card (não encerrada e com processo
	// existindo); nil se não houver.
	Live *domain.Session `json:"live"`
	// TouchedFiles são os arquivos lidos/editados (eventos dos hooks).
	TouchedFiles []store.TouchedFile `json:"touched_files"`
	// ParentKey/ParentTitle: o épico pai ("" sem pai). Children, ChildrenTotals,
	// TreeTotals e ChildCards só existem no épico (nil nos outros cards).
	ParentKey      string        `json:"parent_key"`
	ParentTitle    string        `json:"parent_title"`
	Children       *EpicProgress `json:"children"`
	ChildrenTotals *store.Total  `json:"children_totals"`
	TreeTotals     *store.Total  `json:"tree_totals"`
	ChildCards     []CardSummary `json:"child_cards"`
}

// CardSummary é o card como aparece no board.
type CardSummary struct {
	domain.Card
	// Agent é o agente da sessão mais recente ("" se nunca houve sessão).
	Agent        string `json:"agent"`
	SessionCount int    `json:"session_count"`
	// Live: uma sessão viva (não encerrada e com processo existindo) é dona do card.
	Live bool `json:"live"`
	// PIDDeadAt: sem sessão viva, quando o processo da sessão dona (ainda na
	// carência, não encerrada) sumiu; mostrado como "processo encerrado há N min".
	PIDDeadAt *time.Time `json:"pid_dead_at"`
	// LastActivityAt é o último sinal de qualquer sessão do card (fim ou
	// último evento), para "sem sessão há <n> min".
	LastActivityAt *time.Time             `json:"last_activity_at"`
	Totals         store.Total            `json:"totals"`
	Readiness      domain.ReadinessResult `json:"readiness"`
	// Signal (domain.CardStatusSignal): "bloqueado" (Pausado ou Backlog com
	// bloqueador existente e não concluído), "pronto_para_retomar" (bloqueador
	// concluído) e "aguardando_voce" (Pausado com pergunta aberta) valem na
	// hora; "parado" (Em andamento ou Pausado) e "aguardando_revisao" (Revisão)
	// quando a última atividade (sessões ou mudança do card) passou de
	// domain.StaleAfter. SignalSince é essa atividade.
	Signal      domain.Signal `json:"signal"`
	SignalSince *time.Time    `json:"signal_since"`
	// Plan são as tarefas do plano de implementação (nil sem tarefas).
	Plan *domain.Plan `json:"plan"`
	// ParentKey/ParentTitle: o épico pai ("" sem pai). Children (progresso),
	// ChildrenTotals (uso das filhas) e TreeTotals (épico + filhas) só no épico.
	ParentKey      string        `json:"parent_key"`
	ParentTitle    string        `json:"parent_title"`
	Children       *EpicProgress `json:"children"`
	ChildrenTotals *store.Total  `json:"children_totals"`
	TreeTotals     *store.Total  `json:"tree_totals"`
	// WaitingOnYou (domain.WaitingOnYou): o card espera uma ação sua;
	// WaitingReason diz qual.
	WaitingOnYou  bool   `json:"waiting_on_you"`
	WaitingReason string `json:"waiting_reason"`
}

// cardDetail carrega conteúdo, sessões (com uso), sessão viva e prontidão do card.
func (s *server) cardDetail(c domain.Card) (CardDetail, error) {
	c.Labels = nonNil(c.Labels)
	d := CardDetail{Card: c}
	var err error
	if d.ProjectName, err = s.projectLabel(c.ProjectID); err != nil {
		return d, err
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		return d, err
	}
	d.Content = domain.Content{
		Decisions: nonNil(ct.Decisions), Files: nonNil(ct.Files),
		Todos: nonNil(ct.Todos), Questions: nonNil(ct.Questions),
		Plan: ct.Plan,
	}
	sessions, err := s.st.SessionsForCard(c.ID)
	if err != nil {
		return d, err
	}
	// Uso de cada sessão só naquele card: o que ela gastou em outro card fica lá.
	bySession, err := s.totalsWhere(store.TotalsFilter{GroupBy: "session", CardID: &c.ID})
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
	ids := make([]string, len(sessions))
	for i, x := range sessions {
		ids[i] = x.ID
	}
	projects, err := s.st.SessionProjects(ids)
	if err != nil {
		return d, err
	}
	d.Sessions = withUsage(sessions, bySession, projects)
	d.Totals = byCard[c.Key]
	for i := range sessions {
		if owns(sessions[i], c.ID) && sessions[i].Live() {
			d.Live = &sessions[i]
		}
	}
	v, err := s.hierarchyView(c, byCard)
	if err != nil {
		return d, err
	}
	d.ParentKey, d.ParentTitle = v.ParentKey, v.ParentTitle
	d.Children, d.ChildrenTotals, d.TreeTotals = v.Progress, v.ChildrenTotals, v.TreeTotals
	if c.Type == domain.TypeEpico {
		d.ChildCards = make([]CardSummary, 0, len(v.Children))
		for _, ch := range v.Children {
			sum, err := s.cardSummary(ch, byCard)
			if err != nil {
				return d, err
			}
			d.ChildCards = append(d.ChildCards, sum)
		}
	}
	d.Readiness, err = s.readiness(c, d.Content, d.Live != nil)
	return d, err
}

func (s *server) readiness(c domain.Card, ct domain.Content, live bool) (domain.ReadinessResult, error) {
	if c.Type == domain.TypeEpico {
		// O épico não é retomado: a prontidão é das filhas.
		return domain.ReadinessResult{Items: []domain.ReadinessItem{}}, nil
	}
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

// owns diz se a sessão está ligada ao card agora (e não só no histórico dele).
func owns(x domain.Session, cardID int64) bool { return x.CardID != nil && *x.CardID == cardID }

// cardSummary monta o card do board; byCard é o uso por card (para o card e,
// num épico, as filhas).
func (s *server) cardSummary(c domain.Card, byCard map[string]store.Total) (CardSummary, error) {
	c.Labels = nonNil(c.Labels)
	sum := CardSummary{Card: c, Totals: byCard[c.Key]}
	v, err := s.hierarchyView(c, byCard)
	if err != nil {
		return sum, err
	}
	sum.ParentKey, sum.ParentTitle = v.ParentKey, v.ParentTitle
	sum.Children, sum.ChildrenTotals, sum.TreeTotals = v.Progress, v.ChildrenTotals, v.TreeTotals
	sessions, err := s.st.SessionsForCard(c.ID)
	if err != nil {
		return sum, err
	}
	sum.SessionCount = len(sessions)
	for _, x := range sessions {
		sum.Agent = x.Agent
		if !owns(x, c.ID) {
			// Sessão só no histórico: sua atividade agora é em outro card.
			continue
		}
		if x.Live() {
			sum.Live = true
		} else if x.EndedAt == nil && x.PIDDeadAt != nil {
			sum.PIDDeadAt = x.PIDDeadAt
		}
		last := x.LastEventAt
		if x.EndedAt != nil && x.EndedAt.After(last) {
			last = *x.EndedAt
		}
		if sum.LastActivityAt == nil || last.After(*sum.LastActivityAt) {
			sum.LastActivityAt = &last
		}
	}
	if sum.Live {
		sum.PIDDeadAt = nil
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		return sum, err
	}
	sum.Plan = ct.Plan
	if sum.Readiness, err = s.readiness(c, ct, sum.Live); err != nil {
		return sum, err
	}
	last := c.UpdatedAt
	if sum.LastActivityAt != nil && sum.LastActivityAt.After(last) {
		last = *sum.LastActivityAt
	}
	in := domain.SignalInput{Column: c.Column, LastActivity: last, Now: s.now(), BlockedBy: c.BlockedBy}
	for _, q := range ct.Questions {
		if q.Answer == "" {
			in.OpenQuestions++
		}
	}
	if c.BlockedBy != "" {
		switch b, err := s.st.GetCard(c.BlockedBy); {
		case err == nil:
			in.BlockerExists, in.BlockerConcluded = true, b.Column == domain.Concluido
		case !errors.Is(err, store.ErrNotFound):
			return sum, err
		}
	}
	sig := domain.CardStatusSignal(in)
	if c.Type == domain.TypeEpico && sig == domain.SignalParado {
		// O épico não tem sessão própria: a atividade está nas filhas.
		sig = domain.SignalNone
	}
	// Esperando você usa o sinal cru: pergunta aberta conta mesmo com sessão viva.
	sum.WaitingOnYou, sum.WaitingReason = domain.WaitingOnYou(domain.WaitingInput{
		Column: c.Column, OpenQuestions: in.OpenQuestions, HandoffIncomplete: c.HandoffIncomplete,
		Ready: sum.Readiness.Ready, PauseReason: c.PauseReason, Signal: sig,
	})
	switch sig {
	case domain.SignalBloqueado, domain.SignalProntoParaRetomar:
		// Valem na hora e também com sessão viva; o tempo é o da pausa.
		sum.Signal, sum.SignalSince = sig, &c.UpdatedAt
	case domain.SignalAguardandoVoce:
		if !sum.Live {
			sum.Signal, sum.SignalSince = sig, &c.UpdatedAt
		}
	default:
		// Sessão viva = o agente está ali; ociosa não vira "parado".
		if !sum.Live && sig != domain.SignalNone {
			sum.Signal, sum.SignalSince = sig, &last
		}
	}
	return sum, err
}
