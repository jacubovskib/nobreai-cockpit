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

// noticeReply avisa o agente principal, uma vez, de que a configuração mudou
// desde o que a sessão viu. O swap compara e troca o registro, então hooks
// simultâneos geram um único aviso. Falhas só impedem o aviso.
func (a *Adapter) noticeReply(sess domain.Session, p payload, event string, now time.Time) (adapter.HookReply, bool) {
	if p.AgentID != "" {
		return adapter.HookReply{}, false
	}
	set, err := a.st.GetSettings()
	if err != nil {
		return adapter.HookReply{}, false
	}
	text, nu := configNotice(sess.SeenConfig, sess.FullCapture, set)
	if sess.SeenConfig != nil && nu == *sess.SeenConfig {
		return adapter.HookReply{}, false
	}
	ok, err := a.st.SwapSeenConfig(sess.ID, sess.SeenConfig, nu)
	if err != nil || !ok || text == "" {
		return adapter.HookReply{}, false
	}
	first, _, _ := strings.Cut(text, "\n")
	_ = a.st.AddConfigLog(domain.LogAgentNotified, map[string]any{"session": sess.ID, "text": first}, now)
	r, err := jsonReply(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     event,
		"additionalContext": text,
	}})
	if err != nil {
		return adapter.HookReply{}, false
	}
	return r, true
}
