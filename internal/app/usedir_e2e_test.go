package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/state"
)

// useDirWorkspace is a repository with lib v1 in third_party/lib and v2 already imported
// into third_party/lib/v2, the way a sync tool leaves it.
func useDirWorkspace(t *testing.T, withV2 bool) string {
	t.Helper()
	ws := t.TempDir()
	files := map[string]string{
		"go.mod":                 "module example.com/app\n\ngo 1.21\n\nrequire example.com/lib v1.0.0\n\nreplace example.com/lib => ./third_party/lib\n",
		"app.go":                 "package app\n\nimport \"example.com/lib\"\n\nfunc Greet() string { return lib.Hello() }\n",
		"app_test.go":            "package app\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n",
		"third_party/lib/go.mod": "module example.com/lib\n\ngo 1.21\n",
		"third_party/lib/lib.go": "package lib\n\nfunc Hello() string { return \"v1\" }\n\nfunc Old() {}\n",
	}
	if withV2 {
		files["third_party/lib/v2/go.mod"] = "module example.com/lib/v2\n\ngo 1.21\n"
		files["third_party/lib/v2/lib.go"] = "package lib\n\nfunc Hello() string { return \"v2\" }\n"
	}
	for rel, body := range files {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(body), 0o644))
	}
	return ws
}

func TestUpgradeUsesImportedCopy(t *testing.T) {
	ws := useDirWorkspace(t, true)
	var a App
	buf := &strings.Builder{}
	a.Out, a.Err = buf, &strings.Builder{}
	opt := Option{Workspace: ws, NoAI: true, Offline: true, ToModule: "example.com/lib/v2", UseDir: "third_party/lib/v2"}
	if _, err := a.Upgrade(context.Background(), opt, "example.com/lib", "v2.0.0", ""); err != nil {
		t.Fatalf("%v\n%s", err, buf)
	}
	mod, _ := os.ReadFile(filepath.Join(ws, "go.mod"))
	if !strings.Contains(string(mod), "example.com/lib/v2 v2.0.0") || !strings.Contains(string(mod), "example.com/lib/v2 => ./third_party/lib/v2") {
		t.Fatalf("go.mod:\n%s", mod)
	}
	src, _ := os.ReadFile(filepath.Join(ws, "app.go"))
	if !strings.Contains(string(src), `"example.com/lib/v2"`) {
		t.Fatalf("app.go:\n%s", src)
	}
	if _, err := os.Stat(filepath.Join(ws, "third_party", "lib", "v2", "METADATA.evolvectl.json")); err == nil {
		t.Fatal("the imported copy was modified")
	}
	runs, err := state.Open(ws).ListReports()
	if err != nil || len(runs) == 0 {
		t.Fatal(err)
	}
	rep := runs[0]
	gates := map[string]string{}
	for _, v := range rep.Validations {
		gates[v.Validator] = v.Status
	}
	if gates["manifest-target"] != domain.GatePass || gates["go test"] != domain.GatePass || gates["module-move"] != domain.GatePass {
		t.Fatalf("gates %v\n%s", gates, buf)
	}
	if rep.Impact == nil || rep.Impact.Status != "recorded" || rep.Impact.Removed != 1 {
		t.Fatalf("impact %+v", rep.Impact)
	}
}

func TestUpgradeUseDirMissingIsBlocked(t *testing.T) {
	ws := useDirWorkspace(t, false)
	var a App
	a.Out, a.Err = &strings.Builder{}, &strings.Builder{}
	opt := Option{Workspace: ws, NoAI: true, Offline: true, ToModule: "example.com/lib/v2", UseDir: "third_party/lib/v2"}
	if _, err := a.Upgrade(context.Background(), opt, "example.com/lib", "v2.0.0", ""); err == nil || !strings.Contains(err.Error(), "has no go.mod") {
		t.Fatalf("err = %v", err)
	}
	mod, _ := os.ReadFile(filepath.Join(ws, "go.mod"))
	if strings.Contains(string(mod), "lib/v2") {
		t.Fatalf("go.mod changed:\n%s", mod)
	}
}
