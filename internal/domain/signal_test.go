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

func sigNow() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) }

func TestCardStatusSignalBlocked(t *testing.T) {
	in := SignalInput{Column: Pausado, Now: sigNow(), LastActivity: sigNow().Add(-time.Minute),
		BlockedBy: "NAC-4", BlockerExists: true}
	if got := CardStatusSignal(in); got != SignalBloqueado {
		t.Fatalf("pausado = %q", got)
	}
	in.Column = Backlog
	if got := CardStatusSignal(in); got != SignalBloqueado {
		t.Fatalf("backlog = %q", got)
	}
	in.Column = Concluido
	if got := CardStatusSignal(in); got != SignalNone {
		t.Fatalf("concluído = %q", got)
	}
}

func TestCardStatusSignalReadyToResume(t *testing.T) {
	for _, col := range []Column{Pausado, Backlog} {
		in := SignalInput{Column: col, Now: sigNow(), LastActivity: sigNow(),
			BlockedBy: "NAC-4", BlockerExists: true, BlockerConcluded: true}
		if got := CardStatusSignal(in); got != SignalProntoParaRetomar {
			t.Fatalf("%s = %q", col, got)
		}
	}
}

func TestCardStatusSignalWaitingUser(t *testing.T) {
	in := SignalInput{Column: Pausado, Now: sigNow(), LastActivity: sigNow().Add(-time.Minute), OpenQuestions: 1}
	if got := CardStatusSignal(in); got != SignalAguardandoVoce {
		t.Fatalf("com pergunta = %q", got)
	}
	in = SignalInput{Column: Pausado, Now: sigNow(), LastActivity: sigNow().Add(-25 * time.Hour)}
	if got := CardStatusSignal(in); got != SignalParado {
		t.Fatalf("sem pergunta = %q", got)
	}
	in = SignalInput{Column: Revisao, Now: sigNow(), LastActivity: sigNow(), OpenQuestions: 2}
	if got := CardStatusSignal(in); got != SignalNone {
		t.Fatalf("revisão = %q", got)
	}
}

func TestCardStatusSignalMissingBlocker(t *testing.T) {
	in := SignalInput{Column: Pausado, Now: sigNow(), LastActivity: sigNow().Add(-25 * time.Hour),
		BlockedBy: "NAC-99", BlockerExists: false}
	if got := CardStatusSignal(in); got != SignalParado {
		t.Fatalf("got %q", got)
	}
	in.Column, in.LastActivity = Backlog, sigNow().Add(-72*time.Hour)
	if got := CardStatusSignal(in); got != SignalNone {
		t.Fatalf("backlog = %q", got)
	}
}
