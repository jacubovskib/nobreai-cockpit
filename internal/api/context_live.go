package api

import (
	"net/http"

	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func (s *server) contextLiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{id}/context", s.sessionContext)
	mux.HandleFunc("GET /api/context/live", s.contextLive)
}

// ctxOver é o contexto (% da janela) a partir do qual a sessão ganha destaque.
const ctxOver = 80

// sessionContext: GET /api/sessions/{id}/context — o medidor de contexto da sessão.
func (s *server) sessionContext(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.SessionContextAt(r.PathValue("id"), s.now())
	if err != nil {
		writeStoreError(w, err, "sessão não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// LiveContext é o resumo de uma sessão viva com captura completa.
type LiveContext struct {
	ID           string  `json:"id"`
	CardKey      string  `json:"card_key"`
	CardTitle    string  `json:"card_title"`
	Tokens       int64   `json:"tokens"`
	WindowTokens int64   `json:"window_tokens"`
	Pct          float64 `json:"pct"`
	PctOnly      bool    `json:"pct_only"`
	Over80       bool    `json:"over80"`
	Rewrite      bool    `json:"rewrite"`
	IdleOverTTL  bool    `json:"idle_over_ttl"`
}

// contextLive: GET /api/context/live — as sessões vivas com captura completa e o resumo do medidor.
func (s *server) contextLive(w http.ResponseWriter, r *http.Request) {
	live, err := s.st.LiveSessions()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	set, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	out := []LiveContext{}
	for _, x := range live {
		if !x.Live() || !x.FullCapture {
			continue
		}
		c, err := s.st.SessionContextWith(x.ID, s.now(), set)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		l := LiveContext{ID: x.ID, Tokens: c.Tokens, WindowTokens: c.WindowTokens, Pct: c.Pct, PctOnly: c.PctOnly,
			Over80: c.Pct > ctxOver, Rewrite: c.RecentRewrite != nil, IdleOverTTL: c.IdleOverTTL}
		if x.CardID != nil {
			if card, err := s.st.GetCardByID(*x.CardID); err == nil {
				l.CardKey, l.CardTitle = card.Key, card.Title
			} else if err != store.ErrNotFound {
				writeStoreError(w, err, "")
				return
			}
		}
		out = append(out, l)
	}
	writeJSON(w, http.StatusOK, out)
}
