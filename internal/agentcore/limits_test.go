package agentcore

import (
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// creditsDriver mede o gasto em créditos (SpendUnit): limites em US$ não
// valem para ele.
type creditsDriver struct{ fakeDriver }

func (d *creditsDriver) SpendsCredits() bool { return true }

// overLimit passa do limite diário de US$ 1,00 do agente "fake" (US$ 2,00
// gastos hoje pela sessão fakeID, que já tem de existir).
func (e *env) overLimit(t *testing.T) {
	t.Helper()
	if err := e.st.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.PutLimit(domain.Limit{Agent: "fake", DailyUSD: 1, WarnPct: 80, Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.UpsertUsage([]domain.UsageRow{{SessionID: fakeID, MessageID: "x", Model: "m", Input: 2_000_000, At: t0}}); err != nil {
		t.Fatal(err)
	}
}

func lastReply(t *testing.T, d *fakeDriver) Reply {
	t.Helper()
	if len(d.replies) == 0 {
		t.Fatal("nenhuma resposta")
	}
	return d.replies[len(d.replies)-1]
}

func TestToolBeforeBlockedByUSDLimit(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	cmd := Tool{Kind: ToolCommand, Name: "shell", Command: "make test"}
	e.handle(t, d, Event{Kind: ToolBefore, Tool: cmd})
	if r := lastReply(t, d); r.Block != nil {
		t.Fatalf("sem limite bloqueou: %+v", r.Block)
	}
	e.overLimit(t)
	e.handle(t, d, Event{Kind: ToolBefore, Tool: cmd})
	want := "NobreAI Cockpit: limite de custo atingido (diário: US$ 2,00 de US$ 1,00). Registre “Onde parei” com nobre note 'texto' (uma chamada só, texto entre aspas simples, sem $, crase, heredoc ou redirecionamento) e encerre."
	if r := lastReply(t, d); r.Block == nil || r.Block.Reason != want {
		t.Fatalf("bloqueio = %+v", r.Block)
	}
	// Uma chamada simples de handoff do nobre passa; encadeada, não.
	e.handle(t, d, Event{Kind: ToolBefore, Tool: Tool{Kind: ToolCommand, Command: "nobre note 'lendo o oauth'"}})
	if r := lastReply(t, d); r.Block != nil {
		t.Fatalf("nobre note bloqueado: %+v", r.Block)
	}
	e.handle(t, d, Event{Kind: ToolBefore, Tool: Tool{Kind: ToolCommand, Command: "nobre status && make test"}})
	if r := lastReply(t, d); r.Block == nil {
		t.Fatal("nobre encadeado furou o limite")
	}
}

func TestToolBeforeCreditsDriverIgnoresUSDLimit(t *testing.T) {
	d := &creditsDriver{}
	e := newEnv(t, d)
	e.project(t)
	e.handle(t, &d.fakeDriver, Event{Kind: Started})
	e.overLimit(t)
	e.handle(t, &d.fakeDriver, Event{Kind: ToolBefore, Tool: Tool{Kind: ToolCommand, Command: "make test"}})
	if r := lastReply(t, &d.fakeDriver); r.Block != nil {
		t.Fatalf("driver em créditos bloqueado por limite em US$: %+v", r.Block)
	}
}

// limitFlow liga a sessão a um card, passa do limite com ação "pause" e
// tenta uma ferramenta (pausa); depois sobe o limite e tenta de novo (retoma).
// Devolve a coluna do card depois de cada tentativa e os eventos de pausa.
func limitFlow(t *testing.T, e *env, d *fakeDriver, prePause bool) (afterOver, afterUnder domain.Column, pauses int) {
	t.Helper()
	c := e.cardSession(t, d)
	if got, _ := e.st.GetCard(c.Key); got.Column != domain.EmAndamento {
		t.Fatalf("card fora de em andamento: %s", got.Column)
	}
	e.overLimit(t)
	cmd := Tool{Kind: ToolCommand, Command: "make test"}
	e.handle(t, d, Event{Kind: ToolBefore, Tool: cmd})
	got, _ := e.st.GetCard(c.Key)
	afterOver = got.Column
	if prePause && afterOver == domain.EmAndamento {
		// O driver não pausou: a pausa por limite vem de antes (p.ex. de
		// quando o agente media em dólares) e não pode ser desfeita por ele.
		if _, err := e.st.PauseForLimit(c.ID, t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.PutLimit(domain.Limit{Agent: "fake", DailyUSD: 100, WarnPct: 80, Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	e.handle(t, d, Event{Kind: ToolBefore, Tool: cmd})
	got, _ = e.st.GetCard(c.Key)
	return afterOver, got.Column, len(e.events(t, domain.EvLimitPause))
}

// Controle: o driver em dólares pausa o card no limite e o retoma abaixo dele.
func TestUSDDriverPausesAndResumesCardOnLimit(t *testing.T) {
	e, d := newFake(t)
	over, under, pauses := limitFlow(t, e, d, false)
	if over != domain.Pausado || under != domain.EmAndamento || pauses != 1 {
		t.Fatalf("acima %s, abaixo %s, pausas %d", over, under, pauses)
	}
}

// O driver em créditos nunca pausa nem retoma card por limite em dólares.
func TestCreditsDriverNeverPausesNorResumesCard(t *testing.T) {
	d := &creditsDriver{}
	e := newEnv(t, d)
	over, under, pauses := limitFlow(t, e, &d.fakeDriver, true)
	if over != domain.EmAndamento || under != domain.Pausado || pauses != 0 {
		t.Fatalf("acima %s, abaixo %s, pausas %d", over, under, pauses)
	}
}
