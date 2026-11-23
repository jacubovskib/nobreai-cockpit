package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

// maxHookBody é o teto do corpo de um hook: PostToolUse de Write leva o
// conteúdo do arquivo duas vezes, e o de Read leva o arquivo lido. Bem acima
// do teto geral da API (maxBody).
const maxHookBody = 32 << 20

func (s *server) hookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/hooks/{agent}", s.hook)
}

// hook repassa a chamada do `nobre hook <agente> <Evento>` ao adaptador. O
// adaptador recebe um contexto desligado da requisição: o `nobre hook` desiste
// em 2 s, mas o que começou a ser gravado termina. Erro do adaptador vai para
// o log e a resposta é vazia com exit 0: hook nunca quebra o agente. Corpo
// recusado (grande demais ou JSON inválido) também vai para o log, com o
// tamanho, porque o evento se perde.
func (s *server) hook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("agent")
	ad, ok := s.deps.Agents.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "agente desconhecido")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHookBody))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		log.Printf("hook %s %s recusado: corpo com mais de %d bytes (Content-Length %d bytes)", name, hookEvent(body), maxHookBody, r.ContentLength)
		writeError(w, http.StatusRequestEntityTooLarge, "corpo do hook grande demais")
		return
	}
	if err != nil {
		log.Printf("hook %s recusado: leitura do corpo (%d bytes lidos): %v", name, len(body), err)
		writeError(w, http.StatusBadRequest, "corpo do hook ilegível")
		return
	}
	var in adapter.HookCall
	if err := json.Unmarshal(body, &in); err != nil {
		log.Printf("hook %s %s recusado: JSON inválido (%d bytes): %v", name, hookEvent(body), len(body), err)
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	reply, err := ad.HandleHook(context.WithoutCancel(r.Context()), in)
	if err != nil {
		log.Printf("hook %s %s: %v", name, in.Event, err)
		reply = adapter.HookReply{}
	}
	writeJSON(w, http.StatusOK, reply)
}

var eventField = regexp.MustCompile(`"event"\s*:\s*"([A-Za-z]+)"`)

// hookEvent tenta achar o nome do evento no começo de um corpo que não pôde
// ser decodificado (o HookCall serializa "event" primeiro).
func hookEvent(body []byte) string {
	if m := eventField.FindSubmatch(body[:min(len(body), 4096)]); m != nil {
		return string(m[1])
	}
	return "(evento desconhecido)"
}
