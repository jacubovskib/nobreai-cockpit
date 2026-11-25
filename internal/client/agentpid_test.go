package client

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/proc"
)

// TestHelperAgentProcess não é um teste: é o agente falso e o comando nobre
// falso do TestRealAgentChain, escolhidos por NOBRE_HELPER_ROLE.
func TestHelperAgentProcess(t *testing.T) {
	switch os.Getenv("NOBRE_HELPER_ROLE") {
	case "agent":
		// O agente roda o "nobre" por um shell, como os hooks do Claude Code.
		fmt.Printf("agent=%d\n", os.Getpid())
		sh := exec.Command("/bin/sh", "-c", `"$NOBRE_HELPER_BIN" -test.run='^TestHelperAgentProcess$'`)
		sh.Env = append(os.Environ(), "NOBRE_HELPER_ROLE=nobre")
		sh.Stdout, sh.Stderr = os.Stdout, os.Stderr
		if err := sh.Run(); err != nil {
			fmt.Printf("erro=%v\n", err)
		}
		_, _ = io.Copy(io.Discard, os.Stdin) // vivo até o teste fechar o stdin
		os.Exit(0)
	case "nobre":
		fmt.Printf("detectado=%d\n", findAgentPID(proc.Root, os.Getppid(), []string{"claude", "kiro-cli"}))
		os.Exit(0)
	}
}

// TestRealAgentChain usa processos de verdade (sem /proc falso): o agente é
// o binário de teste copiado para versions/9.9.9 e chamado pelo link
// "claude", como o instalador nativo do Claude Code deixa. O "nobre" filho
// (via sh) acha o PID do agente, e o board o vê vivo até ele sair. No macOS
// isso passa pelo sysctl; no Linux, pelo /proc.
func TestRealAgentChain(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("só Linux e macOS")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	versioned := filepath.Join(dir, "versions", "9.9.9")
	if err := os.MkdirAll(filepath.Dir(versioned), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, self, versioned)
	link := filepath.Join(dir, "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versioned, link); err != nil {
		t.Fatal(err)
	}

	agent := exec.Command(link, "-test.run=^TestHelperAgentProcess$")
	agent.Env = append(os.Environ(), "NOBRE_HELPER_ROLE=agent", "NOBRE_HELPER_BIN="+self)
	stdin, err := agent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := agent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	agent.Stderr = os.Stderr
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Process.Kill(); _ = agent.Wait() }()

	vals := map[string]int{}
	sc := bufio.NewScanner(out)
	for len(vals) < 2 && sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			if k == "erro" {
				t.Fatalf("agente: %s", v)
			}
			if n, err := strconv.Atoi(v); err == nil {
				vals[k] = n
			}
		}
	}
	if vals["agent"] != agent.Process.Pid {
		t.Fatalf("agente informou %d, PID real %d (saída %v, %v)", vals["agent"], agent.Process.Pid, vals, sc.Err())
	}
	if vals["detectado"] != agent.Process.Pid {
		t.Fatalf("o nobre achou %d; o agente é %d (nomes do agente: %v)", vals["detectado"], agent.Process.Pid, proc.Names(proc.Root, agent.Process.Pid))
	}

	live := board.Liveness{ProcRoot: proc.Root, Names: map[string][]string{"claude": {"claude"}}}
	sess := domain.Session{Agent: "claude", PID: agent.Process.Pid}
	if !live.Alive(sess) {
		t.Fatalf("o board deveria ver o agente vivo (nomes: %v)", proc.Names(proc.Root, sess.PID))
	}
	if (board.Liveness{ProcRoot: proc.Root, Names: map[string][]string{"claude": {"kiro-cli"}}}).Alive(sess) {
		t.Fatal("com outro nome de agente, o PID não deveria contar como dele")
	}
	_ = stdin.Close()
	if err := agent.Wait(); err != nil {
		t.Fatalf("agente saiu com erro: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for live.Alive(sess) {
		if time.Now().After(deadline) {
			t.Fatal("o board ainda vê o agente vivo depois de ele sair")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatal(err)
	}
}
