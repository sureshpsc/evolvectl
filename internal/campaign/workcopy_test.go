package campaign

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/config"
	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/state"
)

func gitRepo(t *testing.T) (top, workspace string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	top = t.TempDir()
	workspace = filepath.Join(top, "app")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = top
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return top, workspace
}

func worktreeCount(t *testing.T, top string) int {
	t.Helper()
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = top
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(out), "worktree ")
}

func treeFor(t *testing.T, workspace, mode string) (string, string, func(), error) {
	t.Helper()
	cfg := config.Default()
	cfg.Execution.DryRunCopy = mode
	return dryRunTree(context.Background(), &runCtx{origRoot: workspace, req: Request{Config: cfg}})
}

func TestDryRunTreeUsesWorktreeWhenClean(t *testing.T) {
	top, ws := gitRepo(t)
	root, reason, cleanup, err := treeFor(t, ws, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "git worktree") {
		t.Fatalf("reason %q", reason)
	}
	if filepath.Base(root) != "app" {
		t.Fatalf("root %s should be the workspace inside the worktree", root)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	if worktreeCount(t, top) != 2 {
		t.Fatal("expected the temporary worktree to be registered")
	}
	cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("worktree still on disk: %v", err)
	}
	if worktreeCount(t, top) != 1 {
		t.Fatal("temporary worktree was not removed")
	}
}

func TestDryRunTreeCopiesWhenDirty(t *testing.T) {
	_, ws := gitRepo(t)
	if err := os.WriteFile(filepath.Join(ws, "new.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, reason, cleanup, err := treeFor(t, ws, "auto")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if strings.Contains(reason, "worktree") {
		t.Fatalf("dirty workspace must be copied, got %q", reason)
	}
	if _, err := os.Stat(filepath.Join(root, "new.go")); err != nil {
		t.Fatal("copy must include the untracked file")
	}
	if _, _, _, err := treeFor(t, ws, "worktree"); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("worktree mode on a dirty workspace should fail, got %v", err)
	}
}

func TestDryRunTreeCopyMode(t *testing.T) {
	_, ws := gitRepo(t)
	_, reason, cleanup, err := treeFor(t, ws, "copy")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if strings.Contains(reason, "worktree") {
		t.Fatalf("copy mode used %q", reason)
	}
}

func TestResumeRefusesDryRun(t *testing.T) {
	dir := t.TempDir()
	st := state.Open(dir)
	rep := &domain.RunReport{SchemaVersion: "evolvectl.run/v1", ID: "run_dry", DryRun: true, Outcome: domain.OutcomeCancelled}
	if err := st.SaveReport(rep); err != nil {
		t.Fatal(err)
	}
	_, err := (&Executor{Store: st}).Run(context.Background(), Request{Workspace: dir, ResumeID: rep.ID, NoAI: true})
	if err == nil || !strings.Contains(err.Error(), "dry run") {
		t.Fatal(err)
	}
}
