package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

// registeredPhrase abre o contexto do SessionStart quando a sessão é
// registrada no board; a skill global só usa o nobre quando a vê.
const registeredPhrase = "NobreAI Cockpit: esta sessão é registrada no board"

// unregisteredPhrase abre o aviso do SessionStart num repositório git que
// ainda não está no board.
const unregisteredPhrase = "NobreAI Cockpit: este repositório não está cadastrado no board"

// createGuidance é o critério de criação de card (spec 6.0) quando o agente
// cria cards por conta própria.
const createGuidance = `Crie um card com nobre start "<título>" quando começar algo que precisa de histórico: tarefa de várias etapas, mudanças em vários arquivos ou trabalho que outro agente pode continuar. Se o usuário pedir um card, crie; se citar uma chave NAC-, use nobre resume <KEY>. Perguntas rápidas não viram card.`

// askOnlyGuidance é o critério com AgentCreatesCards desligado.
const askOnlyGuidance = `Só crie card quando o usuário pedir ("crie um card para isso"); se ele citar uma chave NAC-, use nobre resume <KEY>.`

// commandsGuidance são os comandos nobre para uma sessão com card.
const commandsGuidance = `Com card: nobre note "<onde parei>" a cada passo relevante; nobre decide "<decisão>" --why "<porquê>"; nobre file <caminho> --why "<motivo>"; nobre todo add "<texto>"; nobre ask "<pergunta>" quando precisar de uma decisão do usuário; nobre answer <id> "<resposta>" quando uma pergunta for decidida; nobre block <KEY> quando o card só puder continuar depois de outro, antes de parar; nobre branch <nome> ao criar ou entrar no branch do card; nobre test "<como testar>"; nobre done ao terminar; nobre status mostra o card.`

// appGuidance ensina o nobre app em projetos com sub-projetos.
const appGuidance = "Quando o card for de um app só, rode nobre app <nome> ao criar o card ou ao começar a mexer nele; nobre app --geral devolve ao projeto principal."

// guidanceFor são as instruções sem a frase de sessão registrada (o aviso de
// repositório não cadastrado termina nelas): o critério de criação conforme a
// opção e os comandos.
func guidanceFor(set domain.Settings) string {
	create := createGuidance
	if !set.AgentCreatesCards {
		create = askOnlyGuidance
	}
	return create + "\n" + commandsGuidance
}

// scanCardsStep é o que fazer com os cards do levantamento no aviso de
// repositório não cadastrado: criar, ou só sugerir com AgentCreatesCards desligado.
func scanCardsStep(set domain.Settings) string {
	if !set.AgentCreatesCards {
		return "liste ao usuário os cards sugeridos (não crie sem ele pedir)"
	}
	return "crie os cards importantes com nobre card add"
}

// guidance é o texto com os padrões.
var guidance = guidanceFor(domain.DefaultSettings())

// instructionsFor é o começo do contexto injetado no SessionStart.
func instructionsFor(set domain.Settings) string { return registeredPhrase + ".\n" + guidanceFor(set) }

// instructions é o contexto com os padrões.
var instructions = instructionsFor(domain.DefaultSettings())

// resumeEnv é a variável de ambiente com que a UI abre o agente para
// continuar um card.
const resumeEnv = "NOBRE_RESUME"

