package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sureshpsc/evolvectl/internal/batch"
	"github.com/sureshpsc/evolvectl/internal/campaign"
	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/exitcode"
	"github.com/sureshpsc/evolvectl/internal/fsio"
	"github.com/sureshpsc/evolvectl/internal/idgen"
	"github.com/sureshpsc/evolvectl/internal/planner"
	"github.com/sureshpsc/evolvectl/internal/report"
	"github.com/sureshpsc/evolvectl/internal/scm"
	"github.com/sureshpsc/evolvectl/internal/version"
)

// Batch modes.
const (
	BatchDryRun   = "dry-run"
	BatchApply    = "apply"
	BatchBranches = "branches"
)

// Batch runs every row of an upgrade sheet and writes one combined report.
//
// dry-run applies each row in its own temporary copy, so rows are independent and the
// workspace is untouched. apply edits the workspace row after row. branches starts each row
// on a new branch from the same commit, commits a finished row there, and returns to the
// starting branch; with openPR it also pushes and opens a pull request per row.
func (a *App) Batch(ctx context.Context, opt Option, file, mode string, openPR bool) (int, error) {
	if mode == "" {
		mode = BatchDryRun
	}
	if mode != BatchDryRun && mode != BatchApply && mode != BatchBranches {
		return exitcode.Invalid, fmt.Errorf("unknown batch mode %s (dry-run, apply, or branches)", mode)
	}
	if openPR && mode != BatchBranches {
		return exitcode.Invalid, fmt.Errorf("--open-pr needs --mode branches")
	}
	base, err := filepath.Abs(a.dir(opt))
	if err != nil {
		return exitcode.Invalid, err
	}
	path := file
	if _, err := os.Stat(path); err != nil && !filepath.IsAbs(path) {
		path = filepath.Join(base, file)
	}
	rows, err := batch.Load(path)
	if err != nil {
		return exitcode.Invalid, err
	}
	br := &domain.BatchReport{
		SchemaVersion: version.ReportSchema, ID: idgen.New("batch_"), CreatedAt: time.Now().UTC(),
		Source: filepath.ToSlash(file), Mode: mode, WorkspaceRoot: ".",
	}
	g := scm.Git{R: a.gitRunner(), Dir: base}
	var startBranch, startCommit string
	if mode == BatchBranches {
		dirty, err := g.Dirty(ctx)
		if err != nil {
			return exitcode.Invalid, fmt.Errorf("branches mode needs a git worktree: %w", err)
		}
		if len(dirty) > 0 {
			return exitcode.DirtyWorkspace, fmt.Errorf("branches mode needs a clean worktree; %d uncommitted path(s), first: %s", len(dirty), dirty[0])
		}
		if startBranch, err = g.Branch(ctx); err != nil {
			return exitcode.Invalid, err
		}
		if startCommit, err = g.Head(ctx); err != nil {
			return exitcode.Invalid, err
		}
	}
	out := a.Out
	for i, row := range rows {
		br.Rows = append(br.Rows, a.batchRow(ctx, opt, base, i, row, mode, openPR, g, startBranch, startCommit))
		r := br.Rows[len(br.Rows)-1]
		fmt.Fprintf(out, "[%d/%d] %s %s -> %s\n", i+1, len(rows), r.Outcome, r.Dependency, r.To)
		if mode == BatchBranches && r.Reason == "stopped: could not return to "+startBranch {
			break
		}
	}
	for _, r := range br.Rows {
		switch r.Outcome {
		case domain.OutcomeSucceeded:
			br.Succeeded++
		case domain.OutcomeSucceededWithReview, domain.OutcomePartial:
			br.NeedsReview++
		default:
			br.Failed++
		}
	}
	dir := filepath.Join(base, ".evolvectl", "batches", br.ID)
	body, _ := json.MarshalIndent(br, "", "  ")
	_ = fsio.WriteAtomic(filepath.Join(dir, "batch.json"), append(body, '\n'), 0o644)
	_ = fsio.WriteAtomic(filepath.Join(dir, "batch.md"), []byte(report.BatchMarkdown(br)), 0o644)
	if page, err := report.BatchHTML(br); err == nil {
		_ = fsio.WriteAtomic(filepath.Join(dir, "batch.html"), page, 0o644)
	}
	switch opt.Format {
	case "json":
		fmt.Fprintf(out, "%s\n", body)
	case "md", "markdown":
		fmt.Fprint(out, report.BatchMarkdown(br))
	case "html":
		page, _ := report.BatchHTML(br)
		fmt.Fprintf(out, "%s", page)
	default:
		fmt.Fprint(out, report.BatchText(br))
	}
	if opt.Format != "html" {
		fmt.Fprintf(out, "Batch report         %s\n", filepath.ToSlash(filepath.Join(".evolvectl", "batches", br.ID, "batch.html")))
	}
	switch {
	case br.Failed > 0:
		return exitcode.Upgrade, fmt.Errorf("%d of %d upgrades failed", br.Failed, len(br.Rows))
	case br.NeedsReview > 0:
		return exitcode.NeedsReview, nil
	}
	return exitcode.Success, nil
}

