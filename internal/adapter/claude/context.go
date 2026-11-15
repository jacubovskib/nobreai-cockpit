package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// registeredPhrase abre o contexto do SessionStart quando a sessão é
// registrada no board; a skill global só usa o nobre quando a vê.
const registeredPhrase = "NobreAI Cockpit: esta sessão é registrada no board"

// unregisteredPhrase abre o aviso do SessionStart num repositório git que
// ainda não está no board.
const unregisteredPhrase = "NobreAI Cockpit: este repositório não está cadastrado no board"

// instructions é o começo do contexto injetado no SessionStart: quando criar
// card (spec 6.0) e os comandos nobre.
const instructions = registeredPhrase + `.
Crie um card com nobre start "<título>" quando começar algo que precisa de histórico: tarefa de várias etapas, mudanças em vários arquivos ou trabalho que outro agente pode continuar. Se o usuário pedir um card, crie; se citar uma chave NAC-, use nobre resume <KEY>. Perguntas rápidas não viram card.
Com card: nobre note "<onde parei>" a cada passo relevante; nobre decide "<decisão>" --why "<porquê>"; nobre file <caminho> --why "<motivo>"; nobre todo add "<texto>"; nobre ask "<pergunta>" quando precisar de uma decisão do usuário; nobre test "<como testar>"; nobre done ao terminar; nobre status mostra o card.`

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
		root := a.unregisteredRoot(sess.Cwd)
		if root == "" {
			return adapter.HookReply{}, nil
		}
		return jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "SessionStart",
			"additionalContext": unregisteredPhrase + " (" + root + ").\nAntes de seguir com o pedido do usuário, cadastre com nobre project add " + root +
				" e faça o levantamento com nobre project scan; crie os cards importantes com nobre card add (veja a skill nobre-cockpit). O cadastro liga esta sessão ao board.\n\n" + instructions,
		}})
	}

	var b strings.Builder
	b.WriteString(instructions)
	if notFound != "" {
		fmt.Fprintf(&b, "\n\nO card %s não foi encontrado; a sessão segue sem card.", notFound)
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
	if sess.FullCapture {
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
				fmt.Fprintf(&b, "\n- %s · %s · %s", c.Key, c.Title, oneLine(where))
			}
		}
	}
	if resumed != nil {
		fmt.Fprintf(&b, "\n\nEsta sessão foi ligada ao card %s. Continue a partir do pacote de retomada; não pergunte o que já está nele.\n\n%s", resumed.Key, pkg)
	}
	return jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "SessionStart",
		"additionalContext": b.String(),
	}})
}

// unregisteredRoot devolve a raiz do repositório git de cwd quando vale
// avisar que ele não está cadastrado; "" quando não há repositório, a raiz é o
// HOME ou já existe projeto para ela (mesmo com captura desligada, que funciona
// como "ignorar").
func (a *Adapter) unregisteredRoot(cwd string) string {
	wr, ok := a.deps.Git.(gitinfo.WorktreeReader)
	if !ok || cwd == "" {
		return ""
	}
	root := wr.Locate(cwd).Main
	if root == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == filepath.Clean(root) {
		return ""
	}
	if p, err := a.st.ProjectForPath(root); err != nil || p != nil {
		return ""
	}
	return root
}

// pausedCards lista os cards pausados do projeto e do branch da sessão
// (exceto o que acabou de ser retomado).
func (a *Adapter) pausedCards(sess domain.Session, except *domain.Card) ([]domain.Card, error) {
	if sess.ProjectID == nil {
		return nil, nil
	}
	cards, err := a.st.ListCards(store.CardFilter{ProjectID: *sess.ProjectID})
	if err != nil {
		return nil, err
	}
	var out []domain.Card
	for _, c := range cards {
		if c.Column == domain.Pausado && c.Branch == sess.Branch && (except == nil || c.ID != except.ID) {
			out = append(out, c)
		}
	}
	return out, nil
}

// oneLine junta as linhas do texto (o "onde parei" vai numa linha da lista).
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// resumePackage monta o pacote de retomada; a sessão atual, que acabou de
// entrar no card, não serve de base para o diff.
func (a *Adapter) resumePackage(c domain.Card, currentSession string) (string, error) {
	return resume.ForCard(a.st, a.deps.Git, c, currentSession)
}
