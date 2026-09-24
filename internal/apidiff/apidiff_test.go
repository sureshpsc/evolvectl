package apidiff

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFindsGaxStyleBreaks(t *testing.T) {
	before := t.TempDir()
	after := t.TempDir()
	ws := t.TempDir()
	write(t, before, "invoke.go", `package gax
import "context"
type APICall func(context.Context) error
type CallOption interface{ Resolve(*CallSettings) }
type CallSettings struct{ Retry func() int }
func Invoke(ctx context.Context, call APICall, opts ...CallOption) error { return nil }
func MustCompilePathTemplate(t string) *PathTemplate { return nil }
type PathTemplate struct{ segments []string }
func (pt *PathTemplate) Render(b map[string]string) (string, error) { return "", nil }
`)
	write(t, before, "internal/x/x.go", "package x\nfunc Hidden() {}\n")
	write(t, after, "invoke.go", `package gax
import "context"
type APICall func(context.Context, CallSettings) error
type CallOption interface{ Resolve(*CallSettings) }
type CallSettings struct{ Retry func() int; GRPC []string }
func Invoke(ctx context.Context, call APICall, opts ...CallOption) error { return nil }
func XGoogHeader(kv ...string) string { return "" }
`)
	write(t, ws, "client/client.go", `package client
import (
	"context"
	gax "github.com/googleapis/gax-go"
)
var tpl = gax.MustCompilePathTemplate("projects/{p}")
func Call(ctx context.Context) error {
	return gax.Invoke(ctx, func(ctx context.Context) error { return nil })
}
`)
	imp := Build(ws, []string{"client/client.go"}, "github.com/googleapis/gax-go", "v0.0.0-20161107002406-da06d194a00e",
		"github.com/googleapis/gax-go/v2", "v2.0.2", before, after)
	if imp.Status != "recorded" {
		t.Fatal(imp.Reason)
	}
	got := map[string]string{}
	for _, c := range imp.Changes {
		got[c.Symbol] = c.Change
	}
	want := map[string]string{
		"APICall": "changed", "CallSettings": "changed", "Invoke": "indirect",
		"MustCompilePathTemplate": "removed", "PathTemplate": "removed", "PathTemplate.Render": "removed",
		"XGoogHeader": "added",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	if _, ok := got["Hidden"]; ok {
		t.Error("internal package leaked into the API")
	}
	sites := map[string]int{}
	for _, s := range imp.Sites {
		sites[s.Symbol] = s.Line
	}
	if sites["MustCompilePathTemplate"] != 6 || sites["Invoke"] != 8 {
		t.Fatalf("sites %+v", imp.Sites)
	}
	if imp.FilesAffected != 1 || imp.MethodsChanged != 1 {
		t.Fatalf("files %d methods %d", imp.FilesAffected, imp.MethodsChanged)
	}
}

func TestImportName(t *testing.T) {
	cases := map[string]string{
		"github.com/googleapis/gax-go":    "gax",
		"github.com/googleapis/gax-go/v2": "gax",
		"gopkg.in/yaml.v3":                "yaml",
		"github.com/go-chi/chi/v5":        "chi",
		"github.com/paulmach/go.geojson":  "geojson",
	}
	for in, want := range cases {
		if got := importName(in); got != want {
			t.Errorf("importName(%s) = %s, want %s", in, got, want)
		}
	}
}