func (a *App) batchRow(ctx context.Context, opt Option, base string, i int, row batch.Row, mode string, openPR bool, g scm.Git, startBranch, startCommit string) domain.BatchRow {
	res := domain.BatchRow{Index: i + 1, Dependency: row.Dependency, To: row.To, ToModule: row.ToModule, Workspace: row.Workspace, Owner: row.Owner}
	ws := base
	if row.Workspace != "" {
		ws = filepath.Join(base, filepath.FromSlash(row.Workspace))
	}
	ropt := opt
	ropt.Workspace = ws
	ropt.ToModule = row.ToModule
	ropt.VendorDir = ""
	ropt.OpenPR = false
	ropt.Format = "text"
	if mode == BatchApply && i > 0 {
		ropt.AllowDirty = true
	}
	branch := ""
	if mode == BatchBranches {
		ropt.AllowDirty = true
		t := planner.ParseTarget(row.Dependency, "")
		t.To, t.ToModule = row.To, row.ToModule
		b, err := g.NewBranch(ctx, scm.BranchName(t), startCommit)
		if err != nil {
			res.Outcome = domain.OutcomeBlocked
			res.Reason = err.Error()
			return res
		}
		branch = b
	}
	saved := a.Out
	a.Out = io.Discard
	rep, code, err := a.campaign(ctx, ropt, row.Dependency, row.To, mode == BatchDryRun, "")
	a.Out = saved
	res.ExitCode = code
	if rep != nil {
		res.RunID = rep.ID
		res.Outcome = rep.Outcome
		res.Reason = rep.OutcomeReason
		res.Changes = len(scm.ChangedFiles(rep))
		res.Report = filepath.ToSlash(filepath.Join(row.Workspace, rep.Artifacts.HTML))
		if q := rep.Quality; q != nil {
			res.NewlyFailed = len(q.NewlyFailed)
			res.CoverageBefore = report.Coverage(q.Before)
			if q.AfterRecorded {
				res.CoverageAfter = report.Coverage(q.After)
			}
		}
		if rep.Impact != nil {
			res.ImpactSites = len(rep.Impact.Sites)
		}
	}
	if res.Outcome == "" {
		res.Outcome = domain.OutcomeFailed
	}
	if err != nil && res.Reason == "" {
		res.Reason = err.Error()
	}
	if mode != BatchBranches {
		return res
	}
	res.Branch = branch
	committed := false
	if rep != nil && scm.Eligible(rep) == "" {
		bodyFile := filepath.Join(ws, ".evolvectl", "runs", rep.ID, "pr.md")
		rep.SCM = scm.CommitAndOpen(ctx, scm.Git{R: g.R, Dir: ws}, rep, branch, opt.PRBase, bodyFile, report.PRBody(rep), openPR)
		a.saveRun(ws, rep)
		committed = rep.SCM.Commit != ""
		res.PRURL = rep.SCM.PRURL
		if rep.SCM.Note != "" {
			res.Reason = joinReason(res.Reason, rep.SCM.Note)
		}
	} else if rep != nil && !rep.DryRun && len(rep.Changes) > 0 {
		if err := campaign.Rollback(ws, rep.ID); err != nil {
			res.Reason = joinReason(res.Reason, "rollback failed: "+err.Error())
		}
	}
	if err := g.Switch(ctx, startBranch); err != nil {
		res.Reason = "stopped: could not return to " + startBranch
		return res
	}
	if !committed {
		_, _ = g.R.Run(ctx, base, "git", "branch", "-d", branch)
		res.Branch = ""
	}
	return res
}

func joinReason(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
