package agentcore

import (
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// cardSession cria o card com "Onde parei" em t0 e liga a sessão a ele pelo
// NOBRE_RESUME.
func (e *env) cardSession(t *testing.T, d *fakeDriver) domain.Card {
	t.Helper()
	p := e.project(t)
	at := t0
	c, err := e.st.CreateCard(domain.Card{ProjectID: p.ID, Title: "Migrar OAuth2", Branch: "main",
		WhereStopped: "lendo o oauth.go", WhereStoppedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	e.handle(t, d, Event{Kind: Started, Source: "startup", Env: map[string]string{"NOBRE_RESUME": c.Key}})
	return c
}

func TestTurnEndHoldsWhenEditedAfterWhereStopped(t *testing.T) {
	e, d := newFake(t)
	c := e.cardSession(t, d)
	e.c.Deps().Now = func() time.Time { return t0.Add(time.Minute) }
	e.handle(t, d, Event{Kind: TurnEnd})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("sem edição segurou o turno: %+v", r.HoldTurn)
	}
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/auth/oauth.go"}})
	e.c.Deps().Now = func() time.Time { return t0.Add(2 * time.Minute) }
	e.handle(t, d, Event{Kind: TurnEnd})
	r := lastReply(t, d)
	if r.HoldTurn == nil || !strings.Contains(r.HoldTurn.Reason, "Antes de encerrar, atualize o card "+c.Key) ||
		strings.Contains(r.HoldTurn.Reason, "{{") {
		t.Fatalf("HoldTurn = %+v", r.HoldTurn)
	}
	// A continuação de um turno segurado nunca é segurada de novo.
	e.handle(t, d, Event{Kind: TurnEnd, StopActive: true})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("StopActive segurou: %+v", r.HoldTurn)
	}
	// Com IncompleteAction "mark", só marca no fim da sessão.
	set, err := e.st.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	set.IncompleteAction = "mark"
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("mark segurou: %+v", r.HoldTurn)
	}
}

func TestTurnEndRemindsOnceWithoutCard(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + f}})
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	want := `NobreAI Cockpit: esta sessão já editou 3 arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`
	if r := lastReply(t, d); r.HoldTurn == nil || r.HoldTurn.Reason != want {
		t.Fatalf("lembrete = %+v", r.HoldTurn)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if r := lastReply(t, d); r.HoldTurn != nil {
		t.Fatalf("lembrou de novo: %+v", r.HoldTurn)
	}
}

const planRel = "docs/superpowers/plans/p.md"

func planText(key string) string {
	return "NobreAI Cockpit: você escreveu um plano (" + planRel + ") e o card " + key +
		` ainda não tem as tarefas. Registre com nobre plan ` + planRel + ` --task "<tarefa 1>" --task "<tarefa 2>" ...`
}

func taskText(key string) string {
	return "NobreAI Cockpit: houve commit desde a última atualização das tarefas do card " + key +
		". Atualize com nobre task <n> andamento|revisao|concluida."
}

func (e *env) at(d time.Duration) { e.c.Deps().Now = func() time.Time { return t0.Add(d) } }

func (e *env) settings(t *testing.T, f func(*domain.Settings)) {
	t.Helper()
	set, err := e.st.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	f(&set)
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
}

// editPlanNoted edita o plano em t0+1m e atualiza o "Onde parei" em t0+2m,
// para não haver handoff pendente; o relógio fica em t0+3m.
func (e *env) editPlanNoted(t *testing.T, d *fakeDriver, c domain.Card) {
	t.Helper()
	e.at(time.Minute)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + planRel}})
	if err := e.st.SetWhereStopped(c.ID, "plano escrito", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	e.at(3 * time.Minute)
}

func (e *env) commit(t *testing.T, d *fakeDriver, at time.Duration, cmd string) {
	t.Helper()
	e.at(at)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolCommand, Command: cmd}})
}

func holdReason(t *testing.T, d *fakeDriver) string {
	t.Helper()
	if r := lastReply(t, d); r.HoldTurn != nil {
		return r.HoldTurn.Reason
	}
	return ""
}

func TestTurnEndPlanReminderOnce(t *testing.T) {
	e, d := newFake(t)
	c := e.cardSession(t, d)
	e.editPlanNoted(t, d, c)
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != planText(c.Key) {
		t.Fatalf("motivo = %q\nquer      %q", got, planText(c.Key))
	}
	e.handle(t, d, Event{Kind: TurnEnd, StopActive: true})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("StopActive segurou: %q", got)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("lembrou de novo: %q", got)
	}
}

func TestTurnEndPlanReminderWithMark(t *testing.T) {
	e, d := newFake(t)
	c := e.cardSession(t, d)
	e.settings(t, func(s *domain.Settings) { s.IncompleteAction = "mark" })
	e.at(time.Minute)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + planRel}})
	e.at(2 * time.Minute)
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != planText(c.Key) {
		t.Fatalf("motivo = %q", got)
	}
}

