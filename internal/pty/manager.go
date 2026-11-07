package pty

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	cpty "github.com/creack/pty"
)

const (
	defaultRingSize  = 2 << 20
	defaultMaxLive   = 20
	defaultRetention = 30 * time.Minute
	subBuffer        = 256
	killGrace        = 3 * time.Second
	drainGrace       = 2 * time.Second
)

var (
	// ErrTooMany indica que o limite de terminais vivos foi atingido.
	ErrTooMany = errors.New("limite de terminais atingido")
	// ErrNotFound indica id de terminal desconhecido (ou já descartado).
	ErrNotFound = errors.New("terminal não encontrado")
	// ErrEnded indica operação de entrada em terminal já encerrado.
	ErrEnded = errors.New("terminal encerrado")
)

// Options configura o Manager; zero usa os padrões.
type Options struct {
	RingSize  int
	MaxLive   int
	Retention time.Duration
	Now       func() time.Time
}

// StartSpec descreve o processo a rodar no PTY. Origin: "plataforma" | "nobre run".
type StartSpec struct {
	Argv                   []string
	Dir                    string
	Env                    []string
	Cols, Rows             uint16
	CardKey, Agent, Origin string
}

// Info é a visão pública de um terminal.
type Info struct {
	ID, CardKey, Agent, Origin, Title string
	PID                               int
	StartedAt                         time.Time
	EndedAt                           *time.Time
	ExitCode                          *int
	Cols, Rows                        uint16
	Clients                           int
}

// Manager mantém os terminais vivos e os recém-encerrados.
type Manager struct {
	opts  Options
	mu    sync.Mutex
	terms map[string]*Terminal
	wg    sync.WaitGroup
}

// NewManager cria um Manager aplicando os padrões.
func NewManager(opts Options) *Manager {
	if opts.RingSize <= 0 {
		opts.RingSize = defaultRingSize
	}
	if opts.MaxLive <= 0 {
		opts.MaxLive = defaultMaxLive
	}
	if opts.Retention <= 0 {
		opts.Retention = defaultRetention
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Manager{opts: opts, terms: map[string]*Terminal{}}
}

// Start inicia o processo num novo PTY (em sessão própria).
func (m *Manager) Start(spec StartSpec) (*Terminal, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("argv vazio")
	}
	if spec.Cols == 0 {
		spec.Cols = 80
	}
	if spec.Rows == 0 {
		spec.Rows = 24
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	live := 0
	for _, t := range m.terms {
		if !t.ended() {
			live++
		}
	}
	if live >= m.opts.MaxLive {
		return nil, ErrTooMany
	}
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	} else {
		cmd.Env = os.Environ()
	}
	// StartWithSize já define Setsid+Setctty.
	ptmx, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: spec.Cols, Rows: spec.Rows})
	if err != nil {
		return nil, err
	}
	title := spec.CardKey
	if title == "" {
		title = spec.Agent
	}
	if title == "" {
		title = filepath.Base(spec.Argv[0])
	}
	t := &Terminal{
		ptmx: ptmx, cmd: cmd, ring: NewRing(m.opts.RingSize), now: m.opts.Now,
		subs: map[*sub]struct{}{}, done: make(chan struct{}),
		info: Info{
			ID: newID(), CardKey: spec.CardKey, Agent: spec.Agent, Origin: spec.Origin,
			Title: title, PID: cmd.Process.Pid, StartedAt: m.opts.Now(),
			Cols: spec.Cols, Rows: spec.Rows,
		},
	}
	m.terms[t.info.ID] = t
	m.wg.Add(1)
	go func() { defer m.wg.Done(); t.run() }()
	return t, nil
}

func newID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "t-" + hex.EncodeToString(b[:])
}

// pruneLocked descarta terminais encerrados há mais que a retenção.
func (m *Manager) pruneLocked() {
	now := m.opts.Now()
	for id, t := range m.terms {
		if end := t.endedAt(); end != nil && now.Sub(*end) > m.opts.Retention {
			delete(m.terms, id)
		}
	}
}

// Get devolve o terminal pelo id.
func (m *Manager) Get(id string) (*Terminal, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	t, ok := m.terms[id]
	return t, ok
}

