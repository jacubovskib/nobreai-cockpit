// Package agents é o registro dos agentes de IA que o Cockpit conhece: o
// daemon registra os drivers instanciados; o `nobre` só precisa dos nomes de
// processo (Static).
package agents

import (
	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
)

// Known é a ordem fixa dos agentes conhecidos, com ou sem driver.
var Known = []string{"claude", "codex", "kiro"}

var displayNames = map[string]string{"claude": "Claude Code", "codex": "Codex", "kiro": "Kiro"}

// DisplayName é o nome de tela do agente; desconhecido volta como veio.
func DisplayName(name string) string {
	if n, ok := displayNames[name]; ok {
		return n
	}
	return name
}

// Registry guarda os drivers na ordem de registro. Um *Registry nil é um
// registro vazio.
type Registry struct {
	list []adapter.Adapter
	by   map[string]adapter.Adapter
}

// New registra os drivers na ordem dada.
func New(list ...adapter.Adapter) *Registry {
	r := &Registry{by: make(map[string]adapter.Adapter, len(list))}
	for _, a := range list {
		r.list = append(r.list, a)
		r.by[a.Name()] = a
	}
	return r
}

// Get devolve o driver do agente.
func (r *Registry) Get(name string) (adapter.Adapter, bool) {
	if r == nil {
		return nil, false
	}
	a, ok := r.by[name]
	return a, ok
}

// All devolve os drivers na ordem de registro.
func (r *Registry) All() []adapter.Adapter {
	if r == nil {
		return nil
	}
	return r.list
}

// ProcessNames mapeia cada agente aos nomes de processo do seu CLI.
func (r *Registry) ProcessNames() map[string][]string {
	out := map[string][]string{}
	for _, a := range r.All() {
		out[a.Name()] = a.ProcessNames()
	}
	return out
}

// AllProcessNames junta os nomes de processo de todos os drivers, em ordem.
func (r *Registry) AllProcessNames() []string {
	var out []string
	for _, a := range r.All() {
		out = append(out, a.ProcessNames()...)
	}
	return out
}

// Static são os nomes de processo de todos os drivers compilados, sem store:
// o `nobre` os usa para achar o PID do agente. Acrescentar um driver é uma
// linha aqui.
func Static() []string {
	return New(
		claude.New(nil, adapter.Deps{}),
	).AllProcessNames()
}
