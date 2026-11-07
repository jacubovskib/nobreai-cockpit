package api

import (
	"net/http"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

// agentOrder é a ordem fixa da lista de agentes.
var agentOrder = []string{"claude", "codex", "kiro"}

const msgAdapterLater = "Adaptador chega no plano 2"

type agentInfo struct {
	Agent     string            `json:"agent"`
	Detection adapter.Detection `json:"detection"`
}

func (s *server) agentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents", s.listAgents)
	mux.HandleFunc("POST /api/agents/{agent}/install", s.installAgent)
}

// listAgents devolve a detecção de cada agente. Agentes sem adaptador
// aparecem como não encontrados.
func (s *server) listAgents(w http.ResponseWriter, r *http.Request) {
	out := make([]agentInfo, 0, len(agentOrder))
	for _, name := range agentOrder {
		if ad, ok := s.deps.Adapters[name]; ok {
			d := ad.Detect()
			if d.Checks == nil {
				d.Checks = []adapter.Check{}
			}
			out = append(out, agentInfo{Agent: name, Detection: d})
			continue
		}
		out = append(out, agentInfo{Agent: name, Detection: adapter.Detection{
			Checks: []adapter.Check{{Key: "adapter", Label: "Adaptador", Detail: msgAdapterLater}},
		}})
	}
	writeJSON(w, http.StatusOK, out)
}

// installAgent instala hooks e skill do agente, no lado do daemon (com os
// caminhos do usuário que roda o nobred).
func (s *server) installAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("agent")
	ad, ok := s.deps.Adapters[name]
	if !ok {
		for _, n := range agentOrder {
			if n == name {
				writeError(w, http.StatusNotImplemented, msgAdapterLater)
				return
			}
		}
		writeError(w, http.StatusNotFound, "agente desconhecido")
		return
	}
	rep, err := ad.Install()
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
	s.publish("config")
	writeJSON(w, http.StatusOK, rep)
}
