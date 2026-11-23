package kiro

import (
	"encoding/json"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
)

func TestRender(t *testing.T) {
	a := New(nil, adapterDeps())
	render := func(r agentcore.Reply) adapter.HookReply {
		t.Helper()
		out, err := a.Render(agentcore.Event{Kind: agentcore.Prompt}, r)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Contexto: texto puro no stdout, os vazios fora, juntos por linha em branco.
	if got := render(agentcore.Reply{Context: []string{"", "um <a> & b", "", "dois"}}); got != (adapter.HookReply{Stdout: "um <a> & b\n\ndois"}) {
		t.Fatalf("contexto = %+v", got)
	}
	if got := render(agentcore.Reply{Context: []string{"", ""}}); got != (adapter.HookReply{}) {
		t.Fatalf("contexto vazio = %+v", got)
	}

	// Bloqueio da ferramenta: código 2, motivo no stderr.
	if got := render(agentcore.Reply{Block: &agentcore.Decision{Reason: "limite diário"}, Context: []string{"x"}}); got != (adapter.HookReply{Stderr: "limite diário", Exit: 2}) {
		t.Fatalf("bloqueio = %+v", got)
	}

	// Segurar o turno: JSON de decisão no stdout, código 0 (conferido ao vivo
	// com o kiro-cli 3.5.0: o agente continua o turno com o motivo).
	got := render(agentcore.Reply{HoldTurn: &agentcore.Decision{Reason: "Antes de encerrar, atualize o card <NAC-1> & cia"}})
	if got.Exit != 0 || got.Stderr != "" {
		t.Fatalf("segurar = %+v", got)
	}
	if want := `{"decision":"block","reason":"Antes de encerrar, atualize o card <NAC-1> & cia"}`; got.Stdout != want {
		t.Fatalf("segurar stdout = %s, quer %s", got.Stdout, want)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(got.Stdout), &m); err != nil || m["decision"] != "block" {
		t.Fatalf("segurar não é JSON: %v", err)
	}

	// Saída pronta passa direto.
	raw := adapter.HookReply{Stdout: "x", Exit: 1}
	if got := render(agentcore.Reply{Raw: &raw, Context: []string{"y"}}); got != raw {
		t.Fatalf("raw = %+v", got)
	}
}
