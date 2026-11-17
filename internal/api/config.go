package api

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

const (
	msgPriceRequired = "Informe os preços de entrada e saída (maiores que zero)"
	msgHomeProject   = "a pasta pessoal, a raiz e as pastas acima da pessoal não podem ser cadastradas como projeto"
)

// homeOrAncestor diz se path (limpo) é a raiz, a pasta pessoal ou uma pasta
// que a contém: cadastrá-la engoliria todas as sessões do usuário.
func homeOrAncestor(path string) bool {
	if path == string(filepath.Separator) {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	home = filepath.Clean(home)
	return home == path || strings.HasPrefix(home, path+string(filepath.Separator))
}

func (s *server) configRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("GET /api/limits", s.listLimits)
	mux.HandleFunc("PUT /api/limits", s.putLimit)
	mux.HandleFunc("GET /api/prices", s.listPrices)
	mux.HandleFunc("PUT /api/prices", s.putPrice)
	mux.HandleFunc("DELETE /api/prices", s.deletePrice)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("PATCH /api/projects/{id}", s.patchProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/projects/{id}/scan", s.scanProject)
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// putSettings aplica o corpo sobre as preferências atuais: chaves ausentes
// mantêm o valor, e "rules" é mesclado regra a regra.
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	keepCmd := st.TerminalCmd
	if !readJSON(w, r, &st) {
		return
	}
	if st.TerminalCmd == nil { // omitido ou null: mantém o atual
		st.TerminalCmd = keepCmd
	}
	if msg := validateLaunch(st.LaunchMode, st.TerminalCmd); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	switch {
	case st.IncompleteAction != "ask_agent" && st.IncompleteAction != "mark":
		writeError(w, http.StatusBadRequest, "incomplete_action inválida (use ask_agent ou mark)")
		return
	case st.Currency != "USD" && st.Currency != "BRL":
		writeError(w, http.StatusBadRequest, "moeda inválida (use USD ou BRL)")
		return
	case st.PlanWarnPct < 1 || st.PlanWarnPct > 100:
		writeError(w, http.StatusBadRequest, "plan_warn_pct deve ficar entre 1 e 100")
		return
	case st.WhereStoppedTTLMin < 0 || st.GraceMin < 0 || st.ReminderEditThreshold < 0 || st.BRLRate < 0:
		writeError(w, http.StatusBadRequest, "valores numéricos não podem ser negativos")
		return
	}
	if err := s.st.PutSettings(st); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config")
	s.getSettings(w, r)
}

func (s *server) listLimits(w http.ResponseWriter, r *http.Request) {
	ls, err := s.st.ListLimits()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ls))
}

// putLimit grava (cria ou atualiza) o limite de um agente.
func (s *server) putLimit(w http.ResponseWriter, r *http.Request) {
	l := domain.Limit{WarnPct: 80, Action: "warn"}
	if !readJSON(w, r, &l) {
		return
	}
	switch {
	case strings.TrimSpace(l.Agent) == "":
		writeError(w, http.StatusBadRequest, "agente obrigatório")
		return
	case l.Action != "pause" && l.Action != "warn":
		writeError(w, http.StatusBadRequest, "ação inválida (use pause ou warn)")
		return
	case l.DailyUSD < 0 || l.CardUSD < 0 || l.WarnPct < 0 || l.WarnPct > 100:
		writeError(w, http.StatusBadRequest, "valores do limite fora do intervalo")
		return
	}
	if err := s.st.PutLimit(l); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config")
	writeJSON(w, http.StatusOK, l)
}

func (s *server) listPrices(w http.ResponseWriter, r *http.Request) {
	ps, err := s.st.ListPrices()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ps))
}

// putPrice grava (cria ou atualiza) o preço de um modelo. Preço muda custo,
// por isso publica também "usage".
func (s *server) putPrice(w http.ResponseWriter, r *http.Request) {
	var p domain.Price
	if !readJSON(w, r, &p) {
		return
	}
	if strings.TrimSpace(p.Model) == "" {
		writeError(w, http.StatusBadRequest, "modelo obrigatório")
		return
	}
	if p.InputPerM <= 0 || p.OutputPerM <= 0 {
		writeError(w, http.StatusBadRequest, msgPriceRequired)
		return
	}
	// Preço de cache ausente (null) = desconhecido; 0 = grátis.
	if (p.CacheWritePerM != nil && *p.CacheWritePerM < 0) || (p.CacheReadPerM != nil && *p.CacheReadPerM < 0) {
		writeError(w, http.StatusBadRequest, "preço não pode ser negativo")
		return
	}
	if err := s.st.PutPrice(p); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config", "usage")
	writeJSON(w, http.StatusOK, p)
}

