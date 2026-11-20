// Package claude é o adaptador do Claude Code: recebe os eventos dos hooks,
// registra sessão, eventos e uso, e responde com contexto, bloqueios e
// negações de ferramenta.
package claude

import (
	"slices"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/proc"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Adapter implementa adapter.Adapter para o Claude Code.
type Adapter struct {
	st   *store.Store
	deps adapter.Deps
	// paths são os arquivos que Detect e Install leem e escrevem.
	paths   Paths
	nobreMu sync.Mutex // protege paths.NobreBin, re-resolvido a cada instalação

	// ingest serializa a leitura do transcript por sessão (chave: ID da
	// sessão, valor: *sync.Mutex); wg acompanha as leituras em andamento.
	ingest sync.Map
	wg     sync.WaitGroup
	// repoRoots guarda o topo do repositório de cada diretório de projeto
	// (chave e valor: string), para traduzir caminhos de worktrees.
	repoRoots sync.Map
	// outsideSwitch marca a troca de branch vista de fora do agente (prompt,
	// fim de turno) que ainda não levou o card: chave ID da sessão, valor o
	// branch que a sessão tinha antes. Só em memória: perder a marca num
	// restart só deixa o card onde estava.
	outsideSwitch sync.Map
	// childUnwarned guarda as sessões filhas criadas fora do SessionStart que
	// ainda não receberam a linha da filha (chave: ID da sessão). Só em
	// memória: perder a marca num restart só deixa de mandar a linha.
	childUnwarned sync.Map
	// alive diz se o processo do agente com esse PID ainda existe (decide
	// entre sessão filha e retomada); padrão: /proc, com o nome do processo
	// do Claude (um PID reaproveitado por outro programa não conta).
	alive func(pid int) bool
}

var _ adapter.Adapter = (*Adapter)(nil)

// New cria o adaptador sem caminhos de instalação (use SetPaths; o nobred usa
// DefaultPaths). deps.Now nil vira time.Now; Git e Publish podem ser nil.
func New(st *store.Store, deps adapter.Deps) *Adapter {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	a := &Adapter{st: st, deps: deps}
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
