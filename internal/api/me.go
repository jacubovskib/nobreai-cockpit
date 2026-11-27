package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

// Rotas do agente: a sessão é a sessão viva do processo informado em
// X-Nobre-Agent-PID (o CLI `nobre` descobre o PID do agente ancestral).

const (
	agentPIDHeader = "X-Nobre-Agent-PID"
	// agentSessionHeader é `<agente>:<id>` da variável de sessão do agente.
	agentSessionHeader = "X-Nobre-Agent-Session"
	msgNoSession       = "nenhuma sessão de agente encontrada para este processo"
	msgNoCard          = `Esta sessão ainda não tem card. Use nobre start "<título>" ou nobre resume <KEY>.`
	// msgNoProject é o aviso sem guia; noProjectMsg cita o do agente.
	msgNoProject = "Este repositório não está cadastrado no NobreAI Cockpit. Cadastre com nobre project add e faça o levantamento com nobre project scan."
	msgNotGit    = "Esta pasta não é um repositório git e não pode ser cadastrada no NobreAI Cockpit; a sessão segue sem card."
	// msgRepoFolder é o aviso numa pasta sem git que tem repositórios dentro
	// (%s é a pasta): ela pode virar grupo, com o aval do usuário.
	msgRepoFolder = "Esta pasta não é um repositório git, mas tem repositórios dentro e pode ser cadastrada como grupo. Pergunte ao usuário se quer cadastrá-la com nobre project add %s e só cadastre se ele aceitar; para criar o card num dos repositórios, rode o comando dentro dele. A sessão segue sem card até lá."
)

func (s *server) meRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/me/start", s.withSession(s.meStart))
	mux.HandleFunc("POST /api/me/cards", s.withSession(s.meCards))
	mux.HandleFunc("POST /api/me/card-set", s.withSession(s.meCardSet))
	mux.HandleFunc("POST /api/me/resume", s.withSession(s.meResume))
	mux.HandleFunc("GET /api/me/status", s.withSession(s.meStatus))
	mux.HandleFunc("POST /api/me/note", s.withSessionCard(s.meNote))
	mux.HandleFunc("POST /api/me/decide", s.withSessionCard(s.meContent(s.addDecision)))
	mux.HandleFunc("POST /api/me/file", s.withSessionCard(s.meContent(s.addFile)))
	mux.HandleFunc("POST /api/me/todo", s.withSessionCard(s.meContent(s.addTodo)))
	mux.HandleFunc("POST /api/me/plan", s.withSessionCard(s.meContent(s.setPlan)))
	mux.HandleFunc("POST /api/me/task", s.withSessionCard(s.meContent(s.setTask)))
	mux.HandleFunc("POST /api/me/ask", s.withSessionCard(s.meContent(s.addQuestion)))
	mux.HandleFunc("POST /api/me/answer", s.withSessionCard(s.meAnswer))
	mux.HandleFunc("POST /api/me/block", s.withSessionCard(s.meBlock))
	mux.HandleFunc("POST /api/me/todo-done", s.withSessionCard(s.meTodoDone))
	mux.HandleFunc("POST /api/me/test", s.withSessionCard(s.meTest))
	mux.HandleFunc("POST /api/me/branch", s.withSessionCard(s.meBranch))
	mux.HandleFunc("POST /api/me/app", s.withSessionCard(s.meApp))
	mux.HandleFunc("POST /api/me/done", s.withSessionCard(s.meDone))
}

type sessionHandler func(http.ResponseWriter, *http.Request, domain.Session)
type sessionCardHandler func(http.ResponseWriter, *http.Request, domain.Session, domain.Card)

func (s *server) withSession(h sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid, perr := strconv.Atoi(r.Header.Get(agentPIDHeader))
		hasPID := perr == nil && pid > 0
		if hasPID {
			sess, err := s.st.SessionByPID(pid)
			if err != nil {
				writeStoreError(w, err, "")
				return
			}
			if sess != nil {
				h(w, r, *sess)
				return
			}
		}
		// Sem PID que ache sessão viva: a variável de sessão do agente.
		if id, ok := agentSessionID(r.Header.Get(agentSessionHeader)); ok {
			sess, err := s.st.GetSession(id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				writeStoreError(w, err, "")
				return
			}
			if err == nil && sess.EndedAt == nil {
				h(w, r, sess)
				return
			}
			writeError(w, http.StatusNotFound, msgNoSession)
			return
		}
		if !hasPID {
			writeError(w, http.StatusBadRequest, "cabeçalho "+agentPIDHeader+" ausente ou inválido")
			return
		}
		writeError(w, http.StatusNotFound, msgNoSession)
	}
}

