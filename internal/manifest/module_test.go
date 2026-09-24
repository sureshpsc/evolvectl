package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalReplaceAndSum(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "examples", "git-go-grpc", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !HasLocalReplace(body, "google.golang.org/grpc") {
		t.Fatal("expected local replace")
	}
	if HasLocalReplace(body, "example.com/other") {
		t.Fatal("unexpected replace")
	}
	sum := []byte("rsc.io/quote v1.5.2 h1:abc\nrsc.io/quote v1.5.2/go.mod h1:def\n")
	if !SumContains(sum, "rsc.io/quote", "v1.5.2") {
		t.Fatal("missing sum")
	}
	if SumContains(sum, "rsc.io/quote", "v1.5.3") {
		t.Fatal("newer version should be absent")
	}
}

func TestExternalReplace(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module example.com/app\n\ngo 1.22\n\nrequire example.com/lib v1.0.0\n\nreplace example.com/lib => ../lib\n"
	if err := os.WriteFile(filepath.Join(app, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ExternalReplaces(app, []string{"go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "../lib" || !strings.Contains(FormatExternalReplace(got[0]), "dry-run unsupported") {
		t.Fatalf("%+v", got)
	}
	inside := "module example.com/app\n\ngo 1.22\n\nrequire example.com/lib v1.0.0\n\nreplace example.com/lib => ./lib\n"
	if err := os.WriteFile(filepath.Join(app, "go.mod"), []byte(inside), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ExternalReplaces(app, []string{"go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("inside replace reported: %+v", got)
	}
}
