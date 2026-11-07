package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestServesSPAFallback(t *testing.T) {
	h := spaHandler(fstest.MapFS{
		"index.html":    {Data: []byte(`<html><body><div id="root"></div></body></html>`)},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	for _, p := range []string{"/", "/board", "/cards/NAC-1"} {
		rec := get(h, p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<div id="root">`) {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := get(h, "/assets/app.js"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("asset: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "/api/nada"); rec.Code != 404 || strings.Contains(rec.Body.String(), "root") {
		t.Errorf("api desconhecida: %d %q", rec.Code, rec.Body.String())
	}
}

func TestServesBuildHintWithoutIndex(t *testing.T) {
	rec := get(spaHandler(fstest.MapFS{".gitkeep": {}}), "/board")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "make web") {
		t.Errorf("%d %q", rec.Code, rec.Body.String())
	}
}

func TestRouterServesSPAAndKeepsAPI(t *testing.T) {
	a := newTestAPI(t)
	rec := a.do(t, "GET", "/board", nil, 0)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("/board: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = a.do(t, "GET", "/api/health", nil, 0)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Errorf("health: %d %s", rec.Code, rec.Body.String())
	}
}
