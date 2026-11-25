package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// hierarchyInput são os campos de tipo, pai, prioridade, labels e points do
// card. Ausente não muda; parent_key "" tira o pai; points null tira a estimativa.
type hierarchyInput struct {
	Type      *string         `json:"type"`
	ParentKey *string         `json:"parent_key"`
	Priority  *string         `json:"priority"`
	Labels    *[]string       `json:"labels"`
	Points    json.RawMessage `json:"points"`
}

// wantsEpic diz se o pedido transforma o card em épico.
func (in hierarchyInput) wantsEpic() bool {
	return in.Type != nil && domain.CardType(*in.Type) == domain.TypeEpico
}

// applyHierarchy aplica o pedido ao card e valida as regras da hierarquia.
// Devolve (0, "") se ok ou o status HTTP e a mensagem do erro.
func (s *server) applyHierarchy(c *domain.Card, in hierarchyInput, isNew bool) (int, string) {
	if c.Type == "" {
		c.Type = domain.TypeTarefa
	}
	if c.Priority == "" {
		c.Priority = domain.PrioMedia
	}
	oldType := c.Type
	if isNew {
		// Card novo: "" faz CheckHierarchy tratar como quem passa a ser épico.
		oldType = ""
	}
	if in.Type != nil {
		c.Type = domain.CardType(*in.Type)
	}
	if in.Priority != nil {
		c.Priority = domain.Priority(*in.Priority)
	}
	if in.Labels != nil {
		labels, err := domain.NormalizeLabels(*in.Labels)
		if err != nil {
			return http.StatusBadRequest, err.Error()
		}
		c.Labels = labels
	}
	if len(in.Points) > 0 {
		var p *float64
		if err := json.Unmarshal(in.Points, &p); err != nil {
			return http.StatusBadRequest, "points inválido"
		}
		if err := domain.ValidPoints(p); err != nil {
			return http.StatusBadRequest, err.Error()
		}
		c.Points = p
	}
	var parent *domain.Card
	switch {
	case in.ParentKey != nil && normKey(*in.ParentKey) != "":
		key := normKey(*in.ParentKey)
		p, err := s.st.GetCard(key)
		if errors.Is(err, store.ErrNotFound) {
			return http.StatusNotFound, "card " + key + " não encontrado"
		}
		if err != nil {
			return http.StatusInternalServerError, "erro interno: " + err.Error()
		}
		parent = &p
	case in.ParentKey == nil && c.ParentID != nil:
		p, err := s.st.GetCardByID(*c.ParentID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return http.StatusInternalServerError, "erro interno: " + err.Error()
		}
		if err == nil {
			parent = &p
		}
	}
	h := domain.HierarchyCheck{Card: *c, OldType: oldType, Parent: parent, New: isNew}
	if !isNew {
		var err error
		if h.ChildCount, err = s.st.CountChildren(c.ID); err != nil {
			return http.StatusInternalServerError, "erro interno: " + err.Error()
		}
		if h.Live, err = s.st.HasLiveSession(c.ID); err != nil {
			return http.StatusInternalServerError, "erro interno: " + err.Error()
		}
	}
	if err := domain.CheckHierarchy(h); err != nil {
		code := http.StatusBadRequest
		if h.Live {
			// Sem a sessão viva passaria: o conflito é a sessão.
			h.Live = false
			if domain.CheckHierarchy(h) == nil {
				code = http.StatusConflict
			}
		}
		return code, err.Error()
	}
	if parent != nil {
		c.ParentID = &parent.ID
	} else {
		c.ParentID = nil
	}
	if c.Type == domain.TypeEpico && oldType != domain.TypeEpico {
		// O épico nunca pausa nem guarda branch; a coluna vem das filhas (nenhuma ainda).
		c.PauseReason, c.HandoffIncomplete, c.BlockedBy = "", false, ""
		c.Column = domain.EpicColumn(c.Column, nil)
		if !isNew && c.Branch != "" {
			if _, err := s.st.SetCardBranch(c.ID, ""); err != nil {
				return http.StatusInternalServerError, "erro interno: " + err.Error()
			}
		}
		c.Branch = ""
	}
	return 0, ""
}

// EpicProgress é o andamento das filhas de um épico.
type EpicProgress struct {
	Total int `json:"total"`
	Done  int `json:"done"`
}

// addTotals soma dois totais de uso; Unpriced é OU.
func addTotals(a, b store.Total) store.Total {
	a.Sessions += b.Sessions
	a.Input += b.Input
	a.Output += b.Output
	a.CacheWrite += b.CacheWrite
	a.CacheRead += b.CacheRead
	a.CostUSD += b.CostUSD
	a.Credits += b.Credits
	a.Unpriced = a.Unpriced || b.Unpriced
	return a
}

// epicView é o pai de um card e, num épico, as filhas com progresso e totais.
type epicView struct {
	ParentKey, ParentTitle string
	Children               []domain.Card
	Progress               *EpicProgress
	ChildrenTotals         *store.Total
	TreeTotals             *store.Total
}

// hierarchyView carrega o pai do card e, se for épico, as filhas com o uso
// delas (de byCard) somado.
func (s *server) hierarchyView(c domain.Card, byCard map[string]store.Total) (epicView, error) {
	var v epicView
	if c.ParentID != nil {
		switch p, err := s.st.GetCardByID(*c.ParentID); {
		case err == nil:
			v.ParentKey, v.ParentTitle = p.Key, p.Title
		case !errors.Is(err, store.ErrNotFound):
			return v, err
		}
	}
	if c.Type != domain.TypeEpico {
		return v, nil
	}
	children, err := s.st.ListChildren(c.ID)
	if err != nil {
		return v, err
	}
	v.Children = children
	v.Progress = &EpicProgress{Total: len(children)}
	var sum store.Total
	for _, ch := range children {
		if ch.Column == domain.Concluido {
			v.Progress.Done++
		}
		sum = addTotals(sum, byCard[ch.Key])
	}
	tree := addTotals(byCard[c.Key], sum)
	v.ChildrenTotals, v.TreeTotals = &sum, &tree
	return v, nil
}

// childColumns são as colunas das filhas do épico.
func (s *server) childColumns(epicID int64) ([]domain.Column, error) {
	children, err := s.st.ListChildren(epicID)
	if err != nil {
		return nil, err
	}
	cols := make([]domain.Column, len(children))
	for i, ch := range children {
		cols[i] = ch.Column
	}
	return cols, nil
}
