package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/exitcode"
	"github.com/sureshpsc/evolvectl/internal/fsio"
	"github.com/sureshpsc/evolvectl/internal/runner"
	"github.com/sureshpsc/evolvectl/internal/scm"
)

func batchWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	for name, example := range map[string]string{"payments": "git-go-grpc", "speech": "go-gax-v2"} {
		src := copyExample(t, example)
		if err := os.Rename(src, filepath.Join(ws, name)); err != nil {
			t.Fatal(err)
		}
	}
	sheet := "upgrades:\n" +
		"  - dependency: google.golang.org/grpc\n    to: v1.75.0\n    workspace: payments\n    owner: Alekhya\n" +
		"  - dependency: github.com/googleapis/gax-go\n    to: v2.0.2\n    to_module: github.com/googleapis/gax-go/v2\n    workspace: speech\n    owner: Suresh\n"
	if err := os.WriteFile(filepath.Join(ws, "upgrades.yaml"), []byte(sheet), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

func loadBatch(t *testing.T, ws string) *domain.BatchReport {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(ws, ".evolvectl", "batches"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("batches %v %v", entries, err)
	}
	b, err := os.ReadFile(filepath.Join(ws, ".evolvectl", "batches", entries[0].Name(), "batch.json"))
	if err != nil {
		t.Fatal(err)
	}
	var br domain.BatchReport
	if err := json.Unmarshal(b, &br); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".evolvectl", "batches", entries[0].Name(), "batch.html")); err != nil {
		t.Fatal(err)
	}
	return &br
}

func TestBatchDryRunLeavesWorkspace(t *testing.T) {
	ws := batchWorkspace(t)
	before, _ := fsio.HashFile(filepath.Join(ws, "speech", "speech", "client.go"))
	var a App
	buf := &strings.Builder{}
	a.Out, a.Err = buf, &strings.Builder{}
	code, err := a.Batch(context.Background(), Option{Workspace: ws, Offline: true, NoAI: true}, "upgrades.yaml", BatchDryRun, false)
	if err != nil || code != exitcode.NeedsReview {
		t.Fatalf("code=%d err=%v\n%s", code, err, buf)
	}
	after, _ := fsio.HashFile(filepath.Join(ws, "speech", "speech", "client.go"))
	if before != after {
		t.Fatal("dry-run batch changed source")
	}
	br := loadBatch(t, ws)
	if len(br.Rows) != 2 || br.Rows[0].Outcome != domain.OutcomeSucceeded || br.Rows[1].Outcome != domain.OutcomeSucceededWithReview {
		t.Fatalf("%+v", br.Rows)
	}
	if br.Rows[1].ImpactSites == 0 || br.Rows[1].Owner != "Suresh" || br.Rows[0].CoverageAfter == "" {
		t.Fatalf("row detail %+v", br.Rows)
	}
	if !strings.Contains(buf.String(), "succeeded 1, needs review 1, failed 0") {
		t.Fatal(buf.String())
	}
}

func TestBatchBranchesCommitsEachRow(t *testing.T) {
	if _, ok := runner.Look("git"); !ok {
		t.Skip("git is not on PATH")
	}
	ws := batchWorkspace(t)
	ex := scm.Exec{}
	ctx := context.Background()
	for _, args := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "add", "-A"},
		{"git", "commit", "-q", "-m", "init"},
	} {
		if _, err := ex.Run(ctx, ws, args...); err != nil {
			t.Fatal(err)
		}
	}
	var a App
	buf := &strings.Builder{}
	a.Out, a.Err = buf, &strings.Builder{}
	code, err := a.Batch(ctx, Option{Workspace: ws, Offline: true, NoAI: true}, "upgrades.yaml", BatchBranches, false)
	if err != nil || code != exitcode.NeedsReview {
		t.Fatalf("code=%d err=%v\n%s", code, err, buf)
	}
	br := loadBatch(t, ws)
	for _, r := range br.Rows {
		if r.Branch == "" {
			t.Fatalf("row %d has no branch: %+v", r.Index, r)
		}
	}
	head, _ := ex.Run(ctx, ws, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if strings.TrimSpace(head) != "main" {
		t.Fatalf("left on %s", head)
	}
	mainClient, _ := os.ReadFile(filepath.Join(ws, "speech", "speech", "client.go"))
	if strings.Contains(string(mainClient), "gax-go/v2") {
		t.Fatal("main was modified")
	}
	show, err := ex.Run(ctx, ws, "git", "show", br.Rows[1].Branch+":speech/speech/client.go")
	if err != nil || !strings.Contains(show, "gax-go/v2") || !strings.Contains(show, "_ gax.CallSettings") {
		t.Fatalf("%v\n%s", err, show)
	}
	grpcOnGax, _ := ex.Run(ctx, ws, "git", "show", br.Rows[1].Branch+":payments/client/client.go")
	if strings.Contains(grpcOnGax, "NewClient") {
		t.Fatal("rows share a branch")
	}
}
