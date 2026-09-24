package skyparse

import (
	"strings"
	"testing"
)

func TestPinInlineRef(t *testing.T) {
	src := `core.workflow(
    name = "default",
    # origin = git.origin(ref = "commented"),
    origin = git.origin(
        url = "https://github.com/googleapis/gax-go",
        ref = "da06d194a00e",  # pinned for v0
    ),
    destination = folder.destination(),
)

core.workflow(
    name = "other",
    origin = git.origin(url = "https://example.com/x", ref = "main"),
    destination = folder.destination(),
)
`
	out, old, err := Pin(src, "default", "v2.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if old != "da06d194a00e" {
		t.Fatalf("old %q", old)
	}
	want := strings.Replace(src, `ref = "da06d194a00e",`, `ref = "v2.0.2",`, 1)
	if out != want {
		t.Fatalf("got\n%s", out)
	}
	out, old, err = Pin(src, "other", "v1")
	if err != nil || old != "main" || !strings.Contains(out, `ref = "v1")`) || !strings.Contains(out, `"da06d194a00e"`) {
		t.Fatalf("%v %q\n%s", err, old, out)
	}
}

func TestPinVariables(t *testing.T) {
	src := `GAX_REF = "da06d194a00e"
gax_origin = git.origin(url = "https://github.com/googleapis/gax-go", ref = GAX_REF)

core.workflow(name = "default", origin = gax_origin, destination = folder.destination())
`
	out, old, err := Pin(src, "default", "v2.0.2")
	if err != nil || old != "da06d194a00e" {
		t.Fatalf("%v %q", err, old)
	}
	if !strings.HasPrefix(out, `GAX_REF = "v2.0.2"`+"\n") || !strings.Contains(out, "ref = GAX_REF)") {
		t.Fatal(out)
	}
}

func TestPinInsertsRef(t *testing.T) {
	one := `core.workflow(name = "default", origin = git.origin(url = "u"), destination = d)`
	out, old, err := Pin(one, "default", "abc")
	if err != nil || old != "" || !strings.Contains(out, `git.origin(url = "u", ref = "abc")`) {
		t.Fatalf("%v\n%s", err, out)
	}
	multi := "core.workflow(\n    name = \"default\",\n    origin = git.origin(\n        url = \"u\",\n    ),\n)\n"
	out, _, err = Pin(multi, "default", "abc")
	if err != nil || !strings.Contains(out, "        url = \"u\",\n        ref = \"abc\",\n    ),") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestPinErrors(t *testing.T) {
	src := `core.workflow(name = "default", origin = git.origin(url = "u", ref = compute()), destination = d)`
	if _, _, err := Pin(src, "missing", "x"); err == nil || !strings.Contains(err.Error(), "workflows: default") {
		t.Fatal(err)
	}
	if _, _, err := Pin(src, "default", "x"); err == nil || !strings.Contains(err.Error(), "not a string") {
		t.Fatal(err)
	}
	if _, _, err := Pin(src, "default", `a"b`); err == nil {
		t.Fatal("quote accepted")
	}
}
