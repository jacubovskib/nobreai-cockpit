// Package integration vigia a saúde da integração do Cockpit com o agente
// (hooks, status line, skill, CLI) e diz o que está quebrado e como consertar.
// Não escreve em ~/.claude: só observa e registra.
package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

const (
	keyBaseline = "integration_baseline"
	keyIgnored  = "integration_ignored"
	keyRemoved  = "integration_removed" // key -> hora da remoção deliberada

	againWindow   = 10 * time.Minute
	removalWindow = 10 * time.Minute // validade da marca de remoção deliberada

	unreadableText = "O ~/.claude/settings.json não pôde ser lido (JSON inválido?). O Cockpit não vai mexer nele; corrija o arquivo."
)

// Store é o que o monitor precisa do banco.
type Store interface {
	GetKV(key string, v any) (bool, error)
	PutKV(key string, v any) error
	AddConfigLog(kind string, data any, at time.Time) error
}

// Action é o conserto que a interface oferece para um problema.
type Action string

const (
	ActionReinstall   Action = "reinstall"
	ActionStatusline  Action = "statusline"
	ActionRestart     Action = "restart"
	ActionMakeInstall Action = "make_install"
	ActionConfig      Action = "config"
)

// Problem é um item da integração que funcionava e parou.
type Problem struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Action Action `json:"action"`
	Again  bool   `json:"again"` // quebrou de novo < 10 min depois de corrigido
}

// Monitor confere a integração e guarda a linha de base do que já funcionou.
type Monitor struct {
	Health func() []adapter.Check
	// Files são os arquivos vigiados; o primeiro é o settings.json, cuja data
	// e tamanho entram na impressão de "Ignorar".
	Files       func() []string
	NobreBin    func() string // binário nobre atual em disco (para "restart")
	Store       Store
	Now         func() time.Time
	DaemonStart time.Time
	Publish     func(kind string)
	Logf        func(string, ...any)

	mu       sync.Mutex
	loaded   bool
	baseline map[string]bool
	ignored  map[string]string
	fixedAt  map[string]time.Time
	removed  map[string]time.Time
	healthy  map[string]bool // último estado visto nesta execução
	checks   map[string]adapter.Check
	problems []Problem
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Monitor) logf(format string, a ...any) {
	if m.Logf != nil {
		m.Logf(format, a...)
	}
}

func (m *Monitor) publish() {
	if m.Publish != nil {
		m.Publish("integration")
	}
}

func (m *Monitor) load() {
	if m.loaded {
		return
	}
	m.loaded = true
	m.baseline, m.ignored = map[string]bool{}, map[string]string{}
	m.removed = map[string]time.Time{}
	m.fixedAt, m.healthy, m.checks = map[string]time.Time{}, map[string]bool{}, map[string]adapter.Check{}
	if _, err := m.Store.GetKV(keyBaseline, &m.baseline); err != nil {
		m.logf("integração: ler linha de base: %v", err)
	}
	if _, err := m.Store.GetKV(keyIgnored, &m.ignored); err != nil {
		m.logf("integração: ler ignorados: %v", err)
	}
	if _, err := m.Store.GetKV(keyRemoved, &m.removed); err != nil {
		m.logf("integração: ler remoções: %v", err)
	}
	if m.removed == nil {
		m.removed = map[string]time.Time{}
	}
	if m.baseline == nil {
		m.baseline = map[string]bool{}
	}
	if m.ignored == nil {
		m.ignored = map[string]string{}
	}
}

func (m *Monitor) put(key string, v any) {
	if err := m.Store.PutKV(key, v); err != nil {
		m.logf("integração: gravar %s: %v", key, err)
	}
}

func (m *Monitor) addLog(kind string, data any) {
	if err := m.Store.AddConfigLog(kind, data, m.now()); err != nil {
		m.logf("integração: registrar %s: %v", kind, err)
	}
}

