package modmove

import (
	"strings"
	"testing"
)

func TestInModule(t *testing.T) {
	cases := []struct {
		path, mod string
		want      bool
	}{
		{"github.com/googleapis/gax-go", "github.com/googleapis/gax-go", true},
		{"github.com/googleapis/gax-go/internal", "github.com/googleapis/gax-go", true},
		{"github.com/googleapis/gax-go/v2", "github.com/googleapis/gax-go", false},
		{"github.com/googleapis/gax-go/v2/apierror", "github.com/googleapis/gax-go", false},
		{"github.com/googleapis/gax-go-extra", "github.com/googleapis/gax-go", false},
	}
	for _, c := range cases {
		if got := InModule(c.path, c.mod); got != c.want {
			t.Errorf("InModule(%s, %s) = %v", c.path, c.mod, got)
		}
	}
}

func TestRewriteImportsMajor(t *testing.T) {
	src := []byte(`package p

import (
	"context"

	gax "github.com/googleapis/gax-go"
	"github.com/googleapis/gax-go/v2/apierror"
	"github.com/go-chi/chi/middleware"
)

var _ = gax.Invoke
var _ = apierror.APIError{}
var _ = middleware.Logger
var _ context.Context
`)
	out, n, err := RewriteImports("p.go", src, "github.com/googleapis/gax-go", "github.com/googleapis/gax-go/v2")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("changed %d imports:\n%s", n, out)
	}
	s := string(out)
	if !strings.Contains(s, `gax "github.com/googleapis/gax-go/v2"`) || strings.Contains(s, "v2/v2") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "gax.Invoke") {
		t.Fatal("package identifier changed")
	}
}

func TestRewriteImportsSubpackage(t *testing.T) {
	src := []byte("package p\n\nimport \"github.com/go-chi/chi/middleware\"\n\nvar _ = middleware.Logger\n")
	out, n, err := RewriteImports("p.go", src, "github.com/go-chi/chi", "github.com/go-chi/chi/v5")
	if err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	if !strings.Contains(string(out), `"github.com/go-chi/chi/v5/middleware"`) {
		t.Fatal(string(out))
	}
}

func TestMoveRequire(t *testing.T) {
	body := []byte("module example.com/app\n\ngo 1.21\n\nrequire github.com/googleapis/gax-go v0.0.0-20161107002406-da06d194a00e\n")
	out, changed, _, err := MoveRequire("go.mod", body, "github.com/googleapis/gax-go", "github.com/googleapis/gax-go/v2", "v2.0.2")
	if err != nil || !changed {
		t.Fatalf("%v %v", err, changed)
	}
	s := string(out)
	if strings.Contains(s, "da06d194a00e") || !strings.Contains(s, "github.com/googleapis/gax-go/v2 v2.0.2") {
		t.Fatal(s)
	}
	again, changed, _, err := MoveRequire("go.mod", out, "github.com/googleapis/gax-go", "github.com/googleapis/gax-go/v2", "v2.0.2")
	if err != nil || changed || string(again) != string(out) {
		t.Fatalf("second move changed the file: %v\n%s", err, again)
	}
}

func TestSuggestMajorPath(t *testing.T) {
	if got := SuggestMajorPath("github.com/googleapis/gax-go", "v2.0.2+incompatible"); got != "github.com/googleapis/gax-go/v2" {
		t.Fatal(got)
	}
	if got := SuggestMajorPath("github.com/go-chi/chi/v5", "v5.3.2"); got != "" {
		t.Fatal(got)
	}
	if got := SuggestMajorPath("gopkg.in/yaml.v3", "v3.0.1"); got != "" {
		t.Fatal(got)
	}
	if got := SuggestMajorPath("github.com/x/y", "v1.9.0"); got != "" {
		t.Fatal(got)
	}
}

func TestSetReplace(t *testing.T) {
	body := []byte("module example.com/app\n\ngo 1.21\n\nrequire github.com/x/y v1.0.0\n")
	out, changed, err := SetReplace("go.mod", body, "github.com/x/y", "third_party/y")
	if err != nil || !changed {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "replace github.com/x/y => ./third_party/y") {
		t.Fatal(string(out))
	}
}
