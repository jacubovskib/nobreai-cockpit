// Package proc identifica o processo do agente: pelo /proc no Linux e pelo
// sysctl no macOS (native_darwin.go).
package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const maxDepth = 64

// FindAgentAncestor sobe a árvore de processos a partir de start e devolve o
// primeiro PID cujo comm esteja em names.
func FindAgentAncestor(root string, start int, names []string) (int, error) {
	pid := start
	for i := 0; i < maxDepth && pid > 0; i++ {
		got := Names(root, pid)
		if got == nil {
			break
		}
		for _, n := range got {
			if slices.Contains(names, n) {
				return pid, nil
			}
		}
		ppid, err := parentPID(root, pid)
		if err != nil {
			break
		}
		pid = ppid
	}
	return 0, fmt.Errorf("nenhum processo de agente encontrado acima de %d", start)
}

// parentPID lê o ppid (campo 4) de <root>/<pid>/stat. O comm fica entre
// parênteses e pode conter espaços; por isso usa o texto após o último ')'.
func parentPID(root string, pid int) (int, error) {
	if useNative(root) {
		return native.parent(pid)
	}
	b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, fmt.Errorf("stat inválido para %d", pid)
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return 0, fmt.Errorf("stat inválido para %d", pid)
	}
	return strconv.Atoi(f[1]) // f[0] é o estado; f[1] é o ppid
}

// Comm devolve o nome do processo (<root>/<pid>/comm), "" se não der para
// ler. No macOS (root == Root) é o primeiro de Names.
func Comm(root string, pid int) string {
	if pid <= 0 {
		return ""
	}
	if useNative(root) {
		if n := native.names(pid); len(n) > 0 {
			return n[0]
		}
		return ""
	}
	b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(b), "\n")
}

// Alive informa se existe <root>/<pid> (no macOS, com root == Root, se o
// processo existe).
func Alive(root string, pid int) bool {
	if pid <= 0 {
		return false
	}
	if useNative(root) {
		return native.alive(pid)
	}
	_, err := os.Stat(filepath.Join(root, strconv.Itoa(pid)))
	return err == nil
}
