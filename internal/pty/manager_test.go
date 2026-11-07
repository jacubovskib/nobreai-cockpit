package pty

import (
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitDone(t *testing.T, tm *Terminal) {
	t.Helper()
	select {
	case <-tm.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("terminal não encerrou")
	}
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout esperando: %s", what)
}

func sh(script string) StartSpec {
	return StartSpec{Argv: []string{"/bin/sh", "-c", script}, Origin: "plataforma"}
}

func TestStartEchoesOutput(t *testing.T) {
	m := NewManager(Options{})
	defer m.Close()
	tm, err := m.Start(sh("printf oi"))
	if err != nil {
		t.Fatal(err)
	}
	_, ch, cancel := tm.Subscribe()
	defer cancel()
	waitDone(t, tm)
	snap, _, _ := tm.Subscribe()
	if !strings.Contains(string(snap), "oi") {
		t.Fatalf("snapshot = %q", snap)
	}
	in := tm.Info()
	if in.ExitCode == nil || *in.ExitCode != 0 || in.EndedAt == nil {
		t.Fatalf("info = %+v", in)
	}
	if !strings.HasPrefix(in.ID, "t-") || len(in.ID) != 10 {
		t.Fatalf("id = %q", in.ID)
	}
	for range ch { // canal precisa fechar no fim
	}
}

func TestExitCodePropagates(t *testing.T) {
	m := NewManager(Options{})
	defer m.Close()
	tm, _ := m.Start(sh("exit 7"))
	waitDone(t, tm)
	if c := tm.Info().ExitCode; c == nil || *c != 7 {
		t.Fatalf("exit = %v", c)
	}
}

func TestInputReachesProcess(t *testing.T) {
	m := NewManager(Options{})
	defer m.Close()
	tm, err := m.Start(StartSpec{Argv: []string{"cat"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.Write([]byte("abc\n")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "abc na saída", func() bool {
		return bytes.Contains(tm.ring.Snapshot(), []byte("abc"))
	})
	if err := m.Kill(tm.Info().ID); err != nil {
		t.Fatal(err)
	}
	waitDone(t, tm)
}

func TestSlowSubscriberDropped(t *testing.T) {
	m := NewManager(Options{RingSize: 1024})
	defer m.Close()
	// escreve 2 MiB de uma vez, sem esperar ninguém ler
	tm, err := m.Start(sh("head -c 2097152 /dev/zero | tr '\\0' x"))
	if err != nil {
		t.Fatal(err)
	}
	_, slow, cancel := tm.Subscribe() // nunca lê
	defer cancel()
	waitDone(t, tm) // o processo não travou
	closed := false
	for i := 0; i < subBuffer+1 && !closed; i++ {
		_, ok := <-slow
		closed = !ok
	}
	if !closed {
		t.Fatal("assinante lento não foi desconectado")
	}
	if n := tm.Info().Clients; n != 0 {
		t.Fatalf("clients = %d", n)
	}
}

func TestResizeApplies(t *testing.T) {
	m := NewManager(Options{})
	defer m.Close()
	tm, err := m.Start(sh("sleep 0.3; stty size"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	waitDone(t, tm)
	if got := string(tm.ring.Snapshot()); !strings.Contains(got, "40 100") {
		t.Fatalf("saída = %q", got)
	}
	if in := tm.Info(); in.Cols != 100 || in.Rows != 40 {
		t.Fatalf("info = %+v", in)
	}
}

func TestMaxLive(t *testing.T) {
	m := NewManager(Options{MaxLive: 1})
	defer m.Close()
	if _, err := m.Start(StartSpec{Argv: []string{"cat"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(StartSpec{Argv: []string{"cat"}}); err != ErrTooMany {
		t.Fatalf("err = %v", err)
	}
}

func TestEndedDoesNotCountAsLive(t *testing.T) {
	m := NewManager(Options{MaxLive: 1})
	defer m.Close()
	tm, _ := m.Start(sh("true"))
	waitDone(t, tm)
	if _, err := m.Start(sh("true")); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestRetentionPrunes(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	m := NewManager(Options{Now: clock})
	defer m.Close()
	tm, _ := m.Start(sh("true"))
	waitDone(t, tm)
	mu.Lock()
	now = now.Add(29 * time.Minute)
	mu.Unlock()
	if len(m.List()) != 1 {
		t.Fatal("deveria ainda estar listado aos 29 min")
	}
	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	if l := m.List(); len(l) != 0 {
		t.Fatalf("deveria ter sumido aos 31 min: %+v", l)
	}
	if _, ok := m.Get(tm.Info().ID); ok {
		t.Fatal("Get ainda acha terminal descartado")
	}
}

func TestKillEscalatesToSIGKILL(t *testing.T) {
	m := NewManager(Options{})
	defer m.Close()
	tm, err := m.Start(sh("trap '' TERM; while :; do sleep 1; done"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // deixa o trap instalar
	start := time.Now()
	if err := m.Kill(tm.Info().ID); err != nil {
		t.Fatal(err)
	}
	waitDone(t, tm)
	if d := time.Since(start); d < 2*time.Second {
		t.Fatalf("morreu cedo demais (%v): SIGTERM deveria ser ignorado", d)
	}
}

func TestKillUnknown(t *testing.T) {
	m := NewManager(Options{})
	if err := m.Kill("t-nope"); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestCloseKillsAll(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	m := NewManager(Options{})
	var tms []*Terminal
	for i := 0; i < 3; i++ {
		tm, err := m.Start(StartSpec{Argv: []string{"cat"}})
		if err != nil {
			t.Fatal(err)
		}
		_, _, _ = tm.Subscribe()
		tms = append(tms, tm)
	}
	m.Close()
	for _, tm := range tms {
		waitDone(t, tm)
		if tm.Info().ExitCode == nil {
			t.Fatal("sem exit code")
		}
	}
	eventually(t, "goroutines voltarem ao patamar", func() bool {
		return runtime.NumGoroutine() <= before
	})
}
