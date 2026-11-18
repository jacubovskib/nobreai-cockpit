package api

import (
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resources"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// worktreeWindow é até quando uma sessão encerrada aparece no seu worktree.
const worktreeWindow = 24 * time.Hour

const msgNoWorktreeReader = "leitura de worktrees indisponível neste daemon"

// ProjectWorktrees são os worktrees git de um projeto com os agentes e cards
// de cada um. Error explica por que a lista não pôde ser lida (diretório fora
// de um repositório, por exemplo). Orphans são as sessões não encerradas cujo
// worktree não existe mais na lista do git.
type ProjectWorktrees struct {
	Project   domain.Project    `json:"project"`
	Error     string            `json:"error"`
	Worktrees []WorktreeView    `json:"worktrees"`
	Orphans   []WorktreeSession `json:"orphans"`
}

// WorktreeView é um worktree com as sessões que rodam (ou rodaram nas
// últimas 24 h) nele, os cards abertos do seu branch e o que consome da
// máquina (nil sem Deps.Resources).
type WorktreeView struct {
	gitinfo.Worktree
	Sessions  []WorktreeSession  `json:"sessions"`
	Cards     []CardRef          `json:"cards"`
	Resources *WorktreeResources `json:"resources"`
}

// ResourceReader é o que a API precisa do medidor de recursos.
type ResourceReader interface {
	// Snapshot devolve os processos da última amostra; ready falso enquanto
	// a CPU ainda não pôde ser medida.
	Snapshot() ([]resources.Proc, bool)
	// Disk devolve a medida de espaço de path (pedindo uma nova se preciso).
	Disk(path string, exclude []string) resources.DiskInfo
}

// WorktreeResources é o consumo dos processos cujo cwd está no worktree (ou
// descendentes deles) e o espaço em disco. CPU em % de um núcleo; CPUCores
// para converter em % da máquina. Disk é nil em worktree sem diretório.
type WorktreeResources struct {
	Ready bool `json:"ready"`
	resources.Usage
	CPUCores int                 `json:"cpu_cores"`
	Disk     *resources.DiskInfo `json:"disk"`
}

// WorktreeSession é uma sessão como aparece na lista de worktrees. Live segue
// domain.Session.Live; LastActivity só é preenchido para sessões vivas.
type WorktreeSession struct {
	domain.Session
	Live         bool     `json:"live"`
	LastActivity string   `json:"last_activity"`
	Card         *CardRef `json:"card"`
}

// CardRef é o mínimo de um card para mostrar e linkar.
type CardRef struct {
	Key    string        `json:"key"`
	Title  string        `json:"title"`
	Column domain.Column `json:"column"`
}

func cardRef(c domain.Card) *CardRef { return &CardRef{Key: c.Key, Title: c.Title, Column: c.Column} }

func (s *server) worktreeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/worktrees", s.listWorktrees)
	mux.HandleFunc("GET /api/worktrees/detail", s.worktreeDetail)
	mux.HandleFunc("DELETE /api/worktrees", s.removeWorktree)
}

