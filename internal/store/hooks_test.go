package store

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func TestReopenEndedSession(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	x := newSession("claude:1", 10)
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession(x.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPIDDead(x.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession(x.ID, "processo encerrado", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c.Key); got.Column != domain.Pausado {
		t.Fatalf("col = %s", got.Column)
	}
	at := time.Now().UTC().Add(time.Minute)
	if err := s.ReopenSession(x.ID, 20, at); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EndedAt != nil || got.EndReason != "" || got.PIDDeadAt != nil || got.PID != 20 || !got.LastEventAt.Equal(at) || !got.Live() {
		t.Fatalf("sessão reaberta = %+v", got)
	}
	// Dona viva de novo: o card pausado volta para em_andamento.
	if cc, _ := s.GetCard(c.Key); cc.Column != domain.EmAndamento {
		t.Fatalf("col = %s", cc.Column)
	}
	if err := s.ReopenSession("nope", 1, at); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestReopenSessionLosesCardTakenByOther(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	old := newSession("claude:old", 10)
	cur := newSession("claude:cur", 11)
	for _, x := range []domain.Session{old, cur} {
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AttachSession(old.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession(old.ID, "fim", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession(cur.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.ReopenSession(old.ID, 12, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSession(old.ID)
	if got.CardID != nil {
		t.Fatalf("sessão reaberta continua no card de outra dona viva: %+v", got)
	}
	if cc, _ := s.GetCard(c.Key); cc.Column != domain.EmAndamento {
		t.Fatalf("col = %s", cc.Column)
	}
}

func TestReopenKeepsReviewColumn(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	x := newSession("claude:1", 10)
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession(x.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveCard(c.Key, domain.Revisao); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession(x.ID, "fim", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.ReopenSession(x.ID, 10, time.Now()); err != nil {
		t.Fatal(err)
	}
	if cc, _ := s.GetCard(c.Key); cc.Column != domain.Revisao {
		t.Fatalf("card em revisão não deveria voltar: %s", cc.Column)
	}
}

func TestAttachDeadProcessSessionPausesCard(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	x := newSession("claude:1", 10)
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPIDDead(x.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession(x.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	if cc, _ := s.GetCard(c.Key); cc.Column != domain.Pausado {
		t.Fatalf("sessão com processo morto não é dona viva; col = %s", cc.Column)
	}
}

func TestSessionEditQueries(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	x := newSession("claude:1", 10)
	other := newSession("claude:2", 11)
	for _, y := range []domain.Session{x, other} {
		if err := s.StartSession(y); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AttachSession(x.ID, c.Key, false); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	edit := func(sid, path string, at time.Time) {
		b, _ := json.Marshal(map[string]string{"path": path})
		if err := s.AddEvent(domain.Event{SessionID: sid, Kind: domain.EvFileEdit, Data: b, At: at}); err != nil {
			t.Fatal(err)
		}
	}
	if has, err := s.SessionEditedAfter(x.ID, c.ID, nil); err != nil || has {
		t.Fatalf("sem edits: %v %v", has, err)
	}
	edit(x.ID, "/a.go", t0)
	edit(x.ID, "/a.go", t0.Add(time.Minute))
	edit(x.ID, "/b.go", t0.Add(2*time.Minute))
	edit(other.ID, "/c.go", t0.Add(time.Hour))

	if has, _ := s.SessionEditedAfter(x.ID, c.ID, nil); !has {
		t.Fatal("onde parei nulo com edit deveria contar")
	}
	after := t0.Add(2 * time.Minute)
	if has, _ := s.SessionEditedAfter(x.ID, c.ID, &after); has {
		t.Fatal("edit no mesmo instante não é depois")
	}
	before := t0.Add(90 * time.Second)
	if has, _ := s.SessionEditedAfter(x.ID, c.ID, &before); !has {
		t.Fatal("edit depois de onde parei")
	}
	if n, err := s.DistinctEditedFiles(x.ID); err != nil || n != 2 {
		t.Fatalf("distintos = %d %v", n, err)
	}

	if r, err := s.SessionReminded(x.ID); err != nil || r {
		t.Fatalf("reminded = %v %v", r, err)
	}
	if err := s.MarkReminded(x.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.SessionReminded(x.ID); !r {
		t.Fatal("deveria estar lembrada")
	}
}

func TestTranscriptOffsets(t *testing.T) {
	s := newTestStore(t)
	x := newSession("claude:1", 10)
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
	if off, err := s.TranscriptOffset(x.ID); err != nil || off != 0 {
		t.Fatalf("%d %v", off, err)
	}
	if err := s.SetTranscriptOffset(x.ID, 42); err != nil {
		t.Fatal(err)
	}
	if off, _ := s.TranscriptOffset(x.ID); off != 42 {
		t.Fatalf("off = %d", off)
	}
	if off, err := s.FileOffset(x.ID, "/t/agent-1.jsonl"); err != nil || off != 0 {
		t.Fatalf("%d %v", off, err)
	}
	if err := s.SetFileOffset(x.ID, "/t/agent-1.jsonl", 7); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileOffset(x.ID, "/t/agent-1.jsonl", 9); err != nil {
		t.Fatal(err)
	}
	if off, _ := s.FileOffset(x.ID, "/t/agent-1.jsonl"); off != 9 {
		t.Fatalf("off = %d", off)
	}
	if err := s.SetSessionModel(x.ID, "claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSession(x.ID); got.Model != "claude-opus-5-5" {
		t.Fatalf("model = %q", got.Model)
	}
}

func TestOpenUsesImmediateTransactions(t *testing.T) {
	path := t.TempDir() + "/cockpit.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Com _txlock=immediate, BEGIN já pega o lock de escrita: outra conexão
	// não consegue escrever enquanto a transação estiver aberta.
	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(50)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.Exec(`INSERT INTO settings(key, value) VALUES ('x', '1')`); err == nil {
		t.Fatal("escrita concorrente deveria esperar o lock da transação imediata")
	}
}
