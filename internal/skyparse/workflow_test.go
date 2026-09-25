package skyparse

import (
	"strings"
	"testing"
)

func TestWorkflowSpec(t *testing.T) {
	src := `gax_url = "https://github.com/googleapis/gax-go.git"
gax_ref = "v2.24.1"  # the version otel uses
gax = git.origin(url = gax_url, ref = gax_ref)

core.workflow(
    name = "import_gax_go",
    origin = gax,
    origin_files = glob(["v2/**", "LICENSE"], exclude = ["v2/**/*_test.go"]),
    destination = folder.destination(),
    destination_files = glob(["third_party/gax-go/v2/**"]),
    transformations = [
        metadata.squash_notes(),
        core.move("LICENSE", "third_party/gax-go/v2/LICENSE", overwrite = True),
        core.move("v2", "third_party/gax-go/v2"),
        core.replace(before = "Copyright 2016", after = "Copyright 2016 Google", paths = glob(["**/*.go"])),
        core.replace(before = "${x}", after = "y", regex_groups = {"x": "a+"}),
        core.transform([core.move("a", "b")]),
    ],
)
`
	sp, err := Workflow(src, "import_gax_go")
	if err != nil {
		t.Fatal(err)
	}
	if sp.OriginURL != "https://github.com/googleapis/gax-go.git" || sp.OriginRef != "v2.24.1" || sp.Destination != "folder.destination" {
		t.Fatalf("%+v", sp)
	}
	if strings.Join(sp.OriginFiles, ",") != "v2/**,LICENSE" || strings.Join(sp.OriginExclude, ",") != "v2/**/*_test.go" {
		t.Fatalf("files %+v", sp)
	}
	if len(sp.Steps) != 3 || sp.Steps[0].Kind != "move" || !sp.Steps[0].Overwrite || sp.Steps[2].Kind != "replace" || sp.Steps[2].Paths[0] != "**/*.go" {
		t.Fatalf("steps %+v", sp.Steps)
	}
	if len(sp.Ignored) != 1 || len(sp.Unsupported) != 2 {
		t.Fatalf("ignored %v unsupported %v", sp.Ignored, sp.Unsupported)
	}
}

func TestMatchCopybaraGlobs(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/main.go", true},
		{"*.go", "a/main.go", false},
		{"v2/**", "v2/call_option.go", true},
		{"v2/**", "v3/x.go", false},
		{"**", "any/thing", true},
		{"LICENSE", "LICENSE", true},
	}
	for _, c := range cases {
		if got := Match([]string{c.glob}, nil, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.glob, c.path, got)
		}
	}
	if Match([]string{"**"}, []string{"**/*_test.go"}, "v2/x_test.go") {
		t.Error("exclude ignored")
	}
}

func TestTemplateRoundTrip(t *testing.T) {
	out, err := Template(InitOptions{
		Name: "import_gax_go", URL: "https://github.com/googleapis/gax-go.git", Ref: "v2.24.1",
		From: "v2", To: "third_party/gax-go/v2", License: true, Exclude: []string{"v2/**/testdata/**"},
		Replace: [][2]string{{"old", "new"}}, DestinationURL: "file:///tmp/dest",
	})
	if err != nil {
		t.Fatal(err)
	}
	sp, err := Workflow(out, "import_gax_go")
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.Unsupported) != 0 || sp.OriginRef != "v2.24.1" || sp.Destination != "git.destination" {
		t.Fatalf("%+v\n%s", sp, out)
	}
	if strings.Join(sp.DestFiles, ",") != "third_party/gax-go/v2/**" || len(sp.Steps) != 3 {
		t.Fatalf("%+v\n%s", sp, out)
	}
	if !strings.Contains(out, "folder.destination") && !strings.Contains(out, "git.destination") {
		t.Fatal(out)
	}
	pinned, old, err := Pin(out, "import_gax_go", "v2.0.2")
	if err != nil || old != "v2.24.1" || !strings.Contains(pinned, `ref = "v2.0.2"`) {
		t.Fatalf("%v %q", err, old)
	}
	if _, err := Template(InitOptions{Name: "x", URL: "u", Ref: "r", To: "../escape"}); err == nil {
		t.Fatal("escape accepted")
	}
}
