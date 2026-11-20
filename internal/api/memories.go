package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/memory"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const (
	msgNoMemProject = "sessão sem projeto"
	msgMemNotFound  = "memória não encontrada"
)

func (s *server) memoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/memories", s.listMemories)
	mux.HandleFunc("POST /api/memories", s.createMemory)
	mux.HandleFunc("GET /api/memories/preview", s.previewMemories)
	mux.HandleFunc("POST /api/memories/import", s.importMemories)
	mux.HandleFunc("POST /api/memories/batch", s.batchMemories)
	mux.HandleFunc("PATCH /api/memories/{id}", s.patchMemory)
	mux.HandleFunc("DELETE /api/memories/{id}", s.deleteMemory) // não apaga: vira descartada
	mux.HandleFunc("POST /api/memories/{id}/move", s.moveMemory)
	mux.HandleFunc("POST /api/me/remember", s.withSession(s.meRemember))
	mux.HandleFunc("GET /api/me/memories", s.withSession(s.meMemories))
}

// writeMemoryError traduz erros de validação (400) e not found (404).
func writeMemoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrMemory) {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), store.ErrMemory.Error()+": "))
		return
	}
	writeStoreError(w, err, msgMemNotFound)
}

// cardMemoryCandidates cria as candidatas de um card que acabou de ir para
// concluido. Falha não atrapalha a movimentação do card: só vai para o log.
func (s *server) cardMemoryCandidates(c domain.Card) {
	n, err := s.st.CardMemoryCandidates(c.ID)
	if err != nil {
		log.Printf("memórias do card %s: %v", c.Key, err)
		return
	}
	if n > 0 {
		s.publish("memories")
	}
}

// memoryProject lê o projeto do parâmetro ?project= (400 se inválido, 404 se não existe).
func (s *server) memoryProjectParam(w http.ResponseWriter, r *http.Request, required bool) (*domain.Project, bool) {
	raw := r.URL.Query().Get("project")
	if raw == "" {
		if required {
			writeError(w, http.StatusBadRequest, "informe project")
			return nil, false
		}
		return nil, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "project inválido")
		return nil, false
	}
	p, err := s.projectByID(id)
	if err != nil {
		writeStoreError(w, err, "")
		return nil, false
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "projeto não encontrado")
		return nil, false
	}
	return p, true
}

type memoryOut struct {
	domain.Memory
	Inherited bool `json:"inherited"`
}

func (s *server) listMemories(w http.ResponseWriter, r *http.Request) {
	p, ok := s.memoryProjectParam(w, r, false)
	if !ok {
		return
	}
	f := store.MemoryFilter{Status: r.URL.Query().Get("status"), Term: r.URL.Query().Get("q")}
	switch f.Status {
	case "", "ativa", "candidata", "arquivada", "descartada":
	default:
		writeError(w, http.StatusBadRequest, "status inválido")
		return
	}
	exact := r.URL.Query().Get("exact") == "1"
	// family=1 numa raiz: também as memórias dos apps (filhos), que não são herdadas.
	family := r.URL.Query().Get("family") == "1" && p != nil && p.ParentID == nil
	if p != nil {
		f.ProjectIDs = []int64{p.ID}
		if family {
			ids, err := s.st.FamilyIDs(p.ID)
			if err != nil {
				writeStoreError(w, err, "")
				return
			}
			f.ProjectIDs = ids
		}
		if !exact {
			if root := store.RootID(*p); root != p.ID {
				f.ProjectIDs = append(f.ProjectIDs, root)
			}
			f.Global = true
		}
	}
	list, err := s.st.ListMemories(f)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	out := make([]memoryOut, 0, len(list))
	for _, m := range list {
		inherited := p != nil && (m.ProjectID == nil || *m.ProjectID != p.ID)
		if family && m.ProjectID != nil {
			inherited = false // da raiz ou de um app da família
		}
		out = append(out, memoryOut{m, inherited})
	}
	writeJSON(w, http.StatusOK, out)
}

type memoryIn struct {
	ProjectID *int64 `json:"project_id"`
	Category  string `json:"category"`
	Rule      string `json:"rule"`
	Do        string `json:"do"`
	Status    string `json:"status"`
}

