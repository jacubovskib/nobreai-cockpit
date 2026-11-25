// Package contracttest é a bateria de contrato dos drivers de agente: o que
// todo agente (Claude, Kiro...) tem de fazer com os hooks, observado pelo
// banco e pelas respostas decodificadas, nunca pelo formato do agente. Cada
// driver roda a mesma bateria com os payloads e os decodificadores dele.
//
// Como plugar um driver novo: no _test.go do pacote do adaptador,
//
//	contracttest.Run(t, func(st *store.Store, deps adapter.Deps) adapter.Adapter {
//		return meuagente.New(st, deps)
//	}, contracttest.Fixtures{...})
//
// A fábrica devolve o driver pronto para os hooks: o que depende da máquina
// tem de ser configurado nela, a verificação de processo vivo inclusive (no
// Claude, SetAlive): a bateria usa PIDs falsos (4242), que não existem no
// /proc de quem roda os testes.
//
// Os payloads usam três marcadores, trocados pela bateria antes de cada
// chamada: {{session}} (id da sessão no agente), {{cwd}} (diretório do
// projeto cadastrado) e {{transcript}} (caminho de um arquivo temporário).
package contracttest

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const (
	// SessionID e Cwd são os valores que a bateria injeta nos marcadores.
	SessionID = "sess_contract0001"
	Cwd       = "/w/api"

	agentPID = 4242
)

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// Fixtures são os payloads de cada hook do agente e como ler as respostas.
// Os payloads são os bytes que o agente manda ao `nobre hook`, com os
// marcadores {{session}}, {{cwd}} e {{transcript}}. O nome do evento, quando
// o agente o manda fora do payload, a bateria passa em HookCall.Event.
type Fixtures struct {
	Start, Prompt, ReadAfter, EditAfter, CommandAfter, Stop, End []byte
	// Events são os nomes dos eventos do agente (HookCall.Event) de cada
	// payload acima; vazio = o driver os acha no payload.
	Events EventNames
	// PromptText é o texto do usuário em Prompt.
	PromptText string
	// ReadPath e EditPath são os caminhos que os payloads de leitura e edição
	// citam, relativos ao projeto ({{cwd}}/<caminho> no payload); Command é a
	// linha de comando de CommandAfter.
	ReadPath, EditPath, Command string
	// DecodeContext devolve os textos de contexto que a resposta entrega ao
	// agente (vazio se não há).
	DecodeContext func(adapter.HookReply) []string
	// DecodeHold diz se a resposta segura o fim do turno e o motivo. nil: o
	// agente não segura o turno e o cenário é pulado (Claude e Kiro seguram;
	// o núcleo ainda não tem como entregar o texto no próximo prompt).
	DecodeHold func(adapter.HookReply) (string, bool)
}

// EventNames são os nomes dos eventos do agente de cada payload.
type EventNames struct{ Start, Prompt, ReadAfter, EditAfter, CommandAfter, Stop, End string }

// harness reúne o adaptador, o banco e o relógio de um cenário.
type harness struct {
	t          *testing.T
	st         *store.Store
	a          adapter.Adapter
	fx         Fixtures
	now        time.Time
	transcript string
	sid        string // id da sessão no Cockpit
	project    domain.Project
}