// CheckNow confere a integração agora e devolve os problemas ativos.
func (m *Monitor) CheckNow() []Problem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkLocked()
}

// Problems devolve o resultado da última conferência.
func (m *Monitor) Problems() []Problem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Problem{}, m.problems...)
}

func (m *Monitor) checkLocked() []Problem {
	m.load()
	var checks []adapter.Check
	if m.Health != nil {
		checks = m.Health()
	}
	problems := []Problem{}
	changed, transition, tombs := false, false, false
	for _, c := range checks {
		m.checks[c.Key] = c
		if c.OK && c.Code == "" {
			if m.ignored[c.Key] != "" {
				delete(m.ignored, c.Key)
				m.put(keyIgnored, m.ignored)
			}
			if at, ok := m.removed[c.Key]; ok {
				if m.now().Sub(at) < removalWindow {
					// remoção deliberada em andamento: ainda não vale como "já funcionou"
					m.healthy[c.Key] = true
					continue
				}
				delete(m.removed, c.Key)
				tombs = true
			}
			if !m.baseline[c.Key] {
				m.baseline[c.Key] = true
				changed = true
			}
			if prev, seen := m.healthy[c.Key]; seen && !prev {
				transition = true
			}
			m.healthy[c.Key] = true
			continue
		}
		if _, ok := m.removed[c.Key]; ok {
			delete(m.removed, c.Key) // a remoção aconteceu
			tombs = true
		}
		if !m.baseline[c.Key] {
			m.healthy[c.Key] = false
			continue
		}
		if prev, seen := m.healthy[c.Key]; !seen || prev {
			m.addLog(domain.LogIntegrationBroken, map[string]any{"key": c.Key, "code": c.Code, "text": c.Detail})
			transition = true
		}
		m.healthy[c.Key] = false
		if m.ignored[c.Key] != "" && m.ignored[c.Key] == m.fingerprint(c) {
			continue
		}
		problems = append(problems, m.problemFor(c))
	}
	if changed {
		m.put(keyBaseline, m.baseline)
	}
	if tombs {
		m.put(keyRemoved, m.removed)
	}
	m.problems = problems
	if transition {
		m.publish()
	}
	return append([]Problem{}, problems...)
}

func (m *Monitor) problemFor(c adapter.Check) Problem {
	p := Problem{Key: c.Key, Text: c.Detail, Action: ActionConfig}
	switch c.Key {
	case "hooks":
		switch c.Code {
		case "unreadable":
			p.Text = unreadableText
		case "dead_binary":
			p.Action = ActionReinstall
			if m.NobreBin == nil || m.NobreBin() == "" {
				p.Action = ActionMakeInstall
			}
		default:
			p.Action = ActionReinstall
		}
	case "skill":
		p.Action = ActionReinstall
		if c.Code == "outdated" && m.binaryNewerThanDaemon() {
			p.Action = ActionRestart
		}
	case "statusline":
		p.Action = ActionStatusline
	}
	if t, ok := m.fixedAt[c.Key]; ok && m.now().Sub(t) < againWindow {
		p.Again = true
	}
	p.Text = problemText(c, p)
	return p
}

// problemText é o texto ao usuário de cada problema conhecido; o que o
// adaptador não nomeia mantém o detalhe do próprio check.
func problemText(c adapter.Check, p Problem) string {
	const deadPrefix = "aponta para um binário que não existe: "
	bin := strings.TrimPrefix(c.Detail, deadPrefix)
	switch {
	case p.Action == ActionMakeInstall:
		return "O nobre não foi encontrado; rode make install."
	case p.Action == ActionRestart:
		return "O Cockpit foi atualizado; reinicie o daemon (make restart)."
	case c.Key == "hooks" && c.Code == "dead_binary":
		return "Os hooks apontam para um nobre que não existe mais (" + bin + ")."
	case c.Key == "hooks" && c.Code == "missing" && p.Again:
		return "Os hooks do Cockpit foram removidos de novo logo depois de reinstalados: outra ferramenta pode estar reescrevendo o ~/.claude/settings.json."
	case c.Key == "hooks" && c.Code == "missing":
		return "Os hooks do Cockpit sumiram do ~/.claude/settings.json. As sessões novas do Claude Code não serão registradas."
	case c.Key == "skill":
		return "A skill do Cockpit está desatualizada."
	case c.Key == "statusline" && c.Code == "dead_binary":
		return "A status line do Cockpit aponta para um nobre que não existe mais (" + bin + ")."
	case c.Key == "statusline":
		return "A status line do Cockpit foi trocada por outro comando."
	case c.Key == "cli":
		return "O claude não foi encontrado no PATH."
	}
	return p.Text
}

