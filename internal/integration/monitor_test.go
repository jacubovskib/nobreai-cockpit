package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
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
	return true, jsonUnmarshal(raw, v)
}
func (f *fakeStore) PutKV(key string, v any) error {
	b, err := jsonMarshal(v)
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
	e := &env{st: newFakeStore(), now: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC), settings: filepath.Join(dir, "settings.json")}
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
	e.checks = []adapter.Check{bad("statusline", "missing", "não integrada")}
	m.Removed([]string{"statusline"}, "config")
	if p := m.CheckNow(); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
	if e.st.count(domain.LogIntegrationRemoved) != 1 {
		t.Fatal(e.st.logs)
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
	m := e.monitor()
	e.checks = []adapter.Check{ok("hooks")}
	m.CheckNow()
	e.checks = []adapter.Check{bad("hooks", "dead_binary", "morto")}
	if p := m.CheckNow(); len(p) != 1 || p[0].Action != ActionMakeInstall {
		t.Fatalf("%+v", p)
	}
}
