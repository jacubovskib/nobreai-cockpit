package api

import (
	"net/http"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/rtk"
)

const (
	msgNoRTK       = "RTK indisponível"
	rtkMinEach     = 5
	rtkCompareRows = 15
)

func (s *server) rtkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rtk", s.rtkStatus)
	mux.HandleFunc("GET /api/rtk/preview", s.rtkPreview)
	mux.HandleFunc("POST /api/rtk", s.rtkApply)
	mux.HandleFunc("GET /api/rtk/savings", s.rtkSavings)
}

// rtkTool devolve o RTK ou responde 503 quando o daemon não o tem.
func (s *server) rtkTool(w http.ResponseWriter) (*rtk.Tool, bool) {
	if s.deps.RTK == nil {
		writeError(w, http.StatusServiceUnavailable, msgNoRTK)
		return nil, false
	}
	return s.deps.RTK, true
}

// rtkStatus: GET /api/rtk — instalado, versão, hook ativo e intervalos.
func (s *server) rtkStatus(w http.ResponseWriter, r *http.Request) {
	t, ok := s.rtkTool(w)
	if !ok {
		return
	}
	st := t.Status(true)
	if st.Activations == nil {
		st.Activations = []rtk.Interval{}
	}
	writeJSON(w, http.StatusOK, st)
}

// rtkPreview: GET /api/rtk/preview?action=activate|deactivate — o que o init
// faria (--dry-run), sem mudar nada.
func (s *server) rtkPreview(w http.ResponseWriter, r *http.Request) {
	t, ok := s.rtkTool(w)
	if !ok {
		return
	}
	action := r.URL.Query().Get("action")
	if action != "activate" && action != "deactivate" {
		writeError(w, http.StatusBadRequest, `action deve ser "activate" ou "deactivate"`)
		return
	}
	out, err := t.Preview(action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

// rtkApply: POST /api/rtk {"action"} — ativa ou desativa o hook, sempre por
// ação explícita do usuário.
func (s *server) rtkApply(w http.ResponseWriter, r *http.Request) {
	t, ok := s.rtkTool(w)
	if !ok {
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Action != "activate" && in.Action != "deactivate" {
		writeError(w, http.StatusBadRequest, `action deve ser "activate" ou "deactivate"`)
		return
	}
	if in.Action == "deactivate" && s.deps.Integration != nil {
		// A marca de remoção deliberada vem antes de o arquivo mudar, para o
		// monitor não alertar a desativação que o usuário acabou de pedir.
		s.deps.Integration.Removed([]string{"rtk"}, "config")
	}
	out, backup, err := t.Apply(in.Action)
	if err != nil {
		// Falhou: o hook pode não ter mudado, então o monitor confere de novo.
		if s.deps.Integration != nil {
			s.deps.Integration.CheckNow()
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	st := t.Status(true)
	kind := domain.LogRTKOn
	if in.Action == "deactivate" {
		kind = domain.LogRTKOff
	} else if s.deps.Integration != nil {
		s.deps.Integration.Fixed([]string{"rtk"}, []string{t.SettingsPath})
	}
	s.logConfig(kind, map[string]string{"version": st.Version, "backup": backup})
	s.publish("config", "integration")
	if st.Activations == nil {
		st.Activations = []rtk.Interval{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": out, "backup": backup, "status": st})
}

type rtkGainTotals struct {
	Available   bool    `json:"available"`
	Commands    int64   `json:"commands"`
	InputTokens int64   `json:"input_tokens"`
	SavedTokens int64   `json:"saved_tokens"`
	SavingsPct  float64 `json:"savings_pct"`
	OldestDay   string  `json:"oldest_day"`
}

// gainTotals soma os dias do gain com data em [from, to) (datas locais;
// zero = sem limite). OldestDay é o dia mais antigo de toda a saída.
func gainTotals(days []rtk.GainDay, from, to time.Time) rtkGainTotals {
	g := rtkGainTotals{Available: true}
	var lo, hi string
	if !from.IsZero() {
		lo = from.In(time.Local).Format("2006-01-02")
	}
	if !to.IsZero() {
		hi = to.In(time.Local).Format("2006-01-02")
	}
	for _, d := range days {
		if g.OldestDay == "" || d.Date < g.OldestDay {
			g.OldestDay = d.Date
		}
		if (lo != "" && d.Date < lo) || (hi != "" && d.Date >= hi) {
			continue
		}
		g.Commands += d.Commands
		g.InputTokens += d.InputTokens
		g.SavedTokens += d.SavedTokens
	}
	if g.InputTokens > 0 {
		g.SavingsPct = float64(g.SavedTokens) / float64(g.InputTokens) * 100
	}
	return g
}

// rtkSavings: GET /api/rtk/savings?from=&to=&project=&exact=1 — a economia
// medida pelo RTK no período e o antes/depois da ativação (todo o histórico,
// só o projeto filtra).
func (s *server) rtkSavings(w http.ResponseWriter, r *http.Request) {
	t, ok := s.rtkTool(w)
	if !ok {
		return
	}
	from, to, ids, ok := s.parseScope(w, r)
	if !ok {
		return
	}
	gain := rtkGainTotals{}
	if days, err := t.Gain(); err == nil {
		gain = gainTotals(days, from, to)
	}
	acts, err := t.Activations()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	cmp, err := s.st.RTKBeforeAfter(ids, acts, rtkMinEach, rtkCompareRows)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if acts == nil {
		acts = []rtk.Interval{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"gain": gain, "compare": cmp, "activations": acts})
}
