package pty

import (
	"bytes"
	"testing"
)

func TestRingKeepsLastBytes(t *testing.T) {
	const size = 8
	r := NewRing(size)
	var all []byte
	for i := 0; i < 3*size; i++ {
		all = append(all, byte('a'+i%26))
	}
	// escritas de tamanhos variados, atravessando a fronteira
	for off := 0; off < len(all); {
		n := 3
		if off+n > len(all) {
			n = len(all) - off
		}
		r.Write(all[off : off+n])
		off += n
	}
	if got, want := r.Snapshot(), all[len(all)-size:]; !bytes.Equal(got, want) {
		t.Fatalf("snapshot = %q, quer %q", got, want)
	}
}

func TestRingPartialAndOversizedWrite(t *testing.T) {
	r := NewRing(4)
	r.Write([]byte("ab"))
	if got := string(r.Snapshot()); got != "ab" {
		t.Fatalf("parcial = %q", got)
	}
	r.Write([]byte("0123456789"))
	if got := string(r.Snapshot()); got != "6789" {
		t.Fatalf("maior que o buffer = %q", got)
	}
}

func TestAllowedEnv(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_X", "AWS_REGION", "GOOGLE_APPLICATION_CREDENTIALS", "VERTEX_X", "OTEL_X", "HTTPS_PROXY", "no_proxy", "LANG", "TZ", "LC_ALL"} {
		if !AllowedEnv(k) {
			t.Errorf("%s deveria ser permitida", k)
		}
	}
	for _, k := range []string{"PATH", "LD_PRELOAD", "NOBRE_RESUME", "HOME", "ANTHROPIC", "LANGUAGE", "claude_x", "TZ=x", ""} {
		if AllowedEnv(k) {
			t.Errorf("%s não deveria ser permitida", k)
		}
	}
}
