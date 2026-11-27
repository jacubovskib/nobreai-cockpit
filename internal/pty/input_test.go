package pty

import (
	"strings"
	"testing"
	"time"
)

func TestCleanInput(t *testing.T) {
	casos := []struct{ in, want string }{
		{"a\r\nb\rc", "a\nb\nc"},
		// Todo ESC sai; o resto de um marcador fica como texto comum.
		{"x\x1b[201~y\x1b[200~z", "x[201~y[200~z"},
		{"\x1b[20\x1b[201~1~fim", "[20[201~1~fim"},
		{"fim\x1b", "fim"},           // ESC solto antes do marcador de fim
		{"fim\x1b[", "fim["},         // início de CSI antes do marcador de fim
		{"a\u009b31mb", "a31mb"},     // CSI C1 (U+009B) também sai
		{"\xc2\x1b\x9bx", "x"},       // tirar o ESC não deixa formar um CSI C1
		{"ě e \u011b", "ě e \u011b"}, // o byte 0x9B dentro de outro caractere fica
		{"oi\n\n", "oi"},
		{"\n\n", ""},
		{" !eco", " !eco"}, // espaços do começo ficam
	}
	for _, c := range casos {
		if got := CleanInput(c.in); got != c.want {
			t.Errorf("CleanInput(%q) = %q, quero %q", c.in, got, c.want)
		}
	}
}

func startCatV(t *testing.T) (*Manager, *Terminal) {
	t.Helper()
	m := NewManager(Options{PasteSettle: 20 * time.Millisecond})
	t.Cleanup(m.Close)
	// Modo cru e sem eco: a saída é só o que o cat -v imprime, sem o eco da tty.
	tm, err := m.Start(StartSpec{Argv: []string{"/bin/sh", "-c", "stty raw -echo; echo pronto; exec cat -v"}})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "cat pronto", func() bool { return strings.Contains(snapStr(tm), "pronto") })
	return m, tm
}

func snapStr(tm *Terminal) string { return string(tm.ring.Snapshot()) }

func TestSubmitColaMarcadaEEnter(t *testing.T) {
	_, tm := startCatV(t)
	if err := tm.Submit("l1\nl2"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "colagem na saída", func() bool {
		s := snapStr(tm)
		i := strings.Index(s, "^[[200~l1")
		return i >= 0 && strings.Contains(s[i:], "l2^[[201~")
	})
}

// fakeSleep substitui a espera entre colagem e Enter: cada chamada avisa em
// entered e só volta quando o teste solta em release.
type fakeSleep struct {
	entered chan time.Duration
	release chan struct{}
}

func newFakeSleep(t *testing.T, tm *Terminal) *fakeSleep {
	t.Helper()
	f := &fakeSleep{entered: make(chan time.Duration, 4), release: make(chan struct{}, 4)}
	tm.sleep = func(d time.Duration) { f.entered <- d; <-f.release }
	// Solta quem ainda estiver parado antes de fechar o manager.
	t.Cleanup(func() { close(f.release) })
	return f
}

func (f *fakeSleep) waitEntered(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-f.entered:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("Submit não chegou à espera")
		return 0
	}
}

func TestSubmitNaoIntercalaSequencias(t *testing.T) {
	_, tm := startCatV(t)
	fs := newFakeSleep(t, tm)
	errs := make(chan error, 2)
	go func() { errs <- tm.Submit("AAAA") }()
	if d := fs.waitEntered(t); d != 20*time.Millisecond {
		t.Fatalf("espera = %v, quero o PasteSettle (20ms)", d)
	}
	eventually(t, "colagem de A", func() bool { return strings.Contains(snapStr(tm), "^[[200~AAAA^[[201~") })

	// A está entre a colagem e o Enter: a sequência segura o terminal.
	if tm.inMu.TryLock() {
		tm.inMu.Unlock()
		t.Fatal("inMu livre com A no meio da sequência")
	}
	go func() { errs <- tm.Submit("BBBB") }()
	select {
	case <-fs.entered:
		t.Fatal("B colou antes do Enter de A")
	case <-time.After(50 * time.Millisecond):
	}
	if s := snapStr(tm); strings.Contains(s, "BBBB") {
		t.Fatalf("B colou antes do Enter de A: %q", s)
	}

	// Solta A: o Enter de A sai antes da colagem de B.
	fs.release <- struct{}{}
	fs.waitEntered(t)
	eventually(t, "Enter de A antes da colagem de B", func() bool {
		return strings.Contains(snapStr(tm), "^[[200~AAAA^[[201~^M^[[200~BBBB^[[201~")
	})
	fs.release <- struct{}{}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	// Cada marcador de fim vem seguido do Enter da própria sequência.
	eventually(t, "Enter de B", func() bool {
		return strings.Contains(snapStr(tm), "^[[200~AAAA^[[201~^M^[[200~BBBB^[[201~^M")
	})
}

// Simula o run no meio do encerramento (PTY fechado, done ainda aberto): o
// erro de escrita vira ErrEnded. O processo ignora SIGHUP para não terminar
// antes da hora e fechar done.
func TestWriteComPTYFechadoDevolveErrEnded(t *testing.T) {
	m := NewManager(Options{PasteSettle: time.Millisecond})
	t.Cleanup(m.Close)
	tm, err := m.Start(StartSpec{Argv: []string{"/bin/sh", "-c", "trap '' HUP; echo pronto; exec sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "sleep pronto", func() bool { return strings.Contains(snapStr(tm), "pronto") })
	tm.mu.Lock()
	tm.ptmxClosed = true
	_ = tm.ptmx.Close()
	tm.mu.Unlock()
	if tm.ended() {
		t.Fatal("terminal já encerrado: o teste não cobre a janela")
	}
	if err := tm.Write([]byte("x")); err != ErrEnded {
		t.Fatalf("Write: err = %v, quero ErrEnded", err)
	}
	if err := tm.Submit("x"); err != ErrEnded {
		t.Fatalf("Submit: err = %v, quero ErrEnded", err)
	}
}

func TestSubmitEncerrado(t *testing.T) {
	m, tm := startCatV(t)
	if err := m.Kill(tm.Info().ID); err != nil {
		t.Fatal(err)
	}
	waitDone(t, tm)
	if err := tm.Submit("x"); err != ErrEnded {
		t.Fatalf("err = %v, quero ErrEnded", err)
	}
}
