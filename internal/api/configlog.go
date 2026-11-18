package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
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

// logConfig grava uma linha no registro e avisa o stream. O registro é só
// histórico: falhar aqui não desfaz a mudança já aplicada.
func (s *server) logConfig(kind string, data any) {
	if err := s.st.AddConfigLog(kind, data, s.now()); err != nil {
		log.Printf("config_log: gravar %s: %v", kind, err)
		return
	}
	s.publish("config_log")
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
// as vivas sem projeto cujo dono, pela regra do hook, é um membro da família
// (um projeto de outra família aninhado na pasta fica com as dele).
func (s *server) applyProjectCapture(projectID int64, on bool) (int, error) {
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
	// Só conta quem muda de fato: com captura ligada ao desligar, desligada ao ligar.
	was := map[string]bool{}
	for _, x := range live {
		if x.Live() && x.ProjectID != nil && inFam[*x.ProjectID] && x.FullCapture != on {
			was[x.ID] = true
		}
	}
	ids, err := s.st.SetProjectSessionsCapture(projectID, on)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if was[id] {
			n++
		}
	}
	if !on {
		return n, nil
	}
	wr, _ := s.deps.Git.(gitinfo.WorktreeReader)
	for _, x := range live {
		if !x.Live() || x.ProjectID != nil {
			continue
		}
		owner, err := s.sessionOwner(wr, x)
		if err != nil {
			return n, err
		}
		if owner == nil || !inFam[owner.ID] {
			continue
		}
		if err := s.st.AdoptSession(x.ID, owner.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// sessionOwner acha o dono da sessão pela mesma regra do hook
// (store.ProjectForLocation): o projeto mais fundo de qualquer família, com a
// tradução do worktree ligado para o checkout principal.
func (s *server) sessionOwner(wr gitinfo.WorktreeReader, x domain.Session) (*domain.Project, error) {
	l := store.SessionLocation{Cwd: x.Cwd, Worktree: x.Worktree, WorktreeLinked: x.WorktreeLinked}
	if wr != nil && x.WorktreeLinked && x.Worktree != "" {
		l.Main = wr.Locate(x.Worktree).Main
	}
	return s.st.ProjectForLocation(x.Cwd, l)
}
