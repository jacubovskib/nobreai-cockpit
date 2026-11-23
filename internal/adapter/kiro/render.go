package kiro

import (
	"encoding/json"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
)

// Render escreve a resposta como o kiro-cli a lê: com código 0, o stdout
// entra no contexto do agente (texto puro, sem JSON); o código 2 com o motivo
// no stderr nega a ferramenta (PreToolUse); a decisão "block" em JSON no
// stdout segura o fim do turno (Stop), e o agente continua com o motivo
// (conferido ao vivo com o kiro-cli 3.5.0). Nada a dizer é saída vazia.
func (a *Adapter) Render(ev agentcore.Event, r agentcore.Reply) (adapter.HookReply, error) {
	switch {
	case r.Raw != nil:
		return *r.Raw, nil
	case r.Block != nil:
		return adapter.HookReply{Stderr: r.Block.Reason, Exit: 2}, nil
	case r.HoldTurn != nil:
		var b strings.Builder
		enc := json.NewEncoder(&b)
		// Sem escapar <, > e &: o texto vai para o agente ler.
		enc.SetEscapeHTML(false)
		if err := enc.Encode(map[string]string{"decision": "block", "reason": r.HoldTurn.Reason}); err != nil {
			return adapter.HookReply{}, err
		}
		return adapter.HookReply{Stdout: strings.TrimSuffix(b.String(), "\n")}, nil
	}
	var parts []string
	for _, t := range r.Context {
		if t != "" {
			parts = append(parts, t)
		}
	}
	return adapter.HookReply{Stdout: strings.Join(parts, "\n\n")}, nil
}
