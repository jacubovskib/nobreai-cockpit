package resources

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, make([]byte, size), 0o644))
}

func TestMeasureDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.bin"), 64<<10)
	write(t, filepath.Join(root, "sub", "b.bin"), 32<<10)
	must(t, os.Link(filepath.Join(root, "a.bin"), filepath.Join(root, "sub", "a-link.bin"))) // hardlink conta uma vez
	must(t, os.Symlink("/usr", filepath.Join(root, "link-usr")))                             // symlink não é seguido
	nested := filepath.Join(root, ".claude", "worktrees", "x")
	write(t, filepath.Join(nested, "grande.bin"), 512<<10) // worktree aninhado fica de fora

	base := measureDir(root, nil)
	without := measureDir(root, []string{nested})
	if without < 96<<10 || without > 200<<10 {
		t.Fatalf("sem o aninhado = %d bytes", without)
	}
	if base-without < 512<<10 {
		t.Fatalf("o aninhado deveria somar ~512 KiB: com %d, sem %d", base, without)
	}
	if measureDir(filepath.Join(root, "nao-existe"), nil) != 0 {
		t.Fatal("diretório inexistente deveria dar 0")
	}
}

func TestDiskMeterCachesAndRefreshes(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.bin"), 64<<10)
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := NewDiskMeter(10*time.Minute, func() time.Time { return now })
	ctx := t.Context()
	go m.Run(ctx)

	if d := m.Disk(root, nil); !d.Pending || d.MeasuredAt != nil {
		t.Fatalf("primeira consulta deveria ficar pendente: %+v", d)
	}
	d := waitMeasured(t, m, root)
	if d.Bytes < 64<<10 || !d.MeasuredAt.Equal(now) {
		t.Fatalf("medida = %+v", d)
	}

	write(t, filepath.Join(root, "b.bin"), 256<<10)
	if d2 := m.Disk(root, nil); d2.Bytes != d.Bytes || d2.Pending {
		t.Fatalf("dentro do prazo deveria usar o cache: %+v", d2)
	}
	now = now.Add(11 * time.Minute)
	if d3 := m.Disk(root, nil); d3.Bytes != d.Bytes || !d3.Pending {
		t.Fatalf("vencido: mostra o valor antigo e remede: %+v", d3)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if d4 := m.Disk(root, nil); !d4.Pending && d4.Bytes > d.Bytes {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("não remediu")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitMeasured(t *testing.T, m *DiskMeter, path string) DiskInfo {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if d := m.Disk(path, nil); d.MeasuredAt != nil && !d.Pending {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("medição não terminou")
	return DiskInfo{}
}
