// Package agentcore é o núcleo de sessões do Cockpit: as regras que valem
// para qualquer agente (criar, retomar e encerrar a sessão, gravar os eventos
// de prompt, arquivo e comando, acompanhar branch e worktree, localizar o
// projeto) e as respostas ao agente (contexto do início da sessão, avisos de
// configuração, de contexto e de saída grande, bloqueio por limite de custo,
// cobrança de handoff no fim do turno). Cada agente entra por um Driver, que traduz o hook do agente para
// um Event e a Reply do núcleo para a saída que o agente entende.
package agentcore

import (
	"encoding/json"
	"time"
)

// Kind é o tipo de um evento normalizado.
type Kind int

const (
	// Other é o evento sem regra própria: só toca a sessão.
	Other Kind = iota
	// Started é o início (ou retomada, limpeza, compactação) da sessão.
	Started
	// Prompt é o texto que o usuário mandou ao agente.
	Prompt
	// ToolBefore vem antes de o agente usar uma ferramenta (pode bloqueá-la).
	ToolBefore
	// ToolAfter vem depois de o agente usar uma ferramenta.
	ToolAfter
	// TurnEnd é o fim de uma resposta do agente (pode segurar o turno).
	TurnEnd
	// Ended é o fim da sessão.
	Ended
	// Compacted é a compactação do contexto: só toca a sessão.
	Compacted
)

// ToolKind é o que a ferramenta faz, para o Cockpit.
type ToolKind int

const (
	ToolOther ToolKind = iota
	ToolRead
	ToolEdit
	ToolCommand
)

// Tool é a ferramenta de um ToolBefore/ToolAfter.
type Tool struct {
	// Name é o nome da ferramenta no agente.
	Name string
	Kind ToolKind
	// Path é o arquivo lido ou editado (ToolRead, ToolEdit).
	Path string
	// Command é a linha de comando (ToolCommand).
	Command string
	// ExitCode é o código de saída do comando, quando o agente informa (nunca
	// é inventado).
	ExitCode *int
	// OutputBytes é o tamanho da saída da ferramenta.
	OutputBytes int64
	// RawInput e RawOutput são a entrada e a saída da ferramenta como o agente
	// as mandou.
	RawInput, RawOutput json.RawMessage
}

// Event é um hook traduzido pelo driver. Agent, PID, Env e At (quando o
// driver não o preenche) vêm do núcleo, a partir da chamada do hook.
type Event struct {
	Kind Kind
	// Agent é o nome do driver.
	Agent string
	// ProviderSessionID é o id da sessão no agente (obrigatório).
	ProviderSessionID string
	Cwd               string
	// Source diz como a sessão começou (Started): startup, resume, clear,
	// compact.
	Source string
	// Prompt é o texto do usuário (Prompt).
	Prompt string
	// Reason é o motivo do fim no texto do Cockpit, já traduzido pelo driver
	// (Ended; vazio = "encerrada pelo agente").
	Reason string
	// ReasonCode é o motivo do fim como o agente o informa, guardado no
	// evento de fim da sessão (Ended).
	ReasonCode string
	// TranscriptPath é o histórico da conversa no disco.
	TranscriptPath string
	// SubagentID vem preenchido quando o hook dispara dentro de um subagente:
	// só o agente principal muda o local da sessão.
	SubagentID string
	// PID é o processo do agente (0 = desconhecido).
	PID int
	// At é o horário do hook.
	At time.Time
	// Env são as variáveis de ambiente repassadas pelo `nobre hook`
	// (NOBRE_RESUME).
	Env  map[string]string
	Tool Tool
	// StopActive diz que o fim de turno já é a continuação de um turno
	// segurado (TurnEnd).
	StopActive bool
}

// Reply é a resposta normalizada ao hook; a vazia é "nada a dizer".
type Reply struct {
	// Context são os textos para o contexto do agente (os vazios não contam).
	Context []string
	// Block nega a ferramenta (ToolBefore).
	Block *Decision
	// HoldTurn segura o fim do turno (TurnEnd).
	HoldTurn *Decision
}

// Decision é um bloqueio com o motivo que o agente lê.
type Decision struct{ Reason string }
