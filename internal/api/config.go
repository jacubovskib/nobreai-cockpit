package api

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
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
	s.configLogRoutes(mux)
	mux.HandleFunc("GET /api/limits", s.listLimits)
	mux.HandleFunc("PUT /api/limits", s.putLimit)
	mux.HandleFunc("GET /api/prices", s.listPrices)
	mux.HandleFunc("PUT /api/prices", s.putPrice)
	mux.HandleFunc("DELETE /api/prices", s.deletePrice)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("POST /api/projects/batch", s.batchProjects)
	mux.HandleFunc("PATCH /api/projects/{id}", s.patchProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/projects/{id}/scan", s.scanProject)
	mux.HandleFunc("GET /api/projects/{id}/suggestions", s.suggestions)
	mux.HandleFunc("POST /api/projects/{id}/suggestions/dismiss", s.dismissSuggestions)
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
	// Cópia funda: readJSON decodifica sobre os mapas e fatias existentes.
	old := st
	old.Rules = maps.Clone(st.Rules)
	old.TerminalCmd = slices.Clone(st.TerminalCmd)
	// readJSON decodifica sobre a fatia existente: sem cópia, old mudaria junto e o diff do log perderia a troca.
	old.ContextWarnPcts = slices.Clone(st.ContextWarnPcts)
	old.ModelWindows = maps.Clone(st.ModelWindows)
	keepCmd := old.TerminalCmd
	// Janelas por modelo: o corpo substitui o mapa inteiro (decodificar sobre o atual só mesclaria).
	keepWindows := st.ModelWindows
	st.ModelWindows = nil
	if !readJSON(w, r, &st) {
		return
	}
	if st.ModelWindows == nil {
		st.ModelWindows = keepWindows
	}
	if st.ContextWarnPcts == nil {
		st.ContextWarnPcts = []int{}
	}
	if st.TerminalCmd == nil { // omitido ou null: mantém o atual
		st.TerminalCmd = keepCmd
	}
	if msg := validateLaunch(st.LaunchMode, st.TerminalCmd); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if msg := validateContextSettings(st); msg != "" {
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
	case st.MemoryBudgetTokens < 0 || st.MemoryBudgetTokens > 2000:
		writeError(w, http.StatusBadRequest, "limite das memórias deve ficar entre 0 e 2000")
		return
	case st.WhereStoppedTTLMin < 0 || st.GraceMin < 0 || st.ReminderEditThreshold < 0 || st.BRLRate < 0:
		writeError(w, http.StatusBadRequest, "valores numéricos não podem ser negativos")
		return
	}
	if err := s.st.PutSettings(st); err != nil {
		writeStoreError(w, err, "")
		return
	}
	changes, err := changedSettings(old, st)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	notified := 0
	for _, c := range changes {
		s.logConfig(domain.LogSettingChanged, c)
		if string(c["key"]) == `"agent_creates_cards"` {
			if notified, err = s.countStaleCardsRule(st.AgentCreatesCards); err != nil {
				writeStoreError(w, err, "")
				return
			}
		}
	}
	s.publish("config")
	fresh, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		domain.Settings
		NotifiedSessions int `json:"notified_sessions"`
	}{fresh, notified})
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

// writeProjectError traduz os erros de cadastro: nome ou pasta repetidos
// viram 409 e as regras de família do store viram 400 com a mensagem da regra.
func writeProjectError(w http.ResponseWriter, err error, notFound string) {
	if isConstraint(err, "UNIQUE") {
		writeError(w, http.StatusConflict, "já existe um projeto com esse caminho")
		return
	}
	writeStoreError(w, err, notFound)
}