// agentSessionID valida `<agente>:<id>` (agente conhecido, tamanho razoável,
// sem espaços nem controle) e devolve o id da sessão no store.
func agentSessionID(v string) (string, bool) {
	name, rest, ok := strings.Cut(v, ":")
	if !ok || rest == "" || len(v) > 200 || !slices.Contains(agents.Known, name) {
		return "", false
	}
	for _, c := range v {
		if c <= ' ' || c == 0x7f {
			return "", false
		}
	}
	return v, true
}

func (s *server) withSessionCard(h sessionCardHandler) http.HandlerFunc {
	return s.withSession(func(w http.ResponseWriter, r *http.Request, sess domain.Session) {
		c, ok := s.sessionCard(w, sess)
		if ok {
			h(w, r, sess, c)
		}
	})
}

// sessionCard devolve o card da sessão ou responde 409.
func (s *server) sessionCard(w http.ResponseWriter, sess domain.Session) (domain.Card, bool) {
	if sess.CardID == nil {
		writeError(w, http.StatusConflict, msgNoCard)
		return domain.Card{}, false
	}
	c, err := s.st.GetCardByID(*sess.CardID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusConflict, msgNoCard)
		return c, false
	}
	if err != nil {
		writeStoreError(w, err, "")
		return c, false
	}
	return c, true
}

// meContent adapta um handler de conteúdo do card para a sessão do agente.
func (s *server) meContent(h func(http.ResponseWriter, *http.Request, domain.Card, string)) sessionCardHandler {
	return func(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
		h(w, r, c, sess.ID)
	}
}

// recordEvent grava um evento da sessão (no card atual dela) e atualiza a
// última atividade. Sem sessão (escrita do humano) não faz nada. Falha aqui
// não desfaz a escrita principal: só é registrada no log.
func (s *server) recordEvent(sessionID, kind string, data map[string]any) {
	if sessionID == "" {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		log.Printf("evento %s: %v", kind, err)
		return
	}
	now := s.now()
	if err := s.st.AddEvent(domain.Event{SessionID: sessionID, Kind: kind, Data: b, At: now}); err != nil {
		log.Printf("evento %s da sessão %s: %v", kind, sessionID, err)
		return
	}
	if err := s.st.TouchSession(sessionID, now); err != nil {
		log.Printf("atualizar sessão %s: %v", sessionID, err)
	}
	s.publish("sessions")
}

// commandDir limpa a pasta que a CLI informou (onde o comando rodou). ""
// quando ela não veio (CLI antiga) ou não é um caminho absoluto: aí vale só a
// regra da sessão.
func commandDir(cwd string) string {
	if !filepath.IsAbs(cwd) {
		return ""
	}
	return filepath.Clean(cwd)
}

// cardTarget é o projeto escolhido para o card que o comando cria. from é a
// pasta do comando quando foi ela que definiu o projeto ("" = foi a sessão);
// other diz que o projeto não é o da sessão; linkedRepo e linkedGroup vêm
// preenchidos quando o comando cadastrou o repositório no grupo (linkedDir é
// o nome da pasta, quando o projeto entrou com outro nome porque esse já
// existia no grupo); linkRefused é a pasta do repositório que o grupo
// linkedGroup recusou: o card ficou no grupo.
type cardTarget struct {
	id                      int64
	from                    string
	other                   bool
	linkedRepo, linkedGroup string
	linkedDir, linkRefused  string
	// O que linkRepo usa: o projeto da sessão, o da pasta do comando e o
	// repositório git dela.
	own   *int64
	found *domain.Project
	loc   gitinfo.Location
}

// commandProject escolhe o projeto do card que o comando cria: o projeto
// cadastrado mais fundo que contém cwd, a pasta onde o comando rodou (a regra
// do hook, store.ProjectForLocation, com a tradução do worktree ligado); sem
// projeto lá (ou sem cwd), o da sessão (o ligado ou o do cwd dela). Só
// escolhe, não grava nada: o repositório ainda não cadastrado dentro de um
// grupo com pasta é ligado depois, por linkRepo, quando o pedido já foi
// validado. Sem projeto, responde 409 com o aviso certo para a pasta.
func (s *server) commandProject(w http.ResponseWriter, sess domain.Session, cwd string) (cardTarget, bool) {
	fail := func(err error) (cardTarget, bool) {
		writeStoreError(w, err, "")
		return cardTarget{}, false
	}
	// O projeto da sessão, pela regra de antes.
	var own *int64
	if sess.ProjectID != nil {
		own = sess.ProjectID
	} else if p, err := s.st.ProjectForPath(sess.Cwd); err != nil {
		return fail(err)
	} else if p != nil {
		own = &p.ID
	}
	dir := commandDir(cwd)
	wr, _ := s.deps.Git.(gitinfo.WorktreeReader)
	if dir != "" {
		l := store.SessionLocation{Cwd: dir}
		var loc gitinfo.Location
		if wr != nil {
			loc = wr.Locate(dir)
			l.Worktree, l.WorktreeLinked, l.Main = loc.Root, loc.Linked, loc.Main
		}
		p, err := s.st.ProjectForLocation(dir, l)
		if err != nil {
			return fail(err)
		}
		if p != nil {
			return cardTarget{id: p.ID, from: dir, other: own == nil || *own != p.ID, own: own, found: p, loc: loc}, true
		}
	}
	if own != nil {
		return cardTarget{id: *own}, true
	}
	// O aviso fala da pasta do comando; sem ela, da pasta da sessão.
	if dir == "" {
		dir = sess.Cwd
	}
	msg := s.noProjectMsg(sess)
	if wr != nil && wr.Locate(dir).Root == "" {
		msg = msgNotGit
		if s.canBeGroup(dir) {
			msg = fmt.Sprintf(msgRepoFolder, dir)
		}
	}
	writeError(w, http.StatusConflict, msg)
	return cardTarget{}, false
}

