package domain

// Column é uma coluna do board.
type Column string

const (
	Backlog     Column = "backlog"
	EmAndamento Column = "em_andamento"
	Pausado     Column = "pausado"
	Revisao     Column = "revisao"
	Concluido   Column = "concluido"
)

// Columns lista as colunas do board na ordem de exibição.
var Columns = []Column{Backlog, EmAndamento, Pausado, Revisao, Concluido}

// Tipos de evento.
const (
	EvSessionStart  = "session_start"
	EvSessionEnd    = "session_end"
	EvPrompt        = "prompt"
	EvFileRead      = "file_read"
	EvFileEdit      = "file_edit"
	EvCommand       = "command"
	EvNote          = "note"
	EvDecision      = "decision"
	EvEssentialFile = "essential_file"
	EvTodo          = "todo"
	EvQuestion      = "question"
	EvAnswer        = "answer"
	EvHeartbeat     = "heartbeat"
	EvDone          = "done"        // nobre done: agente marcou o card como pronto para revisão
	EvHowToTest     = "how_to_test" // nobre test: agente registrou "como testar"
	EvLimitPause    = "limit_pause" // limite de custo pausou o card
)

// PauseReasonLimit é o motivo gravado quando o limite de custo pausa o card.
const PauseReasonLimit = "limite de custo"

// Transition é um evento que pode mover um card entre colunas.
type Transition int

const (
	SessionAttached Transition = iota
	SessionEnded
	AgentDone
	HumanApproved
)

// NextColumn aplica a regra automática de movimentação do board.
func NextColumn(cur Column, ev Transition) Column {
	switch ev {
	case SessionAttached:
		return EmAndamento
	case SessionEnded:
		if cur == EmAndamento {
			return Pausado
		}
	case AgentDone:
		if cur == EmAndamento {
			return Revisao
		}
	case HumanApproved:
		if cur == Revisao {
			return Concluido
		}
	}
	return cur
}
