package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/scan"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

const (
	scanSessionDays = 30
	scanMaxSessions = 10
)

type scanSession struct {
	ID        string     `json:"id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Branch    string     `json:"branch"`
	Cwd       string     `json:"cwd"`
	CostUSD   float64    `json:"cost_usd"`
}

type scanCard struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Column string `json:"column"`
	Branch string `json:"branch"`
}

// scanProject: GET /api/projects/{id}/scan, o levantamento só de leitura do
// histórico do projeto (branches, planos, sessões com custo e cards atuais).
func (s *server) scanProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "projeto inválido")
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
	if p.Path == "" {
		writeError(w, http.StatusBadRequest, "grupo não tem repositório; rode o levantamento em cada sub-projeto")
		return
	}
	// App de monorepo: o levantamento roda na pasta do pai.
	repo, err := s.st.RepoProject(*p)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	family, err := s.st.FamilyIDs(p.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	now := s.now()
	res, err := scan.Run(repo.Path, board.ProjectBase(s.branchReader(), repo), now)
	var be *scan.BaseError
	if errors.As(err, &be) {
		writeError(w, http.StatusConflict, be.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "levantamento falhou: "+err.Error())
		return
	}
	since := now.AddDate(0, 0, -scanSessionDays)
	sessions := []scanSession{}
	ss, err := s.st.ProjectsSessionsSince(family, since)
	if err == nil {
		var costs map[string]store.Total
		costs, err = s.totalsWhere(store.TotalsFilter{GroupBy: "session", From: since, ProjectIDs: family})
		for _, x := range ss {
			if c := costs[x.ID].CostUSD; c > 0 {
				sessions = append(sessions, scanSession{x.ID, x.StartedAt, x.EndedAt, x.Branch, x.Cwd, c})
			}
		}
	}
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].CostUSD > sessions[j].CostUSD })
	if len(sessions) > scanMaxSessions {
		sessions = sessions[:scanMaxSessions]
	}
	cs, err := s.st.ListCards(store.CardFilter{ProjectIDs: family})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	cards := make([]scanCard, 0, len(cs))
	for _, c := range cs {
		cards = append(cards, scanCard{c.Key, c.Title, string(c.Column), c.Branch})
	}
	sugg := []workspaces.Suggestion{}
	if p.ParentID == nil { // um nível só: um sub-projeto não recebe sugestões
		if sugg, err = s.openSuggestions(repo); err != nil {
			writeStoreError(w, err, "")
			return
		}
	}
	writeJSON(w, http.StatusOK, struct {
		scan.Result
		Sessions    []scanSession           `json:"sessions"`
		Cards       []scanCard              `json:"cards"`
		Suggestions []workspaces.Suggestion `json:"suggestions"`
	}{res, sessions, cards, sugg})
}
