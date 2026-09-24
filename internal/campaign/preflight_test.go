package campaign

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/fsio"
	"github.com/evolvectl/evolvectl/internal/state"
)

func TestGoSumDecision(t *testing.T) {
	err := goSumDecision(true, false, nil, "rsc.io/quote", "v1.5.3")
	if err == nil || !strings.Contains(err.Error(), "go.sum was not updated") {
		t.Fatal(err)
	}
	if err := goSumDecision(true, true, nil, "rsc.io/quote", "v1.5.3"); err != nil {
		t.Fatal(err)
	}
	sum := []byte("rsc.io/quote v1.5.3 h1:abc\n")
	if err := goSumDecision(true, false, sum, "rsc.io/quote", "v1.5.3"); err != nil {
		t.Fatal(err)
	}
}

func TestDryRunNamesExternalReplace(t *testing.T) {
	root := t.TempDir()
	mod := "module example.com/app\n\ngo 1.22\n\nrequire example.com/lib v1.0.0\n\nreplace example.com/lib => ../lib\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := &Executor{Store: state.Open(root)}
	_, err := ex.Run(context.Background(), Request{
		Workspace: root, Dependency: "example.com/lib", To: "v1.2.0", DryRun: true, Offline: true, NoAI: true,
	})
	if err == nil || !strings.Contains(err.Error(), "dry-run unsupported") || !strings.Contains(err.Error(), "../lib") {
		t.Fatal(err)
	}
}

func TestRollbackRestoresSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.sum"), []byte("created\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := &domain.RunReport{
		SchemaVersion: "evolvectl.run/v1",
		ID:            "run_test",
		Outcome:       domain.OutcomeSucceeded,
		Changes: []domain.Change{
			{File: "a.go", BeforeHash: fsio.HashBytes([]byte("before\n")), AfterHash: fsio.HashBytes([]byte("after\n"))},
			{File: "new.sum", BeforeHash: "", AfterHash: fsio.HashBytes([]byte("created\n"))},
		},
	}
	st := state.Open(dir)
	if err := st.SaveReport(rep); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(dir, ".evolvectl", "snapshots", rep.ID)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap, "a.go"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(dir, rep.ID); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.go"))
	if err != nil || string(got) != "before\n" {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.sum")); !os.IsNotExist(err) {
		t.Fatal("created file should be removed")
	}
}
