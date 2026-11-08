package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type planResp struct {
	FiveHour *struct {
		UsedPct    float64    `json:"used_pct"`
		ResetsAt   *time.Time `json:"resets_at"`
		ReceivedAt time.Time  `json:"received_at"`
		Expired    bool       `json:"expired"`
	} `json:"five_hour"`
	SevenDay *struct {
		UsedPct float64 `json:"used_pct"`
		Expired bool    `json:"expired"`
	} `json:"seven_day"`
	History map[string][]struct {
		At      time.Time `json:"at"`
		UsedPct float64   `json:"used_pct"`
	} `json:"history"`
	WarnPct int `json:"warn_pct"`
}

func TestPlanLimitsPostAndGet(t *testing.T) {
	a := newTestAPI(t)
	ch, cancel := a.hub.Subscribe()
	defer cancel()
	reset := testNow.Add(2 * time.Hour).Unix()
	body := map[string]any{"session_id": "s1", "rate_limits": map[string]any{
		"five_hour": map[string]any{"used_percentage": 23.5, "resets_at": reset},
		"seven_day": map[string]any{"used_percentage": 41, "resets_at": testNow.Add(72 * time.Hour).Unix(), "spend_limit": map[string]any{"x": 1}},
		"extra":     map[string]any{"foo": "bar"},
	}}
	if rec := a.do(t, "POST", "/api/plan-limits", body, 0); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	select {
	case k := <-ch:
		if k != "plan" {
			t.Fatalf("hub = %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("sem publicação plan")
	}
	rec := a.do(t, "GET", "/api/plan-limits?hours=24", nil, 0)
	if rec.Code != 200 {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	p := decode[planResp](t, rec)
	if p.FiveHour == nil || p.FiveHour.UsedPct != 23.5 || p.FiveHour.Expired || p.FiveHour.ResetsAt.Unix() != reset {
		t.Fatalf("five_hour = %+v", p.FiveHour)
	}
	if p.SevenDay == nil || p.SevenDay.UsedPct != 41 {
		t.Fatalf("seven_day = %+v", p.SevenDay)
	}
	if len(p.History["five_hour"]) != 1 || len(p.History["seven_day"]) != 1 || p.WarnPct != 80 {
		t.Fatalf("history/warn = %+v", p)
	}
	// Mesma leitura de novo (sessão ociosa) ou menor: vigente igual, sem publicar.
	a.do(t, "POST", "/api/plan-limits", body, 0)
	body["rate_limits"].(map[string]any)["five_hour"] = map[string]any{"used_percentage": 10, "resets_at": reset}
	a.do(t, "POST", "/api/plan-limits", body, 0)
	select {
	case k := <-ch:
		t.Fatalf("publicou %q sem mudança da vigente", k)
	case <-time.After(100 * time.Millisecond):
	}
	// Valor maior muda a vigente: publica.
	body["rate_limits"].(map[string]any)["five_hour"] = map[string]any{"used_percentage": 30, "resets_at": reset}
	a.do(t, "POST", "/api/plan-limits", body, 0)
	select {
	case k := <-ch:
		if k != "plan" {
			t.Fatalf("hub = %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("sem publicação plan")
	}
}

func TestPlanLimitsGetEmptyAndExpired(t *testing.T) {
	a := newTestAPI(t)
	p := decode[planResp](t, a.do(t, "GET", "/api/plan-limits", nil, 0))
	if p.FiveHour != nil || p.SevenDay != nil || p.History["five_hour"] == nil || len(p.History["seven_day"]) != 0 {
		t.Fatalf("vazio = %+v", p)
	}
	raw := a.do(t, "GET", "/api/plan-limits", nil, 0).Body.String()
	if !strings.Contains(raw, `"five_hour":null`) || !strings.Contains(raw, `"five_hour":[]`) {
		t.Fatalf("json = %s", raw)
	}
	a.do(t, "POST", "/api/plan-limits", map[string]any{"rate_limits": map[string]any{
		"five_hour": map[string]any{"used_percentage": 95, "resets_at": testNow.Add(-time.Minute).Unix()}}}, 0)
	p = decode[planResp](t, a.do(t, "GET", "/api/plan-limits", nil, 0))
	if p.FiveHour == nil || !p.FiveHour.Expired {
		t.Fatalf("expirada = %+v", p.FiveHour)
	}
}

func TestPlanLimitsValidation(t *testing.T) {
	a := newTestAPI(t)
	post := func(rl map[string]any) {
		t.Helper()
		if rec := a.do(t, "POST", "/api/plan-limits", map[string]any{"session_id": "s", "rate_limits": rl}, 0); rec.Code != 204 {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body)
		}
	}
	post(map[string]any{"five_hour": map[string]any{"used_percentage": 101}, "seven_day": map[string]any{"used_percentage": -1}})
	post(map[string]any{"five_hour": map[string]any{"resets_at": 5}})
	post(nil)
	if rec := a.do(t, "POST", "/api/plan-limits", map[string]any{"session_id": "s"}, 0); rec.Code != 204 {
		t.Fatalf("sem rate_limits = %d", rec.Code)
	}
	p := decode[planResp](t, a.do(t, "GET", "/api/plan-limits", nil, 0))
	if p.FiveHour != nil || p.SevenDay != nil {
		t.Fatalf("nada deveria ser gravado: %+v %+v", p.FiveHour, p.SevenDay)
	}
	// Janela válida com resets_at inválido (fracionário/negativo): grava sem reinício.
	post(map[string]any{"five_hour": map[string]any{"used_percentage": 100, "resets_at": 1.5}, "seven_day": map[string]any{"used_percentage": 0, "resets_at": -3}})
	p = decode[planResp](t, a.do(t, "GET", "/api/plan-limits", nil, 0))
	if p.FiveHour == nil || p.FiveHour.UsedPct != 100 || p.FiveHour.ResetsAt != nil || p.SevenDay == nil || p.SevenDay.UsedPct != 0 {
		t.Fatalf("limites 0 e 100 = %+v %+v", p.FiveHour, p.SevenDay)
	}
	if rec := a.do(t, "GET", "/api/plan-limits?hours=0", nil, 0); rec.Code != 400 {
		t.Fatalf("hours=0 = %d", rec.Code)
	}
}
