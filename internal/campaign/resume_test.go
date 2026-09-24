package campaign

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evolvectl/evolvectl/internal/recipe"
	"github.com/evolvectl/evolvectl/internal/state"
)

func TestResumeRejectsStalePlan(t *testing.T) {
	dir := copyExample(t, "git-go-grpc")
	recipes, err := recipe.LoadDir(filepath.Join(moduleRoot(t), "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	ex := &Executor{Store: state.Open(dir)}
	rep, err := ex.Run(context.Background(), Request{
		Workspace: dir, Dependency: "google.golang.org/grpc", To: "v1.75.0", NoAI: true,
		HaltAfter: "plan", Recipes: recipes,
	})
	if err != ErrInterrupted {
		outcome := ""
		if rep != nil {
			outcome = rep.Outcome
		}
		t.Fatalf("err=%v outcome=%s", err, outcome)
	}
	client := filepath.Join(dir, "client", "client.go")
	body, err := os.ReadFile(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(client, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ex.Run(context.Background(), Request{Workspace: dir, ResumeID: rep.ID, NoAI: true, Recipes: recipes})
	if err == nil || !strings.Contains(err.Error(), "stale plan") {
		t.Fatalf("err=%v", err)
	}
}

func copyExample(t *testing.T, example string) string {
	t.Helper()
	src := filepath.Join(moduleRoot(t), "examples", example)
	dst := t.TempDir()
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	return dst
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(b), "module github.com/evolvectl/evolvectl") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}
