package board

import (
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// putSettings grava as opções de teste (IncompleteAction e, se informado, as regras).
func putSettings(t *testing.T, st *store.Store, action string, rules map[string]bool) {
	t.Helper()
	set := domain.DefaultSettings()
	set.IncompleteAction = action
	if rules != nil {
		set.Rules = rules
	}
	if err := st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
}

func TestFinishSessionMarksIncompleteInMarkMode(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "mark", nil)
	c, x := liveCard(t, st, 1)
	if err := FinishSession(st, x.ID, "fim", t0); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCard(c.Key)
	if got.Column != domain.Pausado || !got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
}

func TestFinishSessionDoesNotMarkInAskAgentMode(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "ask_agent", nil)
	c, x := liveCard(t, st, 1)
	if err := FinishSession(st, x.ID, "fim", t0); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCard(c.Key)
	if got.Column != domain.Pausado || got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
}

func TestFinishSessionDoesNotMarkReadyCard(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "mark", map[string]bool{"goal": true, "where_stopped": false, "essential_file": false, "todo_open": false, "how_to_test": true, "no_open_questions": false})
	c, x := liveCard(t, st, 1)
	c, _ = st.GetCard(c.Key)
	c.Goal, c.DoneWhen, c.HowToTest = "g", "d", "go test"
	if err := st.UpdateCard(c); err != nil {
		t.Fatal(err)
	}
	if err := FinishSession(st, x.ID, "fim", t0); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCard(c.Key)
	if got.Column != domain.Pausado || got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
}

func TestFinishSessionKeepsFlagOffWhenAnotherSessionLive(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "mark", nil)
	c, x := liveCard(t, st, 1)
	y := domain.Session{ID: "claude:2", Agent: "claude", PID: 2, StartedAt: t0, LastEventAt: t0}
	if err := st.StartSession(y); err != nil {
		t.Fatal(err)
	}
	// AttachSession não permite dois donos vivos; força a corrida direto no banco.
	if _, err := st.DB.Exec(`UPDATE sessions SET card_id=? WHERE id=?`, c.ID, y.ID); err != nil {
		t.Fatal(err)
	}
	if err := FinishSession(st, x.ID, "fim", t0); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCard(c.Key)
	if got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
}

// failHandoffMark faz qualquer gravação de handoff_incomplete falhar.
func failHandoffMark(t *testing.T, st *store.Store) {
	t.Helper()
	if _, err := st.DB.Exec(`CREATE TRIGGER fail_mark BEFORE UPDATE OF handoff_incomplete ON cards
		BEGIN SELECT RAISE(ABORT, 'falha simulada'); END`); err != nil {
		t.Fatal(err)
	}
}

// Sessão já encerrada antes desta chamada (SessionEnd repetido, ou hook e
// reconciliador na mesma sessão): nada é avaliado de novo.
func TestFinishSessionSkipsAlreadyEndedSession(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "mark", nil)
	c, x := liveCard(t, st, 1)
	if err := st.EndSession(x.ID, "antes", t0); err != nil {
		t.Fatal(err)
	}
	if err := FinishSession(st, x.ID, "fim", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCard(c.Key)
	if got.Column != domain.Pausado || got.HandoffIncomplete {
		t.Fatalf("card = %+v", got)
	}
	if s, _ := st.GetSession(x.ID); s.EndReason != "antes" {
		t.Fatalf("sessão = %+v", s)
	}
}

// A marca é melhor esforço: falhar nela não desfaz nem esconde o fim da sessão.
func TestFinishSessionMarkFailureIsBestEffort(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "mark", nil)
	c, x := liveCard(t, st, 1)
	failHandoffMark(t, st)
	if err := FinishSession(st, x.ID, "fim", t0); err != nil {
		t.Fatalf("err = %v", err)
	}
	if s, _ := st.GetSession(x.ID); s.EndedAt == nil {
		t.Fatalf("sessão não encerrada: %+v", s)
	}
	if got, _ := st.GetCard(c.Key); got.Column != domain.Pausado {
		t.Fatalf("card = %+v", got)
	}
}

// O reconciliador conta a sessão encerrada mesmo quando a marca falha.
func TestReconcileChangedWhenMarkFails(t *testing.T) {
	st := newStore(t)
	putSettings(t, st, "mark", nil)
	_, x := liveCard(t, st, 0) // sem PID: encerra por inatividade
	failHandoffMark(t, st)
	set, _ := st.GetSettings()
	changed, err := Reconcile(st, fakeProc(t), t0.Add(IdleNoPID+time.Minute), set)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if s, _ := st.GetSession(x.ID); s.EndedAt == nil {
		t.Fatalf("sessão não encerrada: %+v", s)
	}
}
