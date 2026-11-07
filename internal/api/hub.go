package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// subscriberBuffer é quantas mensagens um assinante pode acumular antes de
// ser descartado.
const subscriberBuffer = 32

// Hub distribui avisos de mudança ("cards", "sessions", "usage", "config",
// "prompts") para os clientes do stream SSE. Publish nunca bloqueia: um
// assinante que não acompanha é desligado (o EventSource do navegador
// reconecta sozinho).
type Hub struct {
	// Heartbeat é o intervalo do comentário de keep-alive no stream.
	Heartbeat time.Duration

	mu     sync.Mutex
	subs   map[chan string]struct{}
	closed bool
}

// NewHub cria um Hub com heartbeat de 25 s.
func NewHub() *Hub {
	return &Hub{Heartbeat: 25 * time.Second, subs: map[chan string]struct{}{}}
}

// Subscribe registra um assinante. O canal é fechado quando o assinante é
// descartado, quando cancel é chamado ou quando o Hub é fechado.
func (h *Hub) Subscribe() (<-chan string, func()) {
	ch := make(chan string, subscriberBuffer)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(ch)
		return ch, func() {}
	}
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.drop(ch)
	}
}

// drop remove e fecha o canal; exige h.mu.
func (h *Hub) drop(ch chan string) {
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

// Publish avisa todos os assinantes, sem bloquear.
func (h *Hub) Publish(kind string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- kind:
		default:
			h.drop(ch)
		}
	}
}

// Close desliga todos os assinantes; Publish posterior não faz nada.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.subs {
		h.drop(ch)
	}
}

// ServeHTTP atende GET /api/stream: eventos `changed` com
// `data: {"kind":"..."}` e um comentário de heartbeat periódico.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream não suportado")
		return
	}
	ch, cancel := h.Subscribe()
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": conectado\n\n")
	flusher.Flush()

	tick := time.NewTicker(h.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case kind, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: changed\ndata: {\"kind\":%q}\n\n", kind); err != nil {
				return
			}
			flusher.Flush()
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
