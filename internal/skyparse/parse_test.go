package skyparse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIgnoresCommentLines(t *testing.T) {
	src := "core.workflow(\n    name = \"w\",\n    origin_files = glob([\"a/**\"]),\n    # only this folder\n    destination_files = glob([\"b/**\"]),  # owned here\n)\n"
	parsed := Parse("copy.bara.sky", src)
	if len(parsed.Workflows) != 1 {
		t.Fatalf("%+v", parsed)
	}
	wf := parsed.Workflows[0]
	if len(wf.Unresolved) != 0 || strings.Join(wf.DestFiles, ",") != "b/**" {
		t.Fatalf("unresolved %v destination %v", wf.Unresolved, wf.DestFiles)
	}
}

func TestExportWorkflow(t *testing.T) {
	root := findModule(t)
	b, err := os.ReadFile(filepath.Join(root, "examples", "mixed-monorepo", "copy.bara.sky"))
	if err != nil {
		t.Fatal(err)
	}
	parsed := Parse("copy.bara.sky", string(b))
	if len(parsed.Workflows) != 1 {
		t.Fatalf("%+v", parsed)
	}
	wf := parsed.Workflows[0]
	if wf.Name != "export-go-lib" {
		t.Fatal(wf.Name)
	}
	if !strings.Contains(wf.Origin, "https://github.com/example/source.git") || strings.Contains(wf.Origin, "source_url") {
		t.Fatalf("origin %s", wf.Origin)
	}
	if strings.Contains(wf.Destination, "ghp_") || strings.Contains(wf.Destination, "user:") {
		t.Fatalf("secret leaked: %s", wf.Destination)
	}
	moved := TraceFile(wf, "go/client/client.go")
	if !moved.Included || moved.OutputPath != "third_party/go/client/client.go" || moved.ContentModified {
		t.Fatalf("%+v", moved)
	}
	vendor := TraceFile(wf, "go/vendor/x.go")
	if vendor.Included || vendor.Stage != "origin_files exclude" {
		t.Fatalf("%+v", vendor)
	}
}

func findModule(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no module")
		}
		dir = parent
	}
}
