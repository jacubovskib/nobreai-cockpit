package claude

import (
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// configNotice compara o que a sessão viu com a configuração de agora. text
// vazio é nada a dizer; nu é o que a sessão passa a ter visto. Sem registro
// (sessão anterior ao recurso), só registra o estado atual, sem aviso.
func configNotice(seen *domain.SeenConfig, fullCapture bool, set domain.Settings) (text string, nu domain.SeenConfig) {
	nu = domain.SeenConfig{AgentCreatesCards: set.AgentCreatesCards, Capture: fullCapture}
	if seen == nil {
		return "", nu
	}
	switch {
	case seen.Capture && !fullCapture:
		return "NobreAI Cockpit: a captura deste projeto foi desligada. Não use mais os comandos nobre nesta sessão.", nu
	case !seen.Capture && fullCapture:
		return "NobreAI Cockpit: a captura deste projeto foi ligada.\n" + instructionsFor(set), nu
	case fullCapture && seen.AgentCreatesCards != set.AgentCreatesCards:
		return "NobreAI Cockpit: a configuração mudou.\n" + guidanceFor(set), nu
	}
	return "", nu
}

// noticeText é o texto do aviso ao agente principal, uma vez, de que a
// configuração mudou desde o que a sessão viu ("" sem nada a dizer). O swap
// compara e troca o registro, então hooks simultâneos geram um único aviso.
// Falhas só impedem o aviso.
func (a *Adapter) noticeText(sess domain.Session, p payload, now time.Time) string {
	if p.AgentID != "" {
		return ""
	}
	set, err := a.st.GetSettings()
	if err != nil {
		return ""
	}
	text, nu := configNotice(sess.SeenConfig, sess.FullCapture, set)
	if sess.SeenConfig != nil && nu == *sess.SeenConfig {
		return ""
	}
	ok, err := a.st.SwapSeenConfig(sess.ID, sess.SeenConfig, nu)
	if err != nil || !ok || text == "" {
		return ""
	}
	first, _, _ := strings.Cut(text, "\n")
	_ = a.st.AddConfigLog(domain.LogAgentNotified, map[string]any{"session": sess.ID, "text": first}, now)
	return text
}

// contextReply entrega os textos não vazios ao agente como additionalContext
// do evento; false quando não há nada a dizer.
func contextReply(event string, texts ...string) (adapter.HookReply, bool) {
	var parts []string
	for _, t := range texts {
		if t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return adapter.HookReply{}, false
	}
	r, err := jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     event,
		"additionalContext": strings.Join(parts, "\n\n"),
	}})
	if err != nil {
		return adapter.HookReply{}, false
	}
	return r, true
}
