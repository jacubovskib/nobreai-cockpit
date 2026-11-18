package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func asst(id string, in, cr, cw int, blocks ...string) string {
	out := 3
	if in+cr+cw == 0 {
		out = 0
	}
	return `{"type":"assistant","uuid":"u-` + id + `","timestamp":"2026-11-18T10:00:00.5Z","message":{"id":"` + id +
		`","usage":{"input_tokens":` + itoa(in) + `,"cache_read_input_tokens":` + itoa(cr) +
		`,"cache_creation_input_tokens":` + itoa(cw) + `,"output_tokens":` + itoa(out) + `},"content":[` + strings.Join(blocks, ",") + `]}}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func toolUse(id, name, input string) string {
	return `{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}`
}

func userResult(id, content string, isErr bool) string {
	e := ""
	if isErr {
		e = `,"is_error":true`
	}
	return `{"type":"user","uuid":"ur-` + id + `","timestamp":"2026-11-18T10:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"` +
		id + `","content":` + content + e + `}]}}`
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, p, content string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func hookLine(uuid, event, cmd, stdout string) string {
	return `{"type":"attachment","uuid":"` + uuid + `","timestamp":"2026-11-18T10:00:02Z","attachment":{"type":"hook_success","hookEvent":"` +
		event + `","command":` + quote(cmd) + `,"stdout":` + quote(stdout) + `}}`
}

const compactLine = `{"type":"system","subtype":"compact_boundary","uuid":"c1","timestamp":"2026-11-18T10:00:03Z"}`

var zero = ContextState{LastTurn: -1}

func TestParseContext_Basic(t *testing.T) {
	input := `{"command":"git status -s"}`
	p := writeLines(t,
		asst("m1", 10, 100, 5, toolUse("t1", "Bash", input)),
		userResult("t1", quote(strings.Repeat("a", 400)), false),
		asst("m2", 1, 200, 0),
	)
	outs, turns, _, st, reset, err := ParseContext(p, 0, zero)
	if err != nil || reset {
		t.Fatal(err, reset)
	}
	if len(outs) != 1 {
		t.Fatalf("outs %+v", outs)
	}
	o := outs[0]
	if o.ItemID != "t1" || o.Tool != "Bash" || o.Key != "git status" || o.Bytes != 400 || o.Tokens != 100 ||
		o.InTokens != EstimateTokens(int64(len(input)), 0) || o.Turn != 0 || o.Segment != 0 || o.IsError || !o.Result || o.At.IsZero() {
		t.Fatalf("out %+v", o)
	}
	if len(turns) != 2 || turns[0].Turn != 0 || turns[0].MessageID != "m1" || turns[0].Tokens != 115 ||
		turns[1].Turn != 1 || turns[1].MessageID != "m2" || turns[1].Tokens != 201 {
		t.Fatalf("turns %+v", turns)
	}
	if st.LastTurn != 1 || st.LastMessageID != "m2" {
		t.Fatalf("state %+v", st)
	}
}

func TestParseContext_ZeroTokenMessage(t *testing.T) {
	p := writeLines(t, asst("m1", 1, 1, 1), asst("m0", 0, 0, 0), asst("m2", 2, 2, 2))
	_, turns, _, _, _, _ := ParseContext(p, 0, zero)
	if len(turns) != 2 || turns[0].MessageID != "m1" || turns[1].MessageID != "m2" || turns[1].Turn != 1 {
		t.Fatalf("turns %+v", turns)
	}
}

func TestParseContext_ListContentAndImage(t *testing.T) {
	c := `[{"type":"text","text":"12345678"},{"type":"image","source":{}},{"type":"tool_reference","tool_name":"x"}]`
	p := writeLines(t, asst("m1", 1, 1, 1, toolUse("t1", "Read", `{"file_path":"/a/b.go"}`)), userResult("t1", c, true))
	outs, _, _, _, _, _ := ParseContext(p, 0, zero)
	if len(outs) != 1 || outs[0].Bytes != 8 || outs[0].Tokens != 1602 || !outs[0].IsError {
		t.Fatalf("outs %+v", outs)
	}
}

func TestParseContext_SplitMessageAcrossReads(t *testing.T) {
	l1 := asst("m1", 1, 1, 1)
	l2 := asst("m1", 10, 20, 30)
	p := writeLines(t, l1, l2)
	// primeira leitura só vê a primeira linha
	mustWrite(t, p, l1+"\n")
	_, turns, next, st, _, _ := ParseContext(p, 0, zero)
	if len(turns) != 1 || turns[0].Tokens != 3 {
		t.Fatalf("turns %+v", turns)
	}
	appendTo(t, p, l2+"\n")
	_, turns, _, st, _, _ = ParseContext(p, next, st)
	if len(turns) != 1 || turns[0].Turn != 0 || turns[0].Tokens != 60 || st.LastTurn != 0 {
		t.Fatalf("turns %+v st %+v", turns, st)
	}
}

func TestParseContext_Compaction(t *testing.T) {
	hook := hookLine("h1", "SessionStart", "/x/nobre hook claude SessionStart", `{"hookSpecificOutput":{"additionalContext":"abcd"}}`)
	p := writeLines(t, asst("m1", 1, 1, 1), compactLine, hook, asst("m2", 2, 2, 2))
	outs, turns, _, st, _, _ := ParseContext(p, 0, zero)
	if len(outs) != 1 || outs[0].Turn != 0 || outs[0].Segment != 1 {
		t.Fatalf("outs %+v", outs)
	}
	if turns[0].Segment != 0 || turns[1].Segment != 1 || turns[1].Turn != 1 || st.Segment != 1 {
		t.Fatalf("turns %+v st %+v", turns, st)
	}
	// compact_boundary como última linha de uma leitura
	p2 := writeLines(t, asst("m1", 1, 1, 1), compactLine)
	_, _, next, st, _, _ := ParseContext(p2, 0, zero)
	if st.Segment != 1 {
		t.Fatalf("st %+v", st)
	}
	appendTo(t, p2, asst("m2", 2, 2, 2)+"\n")
	_, turns, _, _, _, _ = ParseContext(p2, next, st)
	if len(turns) != 1 || turns[0].Segment != 1 {
		t.Fatalf("turns %+v", turns)
	}
}

func TestParseContext_Hooks(t *testing.T) {
	ctx40 := strings.Repeat("x", 40)
	blocking := `{"type":"attachment","uuid":"b1","timestamp":"2026-11-18T10:00:02Z","attachment":{"type":"hook_blocking_error","hookEvent":"Stop","blockingError":{"blockingError":"` + strings.Repeat("y", 30) + `"}}}`
	blockingStr := `{"type":"attachment","uuid":"b2","timestamp":"2026-11-18T10:00:02Z","attachment":{"type":"hook_blocking_error","hookEvent":"Stop","blockingError":"` + strings.Repeat("z", 12) + `"}}`
	p := writeLines(t,
		hookLine("h1", "SessionStart", "/x/nobre hook claude SessionStart", `{"hookSpecificOutput":{"additionalContext":"`+ctx40+`"}}`),
		hookLine("h2", "SessionStart", "/x/other.sh", "texto puro"),
		hookLine("h3", "Stop", "/x/other.sh", "texto puro"),
		hookLine("h4", "PostToolUse", "/x/other.sh", "texto puro"),
		hookLine("h5", "SessionStart", "/x/other.sh", ""),
		blocking, blockingStr,
		hookLine("h6", "UserPromptSubmit", "/x/other.sh", "abcde"),
		"{isto nao e json",
	)
	outs, _, _, _, _, _ := ParseContext(p, 0, zero)
	if len(outs) != 5 {
		t.Fatalf("outs %+v", outs)
	}
	want := []struct {
		id, key string
		bytes   int64
	}{{"h1", "SessionStart · cockpit", 40}, {"h2", "SessionStart · outros", 10}, {"b1", "Stop · bloqueio", 30}, {"b2", "Stop · bloqueio", 12}, {"h6", "UserPromptSubmit · outros", 5}}
	for i, w := range want {
		o := outs[i]
		if o.ItemID != w.id || o.Tool != "hook" || o.Key != w.key || o.Bytes != w.bytes || o.InTokens != 0 || o.Turn != -1 || !o.Result || o.Tokens != EstimateTokens(w.bytes, 0) {
			t.Fatalf("out %d %+v", i, o)
		}
	}
}

func TestParseContext_Keys(t *testing.T) {
	p := writeLines(t,
		asst("m1", 1, 1, 1,
			toolUse("a", "Read", `{"file_path":"/a/b.go"}`),
			toolUse("b", "Read", `{"file_path":"/a/Makefile"}`),
			toolUse("c", "Agent", `{"subagent_type":"Explore","prompt":"x"}`),
			toolUse("d", "Task", `{"prompt":"x"}`),
			toolUse("e", "Skill", `{"skill":"napkin"}`),
			toolUse("f", "Grep", `{"pattern":"x"}`)),
		userResult("a", `"x"`, false), userResult("b", `"x"`, false), userResult("c", `"x"`, false),
		userResult("d", `"x"`, false), userResult("e", `"x"`, false), userResult("f", `"x"`, false),
		userResult("zzz", `"x"`, false),
	)
	outs, _, _, _, _, _ := ParseContext(p, 0, zero)
	want := []string{"Read .go", "Read sem extensão", "Agent Explore", "Agent geral", "Skill napkin", "Grep", ""}
	if len(outs) != len(want) {
		t.Fatalf("outs %+v", outs)
	}
	for i, w := range want {
		if outs[i].Key != w {
			t.Errorf("%d: %q != %q", i, outs[i].Key, w)
		}
	}
	if outs[6].Tool != "" || !outs[6].Result || outs[6].InTokens != 0 {
		t.Errorf("desconhecido %+v", outs[6])
	}
}

func TestParseContext_Truncated(t *testing.T) {
	p := writeLines(t, asst("m1", 1, 1, 1))
	_, turns, _, _, reset, _ := ParseContext(p, 1<<30, ContextState{LastTurn: 5, LastMessageID: "old", Segment: 3})
	if !reset || len(turns) != 1 || turns[0].Turn != 0 || turns[0].Segment != 0 {
		t.Fatalf("reset %v turns %+v", reset, turns)
	}
}

func TestParseContext_PartialLastLine(t *testing.T) {
	l1 := asst("m1", 1, 1, 1)
	p := filepath.Join(t.TempDir(), "t.jsonl")
	mustWrite(t, p, l1+"\n"+asst("m2", 2, 2, 2))
	_, turns, next, _, _, _ := ParseContext(p, 0, zero)
	if len(turns) != 1 || next != int64(len(l1)+1) {
		t.Fatalf("turns %+v next %d", turns, next)
	}
}

func TestBashKey(t *testing.T) {
	cases := map[string]string{
		"git status -s":                                 "git status",
		"cd /x && go test ./...":                        "go test",
		"FOO=1 BAR=2 pnpm test --run":                   "pnpm test",
		"cat secret.txt":                                "cat",
		"/usr/bin/python3 -I x.py":                      "python3",
		`curl -H "Authorization: Bearer abc" https://x`: "curl",
		"export TOKEN=abc && psql -c 'select'":          "psql",
		"git --no-pager log":                            "git",
		"ls | grep x":                                   "ls",
		"":                                              "outro",
		"$(weird)":                                      "outro",
		"make update":                                   "make update",
		`nobre note "x"`:                                "nobre note",
		"sudo systemctl restart x":                      "systemctl restart",
		"env A=1 go build":                              "go build",
		"time go test ./...":                            "go test",
		"timeout 30 pnpm test":                          "pnpm test",
		"rtk git status":                                "git status",
		"nohup nice make update":                        "make update",
		"cd /x\ngo test ./...":                          "go test",
		"git\nfoo":                                      "git",
		"sudo -u root systemctl restart x":              "systemctl restart",
		"timeout -s KILL 30 go test":                    "go test",
		"env -i A=1 go build":                           "go build",
		"env -u HOME go build":                          "go build",
		"-x foo":                                        "outro",
		"S=/tmp/x && go test ./...":                     "go test",
		"cd /x && W=a && git status":                    "git status",
		"D=origin/devel\ngit log":                       "git log",
		`GIT_SSH_COMMAND="ssh -i k" git push`:           "git push",
		"( cd x && make test )":                         "make test",
		"echo 'a;b' | wc":                               "echo",
		"A=1":                                           "outro",
	}
	for in, want := range cases {
		if got := BashKey(in); got != want {
			t.Errorf("BashKey(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestParseContext_UseAndResultAcrossReads(t *testing.T) {
	input := `{"command":"git status -s"}`
	l1 := asst("m1", 1, 1, 1, toolUse("t1", "Bash", input))
	p := filepath.Join(t.TempDir(), "t.jsonl")
	mustWrite(t, p, l1+"\n")
	outs, _, next, st, _, _ := ParseContext(p, 0, zero)
	if len(outs) != 1 || outs[0].Result || outs[0].Tool != "Bash" || outs[0].Key != "git status" ||
		outs[0].InTokens == 0 || outs[0].Bytes != 0 || outs[0].Turn != 0 {
		t.Fatalf("outs %+v", outs)
	}
	appendTo(t, p, userResult("t1", quote("abcdefgh"), false)+"\n")
	outs, _, _, _, _, _ = ParseContext(p, next, st)
	if len(outs) != 1 || outs[0].ItemID != "t1" || outs[0].Tool != "" || !outs[0].Result || outs[0].Bytes != 8 || outs[0].Turn != 0 {
		t.Fatalf("outs %+v", outs)
	}
}

func TestParseContext_UseWithoutResult(t *testing.T) {
	p := writeLines(t, asst("m1", 1, 1, 1, toolUse("t1", "Write", `{"file_path":"/a","content":"xxxxxxxx"}`)))
	outs, _, _, _, _, _ := ParseContext(p, 0, zero)
	if len(outs) != 1 || outs[0].Result || outs[0].InTokens == 0 || outs[0].Tokens != 0 || outs[0].Key != "Write" {
		t.Fatalf("outs %+v", outs)
	}
}

func TestParseContext_EmptyIDsSkipped(t *testing.T) {
	p := writeLines(t,
		asst("m1", 1, 1, 1, toolUse("", "Bash", `{"command":"ls"}`)),
		userResult("", `"abc"`, false),
		hookLine("", "SessionStart", "/x/nobre hook", `{"hookSpecificOutput":{"additionalContext":"abcd"}}`),
	)
	outs, _, _, _, _, _ := ParseContext(p, 0, zero)
	if len(outs) != 0 {
		t.Fatalf("outs %+v", outs)
	}
}