// linkRepo liga ao grupo o repositório ainda não cadastrado da pasta do
// comando (linkRepoToGroup) e passa o card para ele. É a única gravação da
// escolha do projeto: os handlers chamam depois de validar o pedido, para um
// pedido recusado não deixar o repositório cadastrado.
func (s *server) linkRepo(w http.ResponseWriter, t *cardTarget) bool {
	if t.found == nil {
		return true
	}
	child, refused, err := s.linkRepoToGroup(*t.found, t.loc)
	switch {
	case err != nil:
		writeStoreError(w, err, "")
		return false
	case child != nil:
		t.id, t.linkedRepo, t.linkedGroup = child.ID, child.Name, t.found.Name
		if dir := filepath.Base(child.Path); dir != child.Name {
			t.linkedDir = dir
		}
		t.other = t.own == nil || *t.own != t.id
	case refused != "":
		t.linkRefused, t.linkedGroup = refused, t.found.Name
	}
	return true
}

// groupAccepts diz se o repositório da pasta root pode ser ligado ao grupo g
// sem o usuário pedir, e devolve o caminho dele relativo ao grupo. O cadastro
// do grupo é o consentimento, então só vale o que a varredura dele sugeriria:
// grupo com pasta, até 2 níveis, fora das pastas que ela pula, e nada que o
// usuário mandou não sugerir. Vale para o comando (linkRepoToGroup) e para o
// POST /api/projects sem pai (createProject).
func (s *server) groupAccepts(g domain.Project, root string) (string, bool, error) {
	if !g.IsGroup() || g.Path == "" {
		return "", false, nil
	}
	rel, err := filepath.Rel(filepath.Clean(g.Path), root)
	if err != nil || !workspaces.InScan(rel) {
		return "", false, nil
	}
	rel = filepath.ToSlash(rel)
	hidden, err := s.dismissedFor(g.ID)
	if err != nil {
		return "", false, err
	}
	return rel, !hidden[rel], nil
}

// linkRepoToGroup cadastra como filho do grupo g o repositório git de loc (o
// checkout principal: um worktree ligado nunca vira projeto) quando a pasta do
// comando caiu no grupo por falta de projeto mais fundo, dentro das travas de
// groupAccepts. Passa pelo cadastro em lote (addFamily), como os filhos marcados na
// interface; nome de pasta que já existe no grupo não impede: o projeto entra
// com um nome distinto (store.ChildSpec.Auto). Devolve nil quando não é o
// caso; se as regras de família recusam o cadastro, o card fica no grupo e
// refused é a pasta do repositório, para a CLI avisar.
func (s *server) linkRepoToGroup(g domain.Project, loc gitinfo.Location) (child *domain.Project, refused string, err error) {
	if loc.Root == "" || loc.Main == "" {
		return nil, "", nil
	}
	root := filepath.Clean(loc.Main)
	rel, ok, err := s.groupAccepts(g, root)
	if err != nil || !ok {
		return nil, "", err
	}
	_, kids, _, err := s.addFamily(g, []store.ChildSpec{{Name: filepath.Base(root), Path: root, Auto: true}}, false)
	if err != nil {
		if errors.Is(err, store.ErrFamily) || isConstraint(err, "UNIQUE") {
			log.Printf("ligar %s ao grupo %s: %v", root, g.Name, err)
			return nil, rel, nil
		}
		return nil, "", err
	}
	if len(kids) != 1 {
		return nil, "", nil
	}
	return &kids[0], "", nil
}

