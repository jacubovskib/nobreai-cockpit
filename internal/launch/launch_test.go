package launch

import (
	"reflect"
	"testing"
)

func TestTerminalArgv(t *testing.T) {
	got := TerminalArgv([]string{"x-terminal-emulator", "-e"}, "/w/api",
		map[string]string{"NOBRE_RESUME": "NAC-1"}, []string{"claude", "Continue o NAC-1: 'x'"})
	want := []string{"x-terminal-emulator", "-e", `cd '/w/api' && NOBRE_RESUME='NAC-1' exec 'claude' 'Continue o NAC-1: '\''x'\'''`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestExecStartRejectsEmpty(t *testing.T) {
	if err := (Exec{}).Start(nil); err == nil {
		t.Fatal("esperava erro")
	}
}
