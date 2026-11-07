package api

import (
	"context"
	"log"
	"net/http"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

func (s *server) hookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/hooks/{agent}", s.hook)
}

// hook repassa a chamada do `nobre hook <agente> <Evento>` ao adaptador. O
// adaptador recebe um contexto desligado da requisição: o `nobre hook` desiste
// em 2 s, mas o que começou a ser gravado termina. Erro do adaptador vai para
// o log e a resposta é vazia com exit 0: hook nunca quebra o agente.
func (s *server) hook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("agent")
	ad, ok := s.deps.Adapters[name]
	if !ok {
		writeError(w, http.StatusNotFound, "agente desconhecido")
		return
	}
	var in adapter.HookCall
	if !readJSON(w, r, &in) {
		return
	}
	reply, err := ad.HandleHook(context.WithoutCancel(r.Context()), in)
	if err != nil {
		log.Printf("hook %s %s: %v", name, in.Event, err)
		reply = adapter.HookReply{}
	}
	writeJSON(w, http.StatusOK, reply)
}
