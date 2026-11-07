// Package adapter define a interface dos adaptadores de agente (Claude Code,
// Codex, Kiro): hooks, detecção, instalação e comando de lançamento.
package adapter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

// Adapter traduz um agente de IA para o Cockpit.
type Adapter interface {
	Name() string
	// ProcessNames são os nomes de processo usados para achar o PID do agente.
	ProcessNames() []string
	HandleHook(ctx context.Context, h HookCall) (HookReply, error)
	Detect() Detection
	Install() (InstallReport, error)
	// LaunchCommand devolve o argv que inicia o agente com o prompt inicial.
	LaunchCommand(initialPrompt string) []string
}

// HookCall é a chamada de hook repassada pelo `nobre hook`.
type HookCall struct {
	Event    string            `json:"event"`
	Payload  json.RawMessage   `json:"payload"`
	AgentPID int               `json:"agent_pid"`
	Env      map[string]string `json:"env"`
}

// HookReply é o que o `nobre hook` imprime e o código com que sai.
type HookReply struct {
	Stdout string `json:"stdout"`
	Exit   int    `json:"exit"`
}

// Deps são as dependências injetadas nos adaptadores.
type Deps struct {
	Now     func() time.Time
	Git     gitinfo.GitInfo
	Publish func(kind string)
}

// Detection descreve o agente encontrado na máquina.
type Detection struct {
	Found   bool    `json:"found"`
	Path    string  `json:"path"`
	Version string  `json:"version"`
	Checks  []Check `json:"checks"`
}

// Check é um item do checklist de saúde do agente.
type Check struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	OK     bool   `json:"ok"`
}

// InstallReport resume o que Install alterou.
type InstallReport struct {
	Changed  []string `json:"changed"`
	Backup   string   `json:"backup"`
	Messages []string `json:"messages"`
}
