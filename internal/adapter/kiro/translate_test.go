package kiro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
)

const providerID = "sess_0b6f3c1e-5a2d-4e8f-9c7b-2d1e0f3a4b5c"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// translate traduz a fixture com o nome do evento que o `nobre hook kiro`
// repassa e, de novo, sem ele (o nome vem do hook_event_name): os dois
// caminhos dão o mesmo evento.
func translate(t *testing.T, event, name string) agentcore.Event {
	t.Helper()
	a := New(nil, adapterDeps())
	raw := readFixture(t, name)
	ev, err := a.Translate(event, raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	fromPayload, err := a.Translate("", raw)
	if err != nil {
		t.Fatalf("%s sem nome de evento: %v", name, err)
	}
	if fromPayload.Kind != ev.Kind {
		t.Fatalf("%s: Kind com nome %v, sem nome %v", name, ev.Kind, fromPayload.Kind)
	}
	if ev.ProviderSessionID != providerID || ev.Cwd != "/w/api" {
		t.Fatalf("%s: sessão %q cwd %q", name, ev.ProviderSessionID, ev.Cwd)
	}
	return ev
}

func TestTranslateFixtures(t *testing.T) {
	if ev := translate(t, "SessionStart", "session_start"); ev.Kind != agentcore.Started || ev.Source != "startup" {
		t.Fatalf("SessionStart = %+v", ev)
	}
	ev := translate(t, "UserPromptSubmit", "user_prompt_submit")
	if ev.Kind != agentcore.Prompt || ev.Prompt != "Leia o arquivo a.txt, depois rode o comando shell 'echo oi', depois crie o arquivo b.txt com o texto ok. Responda só 'feito'." {
		t.Fatalf("UserPromptSubmit = %+v", ev)
	}
	if ev := translate(t, "Stop", "stop"); ev.Kind != agentcore.TurnEnd || ev.StopActive {
		t.Fatalf("Stop = %+v", ev)
	}
	ev = translate(t, "SessionEnd", "session_end")
	if ev.Kind != agentcore.Ended || ev.Reason != "saiu do kiro-cli" || ev.ReasonCode != "shutdown" {
		t.Fatalf("SessionEnd = %+v", ev)
	}

	for _, c := range []struct {
		name    string
		kind    agentcore.Kind
		tool    agentcore.ToolKind
		toolNm  string
		path    string
		command string
	}{
		{"pre_tool_use_read", agentcore.ToolBefore, agentcore.ToolRead, "read_file", "/w/api/a.txt", ""},
		{"post_tool_use_read", agentcore.ToolAfter, agentcore.ToolRead, "read_file", "/w/api/a.txt", ""},
		{"pre_tool_use_write", agentcore.ToolBefore, agentcore.ToolEdit, "fs_write", "/w/api/b.txt", ""},
		{"post_tool_use_write", agentcore.ToolAfter, agentcore.ToolEdit, "fs_write", "/w/api/b.txt", ""},
		{"pre_tool_use_bash", agentcore.ToolBefore, agentcore.ToolCommand, "execute_bash", "", "echo oi"},
		{"post_tool_use_bash", agentcore.ToolAfter, agentcore.ToolCommand, "execute_bash", "", "echo oi"},
	} {
		event := "PreToolUse"
		if c.kind == agentcore.ToolAfter {
			event = "PostToolUse"
		}
		ev := translate(t, event, c.name)
		tl := ev.Tool
		if ev.Kind != c.kind || tl.Kind != c.tool || tl.Name != c.toolNm || tl.Path != c.path || tl.Command != c.command {
			t.Fatalf("%s = %+v", c.name, ev)
		}
		if len(tl.RawInput) == 0 {
			t.Fatalf("%s: sem RawInput", c.name)
		}
	}

	// Depois da ferramenta: tamanho da saída (o texto, sem as aspas do
	// JSON) e, no comando, o código de saída do fim do tool_response.
	bash := translate(t, "PostToolUse", "post_tool_use_bash").Tool
	if bash.ExitCode == nil || *bash.ExitCode != 0 {
		t.Fatalf("exit_code = %v", bash.ExitCode)
	}
	if out := "Output:\noi\n\n\nExit Code: 0"; bash.OutputBytes != int64(len(out)) {
		t.Fatalf("OutputBytes = %d, quer %d", bash.OutputBytes, len(out))
	}
	if pre := translate(t, "PreToolUse", "pre_tool_use_bash").Tool; pre.ExitCode != nil || pre.OutputBytes != 0 {
		t.Fatalf("antes do comando = %+v", pre)
	}
	if read := translate(t, "PostToolUse", "post_tool_use_read").Tool; read.OutputBytes == 0 || len(read.RawOutput) == 0 {
		t.Fatalf("leitura = %+v", read)
	}
}

// toolPayload monta um PostToolUse do Kiro.
func toolPayload(t *testing.T, tool string, input map[string]any, response any) []byte {
	t.Helper()
	m := map[string]any{"session_id": providerID, "hook_event_name": "PostToolUse", "cwd": "/w/api",
		"tool_name": tool, "tool_input": input}
	if response != nil {
		m["tool_response"] = response
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Ferramentas fora do caminho feliz: edições além do fs_write, buscas sem
// arquivo, comandos de outros shells, MCP e desconhecidas sem evento.
func TestTranslateToolKinds(t *testing.T) {
	a := New(nil, adapterDeps())
	for _, c := range []struct {
		tool  string
		input map[string]any
		kind  agentcore.ToolKind
		path  string
		cmd   string
	}{
		{"str_replace", map[string]any{"path": "/w/api/x.go", "oldStr": "a", "newStr": "b"}, agentcore.ToolEdit, "/w/api/x.go", ""},
		{"fs_append", map[string]any{"path": "/w/api/x.go", "text": "c"}, agentcore.ToolEdit, "/w/api/x.go", ""},
		{"delete_file", map[string]any{"path": "/w/api/x.go"}, agentcore.ToolEdit, "/w/api/x.go", ""},
		// Caminho relativo: o do cwd do hook.
		{"fs_write", map[string]any{"path": "auth/oauth.go", "text": "x"}, agentcore.ToolEdit, "/w/api/auth/oauth.go", ""},
		{"read_file", map[string]any{"path": "README.md"}, agentcore.ToolRead, "/w/api/README.md", ""},
		// Buscas leem, mas não um arquivo: o path delas é a pasta da busca.
		{"grep_search", map[string]any{"pattern": "TODO", "path": "/w/api/auth"}, agentcore.ToolRead, "", ""},
		{"file_search", map[string]any{"pattern": "*.go", "path": "/w/api"}, agentcore.ToolRead, "", ""},
		{"execute_pwsh", map[string]any{"command": "Get-ChildItem"}, agentcore.ToolCommand, "", "Get-ChildItem"},
		{"run_command", map[string]any{"command": "make test"}, agentcore.ToolCommand, "", "make test"},
		{"mcp_github_create_issue", map[string]any{"path": "/w/api/x.go", "title": "t"}, agentcore.ToolOther, "", ""},
		{"web_fetch", map[string]any{"url": "https://example.com"}, agentcore.ToolOther, "", ""},
	} {
		ev, err := a.Translate("PostToolUse", toolPayload(t, c.tool, c.input, "ok"))
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		if tl := ev.Tool; tl.Name != c.tool || tl.Kind != c.kind || tl.Path != c.path || tl.Command != c.cmd {
			t.Fatalf("%s = %+v", c.tool, tl)
		}
	}
}

// O código de saída só vem do "Exit Code: N" no fim do tool_response; sem
// ele, ou com uma resposta que não é texto, fica desconhecido (nunca
// inventado), e o hook não falha.
func TestTranslateExitCode(t *testing.T) {
	a := New(nil, adapterDeps())
	code := func(resp any) *int {
		t.Helper()
		ev, err := a.Translate("PostToolUse", toolPayload(t, "execute_bash", map[string]any{"command": "make"}, resp))
		if err != nil {
			t.Fatalf("%v: %v", resp, err)
		}
		return ev.Tool.ExitCode
	}
	if c := code("Output:\nfalhou\n\n\nExit Code: 2"); c == nil || *c != 2 {
		t.Fatalf("Exit Code: 2 = %v", c)
	}
	if c := code("Exit Code: 127\n"); c == nil || *c != 127 {
		t.Fatalf("Exit Code: 127 = %v", c)
	}
	if c := code("Output:\nExit Code: 1 no meio\nfim"); c != nil {
		t.Fatalf("Exit Code fora do fim = %d", *c)
	}
	for _, resp := range []any{"Output:\nok", map[string]any{"exit_code": 3}, []any{"x"}} {
		if c := code(resp); c != nil {
			t.Fatalf("%v: exit_code = %d", resp, *c)
		}
	}
	if c := code(nil); c != nil {
		t.Fatalf("sem tool_response: exit_code = %d", *c)
	}
	// Saída que não é texto conta o JSON como veio.
	ev, err := a.Translate("PostToolUse", toolPayload(t, "mcp_x_y", map[string]any{}, map[string]any{"a": 1}))
	if err != nil || ev.Tool.OutputBytes != int64(len(`{"a":1}`)) {
		t.Fatalf("saída JSON: %+v, %v", ev.Tool, err)
	}
}

func TestTranslateOtherEvents(t *testing.T) {
	a := New(nil, adapterDeps())
	for event, want := range map[string]agentcore.Kind{"PostCompact": agentcore.Compacted, "PreCompact": agentcore.Other, "Notificacao": agentcore.Other} {
		ev, err := a.Translate(event, []byte(`{"session_id":"`+providerID+`","hook_event_name":"`+event+`","cwd":"/w/api"}`))
		if err != nil || ev.Kind != want {
			t.Fatalf("%s = %v, %v", event, ev.Kind, err)
		}
	}
	// Motivo desconhecido fica como veio; vazio cai no padrão do núcleo.
	for code, want := range map[string]string{"shutdown": "saiu do kiro-cli", "crash": "crash", "": ""} {
		ev, err := a.Translate("SessionEnd", []byte(`{"session_id":"`+providerID+`","reason":"`+code+`"}`))
		if err != nil || ev.Reason != want || ev.ReasonCode != code {
			t.Fatalf("%q: Reason %q ReasonCode %q, %v", code, ev.Reason, ev.ReasonCode, err)
		}
	}
	if _, err := a.Translate("Stop", []byte("{")); err == nil {
		t.Fatal("payload inválido sem erro")
	}
}
