package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/version"
)

func TestRejectsForeignHost(t *testing.T) {
	a := newTestAPI(t)
	for _, host := range []string{"evil.example:7420", "127.0.0.1.evil.example:7420", "localhost", ""} {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = host
		req.RemoteAddr = "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || decode[map[string]string](t, rec)["error"] != "host não permitido" {
			t.Fatalf("host %q = %d %s", host, rec.Code, rec.Body)
		}
	}
	for _, host := range []string{"127.0.0.1:7420", "localhost:7420", "127.0.0.2:7420"} {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = host
		req.RemoteAddr = "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("host %q = %d %s", host, rec.Code, rec.Body)
		}
		if v := decode[map[string]any](t, rec)["version"]; v != version.Version {
			t.Fatalf("version = %v, quer %s", v, version.Version)
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
		req.RemoteAddr = "127.0.0.1:50000"
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
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("json com charset = %d %s", rec.Code, rec.Body)
	}
	// POST sem corpo também exige Content-Type
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		req = httptest.NewRequest(m, "/api/prompts/retomar-card/render", nil)
		req.Host = "127.0.0.1:7420"
		req.RemoteAddr = "127.0.0.1:50000"
		rec = httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("%s sem corpo e sem Content-Type = %d %s", m, rec.Code, rec.Body)
		}
	}
	req = httptest.NewRequest("POST", "/api/prompts/retomar-card/render", nil)
	req.Host = "127.0.0.1:7420"
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST sem corpo com Content-Type = %d %s", rec.Code, rec.Body)
	}
}

// Só aceita conexões vindas da própria máquina (loopback).
func TestRejectsNonLoopbackRemote(t *testing.T) {
	a := newTestAPI(t)
	for remote, want := range map[string]int{
		"192.168.0.7:41000": http.StatusForbidden,
		"10.0.0.1:41000":    http.StatusForbidden,
		"127.0.0.1:41000":   http.StatusOK,
		"127.0.0.9:41000":   http.StatusOK,
	} {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.Host = "127.0.0.1:7420"
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("remote %s = %d %s, want %d", remote, rec.Code, rec.Body, want)
		}
		if want == http.StatusForbidden && decode[map[string]string](t, rec)["error"] != msgRemoteForbidden {
			t.Fatalf("remote %s: %s", remote, rec.Body)
		}
	}
}
