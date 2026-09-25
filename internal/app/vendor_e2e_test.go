package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/campaign"
	"github.com/sureshpsc/evolvectl/internal/exitcode"
	"github.com/sureshpsc/evolvectl/internal/runner"
	"github.com/sureshpsc/evolvectl/internal/state"
)

// fileProxy writes a GOPROXY directory serving example.com/lib at the given versions.
func fileProxy(t *testing.T, versions map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "example.com", "lib", "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var list []string
	for v, files := range versions {
		list = append(list, v)
		mod := "module example.com/lib\n\ngo 1.21\n"
		must(t, os.WriteFile(filepath.Join(dir, v+".mod"), []byte(mod), 0o644))
		must(t, os.WriteFile(filepath.Join(dir, v+".info"), []byte(`{"Version":"`+v+`","Time":"2024-01-01T00:00:00Z"}`), 0o644))
		f, err := os.Create(filepath.Join(dir, v+".zip"))
		must(t, err)
		zw := zip.NewWriter(f)
		files["go.mod"] = mod
		for name, body := range files {
			w, err := zw.Create("example.com/lib@" + v + "/" + name)
			must(t, err)
			_, err = w.Write([]byte(body))
			must(t, err)
		}
		must(t, zw.Close())
		must(t, f.Close())
	}
	must(t, os.WriteFile(filepath.Join(dir, "list"), []byte(strings.Join(list, "\n")+"\n"), 0o644))
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(root)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u.String()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestVendorEndToEnd(t *testing.T) {
	if _, ok := runner.Look("go"); !ok {
		t.Skip("go is not on PATH")
	}
	proxy := fileProxy(t, map[string]map[string]string{
		"v1.0.0": {"lib.go": "package lib\n\nfunc Hello() string { return \"v1.0\" }\n"},
		"v1.1.0": {"lib.go": "package lib\n\nfunc Hello() string { return \"v1.1\" }\n\nfunc Bye() string { return \"bye\" }\n", "LICENSE": "BSD-3-Clause\n"},
	})
	t.Setenv("GOPROXY", proxy)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	setModuleCache(t)

	ws := t.TempDir()
	must(t, os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module example.com/app\n\ngo 1.21\n\nrequire example.com/lib v1.0.0\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(ws, "app.go"), []byte("package app\n\nimport \"example.com/lib\"\n\nfunc Greet() string { return lib.Hello() }\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(ws, "app_test.go"), []byte("package app\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n"), 0o644))
	old := filepath.Join(ws, "third_party", "lib")
	must(t, os.MkdirAll(old, 0o755))
	must(t, os.WriteFile(filepath.Join(old, "STALE.txt"), []byte("previous copy\n"), 0o644))
	tidy := runner.Run(context.Background(), runner.Request{Argv: []string{"go", "mod", "tidy"}, Dir: ws})
	if tidy.ExitCode != 0 {
		t.Fatalf("setup tidy: %s %s", tidy.Stderr, tidy.Err)
	}
	gomodBefore, _ := os.ReadFile(filepath.Join(ws, "go.mod"))

	var a App
	buf := &strings.Builder{}
	a.Out, a.Err = buf, &strings.Builder{}
	code, err := a.Upgrade(context.Background(), Option{Workspace: ws, NoAI: true, VendorDir: "third_party/lib"}, "example.com/lib", "v1.1.0", "")
	if err != nil || code != exitcode.Success {
		t.Fatalf("code=%d err=%v\n%s", code, err, buf)
	}
	if _, err := os.Stat(filepath.Join(old, "STALE.txt")); !os.IsNotExist(err) {
		t.Fatal("old vendored file kept")
	}
	lib, _ := os.ReadFile(filepath.Join(old, "lib.go"))
	if !strings.Contains(string(lib), "Bye") {
		t.Fatal("v1.1.0 not vendored")
	}
	var meta map[string]any
	b, _ := os.ReadFile(filepath.Join(old, campaign.VendorMetadata))
	must(t, json.Unmarshal(b, &meta))
	if meta["module"] != "example.com/lib" || meta["version"] != "v1.1.0" || meta["sum"] == "" {
		t.Fatalf("%v", meta)
	}
	gomod, _ := os.ReadFile(filepath.Join(ws, "go.mod"))
	if !strings.Contains(string(gomod), "replace example.com/lib => ./third_party/lib") {
		t.Fatal(string(gomod))
	}

	reps, _ := state.Open(ws).ListReports()
	if len(reps) != 1 {
		t.Fatalf("runs %d", len(reps))
	}
	rep := reps[0]
	vendored := false
	for _, c := range rep.Changes {
		vendored = vendored || (c.Kind == "vendor-tree" && strings.Contains(c.Diff, "D third_party/lib/STALE.txt"))
	}
	if !vendored {
		t.Fatalf("%+v", rep.Changes)
	}
	must(t, campaign.Rollback(ws, rep.ID))
	restored, _ := os.ReadFile(filepath.Join(old, "STALE.txt"))
	if string(restored) != "previous copy\n" {
		t.Fatal("vendored directory not restored")
	}
	gomodAfter, _ := os.ReadFile(filepath.Join(ws, "go.mod"))
	if string(gomodAfter) != string(gomodBefore) {
		t.Fatalf("go.mod not restored:\n%s", gomodAfter)
	}
}

// setModuleCache points GOMODCACHE at a temp directory. go mod download extracts
// module trees read-only, so the cache is made writable again before TempDir
// cleanup; otherwise RemoveAll fails with permission denied.
func setModuleCache(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "modcache")
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			mode := os.FileMode(0o644)
			if d.IsDir() {
				mode = 0o755
			}
			_ = os.Chmod(path, mode)
			return nil
		})
	})
	t.Setenv("GOMODCACHE", dir)
}
