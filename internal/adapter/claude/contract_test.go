package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore/contracttest"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// fixture lê um payload de testdata e troca os valores fixos pelos
// marcadores da bateria de contrato.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.NewReplacer(
		"/home/u/.claude/projects/-w-api/5f1c2d9e-8b7a-4c3d-9e2f-1a2b3c4d5e6f.jsonl", "{{transcript}}",
		"5f1c2d9e-8b7a-4c3d-9e2f-1a2b3c4d5e6f", "{{session}}",
		"/w/api", "{{cwd}}",
	).Replace(string(b)))
}

// TestAgentContract roda a bateria de contrato dos drivers contra o Claude.
func TestAgentContract(t *testing.T) {
	reply := func(r adapter.HookReply) map[string]any {
		var m map[string]any
		if r.Stdout != "" {
			if err := json.Unmarshal([]byte(r.Stdout), &m); err != nil {
				t.Fatalf("saída não é JSON: %v\n%s", err, r.Stdout)
			}
		}
		return m
	}
	contracttest.Run(t, func(st *store.Store, deps adapter.Deps) adapter.Adapter {
		a := New(st, deps)
		a.SetAlive(func(int) bool { return false })
		return a
	}, contracttest.Fixtures{
		Start:        fixture(t, "session_start"),
		Prompt:       fixture(t, "user_prompt_submit"),
		ReadAfter:    fixture(t, "post_tool_use_read"),
		EditAfter:    fixture(t, "post_tool_use_edit"),
		CommandAfter: fixture(t, "post_tool_use_bash"),
		Stop:         fixture(t, "stop"),
		End:          fixture(t, "session_end"),
		PromptText:   "corrija o refresh do token OAuth",
		ReadPath:     "auth/oauth_test.go",
		EditPath:     "auth/oauth.go",
		Command:      "go test ./auth/...",
		DecodeContext: func(r adapter.HookReply) []string {
			out, _ := reply(r)["hookSpecificOutput"].(map[string]any)
			if s, _ := out["additionalContext"].(string); s != "" {
				return []string{s}
			}
			return nil
		},
		DecodeHold: func(r adapter.HookReply) (string, bool) {
			m := reply(r)
			if m["decision"] != "block" {
				return "", false
			}
			s, _ := m["reason"].(string)
			return s, true
		},
	})
}
