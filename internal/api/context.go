package api

import (
	"net/http"

	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const topKeys = 30

func (s *server) contextRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/context/top", s.contextTop)
	mux.HandleFunc("GET /api/context/sessions/{id}", s.contextSession)
}

// contextTop: GET /api/context/top?from=&to=&project=&exact=1 — o que mais
// enche o contexto: chaves (até 30, mais as de hook), sessões (até 10), projetos e totais.
func (s *server) contextTop(w http.ResponseWriter, r *http.Request) {
	var f store.ContextFilter
	var ok bool
	if f.From, f.To, f.ProjectIDs, ok = s.parseScope(w, r); !ok {
		return
	}
	all, err := s.st.ContextTopKeys(f, 0)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	// As 30 de maior peso, mais toda chave de hook (a injeção do próprio
	// Cockpit) que ficou abaixo do corte, em ordem de peso.
	keys := all
	if len(all) > topKeys {
		keys = append([]store.ContextKey{}, all[:topKeys]...)
		for _, k := range all[topKeys:] {
			if k.Tool == "hook" {
				keys = append(keys, k)
			}
		}
	}
	sessions, err := s.st.ContextTopSessions(f, 10)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	projects, err := s.st.ContextByProject(f)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	tot, err := s.st.ContextTotals(f)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"keys": keys, "sessions": sessions, "projects": projects,
		"context_total": tot.ContextTotal, "weight_total": tot.WeightTotal,
	})
}

// contextSession: GET /api/context/sessions/{id} — contexto por turno e as
// 10 saídas de maior peso da sessão.
func (s *server) contextSession(w http.ResponseWriter, r *http.Request) {
	turns, top, err := s.st.ContextSessionDetail(r.PathValue("id"), 10)
	if err != nil {
		writeStoreError(w, err, "sessão não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": turns, "top": top})
}
