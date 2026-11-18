package store

import (
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func TestSwapSeenConfig(t *testing.T) {
	s := newTestStore(t)
	if err := s.StartSession(newSession("claude:1", 10)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSession("claude:1")
	if got.SeenConfig != nil {
		t.Fatalf("seen = %+v", got.SeenConfig)
	}
	a := domain.SeenConfig{AgentCreatesCards: true, Capture: true}
	if ok, err := s.SwapSeenConfig("claude:1", nil, a); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ok, err := s.SwapSeenConfig("claude:1", nil, a); err != nil || ok {
		t.Fatalf("segunda troca com nil: ok=%v err=%v", ok, err)
	}
	b := domain.SeenConfig{AgentCreatesCards: false, Capture: true}
	if ok, err := s.SwapSeenConfig("claude:1", &a, b); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	got, _ = s.GetSession("claude:1")
	if got.SeenConfig == nil || *got.SeenConfig != b {
		t.Fatalf("seen = %+v", got.SeenConfig)
	}
}

func TestSetProjectSessionsCaptureOnlyLiveAndFamily(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("a", "/w/a")
	p2, _ := s.CreateProject("b", "/w/b")
	child, _, err := s.CreateChild("web", "/w/a/apps/web", p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(id string, pid int, p int64) {
		x := newSession(id, pid)
		x.ProjectID = &p
		x.FullCapture = true
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	mk("claude:live", 1, p1.ID)
	mk("claude:ended", 2, p1.ID)
	mk("claude:child", 3, child.ID)
	mk("claude:other", 4, p2.ID)
	if err := s.EndSession("claude:ended", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	ids, err := s.SetProjectSessionsCapture(p1.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}
	want := map[string]bool{"claude:live": false, "claude:child": false, "claude:ended": true, "claude:other": true}
	for id, full := range want {
		x, _ := s.GetSession(id)
		if x.FullCapture != full {
			t.Errorf("%s FullCapture = %v, quer %v", id, x.FullCapture, full)
		}
	}
}

func TestAdoptSession(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("a", "/w/a")
	if err := s.StartSession(newSession("claude:1", 10)); err != nil {
		t.Fatal(err)
	}
	if err := s.AdoptSession("claude:1", p.ID); err != nil {
		t.Fatal(err)
	}
	x, _ := s.GetSession("claude:1")
	if x.ProjectID == nil || *x.ProjectID != p.ID || !x.FullCapture {
		t.Fatalf("%+v", x)
	}
}

func TestConfigLogKeeps500(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	for i := 0; i < 502; i++ {
		if err := s.AddConfigLog(domain.LogSettingChanged, map[string]int{"i": i}, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.RecentConfigLog(1000)
	if err != nil || len(got) != 500 {
		t.Fatalf("len=%d err=%v", len(got), err)
	}
	if string(got[0].Data) != `{"i":501}` || got[0].Kind != domain.LogSettingChanged {
		t.Fatalf("primeira = %+v", got[0])
	}
}