func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		BaseBranch string `json:"base_branch"`
		ParentID   *int64 `json:"parent_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// Sem pasta e sem pai é um grupo: projeto principal que só reúne filhos.
	if in.ParentID == nil && strings.TrimSpace(in.Path) == "" {
		if strings.TrimSpace(in.Name) == "" {
			writeError(w, http.StatusBadRequest, "nome obrigatório")
			return
		}
		p, err := s.st.CreateGroup(strings.TrimSpace(in.Name))
		if err != nil {
			writeProjectError(w, err, "")
			return
		}
		s.publish("config")
		writeJSON(w, http.StatusCreated, struct {
			domain.Project
			Reassigned  int                     `json:"reassigned"`
			Moved       store.Moved             `json:"moved"`
			Suggestions []workspaces.Suggestion `json:"suggestions"`
		}{p, 0, store.Moved{}, []workspaces.Suggestion{}})
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
	var (
		p     domain.Project
		moved store.Moved
		err   error
	)
	if in.ParentID != nil {
		p, moved, err = s.st.CreateChild(in.Name, in.Path, *in.ParentID)
	} else {
		p, err = s.st.CreateProject(in.Name, in.Path)
	}
	if err != nil {
		writeProjectError(w, err, "projeto pai não encontrado")
		return
	}
	// A base de um app de monorepo é a do repositório do pai.
	repo, err := s.st.RepoProject(p)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if repo.ID == p.ID {
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
	// Só informativo: falha ao ler os workspaces não desfaz o cadastro.
	sugg := []workspaces.Suggestion{}
	if p.ParentID == nil {
		if x, err := s.openSuggestions(p); err == nil {
			sugg = x
		}
	}
	writeJSON(w, http.StatusCreated, struct {
		domain.Project
		Reassigned  int                     `json:"reassigned"`
		Moved       store.Moved             `json:"moved"`
		Suggestions []workspaces.Suggestion `json:"suggestions"`
	}{p, n, moved, sugg})
}

// readPatchBody lê o corpo do PATCH de projeto: aplica os campos em p e devolve
// os campos presentes (para distinguir "parent_id": null de ausente).
func readPatchBody(w http.ResponseWriter, r *http.Request, p *domain.Project) (map[string]json.RawMessage, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(body)) == 0 {
		return fields, true
	}
	if err := json.Unmarshal(body, p); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return nil, false
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return nil, false
	}
	return fields, true
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
	oldBase, oldPath, oldParent, oldCapture := p.BaseBranch, p.Path, p.ParentID, p.Capture
	repo, err := s.st.RepoProject(*p)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	isApp := repo.ID != p.ID
	fields, ok := readPatchBody(w, r, p)
	if !ok {
		return
	}
	p.ID = id
	p.ParentID = oldParent // o pai muda só por "parent_id", abaixo
	if isApp {
		if p.BaseBranch != oldBase {
			writeError(w, http.StatusBadRequest, "a base de um app é a do projeto principal "+repo.Name)
			return
		}
		p.BaseBranch = oldBase // omitida ou igual: segue a do repositório do pai
	}
	if strings.TrimSpace(p.Name) == "" {
		writeError(w, http.StatusBadRequest, "nome obrigatório")
		return
	}
	// Grupo (sem pasta) segue sem pasta; senão a pasta é absoluta.
	if p.Path != "" || oldPath != "" {
		if !filepath.IsAbs(p.Path) {
			writeError(w, http.StatusBadRequest, "caminho do projeto deve ser absoluto")
			return
		}
		p.Path = filepath.Clean(p.Path)
		if homeOrAncestor(p.Path) {
			writeError(w, http.StatusBadRequest, msgHomeProject)
			return
		}
	}
	var newParent *int64
	setParent := false
	if raw, has := fields["parent_id"]; has {
		if err := json.Unmarshal(raw, &newParent); err != nil {
			writeError(w, http.StatusBadRequest, "parent_id inválido")
			return
		}
		setParent = (newParent == nil) != (oldParent == nil) || (newParent != nil && *newParent != *oldParent)
	}
	if err := s.st.UpdateProject(*p); err != nil {
		writeProjectError(w, err, "projeto não encontrado")
		return
	}
	var moved store.Moved
	if setParent {
		if moved, err = s.st.SetParent(id, newParent); err != nil {
			writeProjectError(w, err, "projeto pai não encontrado")
			return
		}
	}
	fresh, err := s.projectByID(id)
	if err != nil || fresh == nil {
		writeStoreError(w, err, "projeto não encontrado")
		return
	}
	// Grupo que ganhou ou trocou de pasta adota as sessões soltas que ficam nela.
	if fresh.IsGroup() && fresh.Path != "" && fresh.Path != oldPath {
		n, err := s.adoptRootSessions(*fresh)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		moved.Sessions += n
	}
	// Mudar a captura vale para as sessões vivas da família; o filho segue o pai,
	// então só a mudança de quem tem o controle chega aqui.
	notified := 0
	if fresh.Capture != oldCapture {
		if notified, err = s.applyProjectCapture(id, fresh.Capture); err != nil {
			writeStoreError(w, err, "")
			return
		}
		s.logConfig(domain.LogProjectCapture, map[string]any{
			"project_id": id, "project": fresh.Name, "capture": fresh.Capture, "notified_sessions": notified,
		})
	}
	s.publish("config", "sessions", "cards")
	writeJSON(w, http.StatusOK, struct {
		domain.Project
		Moved            store.Moved `json:"moved"`
		NotifiedSessions int         `json:"notified_sessions"`
	}{*fresh, moved, notified})
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

// validateContextSettings confere as preferências do medidor de contexto; "" = válidas.
func validateContextSettings(st domain.Settings) string {
	prev := 0
	for _, p := range st.ContextWarnPcts {
		if p < 1 || p > 99 || p <= prev {
			return "context_warn_pcts deve ter percentuais entre 1 e 99, em ordem crescente"
		}
		prev = p
	}
	if st.BigOutputTokens < 1000 {
		return "big_output_tokens deve ser de pelo menos 1000"
	}
	if st.BigOutputMax < 0 || st.BigOutputMax > 20 {
		return "big_output_max deve ficar entre 0 e 20"
	}
	for m, w := range st.ModelWindows {
		if m == "" || w < 1000 {
			return "model_windows: modelo não pode ser vazio e a janela deve ser de pelo menos 1000"
		}
	}
	return ""
}
