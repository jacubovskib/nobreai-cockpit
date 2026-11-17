package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	maxEnvValue        = 32 << 10
	termOriginPlatform = "plataforma"
	termOriginRun      = "nobre run"
)

var resumeKeyRe = regexp.MustCompile(`^NAC-[0-9]+$`)

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
		// ResumeKey liga o agente ao card (vira NOBRE_RESUME); `nobre run` o repassa.
		ResumeKey string `json:"resume_key"`
		// Env é a lista permitida de variáveis do CLI (pty.AllowedEnv).
		Env map[string]string `json:"env"`
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
	if in.ResumeKey != "" && !resumeKeyRe.MatchString(in.ResumeKey) {
		writeError(w, http.StatusBadRequest, "resume_key inválida (use NAC-<número>)")
		return
	}
	for k, v := range in.Env {
		if !pty.AllowedEnv(k) || strings.ContainsRune(v, 0) || len(v) > maxEnvValue {
			writeError(w, http.StatusBadRequest, "variável de ambiente não permitida: "+k)
			return
		}
	}
	if in.Cols > maxTermDim || in.Rows > maxTermDim {
		writeError(w, http.StatusBadRequest, "tamanho de terminal inválido")
		return
	}
	path := s.sessionPATH(0)
	bin := ad.Binary()
	if bin == "" {
		writeError(w, http.StatusConflict, msgNoAgentBin)
		return
	}
	env := s.terminalEnv(path)
	for k, v := range in.Env {
		env = setEnv(env, k, v)
	}
	if in.ResumeKey != "" {
		env = setEnv(env, "NOBRE_RESUME", in.ResumeKey)
		if in.CardKey == "" { // `nobre run` dentro de um card: o terminal aparece nele
			in.CardKey = in.ResumeKey
		}
	}
	tm, err := s.startTerminal(pty.StartSpec{
		Argv: append([]string{bin}, in.Args...), Dir: in.Dir, Env: env,
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
	writeJSON(w, http.StatusCreated, tm.Info())
}

// startTerminal inicia o terminal no Manager e publica "terminals" ao criar e
// ao encerrar. Quem inicia terminais (API, Continuar em segundo plano) usa-o.
func (s *server) startTerminal(spec pty.StartSpec) (*pty.Terminal, error) {
	tm, err := s.deps.Terminals.Start(spec)
	if err != nil {
		return nil, err
	}
	s.publish("terminals")
	go func() {
		<-tm.Done()
		s.publish("terminals")
	}()
	return tm, nil
}

// terminalEnv é o ambiente do daemon com o PATH de sessão, TERM do xterm e
// cor de 24 bits. NOBRE_RESUME/NOBRE_AGENT_PID herdados do daemon ligariam o
// agente ao card ou processo errado: só valem os da requisição.
func (s *server) terminalEnv(path string) []string {
	env := make([]string, 0, 64)
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "PATH", "TERM", "COLORTERM", "NOBRE_RESUME", "NOBRE_AGENT_PID":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH="+path, "TERM=xterm-256color", "COLORTERM=truecolor")
}

// setEnv define k=v substituindo ocorrências anteriores (o getenv da libc
// devolve a primeira).
func setEnv(env []string, k, v string) []string {
	out := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, k+"=") {
			out = append(out, kv)
		}
	}
	return append(out, k+"="+v)
}

func (s *server) deleteTerminal(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Terminals.Kill(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, msgTerminalMissing)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// wsOriginOK aceita Origin ausente (cliente CLI) ou http://<host>:<porta> com
// host loopback (localhost ou 127.0.0.0/8, a mesma regra do allowedHost) e a
// porta de escuta.
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
	u, err := url.Parse(origin)
	if err != nil || port == "" || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return loopbackHost(u.Hostname()) && u.Port() == port
}

type wsInfo struct {
	Type string `json:"type"`
	pty.Info
}

// wsExit: Reason é pty.EndAgent, EndKilled ou EndDaemon.
type wsExit struct {
	Type   string `json:"type"`
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

// wsCloseSlow é o motivo de fechamento do cliente que não acompanhou a saída.
const wsCloseSlow = "lento"

// terminalWS: GET /api/terminals/{id}/ws. Servidor: binário = saída do
// terminal (primeiro o snapshot), texto JSON info/exit. Cliente: binário =
// entrada, texto JSON {"type":"resize","cols","rows"}. ?readonly=1 ignora
// entrada e resize. Só uma goroutine escreve no socket.
func (s *server) terminalWS(w http.ResponseWriter, r *http.Request) {
	if !wsOriginOK(r) {
		writeError(w, http.StatusForbidden, "origem não permitida")
		return
	}
	tm, ok := s.deps.Terminals.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, msgTerminalMissing)
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
	// No desligamento, Close espera este cliente entregar a mensagem de fim.
	release, _ := s.deps.Terminals.Hold()
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	snap, out, unsub := tm.Subscribe()
	defer unsub()

	go func() { // leitor: entrada e resize; só erro do socket derruba a conexão
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
				// Agente que não lê (prazo) ou PTY com erro: descarta esta
				// entrada e segue; a conexão continua mostrando a saída.
				if err := tm.Write(data); err != nil && !errors.Is(err, pty.ErrEnded) {
					log.Printf("terminal %s: entrada descartada: %v", tm.Info().ID, err)
				}
				continue
			}
			var m struct {
				Type       string `json:"type"`
				Cols, Rows uint16
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "resize" &&
				m.Cols > 0 && m.Rows > 0 && m.Cols <= maxTermDim && m.Rows <= maxTermDim {
				if err := tm.Resize(m.Cols, m.Rows); err != nil && !errors.Is(err, pty.ErrEnded) {
					log.Printf("terminal %s: resize: %v", tm.Info().ID, err)
				}
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
	// O snapshot vai sempre, mesmo vazio: a interface só libera a entrada
	// depois de escrevê-lo (respostas a consultas do histórico não viram tecla).
	if !write(websocket.MessageBinary, snap) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case b, open := <-out:
			if !open {
				if in := tm.Info(); in.ExitCode != nil {
					exit, _ := json.Marshal(wsExit{Type: "exit", Code: *in.ExitCode, Reason: in.EndReason})
					if write(websocket.MessageText, exit) {
						conn.Close(websocket.StatusNormalClosure, "")
					}
					return
				}
				conn.Close(websocket.StatusPolicyViolation, wsCloseSlow)
				return
			}
			if !write(websocket.MessageBinary, b) {
				return
			}
		}
	}
}
