// Package proc identifica o processo do agente a partir de /proc (Linux).
package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxDepth = 64

// FindAgentAncestor sobe a árvore de processos a partir de start e devolve o
// primeiro PID cujo comm esteja em names.
func FindAgentAncestor(root string, start int, names []string) (int, error) {
	pid := start
	for i := 0; i < maxDepth && pid > 0; i++ {
		comm, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "comm"))
		if err != nil {
			break
		}
		name := strings.TrimSuffix(string(comm), "\n")
		for _, n := range names {
			if n == name {
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

// Alive informa se existe <root>/<pid>.
func Alive(root string, pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(filepath.Join(root, strconv.Itoa(pid)))
	return err == nil
}
