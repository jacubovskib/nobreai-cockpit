package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Driver é o que cada agente implementa: o tradutor do hook e o formatador
// da resposta. As capacidades opcionais (Router, Ingester, Noter, Memories,
// Guide, SpendUnit) são descobertas por asserção de tipo.
type Driver interface {
	Name() string
	// Translate traduz o evento event com o payload bruto do agente. event
	// pode vir vazio (o driver o acha no payload, se puder).
	Translate(event string, payload []byte) (Event, error)
	// Render escreve a resposta r ao evento ev no formato do agente.
	Render(ev Event, r Reply) (adapter.HookReply, error)
}

// Router é opcional: o driver escolhe a sessão do evento (p.ex. uma conversa
// aberta em duas janelas, com uma sessão filha por processo). Com
// handled false o núcleo resolve como sempre; com true, a sessão devolvida
// (ou o erro, ErrIgnore inclusive) é a do evento.
type Router interface {
	Route(c *Core, ev Event) (domain.Session, bool, error)
}

// Ingester é opcional: lê o histórico do agente (uso, contexto) depois do fim
// de turno e do fim da sessão. Roda no caminho do hook: o que for demorado
// fica em segundo plano.
type Ingester interface {
	Ingest(sess domain.Session, ev Event)
}

// Noter é opcional: textos que só o driver conhece para o contexto do
// agente (p.ex. o aviso da sessão filha de uma conversa aberta em duas
// janelas). Vazio é nada a dizer.
type Noter interface {
	// StartNote entra no contexto do início da sessão registrada, logo
	// depois das instruções (e das memórias), antes do resto.
	StartNote(sess domain.Session) string
	// PromptNote abre o contexto da resposta ao prompt. Roda a cada prompt,
	// com ou sem captura, antes das regras do núcleo: o driver pode registrar
	// ali o que for só dele.
	PromptNote(sess domain.Session, ev Event) string
}

// Memories é opcional: o bloco de memórias do projeto da sessão, com no
// máximo room caracteres ("" sem memórias ou sem espaço), que entra no início
// da sessão com captura completa logo depois das instruções.
type Memories interface {
	MemoriesBlock(sess domain.Session, set domain.Settings, room int) string
}

// Guide é opcional: como o agente chama as instruções do nobre que o Cockpit
// instala nele, já com o artigo ("a <tipo> <nome>"), citadas no aviso de
// repositório não cadastrado como " (veja <GuideName>)". Sem ele, o aviso não
// cita guia.
type Guide interface {
	GuideName() string
}

// SpendUnit é opcional: o driver que mede o gasto em créditos
// (SpendsCredits true) não é bloqueado pelos limites, que são em dólares.
type SpendUnit interface {
	SpendsCredits() bool
}

// Core aplica as regras de sessão do Cockpit aos eventos de um driver.
type Core struct {
	st   *store.Store
	deps adapter.Deps
	d    Driver

	// repoRoots guarda o topo do repositório de cada diretório de projeto
	// (chave e valor: string), para traduzir caminhos de worktrees.
	repoRoots sync.Map
	// outsideSwitch marca a troca de branch vista de fora do agente (prompt,
	// fim de turno) que ainda não levou o card: chave ID da sessão, valor o
	// branch que a sessão tinha antes. Só em memória: perder a marca num
	// restart só deixa o card onde estava.
	outsideSwitch sync.Map
}

// New cria o núcleo do driver d. deps.Now nil vira time.Now; Git e Publish
// podem ser nil.
func New(st *store.Store, deps adapter.Deps, d Driver) *Core {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Core{st: st, deps: deps, d: d}
}

// Deps são as dependências do núcleo, compartilhadas com o driver: o que um
// muda (o Git, nos testes) vale para os dois. Só mude antes do primeiro hook
// (como SetAlive): hooks em andamento leem sem trava.
func (c *Core) Deps() *adapter.Deps { return &c.deps }

