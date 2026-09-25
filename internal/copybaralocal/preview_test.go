package copybaralocal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evolvectl/evolvectl/internal/runner"
	"github.com/evolvectl/evolvectl/internal/skyparse"
)

func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "v2.1.0"},
		{"tag", "v2.1.0"},
	} {
		r := runner.Run(context.Background(), runner.Request{Argv: append([]string{"git"}, args...), Dir: dir})
		if r.ExitCode != 0 {
			t.Fatalf("git %v: %s %s", args, r.Stderr, r.Err)
		}
	}
	return dir
}

func TestPreviewImport(t *testing.T) {
	if _, ok := runner.Look("git"); !ok {
		t.Skip("git is not on PATH")
	}
	origin := gitRepo(t, map[string]string{
		"LICENSE":            "BSD\n",
		"README.md":          "upstream readme\n",
		"v2/go.mod":          "module github.com/googleapis/gax-go/v2\n",
		"v2/invoke.go":       "package gax\n\n// Copyright 2016\nfunc Invoke() {}\n",
		"v2/invoke_test.go":  "package gax\n",
		"v2/apierror/err.go": "package apierror\n\n// Copyright 2016\n",
		".github/ci.yml":     "on: push\n",
		"v1/old.go":          "package gax\n",
	})
	src, err := skyparse.Template(skyparse.InitOptions{
		Name: "import_gax_go", URL: filepath.ToSlash(origin), Ref: "v2.1.0", From: "v2", To: "third_party/gax-go/v2",
		License: true, Exclude: []string{"v2/**/*_test.go"}, Replace: [][2]string{{"Copyright 2016", "Copyright 2016 Google LLC"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sp, err := skyparse.Workflow(src, "import_gax_go")
	if err != nil {
		t.Fatal(err)
	}
	dir, commit, err := Fetch(context.Background(), sp.OriginURL, sp.OriginRef)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	res, err := Apply(sp, dir)
	if err != nil {
		t.Fatal(err)
	}
	res.Commit = commit
	want := "third_party/gax-go/v2/LICENSE,third_party/gax-go/v2/apierror/err.go,third_party/gax-go/v2/go.mod,third_party/gax-go/v2/invoke.go"
	if strings.Join(res.Files, ",") != want {
		t.Fatalf("files %v", res.Files)
	}
	if res.Selected != 4 || len(res.Steps) != 3 || res.Steps[2].Files != 2 || res.Steps[2].Occurrences != 2 || !res.Complete {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(string(res.files["third_party/gax-go/v2/invoke.go"]), "Copyright 2016 Google LLC") {
		t.Fatal("replace not applied")
	}

	dest := t.TempDir()
	must := func(p, body string) {
		full := filepath.Join(dest, filepath.FromSlash(p))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("third_party/gax-go/v2/invoke.go", "package gax\n")
	must("third_party/gax-go/v2/call_option.go", "package gax\n")
	must("receiver/pubsub/client.go", "package pubsub\n")
	if err := res.Compare(sp, dest); err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Deleted, ",") != "third_party/gax-go/v2/call_option.go" || strings.Join(res.Modified, ",") != "third_party/gax-go/v2/invoke.go" || len(res.Added) != 3 {
		t.Fatalf("added %v modified %v deleted %v", res.Added, res.Modified, res.Deleted)
	}

	out := filepath.Join(t.TempDir(), "preview")
	if err := res.Write(out); err != nil {
		t.Fatal(err)
	}
	if err := res.Write(out); err != nil {
		t.Fatalf("rewrite own preview: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "third_party", "gax-go", "v2", "invoke.go")); err != nil {
		t.Fatal(err)
	}
	if err := res.Write(dest); err == nil {
		t.Fatal("overwrote a directory that is not a preview")
	}
}

func TestPreviewNoopAndOverwrite(t *testing.T) {
	sp := skyparse.Spec{
		Name: "w", OriginFiles: []string{"**"}, DestFiles: []string{"**"},
		Steps: []skyparse.Step{
			{Kind: "replace", Before: "absent", After: "x"},
			{Kind: "move", Before: "a", After: "b"},
		},
	}
	src := t.TempDir()
	for _, p := range []string{"a/x.go", "b/x.go"} {
		_ = os.MkdirAll(filepath.Join(src, filepath.Dir(p)), 0o755)
		_ = os.WriteFile(filepath.Join(src, filepath.FromSlash(p)), []byte("x"), 0o644)
	}
	res, err := Apply(sp, src)
	if err == nil || !strings.Contains(err.Error(), "would overwrite b/x.go") {
		t.Fatalf("%v", err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "--ignore-noop") {
		t.Fatalf("%v", res.Warnings)
	}
}
