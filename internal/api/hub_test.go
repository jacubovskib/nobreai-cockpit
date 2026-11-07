package api

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHubPublishesOnWrite(t *testing.T) {
	a := newTestAPI(t)
	p := a.project(t)
	srv := httptest.NewServer(a.h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func() string {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream fechou")
			}
			return l
		case <-time.After(2 * time.Second):
			t.Fatal("timeout esperando o stream")
		}
		return ""
	}
	if l := next(); !strings.HasPrefix(l, ":") {
		t.Fatalf("primeira linha = %q", l)
	}

	body := strings.NewReader(fmt.Sprintf(`{"title":"x","project_id":%d}`, p.ID))
	if r, err := http.Post(srv.URL+"/api/cards", "application/json", body); err != nil || r.StatusCode != http.StatusCreated {
		t.Fatalf("POST: %v %v", err, r)
	}
	for {
		l := next()
		if l == "event: changed" {
			if d := next(); d != `data: {"kind":"cards"}` {
				t.Fatalf("data = %q", d)
			}
			return
		}
	}
}

func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub()
	defer h.Close()
	ch, cancel := h.Subscribe()
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish("cards")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish bloqueou com assinante lento")
	}
	n := 0
	for range ch {
		n++
	}
	if n == 0 || n >= 1000 {
		t.Fatalf("assinante lento recebeu %d mensagens; esperava ser descartado após encher o buffer", n)
	}
}
