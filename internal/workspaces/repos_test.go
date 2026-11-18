package workspaces

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mk(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanReposDepthAndStop(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "a/.git", "b/c/.git", "b/c/d/.git", "e/f/g/.git", "h")
	if err := os.WriteFile(filepath.Join(root, "h/.git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ScanRepos(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b/c", "h"}; !reflect.DeepEqual(rels(got), want) {
		t.Fatalf("rels = %v, quer %v", rels(got), want)
	}
	if got[0].Source != "git" || got[0].Name != "a" || got[0].Path != filepath.Join(root, "a") {
		t.Fatalf("sugestão = %+v", got[0])
	}
}

func TestScanReposSkips(t *testing.T) {
	root := t.TempDir()
	mk(t, root, ".oculta/x/.git", "node_modules/y/.git", "dist/z/.git", "real/.git", "ok/sub")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	got, err := ScanRepos(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"real"}; !reflect.DeepEqual(rels(got), want) {
		t.Fatalf("rels = %v, quer %v", rels(got), want)
	}
}
