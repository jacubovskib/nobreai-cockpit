package kiro

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
)

// payload é o JSON que o kiro-cli manda no stdin de cada hook (V3). Não há
// transcript_path: o histórico fica em ~/.kiro/sessions.
type payload struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	Cwd           string `json:"cwd"`
	Prompt        string `json:"prompt"`
	Reason        string `json:"reason"`
	ToolName      string `json:"tool_name"`
	// ToolInput e ToolResponse ficam brutos: o tool_response é texto nas
	// ferramentas nativas, mas nada garante isso nas de MCP.
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

// kinds traduz o trigger do hook do Kiro; os demais só tocam a sessão
// (agentcore.Other).
var kinds = map[string]agentcore.Kind{
	"SessionStart":     agentcore.Started,
	"UserPromptSubmit": agentcore.Prompt,
	"PreToolUse":       agentcore.ToolBefore,
	"PostToolUse":      agentcore.ToolAfter,
	"Stop":             agentcore.TurnEnd,
	"SessionEnd":       agentcore.Ended,
	"PostCompact":      agentcore.Compacted,
}

// Translate traduz o hook do kiro-cli (event vazio: o hook_event_name do
// payload) para o evento do núcleo.
func (a *Adapter) Translate(event string, raw []byte) (agentcore.Event, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return agentcore.Event{}, fmt.Errorf("payload do hook inválido: %w", err)
	}
	if event == "" {
		event = p.HookEventName
	}
	ev := agentcore.Event{Kind: kinds[event], ProviderSessionID: p.SessionID, Cwd: p.Cwd, Prompt: p.Prompt}
	switch ev.Kind {
	case agentcore.Started:
		// O SessionStart do Kiro não diz como a sessão começou: o Cockpit só
		// abre conversas novas (nunca --resume-id), então é um início.
		ev.Source = "startup"
	case agentcore.Ended:
		ev.Reason, ev.ReasonCode = endReason(p.Reason), p.Reason
	}
	if p.ToolName != "" {
		ev.Tool = translateTool(p)
	}
	return ev, nil
}

// endReasons traduz o reason do SessionEnd do Kiro.
var endReasons = map[string]string{
	"shutdown": "saiu do kiro-cli",
}

// endReason é o motivo do fim no texto do Cockpit: o traduzido, senão o
// reason como veio (vazio fica com o padrão do núcleo).
func endReason(code string) string {
	if r := endReasons[code]; r != "" {
		return r
	}
	return code
}

// toolKinds mapeia as ferramentas do harness V3. As buscas (grep_search,
// file_search) leem, mas não um arquivo: ficam sem caminho. As demais,
// inclusive as de MCP (mcp_<servidor>_<ferramenta>), são "outro".
var toolKinds = map[string]agentcore.ToolKind{
	"read_file":    agentcore.ToolRead,
	"grep_search":  agentcore.ToolRead,
	"file_search":  agentcore.ToolRead,
	"fs_write":     agentcore.ToolEdit,
	"str_replace":  agentcore.ToolEdit,
	"fs_append":    agentcore.ToolEdit,
	"delete_file":  agentcore.ToolEdit,
	"execute_bash": agentcore.ToolCommand,
	"execute_pwsh": agentcore.ToolCommand,
	"run_command":  agentcore.ToolCommand,
}

// fileTools são as ferramentas cujo tool_input.path é o arquivo lido ou
// editado.
var fileTools = map[string]bool{"read_file": true, "fs_write": true, "str_replace": true, "fs_append": true, "delete_file": true}

func translateTool(p payload) agentcore.Tool {
	out := responseText(p.ToolResponse)
	t := agentcore.Tool{Name: p.ToolName, Kind: toolKinds[p.ToolName], RawInput: p.ToolInput, RawOutput: p.ToolResponse,
		OutputBytes: int64(len(out))}
	var in struct {
		Path    string `json:"path"`
		Command string `json:"command"`
	}
	// Campo com outro tipo só fica vazio: o resto da entrada é lido assim
	// mesmo, e o hook nunca falha por causa da ferramenta.
	_ = json.Unmarshal(p.ToolInput, &in)
	switch {
	case fileTools[p.ToolName]:
		t.Path = absPath(in.Path, p.Cwd)
	case t.Kind == agentcore.ToolCommand:
		t.Command = in.Command
		if code, ok := exitCode(out); ok {
			t.ExitCode = &code
		}
	}
	return t
}

// responseText é o tool_response como texto: a string decodificada, ou o
// JSON como veio quando não é string (null e ausente: vazio).
func responseText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// absPath resolve o caminho relativo pelo cwd do hook (o núcleo o deixa
// relativo à raiz do projeto); "~" e vazio ficam como vieram.
func absPath(path, cwd string) string {
	if path == "" || cwd == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return path
	}
	return filepath.Join(cwd, path)
}

// exitLine é o "Exit Code: N" com que o execute_bash termina a resposta.
var exitLine = regexp.MustCompile(`(?:^|\n)Exit Code: (-?\d+)\s*$`)

// exitCode lê o código de saída do fim da resposta do comando, se vier
// (nunca é inventado).
func exitCode(out string) (int, bool) {
	m := exitLine.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	code, err := strconv.Atoi(m[1])
	return code, err == nil
}