// writeCreated responde o card criado pelo comando com o que a CLI diz a mais:
// o projeto, quando não é o da sessão, e o repositório recém-ligado ao grupo
// (ou o que o grupo recusou).
func (s *server) writeCreated(w http.ResponseWriter, key string, t cardTarget) {
	c, err := s.st.GetCard(key)
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	c.Labels = nonNil(c.Labels)
	out := struct {
		domain.Card
		ProjectLabel string `json:"project_label,omitempty"`
		LinkedRepo   string `json:"linked_repo,omitempty"`
		LinkedGroup  string `json:"linked_group,omitempty"`
		LinkedDir    string `json:"linked_dir,omitempty"`
		LinkRefused  string `json:"link_refused,omitempty"`
	}{Card: c, LinkedRepo: t.linkedRepo, LinkedGroup: t.linkedGroup, LinkedDir: t.linkedDir, LinkRefused: t.linkRefused}
	if t.other {
		if out.ProjectLabel, err = s.projectLabel(c.ProjectID); err != nil {
			writeStoreError(w, err, "")
			return
		}
	}
	writeJSON(w, http.StatusCreated, out)
}

// adoptForCard liga a sessão sem projeto ao projeto do card que ela acabou de
// criar, com a captura completa (AdoptSession), como o cadastro faz com as
// sessões da pasta. Projeto com captura desligada não adota (como no hook) e
// sessão que já tem projeto fica onde está. Falha só vai para o log.
func (s *server) adoptForCard(sess domain.Session, projectID int64) {
	if sess.ProjectID != nil {
		return
	}
	p, err := s.projectByID(projectID)
	if err != nil || p == nil || !p.Capture {
		return
	}
	if err := s.st.AdoptSession(sess.ID, projectID); err != nil {
		log.Printf("adotar a sessão %s: %v", sess.ID, err)
	}
}

// canBeGroup diz se dir (que não é git) pode virar grupo com pasta pelo
// nobre project add: fica dentro da home (sem ser ela) e a varredura do
// cadastro acha repositórios dentro dela (as travas do fsScan).
func (s *server) canBeGroup(dir string) bool {
	if !filepath.IsAbs(dir) {
		return false
	}
	home, err := s.homeDir()
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil || real == home || !within(real, home) {
		return false
	}
	found, err := workspaces.ScanRepos(real)
	return err == nil && len(found) > 0
}

// commandBranch é o branch de trabalho do card que o comando cria. Quando a
// pasta do comando definiu o projeto e não é a da sessão, o branch é o de lá
// (vazio se o git não leu: o da sessão seria de outro repositório) e a
// localização da sessão não muda. Senão, o branch atual da sessão. A sessão
// relê o próprio branch nos dois casos: senão o PostToolUse deste mesmo
// comando veria a diferença e levaria o branch dela para o card.
func (s *server) commandBranch(sess domain.Session, from string) string {
	live := s.liveBranch(sess).Branch
	if from == "" || from == sess.Cwd {
		return live
	}
	if s.deps.Git == nil {
		return ""
	}
	return s.deps.Git.Branch(from)
}

// noProjectMsg é msgNoProject citando, antes do ponto final, as instruções
// do nobre que o Cockpit instala no agente da sessão (agentcore.Guide), se o
// driver dele as tem.
func (s *server) noProjectMsg(sess domain.Session) string {
	ad, ok := s.deps.Agents.Get(sess.Agent)
	if !ok {
		return msgNoProject
	}
	g, ok := ad.(agentcore.Guide)
	if !ok || g.GuideName() == "" {
		return msgNoProject
	}
	return strings.TrimSuffix(msgNoProject, ".") + " (veja " + g.GuideName() + ")."
}

func (s *server) meStart(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Title    string `json:"title"`
		Goal     string `json:"goal"`
		DoneWhen string `json:"done_when"`
		// Cwd é a pasta onde o nobre start rodou (ausente na CLI antiga).
		Cwd string `json:"cwd"`
		hierarchyInput
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		writeError(w, http.StatusBadRequest, "título obrigatório")
		return
	}
	target, ok := s.commandProject(w, sess, in.Cwd)
	if !ok {
		return
	}
	nc := domain.Card{Title: in.Title, Goal: in.Goal, DoneWhen: in.DoneWhen}
	if code, msg := s.applyHierarchy(&nc, in.hierarchyInput, true); code != 0 {
		writeError(w, code, msg)
		return
	}
	// Pedido validado: só agora o repositório da pasta entra no grupo.
	if !s.linkRepo(w, &target) {
		return
	}
	projectID := target.id
	nc.ProjectID = projectID
	// o branch vem do syncBranch: a base nunca vira branch do card
	c, err := s.st.CreateCardAndAttach(nc, sess.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.adoptForCard(sess, projectID)
	s.syncBranch(c.ID, s.commandBranch(sess, target.from))
	s.publish("cards", "sessions")
	s.writeCreated(w, c.Key, target)
}

