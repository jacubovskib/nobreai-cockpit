package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// SessionWithUsage é a sessão com seus tokens e custo. Unpriced repete
// Usage.Unpriced: há uso de modelo sem preço, então o custo é "sem preço".
type SessionWithUsage struct {
	domain.Session
	Usage    store.Total `json:"usage"`
	Unpriced bool        `json:"unpriced"`
}

func (s *server) usageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/usage/totals", s.usageTotals)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("GET /api/events", s.listEvents)
}

// totalsBy agrega todo o uso pelo grupo e indexa por Key.
func (s *server) totalsBy(group string) (map[string]store.Total, error) {
	ts, err := s.st.Totals(store.TotalsFilter{GroupBy: group})
	if err != nil {
		return nil, err
	}
	m := make(map[string]store.Total, len(ts))
	for _, t := range ts {
		m[t.Key] = t
	}
	return m, nil
}

func withUsage(sessions []domain.Session, bySession map[string]store.Total) []SessionWithUsage {
	out := make([]SessionWithUsage, 0, len(sessions))
	for _, x := range sessions {
		u := bySession[x.ID]
		out = append(out, SessionWithUsage{Session: x, Usage: u, Unpriced: u.Unpriced})
	}
	return out
}

// parseDay aceita RFC3339 ou AAAA-MM-DD (meia-noite local).
func parseDay(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", v, time.Local)
}

// usageTotals: GET /api/usage/totals?from=&to=&group= (padrão group=agent;
// intervalo [from, to)).
func (s *server) usageTotals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TotalsFilter{GroupBy: q.Get("group")}
	if f.GroupBy == "" {
		f.GroupBy = "agent"
	}
	switch f.GroupBy {
	case "agent", "card", "session", "day_agent", "model":
	default:
		writeError(w, http.StatusBadRequest, "agrupamento inválido (use agent, card, session, day_agent ou model)")
		return
	}
	var err error
	if f.From, err = parseDay(q.Get("from")); err != nil {
		writeError(w, http.StatusBadRequest, "data inicial inválida")
		return
	}
	if f.To, err = parseDay(q.Get("to")); err != nil {
		writeError(w, http.StatusBadRequest, "data final inválida")
		return
	}
	ts, err := s.st.Totals(f)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ts))
}

// listSessions: GET /api/sessions?limit= (padrão 50, máximo 500), mais
// recentes primeiro, com uso.
func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "limit inválido")
			return
		}
		limit = min(n, 500)
	}
	sessions, err := s.st.RecentSessions(limit)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	bySession, err := s.totalsBy("session")
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, withUsage(sessions, bySession))
}

// listEvents: GET /api/events?kind=&limit= (padrão limit 50, máximo 500),
// mais recentes primeiro, com agente da sessão e chave do card. kind é
// obrigatório (evita despejar heartbeats).
func (s *server) listEvents(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		writeError(w, http.StatusBadRequest, "kind obrigatório")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "limit inválido")
			return
		}
		limit = min(n, 500)
	}
	evs, err := s.st.EventsByKind(kind, limit)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(evs))
}
