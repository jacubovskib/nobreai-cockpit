// Command fakeagent faz o papel do Claude Code no smoke test do macOS
// (scripts/smoke/macos.sh): instalado como versions/<v> atrás de um link
// "claude", como o instalador nativo deixa, roda pelo shell o hook
// SessionStart e os comandos nobre passados nos argumentos, imprime o
// próprio PID e fica vivo até receber um sinal ou o stdin fechar.
//
// Uso: claude <session-id> <comando de shell>...
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: claude <session-id> [comando de shell]...")
		os.Exit(2)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	fmt.Printf("agent_pid=%d\n", os.Getpid())
	payload, _ := json.Marshal(map[string]string{
		"session_id":      os.Args[1],
		"transcript_path": filepath.Join(cwd, ".transcript.jsonl"),
		"cwd":             cwd,
		"hook_event_name": "SessionStart",
		"source":          "startup",
	})
	run(`nobre hook claude SessionStart`, payload)
	for _, cmd := range os.Args[2:] {
		run(cmd, nil)
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// run roda cmd por /bin/sh -c, como o Claude Code roda hooks e comandos.
func run(cmd string, stdin []byte) {
	c := exec.Command("/bin/sh", "-c", cmd)
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		fail(fmt.Errorf("%s: %w", cmd, err))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "fakeagent:", err)
	os.Exit(1)
}
