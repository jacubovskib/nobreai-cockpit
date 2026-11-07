// Package resources mede o que os worktrees consomem da máquina: CPU e
// memória dos processos que rodam dentro de cada um (amostrados de /proc) e o
// espaço em disco de cada diretório.
package resources

import (
	"sort"
	"strings"
)

// MaxTop é quantos processos cada worktree lista como os que mais consomem.
const MaxTop = 3

// maxDepth limita a subida pela árvore de processos.
const maxDepth = 64

// Proc é um processo com o consumo da última amostra. CPU é em porcentagem
// de um núcleo (200 = dois núcleos inteiros, como no top).
type Proc struct {
	PID  int     `json:"pid"`
	PPID int     `json:"-"`
	Comm string  `json:"comm"`
	Cwd  string  `json:"-"`
	CPU  float64 `json:"cpu_pct"`
	RSS  int64   `json:"rss_bytes"`
}

// Usage é o consumo somado dos processos de um worktree. RSS soma a memória
// residente de cada processo; páginas compartilhadas contam mais de uma vez.
type Usage struct {
	CPU   float64 `json:"cpu_pct"`
	RSS   int64   `json:"rss_bytes"`
	Procs int     `json:"procs"`
	Top   []Proc  `json:"top"`
}

// Attribute soma o consumo por worktree. Cada processo conta para o worktree
// mais profundo que contém seu cwd; sem cwd em nenhum, herda o worktree do
// ancestral mais próximo que tenha (o go build em /tmp conta para quem o
// chamou). Processos sem worktree ficam de fora. Só aparecem no mapa os
// worktrees com algum processo.
func Attribute(procs []Proc, paths []string) map[string]Usage {
	byPID := make(map[int]Proc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	memo := make(map[int]string, len(procs))
	var owner func(pid, depth int) string
	owner = func(pid, depth int) string {
		if w, ok := memo[pid]; ok {
			return w
		}
		p, ok := byPID[pid]
		if !ok || depth > maxDepth {
			return ""
		}
		memo[pid] = "" // protege contra ciclos de ppid
		w := deepest(paths, p.Cwd)
		if w == "" && p.PPID != pid {
			w = owner(p.PPID, depth+1)
		}
		memo[pid] = w
		return w
	}

	out := map[string]Usage{}
	members := map[string][]Proc{}
	for _, p := range procs {
		w := owner(p.PID, 0)
		if w == "" {
			continue
		}
		u := out[w]
		u.CPU += p.CPU
		u.RSS += p.RSS
		u.Procs++
		out[w] = u
		members[w] = append(members[w], p)
	}
	for w, ps := range members {
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].CPU != ps[j].CPU {
				return ps[i].CPU > ps[j].CPU
			}
			return ps[i].RSS > ps[j].RSS
		})
		u := out[w]
		u.Top = ps[:min(len(ps), MaxTop)]
		out[w] = u
	}
	return out
}

// deepest devolve o caminho mais longo de paths que contém dir (respeitando o
// limite de diretório), ou "".
func deepest(paths []string, dir string) string {
	best := ""
	for _, p := range paths {
		root := strings.TrimRight(p, "/")
		if (dir == root || strings.HasPrefix(dir, root+"/")) && len(root) > len(best) {
			best = root
		}
	}
	return best
}
