package api

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const (
	msgModeUnavailable  = "modo ainda não disponível no piloto"
	msgAgentUnavailable = "agente indisponível no piloto"
	msgDirtyTree        = "O repositório tem mudanças não salvas; faça commit ou stash antes de continuar."
	// slugNone é o "prompt" que só entrega o pacote de retomada (via
	// NOBRE_RESUME e SessionStart), sem texto renderizado.
	slugNone = "nenhum"
	// maxArgBytes limita cada argumento do comando final (o prompt viaja nele).
	maxArgBytes     = 128 << 10
	msgPromptTooBig = `prompt grande demais para abrir no terminal; use "Nenhum"`
	msgNoAgentBin   = "não encontrei o comando claude; rode nobre setup de um terminal onde `claude` funciona"
)

// continuePrompt monta o texto inicial que o Continuar envia ao agente: o
// prompt escolhido (slug), o padrão de handoff ("") ou nenhum ("nenhum").
// É o mesmo para o envio e para a prévia. Em erro já respondeu.
func (s *server) continuePrompt(w http.ResponseWriter, c domain.Card, slug string) (initial, usedSlug string, ok bool) {
	initial = "Continue o card " + c.Key + "."
	if slug == slugNone {
		return initial, "", true
	}
	var pr domain.Prompt
	var err error
	if slug == "" {
		pr, err = s.st.DefaultHandoffPrompt()
		if errors.Is(err, store.ErrNotFound) {
			pr, err = domain.Prompt{}, nil
		}
	} else {
		pr, err = s.st.GetPrompt(slug)
	}
	if err != nil {
		writeStoreError(w, err, msgPromptNotFound)
		return "", "", false
	}
	if pr.Slug == "" {
		return initial, "", true
	}
	// O pacote de retomada chega pelo SessionStart (NOBRE_RESUME); repeti-lo
	// no prompt só duplicaria o contexto: {{card.pacote}} fica vazio.
	text, ok := s.renderCardBody(w, pr.Body, c.Key, false)
	if !ok {
		return "", "", false
	}
	if text = strings.TrimRight(text, " \t\r\n"); text != "" {
		initial = text
	}
	// As mesmas recusas do envio: a prévia não mostra o que não iria.
	if strings.ContainsRune(initial, 0) || len(initial) >= maxArgBytes {
		writeError(w, http.StatusUnprocessableEntity, msgPromptTooBig)
		return "", "", false
	}
	return initial, pr.Slug, true
}

// continuePreview: POST /api/cards/{key}/continue/preview {prompt_slug}
// devolve o texto que o Continuar enviaria, sem abrir nada nem contar uso.
func (s *server) continuePreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PromptSlug string `json:"prompt_slug"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	c, err := s.st.GetCard(r.PathValue("key"))
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	text, slug, ok := s.continuePrompt(w, c, in.PromptSlug)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text, "prompt_slug": slug})
}

// continueCard: POST /api/cards/{key}/continue {agent, mode, prompt_slug, force}
// abre o agente no diretório do projeto, no branch do card, numa nova janela
// do terminal. Se o branch do card está aberto num worktree ligado, abre lá.
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
	background := mode == "background"
	if mode != "terminal" && !background {
		writeError(w, http.StatusBadRequest, msgModeUnavailable)
		return
	}
	if background {
		if s.deps.Terminals == nil {
			writeError(w, http.StatusServiceUnavailable, msgNoTerminals)
			return
		}
	} else {
		if msg := validateLaunch(set.LaunchMode, set.TerminalCmd); msg != "" {
			writeError(w, http.StatusInternalServerError, "configuração do terminal inválida: "+msg)
			return
		}
		if s.deps.Launcher == nil {
			writeError(w, http.StatusServiceUnavailable, "lançador indisponível")
			return
		}
	}
	// PATH do usuário consultado agora: o adaptador procura o CLI nele (com
	// cache curto) e o terminal o recebe.
	path := s.sessionPATH(0)
	// Caminho absoluto: o terminal e o systemd não têm ~/.local/bin no PATH.
	bin := ad.Binary()
	if bin == "" {
		writeError(w, http.StatusConflict, msgNoAgentBin)
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
	initial, usedSlug, ok := s.continuePrompt(w, c, in.PromptSlug)
	if !ok {
		return
	}
	// Branch do card aberto num worktree ligado: o agente abre lá, sem
	// checkout no principal (o git recusaria o branch já aberto).
	// Grupo com pasta: o agente abre na pasta, sem branch, worktree nem checkout
	// (o git nunca roda na pasta de um grupo).
	dir, inWorktree, skipCheckout := p.Path, false, p.IsGroup()
	if !skipCheckout {
		dir, inWorktree = s.cardWorktreeDir(c, p.Path)
	}
	cmd := ad.LaunchCommand(bin, initial)
	argv := cmd
	if !background {
		resolve := s.deps.ResolveTerminal
		if resolve == nil {
			resolve = launch.ResolveBinary
		}
		argv = launch.TerminalArgvWith(resolve, set.TerminalCmd, path, dir, map[string]string{"NOBRE_RESUME": c.Key}, cmd)
	}
	for _, a := range argv {
		if len(a) >= maxArgBytes { // MAX_ARG_STRLEN do Linux é 131072
			writeError(w, http.StatusUnprocessableEntity, msgPromptTooBig)
			return
		}
	}
	if !inWorktree && !skipCheckout {
		if err := s.checkoutCardBranch(c, p.Path); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	terminalID := ""
	if background {
		tm, err := s.startTerminal(pty.StartSpec{
			Argv: argv, Dir: dir, Env: setEnv(s.terminalEnv(path), "NOBRE_RESUME", c.Key),
			CardKey: c.Key, Agent: in.Agent, Origin: termOriginPlatform,
		})
		switch {
		case errors.Is(err, pty.ErrTooMany):
			writeError(w, http.StatusConflict, err.Error())
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "não consegui iniciar o terminal: "+err.Error())
			return
		}
		terminalID = tm.Info().ID
	} else if err := s.deps.Launcher.Start(argv); err != nil {
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
	resp := map[string]any{"ok": true}
	if terminalID != "" {
		resp["terminal_id"] = terminalID
	}
	writeJSON(w, http.StatusOK, resp)
}

// sessionPATH é o PATH das sessões abertas pelo daemon (terminal ou PTY):
// o do shell de login do usuário (UserPATH, até maxAge), senão o fixo.
func (s *server) sessionPATH(maxAge time.Duration) string {
	if s.deps.UserPATH != nil {
		return s.deps.UserPATH.Get(maxAge)
	}
	if s.deps.SessionPATH != "" {
		return s.deps.SessionPATH
	}
	return os.Getenv("PATH")
}

// cardWorktreeDir devolve onde abrir o agente: o diretório do projeto dentro
// do worktree ligado que tem o branch do card aberto (true), ou projectDir
// (false) quando não há tal worktree ou o git não lê worktrees.
func (s *server) cardWorktreeDir(c domain.Card, projectDir string) (string, bool) {
	wr, ok := s.deps.Git.(gitinfo.WorktreeReader)
	if !ok || c.Branch == "" {
		return projectDir, false
	}
	list, err := wr.Worktrees(projectDir)
	if err != nil || len(list) == 0 {
		return projectDir, false
	}
	// O projeto pode ser um subdiretório do repositório.
	rel, err := filepath.Rel(list[0].Path, projectDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = "."
	}
	for _, wt := range list {
		if !wt.Main && !wt.Prunable && !wt.Bare && wt.Branch == c.Branch {
			return filepath.Join(wt.Path, rel), true
		}
	}
	return projectDir, false
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