func (m *Monitor) binaryNewerThanDaemon() bool {
	if m.NobreBin == nil {
		return false
	}
	bin := m.NobreBin()
	if bin == "" {
		return false
	}
	st, err := os.Stat(bin)
	return err == nil && st.ModTime().After(m.DaemonStart)
}

// fingerprint identifica o estado de um problema: se o conserto do usuário não
// mexeu no erro nem no settings.json, a impressão é a mesma e o Ignorar vale.
func (m *Monitor) fingerprint(c adapter.Check) string {
	fp := c.Code + "|" + c.Detail + "|"
	if m.Files != nil {
		if fs := m.Files(); len(fs) > 0 {
			fp += statSig(fs[0])
		}
	}
	return fp
}

func statSig(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
}

// Ignore esconde o problema de key até a impressão dele mudar.
func (m *Monitor) Ignore(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	c, ok := m.checks[key]
	if !ok {
		return nil
	}
	m.ignored[key] = m.fingerprint(c)
	if err := m.Store.PutKV(keyIgnored, m.ignored); err != nil {
		return err
	}
	if err := m.Store.AddConfigLog(domain.LogIntegrationIgnored, map[string]any{"key": key}, m.now()); err != nil {
		return err
	}
	m.checkLocked()
	m.publish()
	return nil
}

// Fixed registra um conserto (keys) e o que ele mudou, e confere de novo.
func (m *Monitor) Fixed(keys []string, changed []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	if changed == nil {
		changed = []string{}
	}
	m.addLog(domain.LogIntegrationFixed, map[string]any{"keys": keys, "changed": changed})
	for _, k := range keys {
		m.fixedAt[k] = m.now()
		delete(m.ignored, k)
		delete(m.removed, k)
	}
	m.put(keyRemoved, m.removed)
	m.put(keyIgnored, m.ignored)
	m.checkLocked()
	m.publish()
}

// Removed registra que o item saiu de propósito (origin: "config" ou "cli"):
// deixa de contar como "já funcionou", então não alerta.
func (m *Monitor) Removed(keys []string, origin string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	for _, k := range keys {
		m.baseline[k] = false
		m.removed[k] = m.now()
		delete(m.healthy, k)
	}
	m.put(keyBaseline, m.baseline)
	m.put(keyRemoved, m.removed)
	m.addLog(domain.LogIntegrationRemoved, map[string]any{"keys": keys, "origin": origin})
	m.checkLocked()
	m.publish()
}

func (m *Monitor) filesSig() string {
	if m.Files == nil {
		return ""
	}
	s := ""
	for _, f := range m.Files() {
		s += statSig(f) + ";"
	}
	return s
}

// Run confere ao subir e depois a cada every: de novo se algum arquivo
// vigiado mudou (data ou tamanho), ou se já passou full desde a última.
func (m *Monitor) Run(ctx context.Context, every, full time.Duration) {
	sig, last := m.filesSig(), m.now()
	m.CheckNow()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cur := m.filesSig(); cur != sig || m.now().Sub(last) >= full {
				sig, last = cur, m.now()
				m.CheckNow()
			}
		}
	}
}
