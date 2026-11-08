package claude

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// userCmd é a status line real do usuário (bash: ${cwd/#$HOME/~}, $(cat), aspas, \033).
const userCmd = `input=$(cat); cwd=$(echo "$input" | jq -r '.workspace.current_dir'); dir="${cwd/#$HOME/~}"; printf '\033[01;32m%s@%s\033[00m:\033[01;34m%s\033[00m' "$(whoami)" "$(hostname -s)" "$dir"`

func slSettings(t *testing.T, sl map[string]any) string {
	t.Helper()
	root := map[string]any{"theme": "dark", "env": map[string]any{"A": "x && y <z>"}}
	if sl != nil {
		root["statusLine"] = sl
	}
	b, _ := json.MarshalIndent(root, "", "  ")
	return writeSettings(t, string(b))
}

func readSL(t *testing.T, p string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(mustRead(t, p), &root); err != nil {
		t.Fatal(err)
	}
	sl, _ := root["statusLine"].(map[string]any)
	return sl
}

func TestStatusLineWrapsExistingCommandAndRoundTrips(t *testing.T) {
	p := slSettings(t, map[string]any{"type": "command", "command": userCmd, "padding": 2})
	rep, err := InstallStatusLine(p, bin)
	if err != nil || len(rep.Changed) != 1 || rep.Backup == "" {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	sl := readSL(t, p)
	want := bin + " statusline | { " + userCmd + " ; }"
	if sl["command"] != want || sl["type"] != "command" || sl["padding"] != float64(2) || sl["refreshInterval"] != float64(60) {
		t.Fatalf("statusLine = %+v", sl)
	}
	if strings.Contains(string(mustRead(t, p)), `\u0026`) {
		t.Fatal("escapou &")
	}
	// Desinstalar devolve o original.
	if _, err := UninstallStatusLine(p, bin); err != nil {
		t.Fatal(err)
	}
	if got := readSL(t, p)["command"]; got != userCmd {
		t.Fatalf("original = %q", got)
	}
}

func TestStatusLineIdempotentAndReinstallOtherPath(t *testing.T) {
	p := slSettings(t, map[string]any{"type": "command", "command": userCmd})
	InstallStatusLine(p, bin)
	before := string(mustRead(t, p))
	rep, err := InstallStatusLine(p, bin)
	if err != nil || len(rep.Changed) != 0 || rep.Backup != "" || string(mustRead(t, p)) != before {
		t.Fatalf("não idempotente: %+v %v", rep, err)
	}
	other := "/home/u/my bin/nobre"
	if _, err := InstallStatusLine(p, other); err != nil {
		t.Fatal(err)
	}
	got := readSL(t, p)["command"].(string)
	if got != "'"+other+"' statusline | { "+userCmd+" ; }" || strings.Count(got, "statusline") != 1 {
		t.Fatalf("command = %q", got)
	}
	// E de volta, sem duplicar.
	InstallStatusLine(p, bin)
	if got := readSL(t, p)["command"]; got != bin+" statusline | { "+userCmd+" ; }" {
		t.Fatalf("command = %q", got)
	}
}

func TestStatusLineWithoutPreviousUsesResumo(t *testing.T) {
	p := slSettings(t, nil)
	if _, err := InstallStatusLine(p, bin); err != nil {
		t.Fatal(err)
	}
	sl := readSL(t, p)
	if sl["command"] != bin+" statusline --resumo" || sl["type"] != "command" {
		t.Fatalf("%+v", sl)
	}
	InstallStatusLine(p, "/x/nobre")
	if got := readSL(t, p)["command"]; got != "/x/nobre statusline --resumo" {
		t.Fatalf("%q", got)
	}
	UninstallStatusLine(p, bin)
	if readSL(t, p) != nil {
		t.Fatal("statusLine deveria sumir")
	}
}

func TestStatusLineRefreshIntervalOnlyWhenAbsent(t *testing.T) {
	p := slSettings(t, map[string]any{"type": "command", "command": "echo hi", "refreshInterval": 5})
	InstallStatusLine(p, bin)
	if got := readSL(t, p)["refreshInterval"]; got != float64(5) {
		t.Fatalf("refreshInterval = %v", got)
	}
}

func TestStatusLineInvalidJSONAbortsAndNothingWritten(t *testing.T) {
	p := writeSettings(t, `{"statusLine": {`)
	_, err := InstallStatusLine(p, bin)
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("err = %v", err)
	}
	if string(mustRead(t, p)) != `{"statusLine": {` {
		t.Fatal("arquivo alterado")
	}
	if m, _ := filepath.Glob(p + ".bak-*"); len(m) != 0 {
		t.Fatalf("backup criado: %v", m)
	}
	// statusLine de tipo errado também aborta.
	p = writeSettings(t, `{"statusLine": "x"}`)
	if _, err := InstallStatusLine(p, bin); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusLineBackupAndDryRun(t *testing.T) {
	p := slSettings(t, map[string]any{"type": "command", "command": "echo hi"})
	orig := string(mustRead(t, p))
	rep, err := PlanStatusLine(p, bin)
	if err != nil || len(rep.Changed) != 1 || string(mustRead(t, p)) != orig {
		t.Fatalf("dry-run alterou: %+v %v", rep, err)
	}
	rep, err = installStatusLine(p, bin, time.Date(2026, 11, 8, 1, 2, 3, 0, time.UTC), true)
	if err != nil || filepath.Base(rep.Backup) != "settings.json.bak-20261108010203" || string(mustRead(t, rep.Backup)) != orig {
		t.Fatalf("backup: %+v %v", rep, err)
	}
}

func TestStatusLineSymlinkPreserved(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	os.WriteFile(real, []byte(`{"statusLine":{"type":"command","command":"echo hi"}}`), 0o600)
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if _, err := InstallStatusLine(link, bin); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink virou arquivo")
	}
	if !strings.Contains(string(mustRead(t, real)), "statusline") {
		t.Fatal("destino não atualizado")
	}
}

