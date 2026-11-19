package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/rtk"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

var rtkOn = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

// rtkSession cria a sessão com started_at e project/card dados.
func rtkSession(t *testing.T, s *Store, id string, started time.Time, card, project *int64) {
	t.Helper()
	x := newSession(id, 1)
	x.TranscriptPath = "/main"
	x.StartedAt, x.LastEventAt = started, started
	x.CardID, x.ProjectID = card, project
	if err := s.StartSession(x); err != nil {
		t.Fatal(err)
	}
}

// rtkOuts grava n saídas Bash da chave, cada uma com tokens, no instante at.
func rtkOuts(t *testing.T, s *Store, sid, key string, n int, tokens int64, at time.Time) {
	t.Helper()
	var outs []usage.ToolOutput
	for i := 0; i < n; i++ {
		outs = append(outs, usage.ToolOutput{ItemID: fmt.Sprintf("%s-%s-%d", sid, key, i), Tool: "Bash", Key: key,
			Bytes: tokens * 4, Tokens: tokens, Turn: 0, At: at, Result: true})
	}
	save(t, s, sid, "/main", outs, nil)
}

func TestRTKBeforeAfter(t *testing.T) {
	s := newTestStore(t)
	ivs := []rtk.Interval{{On: rtkOn}}
	rtkSession(t, s, "old", rtkOn.Add(-48*time.Hour), nil, nil)
	rtkSession(t, s, "new", rtkOn.Add(time.Hour), nil, nil)
	rtkSession(t, s, "straddle", rtkOn.Add(-time.Hour), nil, nil)
	rtkOuts(t, s, "old", "git status", 5, 100, rtkOn.Add(-47*time.Hour))
	rtkOuts(t, s, "new", "git status", 5, 40, rtkOn.Add(2*time.Hour))
	// começou antes do on: as saídas de depois não contam como "depois"
	rtkOuts(t, s, "straddle", "git status", 3, 999, rtkOn.Add(time.Hour))
	// 4 chamadas de um lado: fica de fora
	rtkOuts(t, s, "old", "ls", 5, 10, rtkOn.Add(-47*time.Hour))
	rtkOuts(t, s, "new", "ls", 4, 5, rtkOn.Add(2*time.Hour))
	got, err := s.RTKBeforeAfter(nil, ivs, 5, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got = %+v", got)
	}
	c := got[0]
	if c.Key != "git status" || c.BeforeCount != 5 || c.BeforeAvg != 100 || c.AfterCount != 5 || c.AfterAvg != 40 || c.ChangePct != -60 {
		t.Errorf("c = %+v", c)
	}
}

func TestRTKBeforeAfter_ClosedInterval(t *testing.T) {
	s := newTestStore(t)
	off := rtkOn.Add(24 * time.Hour)
	ivs := []rtk.Interval{{On: rtkOn, Off: &off}}
	rtkSession(t, s, "old", rtkOn.Add(-time.Hour), nil, nil)
	rtkSession(t, s, "in", rtkOn.Add(time.Hour), nil, nil)
	rtkSession(t, s, "later", off.Add(time.Hour), nil, nil)
	rtkOuts(t, s, "old", "k", 5, 100, rtkOn.Add(-30*time.Minute))
	rtkOuts(t, s, "in", "k", 5, 50, rtkOn.Add(2*time.Hour))
	rtkOuts(t, s, "later", "k", 5, 900, off.Add(2*time.Hour))
	got, _ := s.RTKBeforeAfter(nil, ivs, 5, 15)
	if len(got) != 1 || got[0].AfterCount != 5 || got[0].AfterAvg != 50 {
		t.Fatalf("got = %+v", got)
	}
}

func TestRTKBeforeAfter_Project(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("a", "/w/a")
	p2, _ := s.CreateProject("b", "/w/b")
	// O projeto do card vence o da sessão: esta sessão (projeto a) conta como b.
	card := seedCard(t, s, "todo")
	ivs := []rtk.Interval{{On: rtkOn}}
	rtkSession(t, s, "a-old", rtkOn.Add(-time.Hour), nil, &p1.ID)
	rtkSession(t, s, "a-new", rtkOn.Add(time.Hour), nil, &p1.ID)
	rtkSession(t, s, "b-old", rtkOn.Add(-time.Hour), nil, &p2.ID)
	rtkSession(t, s, "b-new", rtkOn.Add(time.Hour), nil, &p2.ID)
	rtkSession(t, s, "c-old", rtkOn.Add(-time.Hour), &card.ID, &p1.ID)
	rtkOuts(t, s, "c-old", "k", 5, 100, rtkOn.Add(-30*time.Minute))
	for _, sid := range []string{"a-old", "b-old"} {
		rtkOuts(t, s, sid, "k", 5, 100, rtkOn.Add(-30*time.Minute))
	}
	for _, sid := range []string{"a-new", "b-new"} {
		rtkOuts(t, s, sid, "k", 5, 50, rtkOn.Add(2*time.Hour))
	}
	got, _ := s.RTKBeforeAfter([]int64{p1.ID}, ivs, 5, 15)
	if len(got) != 1 || got[0].BeforeCount != 5 || got[0].AfterCount != 5 {
		t.Fatalf("p1 = %+v", got)
	}
	got, _ = s.RTKBeforeAfter(nil, ivs, 5, 15)
	if len(got) != 1 || got[0].BeforeCount != 15 {
		t.Fatalf("all = %+v", got)
	}
}

func TestRTKBeforeAfter_EmptyAndLimit(t *testing.T) {
	s := newTestStore(t)
	got, err := s.RTKBeforeAfter(nil, nil, 5, 15)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got = %#v, %v", got, err)
	}
	rtkSession(t, s, "old", rtkOn.Add(-time.Hour), nil, nil)
	rtkSession(t, s, "new", rtkOn.Add(time.Hour), nil, nil)
	for i := 0; i < 17; i++ {
		k := fmt.Sprintf("cmd%02d", i)
		rtkOuts(t, s, "old", k, 5+i, 10, rtkOn.Add(-30*time.Minute))
		rtkOuts(t, s, "new", k, 5, 10, rtkOn.Add(2*time.Hour))
	}
	got, _ = s.RTKBeforeAfter(nil, []rtk.Interval{{On: rtkOn}}, 5, 15)
	if len(got) != 15 || got[0].Key != "cmd16" {
		t.Fatalf("len=%d first=%+v", len(got), got[0])
	}
}
