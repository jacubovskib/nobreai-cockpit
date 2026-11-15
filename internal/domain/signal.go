package domain

import "time"

// StaleAfter é o tempo sem atividade a partir do qual um card ganha sinal.
const StaleAfter = 24 * time.Hour

// Signal é o aviso que o card mostra quando está parado há muito tempo.
type Signal string

const (
	SignalNone              Signal = ""
	SignalParado            Signal = "parado"
	SignalAguardandoRevisao Signal = "aguardando_revisao"
)

// CardSignal diz se o card precisa de atenção: em andamento ou pausado sem
// atividade há mais de StaleAfter é "parado"; em revisão, "aguardando
// revisão". Backlog e concluído nunca têm sinal.
func CardSignal(col Column, lastActivity, now time.Time) Signal {
	if now.Sub(lastActivity) <= StaleAfter {
		return SignalNone
	}
	switch col {
	case EmAndamento, Pausado:
		return SignalParado
	case Revisao:
		return SignalAguardandoRevisao
	}
	return SignalNone
}
