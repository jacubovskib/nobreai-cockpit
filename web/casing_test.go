package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoCaseCollidingModules: o macOS (APFS) não diferencia maiúsculas por
// padrão, e "./Board" resolveria para board.ts se os dois existissem na mesma
// pasta, quebrando o build lá e não no Linux. Compara o nome até o primeiro
// ponto (Board.tsx e Board.test.tsx são o mesmo módulo; board.ts não).
func TestNoCaseCollidingModules(t *testing.T) {
	seen := map[string]string{}
	err := filepath.WalkDir("src", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		dir, base := filepath.Split(p)
		stem, _, _ := strings.Cut(base, ".")
		key := strings.ToLower(dir + stem)
		if prev, ok := seen[key]; ok && prev != dir+stem {
			t.Errorf("%s e %s só diferem em maiúsculas: quebram o build no macOS", prev, dir+stem)
		}
		seen[key] = dir + stem
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
