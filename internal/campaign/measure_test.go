package campaign

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/evolvectl/evolvectl/internal/config"
	"github.com/evolvectl/evolvectl/internal/domain"
)

func TestGoSuiteRunsModulesInParallelInOrder(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	root := t.TempDir()
	projects := []string{"b", "a", "c"}
	for _, p := range projects {
		dir := filepath.Join(root, p)
		files := map[string]string{
			"go.mod":    "module example.com/" + p + "\n\ngo 1.22\n",
			"x.go":      "package x\n\nfunc One() int { return 1 }\n",
			"x_test.go": "package x\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal()\n\t}\n}\n",
		}
		if p == "c" {
			files["x_test.go"] = "package x\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) { t.Fatal(\"broken\") }\n"
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cfg := config.Default()
	cfg.Execution.ValidatorWorkers = 3
	cfg.Validation.TestCache = true
	rc := &runCtx{
		origRoot: root, workRoot: root,
		req:    Request{Config: cfg, Offline: true},
		report: &domain.RunReport{ID: "run_par", Plan: &domain.Plan{Projects: projects}},
	}
	(&Executor{}).runGoSuite(context.Background(), rc, "after", true)
	var scopes, states []string
	for _, g := range rc.report.Validations {
		scopes = append(scopes, g.Scope)
		states = append(states, g.Status)
		if slices.Contains(g.Command, "-count=1") {
			t.Fatalf("test cache is on but argv forces -count=1: %v", g.Command)
		}
	}
	if !slices.Equal(scopes, projects) {
		t.Fatalf("gates %v should follow module order %v", scopes, projects)
	}
	if !slices.Equal(states, []string{domain.GatePass, domain.GatePass, domain.GateFail}) {
		t.Fatalf("states %v", states)
	}
}