func TestStatusLineWrapperShapesRoundTrip(t *testing.T) {
	for _, orig := range []string{
		"echo hi # comentário",
		"a=1\nb=2\necho $a$b",
		"cat <<EOF\nx\nEOF",
		"echo hi;",
		"sleep 1 &",
		`echo "a;b" '{}' $((1+2))`,
		"echo x\n",
	} {
		w := wrapCommand(bin, orig)
		sl, ours, err := parseStatusLine(w)
		if !ours || err != nil || sl.orig != orig || sl.bin != bin {
			t.Errorf("%q: sl=%+v ours=%v err=%v (wrapped %q)", orig, sl, ours, err, w)
		}
		if out, err := exec.Command("bash", "-n", "-c", w).CombinedOutput(); err != nil {
			t.Errorf("%q: sintaxe inválida: %v %s", orig, err, out)
		}
	}
	if _, ours, _ := parseStatusLine("/usr/bin/other statusline | { x ; }"); ours {
		t.Fatal("não é nosso")
	}
}

// fakeNobre é um "nobre" que só repassa o stdin (como o real faz sem rede).
func fakeNobre(t *testing.T) string {
	t.Helper()
	b := filepath.Join(t.TempDir(), "nobre")
	if err := os.WriteFile(b, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestStatusLineWrappedCommandSameOutputAsOriginal(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq não instalado")
	}
	b := fakeNobre(t)
	p := slSettings(t, map[string]any{"type": "command", "command": userCmd})
	if _, err := InstallStatusLine(p, b); err != nil {
		t.Fatal(err)
	}
	wrapped := readSL(t, p)["command"].(string)
	home, _ := os.UserHomeDir()
	in := `{"workspace":{"current_dir":"` + home + `/proj"},"rate_limits":{"five_hour":{"used_percentage":12}}}`
	// O original usa substituição do bash: compara-se sob bash. Qual shell o
	// Claude Code usa para statusLine.command não é documentado; o wrapper só
	// usa sintaxe POSIX (pipe e grupo { ; }), então roda igual em sh.
	run := func(sh, cmd string) string {
		c := exec.Command(sh, "-c", cmd)
		c.Stdin = strings.NewReader(in)
		out, err := c.Output()
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		return string(out)
	}
	want := run("bash", userCmd)
	if !strings.Contains(want, "/proj") {
		t.Fatalf("saída do original inesperada: %q", want)
	}
	if got := run("bash", wrapped); got != want {
		t.Fatalf("bash: %q != %q", got, want)
	}
	// Em sh o wrapper tem de se comportar como o original no mesmo sh.
	if shOut, err := func() (string, error) {
		c := exec.Command("sh", "-c", userCmd)
		c.Stdin = strings.NewReader(in)
		o, err := c.Output()
		return string(o), err
	}(); err == nil {
		if got := run("sh", wrapped); got != shOut {
			t.Fatalf("sh: %q != %q", got, shOut)
		}
	}
}

func TestDetectStatusLineCheck(t *testing.T) {
	e := newEnv(t)
	b := fakeNobre(t)
	p := slSettings(t, map[string]any{"type": "command", "command": "echo hi"})
	check := func() (bool, string) {
		e.a.SetPaths(Paths{SettingsPath: p, SkillsDir: t.TempDir(), NobreBin: b, ClaudeBin: b})
		for _, c := range e.a.Detect().Checks {
			if c.Key == "statusline" {
				if c.Label != "Status line integrada" {
					t.Fatalf("label %q", c.Label)
				}
				return c.OK, c.Detail
			}
		}
		t.Fatal("sem check statusline")
		return false, ""
	}
	if ok, _ := check(); ok {
		t.Fatal("não integrada deveria falhar")
	}
	InstallStatusLine(p, b)
	if ok, d := check(); !ok {
		t.Fatalf("integrada: %s", d)
	}
	InstallStatusLine(p, "/nope/nobre")
	if ok, d := check(); ok || !strings.Contains(d, "/nope/nobre") {
		t.Fatalf("binário ausente: %v %s", ok, d)
	}
}
