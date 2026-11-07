package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resources"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const (
	msgWorktreeNotFound = "worktree não encontrado"
	// detailSessions e detailEvents limitam o histórico do detalhe.
	detailSessions = 50
	detailEvents   = 40
	detailCommits  = 10
)

// WorktreeDetail é tudo o que o Cockpit sabe de um worktree: o mesmo item da
// lista (com recursos), todos os processos atribuídos a ele, o estado do git,
// as sessões sem limite de tempo (com uso), os cards do branch (inclusive
// concluídos) e os eventos recentes dessas sessões.
type WorktreeDetail struct {
	Project   domain.Project          `json:"project"`
	Worktree  WorktreeView            `json:"worktree"`
	Processes []ProcDetail            `json:"processes"`
	Git       *GitDetail              `json:"git"`
	Sessions  []WorktreeSessionDetail `json:"sessions"`
	Cards     []CardRef               `json:"cards"`
	Events    []WorktreeEvent         `json:"events"`
}

// ProcDetail é um processo do worktree com o diretório onde roda.
type ProcDetail struct {
	resources.Proc
	Cwd string `json:"cwd"`
}

// GitDetail é o estado git do worktree; Error explica quando não pôde ser
// lido (o diretório sumiu, por exemplo).
type GitDetail struct {
	gitinfo.Inspection
	Error string `json:"error"`
}

// WorktreeSessionDetail é uma sessão do worktree com o uso total dela.
type WorktreeSessionDetail struct {
	WorktreeSession
	Usage    store.Total `json:"usage"`
	Unpriced bool        `json:"unpriced"`
}

// WorktreeEvent é um evento de hook das sessões do worktree, já em texto.
type WorktreeEvent struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Agent     string    `json:"agent"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	At        time.Time `json:"at"`
}

// worktreeDetail: GET /api/worktrees/detail?project=<id>&path=<caminho>. O
// caminho precisa ser um dos worktrees que o git lista para o projeto.
func (s *server) worktreeDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "project inválido")
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path é obrigatório")
		return
	}
	path = filepath.Clean(path)
	p, err := s.projectByID(id)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "projeto não encontrado")
		return
	}
	pw, err := s.projectWorktrees(*p)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	idx := -1
	for i, v := range pw.Worktrees {
		if v.Path == path {
			idx = i
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, msgWorktreeNotFound)
		return
	}
	v := pw.Worktrees[idx]
	d := WorktreeDetail{Project: *p, Worktree: v, Processes: []ProcDetail{}, Sessions: []WorktreeSessionDetail{}, Cards: []CardRef{}, Events: []WorktreeEvent{}}

	// Processos: mesma lista de caminhos da página, para os números baterem.
	if s.deps.Resources != nil {
		procs, _ := s.deps.Resources.Snapshot()
		paths := make([]string, len(pw.Worktrees))
		for i, x := range pw.Worktrees {
			paths[i] = x.Path
		}
		for _, pr := range resources.Attribute(procs, paths)[strings.TrimRight(v.Path, "/")].All {
			d.Processes = append(d.Processes, ProcDetail{Proc: pr, Cwd: pr.Cwd})
		}
	}

	d.Git = s.worktreeGit(pw.Worktrees, v)

	// Sessões de qualquer época, do projeto, que rodam neste worktree.
	all, err := s.st.ProjectSessionsSince(p.ID, time.Time{})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	cards, err := s.st.ListCards(store.CardFilter{ProjectID: p.ID})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	byID := make(map[int64]domain.Card, len(cards))
	for _, c := range cards {
		byID[c.ID] = c
		if v.Branch != "" && c.Branch == v.Branch {
			d.Cards = append(d.Cards, *cardRef(c))
		}
	}
	bySession, err := s.totalsBy("session")
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	agentOf := map[string]string{}
	var ids []string
	for _, x := range all {
		if worktreeOf(pw.Worktrees, x) != idx {
			continue
		}
		agentOf[x.ID] = x.Agent
		ids = append(ids, x.ID)
		if len(d.Sessions) >= detailSessions {
			continue
		}
		ws := WorktreeSession{Session: x, Live: x.Live()}
		if x.CardID != nil {
			if c, ok := byID[*x.CardID]; ok {
				ws.Card = cardRef(c)
			}
		}
		if ws.Live {
			ev, err := s.st.LastSessionEvent(x.ID)
			if err != nil {
				writeStoreError(w, err, "")
				return
			}
			if ev != nil {
				ws.LastActivity = lastActivityText(*ev)
			}
		}
		u := bySession[x.ID]
		d.Sessions = append(d.Sessions, WorktreeSessionDetail{WorktreeSession: ws, Usage: u, Unpriced: u.Unpriced})
	}
	events, err := s.st.SessionEvents(ids, detailEvents)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	for _, e := range events {
		d.Events = append(d.Events, WorktreeEvent{ID: e.ID, SessionID: e.SessionID, Agent: agentOf[e.SessionID], Kind: e.Kind, Text: lastActivityText(e), At: e.At})
	}
	writeJSON(w, http.StatusOK, d)
}

// worktreeGit lê o estado git do worktree, comparando o branch dele com o
// branch do worktree principal (o primeiro da lista).
func (s *server) worktreeGit(list []WorktreeView, v WorktreeView) *GitDetail {
	gi, ok := s.deps.Git.(gitinfo.WorktreeInspector)
	if !ok {
		return nil
	}
	if v.Prunable {
		return &GitDetail{Inspection: gitinfo.Inspection{Files: []gitinfo.FileChange{}, Commits: []gitinfo.Commit{}}, Error: "o diretório do worktree não existe mais"}
	}
	base := ""
	if !v.Main && !v.Detached && len(list) > 0 {
		base = list[0].Branch
	}
	in, err := gi.Inspect(v.Path, base, v.Branch, detailCommits)
	if err != nil {
		return &GitDetail{Inspection: in, Error: "não consegui ler o estado do git deste worktree"}
	}
	return &GitDetail{Inspection: in}
}
