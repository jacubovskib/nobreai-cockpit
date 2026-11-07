package resources

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/proc"
)

// clockTicks é o USER_HZ do Linux: os tempos de /proc/<pid>/stat vêm nessa
// unidade (100 em todas as arquiteturas comuns).
const clockTicks = 100

type procKey struct {
	pid   int
	start int64
}

// prevProc é o que a amostra anterior guardou de cada processo.
type prevProc struct {
	ticks int64
	ppid  int
}

// Sampler lê /proc periodicamente e calcula a CPU de cada processo pela
// diferença de ticks entre duas amostras.
type Sampler struct {
	root string
	now  func() time.Time

	mu    sync.Mutex
	procs []Proc
	ready bool

	// Estado só do laço de amostragem (Sample não roda em paralelo).
	prev       map[procKey]prevProc
	prevAt     time.Time
	prevUptime float64
}

// NewSampler cria o amostrador de root (normalmente /proc). now nil = time.Now.
func NewSampler(root string, now func() time.Time) *Sampler {
	if now == nil {
		now = time.Now
	}
	return &Sampler{root: root, now: now}
}

// Snapshot devolve os processos da última amostra; ready fica falso até haver
// duas amostras (sem elas não há como medir CPU).
func (s *Sampler) Snapshot() ([]Proc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.procs, s.ready
}

// Run amostra a cada interval até ctx terminar.
func (s *Sampler) Run(ctx context.Context, interval time.Duration) {
	s.Sample()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sample()
		}
	}
}

// Sample faz uma leitura de /proc. A CPU de um processo é o aumento dos seus
// ticks (dele e dos filhos colhidos) desde a amostra anterior. Processo novo
// que nasceu depois dela conta todos os ticks; um que já existia mas não foi
// visto (cwd ilegível antes) conta zero nesta rodada.
func (s *Sampler) Sample() {
	raw := proc.ReadProcs(s.root)
	at, uptime := s.now(), readUptime(s.root)
	elapsed := at.Sub(s.prevAt).Seconds()
	first := s.prev == nil || elapsed <= 0

	next := make(map[procKey]prevProc, len(raw))
	for _, r := range raw {
		next[procKey{r.PID, r.Start}] = prevProc{ticks: r.Ticks, ppid: r.PPID}
	}
	// Filho que já vinha sendo contado e terminou: ao ser colhido, o pai
	// recebe no cutime a vida inteira dele. Desconta do pai o que o filho já
	// tinha na amostra anterior, para não contar duas vezes.
	reaped := map[int]int64{}
	for k, pp := range s.prev {
		if _, alive := next[k]; !alive {
			reaped[pp.ppid] += pp.ticks
		}
	}
	out := make([]Proc, 0, len(raw))
	for _, r := range raw {
		k := procKey{r.PID, r.Start}
		p := Proc{PID: r.PID, PPID: r.PPID, Comm: r.Comm, Cwd: r.Cwd, RSS: r.RSS}
		if !first {
			var delta int64
			if before, ok := s.prev[k]; ok {
				delta = r.Ticks - before.ticks - reaped[r.PID]
			} else if s.prevUptime > 0 && float64(r.Start)/clockTicks >= s.prevUptime {
				delta = r.Ticks
			}
			if delta > 0 {
				p.CPU = float64(delta) / clockTicks / elapsed * 100
			}
		}
		out = append(out, p)
	}
	s.prev, s.prevAt, s.prevUptime = next, at, uptime

	s.mu.Lock()
	s.procs, s.ready = out, !first
	s.mu.Unlock()
}

// readUptime lê os segundos desde o boot de <root>/uptime (0 se ilegível).
func readUptime(root string) float64 {
	b, err := os.ReadFile(filepath.Join(root, "uptime"))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// Monitor junta o amostrador de processos e o medidor de disco no formato que
// a API consome.
type Monitor struct {
	Sampler *Sampler
	Meter   *DiskMeter
}

func (m Monitor) Snapshot() ([]Proc, bool) { return m.Sampler.Snapshot() }

func (m Monitor) Disk(path string, exclude []string) DiskInfo { return m.Meter.Disk(path, exclude) }
