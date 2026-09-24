// Package scm commits a run on its own branch, pushes it, and opens a pull request with gh.
// It adds only the files the run recorded as changed. It never rewrites history, never
// force-pushes, and never changes git config.
package scm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/runner"
)

// Runner executes git or gh in dir.
type Runner interface {
	Run(ctx context.Context, dir string, argv ...string) (string, error)
}

// Exec is the real Runner.
type Exec struct{}

// Run executes argv without a shell.
func (Exec) Run(ctx context.Context, dir string, argv ...string) (string, error) {
	bin, ok := runner.Look(argv[0])
	if !ok {
		return "", fmt.Errorf("%s is not on PATH", argv[0])
	}
	run := runner.Run(ctx, runner.Request{Argv: append([]string{bin}, argv[1:]...), Dir: dir, Timeout: 5 * time.Minute})
	if run.ExitCode != 0 {
		msg := strings.TrimSpace(run.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(run.Stdout + " " + run.Err)
		}
		return run.Stdout, fmt.Errorf("%s %s: %s", argv[0], strings.Join(argv[1:2], " "), msg)
	}
	return run.Stdout, nil
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// BranchName is evolvectl/<dependency>-<version>, with unsafe characters replaced.
func BranchName(t domain.Target) string {
	name := t.Module()
	if parts := strings.Split(name, "/"); len(parts) > 2 {
		name = strings.Join(parts[len(parts)-2:], "-")
	}
	slug := strings.Trim(unsafe.ReplaceAllString(name+"-"+t.To, "-"), "-.")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	return "evolvectl/" + strings.ToLower(slug)
}

// Title is the commit subject and pull request title.
func Title(t domain.Target) string {
	switch {
	case t.VendorDir != "":
		return fmt.Sprintf("Vendor %s %s into %s", t.Module(), t.To, t.VendorDir)
	case t.ToModule != "" && t.ToModule != t.Name:
		return fmt.Sprintf("Move %s to %s %s", t.Name, t.ToModule, t.To)
	default:
		return fmt.Sprintf("Upgrade %s to %s", t.Name, t.To)
	}
}

// Git wraps one workspace.
type Git struct {
	R   Runner
	Dir string
}

func (g Git) run(ctx context.Context, argv ...string) (string, error) {
	out, err := g.R.Run(ctx, g.Dir, argv...)
	return strings.TrimSpace(out), err
}

// Branch is the checked-out branch name.
func (g Git) Branch(ctx context.Context) (string, error) {
	return g.run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
}

// Head is the current commit.
func (g Git) Head(ctx context.Context) (string, error) {
	return g.run(ctx, "git", "rev-parse", "HEAD")
}

// Dirty lists porcelain entries outside .evolvectl/.
func (g Git) Dirty(ctx context.Context) ([]string, error) {
	out, err := g.run(ctx, "git", "status", "--porcelain", "--", ".")
	if err != nil {
		return nil, err
	}
	var dirty []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		path := strings.TrimSpace(line[min(2, len(line)):])
		if strings.HasPrefix(path, ".evolvectl/") || strings.Contains(path, "/.evolvectl/") || path == ".evolvectl" {
			continue
		}
		dirty = append(dirty, line)
	}
	return dirty, nil
}

// NewBranch creates branch at start (or at HEAD when start is empty) and switches to it.
// A name that already exists gets a numeric suffix.
func (g Git) NewBranch(ctx context.Context, name, start string) (string, error) {
	candidate := name
	for i := 2; i < 50; i++ {
		if _, err := g.run(ctx, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+candidate); err != nil {
			break
		}
		candidate = fmt.Sprintf("%s-%d", name, i)
	}
	argv := []string{"git", "switch", "-c", candidate}
	if start != "" {
		argv = append(argv, start)
	}
	if _, err := g.run(ctx, argv...); err != nil {
		return "", err
	}
	return candidate, nil
}

// Switch checks out an existing branch.
func (g Git) Switch(ctx context.Context, branch string) error {
	_, err := g.run(ctx, "git", "switch", branch)
	return err
}

// Commit stages exactly files (workspace-relative, slash-separated) and commits them.
func (g Git) Commit(ctx context.Context, files []string, subject, body string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("the run recorded no changed files")
	}
	argv := append([]string{"git", "add", "-A", "--"}, files...)
	if _, err := g.run(ctx, argv...); err != nil {
		return "", err
	}
	msg := []string{"git", "commit", "-m", subject}
	if body != "" {
		msg = append(msg, "-m", body)
	}
	if _, err := g.run(ctx, msg...); err != nil {
		return "", err
	}
	return g.Head(ctx)
}

