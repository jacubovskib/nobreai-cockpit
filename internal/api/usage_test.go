package api

import "testing"

func TestUsageTotalsProject(t *testing.T) {
	a := newTestAPI(t)
	a.card(t)
	if rec := a.do(t, "GET", "/api/usage/totals?group=project", nil, 0); rec.Code != 200 {
		t.Fatalf("group=project = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "GET", "/api/usage/totals?group=agent&project=1", nil, 0); rec.Code != 200 {
		t.Fatalf("project=1 = %d %s", rec.Code, rec.Body)
	}
	rec := a.do(t, "GET", "/api/usage/totals?project=abc", nil, 0)
	if rec.Code != 400 || decode[map[string]string](t, rec)["error"] != "projeto inválido" {
		t.Fatalf("project=abc = %d %s", rec.Code, rec.Body)
	}
}