// deletePrice remove o preço de ?model=.
func (s *server) deletePrice(w http.ResponseWriter, r *http.Request) {
	model := r.URL.Query().Get("model")
	if model == "" {
		writeError(w, http.StatusBadRequest, "modelo obrigatório")
		return
	}
	if err := s.st.DeletePrice(model); err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("config", "usage")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(ps))
}

func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		BaseBranch string `json:"base_branch"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !filepath.IsAbs(in.Path) {
		writeError(w, http.StatusBadRequest, "caminho do projeto deve ser absoluto")
		return
	}
	in.Path = filepath.Clean(in.Path)
	if homeOrAncestor(in.Path) {
		writeError(w, http.StatusBadRequest, msgHomeProject)
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = filepath.Base(in.Path)
	}
	p, err := s.st.CreateProject(in.Name, in.Path)
	if isConstraint(err, "UNIQUE") {
		writeError(w, http.StatusConflict, "já existe um projeto com esse caminho")
		return
	}
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	base := strings.TrimSpace(in.BaseBranch)
	if base == "" {
		if br := s.branchReader(); br != nil {
			base = br.DetectBase(p.Path)
		}
	}
	if base != "" {
		if err := s.st.SetProjectBase(p.ID, base); err != nil {
			writeStoreError(w, err, "")
			return
		}
		p.BaseBranch = base
	}
	// A sessão aberta antes do cadastro passa a pertencer ao projeto.
	n, err := s.st.ReassignSessions(p.ID, p.Path)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	// Sessões em worktrees ligados (fora do diretório do projeto) também.
	if wr, ok := s.deps.Git.(gitinfo.WorktreeReader); ok {
		wts, err := s.st.UnassignedLinkedWorktrees()
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		for _, wt := range wts {
			if filepath.Clean(wr.Locate(wt).Main) != p.Path {
				continue
			}
			m, err := s.st.AssignSessionsInWorktree(p.ID, wt)
			if err != nil {
				writeStoreError(w, err, "")
				return
			}
			n += m
		}
	}
	s.publish("config", "sessions", "cards")
	writeJSON(w, http.StatusCreated, struct {
		domain.Project
		Reassigned int `json:"reassigned"`
	}{p, n})
}

func (s *server) patchProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.projectByID(id)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "projeto não encontrado")
		return
	}
	oldBase := p.BaseBranch
	isApp := false
	if repo, err := s.st.RepoProject(*p); err == nil && repo.ID != p.ID {
		isApp = true
	}
	if !readJSON(w, r, p) {
		return
	}
	p.ID = id
	if isApp {
		p.BaseBranch = oldBase // a base de um app é a do repositório do pai
	}
	if strings.TrimSpace(p.Name) == "" {
		writeError(w, http.StatusBadRequest, "nome obrigatório")
		return
	}
	if !filepath.IsAbs(p.Path) {
		writeError(w, http.StatusBadRequest, "caminho do projeto deve ser absoluto")
		return
	}
	p.Path = filepath.Clean(p.Path)
	if homeOrAncestor(p.Path) {
		writeError(w, http.StatusBadRequest, msgHomeProject)
		return
	}
	err = s.st.UpdateProject(*p)
	if isConstraint(err, "UNIQUE") {
		writeError(w, http.StatusConflict, "já existe um projeto com esse caminho")
		return
	}
	if err != nil {
		writeStoreError(w, err, "projeto não encontrado")
		return
	}
	s.publish("config")
	writeJSON(w, http.StatusOK, p)
}

func (s *server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.st.DeleteProject(id)
	if isConstraint(err, "FOREIGN KEY") {
		writeError(w, http.StatusConflict, "o projeto tem cards, sessões ou prompts ligados e não pode ser removido")
		return
	}
	if err != nil {
		writeStoreError(w, err, "projeto não encontrado")
		return
	}
	s.publish("config")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

var bareCommand = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

// validateLaunch confere o modo de abertura e o comando do terminal; devolve
// a mensagem de erro ("" se está tudo certo).
func validateLaunch(mode string, cmd []string) string {
	switch mode {
	case "terminal", "tmux", "background":
	default:
		return "launch_mode inválido (use terminal, tmux ou background)"
	}
	if len(cmd) == 0 || len(cmd) > 8 {
		return "terminal_cmd deve ter de 1 a 8 elementos"
	}
	for _, c := range cmd {
		if c == "" {
			return "terminal_cmd não pode ter elementos vazios"
		}
	}
	if !filepath.IsAbs(cmd[0]) && !bareCommand.MatchString(cmd[0]) {
		return "terminal_cmd: o primeiro elemento deve ser um caminho absoluto ou o nome de um comando (sem caracteres especiais do shell)"
	}
	return ""
}
