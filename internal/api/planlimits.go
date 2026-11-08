package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func (s *server) planLimitsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/plan-limits", s.postPlanLimits)
	mux.HandleFunc("GET /api/plan-limits", s.getPlanLimits)
}

// planWindowIn é uma janela como o Claude Code a escreve no JSON da status
// line; chaves desconhecidas (ex.: spend_limit) são ignoradas.
type planWindowIn struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *float64 `json:"resets_at"`
}

// valid devolve a janela do domínio, ou nil quando used_percentage falta ou
// está fora de 0–100. resets_at que não seja inteiro positivo é ignorado.
func (w *planWindowIn) valid() *domain.PlanWindow {
	if w == nil || w.UsedPercentage == nil {
		return nil
	}
	u := *w.UsedPercentage
	if math.IsNaN(u) || u < 0 || u > 100 {
		return nil
	}
	out := &domain.PlanWindow{UsedPct: u}
	if r := w.ResetsAt; r != nil && *r > 0 && *r == math.Trunc(*r) && *r < 1e12 {
		t := time.Unix(int64(*r), 0).UTC()
		out.ResetsAt = &t
	}
	return out
}

func (s *server) postPlanLimits(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SessionID  string `json:"session_id"`
		RateLimits *struct {
			FiveHour *planWindowIn `json:"five_hour"`
			SevenDay *planWindowIn `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.RateLimits != nil {
		rd := domain.PlanReading{SessionID: in.SessionID, At: s.now(),
			FiveHour: in.RateLimits.FiveHour.valid(), SevenDay: in.RateLimits.SevenDay.valid()}
		if rd.FiveHour != nil || rd.SevenDay != nil {
			changed, err := s.st.RecordPlanLimits(rd)
			if err != nil {
				writeStoreError(w, err, "")
				return
			}
			if changed {
				s.publish("plan")
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// latestPlan devolve as últimas leituras com Expired calculado pelo relógio do servidor.
func (s *server) latestPlan() (map[string]domain.PlanWindowState, error) {
	m, err := s.st.LatestPlanLimits()
	if err != nil {
		return nil, err
	}
	now := s.now()
	for k, v := range m {
		v.Expired = v.ResetsAt != nil && !v.ResetsAt.After(now)
		m[k] = v
	}
	return m, nil
}

func (s *server) getPlanLimits(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 24*30 {
			writeError(w, http.StatusBadRequest, "hours inválido (1 a 720)")
			return
		}
		hours = n
	}
	latest, err := s.latestPlan()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	set, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	since := s.now().Add(-time.Duration(hours) * time.Hour)
	hist := map[string][]domain.PlanPoint{}
	out := map[string]any{"history": hist, "warn_pct": set.PlanWarnPct}
	for _, name := range []string{domain.PlanFiveHour, domain.PlanSevenDay} {
		if st, ok := latest[name]; ok {
			out[name] = st
		} else {
			out[name] = nil
		}
		if hist[name], err = s.st.PlanLimitsHistory(name, since); err != nil {
			writeStoreError(w, err, "")
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

var weekdaysPT = [...]string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

// planAlerts avisa quando uma janela passou do percentual de aviso e ainda não reiniciou.
func (s *server) planAlerts(now time.Time) ([]Alert, error) {
	latest, err := s.latestPlan()
	if err != nil {
		return nil, err
	}
	set, err := s.st.GetSettings()
	if err != nil {
		return nil, err
	}
	var out []Alert
	warn := float64(set.PlanWarnPct)
	add := func(name, label string, weekday bool) {
		if w, ok := latest[name]; ok && !w.Expired && w.UsedPct >= warn {
			out = append(out, Alert{Kind: "plan_limit", Agent: "claude",
				Text: fmt.Sprintf("%s do plano em %s%%%s.", label, pctText(w.UsedPct), resetText(w.ResetsAt, weekday))})
		}
	}
	add(domain.PlanFiveHour, "Sessão de 5 h", false)
	add(domain.PlanSevenDay, "Limite semanal", true)
	return out, nil
}

// pctText arredonda para inteiro.
func pctText(v float64) string { return strconv.Itoa(int(math.Round(v))) }

// resetText: " (reinicia às HH:MM)" ou, com weekday, " (reinicia <dia> às HH:MM)",
// no fuso local; vazio sem resets_at.
func resetText(t *time.Time, weekday bool) string {
	if t == nil {
		return ""
	}
	l := t.Local()
	s := "às " + l.Format("15:04")
	if weekday {
		s = weekdaysPT[l.Weekday()] + " " + s
	}
	return " (reinicia " + s + ")"
}