// List devolve os terminais (vivos e retidos), do mais antigo ao mais novo.
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	out := make([]Info, 0, len(m.terms))
	for _, t := range m.terms {
		out = append(out, t.Info())
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].StartedAt.Before(out[j-1].StartedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Kill envia SIGTERM ao grupo do processo e SIGKILL após 3 s se ainda vivo.
// Terminal já encerrado: sem efeito.
func (m *Manager) Kill(id string) error {
	t, ok := m.Get(id)
	if !ok {
		return ErrNotFound
	}
	m.kill(t)
	return nil
}

func (m *Manager) kill(t *Terminal) {
	if t.ended() {
		return
	}
	_ = syscall.Kill(-t.info.PID, syscall.SIGTERM)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		timer := time.NewTimer(killGrace)
		defer timer.Stop()
		select {
		case <-t.done:
		case <-timer.C:
			_ = syscall.Kill(-t.info.PID, syscall.SIGKILL)
		}
	}()
}

// Close mata todos os terminais vivos e espera as goroutines terminarem.
func (m *Manager) Close() {
	m.mu.Lock()
	for _, t := range m.terms {
		m.kill(t)
	}
	m.mu.Unlock()
	m.wg.Wait()
}

type sub struct{ ch chan []byte }

// Terminal é um processo rodando num PTY.
type Terminal struct {
	ptmx *os.File
	cmd  *exec.Cmd
	ring *Ring
	now  func() time.Time
	done chan struct{}

	mu   sync.Mutex
	info Info
	subs map[*sub]struct{}
}

func (t *Terminal) ended() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *Terminal) endedAt() *time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.info.EndedAt
}

// run é a única dona do ciclo de vida: lê o PTY, reapa o processo, fecha tudo.
func (t *Terminal) run() {
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := t.ptmx.Read(buf)
			if n > 0 {
				t.publish(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	werr := t.cmd.Wait()
	code := t.cmd.ProcessState.ExitCode()
	_ = werr

	// Deixa o leitor esgotar o que sobrou; se um neto segurar o PTY, fecha à força.
	select {
	case <-readDone:
	case <-time.After(drainGrace):
	}
	_ = t.ptmx.Close()
	<-readDone

	end := t.now()
	t.mu.Lock()
	t.info.EndedAt = &end
	t.info.ExitCode = &code
	for s := range t.subs {
		close(s.ch)
	}
	t.subs = map[*sub]struct{}{}
	t.mu.Unlock()
	close(t.done)
}

// publish grava no Ring e distribui sem bloquear; assinante cheio é desconectado.
func (t *Terminal) publish(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ring.Write(p)
	if len(t.subs) == 0 {
		return
	}
	for s := range t.subs {
		c := append([]byte(nil), p...)
		select {
		case s.ch <- c:
		default:
			close(s.ch)
			delete(t.subs, s)
		}
	}
}

// Subscribe devolve o histórico recente e um canal com a saída nova. O canal
// fecha quando o terminal encerra ou o assinante é lento demais.
func (t *Terminal) Subscribe() ([]byte, <-chan []byte, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	snap := t.ring.Snapshot()
	s := &sub{ch: make(chan []byte, subBuffer)}
	if t.info.EndedAt != nil {
		close(s.ch)
		return snap, s.ch, func() {}
	}
	t.subs[s] = struct{}{}
	cancel := func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if _, ok := t.subs[s]; ok {
			delete(t.subs, s)
			close(s.ch)
		}
	}
	return snap, s.ch, cancel
}

// Write envia entrada ao processo.
func (t *Terminal) Write(p []byte) error {
	if t.ended() {
		return ErrEnded
	}
	_, err := t.ptmx.Write(p)
	return err
}

// Resize ajusta o tamanho do PTY.
func (t *Terminal) Resize(cols, rows uint16) error {
	if t.ended() {
		return ErrEnded
	}
	if err := cpty.Setsize(t.ptmx, &cpty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return err
	}
	t.mu.Lock()
	t.info.Cols, t.info.Rows = cols, rows
	t.mu.Unlock()
	return nil
}

// Done fecha quando o processo terminou e a saída foi toda lida.
func (t *Terminal) Done() <-chan struct{} { return t.done }

// Info devolve uma cópia do estado atual.
func (t *Terminal) Info() Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.info
	i.Clients = len(t.subs)
	if i.EndedAt != nil {
		e := *i.EndedAt
		i.EndedAt = &e
	}
	if i.ExitCode != nil {
		c := *i.ExitCode
		i.ExitCode = &c
	}
	return i
}
