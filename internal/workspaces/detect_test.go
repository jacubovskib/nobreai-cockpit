package workspaces

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func tree(t *testing.T, files map[string]string, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func rels(ss []Suggestion) []string {
	out := []string{}
	for _, s := range ss {
		out = append(out, s.Rel)
	}
	return out
}

func TestDetectPnpm(t *testing.T) {
	root := tree(t, map[string]string{"pnpm-workspace.yaml": "packages:\n  - 'apps/*'\n  - packages/ui\n"},
		"apps/web", "apps/api", "packages/ui")
	got, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"apps/api", "apps/web", "packages/ui"}; !reflect.DeepEqual(rels(got), want) {
		t.Fatalf("rels = %v", rels(got))
	}
	for _, s := range got {
		if s.Source != "pnpm-workspace.yaml" || s.Path != filepath.Join(root, s.Rel) || s.Name != filepath.Base(s.Rel) {
			t.Fatalf("suggestion = %+v", s)
		}
	}
}

func TestDetectPackageJSONBothForms(t *testing.T) {
	a := tree(t, map[string]string{"package.json": `{"workspaces":["apps/*"]}`}, "apps/web")
	b := tree(t, map[string]string{"package.json": `{"workspaces":{"packages":["libs/core"]}}`}, "libs/core")
	for _, c := range []struct {
		root string
		want []string
	}{{a, []string{"apps/web"}}, {b, []string{"libs/core"}}} {
		got, err := Detect(c.root)
		if err != nil || !reflect.DeepEqual(rels(got), c.want) || got[0].Source != "package.json" {
			t.Fatalf("got %+v err %v", got, err)
		}
	}
}

func TestDetectGoWork(t *testing.T) {
	root := tree(t, map[string]string{"go.work": "go 1.22\n\nuse ./svc/a\n\nuse (\n\t./svc/b\n\t// c\n\t\"./svc/c\"\n)\n"},
		"svc/a", "svc/b", "svc/c")
	got, _ := Detect(root)
	if want := []string{"svc/a", "svc/b", "svc/c"}; !reflect.DeepEqual(rels(got), want) || got[0].Source != "go.work" {
		t.Fatalf("got %+v", got)
	}
}

func TestDetectCargo(t *testing.T) {
	root := tree(t, map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n\n[workspace]\nmembers = [\"crates/*\"]\n"},
		"crates/a", "crates/b")
	got, _ := Detect(root)
	if want := []string{"crates/a", "crates/b"}; !reflect.DeepEqual(rels(got), want) || got[0].Source != "Cargo.toml" {
		t.Fatalf("got %+v", got)
	}
	multi := tree(t, map[string]string{"Cargo.toml": "[workspace]\nmembers = [\n  \"a\",\n  \"b\", # c\n]\nresolver = \"2\"\n"}, "a", "b")
	got, _ = Detect(multi)
	if want := []string{"a", "b"}; !reflect.DeepEqual(rels(got), want) {
		t.Fatalf("multi-line = %v", rels(got))
	}
}

func TestDetectSkipsMissingNegatedAndRoot(t *testing.T) {
	root := tree(t, map[string]string{"pnpm-workspace.yaml": "packages:\n  - '!apps/old'\n  - '**'\n  - 'apps/**'\n  - '.'\n  - ghost\n  - apps/ok\n  - file.txt\n", "file.txt": "x"},
		"apps/old", "apps/ok")
	got, _ := Detect(root)
	if want := []string{"apps/ok"}; !reflect.DeepEqual(rels(got), want) {
		t.Fatalf("rels = %v", rels(got))
	}
}

func TestDetectDedup(t *testing.T) {
	root := tree(t, map[string]string{"pnpm-workspace.yaml": "packages:\n  - apps/web\n", "package.json": `{"workspaces":["apps/web"]}`}, "apps/web")
	got, _ := Detect(root)
	if len(got) != 1 || got[0].Source != "pnpm-workspace.yaml" {
		t.Fatalf("got %+v", got)
	}
}

func TestDetectNoFiles(t *testing.T) {
	got, err := Detect(t.TempDir())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v err %v", got, err)
	}
}
