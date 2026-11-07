package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/nobretech/nobreai-cockpit/internal/pty"
)

const (
	msgNoTerminals     = "terminais indisponíveis"
	msgTerminalMissing = "terminal não encontrado"
	wsWriteTimeout     = 10 * time.Second
	wsReadLimit        = 1 << 20
	maxTermDim         = 1000
	termOriginPlatform = "plataforma"
	termOriginRun      = "nobre run"
)

func (s *server) terminalRoutes(mux *http.ServeMux) {
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.deps.Terminals == nil {
				writeError(w, http.StatusServiceUnavailable, msgNoTerminals)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/terminals", guard(s.listTerminals))
	mux.HandleFunc("POST /api/terminals", guard(s.createTerminal))
	mux.HandleFunc("DELETE /api/terminals/{id}", guard(s.deleteTerminal))
	mux.HandleFunc("GET /api/terminals/{id}/ws", guard(s.terminalWS))
}

func (s *server) listTerminals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, nonNil(s.deps.Terminals.List()))
}

// createTerminal: POST /api/terminals inicia o agente num novo PTY.
func (s *server) createTerminal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Agent   string   `json:"agent"`
		Dir     string   `json:"dir"`
		Args    []string `json:"args"`
		Cols    uint16   `json:"cols"`
		Rows    uint16   `json:"rows"`
		CardKey string   `json:"card_key"`
		Origin  string   `json:"origin"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Agent == "" {
		in.Agent = "claude"
	}
	ad, ok := s.deps.Adapters[in.Agent]
	if in.Agent != "claude" || !ok {
		writeError(w, http.StatusBadRequest, msgAgentUnavailable)
		return
	}
	switch in.Origin {
	case "":
		in.Origin = termOriginPlatform
	case termOriginPlatform, termOriginRun:
	default:
		writeError(w, http.StatusBadRequest, "origem inválida")
		return
	}
	if !filepath.IsAbs(in.Dir) {
		writeError(w, http.StatusBadRequest, "dir precisa ser um caminho absoluto")
		return
	}
	if fi, err := os.Stat(in.Dir); err != nil || !fi.IsDir() {
		writeError(w, http.StatusBadRequest, "dir não existe: "+in.Dir)
		return
	}
	if in.Cols > maxTermDim || in.Rows > maxTermDim {
		writeError(w, http.StatusBadRequest, "tamanho de terminal inválido")
		return
	}
	bin := ad.Binary()
	if bin == "" {
		writeError(w, http.StatusConflict, msgNoAgentBin)
		return
	}
	tm, err := s.deps.Terminals.Start(pty.StartSpec{
		Argv: append([]string{bin}, in.Args...), Dir: in.Dir, Env: s.terminalEnv(),
		Cols: in.Cols, Rows: in.Rows, CardKey: in.CardKey, Agent: in.Agent, Origin: in.Origin,
	})
	switch {
	case errors.Is(err, pty.ErrTooMany):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "não consegui iniciar o terminal: "+err.Error())
		return
	}
	s.publish("terminals")
	go func() {
		<-tm.Done()
		s.publish("terminals")
	}()
	writeJSON(w, http.StatusCreated, tm.Info())
}

// terminalEnv é o ambiente do daemon com o PATH de sessão e TERM do xterm.
func (s *server) terminalEnv() []string {
	env := make([]string, 0, 64)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "TERM=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH="+s.sessionPATH(), "TERM=xterm-256color")
}

func (s *server) deleteTerminal(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Terminals.Kill(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, msgTerminalMissing)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// wsOriginOK aceita Origin ausente (cliente CLI) ou exatamente
// http://127.0.0.1:<porta> / http://localhost:<porta> da porta de escuta.
func wsOriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	port := ""
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		_, port, _ = net.SplitHostPort(addr.String())
	} else {
		_, port, _ = net.SplitHostPort(r.Host)
	}
	return port != "" && (origin == "http://127.0.0.1:"+port || origin == "http://localhost:"+port)
}

type wsInfo struct {
	Type string `json:"type"`
	pty.Info
}

type wsExit struct {
	Type string `json:"type"`
	Code int    `json:"code"`
}

// terminalWS: GET /api/terminals/{id}/ws. Servidor: binário = saída do
// terminal (primeiro o snapshot), texto JSON info/exit. Cliente: binário =
// entrada, texto JSON {"type":"resize","cols","rows"}. ?readonly=1 ignora
// entrada e resize. Só uma goroutine escreve no socket.
func (s *server) terminalWS(w http.ResponseWriter, r *http.Request) {
	tm, ok := s.deps.Terminals.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, msgTerminalMissing)
		return
	}
	if !wsOriginOK(r) {
		writeError(w, http.StatusForbidden, "origem não permitida")
		return
	}
	readonly := r.URL.Query().Get("readonly") == "1"
	// A checagem de Origin acima é nossa (aceita a ausência, usada pelo CLI);
	// a da biblioteca rejeitaria esse caso.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(wsReadLimit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snap, out, unsub := tm.Subscribe()
	defer unsub()

	go func() { // leitor: entrada e resize; qualquer erro derruba a conexão
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if readonly {
				continue
			}
			if typ == websocket.MessageBinary {
				if err := tm.Write(data); err != nil && !errors.Is(err, pty.ErrEnded) {
					return
				}
				continue
			}
			var m struct {
				Type       string `json:"type"`
				Cols, Rows uint16
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "resize" &&
				m.Cols > 0 && m.Rows > 0 && m.Cols <= maxTermDim && m.Rows <= maxTermDim {
				_ = tm.Resize(m.Cols, m.Rows)
			}
		}
	}()

	write := func(typ websocket.MessageType, b []byte) bool {
		wctx, wcancel := context.WithTimeout(ctx, wsWriteTimeout)
		defer wcancel()
		return conn.Write(wctx, typ, b) == nil
	}
	info, _ := json.Marshal(wsInfo{Type: "info", Info: tm.Info()})
	if !write(websocket.MessageText, info) {
		return
	}
	if len(snap) > 0 && !write(websocket.MessageBinary, snap) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case b, open := <-out:
			if !open {
				if code := tm.Info().ExitCode; code != nil {
					exit, _ := json.Marshal(wsExit{Type: "exit", Code: *code})
					if write(websocket.MessageText, exit) {
						conn.Close(websocket.StatusNormalClosure, "")
					}
					return
				}
				conn.Close(websocket.StatusPolicyViolation, "cliente lento demais")
				return
			}
			if !write(websocket.MessageBinary, b) {
				return
			}
		}
	}
}
