package board

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// liveCard cria um card em andamento com uma sessão viva de PID pid.
func liveCard(t *testing.T, st *store.Store, pid int) (domain.Card, domain.Session) {
	t.Helper()
	p, err := st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateCard(domain.Card{Title: "t", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	x := domain.Session{ID: "claude:1", Agent: "claude", PID: pid, StartedAt: t0, LastEventAt: t0}
	if err := st.StartSession(x); err != nil {
		t.Fatal(err)
	}
	if err := st.AttachSession(x.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	return c, x
}

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func fakeProc(t *testing.T, pids ...int) string {
	root := t.TempDir()
	for _, p := range pids {
		if err := os.Mkdir(filepath.Join(root, strconv.Itoa(p)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func column(t *testing.T, st *store.Store, key string) domain.Column {
	t.Helper()
	c, err := st.GetCard(key)
	if err != nil {
		t.Fatal(err)
	}
	return c.Column
}

func TestReconcileWaitsGrace(t *testing.T) {
	st := newStore(t)
	c, x := liveCard(t, st, 4242)
	set := domain.DefaultSettings() // GraceMin = 15
	root := fakeProc(t)             // PID 4242 não existe

	changed, err := Reconcile(st, Liveness{ProcRoot: root}, t0, set)
	if err != nil || !changed {
		t.Fatalf("1ª passada: changed=%v err=%v", changed, err)
	}
	got, _ := st.GetSession(x.ID)
	if got.PIDDeadAt == nil || !got.PIDDeadAt.Equal(t0) || got.EndedAt != nil {
		t.Fatalf("sessão = %+v", got)
	}

	// Morte + 10 min: ainda dentro da carência; nada muda, pid_dead_at é mantido.
	changed, err = Reconcile(st, Liveness{ProcRoot: root}, t0.Add(10*time.Minute), set)
	if err != nil || changed {
		t.Fatalf("+10min: changed=%v err=%v", changed, err)
	}
	if column(t, st, c.Key) != domain.EmAndamento {
		t.Fatal("card saiu de em_andamento antes da carência")
	}
	got, _ = st.GetSession(x.ID)
	if !got.PIDDeadAt.Equal(t0) {
		t.Fatalf("pid_dead_at mudou: %v", got.PIDDeadAt)
	}

	// Morte + 16 min: encerra a sessão e pausa o card.
	changed, err = Reconcile(st, Liveness{ProcRoot: root}, t0.Add(16*time.Minute), set)
	if err != nil || !changed {
		t.Fatalf("+16min: changed=%v err=%v", changed, err)
	}
	if column(t, st, c.Key) != domain.Pausado {
		t.Fatalf("card = %s, quer pausado", column(t, st, c.Key))
	}
	got, _ = st.GetSession(x.ID)
	if got.EndedAt == nil || got.EndReason != "processo encerrado" {
		t.Fatalf("sessão = %+v", got)
	}
}

func TestReconcileKeepsLive(t *testing.T) {
	st := newStore(t)
	c, x := liveCard(t, st, 4242)
	changed, err := Reconcile(st, Liveness{ProcRoot: fakeProc(t, 4242)}, t0.Add(time.Hour), domain.DefaultSettings())
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := st.GetSession(x.ID)
	if got.PIDDeadAt != nil || got.EndedAt != nil || column(t, st, c.Key) != domain.EmAndamento {
		t.Fatalf("sessão = %+v", got)
	}
}

func TestRunReconcilerPublishesAndStops(t *testing.T) {
	st := newStore(t)
	liveCard(t, st, 4242)
	ctx, cancel := context.WithCancel(context.Background())
	changes := make(chan struct{}, 4)
	done := make(chan struct{})
	go func() {
		RunReconciler(ctx, st, Liveness{ProcRoot: fakeProc(t)}, 5*time.Millisecond, func() time.Time { return t0 },
			func() { changes <- struct{}{} }, t.Logf)
		close(done)
	}()
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("onChange não foi chamado")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReconciler não parou")
	}
}

func TestReconcileRevivedPIDResetsGrace(t *testing.T) {
	st := newStore(t)
	c, x := liveCard(t, st, 4242)
	set := domain.DefaultSettings()
	if _, err := Reconcile(st, Liveness{ProcRoot: fakeProc(t)}, t0, set); err != nil {
		t.Fatal(err)
	}
	// PID volta: estado de morte some.
	changed, err := Reconcile(st, Liveness{ProcRoot: fakeProc(t, 4242)}, t0.Add(5*time.Minute), set)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got, _ := st.GetSession(x.ID); got.PIDDeadAt != nil {
		t.Fatalf("pid_dead_at = %v", got.PIDDeadAt)
	}
	// Nova morte em +20 min reinicia a carência: +30 min ainda não encerra, +36 sim.
	root := fakeProc(t)
	if _, err := Reconcile(st, Liveness{ProcRoot: root}, t0.Add(20*time.Minute), set); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(st, Liveness{ProcRoot: root}, t0.Add(30*time.Minute), set); err != nil || column(t, st, c.Key) != domain.EmAndamento {
		t.Fatalf("encerrou cedo (err=%v)", err)
	}
	if _, err := Reconcile(st, Liveness{ProcRoot: root}, t0.Add(36*time.Minute), set); err != nil || column(t, st, c.Key) != domain.Pausado {
		t.Fatalf("não encerrou (err=%v)", err)
	}
}

func TestReconcileContinuesPastSessionError(t *testing.T) {
	st := newStore(t)
	c, _ := liveCard(t, st, 4242) // claude:1, falha ao marcar
	bad := domain.Session{ID: "bad", Agent: "claude", PID: 4343, StartedAt: t0.Add(-time.Hour), LastEventAt: t0}
	if err := st.StartSession(bad); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`CREATE TRIGGER boom BEFORE UPDATE OF pid_dead_at ON sessions WHEN NEW.id='bad'
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	changed, err := Reconcile(st, Liveness{ProcRoot: fakeProc(t)}, t0, domain.DefaultSettings())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if !changed {
		t.Fatal("changed deveria refletir a sessão que mudou")
	}
	if got, _ := st.GetSession("claude:1"); got.PIDDeadAt == nil {
		t.Fatal("sessão boa não foi processada")
	}
	_ = c
}

func TestReconcileEndsIdleSessionWithoutPID(t *testing.T) {
	st := newStore(t)
	c, x := liveCard(t, st, 0)
	root := fakeProc(t)
	set := domain.DefaultSettings()

	// Sem PID não há processo para vigiar: até 2 h sem eventos, segue aberta.
	changed, err := Reconcile(st, Liveness{ProcRoot: root}, t0.Add(2*time.Hour), set)
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got, _ := st.GetSession(x.ID); got.EndedAt != nil || got.PIDDeadAt != nil {
		t.Fatalf("sessão = %+v", got)
	}
	changed, err = Reconcile(st, Liveness{ProcRoot: root}, t0.Add(2*time.Hour+time.Minute), set)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := st.GetSession(x.ID)
	if got.EndedAt == nil || got.EndReason != "sem atividade" {
		t.Fatalf("sessão = %+v", got)
	}
	if column(t, st, c.Key) != domain.Pausado {
		t.Fatalf("col = %s", column(t, st, c.Key))
	}
}

func TestReconcilerMarksIncompleteOnDeadProcess(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "mark", nil)
	c, _ := liveCard(t, st, 4242)
	set, _ := st.GetSettings()
	root := fakeProc(t)
	if _, err := Reconcile(st, Liveness{ProcRoot: root}, t0, set); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(st, Liveness{ProcRoot: root}, t0.Add(16*time.Minute), set); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCard(c.Key)
	if got.Column != domain.Pausado || !got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
}

// writeComm grava o nome do processo pid em root (como /proc/<pid>/comm).
func writeComm(t *testing.T, root string, pid int, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, strconv.Itoa(pid), "comm"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PID reaproveitado por outro programa (comm diferente do agente) conta como
// processo morto; com o nome do agente, segue vivo.
func TestReconcileRecycledPIDIsDead(t *testing.T) {
	st := newStore(t)
	c, x := liveCard(t, st, 4242)
	root := fakeProc(t, 4242)
	live := Liveness{ProcRoot: root, Names: map[string][]string{"claude": {"claude"}}}
	set := domain.DefaultSettings()

	writeComm(t, root, 4242, "claude")
	if changed, err := Reconcile(st, live, t0, set); err != nil || changed {
		t.Fatalf("com o agente vivo: changed=%v err=%v", changed, err)
	}

	writeComm(t, root, 4242, "bash")
	if _, err := Reconcile(st, live, t0.Add(time.Minute), set); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetSession(x.ID)
	if got.PIDDeadAt == nil {
		t.Fatalf("PID reaproveitado não marcou carência: %+v", got)
	}
	grace := time.Duration(set.GraceMin)*time.Minute + 2*time.Minute
	if _, err := Reconcile(st, live, t0.Add(grace), set); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.GetSession(x.ID); got.EndedAt == nil || column(t, st, c.Key) != domain.Pausado {
		t.Fatalf("sessão = %+v card = %s", got, column(t, st, c.Key))
	}
}

// Agente fora do mapa de nomes (ou mapa vazio): basta o PID existir.
func TestReconcileUnknownAgentOnlyNeedsPID(t *testing.T) {
	st := newStore(t)
	_, x := liveCard(t, st, 4242)
	root := fakeProc(t, 4242)
	writeComm(t, root, 4242, "bash")
	if _, err := Reconcile(st, Liveness{ProcRoot: root, Names: map[string][]string{"codex": {"codex"}}}, t0, domain.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetSession(x.ID); got.PIDDeadAt != nil {
		t.Fatalf("sessão marcada como morta: %+v", got)
	}
}

// O fim da sessão pelo reconciliador (processo morto, carência vencida)
// recalcula o épico da filha.
func TestEpicReconcilePath(t *testing.T) {
	st := newStore(t)
	p, err := st.CreateProject("api", "/w/api")
	if err != nil {
		t.Fatal(err)
	}
	e, err := st.CreateCard(domain.Card{Title: "e", ProjectID: p.ID, Type: domain.TypeEpico})
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateCard(domain.Card{Title: "f", ProjectID: p.ID, ParentID: &e.ID})
	if err != nil {
		t.Fatal(err)
	}
	x := domain.Session{ID: "claude:1", Agent: "claude", PID: 4242, StartedAt: t0, LastEventAt: t0}
	if err := st.StartSession(x); err != nil {
		t.Fatal(err)
	}
	if err := st.AttachSession(x.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	// Épico desatualizado (gravação manual crua): só o recálculo no fim da
	// sessão o devolve para em andamento.
	if err := st.MoveCard(e.Key, domain.Backlog); err != nil {
		t.Fatal(err)
	}
	set, root := domain.DefaultSettings(), fakeProc(t)
	if _, err := Reconcile(st, Liveness{ProcRoot: root}, t0, set); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(st, Liveness{ProcRoot: root}, t0.Add(16*time.Minute), set); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetSession(x.ID); got.EndedAt == nil {
		t.Fatal("sessão não foi encerrada")
	}
	if col := column(t, st, c.Key); col != domain.Pausado {
		t.Fatalf("filha = %s, quer pausado", col)
	}
	if col := column(t, st, e.Key); col != domain.EmAndamento {
		t.Fatalf("épico = %s, quer em_andamento", col)
	}
}
