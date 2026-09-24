package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoldenRecipes(t *testing.T) {
	root := moduleRoot(t)
	code := 0
	list, err := LoadDir(filepath.Join(root, "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 9 {
		t.Fatalf("recipes = %d", len(list))
	}
	for _, r := range list {
		inDir := filepath.Join(filepath.Dir(r.Path), "testdata", "input")
		entries, err := os.ReadDir(inDir)
		if err != nil {
			t.Fatalf("%s: %v", r.Metadata.ID, err)
		}
		vc := VersionContext{Ecosystem: r.Spec.Dependency.Ecosystem, Name: r.Spec.Dependency.Name}
		for _, cur := range []string{"1.10.2", "v1.58.3", "1.58.3"} {
			if ConstraintsAllow(r.Spec.Dependency.From, cur) {
				vc.Current = cur
				break
			}
		}
		for _, tgt := range []string{"2.11.0", "v1.75.0", "1.75.0"} {
			if ConstraintsAllow(r.Spec.Dependency.To, tgt) {
				vc.Target = tgt
				break
			}
		}
		if err := Validate(r); err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			in, err := os.ReadFile(filepath.Join(inDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out, changes, err := ApplySource(e.Name(), in, []Recipe{r}, vc)
			if err != nil {
				t.Fatalf("%s: %v", r.Metadata.ID, err)
			}
			exp, err := os.ReadFile(filepath.Join(filepath.Dir(r.Path), "testdata", "expected", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != string(exp) {
				t.Errorf("%s %s\n got:\n%s\nwant:\n%s", r.Metadata.ID, e.Name(), out, exp)
			}
			if len(changes) == 0 {
				t.Errorf("%s made no change", r.Metadata.ID)
			}
			code++
		}
	}
	if code < 9 {
		t.Fatalf("golden files %d", code)
	}
}

func TestDoesNotRewriteOtherPackage(t *testing.T) {
	src := []byte("package p\n\nimport \"other.com/lib\"\n\nfunc F() { lib.Old() }\n")
	// The file's import name is lib but the path is not example.com/lib.
	// Parse will not match because the import path differs. Use a selector that
	// matches the local name only when the path matches.
	src = []byte("package p\n\nimport \"other.com/lib\"\n\nfunc F() { lib.Old() }\n")
	recipes, err := LoadDir(filepath.Join(moduleRoot(t), "recipes", "go", "calls", "rename"))
	if err != nil {
		t.Fatal(err)
	}
	out, changes, err := ApplySource("file.go", src, recipes, VersionContext{
		Ecosystem: "go", Name: "example.com/lib", Current: "v1.58.3", Target: "v1.75.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || string(out) != string(src) {
		t.Fatalf("rewrote unrelated import:\n%s", out)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}
