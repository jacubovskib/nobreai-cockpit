// Package agents é o registro dos agentes de IA que o Cockpit conhece: o
// daemon registra os drivers instanciados; o `nobre` só precisa dos nomes de
// processo (Static).
package agents

import (
	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/kiro"
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

// Compiled instancia os drivers compilados no binário, sem store (nenhum
// hook pode ser tratado): o `nobre` os usa pelos nomes de processo, pela
// variável de sessão e pela instalação. Cada chamada devolve instâncias
// novas. Acrescentar um driver é uma linha aqui.
func Compiled() []adapter.Adapter {
	return []adapter.Adapter{
		claude.New(nil, adapter.Deps{}),
		kiro.New(nil, adapter.Deps{}),
	}
}

// Static são os nomes de processo de todos os drivers compilados, sem store:
// o `nobre` os usa para achar o PID do agente.
func Static() []string {
	return New(Compiled()...).AllProcessNames()
}

// sessionEnver é a capacidade opcional de um driver cujo CLI põe o id da
// sessão numa variável de ambiente do shell do agente.
type sessionEnver interface{ SessionEnv() string }

// SessionEnvs mapeia o agente à variável de ambiente com o id da sua sessão,
// só para os drivers que têm uma (o Claude não tem). Sem store: o `nobre` usa
// como alternativa ao PID.
func SessionEnvs() map[string]string {
	out := map[string]string{}
	for _, a := range Compiled() {
		if e, ok := a.(sessionEnver); ok && e.SessionEnv() != "" {
			out[a.Name()] = e.SessionEnv()
		}
	}
	return out
}
