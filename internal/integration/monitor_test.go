package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/rtk"
)

type fakeStore struct {
	kv   map[string]string
	logs []string
}

func newFakeStore() *fakeStore { return &fakeStore{kv: map[string]string{}} }

func (f *fakeStore) GetKV(key string, v any) (bool, error) {
	raw, ok := f.kv[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal([]byte(raw), v)
}
func (f *fakeStore) PutKV(key string, v any) error {
	b, err := json.Marshal(v)
	f.kv[key] = string(b)
	return err
}
func (f *fakeStore) AddConfigLog(kind string, _ any, _ time.Time) error {
	f.logs = append(f.logs, kind)
	return nil
}
func (f *fakeStore) count(kind string) int {
	n := 0
	for _, k := range f.logs {
		if k == kind {
			n++
		}
	}
	return n
}

type env struct {
	st        *fakeStore
	now       time.Time
	checks    []adapter.Check
	published int
	bin       string
	settings  string
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	// bin: um nobre conhecido (não precisa existir); "" = nobre não encontrado.
	e := &env{st: newFakeStore(), now: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC), settings: filepath.Join(dir, "settings.json"),
		bin: filepath.Join(dir, "bin", "nobre")}
	os.WriteFile(e.settings, []byte("{}"), 0o600)
	return e
}

func (e *env) monitor() *Monitor {
	return &Monitor{
		Health:      func() []adapter.Check { return e.checks },
		Files:       func() []string { return []string{e.settings} },
		NobreBin:    func() string { return e.bin },
		Store:       e.st,
		Now:         func() time.Time { return e.now },
		DaemonStart: e.now,
		Publish:     func(string) { e.published++ },
	}
}

func ok(key string) adapter.Check { return adapter.Check{Key: key, OK: true} }
func bad(key, code, detail string) adapter.Check {
	return adapter.Check{Key: key, Code: code, Detail: detail}
}

func TestNoBaselineNoAlert(t *testing.T) {
	e := newEnv(t)
	e.checks = []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
	if p := e.monitor().CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestBrokenAfterOK(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
	p := m.CheckNow()
	if len(p) != 1 || p[0].Key != "hooks" || p[0].Action != ActionReinstall {
		t.Fatalf("%+v", p)
	}
	m.CheckNow()
	if e.st.count(domain.LogIntegrationBroken) != 1 || e.published == 0 {
		t.Fatalf("logs=%v published=%d", e.st.logs, e.published)
	}
	if got := m.Problems(); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	before := e.published
	e.checks = []adapter.Check{ok("hooks")}
	if p := m.CheckNow(); len(p) != 0 || e.published != before+1 {
		t.Fatalf("%+v published=%d", p, e.published)
	}
}

func TestBaselineSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	e.checks = []adapter.Check{ok("hooks")}
	e.monitor().CheckNow()
	e.checks = []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
	if p := e.monitor().CheckNow(); len(p) != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestIgnoreUntilFingerprintChanges(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
	m.CheckNow()
	if err := m.Ignore("hooks"); err != nil {
		t.Fatal(err)
	}
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
	if e.st.count(domain.LogIntegrationIgnored) != 1 {
		t.Fatal(e.st.logs)
	}
	e.checks = []adapter.Check{bad("hooks", "missing", "faltam: Stop, SessionEnd")}
	if p := m.CheckNow(); len(p) != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestAgainWithin10Min(t *testing.T) {
	for _, tc := range []struct {
		after time.Duration
		again bool
	}{{5 * time.Minute, true}, {11 * time.Minute, false}} {
		e := newEnv(t)
		m := e.monitor()
		e.checks = []adapter.Check{ok("hooks")}
		m.CheckNow()
		m.Fixed([]string{"hooks"}, []string{"settings.json"})
		if e.st.count(domain.LogIntegrationFixed) != 1 {
			t.Fatal(e.st.logs)
		}
		e.now = e.now.Add(tc.after)
		e.checks = []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
		p := m.CheckNow()
		if len(p) != 1 || p[0].Again != tc.again {
			t.Fatalf("%v: %+v", tc.after, p)
		}
	}
}

func TestRemovedDoesNotAlert(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("statusline")}
	m.CheckNow()
	m.Removed([]string{"statusline"}, "config") // a CLI chama antes de gravar o settings.json
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
	e.checks = []adapter.Check{bad("statusline", "missing", "não integrada")}
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
	if e.st.count(domain.LogIntegrationRemoved) != 1 {
		t.Fatal(e.st.logs)
	}
}

func TestRemovedSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	e.checks = []adapter.Check{ok("statusline")}
	e.monitor().Removed([]string{"statusline"}, "cli")
	m2 := e.monitor()
	m2.CheckNow() // ainda OK
	e.checks = []adapter.Check{bad("statusline", "missing", "não integrada")}
	if p := m2.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestRemovalMarkExpires(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("statusline")}
	m.CheckNow()
	m.Removed([]string{"statusline"}, "cli")
	e.now = e.now.Add(11 * time.Minute)
	m.CheckNow() // continua OK: a remoção não aconteceu
	e.checks = []adapter.Check{bad("statusline", "missing", "não integrada")}
	if p := m.CheckNow(); len(p) != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestIgnoreClearedWhenOKAgain(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	broken := []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
	e.checks = broken
	m.CheckNow()
	m.Ignore("hooks")
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = broken
	if p := m.CheckNow(); len(p) != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestOutdatedSkillAfterUpgradeAsksRestart(t *testing.T) {
	e := newEnv(t)
	e.bin = filepath.Join(t.TempDir(), "nobre")
	os.WriteFile(e.bin, []byte("x"), 0o755)
	older := e.now.Add(-time.Hour)
	os.Chtimes(e.bin, older, older)
	m := e.monitor()
	e.checks = []adapter.Check{ok("skill")}
	m.CheckNow()
	e.checks = []adapter.Check{{Key: "skill", OK: true, Code: "outdated", Detail: "desatualizada"}}
	if p := m.CheckNow(); len(p) != 1 || p[0].Action != ActionReinstall {
		t.Fatalf("binário antigo: %+v", p)
	}
	later := e.now.Add(time.Hour)
	os.Chtimes(e.bin, later, later)
	if p := m.CheckNow(); len(p) != 1 || p[0].Action != ActionRestart {
		t.Fatalf("binário novo: %+v", p)
	}
}

func TestUnreadableSettings(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("hooks", "unreadable", "invalid")}
	p := m.CheckNow()
	if len(p) != 1 || p[0].Action != ActionConfig ||
		p[0].Text != "O ~/.claude/settings.json não pôde ser lido (JSON inválido?). O Cockpit não vai mexer nele; corrija o arquivo." {
		t.Fatalf("%+v", p)
	}
}

func TestDeadBinaryWithoutNobreAsksMakeInstall(t *testing.T) {
	e := newEnv(t)
	e.bin = ""
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("hooks", "dead_binary", "morto")}
	if p := m.CheckNow(); len(p) != 1 || p[0].Action != ActionMakeInstall {
		t.Fatalf("%+v", p)
	}
}

