package rtk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const gainJSON = `{"summary":{},"daily":[{"date":"2026-11-19","commands":2,"input_tokens":3471,"output_tokens":3458,"saved_tokens":13,"savings_pct":0.37,"total_time_ms":16,"avg_time_ms":8}]}`

const hookBash = `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/x/rtk hook claude"}]}]}}`

type memKV struct{ m map[string][]byte }

func (k *memKV) GetKV(key string, v any) (bool, error) {
	b, ok := k.m[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, v)
}

func (k *memKV) PutKV(key string, v any) error {
	if k.m == nil {
		k.m = map[string][]byte{}
	}
	b, err := json.Marshal(v)
	k.m[key] = b
	return err
}

type env struct {
	t        *testing.T
	dir      string
	bin      string
	args     string
	settings string
	kv       *memKV
	now      time.Time
	tool     *Tool
}

// novo monta um ambiente com um rtk falso e um settings.json temporário.
func novo(t *testing.T, withBin bool, settings string) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{t: t, dir: dir, kv: &memKV{}, now: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	e.args = filepath.Join(dir, "args.log")
	e.settings = filepath.Join(dir, "claude", "settings.json")
	if settings != "" {
		if err := os.MkdirAll(filepath.Dir(e.settings), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(e.settings, []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(dir, "home")
	bindir := filepath.Join(home, ".cargo", "bin")
	if withBin {
		if err := os.MkdirAll(bindir, 0o755); err != nil {
			t.Fatal(err)
		}
		e.bin = filepath.Join(bindir, "rtk")
		script := "#!/bin/sh\necho \"$@\" >> " + e.args + "\n" +
			"case \"$1\" in\n" +
			"--version) echo 'rtk 1.2.0';;\n" +
			"gain) echo '" + gainJSON + "';;\n" +
			"init) if [ -n \"$RTK_FAIL\" ] || [ -f " + filepath.Join(dir, "fail") + " ]; then echo 'falhou feio'; exit 1; fi; echo '{\"mudou\":1}' > " + e.settings + "; echo 'feito';;\n" +
			"esac\n"
		if err := os.WriteFile(e.bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.tool = &Tool{
		SettingsPath: e.settings,
		Home:         home,
		SearchPATH:   func() string { return filepath.Join(dir, "vazio") },
		Store:        e.kv,
		Now:          func() time.Time { return e.now },
	}
	return e
}

func (e *env) invocacoes() []string {
	b, err := os.ReadFile(e.args)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestFind_Fallbacks(t *testing.T) {
	e := novo(t, true, "")
	if got := e.tool.Find(); got != e.bin {
		t.Fatalf("Find = %q, quer %q", got, e.bin)
	}
	// ~/.local/bin também vale
	local := filepath.Join(e.tool.Home, ".local", "bin")
	os.MkdirAll(local, 0o755)
	os.Rename(e.bin, filepath.Join(local, "rtk"))
	if got := e.tool.Find(); got != filepath.Join(local, "rtk") {
		t.Fatalf("Find = %q", got)
	}
	// PATH tem prioridade
	pdir := t.TempDir()
	os.WriteFile(filepath.Join(pdir, "rtk"), []byte("#!/bin/sh\n"), 0o755)
	e.tool.SearchPATH = func() string { return pdir }
	if got := e.tool.Find(); got != filepath.Join(pdir, "rtk") {
		t.Fatalf("Find = %q", got)
	}
	e2 := novo(t, false, "")
	if got := e2.tool.Find(); got != "" {
		t.Fatalf("Find = %q, quer vazio", got)
	}
}

func TestStatus(t *testing.T) {
	e := novo(t, true, hookBash)
	s := e.tool.Status(true)
	if !s.Installed || !s.Active || s.Version != "1.2.0" || s.Path != e.bin {
		t.Fatalf("status = %+v", s)
	}
	if got := e.tool.Status(false); got.Version != "" {
		t.Fatalf("sem versão: %+v", got)
	}

	set := func(content string) Status {
		os.WriteFile(e.settings, []byte(content), 0o644)
		return e.tool.Status(false)
	}
	if s := set(strings.Replace(hookBash, `"Bash"`, `"Bash|Edit"`, 1)); !s.Active {
		t.Fatalf("Bash|Edit deveria ativar: %+v", s)
	}
	if s := set(strings.Replace(hookBash, `"Bash"`, `"*"`, 1)); !s.Active {
		t.Fatalf("* deveria ativar: %+v", s)
	}
	if s := set(strings.Replace(hookBash, `"matcher":"Bash",`, ``, 1)); !s.Active {
		t.Fatalf("matcher vazio deveria ativar: %+v", s)
	}
	if s := set(strings.Replace(hookBash, `"Bash"`, `"Edit"`, 1)); s.Active {
		t.Fatalf("Edit não deveria ativar: %+v", s)
	}
	if s := set(strings.Replace(hookBash, `rtk hook claude`, `outra coisa`, 1)); s.Active || s.Detail == "" {
		t.Fatalf("comando sem rtk hook claude: %+v", s)
	}
	if s := set(`{invalido`); s.Active || s.Detail == "" {
		t.Fatalf("json inválido: %+v", s)
	}
	os.Remove(e.settings)
	if s := e.tool.Status(false); s.Active || s.Detail == "" {
		t.Fatalf("settings ausente: %+v", s)
	}
}

func TestCheck(t *testing.T) {
	e := novo(t, true, hookBash)
	c := e.tool.Check()
	if c.Key != "rtk" || c.Label != "RTK ativo no Claude Code" || !c.OK || c.Code != "" {
		t.Fatalf("check = %+v", c)
	}
	st1 := c.State

	// hook presente, binário ausente
	e2 := novo(t, false, hookBash)
	c2 := e2.tool.Check()
	if c2.OK || c2.Code != "dead_binary" {
		t.Fatalf("dead_binary: %+v", c2)
	}
	// hook ausente
	os.WriteFile(e.settings, []byte(`{}`), 0o644)
	c3 := e.tool.Check()
	if c3.OK || c3.Code != "missing" || c3.State == st1 {
		t.Fatalf("missing: %+v (state %q vs %q)", c3, c3.State, st1)
	}
	// ilegível
	os.WriteFile(e.settings, []byte(`{x`), 0o644)
	if c := e.tool.Check(); c.Code != "unreadable" || c.OK {
		t.Fatalf("unreadable: %+v", c)
	}
	// sem settings
	os.Remove(e.settings)
	if c := e.tool.Check(); c.Code != "no_settings" || c.OK {
		t.Fatalf("no_settings: %+v", c)
	}
	// State muda com o caminho do binário
	e.tool.SettingsPath = e2.settings
	e.tool.Home = e2.tool.Home
	if c := e.tool.Check(); c.State == st1 {
		t.Fatalf("state deveria mudar com o binário: %q", c.State)
	}
}

func TestCheck_OpensIntervalForManualActivation(t *testing.T) {
	e := novo(t, true, hookBash)
	e.tool.Check()
	iv, err := e.tool.Activations()
	if err != nil || len(iv) != 1 || !iv[0].On.Equal(e.now) || iv[0].Off != nil {
		t.Fatalf("intervalos = %+v (%v)", iv, err)
	}
	e.now = e.now.Add(time.Hour)
	e.tool.Check()
	if iv, _ := e.tool.Activations(); len(iv) != 1 {
		t.Fatalf("duplicou: %+v", iv)
	}
	// hook inativo: Check não fecha
	os.WriteFile(e.settings, []byte(`{}`), 0o644)
	e.tool.Check()
	if iv, _ := e.tool.Activations(); len(iv) != 1 || iv[0].Off != nil {
		t.Fatalf("Check fechou o intervalo: %+v", iv)
	}
}

func TestApply_Activate(t *testing.T) {
	e := novo(t, true, `{}`)
	dir := filepath.Dir(e.settings)
	for _, n := range []string{"20260101-000001", "20260101-000002", "20260101-000003", "20260101-000004"} {
		os.WriteFile(filepath.Join(dir, "settings.json.bak-rtk-"+n), []byte("{}"), 0o644)
	}
	out, backup, err := e.tool.Apply("activate")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "feito") {
		t.Fatalf("out = %q", out)
	}
	if got := e.invocacoes(); len(got) != 1 || got[0] != "init -g --hook-only --auto-patch" {
		t.Fatalf("invocações = %q", got)
	}
	if backup != filepath.Join(dir, "settings.json.bak-rtk-20261119-100000") {
		t.Fatalf("backup = %q", backup)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	}
	baks, _ := filepath.Glob(filepath.Join(dir, "settings.json.bak-rtk-*"))
	if len(baks) != 3 {
		t.Fatalf("cópias = %v", baks)
	}
	for _, b := range baks {
		if strings.HasSuffix(b, "000001") || strings.HasSuffix(b, "000002") {
			t.Fatalf("cópia antiga sobrou: %s", b)
		}
	}
	iv, _ := e.tool.Activations()
	if len(iv) != 1 || !iv[0].On.Equal(e.now) || iv[0].Off != nil {
		t.Fatalf("intervalos = %+v", iv)
	}
}

func TestApply_BackupDetails(t *testing.T) {
	e := novo(t, true, `{"original":true}`)
	dir := filepath.Dir(e.settings)
	os.WriteFile(filepath.Join(dir, "settings.json.bak-other"), []byte("x"), 0o644)
	_, b1, err := e.tool.Apply("activate")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(b1); string(got) != `{"original":true}` {
		t.Fatalf("backup = %q", got)
	}
	// mesmo segundo: nome único
	_, b2, _ := e.tool.Apply("deactivate")
	_, b3, _ := e.tool.Apply("activate")
	_, b4, _ := e.tool.Apply("deactivate")
	if b1 == b2 || b2 == b3 || b3 == b4 || b1 == b3 {
		t.Fatalf("nomes repetidos: %s %s %s %s", b1, b2, b3, b4)
	}
	if _, err := os.Stat(b4); err != nil {
		t.Fatal("a cópia nova foi podada")
	}
	baks, _ := filepath.Glob(filepath.Join(dir, "settings.json.bak-rtk-*"))
	if len(baks) != 3 {
		t.Fatalf("cópias = %v", baks)
	}
	for _, n := range []string{"settings.json", "settings.json.bak-other"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s sumiu", n)
		}
	}
}

func TestApply_BackupSurvivesFailure(t *testing.T) {
	e := novo(t, true, `{"original":true}`)
	os.WriteFile(filepath.Join(e.dir, "fail"), nil, 0o644)
	_, backup, err := e.tool.Apply("activate")
	if err == nil || backup == "" {
		t.Fatalf("err=%v backup=%q", err, backup)
	}
	if _, serr := os.Stat(backup); serr != nil {
		t.Fatal("backup sumiu após falha")
	}
}

func TestApply_DeactivateWithoutSettings(t *testing.T) {
	e := novo(t, true, "")
	_, backup, err := e.tool.Apply("deactivate")
	if err != nil || backup != "" {
		t.Fatalf("err=%v backup=%q", err, backup)
	}
	if got := e.invocacoes(); len(got) != 1 || got[0] != "init -g --uninstall" {
		t.Fatalf("invocações = %q", got)
	}
}

func TestFind_IgnoresRelativePATH(t *testing.T) {
	e := novo(t, false, "")
	e.tool.SearchPATH = func() string { return ":bin:." }
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "bin", "rtk"), []byte("#!/bin/sh\n"), 0o755)
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)
	if got := e.tool.Find(); got != "" {
		t.Fatalf("Find = %q", got)
	}
}

func TestApply_Deactivate(t *testing.T) {
	e := novo(t, true, hookBash)
	e.tool.Apply("activate")
	e.now = e.now.Add(2 * time.Hour)
	if _, _, err := e.tool.Apply("deactivate"); err != nil {
		t.Fatal(err)
	}
	got := e.invocacoes()
	if got[len(got)-1] != "init -g --uninstall" {
		t.Fatalf("invocações = %q", got)
	}
	iv, _ := e.tool.Activations()
	if len(iv) != 1 || iv[0].Off == nil || !iv[0].Off.Equal(e.now) {
		t.Fatalf("intervalos = %+v", iv)
	}
}

func TestApply_Errors(t *testing.T) {
	e := novo(t, false, `{}`)
	if _, _, err := e.tool.Apply("activate"); err == nil || !strings.Contains(err.Error(), "RTK não instalado") {
		t.Fatalf("err = %v", err)
	}
	e = novo(t, true, "")
	if _, _, err := e.tool.Apply("activate"); err == nil {
		t.Fatal("settings ausente deveria falhar")
	}
	if len(e.invocacoes()) != 0 {
		t.Fatalf("nada deveria rodar: %q", e.invocacoes())
	}
	e = novo(t, true, `{}`)
	if _, _, err := e.tool.Apply("explodir"); err == nil {
		t.Fatal("action inválida deveria falhar")
	}
	os.WriteFile(filepath.Join(e.dir, "fail"), nil, 0o644)
	_, _, err := e.tool.Apply("activate")
	if err == nil || !strings.Contains(err.Error(), "falhou feio") {
		t.Fatalf("err = %v", err)
	}
	if iv, _ := e.tool.Activations(); len(iv) != 0 {
		t.Fatalf("intervalo mudou: %+v", iv)
	}
}

func TestPreview(t *testing.T) {
	e := novo(t, true, `{}`)
	out, err := e.tool.Preview("activate")
	if err != nil || !strings.Contains(out, "feito") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := e.tool.Preview("deactivate"); err != nil {
		t.Fatal(err)
	}
	got := e.invocacoes()
	if len(got) != 2 || got[0] != "init -g --hook-only --auto-patch --dry-run" || got[1] != "init -g --uninstall --dry-run" {
		t.Fatalf("invocações = %q", got)
	}
	if _, err := e.tool.Preview("x"); err == nil {
		t.Fatal("action inválida")
	}
}

func TestGain(t *testing.T) {
	e := novo(t, true, `{}`)
	days, err := e.tool.Gain()
	if err != nil || len(days) != 1 || days[0].Date != "2026-11-19" || days[0].SavedTokens != 13 || days[0].InputTokens != 3471 {
		t.Fatalf("days=%+v err=%v", days, err)
	}
	e.now = e.now.Add(30 * time.Second)
	e.tool.Gain()
	if n := len(e.invocacoes()); n != 1 {
		t.Fatalf("invocações = %d, quer 1 (cache)", n)
	}
	e.now = e.now.Add(31 * time.Second)
	e.tool.Gain()
	if n := len(e.invocacoes()); n != 2 {
		t.Fatalf("invocações = %d, quer 2", n)
	}
	// saída inválida
	e.tool.Run = func(ctx context.Context, bin string, args ...string) ([]byte, error) { return []byte("lixo"), nil }
	e.now = e.now.Add(2 * time.Minute)
	if _, err := e.tool.Gain(); err == nil {
		t.Fatal("saída inválida deveria falhar")
	}
}
