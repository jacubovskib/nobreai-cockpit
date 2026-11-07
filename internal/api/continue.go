package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const (
	msgModeUnavailable  = "modo ainda não disponível no piloto"
	msgAgentUnavailable = "agente indisponível no piloto"
	msgDirtyTree        = "O repositório tem mudanças não salvas; faça commit ou stash antes de continuar."
	// slugNone é o "prompt" que só entrega o pacote de retomada (via
	// NOBRE_RESUME e SessionStart), sem texto renderizado.
	slugNone = "nenhum"
	// maxPromptBytes limita o prompt inicial, que viaja na linha de comando.
	maxPromptBytes  = 100 << 10
	msgPromptTooBig = `prompt grande demais para abrir no terminal; use "Nenhum"`
)

// continueCard: POST /api/cards/{key}/continue {agent, mode, prompt_slug, force}
// abre o agente no diretório do projeto, no branch do card, numa nova janela
// do terminal.
func (s *server) continueCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Agent      string `json:"agent"`
		Mode       string `json:"mode"`
		PromptSlug string `json:"prompt_slug"`
		Force      bool   `json:"force"`
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
	set, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	mode := in.Mode
	if mode == "" || mode == "platform" {
		mode = set.LaunchMode
	}
	if mode != "terminal" {
		writeError(w, http.StatusBadRequest, msgModeUnavailable)
		return
	}
	if msg := validateLaunch(set.LaunchMode, set.TerminalCmd); msg != "" {
		writeError(w, http.StatusInternalServerError, "configuração do terminal inválida: "+msg)
		return
	}
	if s.deps.Launcher == nil {
		writeError(w, http.StatusServiceUnavailable, "lançador indisponível")
		return
	}
	c, err := s.st.GetCard(r.PathValue("key"))
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	p, err := s.projectByID(c.ProjectID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if p == nil || p.Path == "" {
		writeError(w, http.StatusConflict, "o card não tem um projeto com diretório definido")
		return
	}
	if !in.Force {
		live, err := s.st.HasLiveSession(c.ID)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if live {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": c.Key + " está com uma sessão viva de " + s.liveAgent(c) + ". Encerre-a ou use forçar.",
				"code":  "live_session",
			})
			return
		}
	}
	initial := "Continue o card " + c.Key + "."
	usedSlug := ""
	if in.PromptSlug != slugNone {
		var pr domain.Prompt
		if in.PromptSlug == "" {
			pr, err = s.st.DefaultHandoffPrompt()
			if errors.Is(err, store.ErrNotFound) {
				pr, err = domain.Prompt{}, nil
			}
		} else {
			pr, err = s.st.GetPrompt(in.PromptSlug)
		}
		if err != nil {
			writeStoreError(w, err, msgPromptNotFound)
			return
		}
		if pr.Slug != "" {
			text, ok := s.renderBody(w, pr.Body, c.Key)
			if !ok {
				return
			}
			if text != "" {
				initial = text
			}
			usedSlug = pr.Slug
		}
	}
	if len(initial) > maxPromptBytes || strings.ContainsRune(initial, 0) {
		writeError(w, http.StatusUnprocessableEntity, msgPromptTooBig)
		return
	}
	if err := s.checkoutCardBranch(c, p.Path); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	resolve := s.deps.ResolveTerminal
	if resolve == nil {
		resolve = launch.ResolveBinary
	}
	argv := launch.TerminalArgvWith(resolve, set.TerminalCmd, p.Path, map[string]string{"NOBRE_RESUME": c.Key}, ad.LaunchCommand(initial))
	if err := s.deps.Launcher.Start(argv); err != nil {
		writeError(w, http.StatusInternalServerError, "não consegui abrir o terminal: "+err.Error())
		return
	}
	if usedSlug != "" {
		// O terminal já abriu: falha ao registrar o uso só é logada.
		if err := s.st.MarkPromptUsed(usedSlug, s.now()); err != nil {
			log.Printf("continue: registrar uso do prompt %s: %v", usedSlug, err)
		} else {
			s.publish("prompts")
		}
	}
	s.publish("cards")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// checkoutCardBranch troca o repositório para o branch do card, se for outro
// e a árvore estiver limpa.
func (s *server) checkoutCardBranch(c domain.Card, dir string) error {
	g := s.deps.Git
	if g == nil || c.Branch == "" || g.Branch(dir) == c.Branch {
		return nil
	}
	if !g.Clean(dir) {
		return errors.New(msgDirtyTree)
	}
	if err := g.Checkout(dir, c.Branch); err != nil {
		return errors.New("não consegui trocar para o branch " + c.Branch + ": " + err.Error())
	}
	return nil
}

// liveAgent é o agente da sessão viva dona do card.
func (s *server) liveAgent(c domain.Card) string {
	sessions, err := s.st.SessionsForCard(c.ID)
	if err == nil {
		for _, x := range sessions {
			if owns(x, c.ID) && x.Live() {
				return agentName(x.Agent)
			}
		}
	}
	return "outro agente"
}
