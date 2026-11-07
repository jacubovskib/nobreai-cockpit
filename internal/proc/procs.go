package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Proc é um processo lido de /proc. Ticks soma o tempo de CPU do processo e
// dos filhos já colhidos (utime+stime+cutime+cstime, em clock ticks): assim
// os processos curtos de um build aparecem no pai quando terminam. Start é o
// starttime (ticks desde o boot), que distingue um PID reaproveitado.
type Proc struct {
	PID   int
	PPID  int
	Comm  string
	Cwd   string
	Ticks int64
	Start int64
	RSS   int64 // bytes residentes
}

// ReadProcs lista os processos de root (normalmente /proc) com cwd legível.
// Processos de outros usuários e threads do kernel ficam de fora; um processo
// que sumiu durante a leitura é ignorado.
func ReadProcs(root string) []Proc {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	page := int64(os.Getpagesize())
	var out []Proc
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		dir := filepath.Join(root, e.Name())
		cwd, err := os.Readlink(filepath.Join(dir, "cwd"))
		if err != nil || cwd == "" {
			continue
		}
		p, ok := readStat(dir)
		if !ok {
			continue
		}
		p.PID, p.Cwd = pid, strings.TrimSuffix(cwd, " (deleted)")
		if b, err := os.ReadFile(filepath.Join(dir, "statm")); err == nil {
			if f := strings.Fields(string(b)); len(f) > 1 {
				if n, err := strconv.ParseInt(f[1], 10, 64); err == nil {
					p.RSS = n * page
				}
			}
		}
		out = append(out, p)
	}
	return out
}

// readStat lê comm, ppid, tempos e starttime de <dir>/stat. O comm fica entre
// parênteses e pode ter espaços e parênteses: os campos vêm após o último ')'.
func readStat(dir string) (Proc, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return Proc{}, false
	}
	s := string(b)
	open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || close < open {
		return Proc{}, false
	}
	f := strings.Fields(s[close+1:])
	if len(f) < 20 {
		return Proc{}, false
	}
	num := func(i int) int64 { n, _ := strconv.ParseInt(f[i], 10, 64); return n }
	return Proc{
		Comm:  s[open+1 : close],
		PPID:  int(num(1)),
		Ticks: num(11) + num(12) + num(13) + num(14),
		Start: num(19),
	}, true
}