// meCards cria um card sem ligar a sessão a ele (o agente cadastra o
// histórico do repositório sem sair do card atual), no projeto da pasta do
// comando ou, na falta, no da sessão (ver commandProject).
func (s *server) meCards(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Title        string `json:"title"`
		Goal         string `json:"goal"`
		DoneWhen     string `json:"done_when"`
		Branch       string `json:"branch"`
		WhereStopped string `json:"where_stopped"`
		Column       string `json:"column"`
		// Cwd é a pasta onde o nobre card add rodou (ausente na CLI antiga).
		Cwd string `json:"cwd"`
		hierarchyInput
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		writeError(w, http.StatusBadRequest, "título obrigatório")
		return
	}
	if in.wantsEpic() && in.Column == string(domain.Pausado) {
		writeError(w, http.StatusBadRequest, "um épico não vai para Pausado; crie em backlog")
		return
	}
	col := domain.Column(in.Column)
	switch col {
	case "":
		col = domain.Backlog
	case domain.Backlog, domain.Pausado:
	default:
		writeError(w, http.StatusBadRequest, "coluna inválida (use backlog ou pausado)")
		return
	}
	target, ok := s.commandProject(w, sess, in.Cwd)
	if !ok {
		return
	}
	// A hierarquia é validada num rascunho antes de qualquer gravação: só com
	// o pedido aceito o repositório da pasta entra no grupo.
	card := domain.Card{Title: in.Title, Goal: in.Goal, DoneWhen: in.DoneWhen, Column: col}
	if code, msg := s.applyHierarchy(&card, in.hierarchyInput, true); code != 0 {
		writeError(w, code, msg)
		return
	}
	if !s.linkRepo(w, &target) {
		return
	}
	projectID := target.id
	branch := strings.TrimSpace(in.Branch)
	if card.Type == domain.TypeEpico {
		branch = "" // o épico nunca guarda branch
	}
	if gp, err := s.projectByID(projectID); err != nil {
		writeStoreError(w, err, "")
		return
	} else if gp != nil && gp.IsGroup() {
		branch = "" // card de grupo nunca guarda branch
	}
	if branch != "" {
		base, err := s.projectBase(projectID)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if branch == base { // o card nunca guarda a base
			branch = ""
		}
	}
	card.Branch, card.ProjectID = branch, projectID
	if w := strings.TrimSpace(in.WhereStopped); w != "" {
		now := s.now()
		card.WhereStopped, card.WhereStoppedAt = w, &now
	}
	c, err := s.st.CreateCard(card)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.publish("cards")
	s.writeCreated(w, c.Key, target)
}

