package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/version"
)

func runVersion(t *testing.T, cliVer, addr string) (string, int) {
	t.Helper()
	old := version.Version
	version.Version = cliVer
	t.Cleanup(func() { version.Version = old })
	var out, errb bytes.Buffer
	c := &cli{in: strings.NewReader(""), out: &out, err: &errb,
		getenv: func(k string) string {
			if k == "NOBRE_ADDR" {
				return addr
			}
			return ""
		},
		cwd: func() (string, error) { return "/w", nil }}
	code := c.run([]string{"version"})
	return out.String(), code
}

func healthFake(t *testing.T, v string) string {
	return fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"version":"` + v + `"}`))
	})
}

func TestCLIVersionIguais(t *testing.T) {
	out, code := runVersion(t, "v0.1.0", healthFake(t, "v0.1.0"))
	if code != 0 || !strings.Contains(out, "nobre v0.1.0") || !strings.Contains(out, "nobred v0.1.0") || strings.Contains(out, "diferem") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestCLIVersionDiferentes(t *testing.T) {
	out, code := runVersion(t, "v0.2.0", healthFake(t, "v0.1.0"))
	if code != 0 || !strings.Contains(out, "As versões diferem") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestCLIVersionDaemonFora(t *testing.T) {
	out, code := runVersion(t, "v0.1.0", "127.0.0.1:1")
	if code != 0 || !strings.Contains(out, "nobre v0.1.0") || !strings.Contains(out, "nobred: não respondeu em 127.0.0.1:1") {
		t.Fatalf("code %d out %q", code, out)
	}
}
