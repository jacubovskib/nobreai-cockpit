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
	// Binary é o caminho absoluto do CLI do agente, sem resolver symlinks (o
	// nome do processo tem de bater com ProcessNames); "" = não encontrado.
	Binary() string
	// LaunchCommand devolve o argv que inicia o agente bin (vindo de Binary)
	// com o prompt inicial.
	LaunchCommand(bin, initialPrompt string) []string
}

// StatusLiner é opcional: adaptador cuja status line o Cockpit integra. Fica
// fora de Install() porque mexe num comando do usuário: só com pedido explícito.
type StatusLiner interface {
	InstallStatusLine() (InstallReport, error)
	UninstallStatusLine() (InstallReport, error)
}

// InstallPlanner é opcional: calcula o que Install mudaria, sem escrever.
type InstallPlanner interface {
	PlanInstall() (InstallReport, error)
}

// HookCall é a chamada de hook repassada pelo `nobre hook`.
type HookCall struct {
	Event    string            `json:"event"`
	Payload  json.RawMessage   `json:"payload"`
	AgentPID int               `json:"agent_pid"`
	Env      map[string]string `json:"env"`
}

// HookReply é o que o `nobre hook` imprime (Stdout no stdout, Stderr no
// stderr) e o código com que sai.
type HookReply struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr,omitempty"`
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
	// Code explica a falha: "missing", "outdated", "dead_binary", "unreadable",
	// "no_settings" (o arquivo de configuração não existe) ou "not_found".
	// Vazio quando OK.
	Code string `json:"code,omitempty"`
	// State resume o estado do próprio item (o comando da status line, a data
	// e o tamanho da skill...): entra na impressão de "Ignorar", para que só
	// uma mudança nele traga o alerta de volta.
	State string `json:"-"`
}

// InstallReport resume o que Install alterou.
type InstallReport struct {
	Changed  []string `json:"changed"`
	Backup   string   `json:"backup"`
	Messages []string `json:"messages"`
}
