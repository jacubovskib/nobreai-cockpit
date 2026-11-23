package kiro

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore/contracttest"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// fixture lê um payload de testdata e troca os valores fixos pelos
// marcadores da bateria de contrato (o Kiro não manda transcript_path).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	return []byte(strings.NewReplacer(providerID, "{{session}}", "/w/api", "{{cwd}}").Replace(string(readFixture(t, name))))
}

// TestAgentContract roda a bateria de contrato dos drivers contra o Kiro.
func TestAgentContract(t *testing.T) {
	contracttest.Run(t, func(st *store.Store, deps adapter.Deps) adapter.Adapter {
		return New(st, deps)
	}, contracttest.Fixtures{
		Start:        fixture(t, "session_start"),
		Prompt:       fixture(t, "user_prompt_submit"),
		ReadAfter:    fixture(t, "post_tool_use_read"),
		EditAfter:    fixture(t, "post_tool_use_write"),
		CommandAfter: fixture(t, "post_tool_use_bash"),
		Stop:         fixture(t, "stop"),
		End:          fixture(t, "session_end"),
		// O `nobre hook kiro <Evento>` repassa o nome do evento.
		Events: contracttest.EventNames{Start: "SessionStart", Prompt: "UserPromptSubmit", ReadAfter: "PostToolUse",
			EditAfter: "PostToolUse", CommandAfter: "PostToolUse", Stop: "Stop", End: "SessionEnd"},
		PromptText: "Leia o arquivo a.txt, depois rode o comando shell 'echo oi', depois crie o arquivo b.txt com o texto ok. Responda só 'feito'.",
		ReadPath:   "a.txt",
		EditPath:   "b.txt",
		Command:    "echo oi",
		DecodeContext: func(r adapter.HookReply) []string {
			if r.Exit != 0 || r.Stdout == "" {
				return nil
			}
			return []string{r.Stdout}
		},
		DecodeHold: func(r adapter.HookReply) (string, bool) {
			var m map[string]string
			if r.Exit != 0 || r.Stdout == "" || json.Unmarshal([]byte(r.Stdout), &m) != nil || m["decision"] != "block" {
				return "", false
			}
			return m["reason"], true
		},
	})
}