// checkProjectExists responde 400 quando o escopo aponta para projeto inexistente.
func (s *server) checkProjectExists(w http.ResponseWriter, id *int64) bool {
	if id == nil {
		return true
	}
	p, err := s.projectByID(*id)
	if err != nil {
		writeStoreError(w, err, "")
		return false
	}
	if p == nil {
		writeError(w, http.StatusBadRequest, "projeto não encontrado")
		return false
	}
	return true
}

func (s *server) createMemory(w http.ResponseWriter, r *http.Request) {
	var in memoryIn
	if !readJSON(w, r, &in) || !s.checkProjectExists(w, in.ProjectID) {
		return
	}
	if in.Category == "" {
		in.Category = string(memory.CategoryDominio)
	}
	m, err := s.st.CreateMemory(domain.Memory{ProjectID: in.ProjectID, Category: in.Category,
		Rule: in.Rule, Do: in.Do, Status: in.Status, Source: "usuario"})
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	s.publish("memories")
	writeJSON(w, http.StatusCreated, m)
}

func memoryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id inválido")
		return 0, false
	}
	return id, true
}

func (s *server) patchMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := memoryID(w, r)
	if !ok {
		return
	}
	var in struct {
		ProjectID   json.RawMessage `json:"project_id"` // ausente = não muda; null = global
		Category    *string         `json:"category"`
		Rule        *string         `json:"rule"`
		Do          *string         `json:"do"`
		Status      *string         `json:"status"`
		NeedsReview *bool           `json:"needs_review"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	m, err := s.st.GetMemory(id)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	if in.ProjectID != nil {
		var pid *int64
		if err := json.Unmarshal(in.ProjectID, &pid); err != nil {
			writeError(w, http.StatusBadRequest, "project_id inválido")
			return
		}
		if !s.checkProjectExists(w, pid) {
			return
		}
		m.ProjectID = pid
	}
	if in.Category != nil {
		m.Category = *in.Category
	}
	if in.Rule != nil {
		m.Rule = *in.Rule
	}
	if in.Do != nil {
		m.Do = *in.Do
	}
	if in.Status != nil {
		m.Status = *in.Status
	}
	if in.NeedsReview != nil {
		m.NeedsReview = *in.NeedsReview
	}
	if in.Status != nil && *in.Status == "ativa" {
		m.NeedsReview = false // aprovar é revisar
	}
	m, err = s.st.UpdateMemory(m)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	s.publish("memories")
	writeJSON(w, http.StatusOK, m)
}

// deleteMemory é o "Excluir" das arquivadas. Não apaga a linha: a memória
// vira descartada (escondida da página e da sessão) e continua barrando a
// reimportação da mesma regra.
func (s *server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := memoryID(w, r)
	if !ok {
		return
	}
	if err := s.st.DiscardArchivedMemory(id); err != nil {
		writeMemoryError(w, err)
		return
	}
	s.publish("memories")
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) moveMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := memoryID(w, r)
	if !ok {
		return
	}
	var in struct {
		Dir int `json:"dir"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Dir != -1 && in.Dir != 1 {
		writeError(w, http.StatusBadRequest, "dir deve ser -1 ou 1")
		return
	}
	if err := s.st.MoveMemory(id, in.Dir); err != nil {
		writeMemoryError(w, err)
		return
	}
	s.publish("memories")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) batchMemories(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	status := map[string]string{"aprovar": "ativa", "descartar": "descartada", "arquivar": "arquivada"}[in.Action]
	if status == "" {
		writeError(w, http.StatusBadRequest, "ação inválida: use aprovar, descartar ou arquivar")
		return
	}
	if len(in.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "informe ids")
		return
	}
	type refusal struct {
		ID    int64  `json:"id"`
		Error string `json:"error"`
	}
	done, refused := []int64{}, []refusal{}
	for _, id := range in.IDs {
		m, err := s.st.GetMemory(id)
		if err == nil {
			m.Status = status
			if status == "ativa" {
				m.NeedsReview = false // aprovar é revisar
			}
			_, err = s.st.UpdateMemory(m)
		}
		switch {
		case err == nil:
			done = append(done, id)
		case errors.Is(err, store.ErrNotFound):
			refused = append(refused, refusal{id, msgMemNotFound})
		case errors.Is(err, store.ErrMemory):
			refused = append(refused, refusal{id, strings.TrimPrefix(err.Error(), store.ErrMemory.Error()+": ")})
		default:
			writeStoreError(w, err, "")
			return
		}
	}
	if len(done) > 0 {
		s.publish("memories")
	}
	writeJSON(w, http.StatusOK, map[string]any{"done": done, "refused": refused})
}

