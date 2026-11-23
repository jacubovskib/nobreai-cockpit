// Package claude é o adaptador do Claude Code: o driver do núcleo de sessões
// (agentcore) que traduz os hooks e formata as respostas, mais a leitura do
// transcript (uso e contexto), a instalação, a status line e as janelas
// duplas de uma conversa.
package claude

import (
	"slices"
	"sync"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/proc"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Adapter implementa adapter.Adapter para o Claude Code.
type Adapter struct {
	st *store.Store
	// core aplica as regras de sessão aos hooks traduzidos por este driver.
	core *agentcore.Core
	// deps são as do núcleo (o mesmo valor: trocar o Git vale para os dois).
	deps *adapter.Deps
	// paths são os arquivos que Detect e Install leem e escrevem.
	paths   Paths
	nobreMu sync.Mutex // protege paths.NobreBin, re-resolvido a cada instalação

	// ingest serializa a leitura do transcript por sessão (chave: ID da
	// sessão, valor: *sync.Mutex); wg acompanha as leituras em andamento.
	ingest sync.Map
	wg     sync.WaitGroup
	// readerMu: leituras de transcript (readLocked) tomam RLock; a troca de
	// versão do leitor toma Lock, para nenhuma leitura em andamento gravar um
	// cursor antigo por cima do cursor que voltou ao começo.
	readerMu sync.RWMutex
	// alive diz se o processo do agente com esse PID ainda existe (decide
	// entre sessão filha e retomada); padrão: /proc, com o nome do processo
	// do Claude (um PID reaproveitado por outro programa não conta).
	alive func(pid int) bool
}

var (
	_ adapter.Adapter    = (*Adapter)(nil)
	_ agentcore.Driver   = (*Adapter)(nil)
	_ agentcore.Router   = (*Adapter)(nil)
	_ agentcore.Ingester = (*Adapter)(nil)
	_ agentcore.Observer = (*Adapter)(nil)
	_ agentcore.Noter    = (*Adapter)(nil)
	_ agentcore.Memories = (*Adapter)(nil)
	_ agentcore.Guide    = (*Adapter)(nil)
)

// New cria o adaptador sem caminhos de instalação (use SetPaths; o nobred usa
// DefaultPaths). deps.Now nil vira time.Now; Git e Publish podem ser nil.
func New(st *store.Store, deps adapter.Deps) *Adapter {
	a := &Adapter{st: st}
	a.core = agentcore.New(st, deps, a)
	a.deps = a.core.Deps()
	a.alive = func(pid int) bool { return slices.Contains(a.ProcessNames(), proc.Comm("/proc", pid)) }
	return a
}

// SetAlive troca a verificação de processo vivo (testes, sem /proc real).
// Chame antes do primeiro hook.
func (a *Adapter) SetAlive(f func(pid int) bool) { a.alive = f }

func (a *Adapter) Name() string { return "claude" }

func (a *Adapter) ProcessNames() []string { return []string{"claude"} }

func (a *Adapter) Binary() string { return a.cliPath() }

func (a *Adapter) LaunchCommand(bin, initialPrompt string) []string {
	// "--" evita que um prompt começando com "-" seja lido como opção.
	return []string{bin, "--", initialPrompt}
}

// Wait espera as leituras de transcript em andamento (desligamento e testes).
func (a *Adapter) Wait() { a.wg.Wait() }

func (a *Adapter) publish(kinds ...string) {
	if a.deps.Publish == nil {
		return
	}
	for _, k := range kinds {
		a.deps.Publish(k)
	}
}
