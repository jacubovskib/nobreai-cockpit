package agentcore

import (
	"strings"

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
		return "NobreAI Cockpit: a captura deste projeto foi ligada.\n" + Instructions(set), nu
	case fullCapture && seen.AgentCreatesCards != set.AgentCreatesCards:
		return "NobreAI Cockpit: a configuração mudou.\n" + Guidance(set), nu
	}
	return "", nu
}

// noticeText é o texto do aviso ao agente principal, uma vez, de que a
// configuração mudou desde o que a sessão viu ("" sem nada a dizer). O swap
// compara e troca o registro, então hooks simultâneos geram um único aviso.
// Falhas só impedem o aviso.
func (c *Core) noticeText(sess domain.Session, ev Event) string {
	if ev.SubagentID != "" {
		return ""
	}
	set, err := c.st.GetSettings()
	if err != nil {
		return ""
	}
	text, nu := configNotice(sess.SeenConfig, sess.FullCapture, set)
	if sess.SeenConfig != nil && nu == *sess.SeenConfig {
		return ""
	}
	ok, err := c.st.SwapSeenConfig(sess.ID, sess.SeenConfig, nu)
	if err != nil || !ok || text == "" {
		return ""
	}
	first, _, _ := strings.Cut(text, "\n")
	_ = c.st.AddConfigLog(domain.LogAgentNotified, map[string]any{"session": sess.ID, "text": first}, ev.At)
	return text
}
