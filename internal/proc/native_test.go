package proc

import (
	"encoding/binary"
	"os"
	"reflect"
	"testing"
)

func procArgs(argc uint32, parts ...string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, argc)
	for _, p := range parts {
		b = append(b, p...)
		b = append(b, 0)
	}
	return b
}

func TestParseProcArgs(t *testing.T) {
	cases := []struct {
		name       string
		in         []byte
		exe, argv0 string
	}{
		{"instalador do claude", procArgs(2, "/Users/u/.local/share/claude/versions/2.1.3", "\x00\x00", "claude", "--resume"), "/Users/u/.local/share/claude/versions/2.1.3", "claude"},
		{"sem alinhamento", procArgs(1, "/bin/zsh", "-zsh"), "/bin/zsh", "-zsh"},
		{"argc zero", procArgs(0, "/bin/ls"), "/bin/ls", ""},
		{"truncado", []byte{1, 0}, "", ""},
		{"sem NUL", append(binary.LittleEndian.AppendUint32(nil, 1), "/bin/ls"...), "/bin/ls", ""},
	}
	for _, c := range cases {
		exe, argv0 := parseProcArgs(c.in)
		if exe != c.exe || argv0 != c.argv0 {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, exe, argv0, c.exe, c.argv0)
		}
	}
}

func TestBaseNames(t *testing.T) {
	got := baseNames("2.1.3", "/x/versions/2.1.3", "claude", "", "/opt/bin/claude")
	if want := []string{"2.1.3", "claude"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestHasNameFakeRoot(t *testing.T) {
	root := t.TempDir()
	fakeProc(t, root, 100, 1, "claude")
	if !HasName(root, 100, []string{"kiro-cli", "claude"}) {
		t.Fatal("claude deveria casar")
	}
	if HasName(root, 100, []string{"node"}) || HasName(root, 999, []string{"claude"}) || HasName(root, 0, []string{"claude"}) {
		t.Fatal("não deveria casar")
	}
}

// TestNativeRoot confere o processo do próprio teste pelo sistema real: no
// macOS pelo sysctl, no Linux pelo /proc.
func TestNativeRoot(t *testing.T) {
	pid := os.Getpid()
	if !Alive(Root, pid) {
		t.Fatal("o próprio processo deveria estar vivo")
	}
	if Alive(Root, 1<<30) {
		t.Fatal("PID inexistente não deveria estar vivo")
	}
	if len(Names(Root, pid)) == 0 || Comm(Root, pid) == "" {
		t.Fatalf("sem nome para o próprio processo: %v", Names(Root, pid))
	}
	if pp, err := parentPID(Root, pid); err != nil || pp != os.Getppid() {
		t.Fatalf("pai = %d, %v; want %d", pp, err, os.Getppid())
	}
}
