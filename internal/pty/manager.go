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

	"golang.org/x/sys/unix"

	cpty "github.com/creack/pty"
)

const (
	defaultRingSize  = 2 << 20
	defaultMaxLive   = 20
	defaultRetention = 30 * time.Minute
	defaultSubLimit  = 4 << 20
	defaultWriteTO   = 5 * time.Second
	killGrace        = 3 * time.Second
	drainGrace       = 2 * time.Second
	holdGrace        = 2 * time.Second
)

// Motivos de encerramento (Info.EndReason).
const (
	EndAgent  = "agent"  // o processo terminou sozinho
	EndKilled = "killed" // Kill (DELETE na API)
	EndDaemon = "daemon" // Close no desligamento do nobred
)

var (
	// ErrTooMany indica que o limite de terminais vivos foi atingido.
	ErrTooMany = errors.New("limite de terminais atingido")
	// ErrNotFound indica id de terminal desconhecido (ou já descartado).
	ErrNotFound = errors.New("terminal não encontrado")
	// ErrEnded indica operação de entrada em terminal já encerrado.
	ErrEnded = errors.New("terminal encerrado")
)

// Options configura o Manager; zero usa os padrões. SubLimit é o máximo de
// bytes pendentes por assinante (4 MiB); WriteTimeout, o prazo de cada
// escrita de entrada (5 s).
type Options struct {
	RingSize     int
	MaxLive      int
	Retention    time.Duration
	SubLimit     int
	WriteTimeout time.Duration
	Now          func() time.Time
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
	ID      string `json:"id"`
	CardKey string `json:"card_key"`
	Agent   string `json:"agent"`
	Origin  string `json:"origin"`
	Title   string `json:"title"`
	// Dir: diretório de trabalho do processo (a UI deduz o projeto por ele).
	Dir       string     `json:"dir"`
	PID       int        `json:"pid"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	ExitCode  *int       `json:"exit_code"`
	Cols      uint16     `json:"cols"`
	Rows      uint16     `json:"rows"`
	Clients   int        `json:"clients"`
	// EndReason: "agent", "killed" ou "daemon" depois do fim; vazio enquanto vivo.
	EndReason string `json:"end_reason,omitempty"`
}

// Manager mantém os terminais vivos e os recém-encerrados.
type Manager struct {
	opts  Options
	mu    sync.Mutex
	terms map[string]*Terminal
	wg    sync.WaitGroup
	// holds são clientes entregando a mensagem de fim; Close espera por eles.
	holds sync.WaitGroup
	// closed é protegido por mu: depois de Close, Start devolve ErrEnded.
	closed bool
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
	if opts.SubLimit <= 0 {
		opts.SubLimit = defaultSubLimit
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = defaultWriteTO
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
	if m.closed {
		return nil, ErrEnded
	}
	m.pruneLocked()
	if m.fullLocked() {
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
	ptmx, err = pollable(ptmx)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
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
		subLimit: m.opts.SubLimit, writeTimeout: m.opts.WriteTimeout,
		subs: map[*sub]struct{}{}, done: make(chan struct{}),
		info: Info{
			ID: newID(), CardKey: spec.CardKey, Agent: spec.Agent, Origin: spec.Origin, Dir: spec.Dir,
			Title: title, PID: cmd.Process.Pid, StartedAt: m.opts.Now(),
			Cols: spec.Cols, Rows: spec.Rows,
		},
	}
	m.terms[t.info.ID] = t
	m.wg.Add(1)
	go func() { defer m.wg.Done(); t.run() }()
	return t, nil
}

// Full diz se um Start agora recusaria por limite (ErrTooMany). Só consulta:
// outro Start pode ocupar a vaga entre esta checagem e o seu.
func (m *Manager) Full() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fullLocked()
}

// fullLocked conta os terminais vivos contra o limite; exige mu.
func (m *Manager) fullLocked() bool {
	live := 0
	for _, t := range m.terms {
		if !t.ended() {
			live++
		}
	}
	return live >= m.opts.MaxLive
}

// pollable recria o master em modo não bloqueante: assim Close interrompe o
// leitor e Write aceita prazo. (As ioctls do creack/pty chamam Fd(), que deixa
// o arquivo bloqueante; por isso o redimensionamento usa SyscallConn.)
func pollable(f *os.File) (*os.File, error) {
	nfd, err := unix.FcntlInt(f.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	name := f.Name()
	_ = f.Close()
	if err := syscall.SetNonblock(nfd, true); err != nil {
		_ = syscall.Close(nfd)
		return nil, err
	}
	return os.NewFile(uintptr(nfd), name), nil
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
	m.kill(t, EndKilled)
	return nil
}

func (m *Manager) kill(t *Terminal, reason string) {
	if t.ended() {
		return
	}
	t.mu.Lock()
	if t.reason == "" {
		t.reason = reason
	}
	t.mu.Unlock()
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

// Close mata todos os terminais vivos, espera as goroutines terminarem e dá
// até 2 s para os clientes (Hold) entregarem a mensagem de fim. Idempotente.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, t := range m.terms {
		m.kill(t, EndDaemon)
	}
	m.mu.Unlock()
	m.wg.Wait()
	held := make(chan struct{})
	go func() { m.holds.Wait(); close(held) }()
	select {
	case <-held:
	case <-time.After(holdGrace):
	}
}

// Hold registra um cliente conectado (ex.: WebSocket) para Close esperá-lo
// entregar a saída final; chame release ao terminar. ok=false depois de Close.
func (m *Manager) Hold() (release func(), ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return func() {}, false
	}
	m.holds.Add(1)
	var once sync.Once
	return func() { once.Do(m.holds.Done) }, true
}

// sub é um assinante: a fila (bytes contados) fica sob Terminal.mu e uma
// goroutine (pump) a entrega no canal out, sem nunca bloquear o leitor do PTY.
type sub struct {
	out      chan []byte
	queue    [][]byte
	pending  int
	ended    bool          // entregar o resto e fechar out
	wake     chan struct{} // cap 1
	stop     chan struct{} // fechado ao cancelar ou desconectar por lentidão
	stopOnce sync.Once
}

func (s *sub) halt() { s.stopOnce.Do(func() { close(s.stop) }) }
func (s *sub) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Terminal é um processo rodando num PTY.
type Terminal struct {
	ptmx *os.File
	cmd  *exec.Cmd
	ring *Ring
	now  func() time.Time
	done chan struct{}

	subLimit     int
	writeTimeout time.Duration

	mu     sync.Mutex
	info   Info
	reason string // motivo pedido por Kill/Close (sob mu)
	subs   map[*sub]struct{}
	// ptmxClosed (sob mu) evita Setsize concorrente com o Close do ptmx.
	ptmxClosed bool
	wmu        sync.Mutex // serializa Write (prazo por escrita)
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
	_ = werr
	code := t.cmd.ProcessState.ExitCode()
	if ws, ok := t.cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		code = 128 + int(ws.Signal())
	}

	// Deixa o leitor esgotar o que sobrou; se um neto segurar o PTY, fecha à força.
	select {
	case <-readDone:
	case <-time.After(drainGrace):
	}
	t.mu.Lock()
	t.ptmxClosed = true
	_ = t.ptmx.Close()
	t.mu.Unlock()
	<-readDone

	end := t.now()
	t.mu.Lock()
	t.info.EndedAt = &end
	t.info.ExitCode = &code
	t.info.EndReason = t.reason
	if t.info.EndReason == "" {
		t.info.EndReason = EndAgent
	}
	for s := range t.subs {
		s.ended = true
		s.signal()
	}
	t.subs = map[*sub]struct{}{}
	t.mu.Unlock()
	close(t.done)
}

// publish grava no Ring e enfileira sem bloquear; assinante com mais que
// SubLimit bytes pendentes é desconectado.
func (t *Terminal) publish(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ring.Write(p)
	for s := range t.subs {
		if s.pending+len(p) > t.subLimit {
			delete(t.subs, s)
			s.queue, s.pending = nil, 0
			s.halt()
			continue
		}
		s.queue = append(s.queue, append([]byte(nil), p...))
		s.pending += len(p)
		s.signal()
	}
}

// pump entrega a fila do assinante em ordem; ao fim do terminal entrega o
// resto e fecha out; ao cancelar ou desconectar fecha na hora.
func (t *Terminal) pump(s *sub) {
	defer close(s.out)
	for {
		t.mu.Lock()
		select {
		case <-s.stop:
			t.mu.Unlock()
			return
		default:
		}
		var b []byte
		have := len(s.queue) > 0
		if have {
			b = s.queue[0]
			s.queue[0] = nil
			s.queue = s.queue[1:]
			s.pending -= len(b)
		}
		ended := s.ended
		t.mu.Unlock()
		if !have {
			if ended {
				return
			}
			select {
			case <-s.wake:
			case <-s.stop:
				return
			}
			continue
		}
		select {
		case s.out <- b:
		case <-s.stop:
			return
		}
	}
}

// Subscribe devolve o histórico recente e um canal com a saída nova. O canal
// fecha quando o terminal encerra (depois da saída final) ou quando o
// assinante acumula mais que SubLimit bytes sem ler. Chame cancel ao sair.
func (t *Terminal) Subscribe() ([]byte, <-chan []byte, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	snap := t.ring.Snapshot()
	s := &sub{out: make(chan []byte), wake: make(chan struct{}, 1), stop: make(chan struct{})}
	if t.info.EndedAt != nil {
		close(s.out)
		return snap, s.out, func() {}
	}
	t.subs[s] = struct{}{}
	go t.pump(s)
	cancel := func() {
		t.mu.Lock()
		delete(t.subs, s)
		t.mu.Unlock()
		s.halt()
	}
	return snap, s.out, cancel
}

// Write envia entrada ao processo.
func (t *Terminal) Write(p []byte) error {
	if t.ended() {
		return ErrEnded
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_ = t.ptmx.SetWriteDeadline(time.Now().Add(t.writeTimeout))
	_, err := t.ptmx.Write(p)
	return err
}

// Resize ajusta o tamanho do PTY.
func (t *Terminal) Resize(cols, rows uint16) error {
	if t.ended() {
		return ErrEnded
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ptmxClosed {
		return ErrEnded
	}
	rc, err := t.ptmx.SyscallConn()
	if err != nil {
		return err
	}
	var ierr error
	if err := rc.Control(func(fd uintptr) {
		ierr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows})
	}); err != nil {
		return err
	}
	if ierr != nil {
		return ierr
	}
	t.info.Cols, t.info.Rows = cols, rows
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