func TestTurnEndJoinsHandoffAndPlan(t *testing.T) {
	e, d := newFake(t)
	c := e.cardSession(t, d)
	e.at(time.Minute)
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + planRel}})
	e.at(2 * time.Minute)
	e.handle(t, d, Event{Kind: TurnEnd})
	got := holdReason(t, d)
	if !strings.HasPrefix(got, "Antes de encerrar, atualize o card "+c.Key) || !strings.HasSuffix(got, "\n\n"+planText(c.Key)) {
		t.Fatalf("motivo = %q", got)
	}
}

func TestTurnEndNoPlanReminderWhenRegistered(t *testing.T) {
	e, d := newFake(t)
	c := e.cardSession(t, d)
	if _, err := e.st.SetPlan(c.ID, planRel, []string{"A"}, fakeID, t0); err != nil {
		t.Fatal(err)
	}
	e.editPlanNoted(t, d, c)
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("segurou com plano registrado: %q", got)
	}
}

func TestTurnEndTaskReminder(t *testing.T) {
	e, d := newFake(t)
	c := e.cardSession(t, d)
	if _, err := e.st.SetPlan(c.ID, planRel, []string{"A", "B"}, fakeID, t0); err != nil {
		t.Fatal(err)
	}
	e.commit(t, d, time.Minute, "git commit -m x")
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != taskText(c.Key) {
		t.Fatalf("motivo = %q", got)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("lembrou de novo sem mudança: %q", got)
	}
	setTask := func(n int, st domain.TaskStatus, at time.Duration) {
		t.Helper()
		if _, err := e.st.SetTaskStatus(c.ID, n, st, fakeID, t0.Add(at)); err != nil {
			t.Fatal(err)
		}
	}
	setTask(1, domain.TaskDoing, 2*time.Minute)
	e.commit(t, d, 3*time.Minute, "git commit -m y")
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != taskText(c.Key) {
		t.Fatalf("depois de nova mudança e commit: %q", got)
	}
	// O comando que muda a tarefa junto com o commit não conta.
	setTask(1, domain.TaskReview, 4*time.Minute)
	e.commit(t, d, 5*time.Minute, "git commit -m x && nobre task 2 concluida")
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("commit com nobre task segurou: %q", got)
	}
	// Tudo concluído: nada a cobrar.
	setTask(1, domain.TaskDone, 6*time.Minute)
	setTask(2, domain.TaskDone, 6*time.Minute)
	e.commit(t, d, 7*time.Minute, "git commit -m z")
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("tarefas concluídas segurou: %q", got)
	}
}

func TestTurnEndNoCardPlanBypassesThreshold(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/docs/plan-x.md"}})
	e.handle(t, d, Event{Kind: TurnEnd})
	want := `NobreAI Cockpit: esta sessão já editou 1 arquivo sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.` +
		"\n" + `Inclui um plano (docs/plan-x.md): crie o card com nobre start "<título>" e registre as tarefas com nobre plan.`
	if got := holdReason(t, d); got != want {
		t.Fatalf("motivo = %q", got)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("lembrou de novo: %q", got)
	}
}

func TestTurnEndNoCardPlanAfterReminder(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.handle(t, d, Event{Kind: Started})
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + f}})
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	want := `NobreAI Cockpit: esta sessão já editou 3 arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`
	if got := holdReason(t, d); got != want {
		t.Fatalf("lembrete = %q", got)
	}
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/docs/plan-x.md"}})
	e.handle(t, d, Event{Kind: TurnEnd})
	want = `NobreAI Cockpit: esta sessão escreveu um plano (docs/plan-x.md) sem card. Crie o card com nobre start "<título>" e registre as tarefas com nobre plan.`
	if got := holdReason(t, d); got != want {
		t.Fatalf("lembrete do plano = %q", got)
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("lembrou de novo: %q", got)
	}
}

func TestTurnEndPlanPatternsEmpty(t *testing.T) {
	e, d := newFake(t)
	e.project(t)
	e.settings(t, func(s *domain.Settings) { s.PlanPatterns = []string{} })
	if set, err := e.st.GetSettings(); err != nil || set.PlanPatterns == nil || len(set.PlanPatterns) != 0 {
		t.Fatalf("PlanPatterns gravado = %#v, %v", set.PlanPatterns, err)
	}
	e.handle(t, d, Event{Kind: Started})
	for _, f := range []string{"docs/plan-x.md", "a.go"} {
		e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/" + f}})
	}
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("plano com padrões vazios segurou: %q", got)
	}
	e.handle(t, d, Event{Kind: ToolAfter, Tool: Tool{Kind: ToolEdit, Path: "/w/api/b.go"}})
	e.handle(t, d, Event{Kind: TurnEnd})
	want := `NobreAI Cockpit: esta sessão já editou 3 arquivos sem card. Se é um trabalho que precisa de histórico, rode nobre start "<título>"; se não, só continue.`
	if got := holdReason(t, d); got != want {
		t.Fatalf("lembrete = %q", got)
	}

	// Com card, o plano editado também não cobra nada.
	e, d = newFake(t)
	c := e.cardSession(t, d)
	e.settings(t, func(s *domain.Settings) { s.PlanPatterns = []string{} })
	e.editPlanNoted(t, d, c)
	e.handle(t, d, Event{Kind: TurnEnd})
	if got := holdReason(t, d); got != "" {
		t.Fatalf("card com padrões vazios segurou: %q", got)
	}
}
