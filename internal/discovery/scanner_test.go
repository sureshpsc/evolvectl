package discovery

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMixedScan(t *testing.T) {
	src := filepath.Join(moduleRoot(t), "examples", "mixed-monorepo")
	root := t.TempDir()
	if err := copyDir(src, root); err != nil {
		t.Fatal(err)
	}
	inv, err := Scan(context.Background(), root, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Projects) < 3 {
		t.Fatalf("projects %+v", inv.Projects)
	}
	if len(inv.CopybaraConfigs) != 1 {
		t.Fatalf("copybara %+v", inv.CopybaraConfigs)
	}
	found := map[string]bool{}
	for _, l := range inv.Languages {
		found[l] = true
	}
	for _, l := range []string{"go", "python", "java", "node"} {
		if !found[l] {
			t.Fatalf("missing %s in %v", l, inv.Languages)
		}
	}
	inv2, err := Scan(context.Background(), root, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if inv2.CacheHits == 0 {
		t.Fatal("expected cache hits on second scan")
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(b), "module github.com/evolvectl/evolvectl") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}
