package resources

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DiskInfo é o espaço que um worktree ocupa. MeasuredAt nil = nunca medido;
// Pending = há uma medição na fila ou em andamento (Bytes, se houver, é a
// anterior).
type DiskInfo struct {
	Bytes      int64      `json:"bytes"`
	MeasuredAt *time.Time `json:"measured_at"`
	Pending    bool       `json:"pending"`
}

type diskEntry struct {
	bytes int64
	at    time.Time
}

// DiskMeter mede diretórios em segundo plano, um por vez, e guarda o
// resultado por ttl. Medir node_modules é caro: a consulta nunca espera.
type DiskMeter struct {
	ttl time.Duration
	now func() time.Time

	mu     sync.Mutex
	cache  map[string]diskEntry
	queued map[string][]string // caminho → exclusões da medição pedida
	queue  chan string
}

// NewDiskMeter cria o medidor; chame Run para processar a fila. now nil = time.Now.
func NewDiskMeter(ttl time.Duration, now func() time.Time) *DiskMeter {
	if now == nil {
		now = time.Now
	}
	return &DiskMeter{ttl: ttl, now: now, cache: map[string]diskEntry{}, queued: map[string][]string{}, queue: make(chan string, 256)}
}

// Disk devolve a última medida de path e, se não houver ou estiver vencida,
// põe uma nova na fila. exclude são subdiretórios que não contam (worktrees
// aninhados).
func (m *DiskMeter) Disk(path string, exclude []string) DiskInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.cache[path]
	if _, inQueue := m.queued[path]; !inQueue && (!ok || m.now().Sub(e.at) > m.ttl) {
		select {
		case m.queue <- path:
			m.queued[path] = exclude
		default: // fila cheia: tenta de novo na próxima consulta
		}
	}
	info := DiskInfo{}
	_, info.Pending = m.queued[path]
	if ok {
		at := e.at
		info.Bytes, info.MeasuredAt = e.bytes, &at
	}
	return info
}

// Run mede os caminhos da fila até ctx terminar.
func (m *DiskMeter) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case path := <-m.queue:
			m.mu.Lock()
			exclude := m.queued[path]
			m.mu.Unlock()
			n := measureDir(path, exclude)
			m.mu.Lock()
			m.cache[path] = diskEntry{bytes: n, at: m.now()}
			delete(m.queued, path)
			m.mu.Unlock()
		}
	}
}

// measureDir soma os blocos alocados sob root, como o du: não segue
// symlinks, não sai do sistema de arquivos de root, conta cada hardlink uma
// vez e pula os diretórios de exclude. Erros de leitura são ignorados.
func measureDir(root string, exclude []string) int64 {
	var rootDev uint64
	var total int64
	seen := map[[2]uint64]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path != root && d.IsDir() && excluded(path, exclude) {
			return fs.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			total += info.Size()
			return nil
		}
		if path == root {
			rootDev = uint64(st.Dev)
		} else if d.IsDir() && uint64(st.Dev) != rootDev {
			return fs.SkipDir
		}
		if st.Nlink > 1 && !d.IsDir() {
			k := [2]uint64{uint64(st.Dev), uint64(st.Ino)}
			if seen[k] {
				return nil
			}
			seen[k] = true
		}
		total += st.Blocks * 512
		return nil
	})
	return total
}

func excluded(path string, exclude []string) bool {
	for _, e := range exclude {
		e = strings.TrimRight(e, "/")
		if path == e || strings.HasPrefix(path, e+"/") {
			return true
		}
	}
	return false
}
