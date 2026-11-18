package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

const (
	configLogDefault = 50
	configLogMax     = 500
)

func (s *server) configLogRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config-log", s.getConfigLog)
}

// getConfigLog devolve as últimas mudanças de configuração, a mais recente primeiro.
func (s *server) getConfigLog(w http.ResponseWriter, r *http.Request) {
	limit := configLogDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit inválido")
			return
		}
		limit = min(n, configLogMax)
	}
	es, err := s.st.RecentConfigLog(limit)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(es))
}

// logConfig grava uma linha no registro e avisa o stream.
func (s *server) logConfig(kind string, data any) error {
	if err := s.st.AddConfigLog(kind, data, s.now()); err != nil {
		return err
	}
	s.publish("config_log")
	return nil
}

// changedSettings lista, por chave do JSON, o que mudou de old para next.
func changedSettings(old, next domain.Settings) ([]map[string]json.RawMessage, error) {
	toMap := func(v domain.Settings) (map[string]json.RawMessage, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m := map[string]json.RawMessage{}
		return m, json.Unmarshal(b, &m)
	}
	om, err := toMap(old)
	if err != nil {
		return nil, err
	}
	nm, err := toMap(next)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(nm))
	for k := range nm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []map[string]json.RawMessage
	for _, k := range keys {
		if string(om[k]) == string(nm[k]) {
			continue
		}
		key, _ := json.Marshal(k)
		out = append(out, map[string]json.RawMessage{"key": key, "from": om[k], "to": nm[k]})
	}
	return out, nil
}

// countStaleCardsRule conta as sessões vivas com captura que já viram a
// configuração e ainda têm o valor antigo de agent_creates_cards.
func (s *server) countStaleCardsRule(now bool) (int, error) {
	live, err := s.st.LiveSessions()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, x := range live {
		if x.Live() && x.FullCapture && x.SeenConfig != nil && x.SeenConfig.AgentCreatesCards != now {
			n++
		}
	}
	return n, nil
}

// applyProjectCapture leva a mudança de captura de um projeto às sessões vivas
// da família e devolve quantas foram avisadas. Desligar para as sessões vivas
// da família; ligar religa as vivas da família que estavam sem captura e adota
// as vivas sem projeto que rodam na pasta de algum membro (ou num worktree
// ligado do repositório dele), no membro mais específico.
func (s *server) applyProjectCapture(projectID int64, on bool) (int, error) {
	if !on {
		ids, err := s.st.SetProjectSessionsCapture(projectID, false)
		return len(ids), err
	}
	fam, err := s.st.FamilyIDs(projectID)
	if err != nil {
		return 0, err
	}
	inFam := map[int64]bool{}
	for _, id := range fam {
		inFam[id] = true
	}
	live, err := s.st.LiveSessions()
	if err != nil {
		return 0, err
	}
	off := map[string]bool{}
	for _, x := range live {
		if x.Live() && x.ProjectID != nil && inFam[*x.ProjectID] && !x.FullCapture {
			off[x.ID] = true
		}
	}
	ids, err := s.st.SetProjectSessionsCapture(projectID, true)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if off[id] {
			n++
		}
	}
	all, err := s.st.ListProjects()
	if err != nil {
		return 0, err
	}
	var members []domain.Project
	for _, p := range all {
		if inFam[p.ID] && p.Path != "" {
			members = append(members, p)
		}
	}
	wr, _ := s.deps.Git.(gitinfo.WorktreeReader)
	for _, x := range live {
		if !x.Live() || x.ProjectID != nil {
			continue
		}
		pid, ok := familyOwner(members, wr, x)
		if !ok {
			continue
		}
		if err := s.st.AdoptSession(x.ID, pid); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// familyOwner escolhe o membro mais específico da família dono da sessão:
// pela pasta (cwd) ou, num worktree ligado, pelo caminho relativo dentro dele.
func familyOwner(members []domain.Project, wr gitinfo.WorktreeReader, x domain.Session) (int64, bool) {
	deepest := func(rel string, within func(p domain.Project) (string, bool)) (int64, bool) {
		var best int64
		bestLen, found := -1, false
		for _, m := range members {
			r, ok := within(m)
			if ok && (rel == r || strings.HasPrefix(rel, r+"/")) && len(r) > bestLen {
				best, bestLen, found = m.ID, len(r), true
			}
		}
		return best, found
	}
	cwd := strings.TrimRight(x.Cwd, "/")
	var best int64
	bestLen := -1
	for _, m := range members {
		root := strings.TrimRight(m.Path, "/")
		if (cwd == root || strings.HasPrefix(cwd, root+"/")) && len(root) > bestLen {
			best, bestLen = m.ID, len(root)
		}
	}
	if bestLen >= 0 {
		return best, true
	}
	if wr == nil || !x.WorktreeLinked || x.Worktree == "" {
		return 0, false
	}
	main := filepath.Clean(wr.Locate(x.Worktree).Main)
	var repo *domain.Project
	for i := range members {
		if members[i].Path == main {
			repo = &members[i]
		}
	}
	if repo == nil {
		return 0, false
	}
	wt := strings.TrimRight(x.Worktree, "/")
	rel := ""
	if r, ok := strings.CutPrefix(cwd, wt+"/"); ok {
		rel = r
	}
	if id, ok := deepest(rel, func(p domain.Project) (string, bool) {
		if p.ID == repo.ID {
			return "", false
		}
		r := strings.TrimPrefix(strings.TrimRight(p.Path, "/"), main+"/")
		return r, strings.HasPrefix(p.Path, main+"/")
	}); ok && rel != "" {
		return id, true
	}
	return repo.ID, true
}
