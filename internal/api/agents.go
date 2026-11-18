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
	mux.HandleFunc("GET /api/agents/{agent}/install/plan", s.installPlan)
	mux.HandleFunc("POST /api/agents/{agent}/statusline", s.agentStatusLine)
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

// agentAdapter acha o adaptador do agente da rota; responde 501 (agente
// conhecido sem adaptador) ou 404 e devolve false quando não há.
func (s *server) agentAdapter(w http.ResponseWriter, r *http.Request) (adapter.Adapter, bool) {
	name := r.PathValue("agent")
	if ad, ok := s.deps.Adapters[name]; ok {
		return ad, true
	}
	for _, n := range agentOrder {
		if n == name {
			writeError(w, http.StatusNotImplemented, msgAdapterLater)
			return nil, false
		}
	}
	writeError(w, http.StatusNotFound, "agente desconhecido")
	return nil, false
}

// writeInstallReport responde o relatório de instalação e avisa a UI.
func (s *server) writeInstallReport(w http.ResponseWriter, rep adapter.InstallReport, err error) {
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

// installAgent instala hooks e skill do agente, no lado do daemon (com os
// caminhos do usuário que roda o nobred). Não mexe na status line.
func (s *server) installAgent(w http.ResponseWriter, r *http.Request) {
	ad, ok := s.agentAdapter(w, r)
	if !ok {
		return
	}
	rep, err := ad.Install()
	if err == nil {
		s.markFixed(rep, "hooks", "skill")
	}
	s.writeInstallReport(w, rep, err)
}

// markFixed avisa o monitor do conserto. Sem nada alterado não houve conserto
// (nem linha no histórico): o monitor só confere de novo.
func (s *server) markFixed(rep adapter.InstallReport, keys ...string) {
	if s.deps.Integration == nil {
		return
	}
	if len(rep.Changed) == 0 {
		s.deps.Integration.CheckNow()
		return
	}
	s.deps.Integration.Fixed(keys, rep.Changed)
}

// agentStatusLine integra ou remove a status line do agente: ação explícita,
// separada de "Instalar o que falta", porque mexe num comando do usuário.
func (s *server) agentStatusLine(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string `json:"action"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Action != "install" && in.Action != "uninstall" {
		writeError(w, http.StatusBadRequest, `action deve ser "install" ou "uninstall"`)
		return
	}
	ad, ok := s.agentAdapter(w, r)
	if !ok {
		return
	}
	sl, ok := ad.(adapter.StatusLiner)
	if !ok {
		writeError(w, http.StatusNotImplemented, "este agente não tem status line integrável")
		return
	}
	var rep adapter.InstallReport
	var err error
	if in.Action == "install" {
		rep, err = sl.InstallStatusLine()
		if err == nil {
			s.markFixed(rep, "statusline")
		}
	} else {
		// A marca de remoção deliberada vem antes de o arquivo mudar, para o
		// monitor não alertar a remoção que o usuário acabou de pedir.
		if s.deps.Integration != nil {
			s.deps.Integration.Removed([]string{"statusline"}, "config")
		}
		rep, err = sl.UninstallStatusLine()
	}
	s.writeInstallReport(w, rep, err)
}