// Sem nobre em disco, reinstalar gravaria um caminho morto: qualquer problema
// dos hooks pede make install.
func TestHooksMissingWithoutNobreAsksMakeInstall(t *testing.T) {
	e := newEnv(t)
	e.bin = ""
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
	p := m.CheckNow()
	if len(p) != 1 || p[0].Action != ActionMakeInstall || p[0].Text != "O nobre não foi encontrado; rode make install." {
		t.Fatalf("%+v", p)
	}
}

// settings.json ilegível ou ausente: a status line não foi "trocada por outro
// comando"; o alerta manda olhar a configuração.
func TestStatuslineWithoutReadableSettings(t *testing.T) {
	for code, text := range map[string]string{
		"unreadable":  unreadableText,
		"no_settings": unreadableText,
	} {
		e := newEnv(t)
		m := e.monitor()
		e.checks = []adapter.Check{ok("statusline")}
		m.CheckNow()
		e.checks = []adapter.Check{bad("statusline", code, "detalhe")}
		if p := m.CheckNow(); len(p) != 1 || p[0].Action != ActionConfig || p[0].Text != text {
			t.Fatalf("%s: %+v", code, p)
		}
	}
}

// A impressão de Ignorar é o estado do próprio item: mexer em outra chave do
// settings.json não traz o alerta de volta; trocar o comando da status line traz.
func TestIgnoreFingerprintIsItemState(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("statusline")}
	m.CheckNow()
	foreign := func(cmd string) adapter.Check {
		return adapter.Check{Key: "statusline", Code: "missing", Detail: "não passa pelo nobre", State: cmd}
	}
	e.checks = []adapter.Check{foreign("echo a")}
	m.CheckNow()
	if err := m.Ignore("statusline"); err != nil {
		t.Fatal(err)
	}
	later := e.now.Add(time.Minute)
	os.WriteFile(e.settings, []byte(`{"model":"opus"}`), 0o600)
	os.Chtimes(e.settings, later, later)
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("outra chave do settings.json trouxe o alerta: %+v", p)
	}
	e.checks = []adapter.Check{foreign("echo b")}
	if p := m.CheckNow(); len(p) != 1 {
		t.Fatalf("comando novo não alertou: %+v", p)
	}
}