func (s *server) listWorktrees(w http.ResponseWriter, r *http.Request) {
	projects, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	out := make([]ProjectWorktrees, 0, len(projects))
	for _, p := range projects {
		// App de monorepo: o repositório aparece uma vez, no item do pai.
		repo, err := s.st.RepoProject(p)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if repo.ID != p.ID {
			continue
		}
		pw, err := s.projectWorktrees(p)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		out = append(out, pw)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) projectWorktrees(p domain.Project) (ProjectWorktrees, error) {
	pw := ProjectWorktrees{Project: p, Worktrees: []WorktreeView{}, Orphans: []WorktreeSession{}}
	if p.Path == "" {
		return pw, nil // grupo: não tem repositório; os filhos têm o seu item
	}
	wr, ok := s.deps.Git.(gitinfo.WorktreeReader)
	if !ok {
		pw.Error = msgNoWorktreeReader
		return pw, nil
	}
	list, err := wr.Worktrees(p.Path)
	if err != nil {
		pw.Error = "não consegui listar os worktrees de " + p.Path + ": não é um repositório git?"
		return pw, nil
	}
	// Num monorepo os cards e as sessões dos apps entram no worktree do pai.
	family, err := s.st.FamilyIDs(p.ID)
	if err != nil {
		return pw, err
	}
	cards, err := s.st.ListCards(store.CardFilter{ProjectIDs: family})
	if err != nil {
		return pw, err
	}
	byID := make(map[int64]domain.Card, len(cards))
	for _, c := range cards {
		byID[c.ID] = c
	}
	sessions, err := s.st.ProjectsSessionsSince(family, s.now().Add(-worktreeWindow))
	if err != nil {
		return pw, err
	}

	for _, wt := range list {
		v := WorktreeView{Worktree: wt, Sessions: []WorktreeSession{}, Cards: []CardRef{}}
		if wt.Branch != "" {
			for _, c := range cards {
				if c.Branch == wt.Branch && c.Column != domain.Concluido {
					v.Cards = append(v.Cards, *cardRef(c))
				}
			}
		}
		pw.Worktrees = append(pw.Worktrees, v)
	}
	for _, x := range sessions {
		ws := WorktreeSession{Session: x, Live: x.Live()}
		if x.CardID != nil {
			if c, ok := byID[*x.CardID]; ok {
				ws.Card = cardRef(c)
			}
		}
		if ws.Live {
			ev, err := s.st.LastSessionEvent(x.ID)
			if err != nil {
				return pw, err
			}
			if ev != nil {
				ws.LastActivity = lastActivityText(*ev)
			}
		}
		i := worktreeOf(pw.Worktrees, x)
		switch {
		case i >= 0:
			pw.Worktrees[i].Sessions = append(pw.Worktrees[i].Sessions, ws)
		case x.EndedAt == nil && x.Worktree != "":
			pw.Orphans = append(pw.Orphans, ws)
		}
	}
	s.attachResources(pw.Worktrees)
	for i := range pw.Worktrees {
		ss := pw.Worktrees[i].Sessions
		// Vivas primeiro; dentro de cada grupo, a ordem do store (mais recente antes).
		sort.SliceStable(ss, func(a, b int) bool { return ss[a].Live && !ss[b].Live })
	}
	return pw, nil
}

// worktreeOf acha o worktree da sessão: pelo worktree gravado ou, nas
// sessões anteriores a ele, pelo cwd dentro do worktree mais profundo.
func worktreeOf(list []WorktreeView, x domain.Session) int {
	if x.Worktree != "" {
		for i, v := range list {
			if v.Path == x.Worktree {
				return i
			}
		}
		return -1
	}
	best := -1
	for i, v := range list {
		if x.Cwd == v.Path || strings.HasPrefix(x.Cwd, strings.TrimRight(v.Path, "/")+"/") {
			if best < 0 || len(v.Path) > len(list[best].Path) {
				best = i
			}
		}
	}
	return best
}

// attachResources preenche o consumo de cada worktree do projeto.
func (s *server) attachResources(list []WorktreeView) {
	if s.deps.Resources == nil {
		return
	}
	procs, ready := s.deps.Resources.Snapshot()
	paths := make([]string, len(list))
	for i, v := range list {
		paths[i] = v.Path
	}
	usage := resources.Attribute(procs, paths)
	cores := runtime.NumCPU()
	for i := range list {
		v := &list[i]
		u := usage[strings.TrimRight(v.Path, "/")]
		if u.Top == nil {
			u.Top = []resources.Proc{}
		}
		r := &WorktreeResources{Ready: ready, Usage: u, CPUCores: cores}
		if !v.Prunable && !v.Bare {
			d := s.deps.Resources.Disk(v.Path, nestedPaths(v.Path, paths))
			r.Disk = &d
		}
		v.Resources = r
	}
}

// nestedPaths são os caminhos de paths dentro de root (worktrees aninhados).
func nestedPaths(root string, paths []string) []string {
	var out []string
	prefix := strings.TrimRight(root, "/") + "/"
	for _, p := range paths {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out
}