// sessionStart registra o início e injeta o contexto do Cockpit. Com
// NOBRE_RESUME=<KEY> num início de sessão (source startup), a sessão é ligada
// ao card (force: o agente foi aberto pelo humano para continuar o card) e o
// pacote de retomada vai junto, mesmo sem captura completa: é um pedido
// explícito. Sem captura e sem retomada, não responde nada.
func (a *Adapter) sessionStart(sess domain.Session, p payload, env map[string]string, now time.Time) (adapter.HookReply, error) {
	a.event(sess.ID, domain.EvSessionStart, map[string]any{"source": p.Source}, now)
	set, err := a.st.GetSettings()
	if err != nil {
		return adapter.HookReply{}, err
	}
	// O que esta sessão passa a ter visto: o contexto de agora. Substitui o
	// registro de antes (resume, clear e compact reinjetam a configuração).
	_, _ = a.st.SwapSeenConfig(sess.ID, sess.SeenConfig, domain.SeenConfig{AgentCreatesCards: set.AgentCreatesCards, Capture: sess.FullCapture})

	var resumed *domain.Card
	var pkg, notFound string
	if key := strings.TrimSpace(env[resumeEnv]); key != "" && (p.Source == "" || p.Source == "startup") {
		switch err := a.st.AttachSession(sess.ID, key, true); {
		case errors.Is(err, store.ErrNotFound):
			notFound = key
		case err != nil:
			return adapter.HookReply{}, err
		default:
			a.publish("cards", "sessions")
			c, err := a.st.GetCard(key)
			if err != nil {
				return adapter.HookReply{}, err
			}
			// O card passa a acompanhar o branch de trabalho desta sessão
			// antes de o pacote de retomada ser montado.
			sess.CardID = &c.ID
			a.syncCardBranch(sess)
			if c, err = a.st.GetCard(key); err != nil {
				return adapter.HookReply{}, err
			}
			if pkg, err = a.resumePackage(c, sess.ID); err != nil {
				return adapter.HookReply{}, err
			}
			resumed = &c
		}
	}
	// Compactação numa sessão com card: o pacote de retomada some do
	// contexto junto com o resto e é reinjetado.
	compacted := p.Source == "compact" && resumed == nil && sess.CardID != nil
	if !sess.FullCapture && resumed == nil && notFound == "" && !compacted {
		if sess.ProjectID != nil {
			return adapter.HookReply{}, nil
		}
		root, folder := a.unregisteredRoot(sess.Cwd)
		if root == "" {
			return adapter.HookReply{}, nil
		}
		phrase := unregisteredPhrase
		todo := "Antes de seguir com o pedido do usuário, cadastre com nobre project add " + root +
			" e faça o levantamento com nobre project scan; " + scanCardsStep(set) + " (veja a skill nobre-cockpit); se o cadastro listar sub-projetos sugeridos, liste-os ao usuário sem cadastrar. O cadastro liga esta sessão ao board."
		if folder {
			// Pasta de repositórios: vira grupo com pasta, que não tem
			// levantamento, e só com o aval do usuário.
			phrase = unregisteredFolderPhrase
			todo = "Pergunte ao usuário se quer cadastrar esta pasta como grupo com nobre project add " + root +
				"; só cadastre se ele aceitar. O comando lista os sub-projetos sugeridos, que você mostra ao usuário sem cadastrar. O cadastro liga esta sessão ao board."
		}
		return jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": phrase + " (" + root + ").\n" + todo + "\n\n" + guidanceFor(set),
		}})
	}

	var b strings.Builder
	b.WriteString(instructionsFor(set))
	if notFound != "" {
		fmt.Fprintf(&b, "\n\nO card %s não foi encontrado; a sessão segue sem card.", notFound)
	}
	if sess.FullCapture {
		apps, err := a.appsGuidance(sess)
		if err != nil {
			return adapter.HookReply{}, err
		}
		if apps != "" {
			b.WriteString("\n\n" + apps)
		}
	}
	if resumed == nil && sess.CardID != nil {
		c, err := a.st.GetCardByID(*sess.CardID)
		if err != nil {
			return adapter.HookReply{}, err
		}
		fmt.Fprintf(&b, "\n\nEsta sessão está no card %s · %s.", c.Key, c.Title)
		if compacted {
			pkg, err := a.resumePackage(c, sess.ID)
			if err != nil {
				return adapter.HookReply{}, err
			}
			fmt.Fprintf(&b, " O contexto foi compactado; continue a partir do pacote de retomada do card.\n\n%s", pkg)
		}
	}
	if sess.FullCapture && set.InjectPausedCards {
		paused, err := a.pausedCards(sess, resumed)
		if err != nil {
			return adapter.HookReply{}, err
		}
		if len(paused) > 0 {
			b.WriteString("\n\nCards pausados neste repositório e branch (retome com nobre resume <KEY> em vez de criar outro):")
			for _, c := range paused {
				where := strings.TrimSpace(c.WhereStopped)
				if where == "" {
					where = "(onde parei não registrado)"
				}
				fmt.Fprintf(&b, "\n- %s · %s · %s%s", c.Key, c.Title, oneLine(where), a.pauseReason(c))
			}
		}
	}
	if sess.FullCapture {
		b.WriteString(a.unregisteredInGroup(sess))
	}
	if resumed != nil {
		fmt.Fprintf(&b, "\n\nEsta sessão foi ligada ao card %s. Continue a partir do pacote de retomada; não pergunte o que já está nele.\n\n%s", resumed.Key, pkg)
	}
	return jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "SessionStart",
		"additionalContext": b.String(),
	}})
}

