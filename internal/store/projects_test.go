package store

import "testing"

func TestReassignSessionsRespectsBoundary(t *testing.T) {
	s := newTestStore(t)
	for i, cwd := range []string{"/r", "/r/sub", "/r-outro", "/x"} {
		x := newSession("claude:"+string(rune('a'+i)), 10+i)
		x.Cwd = cwd
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.CreateProject("r", "/r")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.ReassignSessions(p.ID, "/r")
	if err != nil || n != 2 {
		t.Fatalf("n = %d, %v", n, err)
	}
	for id, want := range map[string]bool{"claude:a": true, "claude:b": true, "claude:c": false, "claude:d": false} {
		x, err := s.GetSession(id)
		if err != nil {
			t.Fatal(err)
		}
		got := x.ProjectID != nil && *x.ProjectID == p.ID && x.FullCapture
		if got != want || (!want && x.ProjectID != nil) {
			t.Fatalf("%s: %+v", id, x)
		}
	}
}