// sessionItems monta os itens injetáveis (app, raiz, global) de uma sessão.
func sessionItems(app, root, global []domain.Memory) []memory.Item {
	var items []memory.Item
	add := func(scope int, ms []domain.Memory) {
		for _, m := range ms {
			items = append(items, memory.Item{ID: m.ID, Scope: scope, Position: m.Position,
				Category: memory.Category(m.Category), Rule: m.Rule, Do: m.Do})
		}
	}
	add(0, app)
	add(1, root)
	add(2, global)
	return items
}

func (s *server) previewMemories(w http.ResponseWriter, r *http.Request) {
	p, ok := s.memoryProjectParam(w, r, true)
	if !ok {
		return
	}
	st, err := s.st.GetSettings()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	app, root, global, err := s.st.MemoriesForSession(domain.Session{ProjectID: &p.ID})
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	resp := map[string]any{"block": "", "tokens": 0, "chosen": 0, "left": 0}
	if st.MemoryBudgetTokens > 0 {
		chosen, left := memory.Select(sessionItems(app, root, global), st.MemoryBudgetTokens)
		resp = map[string]any{"block": memory.FormatBlock(chosen, left), "tokens": memory.UsedTokens(chosen),
			"chosen": len(chosen), "left": left}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- importação dos napkin.md ---

type importFile struct {
	ProjectID int64  `json:"project_id"`
	Path      string `json:"path"`
	Items     int    `json:"items"`
	Error     string `json:"error,omitempty"`
}

// importMu faz as importações andarem uma de cada vez.
var importMu sync.Mutex

// napkinOwners diz qual projeto responde por cada napkin.md: com a pasta
// repetida, a raiz (projeto sem pai) vale antes dos apps, depois o menor id.
func napkinOwners(projects []domain.Project) map[string]int64 {
	owner := map[string]int64{}
	rootOwned := map[string]bool{}
	for _, p := range projects {
		if p.Path == "" {
			continue
		}
		path := filepath.Join(p.Path, ".claude", "napkin.md")
		isRoot := p.ParentID == nil
		if _, ok := owner[path]; !ok || (isRoot && !rootOwned[path]) {
			owner[path] = p.ID
			rootOwned[path] = isRoot
		}
	}
	return owner
}

func (s *server) importMemories(w http.ResponseWriter, r *http.Request) {
	importMu.Lock()
	defer importMu.Unlock()
	projects, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	owner := napkinOwners(projects)
	perProject := map[int64][]memory.NapkinItem{}
	paths := map[int64]string{}
	files := []importFile{}
	for _, p := range projects {
		if p.Path == "" {
			continue
		}
		path := filepath.Join(p.Path, ".claude", "napkin.md")
		if owner[path] != p.ID {
			continue // pasta repetida: um arquivo só
		}
		items, err := readNapkin(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue // sem napkin: segue em silêncio
		}
		if err != nil {
			log.Printf("importar memórias: %s: %v", path, err)
			files = append(files, importFile{ProjectID: p.ID, Path: path, Error: err.Error()})
			continue
		}
		perProject[p.ID], paths[p.ID] = items, path
		files = append(files, importFile{ProjectID: p.ID, Path: path, Items: len(items)})
	}
	global, local := memory.GroupImport(perProject)
	// Texto vai inteiro (só o teto de segurança do store); acima de 300/400 a
	// candidata sai com needs_review. A data do item, quando há, vira a criação.
	var batch []store.ImportItem
	add := func(pid *int64, from []int64, it memory.NapkinItem, ref string) {
		rule, _ := memory.CleanText(it.Rule, 4000)
		do, _ := memory.CleanText(it.Do, 4000)
		if rule == "" {
			return
		}
		m := domain.Memory{ProjectID: pid, Category: string(it.Category), Rule: rule, Do: do,
			Status: "candidata", Source: "napkin", SourceRef: ref, NeedsReview: it.NeedsReview}
		if it.Date != "" {
			if d, err := time.ParseInLocation("2006-01-02", it.Date, time.Local); err == nil {
				m.CreatedAt = d
			}
		}
		batch = append(batch, store.ImportItem{Memory: m, From: from})
	}
	// merged: itens dos napkin.md que foram unidos numa candidata global.
	merged := 0
	for _, g := range global {
		var refs []string
		for _, id := range g.ProjectIDs {
			refs = append(refs, paths[id])
		}
		merged += len(g.ProjectIDs)
		add(nil, g.ProjectIDs, g.Item, strings.Join(refs, "; "))
	}
	ids := make([]int64, 0, len(local))
	for id := range local {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		pid := id
		for _, it := range local[id] {
			add(&pid, []int64{pid}, it, paths[id])
		}
	}
	created, skipped, err := s.st.ImportMemories(batch)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	if created > 0 {
		s.publish("memories")
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "skipped": skipped, "merged": merged, "files": files})
}

func readNapkin(path string) ([]memory.NapkinItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return memory.ParseNapkin(f)
}

// --- rotas do agente ---

type similarOut struct {
	ID   int64  `json:"id"`
	Rule string `json:"rule"`
}

func (s *server) meRemember(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Rule     string `json:"rule"`
		Do       string `json:"do"`
		Category string `json:"category"`
		Scope    string `json:"scope"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Rule) == "" {
		writeError(w, http.StatusBadRequest, "a regra não pode ser vazia")
		return
	}
	if in.Category == "" {
		in.Category = string(memory.CategoryDominio)
	}
	if in.Scope == "" {
		in.Scope = "projeto"
	}
	var pid *int64
	// Global vale em todo projeto: a do agente vai para a fila de aprovação.
	status := "ativa"
	switch in.Scope {
	case "global":
		status = "candidata"
	case "projeto", "raiz":
		p, err := s.st.EffectiveProject(sess)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if p == nil {
			writeError(w, http.StatusBadRequest, msgNoMemProject)
			return
		}
		id := p.ID
		if in.Scope == "raiz" {
			id = store.RootID(*p)
		}
		pid = &id
	default:
		writeError(w, http.StatusBadRequest, "escopo inválido: use projeto, raiz ou global")
		return
	}
	m, err := s.st.CreateMemory(domain.Memory{ProjectID: pid, Category: in.Category, Rule: in.Rule, Do: in.Do,
		Status: status, Source: "agente"})
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	app, root, global, err := s.st.MemoriesForSession(sess)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	similar := []similarOut{}
	for _, list := range [][]domain.Memory{app, root, global} {
		for _, o := range list {
			if o.ID != m.ID && memory.Similarity(o.Rule, m.Rule) >= 0.6 {
				similar = append(similar, similarOut{o.ID, o.Rule})
			}
		}
	}
	scope := "global" // o escopo gravado, no mesmo vocabulário de nobre memorias
	if m.ProjectID != nil {
		scope = "app"
		if p, _ := s.st.EffectiveProject(sess); p != nil && *m.ProjectID == store.RootID(*p) {
			scope = "raiz"
		}
	}
	s.publish("memories")
	writeJSON(w, http.StatusCreated, struct {
		domain.Memory
		Scope   string       `json:"scope"`
		Similar []similarOut `json:"similar"`
	}{m, scope, similar})
}

type sessionMemory struct {
	domain.Memory
	Scope string `json:"scope"` // app, raiz ou global
}

func (s *server) meMemories(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	app, root, global, err := s.st.MemoriesForSession(sess)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	term := memory.Normalize(r.URL.Query().Get("q"))
	out := []sessionMemory{}
	for _, g := range []struct {
		scope string
		list  []domain.Memory
	}{{"app", app}, {"raiz", root}, {"global", global}} {
		for _, m := range g.list {
			if term != "" && !strings.Contains(memory.Normalize(m.Rule), term) && !strings.Contains(memory.Normalize(m.Do), term) {
				continue
			}
			out = append(out, sessionMemory{m, g.scope})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
