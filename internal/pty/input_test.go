package pty

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCleanInput(t *testing.T) {
	casos := []struct{ in, want string }{
		{"a\r\nb\rc", "a\nb\nc"},
		{"x\x1b[201~y\x1b[200~z", "xyz"},
		{"\x1b[20\x1b[201~1~fim", "fim"},
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

func TestSubmitNaoIntercalaSequencias(t *testing.T) {
	_, tm := startCatV(t)
	var wg sync.WaitGroup
	for _, txt := range []string{"AAAA", "BBBB"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tm.Submit(txt); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	eventually(t, "as duas colagens", func() bool {
		s := snapStr(tm)
		return strings.Count(s, "^[[201~") == 2
	})
	s := snapStr(tm)
	// A ordem das sequências é livre; cada uma tem de vir inteira antes da outra.
	ok := strings.Contains(s, "^[[200~AAAA^[[201~") && strings.Contains(s, "^[[200~BBBB^[[201~")
	if !ok {
		t.Fatalf("sequências intercaladas: %q", s)
	}
}

func TestSubmitEsperaEntreSequencias(t *testing.T) {
	_, tm := startCatV(t)
	// Observa a saída com carimbo de tempo por chegada.
	_, out, unsub := tm.Subscribe()
	defer unsub()
	type ev struct {
		at time.Time
		s  string
	}
	var mu sync.Mutex
	var evs []ev
	var acc strings.Builder
	go func() {
		for b := range out {
			mu.Lock()
			acc.Write(b)
			evs = append(evs, ev{time.Now(), acc.String()})
			mu.Unlock()
		}
	}()
	var wg sync.WaitGroup
	for _, txt := range []string{"AAAA", "BBBB"} {
		wg.Add(1)
		go func() { defer wg.Done(); _ = tm.Submit(txt) }()
	}
	wg.Wait()
	eventually(t, "duas colagens", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Count(acc.String(), "^[[201~") == 2
	})
	mu.Lock()
	defer mu.Unlock()
	var fim, ini time.Time
	for _, e := range evs {
		if fim.IsZero() && strings.Contains(e.s, "^[[201~") {
			fim = e.at
		}
		if ini.IsZero() && strings.Count(e.s, "^[[200~") == 2 {
			ini = e.at
		}
	}
	if d := ini.Sub(fim); d < 15*time.Millisecond {
		t.Fatalf("segunda sequência começou %v depois do fim da primeira; quero >= 15ms", d)
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
