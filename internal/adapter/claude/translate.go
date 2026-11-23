package claude

import (
	"encoding/json"
	"fmt"

	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
)

// kinds traduz o nome do hook do Claude Code; os demais eventos só tocam a
// sessão (agentcore.Other).
var kinds = map[string]agentcore.Kind{
	"SessionStart":     agentcore.Started,
	"UserPromptSubmit": agentcore.Prompt,
	"PreToolUse":       agentcore.ToolBefore,
	"PostToolUse":      agentcore.ToolAfter,
	"Stop":             agentcore.TurnEnd,
	"SessionEnd":       agentcore.Ended,
}

// Translate traduz o hook do Claude Code (event vazio: o hook_event_name do
// payload) para o evento do núcleo.
func (a *Adapter) Translate(event string, raw []byte) (agentcore.Event, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return agentcore.Event{}, fmt.Errorf("payload do hook inválido: %w", err)
	}
	if event == "" {
		event = p.HookEventName
	}
	ev := agentcore.Event{Kind: kinds[event], ProviderSessionID: p.SessionID, Cwd: p.Cwd, Source: p.Source,
		Prompt: p.Prompt, Reason: endReason(p.Reason), ReasonCode: p.Reason, TranscriptPath: p.TranscriptPath, SubagentID: p.AgentID,
		StopActive: p.StopHookActive}
	if p.ToolName != "" {
		ev.Tool = translateTool(p)
	}
	return ev, nil
}

// endReasons traduz o reason do SessionEnd do Claude Code.
var endReasons = map[string]string{
	"clear":             "conversa limpa (/clear)",
	"logout":            "logout",
	"prompt_input_exit": "usuário saiu",
	"other":             "encerrada pelo agente",
}

// endReason é o motivo do fim no texto do Cockpit: o traduzido, senão o
// reason como veio (vazio fica com o padrão do núcleo).
func endReason(code string) string {
	if r := endReasons[code]; r != "" {
		return r
	}
	return code
}

// translateTool mapeia as ferramentas do Claude Code: Read é leitura;
// Edit, Write, MultiEdit e NotebookEdit, edição; Bash, comando.
func translateTool(p payload) agentcore.Tool {
	t := agentcore.Tool{Name: p.ToolName, RawInput: p.ToolInput, RawOutput: p.ToolResponse,
		OutputBytes: int64(len(p.ToolResponse))}
	in := p.input()
	switch p.ToolName {
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
		t.Kind = agentcore.ToolEdit
		if p.ToolName == "Read" {
			t.Kind = agentcore.ToolRead
		}
		t.Path = in.FilePath
		if t.Path == "" {
			t.Path = in.NotebookPath
		}
	case "Bash":
		t.Kind, t.Command = agentcore.ToolCommand, in.Command
		if code, ok := exitCode(p.ToolResponse); ok {
			t.ExitCode = &code
		}
	}
	return t
}

// exitCode lê o código de saída do tool_response do Bash, se vier (o Claude
// Code nem sempre o informa; nunca é inventado).
func exitCode(raw json.RawMessage) (int, bool) {
	var r map[string]json.RawMessage
	if json.Unmarshal(raw, &r) != nil {
		return 0, false
	}
	for _, k := range []string{"exit_code", "exitCode"} {
		var code int
		if v, ok := r[k]; ok && json.Unmarshal(v, &code) == nil {
			return code, true
		}
	}
	return 0, false
}