// Handle trata um hook: tradução, sessão, regras e resposta. Toda sessão é
// registrada; sem captura completa (projeto cadastrado com capture=true) só
// sessão e uso são gravados: nada de prompts, arquivos ou comandos. Hook que
// não pertence a nenhuma sessão (ErrIgnore) é respondido vazio.
//
// O store não recebe contexto: o trabalho de banco nunca é cancelado junto
// com a requisição (o `nobre hook` desiste em 2 s, mas o daemon termina o que
// começou).
func (c *Core) Handle(_ context.Context, h adapter.HookCall) (adapter.HookReply, error) {
	ev, err := c.d.Translate(h.Event, h.Payload)
	if err != nil {
		return adapter.HookReply{}, err
	}
	if ev.ProviderSessionID == "" {
		return adapter.HookReply{}, errors.New("hook sem id de sessão")
	}
	ev.Agent, ev.PID, ev.Env = c.d.Name(), h.AgentPID, h.Env
	if ev.At.IsZero() {
		ev.At = c.deps.Now()
	}
	sess, err := c.session(ev)
	if errors.Is(err, ErrIgnore) {
		return adapter.HookReply{}, nil
	}
	if err != nil {
		return adapter.HookReply{}, err
	}
	reply, err := c.apply(sess, ev)
	out, rerr := c.d.Render(ev, reply)
	if rerr != nil {
		// O erro da regra, se houve, não some atrás do da formatação.
		return adapter.HookReply{}, errors.Join(err, rerr)
	}
	return out, err
}

// apply roda as regras do evento e monta a resposta. Started grava o início
// antes da resposta; Prompt e ToolAfter gravam depois dela (os avisos olham o
// estado de antes). TurnEnd lê o histórico mesmo quando a resposta falha.
func (c *Core) apply(sess domain.Session, ev Event) (Reply, error) {
	switch ev.Kind {
	case Started:
		c.Event(sess.ID, domain.EvSessionStart, map[string]any{"source": ev.Source}, ev.At)
		return c.sessionStart(sess, ev)
	case Prompt:
		r := Reply{Context: []string{c.promptNote(sess, ev), c.noticeText(sess, ev), c.ContextWarning(sess, ev)}}
		if sess.FullCapture {
			c.Event(sess.ID, domain.EvPrompt, map[string]any{"text": ev.Prompt}, ev.At)
		} else {
			c.publish("sessions") // última atividade mudou
		}
		return r, nil
	case ToolBefore:
		return c.toolBefore(sess, ev)
	case ToolAfter:
		r := Reply{Context: []string{c.noticeText(sess, ev), c.bigOutputWarning(sess, ev)}}
		var err error
		if sess.FullCapture {
			err = c.recordTool(sess, ev)
		}
		return r, err
	case TurnEnd:
		r, err := c.turnEnd(sess, ev)
		c.ingest(sess, ev)
		return r, err
	case Ended:
		c.outsideSwitch.Delete(sess.ID)
		return Reply{}, c.end(sess, ev)
	}
	return Reply{}, nil
}

func (c *Core) promptNote(sess domain.Session, ev Event) string {
	if n, ok := c.d.(Noter); ok {
		return n.PromptNote(sess, ev)
	}
	return ""
}

func (c *Core) ingest(sess domain.Session, ev Event) {
	if in, ok := c.d.(Ingester); ok {
		in.Ingest(sess, ev)
	}
}

// recordTool grava a leitura, a edição ou o comando de um ToolAfter. O
// comando também faz a sessão e o card acompanharem uma troca de branch.
func (c *Core) recordTool(sess domain.Session, ev Event) error {
	switch t := ev.Tool; t.Kind {
	case ToolRead, ToolEdit:
		if t.Path == "" {
			return nil
		}
		path, err := c.projectRelative(sess, t.Path)
		if err != nil {
			return err
		}
		kind := domain.EvFileEdit
		if t.Kind == ToolRead {
			kind = domain.EvFileRead
		}
		c.Event(sess.ID, kind, map[string]any{"path": path}, ev.At)
	case ToolCommand:
		data := map[string]any{"command": t.Command}
		if t.ExitCode != nil {
			data["exit_code"] = *t.ExitCode
		}
		c.Event(sess.ID, domain.EvCommand, data, ev.At)
		c.followBranch(sess, true)
	}
	return nil
}

// Event grava um evento da sessão e avisa o stream. Falha só vai para o log:
// o hook nunca quebra o agente por causa de um evento.
func (c *Core) Event(sessionID, kind string, data map[string]any, at time.Time) {
	b, err := json.Marshal(data)
	if err == nil {
		err = c.st.AddEvent(domain.Event{SessionID: sessionID, Kind: kind, Data: b, At: at})
	}
	if err != nil {
		c.logf("evento %s da sessão %s: %v", kind, sessionID, err)
		return
	}
	c.publish("sessions")
}

func (c *Core) publish(kinds ...string) {
	if c.deps.Publish == nil {
		return
	}
	for _, k := range kinds {
		c.deps.Publish(k)
	}
}

// logf registra uma falha do hook com o nome do agente.
func (c *Core) logf(format string, args ...any) {
	log.Printf("hook "+c.d.Name()+": "+format, args...)
}
