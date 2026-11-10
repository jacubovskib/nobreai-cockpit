package domain

import (
	"testing"
	"time"
)

func TestCardSignal(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		col  Column
		ago  time.Duration
		want Signal
	}{
		{"limite exato", EmAndamento, 24 * time.Hour, SignalNone},
		{"passou do limite", EmAndamento, 24*time.Hour + time.Second, SignalParado},
		{"pausado", Pausado, 72 * time.Hour, SignalParado},
		{"revisão parada", Revisao, 25 * time.Hour, SignalAguardandoRevisao},
		{"revisão recente", Revisao, 2 * time.Hour, SignalNone},
		{"backlog", Backlog, 30 * 24 * time.Hour, SignalNone},
		{"concluído", Concluido, 30 * 24 * time.Hour, SignalNone},
	}
	for _, c := range cases {
		if got := CardSignal(c.col, now.Add(-c.ago), now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
