package board

import (
	"testing"

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