// meCardSet muda tipo, pai, prioridade, labels e points de um card: o da
// sessão ou, com key, qualquer um. Valida tudo antes de gravar.
func (s *server) meCardSet(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Key string `json:"key"`
		hierarchyInput
	}
	if !readJSON(w, r, &in) {
		return
	}
	var c domain.Card
	if key := normKey(in.Key); key != "" {
		var err error
		if c, err = s.st.GetCard(key); err != nil {
			writeStoreError(w, err, "card "+key+" não encontrado")
			return
		}
	} else {
		var ok bool
		if c, ok = s.sessionCard(w, sess); !ok {
			return
		}
	}
	oldType, oldBranch := c.Type, c.Branch
	if code, msg := s.applyHierarchy(&c, in.hierarchyInput, false); code != 0 {
		writeError(w, code, msg)
		return
	}
	// Épico que deixa de ser épico não pode ficar num grupo sem pasta.
	if oldType == domain.TypeEpico && c.Type != domain.TypeEpico {
		p, err := s.projectByID(c.ProjectID)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if p != nil && p.IsGroup() && p.Path == "" {
			writeError(w, http.StatusBadRequest, msgGroupNoCards)
			return
		}
	}
	// Daqui em diante só gravações.
	if oldBranch != "" && c.Branch == "" {
		if _, err := s.st.SetCardBranch(c.ID, ""); err != nil {
			writeStoreError(w, err, "")
			return
		}
	}
	if err := s.st.UpdateCard(c); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.publish("cards")
	got, err := s.st.GetCardByID(c.ID)
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	got.Labels = nonNil(got.Labels)
	out := struct {
		domain.Card
		ParentKey string `json:"parent_key"`
	}{Card: got}
	if got.ParentID != nil {
		if p, err := s.st.GetCardByID(*got.ParentID); err == nil {
			out.ParentKey = p.Key
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) meResume(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	var in struct {
		Key   string `json:"key"`
		Force bool   `json:"force"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Key == "" {
		writeError(w, http.StatusBadRequest, "chave do card obrigatória")
		return
	}
	if err := s.st.AttachSession(sess.ID, in.Key, in.Force); err != nil {
		if errors.Is(err, store.ErrCardBusy) {
			writeError(w, http.StatusConflict, busyMessage(in.Key))
			return
		}
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.publish("cards", "sessions")
	c, err := s.st.GetCard(in.Key)
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	// O card passa a acompanhar o branch de trabalho desta sessão.
	if s.syncBranch(c.ID, s.liveBranch(sess).Branch) {
		if c, err = s.st.GetCard(in.Key); err != nil {
			writeStoreError(w, err, msgCardNotFound)
			return
		}
	}
	// A base do diff é a sessão anterior: a desta chamada acabou de entrar.
	md, err := resume.ForCard(s.st, s.deps.Git, c, sess.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": c.Key, "markdown": md})
}

// branchReader é o Git do daemon como BranchReader (nil se não souber ler branches).
func (s *server) branchReader() gitinfo.BranchReader {
	br, _ := s.deps.Git.(gitinfo.BranchReader)
	return br
}

// syncBranch faz o branch do card acompanhar branch (ver board.SyncCardBranch).
// Falha só vai para o log; devolve se o branch do card mudou.
func (s *server) syncBranch(cardID int64, branch string) bool {
	changed, err := board.SyncCardBranch(s.st, s.branchReader(), cardID, branch)
	if err != nil {
		log.Printf("branch do card %d: %v", cardID, err)
		return false
	}
	if changed {
		s.publish("cards")
	}
	return changed
}

// projectBase devolve a base do projeto id (ver board.RepoBase; "" se o
// projeto não existe ou não tem base).
func (s *server) projectBase(id int64) (string, error) {
	ps, err := s.st.ListProjects()
	if err != nil {
		return "", err
	}
	for _, p := range ps {
		if p.ID == id {
			return board.RepoBase(s.st, s.branchReader(), p)
		}
	}
	return "", nil
}

// liveBranch relê o branch atual do cwd da sessão (o agente pode ter trocado de
// branch sem mudar de diretório, com git switch) e grava na sessão se mudou.
// Falha ao gravar só vai para o log: o branch lido vale mesmo assim.
func (s *server) liveBranch(sess domain.Session) domain.Session {
	if s.deps.Git == nil || sess.Cwd == "" {
		return sess
	}
	b := s.deps.Git.Branch(sess.Cwd)
	if b == "" || b == sess.Branch {
		return sess
	}
	l := store.SessionLocation{Cwd: sess.Cwd, Branch: b, GitHead: s.deps.Git.Head(sess.Cwd),
		Worktree: sess.Worktree, WorktreeLinked: sess.WorktreeLinked}
	sess.Branch, sess.GitHead = l.Branch, l.GitHead
	if err := s.st.UpdateSessionLocation(sess.ID, l); err != nil {
		log.Printf("branch da sessão %s: %v", sess.ID, err)
		return sess
	}
	s.publish("sessions")
	return sess
}

// meBranch troca o branch do card (o agente informa o branch de trabalho). A
// base do projeto é recusada: o card guarda o branch de trabalho.
func (s *server) meBranch(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Branch string `json:"branch"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if c.Type == domain.TypeEpico {
		writeError(w, http.StatusConflict, "o épico não guarda branch")
		return
	}
	b := strings.TrimSpace(in.Branch)
	if b == "" {
		writeError(w, http.StatusBadRequest, "branch obrigatório")
		return
	}
	if cur, err := s.projectByID(c.ProjectID); err != nil {
		writeStoreError(w, err, "")
		return
	} else if cur != nil && cur.IsGroup() {
		writeError(w, http.StatusBadRequest, msgGroupNoBranch)
		return
	}
	// A sessão fica com o branch atual: senão o PostToolUse deste mesmo comando
	// veria a diferença e trocaria o branch que o agente acabou de informar.
	s.liveBranch(sess)
	base, err := s.projectBase(c.ProjectID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if base == b {
		writeError(w, http.StatusConflict, fmt.Sprintf("%q é a base do projeto; o card fica com o branch de trabalho", b))
		return
	}
	if _, err := board.SyncCardBranch(s.st, s.branchReader(), c.ID, b); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

// meApp move o card da sessão para um app (sub-projeto) do monorepo, ou de
// volta ao projeto principal com geral.
func (s *server) meApp(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		App   string `json:"app"`
		Geral bool   `json:"geral"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	cur, err := s.projectByID(c.ProjectID)
	if err != nil || cur == nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	repo, err := s.st.RepoProject(*cur)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	kids, err := s.st.Children(repo.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if len(kids) == 0 {
		writeError(w, http.StatusBadRequest, "este projeto não tem sub-projetos")
		return
	}
	name := strings.TrimSpace(in.App)
	target := repo.ID
	switch {
	case in.Geral && name != "":
		writeError(w, http.StatusBadRequest, "informe o app ou geral, não os dois")
		return
	case in.Geral:
		if repo.IsGroup() && repo.Path == "" {
			writeError(w, http.StatusBadRequest, msgGroupNoCards)
			return
		}
	case name == "":
		writeError(w, http.StatusBadRequest, "informe o app ou geral")
		return
	default:
		target = 0
		for _, k := range kids {
			if strings.EqualFold(k.Name, name) {
				target = k.ID
			}
		}
		if target == 0 {
			names := make([]string, len(kids))
			for i, k := range kids {
				names[i] = k.Name
			}
			sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
			writeError(w, http.StatusBadRequest, fmt.Sprintf("app desconhecido: %s. Apps deste projeto: %s", name, strings.Join(names, ", ")))
			return
		}
	}
	if err := s.st.SetCardProject(c.ID, target); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

func (s *server) meStatus(w http.ResponseWriter, r *http.Request, sess domain.Session) {
	out := struct {
		Session domain.Session `json:"session"`
		Card    *CardDetail    `json:"card"`
	}{Session: sess}
	if sess.CardID != nil {
		c, ok := s.sessionCard(w, sess)
		if !ok {
			return
		}
		d, err := s.cardDetail(c)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		out.Card = &d
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) meNote(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "texto obrigatório")
		return
	}
	if err := s.st.SetWhereStopped(c.ID, in.Text, s.now()); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.recordEvent(sess.ID, domain.EvNote, map[string]any{"text": in.Text})
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

func (s *server) meTodoDone(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		ID int64 `json:"id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	found := false
	for _, t := range ct.Todos {
		found = found || t.ID == in.ID
	}
	if !found {
		writeError(w, http.StatusNotFound, "pendência não encontrada neste card")
		return
	}
	if err := s.st.SetTodoDone(in.ID, true); err != nil {
		writeStoreError(w, err, "pendência não encontrada")
		return
	}
	s.recordEvent(sess.ID, domain.EvTodo, map[string]any{"id": in.ID, "done": true})
	s.publish("cards")
	writeJSON(w, http.StatusOK, map[string]any{"id": in.ID, "done": true})
}

func (s *server) meAnswer(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		ID     int64  `json:"id"`
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Answer) == "" {
		writeError(w, http.StatusBadRequest, "resposta obrigatória")
		return
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	found := false
	for _, q := range ct.Questions {
		found = found || q.ID == in.ID
	}
	if !found {
		writeError(w, http.StatusNotFound, "pergunta não encontrada neste card")
		return
	}
	if err := s.st.AnswerQuestion(in.ID, in.Answer); err != nil {
		writeStoreError(w, err, "pergunta não encontrada")
		return
	}
	s.recordEvent(sess.ID, domain.EvAnswer, map[string]any{"id": in.ID, "answer": in.Answer})
	s.publish("cards")
	writeJSON(w, http.StatusOK, map[string]any{"id": in.ID, "answer": in.Answer})
}

func (s *server) meTest(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, "texto obrigatório")
		return
	}
	c.HowToTest = in.Text
	if err := s.st.UpdateCard(c); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	s.recordEvent(sess.ID, domain.EvHowToTest, map[string]any{"text": in.Text})
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

// doneProblems lista o que impede o done: "Como testar" vazio ou anterior à
// última edição de arquivo, e perguntas sem resposta.
func (s *server) doneProblems(c domain.Card) ([]string, error) {
	var problems []string
	if strings.TrimSpace(c.HowToTest) == "" {
		problems = append(problems, `"Como testar" vazio: registre com nobre test`)
	} else {
		edit, err := s.st.LastEventAt(c.ID, domain.EvFileEdit)
		if err != nil {
			return nil, err
		}
		test, err := s.st.LastEventAt(c.ID, domain.EvHowToTest)
		if err != nil {
			return nil, err
		}
		if edit != nil && test != nil && edit.After(*test) {
			problems = append(problems, `"Como testar" é anterior à última edição de arquivo: atualize com nobre test`)
		}
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		return nil, err
	}
	open := 0
	for _, q := range ct.Questions {
		if strings.TrimSpace(q.Answer) == "" {
			open++
		}
	}
	if open > 0 {
		problems = append(problems, fmt.Sprintf("%d pergunta(s) sem resposta: responda com nobre answer <id>", open))
	}
	return problems, nil
}

// columnLabels são os nomes das colunas como aparecem no board.
var columnLabels = map[domain.Column]string{
	domain.Backlog:     "Backlog",
	domain.EmAndamento: "Em andamento",
	domain.Pausado:     "Pausado",
	domain.Revisao:     "Revisão",
	domain.Concluido:   "Concluído",
}

// meDone aplica a transição AgentDone (em_andamento → revisao) e registra o
// evento done. O card pausado por limite de custo cuja sessão viva é a que
// chama conta como em andamento (o limite pausou o card com o agente ainda
// trabalhando) e vai para revisão sem o motivo da pausa. Em qualquer outra
// coluna o done é recusado com 409, sem registrar o evento.
func (s *server) meDone(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Force bool `json:"force"`
	}
	if !readJSON(w, r, &in) { // corpo vazio é aceito (force=false)
		return
	}
	if c.Type == domain.TypeEpico {
		writeError(w, http.StatusConflict, c.Key+" é um épico: a coluna do épico segue as filhas (nada mudou)")
		return
	}
	limitPaused := c.Column == domain.Pausado && c.PauseReason == domain.PauseReasonLimit &&
		sess.EndedAt == nil && sess.PIDDeadAt == nil && sess.CardID != nil && *sess.CardID == c.ID
	next := domain.NextColumn(c.Column, domain.AgentDone)
	if limitPaused {
		next = domain.Revisao
	}
	if next == c.Column {
		writeError(w, http.StatusConflict, "o card está em "+columnLabels[c.Column]+"; nobre done só move cards em andamento")
		return
	}
	if !in.Force {
		problems, err := s.doneProblems(c)
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		if len(problems) > 0 {
			writeError(w, http.StatusConflict, strings.Join(problems, "; ")+" (ou nobre done --force)")
			return
		}
	}
	if limitPaused {
		changed, err := s.st.DoneFromLimit(c.ID, s.now())
		if err != nil {
			writeStoreError(w, err, msgCardNotFound)
			return
		}
		if !changed { // o card mudou entre a leitura e a gravação
			writeError(w, http.StatusConflict, "o card não está mais pausado por limite; nobre done só move cards em andamento")
			return
		}
	} else if err := s.st.MoveCard(c.Key, next); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	// Guarda: AgentDone leva a revisao, nunca a concluido; se a regra mudar, o hook já vale.
	if next == domain.Concluido && c.Column != domain.Concluido {
		s.cardMemoryCandidates(c)
	}
	s.publish("cards")
	s.recordEvent(sess.ID, domain.EvDone, map[string]any{})
	s.writeCard(w, http.StatusOK, c.Key)
}

// meBlock marca (ou, com key vazia, limpa) o card que bloqueia o card da sessão.
func (s *server) meBlock(w http.ResponseWriter, r *http.Request, sess domain.Session, c domain.Card) {
	var in struct {
		Key string `json:"key"`
		Why string `json:"why"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	key := normKey(in.Key)
	if key != "" {
		if key == normKey(c.Key) {
			writeError(w, http.StatusBadRequest, "um card não pode bloquear a si mesmo")
			return
		}
		b, err := s.st.GetCard(key)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "card "+key+" não encontrado")
			return
		}
		if err != nil {
			writeStoreError(w, err, "")
			return
		}
		key = b.Key
	}
	c.BlockedBy = key
	if err := s.st.UpdateCard(c); err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	if key != "" {
		if _, err := s.st.AddDecision(domain.Decision{CardID: c.ID, What: "Bloqueado por " + key, Why: in.Why, SessionID: sess.ID, At: s.now()}); err != nil {
			writeStoreError(w, err, "")
			return
		}
		s.recordEvent(sess.ID, domain.EvDecision, map[string]any{"what": "Bloqueado por " + key, "why": in.Why})
	}
	s.publish("cards")
	s.writeCard(w, http.StatusOK, c.Key)
}

// setPlan liga o plano ao card e registra as tarefas, em ordem.
func (s *server) setPlan(w http.ResponseWriter, r *http.Request, c domain.Card, sessionID string) {
	var in struct {
		Path  string   `json:"path"`
		Tasks []string `json:"tasks"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Tasks) == 0 {
		writeError(w, http.StatusBadRequest, "informe ao menos uma tarefa")
		return
	}
	in.Path = strings.TrimSpace(in.Path)
	if in.Path == "" {
		writeError(w, http.StatusBadRequest, "informe o arquivo do plano")
		return
	}
	p, err := s.st.SetPlan(c.ID, in.Path, in.Tasks, sessionID, s.now())
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.recordEvent(sessionID, domain.EvPlan, map[string]any{"path": p.Path, "tasks": len(p.Tasks)})
	s.publish("cards")
	writeJSON(w, http.StatusOK, p)
}

// setTask muda o estado de uma tarefa do plano do card.
func (s *server) setTask(w http.ResponseWriter, r *http.Request, c domain.Card, sessionID string) {
	var in struct {
		N      int    `json:"n"`
		Status string `json:"status"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	st, ok := domain.ParseTaskStatus(in.Status)
	if !ok {
		writeError(w, http.StatusBadRequest, "estado inválido: use a-fazer, andamento, revisao ou concluida")
		return
	}
	ct, err := s.st.CardContent(c.ID)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if ct.Plan == nil {
		writeError(w, http.StatusNotFound, "o card não tem plano: registre com nobre plan")
		return
	}
	t, err := s.st.SetTaskStatus(c.ID, in.N, st, sessionID, s.now())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("tarefa %d não existe no plano do card", in.N))
		return
	}
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	s.recordEvent(sessionID, domain.EvTask, map[string]any{"n": t.N, "status": t.Status})
	s.publish("cards")
	writeJSON(w, http.StatusOK, t)
}
