package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
	buildDir  string
)

// realNobre compila o nobre de verdade uma vez para os testes de pipe.
func realNobre(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "nobre-bin-")
		if buildErr != nil {
			return
		}
		builtBin = filepath.Join(buildDir, "nobre")
		out, err := exec.Command("go", "build", "-o", builtBin, ".").CombinedOutput()
		if err != nil {
			buildErr = &buildFail{err, string(out)}
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}

type buildFail struct {
	err error
	out string
}

func (b *buildFail) Error() string { return b.err.Error() + ": " + b.out }

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

const pipeJSON = `{"session_id":"sp","workspace":{"current_dir":"/tmp/proj"},"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":1790000000}}}`

const userOrig = `input=$(cat); cwd=$(echo "$input" | jq -r '.workspace.current_dir'); dir="${cwd/#$HOME/~}"; printf '\033[01;32m%s@%s\033[00m:\033[01;34m%s\033[00m' "$(whoami)" "$(hostname -s)" "$dir"`

func runShell(t *testing.T, shell, cmd, stdin string, env []string) (string, int) {
	t.Helper()
	c := exec.Command(shell, "-c", cmd)
	c.Stdin = strings.NewReader(stdin) // pipe real
	c.Env = append(os.Environ(), env...)
	out, err := c.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestStatuslineRealBinaryInRealPipeUnderShells(t *testing.T) {
	bin := realNobre(t)
	_, jqErr := exec.LookPath("jq")
	cases := []struct {
		name, orig string
		wantPost   bool
		needJQ     bool
	}{
		{"original do usuário", userOrig, true, true},
		{"não lê o stdin", "echo hi", true, false},
		{"comentário colado", "echo hi;#c", false, false},
		{"subshell com comentário", "(echo sub)#c", false, false},
	}
	for _, shell := range []string{"bash", "dash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			t.Logf("%s ausente: pulando", shell)
			continue
		}
		for _, tc := range cases {
			t.Run(shell+"/"+tc.name, func(t *testing.T) {
				if tc.needJQ && jqErr != nil {
					t.Skip("jq não instalado")
				}
				posts := make(chan string, 4)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					posts <- r.URL.Path + " " + string(b)
					w.WriteHeader(204)
				}))
				defer srv.Close()
				env := []string{"NOBRE_ADDR=" + strings.TrimPrefix(srv.URL, "http://")}
				wantOut, wantCode := runShell(t, shell, tc.orig, pipeJSON, env)
				wrapped := bin + " statusline | {\n" + tc.orig + "\n}"
				gotOut, gotCode := runShell(t, shell, wrapped, pipeJSON, env)
				if gotOut != wantOut || gotCode != wantCode {
					t.Fatalf("(%q, %d) != original (%q, %d)", gotOut, gotCode, wantOut, wantCode)
				}
				if !tc.wantPost {
					return
				}
				select {
				case p := <-posts:
					if !strings.HasPrefix(p, "/api/plan-limits ") || !strings.Contains(p, `"used_percentage":12`) {
						t.Fatalf("POST = %s", p)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("o daemon não recebeu o POST")
				}
			})
		}
	}
}

// O processo pai volta já, mesmo com o daemon pendurado: o POST é destacado.
func TestStatuslineParentReturnsFastWithHungDaemon(t *testing.T) {
	bin := realNobre(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	c := exec.Command(bin, "statusline")
	c.Stdin = strings.NewReader(pipeJSON)
	c.Env = append(os.Environ(), "NOBRE_ADDR="+strings.TrimPrefix(srv.URL, "http://"))
	start := time.Now()
	out, err := c.Output()
	if d := time.Since(start); err != nil || string(out) != pipeJSON || d > 100*time.Millisecond {
		t.Fatalf("out=%q err=%v dur=%v", out, err, d)
	}
}

func TestStatuslineHelpDoesNotReadStdin(t *testing.T) {
	r := runCLIReader(t, "127.0.0.1:1", blockingReader{}, nil, "statusline", "--help")
	if r.code != 0 || !strings.Contains(r.stdout, "nobre statusline") {
		t.Fatalf("%+v", r)
	}
}

type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }
