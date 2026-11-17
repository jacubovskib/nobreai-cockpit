package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("%v: %s", err, a)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return reflect.DeepEqual(x, y)
}

// Instalar e remover devolve o settings.json de antes (mesmo conteúdo).
func TestUninstallSettingsRoundTrip(t *testing.T) {
	cases := map[string]string{
		"sem hooks nem status line": `{"model":"opus"}`,
		"hooks de terceiros": `{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/other stop"}]}],` +
			`"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/x/guard"}]}]}}`,
		"status line do usuário":          `{"statusLine":{"type":"command","command":"echo hi"}}`,
		"status line com refresh próprio": `{"statusLine":{"type":"command","command":"echo hi","refreshInterval":30}}`,
		"entrada mista":                   `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/other stop"}]}]}}`,
	}
	for name, orig := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeSettings(t, orig)
			if _, slErr, err := InstallSettings(p, bin, true, false); err != nil || slErr != nil {
				t.Fatalf("install: %v %v", err, slErr)
			}
			rep, slErr, err := UninstallSettings(p, false)
			if err != nil || slErr != nil {
				t.Fatalf("uninstall: %v %v", err, slErr)
			}
			if got := mustRead(t, p); !sameJSON(t, got, []byte(orig)) {
				t.Fatalf("depois de remover:\n%s\nquer:\n%s", got, orig)
			}
			if rep.Backup == "" || len(rep.Changed) == 0 {
				t.Fatalf("rep = %+v", rep)
			}
		})
	}
}

// Um hook nosso na mesma entrada de um de terceiros: só o nosso sai.
func TestUninstallSettingsKeepsForeignHookInSameEntry(t *testing.T) {
	p := writeSettings(t, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/other stop"},`+
		`{"type":"command","command":"`+bin+` hook claude Stop"}]}]}}`)
	if _, _, err := UninstallSettings(p, false); err != nil {
		t.Fatal(err)
	}
	want := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/other stop"}]}]}}`
	if got := mustRead(t, p); !sameJSON(t, got, []byte(want)) {
		t.Fatalf("%s", got)
	}
}

func TestUninstallSettingsDryRunAndNothingToDo(t *testing.T) {
	p := writeSettings(t, `{"model":"opus"}`)
	if _, _, err := InstallSettings(p, bin, true, false); err != nil {
		t.Fatal(err)
	}
	before := string(mustRead(t, p))
	nb := len(backups(t, p))
	rep, _, err := UninstallSettings(p, true)
	if err != nil || len(rep.Changed) == 0 || string(mustRead(t, p)) != before || len(backups(t, p)) != nb {
		t.Fatalf("dry-run gravou ou não planejou: %+v %v", rep, err)
	}
	if msg := strings.Join(rep.Messages, " "); !strings.Contains(msg, "Removeria") {
		t.Fatalf("mensagens: %q", msg)
	}

	clean := writeSettings(t, `{"model":"opus"}`)
	rep, _, err = UninstallSettings(clean, false)
	if err != nil || len(rep.Changed) != 0 || len(backups(t, clean)) != 0 {
		t.Fatalf("sem nada nosso não grava: %+v %v", rep, err)
	}
	missing := filepath.Join(t.TempDir(), "settings.json")
	if rep, _, err := UninstallSettings(missing, false); err != nil || len(rep.Changed) != 0 {
		t.Fatalf("arquivo ausente: %+v %v", rep, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("criou o settings.json")
	}
}

func TestUninstallSettingsInvalidJSONAborts(t *testing.T) {
	p := writeSettings(t, `{"hooks":`)
	if _, _, err := UninstallSettings(p, false); err == nil {
		t.Fatal("JSON inválido deveria falhar")
	}
	if string(mustRead(t, p)) != `{"hooks":` {
		t.Fatal("alterou o arquivo")
	}
}

func TestUninstallSkill(t *testing.T) {
	dir := t.TempDir()
	if err := InstallSkill(dir); err != nil {
		t.Fatal(err)
	}
	if removed, err := UninstallSkill(dir); err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, skillName)); !os.IsNotExist(err) {
		t.Fatal("pasta da skill ficou")
	}
	if removed, err := UninstallSkill(dir); err != nil || removed {
		t.Fatalf("segunda remoção: %v %v", removed, err)
	}
	// Arquivo extra do usuário na pasta: o SKILL.md sai, a pasta fica.
	if err := InstallSkill(dir); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, skillName, "notas.md")
	if err := os.WriteFile(extra, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if removed, err := UninstallSkill(dir); err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	if _, err := os.Stat(extra); err != nil {
		t.Fatal("apagou arquivo do usuário")
	}
}
