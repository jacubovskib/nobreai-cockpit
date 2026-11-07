// Package api expõe a API HTTP do nobred (REST + stream SSE).
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Version é a versão do nobred informada em /api/health.
const Version = "0.1.0"

// Deps são as dependências do servidor. Campos nil recebem padrão em New
// (Now = time.Now, Hub = NewHub()); Adapters, Launcher e Git podem ficar nil.
type Deps struct {
	Now      func() time.Time
	ProcRoot string
	Adapters map[string]adapter.Adapter
	Launcher launch.Launcher
	Git      gitinfo.GitInfo
	// Hub é compartilhado com quem mais publica mudanças (adaptadores,
	// reconciliação); o dono deve chamar Hub.Close no desligamento.
	Hub *Hub
}

type server struct {
	st   *store.Store
	deps Deps
	hub  *Hub
}

// New monta o roteador da API.
func New(st *store.Store, deps Deps) http.Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Hub == nil {
		deps.Hub = NewHub()
	}
	s := &server{st: st, deps: deps, hub: deps.Hub}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
	})
	mux.Handle("GET /api/stream", s.hub)
	s.cardRoutes(mux)
	s.meRoutes(mux)
	s.configRoutes(mux)
	s.promptRoutes(mux)
	s.usageRoutes(mux)
	return mux
}

func (s *server) now() time.Time { return s.deps.Now() }

// publish avisa o stream; cada escrita chama com o(s) kind(s) afetado(s).
func (s *server) publish(kinds ...string) {
	for _, k := range kinds {
		s.hub.Publish(k)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// writeStoreError traduz erros do store; ErrNotFound vira 404 com notFound.
func writeStoreError(w http.ResponseWriter, err error, notFound string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, notFound)
	default:
		writeError(w, http.StatusInternalServerError, "erro interno: "+err.Error())
	}
}

// isConstraint diz se o erro do SQLite é uma violação do tipo dado
// ("UNIQUE", "FOREIGN KEY", "CHECK").
func isConstraint(err error, kind string) bool {
	return err != nil && strings.Contains(err.Error(), kind+" constraint failed")
}

const maxBody = 1 << 20

// readJSON decodifica o corpo em v; corpo vazio é aceito (v fica como está).
// Em caso de erro já responde 400 e devolve false.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return false
	}
	return true
}

// nonNil troca slice nil por vazio, para o JSON sair [] e não null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
