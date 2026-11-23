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
// da resposta. As capacidades opcionais (Router, Ingester, Responder) são
// descobertas por asserção de tipo.
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

// Responder é TEMPORÁRIO: as respostas (contexto do início da sessão, avisos,
// limites, fim de turno) ainda são montadas pelo driver e vêm para o
// núcleo na tarefa seguinte, que apaga esta interface. O núcleo o
// chama depois de gravar o que é dele, na mesma ordem do adaptador antigo.
type Responder interface {
	Respond(c *Core, sess domain.Session, ev Event) (Reply, error)
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
// muda (o Git, nos testes) vale para os dois.
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
		return adapter.HookReply{}, errors.New("payload do hook sem session_id")
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
		return adapter.HookReply{}, rerr
	}
	return out, err
}

// apply roda as regras do evento. Started grava o início antes da resposta;
// Prompt e ToolAfter gravam depois dela (os avisos olham o estado de antes).
func (c *Core) apply(sess domain.Session, ev Event) (Reply, error) {
	switch ev.Kind {
	case Started:
		c.Event(sess.ID, domain.EvSessionStart, map[string]any{"source": ev.Source}, ev.At)
		return c.respond(sess, ev)
	case Prompt:
		r, err := c.respond(sess, ev)
		if sess.FullCapture {
			c.Event(sess.ID, domain.EvPrompt, map[string]any{"text": ev.Prompt}, ev.At)
		} else {
			c.publish("sessions") // última atividade mudou
		}
		return r, err
	case ToolBefore:
		return c.respond(sess, ev)
	case ToolAfter:
		r, err := c.respond(sess, ev)
		if sess.FullCapture {
			if rerr := c.recordTool(sess, ev); err == nil {
				err = rerr
			}
		}
		return r, err
	case TurnEnd:
		r, err := c.respond(sess, ev)
		c.ingest(sess, ev)
		return r, err
	case Ended:
		c.outsideSwitch.Delete(sess.ID)
		return Reply{}, c.end(sess, ev)
	}
	return Reply{}, nil
}

func (c *Core) respond(sess domain.Session, ev Event) (Reply, error) {
	if r, ok := c.d.(Responder); ok {
		return r.Respond(c, sess, ev)
	}
	return Reply{}, nil
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
