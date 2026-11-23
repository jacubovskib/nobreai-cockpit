package claude

import (
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// Os textos e o aviso de contexto saíram do adaptador para o núcleo
// (agentcore). Estes nomes mantêm os testes do adaptador como eram.
const (
	registeredPhrase         = agentcore.RegisteredPhrase
	unregisteredPhrase       = agentcore.UnregisteredPhrase
	unregisteredFolderPhrase = agentcore.UnregisteredFolderPhrase
	askOnlyGuidance          = agentcore.AskOnlyGuidance
)

var (
	instructionsFor = agentcore.Instructions
	instructions    = agentcore.Instructions(domain.DefaultSettings())
	guidance        = agentcore.Guidance(domain.DefaultSettings())
)

// contextWarning é o aviso de contexto do próximo prompt (consome a faixa).
func (a *Adapter) contextWarning(sess domain.Session, p payload) string {
	return a.core.ContextWarning(sess, agentcore.Event{SubagentID: p.AgentID})
}
