package agents

import (
	"context"
	"reflect"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

type fake struct {
	name  string
	procs []string
}

func (f fake) Name() string           { return f.name }
func (f fake) ProcessNames() []string { return f.procs }
func (f fake) HandleHook(context.Context, adapter.HookCall) (adapter.HookReply, error) {
	return adapter.HookReply{}, nil
}
func (f fake) Detect() adapter.Detection                        { return adapter.Detection{} }
func (f fake) Install() (adapter.InstallReport, error)          { return adapter.InstallReport{}, nil }
func (f fake) Binary() string                                   { return "" }
func (f fake) LaunchCommand(bin, initialPrompt string) []string { return nil }

func TestRegistroNaOrdemDeRegistro(t *testing.T) {
	r := New(fake{"kiro", []string{"kiro-cli"}}, fake{"claude", []string{"claude"}})
	var nomes []string
	for _, a := range r.All() {
		nomes = append(nomes, a.Name())
	}
	if !reflect.DeepEqual(nomes, []string{"kiro", "claude"}) {
		t.Fatalf("ordem = %v", nomes)
	}
	if a, ok := r.Get("claude"); !ok || a.Name() != "claude" {
		t.Fatalf("Get(claude) = %v, %v", a, ok)
	}
	if _, ok := r.Get("codex"); ok {
		t.Fatal("codex não está registrado")
	}
}

func TestNomesDeProcesso(t *testing.T) {
	r := New(fake{"claude", []string{"claude"}}, fake{"kiro", []string{"kiro-cli", "kiro-cli-chat"}})
	want := map[string][]string{"claude": {"claude"}, "kiro": {"kiro-cli", "kiro-cli-chat"}}
	if got := r.ProcessNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ProcessNames = %v", got)
	}
	if got := r.AllProcessNames(); !reflect.DeepEqual(got, []string{"claude", "kiro-cli", "kiro-cli-chat"}) {
		t.Fatalf("AllProcessNames = %v", got)
	}
}

func TestRegistroNilEVazio(t *testing.T) {
	var r *Registry
	if _, ok := r.Get("claude"); ok || len(r.All()) != 0 || len(r.AllProcessNames()) != 0 {
		t.Fatal("registro nil deve se comportar como vazio")
	}
}

func TestNomesExibidos(t *testing.T) {
	if !reflect.DeepEqual(Known, []string{"claude", "codex", "kiro"}) {
		t.Fatalf("Known = %v", Known)
	}
	for k, v := range map[string]string{"claude": "Claude Code", "codex": "Codex", "kiro": "Kiro", "outro": "outro"} {
		if got := DisplayName(k); got != v {
			t.Errorf("DisplayName(%s) = %q", k, got)
		}
	}
}

func TestStaticTemOsDriversCompilados(t *testing.T) {
	if got := Static(); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("Static = %v", got)
	}
}