// Push sets upstream on origin. It never forces.
func (g Git) Push(ctx context.Context, branch string) error {
	_, err := g.run(ctx, "git", "push", "-u", "origin", branch)
	return err
}

// OpenPR runs gh pr create and returns the URL gh prints.
func (g Git) OpenPR(ctx context.Context, branch, base, title, bodyFile string, draft bool) (string, error) {
	argv := []string{"gh", "pr", "create", "--head", branch, "--title", title, "--body-file", bodyFile}
	if base != "" {
		argv = append(argv, "--base", base)
	}
	if draft {
		argv = append(argv, "--draft")
	}
	out, err := g.run(ctx, argv...)
	if err != nil {
		return "", err
	}
	lines := strings.Split(out, "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

// ChangedFiles is the unique list of files a run recorded, relative to the workspace.
func ChangedFiles(rep *domain.RunReport) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range rep.Changes {
		f := filepath.ToSlash(c.File)
		if f == "" || seen[f] || strings.HasPrefix(f, ".evolvectl/") {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// Eligible explains why a run cannot be published, or returns "".
func Eligible(rep *domain.RunReport) string {
	switch {
	case rep == nil:
		return "no run"
	case rep.DryRun:
		return "dry-run left the workspace unchanged; nothing to commit"
	case rep.Outcome != domain.OutcomeSucceeded && rep.Outcome != domain.OutcomeSucceededWithReview:
		return "outcome " + rep.Outcome + " is not published; fix the run first"
	case len(ChangedFiles(rep)) == 0:
		return "the run recorded no changed files"
	}
	return ""
}

// Publish commits a finished run on a new branch, pushes it, and opens a pull request. The
// workspace's uncommitted run changes move to the new branch with git switch -c.
func Publish(ctx context.Context, g Git, rep *domain.RunReport, base, bodyFile, body string) *domain.SCMResult {
	res := &domain.SCMResult{Base: base, Draft: rep.Outcome == domain.OutcomeSucceededWithReview}
	if why := Eligible(rep); why != "" {
		res.Note = why
		return res
	}
	if res.Base == "" {
		if b, err := g.Branch(ctx); err == nil {
			res.Base = b
		}
	}
	branch, err := g.NewBranch(ctx, BranchName(rep.Target), "")
	if err != nil {
		res.Note = err.Error()
		return res
	}
	res.Branch = branch
	return finish(ctx, g, rep, res, base, bodyFile, body)
}

// CommitAndOpen commits a run on the branch that is already checked out, then pushes and opens a PR.
func CommitAndOpen(ctx context.Context, g Git, rep *domain.RunReport, branch, base, bodyFile, body string, push bool) *domain.SCMResult {
	res := &domain.SCMResult{Branch: branch, Base: base, Draft: rep.Outcome == domain.OutcomeSucceededWithReview}
	if why := Eligible(rep); why != "" {
		res.Note = why
		return res
	}
	if !push {
		sha, err := g.Commit(ctx, ChangedFiles(rep), Title(rep.Target), body)
		res.Commit = sha
		if err != nil {
			res.Note = err.Error()
		}
		return res
	}
	return finish(ctx, g, rep, res, base, bodyFile, body)
}

func finish(ctx context.Context, g Git, rep *domain.RunReport, res *domain.SCMResult, base, bodyFile, body string) *domain.SCMResult {
	sha, err := g.Commit(ctx, ChangedFiles(rep), Title(rep.Target), body)
	if err != nil {
		res.Note = err.Error()
		return res
	}
	res.Commit = sha
	if err := g.Push(ctx, res.Branch); err != nil {
		res.Note = "committed on " + res.Branch + "; push failed: " + err.Error()
		return res
	}
	res.Pushed = true
	if err := os.MkdirAll(filepath.Dir(bodyFile), 0o700); err == nil {
		_ = os.WriteFile(bodyFile, []byte(body), 0o644)
	}
	url, err := g.OpenPR(ctx, res.Branch, base, Title(rep.Target), bodyFile, res.Draft)
	if err != nil {
		res.Note = "pushed " + res.Branch + "; gh pr create failed: " + err.Error()
		return res
	}
	res.PRURL = url
	return res
}