// Health roda fora da trava: um GET /api/integration durante a conferência
// não espera por ela.
func TestHealthRunsOutsideLock(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	m.Health = func() []adapter.Check {
		m.Problems()
		return []adapter.Check{ok("hooks")}
	}
	done := make(chan struct{})
	go func() { m.CheckNow(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CheckNow travou: Health roda com a trava")
	}
}

// Conferências simultâneas não se atropelam: uma de cada vez.
func TestChecksAreSerialized(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	var mu sync.Mutex
	running, overlap := 0, false
	m.Health = func() []adapter.Check {
		mu.Lock()
		running++
		overlap = overlap || running > 1
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return []adapter.Check{ok("hooks")}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); m.CheckNow() }()
	}
	wg.Wait()
	if overlap {
		t.Fatal("duas conferências rodaram ao mesmo tempo")
	}
}

// O alerta muda de texto sem mudar de estado (o binário morto agora é outro):
// a interface é avisada.
func TestPublishWhenProblemListChanges(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("hooks", "dead_binary", "aponta para um binário que não existe: /a/nobre")}
	m.CheckNow()
	before := e.published
	m.CheckNow()
	if e.published != before {
		t.Fatalf("publicou sem mudança: %d -> %d", before, e.published)
	}
	e.checks = []adapter.Check{bad("hooks", "dead_binary", "aponta para um binário que não existe: /b/nobre")}
	p := m.CheckNow()
	if len(p) != 1 || e.published != before+1 {
		t.Fatalf("%+v published %d -> %d", p, before, e.published)
	}
}

// Um problema que continua depois de reiniciar o daemon não ganha outra linha
// de "quebrou" no histórico; quebrar de novo depois de voltar ao normal ganha.
func TestBrokenNotReloggedAfterRestart(t *testing.T) {
	e := newEnv(t)
	e.checks = []adapter.Check{ok("hooks")}
	e.monitor().CheckNow()
	broken := []adapter.Check{bad("hooks", "missing", "faltam: Stop")}
	e.checks = broken
	e.monitor().CheckNow()
	m := e.monitor() // reinício
	if p := m.CheckNow(); len(p) != 1 {
		t.Fatalf("%+v", p)
	}
	if n := e.st.count(domain.LogIntegrationBroken); n != 1 {
		t.Fatalf("broken logado %d vezes", n)
	}
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = broken
	e.monitor().CheckNow()
	if n := e.st.count(domain.LogIntegrationBroken); n != 2 {
		t.Fatalf("quebra nova: broken logado %d vezes", n)
	}
}

func TestRTK_NeverActiveNoAlert(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{bad("rtk", "missing", "hook ausente")}
	for i := 0; i < 2; i++ {
		if p := m.CheckNow(); len(p) != 0 {
			t.Fatalf("%+v", p)
		}
	}
}

func TestRTK_RemovedOutside(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("rtk")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("rtk", "missing", "hook ausente")}
	p := m.CheckNow()
	want := "O hook do RTK sumiu do ~/.claude/settings.json; os comandos voltam a sair sem compressão."
	if len(p) != 1 || p[0].Key != "rtk" || p[0].Action != ActionRTK || p[0].Text != want {
		t.Fatalf("%+v", p)
	}
}

func TestRTK_DeadBinary(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("rtk")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("rtk", "dead_binary", "sumiu")}
	p := m.CheckNow()
	want := "O RTK está ativo no Claude Code, mas o binário sumiu. Reinstale com cargo install --git https://github.com/rtk-ai/rtk --branch master rtk."
	if len(p) != 1 || p[0].Action != ActionConfig || p[0].Text != want {
		t.Fatalf("%+v", p)
	}
}

func TestRTK_MissingWithoutBinary(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("rtk")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("rtk", "missing", rtk.DetailNotInstalled)}
	p := m.CheckNow()
	if len(p) != 1 || p[0].Action != ActionConfig || !strings.Contains(p[0].Text, "Reinstale") {
		t.Fatalf("%+v", p)
	}
}

func TestRTK_DeliberateRemoval(t *testing.T) {
	e := newEnv(t)
	m := e.monitor()
	e.checks = []adapter.Check{ok("rtk")}
	m.CheckNow()
	m.Removed([]string{"rtk"}, "config")
	e.checks = []adapter.Check{bad("rtk", "missing", "hook ausente")}
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
}
