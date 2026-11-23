package claude

import (
	"encoding/json"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
)

// hookNames é o nome do hook do Claude Code de cada evento, que volta no
// hookEventName da resposta.
var hookNames = map[agentcore.Kind]string{}

func init() {
	for name, k := range kinds {
		hookNames[k] = name
	}
}

// Render escreve a resposta no JSON que o Claude Code lê: a negação da
// ferramenta (PreToolUse), o bloqueio do fim do turno (Stop) ou os textos
// não vazios como additionalContext do evento; nada a dizer é saída vazia.
func (a *Adapter) Render(ev agentcore.Event, r agentcore.Reply) (adapter.HookReply, error) {
	switch {
	case r.Block != nil:
		return jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": r.Block.Reason,
		}})
	case r.HoldTurn != nil:
		return jsonReply(map[string]any{"decision": "block", "reason": r.HoldTurn.Reason})
	}
	var parts []string
	for _, t := range r.Context {
		if t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return adapter.HookReply{}, nil
	}
	return jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     hookNames[ev.Kind],
		"additionalContext": strings.Join(parts, "\n\n"),
	}})
}

// jsonReply serializa a resposta do hook (sem escapar <, > e &: o texto vai
// para o agente ler).
func jsonReply(v any) (adapter.HookReply, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return adapter.HookReply{}, err
	}
	return adapter.HookReply{Stdout: strings.TrimSuffix(b.String(), "\n")}, nil
}
