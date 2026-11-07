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
	EvHeartbeat     = "heartbeat"
)
