// Package kiro é o adaptador do Kiro (kiro-cli, harness V3): o driver do
// núcleo de sessões (agentcore) que traduz os hooks do kiro-cli e formata as
// respostas. O gasto do Kiro é em créditos (agentcore.SpendUnit): os limites
// em dólares não valem para ele.
package kiro

import (
	"context"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Adapter implementa adapter.Adapter para o kiro-cli.
type Adapter struct {
	st *store.Store
	// core aplica as regras de sessão aos hooks traduzidos por este driver.
	core *agentcore.Core
	// paths são os arquivos e comandos que o instalador e a saúde usam.
	now     func() time.Time
	paths   Paths
	nobreMu sync.Mutex

	// publish avisa a tela (nil = ninguém escuta).
	publish func(kind string)
	// ingest serializa a leitura do histórico por sessão (chave: id da
	// sessão, valor: *sync.Mutex); wg acompanha as leituras em andamento.
	ingest sync.Map
	wg     sync.WaitGroup
	// retries são as esperas das releituras depois do fim de turno (o
	// kiro-cli grava o usage_summary depois do hook Stop); quit as cancela.
	retries  []time.Duration
	quit     chan struct{}
	quitOnce sync.Once
}

var (
	_ adapter.Adapter     = (*Adapter)(nil)
	_ agentcore.Driver    = (*Adapter)(nil)
	_ agentcore.SpendUnit = (*Adapter)(nil)
	_ agentcore.Guide     = (*Adapter)(nil)
	_ agentcore.Ingester  = (*Adapter)(nil)
	_ agentcore.Observer  = (*Adapter)(nil)
)

// New cria o adaptador. deps.Now nil vira time.Now; Git e Publish podem ser
// nil. Com st nil (só os nomes de processo, no `nobre`) nenhum hook pode ser
// tratado.
func New(st *store.Store, deps adapter.Deps) *Adapter {
	a := &Adapter{st: st, paths: DefaultPaths(), now: deps.Now, publish: deps.Publish,
		retries: []time.Duration{2 * time.Second, 6 * time.Second}, quit: make(chan struct{})}
	if a.now == nil {
		a.now = time.Now
	}
	a.core = agentcore.New(st, deps, a)
	return a
}

func (a *Adapter) Name() string { return "kiro" }

// ProcessNames: o processo do agente é o kiro-cli (as ferramentas e os hooks
// rodam num node filho dele, nome genérico demais para identificar).
func (a *Adapter) ProcessNames() []string { return []string{"kiro-cli"} }

// SessionEnv é a variável com o id da sessão que o kiro-cli passa aos hooks e
// aos comandos de shell do agente.
func (a *Adapter) SessionEnv() string { return "KIRO_SESSION_ID" }

// HandleHook trata o hook repassado pelo `nobre hook kiro <Evento>`.
func (a *Adapter) HandleHook(ctx context.Context, h adapter.HookCall) (adapter.HookReply, error) {
	return a.core.Handle(ctx, h)
}

// SpendsCredits: o Kiro mede o gasto em créditos (agentcore.SpendUnit).
func (a *Adapter) SpendsCredits() bool { return true }

// GuideName: as instruções do nobre ficam no steering do Kiro
// (agentcore.Guide).
func (a *Adapter) GuideName() string { return "as instruções do Cockpit no steering do Kiro" }

// LaunchCommand abre uma conversa nova no harness V3 com o prompt inicial.
func (a *Adapter) LaunchCommand(bin, initialPrompt string) []string {
	return []string{bin, "chat", "--v3", initialPrompt}
}