// unregisteredFolderPhrase abre o aviso numa pasta que não é git mas tem
// repositórios no primeiro nível.
const unregisteredFolderPhrase = "NobreAI Cockpit: esta pasta não está cadastrada no board"

// unregisteredRoot devolve a raiz a cadastrar quando vale avisar, e se é uma
// pasta de repositórios (true) em vez de um repositório git; "" quando não há o
// que avisar: a raiz é o HOME (ou o contém), já existe projeto para ela
// (mesmo com captura desligada, que funciona como "ignorar") ou, numa pasta de
// repositórios, ela já contém um projeto ou grupo cadastrado (como
// ~/workspace). Numa pasta que não é git só lê o primeiro nível do próprio
// cwd, sem git.
func (a *Adapter) unregisteredRoot(cwd string) (string, bool) {
	if cwd == "" {
		return "", false
	}
	root := ""
	if wr, ok := a.deps.Git.(gitinfo.WorktreeReader); ok {
		root = wr.Locate(cwd).Main
	}
	folder := root == ""
	if folder {
		root = filepath.Clean(cwd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		home = filepath.Clean(home)
		if home == filepath.Clean(root) || (folder && strings.HasPrefix(home, strings.TrimRight(root, "/")+"/")) {
			return "", false
		}
	}
	if p, err := a.st.ProjectForPath(root); err != nil || p != nil {
		return "", false
	}
	if folder && a.containsProject(root) {
		return "", false
	}
	if folder && !hasRepoChild(root) {
		return "", false
	}
	return root, folder
}

// containsProject diz se algum projeto ou grupo cadastrado fica dentro de dir
// (erro de leitura conta como sim: na dúvida, não avisa).
func (a *Adapter) containsProject(dir string) bool {
	ps, err := a.st.ListProjects()
	if err != nil {
		return true
	}
	prefix := strings.TrimRight(dir, "/") + "/"
	for _, p := range ps {
		if strings.HasPrefix(filepath.Clean(p.Path), prefix) {
			return true
		}
	}
	return false
}

// hasRepoChild diz se alguma pasta do primeiro nível de dir é um repositório
// (.git como pasta ou arquivo); não segue symlinks nem lê pastas ocultas.
func hasRepoChild(dir string) bool {
	es, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range es {
		n := e.Name()
		if !e.IsDir() || workspaces.SkipDir(n) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, n, ".git")); err == nil {
			return true
		}
	}
	return false
}

// unregisteredInGroup é o aviso (com o "\n\n" na frente) de que a sessão está
// num repositório ainda não cadastrado dentro de um grupo com pasta, ou ""
// quando não é o caso ou o usuário já recusou a sugestão. Usa só o que a sessão
// guardou: nenhuma chamada ao git.
func (a *Adapter) unregisteredInGroup(sess domain.Session) string {
	if sess.ProjectID == nil || sess.Worktree == "" || sess.WorktreeLinked {
		return ""
	}
	root := filepath.Clean(sess.Worktree)
	g, err := a.st.ProjectForPath(root)
	if err != nil || g == nil || !g.IsGroup() || g.ID != *sess.ProjectID || filepath.Clean(g.Path) == root {
		return ""
	}
	// Mesma regra da varredura: o que a tela nunca sugere (pasta oculta, de
	// dependências/build ou abaixo de 2 níveis) também não vira aviso.
	rel, err := filepath.Rel(filepath.Clean(g.Path), root)
	if err != nil || !workspaces.InScan(rel) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	dismissed := map[string][]string{}
	if _, err := a.st.GetKV("subproject_dismissed", &dismissed); err != nil {
		dismissed = map[string][]string{}
	}
	for _, d := range dismissed[strconv.FormatInt(g.ID, 10)] {
		if d == rel {
			return ""
		}
	}
	return fmt.Sprintf("\n\nNobreAI Cockpit: o repositório %s fica dentro do grupo %s e não está cadastrado. Liste ao usuário a opção de cadastrá-lo com nobre project add %s --pai %d; não cadastre sem ele pedir.",
		root, g.Name, root, g.ID)
}

