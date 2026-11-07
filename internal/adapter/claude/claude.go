// Package claude é o adaptador do Claude Code: recebe os eventos dos hooks,
// registra sessão, eventos e uso, e responde com contexto, bloqueios e
// negações de ferramenta.
package claude

import (
	"errors"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Adapter implementa adapter.Adapter para o Claude Code.
type Adapter struct {
	st   *store.Store
	deps adapter.Deps

	// ingest serializa a leitura do transcript por sessão (chave: ID da
	// sessão, valor: *sync.Mutex); wg acompanha as leituras em andamento.
	ingest sync.Map
	wg     sync.WaitGroup
}

var _ adapter.Adapter = (*Adapter)(nil)

// New cria o adaptador. deps.Now nil vira time.Now; Git e Publish podem ser nil.
func New(st *store.Store, deps adapter.Deps) *Adapter {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Adapter{st: st, deps: deps}
}

func (a *Adapter) Name() string { return "claude" }

func (a *Adapter) ProcessNames() []string { return []string{"claude"} }

func (a *Adapter) LaunchCommand(initialPrompt string) []string {
	return []string{"claude", initialPrompt}
}

// Wait espera as leituras de transcript em andamento (desligamento e testes).
func (a *Adapter) Wait() { a.wg.Wait() }

// Detect e Install ficam para a Task 10 (setup automático do Claude Code).
func (a *Adapter) Detect() adapter.Detection { return adapter.Detection{} }

func (a *Adapter) Install() (adapter.InstallReport, error) {
	return adapter.InstallReport{}, errors.New("instalação automática do Claude Code ainda não disponível")
}

func (a *Adapter) publish(kinds ...string) {
	if a.deps.Publish == nil {
		return
	}
	for _, k := range kinds {
		a.deps.Publish(k)
	}
}
