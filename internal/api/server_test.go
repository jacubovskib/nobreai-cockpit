package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func TestRejectsForeignHost(t *testing.T) {
	a := newTestAPI(t)
	for _, host := range []string{"evil.example:7420", "127.0.0.1.evil.example:7420", "localhost", ""} {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || decode[map[string]string](t, rec)["error"] != "host não permitido" {
			t.Fatalf("host %q = %d %s", host, rec.Code, rec.Body)
		}
	}
	for _, host := range []string{"127.0.0.1:7420", "localhost:7420"} {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("host %q = %d %s", host, rec.Code, rec.Body)
		}
	}

	// num servidor real, a porta do Host precisa ser a porta em que ele escuta
	srv := httptest.NewServer(a.h)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	req.Host = "127.0.0.1:1"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("porta errada = %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("porta certa = %d", resp.StatusCode)
	}
}

func TestRejectsNonJSONPost(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	body := fmt.Sprintf(`{"title":"x","project_id":%d}`, p.ID)
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		req := httptest.NewRequest("POST", "/api/cards", strings.NewReader(body))
		req.Host = "127.0.0.1:7420"
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType || decode[map[string]string](t, rec)["error"] != "use Content-Type: application/json" {
			t.Fatalf("content-type %q = %d %s", ct, rec.Code, rec.Body)
		}
	}
	if cards, _ := a.st.ListCards(store.CardFilter{}); len(cards) != 0 {
		t.Fatalf("card criado apesar do 415: %+v", cards)
	}
	req := httptest.NewRequest("POST", "/api/cards", strings.NewReader(body))
	req.Host = "127.0.0.1:7420"
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("json com charset = %d %s", rec.Code, rec.Body)
	}
	// POST sem corpo não exige Content-Type
	req = httptest.NewRequest("POST", "/api/prompts/retomar-card/render", nil)
	req.Host = "127.0.0.1:7420"
	rec = httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST sem corpo = %d %s", rec.Code, rec.Body)
	}
}
