// Package pty gerencia pseudo-terminais dos agentes rodando sob o nobred.
package pty

import "sync"

// Ring é um buffer circular que guarda os últimos size bytes escritos.
type Ring struct {
	mu   sync.Mutex
	buf  []byte
	size int
	pos  int  // próxima posição de escrita
	full bool // buf já deu a volta
}

// NewRing cria um Ring com capacidade size (mínimo 1).
func NewRing(size int) *Ring {
	if size < 1 {
		size = 1
	}
	return &Ring{buf: make([]byte, size), size: size}
}

// Write acrescenta p, descartando o que exceder a capacidade. Nunca falha.
func (r *Ring) Write(p []byte) (int, error) {
	n := len(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= r.size {
		copy(r.buf, p[len(p)-r.size:])
		r.pos, r.full = 0, true
		return n, nil
	}
	c := copy(r.buf[r.pos:], p)
	if c < len(p) {
		copy(r.buf, p[c:])
		r.full = true
	}
	r.pos += len(p)
	if r.pos >= r.size {
		r.pos -= r.size
		r.full = true
	}
	return n, nil
}

// Snapshot devolve uma cópia dos bytes guardados, do mais antigo ao mais novo.
func (r *Ring) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]byte(nil), r.buf[:r.pos]...)
	}
	out := make([]byte, 0, r.size)
	out = append(out, r.buf[r.pos:]...)
	return append(out, r.buf[:r.pos]...)
}