// appsGuidance lista os apps (sub-projetos) da família do projeto da sessão e
// ensina o nobre app; "" quando o projeto não tem sub-projetos ou a família é
// um grupo (apps e nobre app são só de monorepo).
func (a *Adapter) appsGuidance(sess domain.Session) (string, error) {
	if sess.ProjectID == nil {
		return "", nil
	}
	ps, err := a.st.ListProjects()
	if err != nil {
		return "", err
	}
	for _, p := range ps {
		if p.ID != *sess.ProjectID {
			continue
		}
		repo, err := a.st.RepoProject(p)
		if err != nil || repo.IsGroup() {
			return "", err
		}
		kids, err := a.st.Children(repo.ID)
		if err != nil || len(kids) == 0 {
			return "", err
		}
		items := make([]string, len(kids))
		for i, k := range kids {
			rel := k.Path
			if repo.Path != "" {
				rel = strings.TrimPrefix(rel, strings.TrimRight(repo.Path, "/")+"/")
			}
			items[i] = fmt.Sprintf("%s (%s)", k.Name, rel)
		}
		return "Apps deste projeto: " + strings.Join(items, ", ") + ".\n" + appGuidance, nil
	}
	return "", nil
}

// pausedCards lista os cards pausados do projeto da sessão (exceto o que
// acabou de ser retomado e os que ainda têm sessão viva dona). Num branch de trabalho, só os desse branch e os sem
// branch; na base (ou sem branch), todos: o card nunca guarda a base.
func (a *Adapter) pausedCards(sess domain.Session, except *domain.Card) ([]domain.Card, error) {
	if sess.ProjectID == nil {
		return nil, nil
	}
	ps, err := a.st.ListProjects()
	if err != nil {
		return nil, err
	}
	onBase := sess.Branch == ""
	br, _ := a.deps.Git.(gitinfo.BranchReader) // nil: só a base configurada conta
	// Num monorepo a base e os cards são os da família do pai.
	family := []int64{*sess.ProjectID}
	for _, p := range ps {
		if p.ID != *sess.ProjectID {
			continue
		}
		repo, err := a.st.RepoProject(p)
		if err != nil {
			return nil, err
		}
		// Grupo não tem base: o git nunca roda na pasta dele.
		if !onBase && !repo.IsGroup() {
			onBase = board.ProjectBase(br, repo) == sess.Branch
		}
		if family, err = a.st.FamilyIDs(repo.ID); err != nil {
			return nil, err
		}
	}
	cards, err := a.st.ListCards(store.CardFilter{ProjectIDs: family})
	if err != nil {
		return nil, err
	}
	var out []domain.Card
	for _, c := range cards {
		if c.Column != domain.Pausado || (except != nil && c.ID == except.ID) {
			continue
		}
		if !onBase && c.Branch != sess.Branch && c.Branch != "" {
			continue
		}
		// Pausado por limite com a sessão dona ainda viva: não é para retomar.
		live, err := a.st.HasLiveSession(c.ID)
		if err != nil {
			return nil, err
		}
		if !live {
			out = append(out, c)
		}
	}
	return out, nil
}

// pauseReason é o motivo da pausa no fim da linha do card (" · ..."), ou "".
func (a *Adapter) pauseReason(c domain.Card) string {
	in := domain.SignalInput{Column: c.Column, Now: time.Now(), LastActivity: time.Now(), BlockedBy: c.BlockedBy}
	if c.BlockedBy != "" {
		if b, err := a.st.GetCard(c.BlockedBy); err == nil {
			in.BlockerExists, in.BlockerConcluded = true, b.Column == domain.Concluido
		}
	}
	if ct, err := a.st.CardContent(c.ID); err == nil {
		for _, q := range ct.Questions {
			if q.Answer == "" {
				in.OpenQuestions++
			}
		}
	}
	switch domain.CardStatusSignal(in) {
	case domain.SignalBloqueado:
		return " · bloqueado por " + c.BlockedBy
	case domain.SignalProntoParaRetomar:
		return " · pronto para retomar (" + c.BlockedBy + " concluído)"
	case domain.SignalAguardandoVoce:
		return fmt.Sprintf(" · aguardando você (%d pergunta(s))", in.OpenQuestions)
	}
	if c.PauseReason == domain.PauseReasonLimit {
		return " · pausado por " + domain.PauseReasonLimit
	}
	return ""
}

// oneLine junta as linhas do texto (o "onde parei" vai numa linha da lista).
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// resumePackage monta o pacote de retomada; a sessão atual, que acabou de
// entrar no card, não serve de base para o diff.
func (a *Adapter) resumePackage(c domain.Card, currentSession string) (string, error) {
	return resume.ForCard(a.st, a.deps.Git, c, currentSession)
}