// Run roda a bateria de contrato contra o driver criado por newAdapter.
func Run(t *testing.T, newAdapter func(st *store.Store, deps adapter.Deps) adapter.Adapter, fx Fixtures) {
	t.Helper()
	if fx.DecodeContext == nil {
		t.Fatal("contracttest: Fixtures.DecodeContext é obrigatório")
	}
	setup := func(t *testing.T) *harness {
		t.Helper()
		st, err := store.Open(filepath.Join(t.TempDir(), "cockpit.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		h := &harness{t: t, st: st, fx: fx, now: t0, transcript: filepath.Join(t.TempDir(), "transcript.jsonl")}
		h.a = newAdapter(st, adapter.Deps{Now: func() time.Time { return h.now }, Git: fakeGit{}})
		if w, ok := h.a.(interface{ Wait() }); ok {
			// Cleanup roda ao contrário: espera a leitura antes de fechar o banco.
			t.Cleanup(w.Wait)
		}
		h.sid = h.a.Name() + ":" + SessionID
		if h.project, err = st.CreateProject("api", Cwd); err != nil {
			t.Fatal(err)
		}
		return h
	}

	t.Run("sessao nasce no Start", func(t *testing.T) {
		h := setup(t)
		reply := h.call(fx.Events.Start, fx.Start, nil)
		sess := h.session()
		if sess.Agent != h.a.Name() || sess.ProviderSessionID != SessionID || sess.Cwd != Cwd ||
			!sess.FullCapture || sess.ProjectID == nil || sess.EndedAt != nil || sess.CardID != nil {
			t.Fatalf("sessão = %+v", sess)
		}
		if sess.PID != agentPID {
			t.Fatalf("PID = %d, quer %d", sess.PID, agentPID)
		}
		if got := h.events(domain.EvSessionStart); len(got) != 1 {
			t.Fatalf("session_start = %d eventos", len(got))
		}
		// A sessão registrada é avisada ao agente.
		ctx := strings.Join(fx.DecodeContext(reply), "\n")
		if !strings.Contains(ctx, agentcore.RegisteredPhrase) {
			t.Fatalf("contexto sem a frase de sessão registrada:\n%s", ctx)
		}
	})

	t.Run("NOBRE_RESUME liga ao card e traz o pacote de retomada", func(t *testing.T) {
		h := setup(t)
		p := h.project
		c, err := h.st.CreateCard(domain.Card{Title: "Migrar OAuth2", ProjectID: p.ID, Branch: "main",
			Column: domain.Pausado, WhereStopped: "falta o teste do refresh"})
		if err != nil {
			t.Fatal(err)
		}
		reply := h.call(fx.Events.Start, fx.Start, map[string]string{"NOBRE_RESUME": c.Key})
		sess := h.session()
		if sess.CardID == nil || *sess.CardID != c.ID {
			t.Fatalf("sessão não ligada ao card: %+v", sess)
		}
		if got, _ := h.st.GetCard(c.Key); got.Column != domain.EmAndamento {
			t.Fatalf("coluna do card = %s", got.Column)
		}
		ctx := strings.Join(fx.DecodeContext(reply), "\n")
		if !strings.Contains(ctx, "## Onde parei") || !strings.Contains(ctx, "falta o teste do refresh") {
			t.Fatalf("contexto sem o pacote de retomada:\n%s", ctx)
		}
	})

	t.Run("prompt registrado", func(t *testing.T) {
		h := setup(t)
		h.call(fx.Events.Start, fx.Start, nil)
		h.call(fx.Events.Prompt, fx.Prompt, nil)
		got := h.events(domain.EvPrompt)
		if len(got) != 1 {
			t.Fatalf("prompt = %d eventos", len(got))
		}
		if text := dataString(t, got[0], "text"); text != fx.PromptText {
			t.Fatalf("texto do prompt = %q, quer %q", text, fx.PromptText)
		}
	})

	t.Run("leitura edicao e comando viram eventos", func(t *testing.T) {
		h := setup(t)
		h.call(fx.Events.Start, fx.Start, nil)
		h.call(fx.Events.ReadAfter, fx.ReadAfter, nil)
		h.call(fx.Events.EditAfter, fx.EditAfter, nil)
		h.call(fx.Events.CommandAfter, fx.CommandAfter, nil)
		// Caminho relativo à raiz do projeto, nunca absoluto.
		if got := h.events(domain.EvFileRead); len(got) != 1 || dataString(t, got[0], "path") != fx.ReadPath {
			t.Fatalf("file_read = %v, quer %q", got, fx.ReadPath)
		}
		if got := h.events(domain.EvFileEdit); len(got) != 1 || dataString(t, got[0], "path") != fx.EditPath {
			t.Fatalf("file_edit = %v, quer %q", got, fx.EditPath)
		}
		if got := h.events(domain.EvCommand); len(got) != 1 || dataString(t, got[0], "command") != fx.Command {
			t.Fatalf("command = %v, quer %q", got, fx.Command)
		}
	})

	t.Run("Stop com handoff pendente segura o turno", func(t *testing.T) {
		if fx.DecodeHold == nil {
			t.Skip("driver sem como segurar o turno: o núcleo ainda não entrega o texto do handoff no próximo prompt")
		}
		h := setup(t)
		p := h.project
		at := t0
		c, err := h.st.CreateCard(domain.Card{ProjectID: p.ID, Title: "Migrar OAuth2", Branch: "main",
			WhereStopped: "lendo o oauth.go", WhereStoppedAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		h.call(fx.Events.Start, fx.Start, map[string]string{"NOBRE_RESUME": c.Key})
		// Sem edição depois do "Onde parei", o turno termina em paz.
		h.now = t0.Add(time.Minute)
		if reason, held := fx.DecodeHold(h.call(fx.Events.Stop, fx.Stop, nil)); held {
			t.Fatalf("Stop sem edição segurou o turno: %q", reason)
		}
		h.call(fx.Events.EditAfter, fx.EditAfter, nil)
		h.now = t0.Add(2 * time.Minute)
		reason, held := fx.DecodeHold(h.call(fx.Events.Stop, fx.Stop, nil))
		if !held || !strings.Contains(reason, "Antes de encerrar, atualize o card "+c.Key) {
			t.Fatalf("Stop com handoff pendente: held=%v reason=%q", held, reason)
		}
	})

	t.Run("Stop cobra plano escrito sem registro", func(t *testing.T) {
		if fx.DecodeHold == nil {
			t.Skip("driver sem como segurar o turno: o núcleo ainda não entrega o texto do lembrete no próximo prompt")
		}
		h := setup(t)
		h.planPatterns(fx.EditPath)
		at := t0
		c, err := h.st.CreateCard(domain.Card{ProjectID: h.project.ID, Title: "Migrar OAuth2", Branch: "main",
			WhereStopped: "lendo o oauth.go", WhereStoppedAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		h.call(fx.Events.Start, fx.Start, map[string]string{"NOBRE_RESUME": c.Key})
		h.now = t0.Add(time.Minute)
		h.call(fx.Events.EditAfter, fx.EditAfter, nil)
		h.now = t0.Add(2 * time.Minute)
		reason, held := fx.DecodeHold(h.call(fx.Events.Stop, fx.Stop, nil))
		if !held || !strings.Contains(reason, "nobre plan") {
			t.Fatalf("Stop com plano sem registro: held=%v reason=%q", held, reason)
		}
	})

	t.Run("Stop não cobra o plano de outro card da sessão", func(t *testing.T) {
		if fx.DecodeHold == nil {
			t.Skip("driver sem como segurar o turno: o núcleo ainda não entrega o texto do lembrete no próximo prompt")
		}
		h := setup(t)
		h.planPatterns(fx.EditPath)
		at := t0
		a, err := h.st.CreateCard(domain.Card{ProjectID: h.project.ID, Title: "Visão por projeto", Branch: "main",
			WhereStopped: "plano escrito", WhereStoppedAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		b, err := h.st.CreateCard(domain.Card{ProjectID: h.project.ID, Title: "Rodar no macOS", Branch: "main",
			WhereStopped: "começando", WhereStoppedAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		h.call(fx.Events.Start, fx.Start, map[string]string{"NOBRE_RESUME": a.Key})
		h.now = t0.Add(time.Minute)
		h.call(fx.Events.EditAfter, fx.EditAfter, nil) // o plano é do card A
		if err := h.st.AttachSession(h.session().ID, b.Key, true); err != nil {
			t.Fatal(err)
		}
		h.now = t0.Add(2 * time.Minute)
		if reason, held := fx.DecodeHold(h.call(fx.Events.Stop, fx.Stop, nil)); held && strings.Contains(reason, "nobre plan") {
			t.Fatalf("o card %s foi cobrado pelo plano escrito no %s: %q", b.Key, a.Key, reason)
		}
	})

	t.Run("Stop cobra o plano do card novo depois de cobrar o do anterior", func(t *testing.T) {
		if fx.DecodeHold == nil {
			t.Skip("driver sem como segurar o turno: o núcleo ainda não entrega o texto do lembrete no próximo prompt")
		}
		h := setup(t)
		h.planPatterns(fx.EditPath)
		at := t0
		var cards []domain.Card
		for _, title := range []string{"Card A", "Card B"} {
			c, err := h.st.CreateCard(domain.Card{ProjectID: h.project.ID, Title: title, Branch: "main",
				WhereStopped: "andando", WhereStoppedAt: &at})
			if err != nil {
				t.Fatal(err)
			}
			cards = append(cards, c)
		}
		h.call(fx.Events.Start, fx.Start, map[string]string{"NOBRE_RESUME": cards[0].Key})
		for i, c := range cards {
			if i > 0 {
				if err := h.st.AttachSession(h.session().ID, c.Key, true); err != nil {
					t.Fatal(err)
				}
			}
			h.now = t0.Add(time.Duration(2*i+1) * time.Minute)
			h.call(fx.Events.EditAfter, fx.EditAfter, nil)
			h.now = t0.Add(time.Duration(2*i+2) * time.Minute)
			reason, held := fx.DecodeHold(h.call(fx.Events.Stop, fx.Stop, nil))
			if !held || !strings.Contains(reason, "o card "+c.Key+" ainda não tem as tarefas") {
				t.Fatalf("plano escrito no %s: held=%v reason=%q", c.Key, held, reason)
			}
		}
	})

	t.Run("Stop sem card cita o plano", func(t *testing.T) {
		if fx.DecodeHold == nil {
			t.Skip("driver sem como segurar o turno: o núcleo ainda não entrega o texto do lembrete no próximo prompt")
		}
		h := setup(t)
		h.planPatterns(fx.EditPath)
		h.call(fx.Events.Start, fx.Start, nil)
		h.now = t0.Add(time.Minute)
		h.call(fx.Events.EditAfter, fx.EditAfter, nil)
		reason, held := fx.DecodeHold(h.call(fx.Events.Stop, fx.Stop, nil))
		if !held || !strings.Contains(reason, "nobre plan") {
			t.Fatalf("Stop sem card com plano: held=%v reason=%q", held, reason)
		}
	})

	t.Run("End encerra a sessao", func(t *testing.T) {
		h := setup(t)
		h.call(fx.Events.Start, fx.Start, nil)
		h.now = t0.Add(time.Minute)
		h.call(fx.Events.End, fx.End, nil)
		sess := h.session()
		if sess.EndedAt == nil || sess.EndReason == "" {
			t.Fatalf("sessão não encerrada: %+v", sess)
		}
		if got := h.events(domain.EvSessionEnd); len(got) != 1 {
			t.Fatalf("session_end = %d eventos", len(got))
		}
	})
}

// planPatterns grava os padrões de plano nas configurações do banco do cenário.
func (h *harness) planPatterns(patterns ...string) {
	h.t.Helper()
	set, err := h.st.GetSettings()
	if err != nil {
		h.t.Fatal(err)
	}
	set.PlanPatterns = patterns
	if err := h.st.PutSettings(set); err != nil {
		h.t.Fatal(err)
	}
}

// call manda o hook ao adaptador e devolve a resposta.
func (h *harness) call(event string, payload []byte, env map[string]string) adapter.HookReply {
	h.t.Helper()
	if len(payload) == 0 {
		h.t.Fatalf("contracttest: payload vazio para o evento %q", event)
	}
	body := strings.NewReplacer("{{session}}", SessionID, "{{cwd}}", Cwd, "{{transcript}}", h.transcript).Replace(string(payload))
	reply, err := h.a.HandleHook(context.Background(), adapter.HookCall{
		Event: event, Payload: json.RawMessage(body), AgentPID: agentPID, Env: env,
	})
	if err != nil {
		h.t.Fatalf("hook %q: %v", event, err)
	}
	return reply
}

func (h *harness) session() domain.Session {
	h.t.Helper()
	s, err := h.st.GetSession(h.sid)
	if err != nil {
		h.t.Fatalf("sessão %s: %v", h.sid, err)
	}
	return s
}

// events devolve os eventos da sessão de um tipo, do mais antigo ao mais novo.
func (h *harness) events(kind string) []domain.Event {
	h.t.Helper()
	all, err := h.st.SessionEvents([]string{h.sid}, 1000)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []domain.Event
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Kind == kind {
			out = append(out, all[i])
		}
	}
	return out
}

func dataString(t *testing.T, e domain.Event, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.Data, &m); err != nil {
		t.Fatalf("dados do evento %s: %v", e.Kind, err)
	}
	s, _ := m[key].(string)
	return s
}

// fakeGit responde um repositório limpo no branch main.
type fakeGit struct{}

func (fakeGit) Branch(string) string           { return "main" }
func (fakeGit) Head(string) string             { return "abc123" }
func (fakeGit) DiffStat(string, string) string { return "" }
func (fakeGit) Clean(string) bool              { return true }
func (fakeGit) Checkout(string, string) error  { return nil }
