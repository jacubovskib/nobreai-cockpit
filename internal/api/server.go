// Package api expõe a API HTTP do nobred (REST + stream SSE).
package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
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
	// ResolveTerminal resolve o binário do terminal (nil = launch.ResolveBinary).
	ResolveTerminal launch.Resolver
	// SessionPATH é o PATH exportado no script do terminal aberto pelo
	// "Continuar" (vazio = o PATH do daemon).
	SessionPATH string
	// UserPATH, se definido, vale no lugar de SessionPATH: o PATH do shell de
	// login do usuário (launch.UserPATH). maxAge=0 consulta agora.
	UserPATH PathSource
	// Resources mede CPU, memória e disco dos worktrees (nil = sem medição;
	// /api/worktrees devolve resources null).
	Resources ResourceReader
	// Hub é compartilhado com quem mais publica mudanças (adaptadores,
	// reconciliação); o dono deve chamar Hub.Close no desligamento.
	Hub *Hub
	// Terminals gerencia os terminais (PTY) da plataforma; nil = rotas respondem 503.
	Terminals *pty.Manager
	// Integration é o monitor da integração com o agente; nil = rotas 503.
	Integration IntegrationSource
	// Home é a raiz do seletor de pastas (vazio = os.UserHomeDir()); existe para os testes.
	Home string
}

type server struct {
	st   *store.Store
	deps Deps
	hub  *Hub
}

// New monta o roteador da API.
// PathSource devolve o PATH do usuário, aceitando um resultado de até maxAge.
type PathSource interface {
	Get(maxAge time.Duration) string
}

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
	s.overviewRoutes(mux)
	s.hookRoutes(mux)
	s.agentRoutes(mux)
	s.integrationRoutes(mux)
	s.worktreeRoutes(mux)
	s.terminalRoutes(mux)
	s.planLimitsRoutes(mux)
	s.fsRoutes(mux)
	mux.Handle("/", spaHandler(distFS()))
	return localOnly(mux)
}

// msgRemoteForbidden responde a conexões que não vêm da própria máquina.
const msgRemoteForbidden = "acesso permitido só a partir desta máquina"

// localOnly protege a API local contra acesso remoto, DNS rebinding e CSRF:
//   - A conexão precisa vir de um endereço loopback (r.RemoteAddr).
//   - Host precisa ser um IP 127.x.x.x:<porta> ou localhost:<porta>, onde <porta> é
//     a porta em que o servidor escuta (lida de http.LocalAddrContextKey).
//     Sem endereço local no contexto (handler chamado direto, como nos
//     testes com httptest.NewRecorder) qualquer porta é aceita.
//   - Toda requisição fora GET/HEAD (mesmo sem corpo) precisa de Content-Type
//     application/json: formulários e text/plain de outro site não passam
//     sem preflight de CORS, que a API não responde.
//
// O proxy do Vite em desenvolvimento precisa de changeOrigin para que o Host
// chegue como 127.0.0.1:7420.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackRemote(r.RemoteAddr) {
			writeError(w, http.StatusForbidden, msgRemoteForbidden)
			return
		}
		if !allowedHost(r) {
			writeError(w, http.StatusForbidden, "host não permitido")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "use Content-Type: application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackRemote diz se o endereço remoto (host:porta) é loopback.
func loopbackRemote(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackHost: "localhost" ou um IPv4 literal em 127.0.0.0/8 (NOBRE_ADDR pode
// escolher qualquer um deles). Nome de outro domínio nunca passa, mesmo que
// resolva para 127.0.0.1 (DNS rebinding).
func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil && ip.IsLoopback()
}

func allowedHost(r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || !loopbackHost(host) {
		return false
	}
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		_, localPort, err := net.SplitHostPort(addr.String())
		return err == nil && port == localPort
	}
	return port != ""
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
	case errors.Is(err, store.ErrFamily):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), store.ErrFamily.Error()+": "))
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
