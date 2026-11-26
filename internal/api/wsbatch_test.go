package api

import (
	"context"
	"testing"
	"time"
)

func recvChunk(t *testing.T, ch <-chan []byte) ([]byte, bool) {
	t.Helper()
	select {
	case b, ok := <-ch:
		return b, ok
	case <-time.After(2 * time.Second):
		t.Fatal("sem bloco no prazo")
		return nil, false
	}
}

func TestBatchChunksJoinsWithinWindow(t *testing.T) {
	in := make(chan []byte)
	out := batchChunks(context.Background(), in, 50*time.Millisecond)
	for _, s := range []string{"a", "b", "c"} {
		in <- []byte(s)
	}
	b, ok := recvChunk(t, out)
	if !ok || string(b) != "abc" {
		t.Fatalf("bloco = %q ok=%v, queria abc", b, ok)
	}
}

func TestBatchChunksSeparateWindowsKeepOrder(t *testing.T) {
	in := make(chan []byte)
	out := batchChunks(context.Background(), in, 30*time.Millisecond)
	in <- []byte("um")
	first, _ := recvChunk(t, out)
	in <- []byte("dois")
	second, _ := recvChunk(t, out)
	if string(first) != "um" || string(second) != "dois" {
		t.Fatalf("blocos = %q, %q", first, second)
	}
}

func TestBatchChunksFlushesPendingOnClose(t *testing.T) {
	in := make(chan []byte)
	out := batchChunks(context.Background(), in, time.Hour)
	in <- []byte("resto")
	close(in)
	b, ok := recvChunk(t, out)
	if !ok || string(b) != "resto" {
		t.Fatalf("bloco = %q ok=%v", b, ok)
	}
	if _, ok := recvChunk(t, out); ok {
		t.Fatal("saída deveria fechar depois do resto")
	}
}

func TestBatchChunksCopiesBytes(t *testing.T) {
	in := make(chan []byte)
	out := batchChunks(context.Background(), in, 30*time.Millisecond)
	buf := []byte("x")
	in <- buf
	in <- []byte("y") // enviado sem buffer: o x já foi copiado
	buf[0] = 'z'       // o chamador reusa o slice
	b, _ := recvChunk(t, out)
	if string(b) != "xy" {
		t.Fatalf("bloco = %q, queria xy", b)
	}
}

func TestBatchChunksContextCancelCloses(t *testing.T) {
	in := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	out := batchChunks(ctx, in, time.Hour)
	in <- []byte("pendente")
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("saída não fechou após cancelar")
		}
	}
}
