package board

import (
	"log"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// FinishSession encerra a sessão (st.EndSession). Se ela tinha card, o card
// ficou em pausado, nenhuma outra sessão viva é dona dele e a opção
// IncompleteAction é "mark", avalia o handoff: card não pronto recebe a marca
// de handoff incompleto. Sessão que já estava encerrada antes desta chamada
// não é avaliada de novo. A marca é melhor esforço: depois do EndSession, um
// erro só vai para o log e a função devolve nil, para quem chama seguir com o
// fim da sessão (evento, transcript, publicação).
func FinishSession(st *store.Store, sessionID, reason string, now time.Time) error {
	sess, err := st.GetSession(sessionID)
	if err != nil {
		return err
	}
	if sess.EndedAt != nil {
		return nil
	}
	if err := st.EndSession(sessionID, reason, now); err != nil {
		return err
	}
	if sess.CardID == nil {
		return nil
	}
	if err := markIncompleteHandoff(st, *sess.CardID, now); err != nil {
		log.Printf("fim da sessão %s: marcar handoff incompleto: %v", sessionID, err)
	}
	return nil
}

// markIncompleteHandoff grava a marca de handoff incompleto no card pausado
// sem outra sessão viva e fora da regra, com IncompleteAction "mark".
func markIncompleteHandoff(st *store.Store, cardID int64, now time.Time) error {
	set, err := st.GetSettings()
	if err != nil {
		return err
	}
	if set.IncompleteAction != "mark" {
		return nil
	}
	card, err := st.GetCardByID(cardID)
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
