package domain

import "testing"

func TestNextColumn(t *testing.T) {
	tests := []struct {
		cur  Column
		ev   Transition
		want Column
	}{
		{Backlog, SessionAttached, EmAndamento},
		{Pausado, SessionAttached, EmAndamento},
		{EmAndamento, SessionEnded, Pausado},
		{Revisao, SessionEnded, Revisao},
		{Concluido, SessionEnded, Concluido},
		{EmAndamento, AgentDone, Revisao},
		{Revisao, HumanApproved, Concluido},
		{Concluido, SessionAttached, EmAndamento},
	}
	for _, tt := range tests {
		if got := NextColumn(tt.cur, tt.ev); got != tt.want {
			t.Errorf("NextColumn(%s, %d) = %s, want %s", tt.cur, tt.ev, got, tt.want)
		}
	}
}
