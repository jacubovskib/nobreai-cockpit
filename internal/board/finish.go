package board

import (
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// FinishSession encerra a sessão (st.EndSession). Se ela tinha card, o card
// ficou em pausado, nenhuma outra sessão viva é dona dele e a opção
// IncompleteAction é "mark", avalia o handoff: card não pronto recebe a marca
// de handoff incompleto.
func FinishSession(st *store.Store, sessionID, reason string, now time.Time) error {
	sess, err := st.GetSession(sessionID)
	if err != nil {
		return err
	}
	if err := st.EndSession(sessionID, reason, now); err != nil {
		return err
	}
	if sess.CardID == nil {
		return nil
	}
	set, err := st.GetSettings()
	if err != nil {
		return err
	}
	if set.IncompleteAction != "mark" {
		return nil
	}
	card, err := st.GetCardByID(*sess.CardID)
	if err != nil {
		return err
	}
	if card.Column != domain.Pausado {
		return nil
	}
	live, err := st.HasLiveSession(card.ID)
	if err != nil || live {
		return err
	}
	content, err := st.CardContent(card.ID)
	if err != nil {
		return err
	}
	lastEdit, err := st.LastEventAt(card.ID, domain.EvFileEdit)
	if err != nil {
		return err
	}
	if domain.Readiness(card, content, false, lastEdit, set, now).Ready {
		return nil
	}
	return st.SetHandoffIncomplete(card.ID, true, now)
}
