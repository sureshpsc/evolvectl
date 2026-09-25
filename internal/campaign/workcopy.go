package campaign

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/runner"
)

// dryRunTree builds the throwaway tree a dry run edits. A detached git
// worktree checks out only tracked files, so it is much cheaper than copying
// a large repository, but it cannot carry uncommitted edits or ignored files.
func dryRunTree(ctx context.Context, rc *runCtx) (root, reason string, cleanup func(), err error) {
	mode := rc.req.Config.Execution.DryRunCopy
	if mode == "" {
		mode = "auto"
	}
	if mode != "copy" {
		root, cleanup, why := gitWorktree(ctx, rc.origRoot)
		if root != "" {
			return root, "changes are applied only in a temporary git worktree at HEAD; files ignored by git are not in it (set execution.dry_run_copy: copy if the build needs them)", cleanup, nil
		}
		if mode == "worktree" {
			return "", "", nil, fmt.Errorf("execution.dry_run_copy is worktree but %s", why)
		}
	}
	dir, err := os.MkdirTemp("", "evolvectl-dry-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	if err := copyTree(rc.origRoot, dir); err != nil {
		cleanup()
		return "", "", nil, err
	}
	return dir, "changes are applied only in a temporary copy", cleanup, nil
}

// gitWorktree returns the workspace path inside a fresh detached worktree, or
// an empty root and the reason one could not be used.
func gitWorktree(ctx context.Context, workspace string) (root string, cleanup func(), why string) {
	gitBin, ok := runner.Look("git")
	if !ok {
		return "", nil, "git is not on PATH"
	}
	git := func(dir string, args ...string) domain.ToolRun {
		return runner.Run(ctx, runner.Request{Argv: append([]string{gitBin}, args...), Dir: dir, Timeout: 2 * time.Minute})
	}
	top := git(workspace, "rev-parse", "--show-toplevel")
	if top.ExitCode != 0 {
		return "", nil, "the workspace is not inside a git repository"
	}
	if git(workspace, "rev-parse", "--verify", "--quiet", "HEAD").ExitCode != 0 {
		return "", nil, "the repository has no commits"
	}
	st := git(workspace, "status", "--porcelain", "--", ".")
	if st.ExitCode != 0 {
		return "", nil, "git status failed"
	}
	if strings.TrimSpace(st.Stdout) != "" {
		return "", nil, "the workspace has uncommitted or untracked files"
	}
	prefix := strings.TrimSpace(git(workspace, "rev-parse", "--show-prefix").Stdout)
	topDir := filepath.FromSlash(strings.TrimSpace(top.Stdout))
	tmp, err := os.MkdirTemp("", "evolvectl-dry-*")
	if err != nil {
		return "", nil, err.Error()
	}
	wt := filepath.Join(tmp, "wt")
	if add := git(topDir, "worktree", "add", "--detach", "--quiet", wt, "HEAD"); add.ExitCode != 0 {
		_ = os.RemoveAll(tmp)
		return "", nil, "git worktree add failed: " + strings.TrimSpace(add.Stderr)
	}
	cleanup = func() {
		git(topDir, "worktree", "remove", "--force", wt)
		_ = os.RemoveAll(tmp)
		git(topDir, "worktree", "prune")
	}
	return filepath.Join(wt, filepath.FromSlash(prefix)), cleanup, ""
}
