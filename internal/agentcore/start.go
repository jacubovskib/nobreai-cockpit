package agentcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

// RegisteredPhrase abre o contexto do início da sessão quando ela é
// registrada no board; as instruções que o Cockpit instala no agente só usam
// o nobre quando a veem.
const RegisteredPhrase = "NobreAI Cockpit: esta sessão é registrada no board"

// UnregisteredPhrase abre o aviso do início da sessão num repositório git que
// ainda não está no board.
const UnregisteredPhrase = "NobreAI Cockpit: este repositório não está cadastrado no board"

// UnregisteredFolderPhrase abre o aviso numa pasta que não é git mas tem
// repositórios no primeiro nível.
const UnregisteredFolderPhrase = "NobreAI Cockpit: esta pasta não está cadastrada no board"

// createGuidance é o critério de criação de card (spec 6.0) quando o agente
// cria cards por conta própria.
const createGuidance = `Crie um card com nobre start "<título>" quando começar algo que precisa de histórico: tarefa de várias etapas, mudanças em vários arquivos ou trabalho que outro agente pode continuar. Se o usuário pedir um card, crie; se citar uma chave NAC-, use nobre resume <KEY>. Perguntas rápidas não viram card.`

// AskOnlyGuidance é o critério com AgentCreatesCards desligado (as
// instruções instaladas no agente mandam obedecer a ele).
const AskOnlyGuidance = `Só crie card quando o usuário pedir ("crie um card para isso"); se ele citar uma chave NAC-, use nobre resume <KEY>.`

// commandsGuidance são os comandos nobre para uma sessão com card.
const commandsGuidance = `Com card: nobre note "<onde parei>" a cada passo relevante; nobre decide "<decisão>" --why "<porquê>"; nobre file <caminho> --why "<motivo>"; nobre todo add "<texto>"; nobre plan <arquivo> --task "<tarefa>" ... ao terminar um plano; nobre task <n> <estado> a cada tarefa; nobre ask "<pergunta>" quando precisar de uma decisão do usuário; nobre answer <id> "<resposta>" quando uma pergunta for decidida; nobre block <KEY> quando o card só puder continuar depois de outro, antes de parar; nobre branch <nome> ao criar ou entrar no branch do card; nobre test "<como testar>"; nobre done ao terminar; nobre status mostra o card.`

// appGuidance ensina o nobre app em projetos com sub-projetos.
const appGuidance = "Quando o card for de um app só, rode nobre app <nome> ao criar o card ou ao começar a mexer nele; nobre app --geral devolve ao projeto principal."

