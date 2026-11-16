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
	SignalAguardandoVoce    Signal = "aguardando_voce"
	SignalBloqueado         Signal = "bloqueado"
	SignalProntoParaRetomar Signal = "pronto_para_retomar"
)

// SignalInput são os fatos de que o sinal de um card depende.
type SignalInput struct {
	Column       Column
	LastActivity time.Time
	Now          time.Time
	// BlockedBy é a chave do card bloqueador ("" = sem bloqueio).
	BlockedBy string
	// BlockerExists: o bloqueador existe; BlockerConcluded: está em concluido.
	BlockerExists    bool
	BlockerConcluded bool
	// OpenQuestions são as perguntas sem resposta no card.
	OpenQuestions int
}

// CardStatusSignal diz por que o card precisa de atenção. Concluído nunca tem
// sinal; pausado ou backlog com bloqueador existente é "bloqueado" (ou "pronto
// para retomar" se o bloqueador concluiu); pausado com pergunta aberta é
// "aguardando você"; no resto vale CardSignal. Os três sinais novos valem na
// hora, sem esperar StaleAfter.
func CardStatusSignal(in SignalInput) Signal {
	if in.Column == Concluido {
		return SignalNone
	}
	if (in.Column == Pausado || in.Column == Backlog) && in.BlockedBy != "" && in.BlockerExists {
		if in.BlockerConcluded {
			return SignalProntoParaRetomar
		}
		return SignalBloqueado
	}
	if in.Column == Pausado && in.OpenQuestions > 0 {
		return SignalAguardandoVoce
	}
	return CardSignal(in.Column, in.LastActivity, in.Now)
}

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
