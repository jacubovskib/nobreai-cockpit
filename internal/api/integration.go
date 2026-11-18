package api

import (
	"net/http"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/integration"
)

// IntegrationSource é o monitor da integração com o agente (nil em Deps =
// rotas respondem 503 e a Sala de controle não mostra o alerta).
type IntegrationSource interface {
	Problems() []integration.Problem
	CheckNow() []integration.Problem
	Ignore(key string) error
	Fixed(keys, changed []string)
	Removed(keys []string, origin string)
}

const msgNoIntegration = "monitor de integração indisponível"

// removableKeys são os itens que o usuário pode tirar de propósito.
var removableKeys = map[string]bool{"hooks": true, "statusline": true, "skill": true}

func (s *server) integrationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/integration", s.integrationProblems)
	mux.HandleFunc("POST /api/integration/check", s.integrationCheck)
	mux.HandleFunc("POST /api/integration/removed", s.integrationRemoved)
	mux.HandleFunc("POST /api/integration/{key}/ignore", s.integrationIgnore)
}

func (s *server) integrationSource(w http.ResponseWriter) (IntegrationSource, bool) {
	if s.deps.Integration == nil {
		writeError(w, http.StatusServiceUnavailable, msgNoIntegration)
		return nil, false
	}
	return s.deps.Integration, true
}

func nonNilProblems(p []integration.Problem) []integration.Problem {
	if p == nil {
		return []integration.Problem{}
	}
	return p
}

func (s *server) integrationProblems(w http.ResponseWriter, r *http.Request) {
	src, ok := s.integrationSource(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, nonNilProblems(src.Problems()))
}

func (s *server) integrationCheck(w http.ResponseWriter, r *http.Request) {
	src, ok := s.integrationSource(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, nonNilProblems(src.CheckNow()))
}

func (s *server) integrationIgnore(w http.ResponseWriter, r *http.Request) {
	src, ok := s.integrationSource(w)
	if !ok {
		return
	}
	if err := src.Ignore(r.PathValue("key")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// integrationRemoved recebe do `nobre setup --uninstall-*` o aviso de que o
// item saiu de propósito, antes de o arquivo mudar.
func (s *server) integrationRemoved(w http.ResponseWriter, r *http.Request) {
	src, ok := s.integrationSource(w)
	if !ok {
		return
	}
	var in struct {
		Keys   []string `json:"keys"`
		Origin string   `json:"origin"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Keys) == 0 {
		writeError(w, http.StatusBadRequest, "keys não pode ser vazio")
		return
	}
	for _, k := range in.Keys {
		if !removableKeys[k] {
			writeError(w, http.StatusBadRequest, "chave desconhecida: "+k)
			return
		}
	}
	if in.Origin != "cli" && in.Origin != "config" {
		in.Origin = "cli"
	}
	src.Removed(in.Keys, in.Origin)
	w.WriteHeader(http.StatusNoContent)
}

// installPlan mostra o que "Instalar o que falta" mudaria, sem escrever.
func (s *server) installPlan(w http.ResponseWriter, r *http.Request) {
	ad, ok := s.agentAdapter(w, r)
	if !ok {
		return
	}
	pl, ok := ad.(adapter.InstallPlanner)
	if !ok {
		writeError(w, http.StatusNotImplemented, "este agente não calcula o plano de instalação")
		return
	}
	rep, err := pl.PlanInstall()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rep.Changed == nil {
		rep.Changed = []string{}
	}
	if rep.Messages == nil {
		rep.Messages = []string{}
	}
	writeJSON(w, http.StatusOK, rep)
}