// Guidance são as instruções sem a frase de sessão registrada (o aviso de
// repositório não cadastrado termina nelas): o critério de criação conforme a
// opção e os comandos.
func Guidance(set domain.Settings) string {
	create := createGuidance
	if !set.AgentCreatesCards {
		create = AskOnlyGuidance
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

// Instructions é o começo do contexto injetado no início da sessão.
func Instructions(set domain.Settings) string { return RegisteredPhrase + ".\n" + Guidance(set) }

// resumeEnv é a variável de ambiente com que a UI abre o agente para
// continuar um card.
const resumeEnv = "NOBRE_RESUME"

// sessionStart monta o contexto do Cockpit para o início da sessão (o início
// já foi gravado). Com NOBRE_RESUME=<KEY> num início de sessão (source
// startup), a sessão é ligada ao card (force: o agente foi aberto pelo humano
// para continuar o card) e o pacote de retomada vai junto, mesmo sem captura
// completa: é um pedido explícito. Sem captura e sem retomada, não responde
// nada (ou só o aviso de repositório não cadastrado).
func (c *Core) sessionStart(sess domain.Session, ev Event) (Reply, error) {
	set, err := c.st.GetSettings()
	if err != nil {
		return Reply{}, err
	}
	// O que esta sessão passa a ter visto: o contexto de agora. Substitui o
	// registro de antes (resume, clear e compact reinjetam a configuração).
	_, _ = c.st.SwapSeenConfig(sess.ID, sess.SeenConfig, domain.SeenConfig{AgentCreatesCards: set.AgentCreatesCards, Capture: sess.FullCapture})

	var resumed *domain.Card
	var pkg, notFound string
	if key := strings.TrimSpace(ev.Env[resumeEnv]); key != "" && (ev.Source == "" || ev.Source == "startup") {
		switch err := c.st.AttachSession(sess.ID, key, true); {
		case errors.Is(err, store.ErrNotFound):
			notFound = key
		case err != nil:
			return Reply{}, err
		default:
			c.publish("cards", "sessions")
			card, err := c.st.GetCard(key)
			if err != nil {
				return Reply{}, err
			}
			// O card passa a acompanhar o branch de trabalho desta sessão
			// antes de o pacote de retomada ser montado.
			sess.CardID = &card.ID
			c.SyncCardBranch(sess)
			if card, err = c.st.GetCard(key); err != nil {
				return Reply{}, err
			}
			if pkg, err = c.resumePackage(card, sess.ID); err != nil {
				return Reply{}, err
			}
			resumed = &card
		}
	}
	// Compactação numa sessão com card: o pacote de retomada some do
	// contexto junto com o resto e é reinjetado.
	compacted := ev.Source == "compact" && resumed == nil && sess.CardID != nil
	if !sess.FullCapture && resumed == nil && notFound == "" && !compacted {
		if sess.ProjectID != nil {
			return Reply{}, nil
		}
		root, folder := c.unregisteredRoot(sess.Cwd)
		if root == "" {
			return Reply{}, nil
		}
		phrase := UnregisteredPhrase
		todo := "Antes de seguir com o pedido do usuário, cadastre com nobre project add " + root +
			" e faça o levantamento com nobre project scan; " + scanCardsStep(set) + c.guideRef() + "; se o cadastro listar sub-projetos sugeridos, liste-os ao usuário sem cadastrar. O cadastro liga esta sessão ao board."
		if folder {
			// Pasta de repositórios: vira grupo com pasta, que não tem
			// levantamento, e só com o aval do usuário.
			phrase = UnregisteredFolderPhrase
			todo = "Pergunte ao usuário se quer cadastrar esta pasta como grupo com nobre project add " + root +
				"; só cadastre se ele aceitar. O comando lista os sub-projetos sugeridos, que você mostra ao usuário sem cadastrar. O cadastro liga esta sessão ao board."
		}
		return Reply{Context: []string{phrase + " (" + root + ").\n" + todo + "\n\n" + Guidance(set)}}, nil
	}

	// O começo (instruções e card não encontrado) e o resto são montados
	// antes; as memórias entram entre os dois com o espaço que sobrar.
	head := Instructions(set)
	if notFound != "" {
		head += fmt.Sprintf("\n\nO card %s não foi encontrado; a sessão segue sem card.", notFound)
	}
	var b strings.Builder
	if n, ok := c.d.(Noter); ok {
		if note := n.StartNote(sess); note != "" {
			b.WriteString("\n\n" + note)
		}
	}
	if sess.FullCapture {
		apps, err := c.appsGuidance(sess)
		if err != nil {
			return Reply{}, err
		}
		if apps != "" {
			b.WriteString("\n\n" + apps)
		}
	}
	if resumed == nil && sess.CardID != nil {
		card, err := c.st.GetCardByID(*sess.CardID)
		if err != nil {
			return Reply{}, err
		}
		fmt.Fprintf(&b, "\n\nEsta sessão está no card %s · %s.", card.Key, card.Title)
		if compacted {
			pkg, err := c.resumePackage(card, sess.ID)
			if err != nil {
				return Reply{}, err
			}
			fmt.Fprintf(&b, " O contexto foi compactado; continue a partir do pacote de retomada do card.\n\n%s", pkg)
		}
	}
	if sess.FullCapture && set.InjectPausedCards {
		paused, err := c.pausedCards(sess, resumed)
		if err != nil {
			return Reply{}, err
		}
		if len(paused) > 0 {
			b.WriteString("\n\nCards pausados neste repositório e branch (retome com nobre resume <KEY> em vez de criar outro):")
			for _, card := range paused {
				where := strings.TrimSpace(card.WhereStopped)
				if where == "" {
					where = "(onde parei não registrado)"
				}
				fmt.Fprintf(&b, "\n- %s · %s · %s%s", card.Key, card.Title, oneLine(where), c.pauseReason(card))
			}
		}
	}
	if sess.FullCapture {
		b.WriteString(c.unregisteredInGroup(sess))
	}
	if resumed != nil {
		fmt.Fprintf(&b, "\n\nEsta sessão foi ligada ao card %s. Continue a partir do pacote de retomada; não pergunte o que já está nele.\n\n%s", resumed.Key, pkg)
	}
	rest := b.String()
	// Memórias (do driver que as tem) logo depois das instruções; usam o card
	// final da sessão (o retomado já está em sess.CardID). O espaço é o que
	// sobra do teto do contexto depois do resto e do "\n\n" que as separa.
	text := head + rest
	if m, ok := c.d.(Memories); ok && sess.FullCapture {
		room := maxContextRunes - utf8.RuneCountInString(head) - utf8.RuneCountInString(rest) - 2
		if blk := m.MemoriesBlock(sess, set, room); blk != "" {
			text = head + "\n\n" + strings.TrimRight(blk, "\n") + rest
		}
	}
	return Reply{Context: []string{text}}, nil
}

// maxContextRunes é o teto do contexto do início da sessão, com folga: o
// agente corta textos maiores (o primeiro driver corta em 10.000 caracteres e
// acima disso o modelo só vê uma prévia de 2.000). As memórias usam o que
// sobrar abaixo dele.
const maxContextRunes = 9500

// guideRef cita, entre parênteses, o guia do nobre que o Cockpit instala no
// agente (" (veja <GuideName>)"), ou "" se o driver não tem Guide.
func (c *Core) guideRef() string {
	if g, ok := c.d.(Guide); ok {
		if name := g.GuideName(); name != "" {
			return " (veja " + name + ")"
		}
	}
	return ""
}

// unregisteredRoot devolve a raiz a cadastrar quando vale avisar, e se é uma
// pasta de repositórios (true) em vez de um repositório git; "" quando não há o
// que avisar: a raiz é o HOME (ou o contém), já existe projeto para ela
// (mesmo com captura desligada, que funciona como "ignorar") ou, numa pasta de
// repositórios, ela já contém um projeto ou grupo cadastrado (como
// ~/workspace). Numa pasta que não é git só lê o primeiro nível do próprio
// cwd, sem git.
func (c *Core) unregisteredRoot(cwd string) (string, bool) {
	if cwd == "" {
		return "", false
	}
	root := ""
	if wr, ok := c.deps.Git.(gitinfo.WorktreeReader); ok {
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
	if p, err := c.st.ProjectForPath(root); err != nil || p != nil {
		return "", false
	}
	if folder && c.containsProject(root) {
		return "", false
	}
	if folder && !hasRepoChild(root) {
		return "", false
	}
	return root, folder
}

// containsProject diz se algum projeto ou grupo cadastrado fica dentro de dir
// (erro de leitura conta como sim: na dúvida, não avisa).
func (c *Core) containsProject(dir string) bool {
	ps, err := c.st.ListProjects()
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
func (c *Core) unregisteredInGroup(sess domain.Session) string {
	if sess.ProjectID == nil || sess.Worktree == "" || sess.WorktreeLinked {
		return ""
	}
	root := filepath.Clean(sess.Worktree)
	g, err := c.st.ProjectForPath(root)
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
	if _, err := c.st.GetKV("subproject_dismissed", &dismissed); err != nil {
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
func (c *Core) appsGuidance(sess domain.Session) (string, error) {
	if sess.ProjectID == nil {
		return "", nil
	}
	ps, err := c.st.ListProjects()
	if err != nil {
		return "", err
	}
	for _, p := range ps {
		if p.ID != *sess.ProjectID {
			continue
		}
		repo, err := c.st.RepoProject(p)
		if err != nil || repo.IsGroup() {
			return "", err
		}
		kids, err := c.st.Children(repo.ID)
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
func (c *Core) pausedCards(sess domain.Session, except *domain.Card) ([]domain.Card, error) {
	if sess.ProjectID == nil {
		return nil, nil
	}
	ps, err := c.st.ListProjects()
	if err != nil {
		return nil, err
	}
	onBase := sess.Branch == ""
	br, _ := c.deps.Git.(gitinfo.BranchReader) // nil: só a base configurada conta
	// Num monorepo a base e os cards são os da família do pai.
	family := []int64{*sess.ProjectID}
	for _, p := range ps {
		if p.ID != *sess.ProjectID {
			continue
		}
		repo, err := c.st.RepoProject(p)
		if err != nil {
			return nil, err
		}
		// Grupo não tem base: o git nunca roda na pasta dele.
		if !onBase && !repo.IsGroup() {
			onBase = board.ProjectBase(br, repo) == sess.Branch
		}
		if family, err = c.st.FamilyIDs(repo.ID); err != nil {
			return nil, err
		}
	}
	cards, err := c.st.ListCards(store.CardFilter{ProjectIDs: family})
	if err != nil {
		return nil, err
	}
	var out []domain.Card
	for _, card := range cards {
		if card.Column != domain.Pausado || (except != nil && card.ID == except.ID) {
			continue
		}
		if !onBase && card.Branch != sess.Branch && card.Branch != "" {
			continue
		}
		// Pausado por limite com a sessão dona ainda viva: não é para retomar.
		live, err := c.st.HasLiveSession(card.ID)
		if err != nil {
			return nil, err
		}
		if !live {
			out = append(out, card)
		}
	}
	return out, nil
}

// pauseReason é o motivo da pausa no fim da linha do card (" · ..."), ou "".
func (c *Core) pauseReason(card domain.Card) string {
	in := domain.SignalInput{Column: card.Column, Now: time.Now(), LastActivity: time.Now(), BlockedBy: card.BlockedBy}
	if card.BlockedBy != "" {
		if b, err := c.st.GetCard(card.BlockedBy); err == nil {
			in.BlockerExists, in.BlockerConcluded = true, b.Column == domain.Concluido
		}
	}
	if ct, err := c.st.CardContent(card.ID); err == nil {
		for _, q := range ct.Questions {
			if q.Answer == "" {
				in.OpenQuestions++
			}
		}
	}
	switch domain.CardStatusSignal(in) {
	case domain.SignalBloqueado:
		return " · bloqueado por " + card.BlockedBy
	case domain.SignalProntoParaRetomar:
		return " · pronto para retomar (" + card.BlockedBy + " concluído)"
	case domain.SignalAguardandoVoce:
		return fmt.Sprintf(" · aguardando você (%d pergunta(s))", in.OpenQuestions)
	}
	if card.PauseReason == domain.PauseReasonLimit {
		return " · pausado por " + domain.PauseReasonLimit
	}
	return ""
}

// oneLine junta as linhas do texto (o "onde parei" vai numa linha da lista).
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// resumePackage monta o pacote de retomada; a sessão atual, que acabou de
// entrar no card, não serve de base para o diff.
func (c *Core) resumePackage(card domain.Card, currentSession string) (string, error) {
	return resume.ForCard(c.st, c.deps.Git, card, currentSession)
}
