package proc

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"slices"
)

// Root é a raiz do /proc do sistema. Os chamadores passam Root para ler os
// processos reais e um diretório temporário nos testes. Num sistema sem
// /proc (macOS), Root faz as funções consultarem o kernel direto (native);
// qualquer outra raiz continua lida como arquivos, em todo sistema.
const Root = "/proc"

// nativeOps consulta os processos sem /proc. nil = o sistema tem /proc.
type nativeOps struct {
	// names devolve os nomes pelos quais o processo responde: o comm do
	// kernel, o nome do executável e o do argv[0] (o instalador do Claude
	// Code põe o binário num arquivo com o nome da versão atrás do link
	// "claude"). nil = o processo não existe ou não dá para ler.
	names  func(pid int) []string
	parent func(pid int) (int, error)
	alive  func(pid int) bool
}

var native *nativeOps

func useNative(root string) bool { return native != nil && root == Root }

// Names devolve os nomes pelos quais o processo pid responde: no Linux, o
// comm; no macOS (root == Root), também o executável e o argv[0]. nil = não
// deu para ler.
func Names(root string, pid int) []string {
	if pid <= 0 {
		return nil
	}
	if useNative(root) {
		return native.names(pid)
	}
	if c := Comm(root, pid); c != "" {
		return []string{c}
	}
	return nil
}

// HasName diz se o processo pid responde por algum dos nomes em names.
func HasName(root string, pid int, names []string) bool {
	for _, n := range Names(root, pid) {
		if slices.Contains(names, n) {
			return true
		}
	}
	return false
}

// parseProcArgs lê o buffer do sysctl kern.procargs2 do macOS: argc (int32
// em ordem nativa, little-endian em amd64 e arm64), o caminho do executável
// terminado em NUL, NULs de alinhamento e então os argumentos. Devolve o
// executável e o argv[0] ("" quando ausentes).
func parseProcArgs(b []byte) (exe, argv0 string) {
	if len(b) < 4 {
		return "", ""
	}
	argc := binary.LittleEndian.Uint32(b[:4])
	rest := b[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return string(rest), ""
	}
	exe, rest = string(rest[:i]), rest[i:]
	rest = bytes.TrimLeft(rest, "\x00")
	if argc == 0 || len(rest) == 0 {
		return exe, ""
	}
	if j := bytes.IndexByte(rest, 0); j >= 0 {
		rest = rest[:j]
	}
	return exe, string(rest)
}

// baseNames junta os nomes-base não vazios e distintos de paths.
func baseNames(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if b := filepath.Base(p); b != "." && b != "/" && !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	return out
}
