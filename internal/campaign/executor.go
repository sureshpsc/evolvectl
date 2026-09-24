// Package campaign runs the upgrade lifecycle and persists a run report.
package campaign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/commandadapt"
	"github.com/evolvectl/evolvectl/internal/config"
	"github.com/evolvectl/evolvectl/internal/diagnostics"
	"github.com/evolvectl/evolvectl/internal/discovery"
	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/fsio"
	"github.com/evolvectl/evolvectl/internal/idgen"
	"github.com/evolvectl/evolvectl/internal/manifest"
	"github.com/evolvectl/evolvectl/internal/patch"
	"github.com/evolvectl/evolvectl/internal/planner"
	"github.com/evolvectl/evolvectl/internal/policy"
	"github.com/evolvectl/evolvectl/internal/quality"
	"github.com/evolvectl/evolvectl/internal/recipe"
	"github.com/evolvectl/evolvectl/internal/repair"
	"github.com/evolvectl/evolvectl/internal/report"
	"github.com/evolvectl/evolvectl/internal/runner"
	"github.com/evolvectl/evolvectl/internal/skyparse"
	"github.com/evolvectl/evolvectl/internal/state"
	"github.com/evolvectl/evolvectl/internal/version"
)

// ErrInterrupted stops a run between stages and leaves it resumable.
var ErrInterrupted = errors.New("campaign interrupted")

// Request is one campaign invocation.
type Request struct {
	Workspace      string
	Dependency     string
	To             string
	DryRun         bool
	NoAI           bool
	Offline        bool
	AllowDirty     bool
	Provider       string
	ProviderSource string
	MaxIterations  int
	Confidence     string
	HaltAfter      string
	PlanOnly       bool
	ResumeID       string
	Config         config.File
	Recipes        []recipe.Recipe
}

// Executor runs campaigns.
type Executor struct {
	Store *state.Store
}

type runCtx struct {
	req      Request
	report   *domain.RunReport
	workRoot string
	seq      int64
	origRoot string
}

// Run executes or resumes a campaign.
func (e *Executor) Run(ctx context.Context, req Request) (*domain.RunReport, error) {
	if req.MaxIterations < 1 {
		req.MaxIterations = req.Config.Repair.MaxIterations
	}
	if req.MaxIterations < 1 {
		req.MaxIterations = 5
	}
	if req.Confidence == "" {
		req.Confidence = req.Config.Repair.ConfidenceThreshold
	}
	abs, err := filepath.Abs(req.Workspace)
	if err != nil {
		return nil, err
	}
	req.Workspace = abs
	rc := &runCtx{req: req, origRoot: abs, workRoot: abs}
	if req.ResumeID != "" {
		loaded, err := e.Store.LoadReport(req.ResumeID)
		if err != nil {
			return nil, err
		}
		if loaded.Outcome != domain.OutcomeCancelled && loaded.Outcome != domain.OutcomePartial && loaded.Outcome != domain.OutcomeBlocked {
			return nil, fmt.Errorf("run %s is %s and cannot be resumed", req.ResumeID, loaded.Outcome)
		}
		rc.report = loaded
		rc.report.ResumeOf = req.ResumeID
		rc.report.Outcome = ""
		rc.report.OutcomeReason = ""
	} else {
		now := time.Now().UTC()
		rc.report = &domain.RunReport{
			SchemaVersion:   version.ReportSchema,
			ID:              idgen.New("run_"),
			CreatedAt:       now,
			UpdatedAt:       now,
			DataAsOf:        now,
			DryRun:          req.DryRun,
			NoAI:            req.NoAI || !req.Config.AI.Enabled,
			Offline:         req.Offline,
			WorkspaceRoot:   ".",
			Provider:        req.Provider,
			ProviderSource:  req.ProviderSource,
			RedactionNotice: "token-shaped strings are redacted in reports; this is not a complete secret scan",
			Checksums:       map[string]string{},
		}
		rc.report.EnsureStages()
		rc.report.Target = planner.ParseTarget(req.Dependency, "")
		rc.report.Target.To = req.To
	}
	err = e.loop(ctx, rc)
	if err != nil && !errors.Is(err, ErrInterrupted) {
		if rc.report.Outcome == "" {
			rc.report.Outcome = domain.OutcomeFailed
			rc.report.OutcomeReason = err.Error()
		}
		_ = e.persist(rc)
		return rc.report, err
	}
	if errors.Is(err, ErrInterrupted) {
		rc.report.Outcome = domain.OutcomeCancelled
		rc.report.OutcomeReason = "stopped after " + req.HaltAfter
		rc.report.NextStep = "evolvectl upgrade --resume " + rc.report.ID
		_ = e.persist(rc)
		return rc.report, ErrInterrupted
	}
	return rc.report, nil
}

func (e *Executor) loop(ctx context.Context, rc *runCtx) error {
	stages := []struct {
		name string
		fn   func(context.Context, *runCtx) error
	}{
		{"preflight", e.preflight},
		{"discover", e.discover},
		{"assess", e.assess},
		{"plan", e.plan},
		{"prepare", e.prepare},
		{"apply", e.apply},
		{"diagnose", e.diagnose},
		{"repair", e.repair},
		{"validate", e.validate},
		{"finalize", e.finalize},
	}
	for _, st := range stages {
		if rc.report.StageState(st.name) == domain.StagePassed {
			continue
		}
		if ctx.Err() != nil {
			rc.report.MarkStage(st.name, domain.StageCancelled, "context cancelled", "", time.Now().UTC())
			rc.report.Outcome = domain.OutcomeCancelled
			rc.report.OutcomeReason = "cancelled"
			_ = e.persist(rc)
			return ErrInterrupted
		}
		rc.report.MarkStage(st.name, domain.StageRunning, "", "", time.Now().UTC())
		e.event(rc, st.name, "running", "stage started")
		if err := st.fn(ctx, rc); err != nil {
			state := domain.StageFailed
			if rc.report.Outcome == domain.OutcomeBlocked {
				state = domain.StageBlocked
			}
			rc.report.MarkStage(st.name, state, err.Error(), "", time.Now().UTC())
			e.event(rc, st.name, state, err.Error())
			return err
		}
		rc.report.MarkStage(st.name, domain.StagePassed, "", "", time.Now().UTC())
		e.event(rc, st.name, "passed", "stage passed")
		if err := e.persist(rc); err != nil {
			return err
		}
		if rc.req.PlanOnly && st.name == "plan" {
			for _, later := range []string{"prepare", "apply", "diagnose", "repair", "validate", "finalize"} {
				rc.report.MarkStage(later, domain.StageSkipped, "plan-only invocation does not write source", "user", time.Now().UTC())
			}
			rc.report.Outcome = domain.OutcomePartial
			rc.report.OutcomeReason = "plan-only; source was not modified"
			rc.report.NextStep = "evolvectl upgrade " + rc.report.Target.Raw + " --to " + rc.report.Target.To
			return e.persist(rc)
		}
		if rc.req.HaltAfter == st.name {
			return ErrInterrupted
		}
	}
	return nil
}

func (e *Executor) preflight(ctx context.Context, rc *runCtx) error {
	provider := rc.req.Provider
	if provider == "" || provider == "auto" {
		if _, err := os.Stat(filepath.Join(rc.origRoot, ".git")); err == nil {
			provider = "git"
			rc.report.ProviderSource = "auto"
		} else {
			provider = "filesystem"
			rc.report.ProviderSource = "auto"
		}
		rc.report.Provider = provider
	}
	rc.report.Capabilities = capabilityMatrix(provider)
	if provider == "copybara" {
		bin := rc.req.Config.Workspace.Copybara.Binary
		if bin == "" {
			bin = "copybara"
		}
		if _, ok := runner.Look(bin); !ok {
			rc.report.Outcome = domain.OutcomeBlocked
			rc.report.NextStep = "install Copybara or select another provider: evolvectl session export git"
			return fmt.Errorf("COPYBARA_NOT_FOUND: selected provider copybara is unavailable")
		}
		rc.report.ManualReview = append(rc.report.ManualReview, "copybara provider is selected; upgrade will not invoke copybara migrate")
	}
	gitPath, hasGit := runner.Look("git")
	if hasGit {
		if _, err := os.Stat(filepath.Join(rc.origRoot, ".git")); err == nil {
			rev := runner.Run(ctx, runner.Request{Argv: []string{gitPath, "rev-parse", "HEAD"}, Dir: rc.origRoot, Timeout: 15 * time.Second})
			rc.report.BaselineRevision = strings.TrimSpace(rev.Stdout)
			st := runner.Run(ctx, runner.Request{Argv: []string{gitPath, "status", "--porcelain"}, Dir: rc.origRoot, Timeout: 15 * time.Second})
			if strings.TrimSpace(st.Stdout) != "" {
				rc.report.PolicyDecisions = append(rc.report.PolicyDecisions, domain.PolicyDecision{
					ID: idgen.New("pol_"), Action: "worktree", Allowed: rc.req.AllowDirty || rc.req.DryRun,
					Reason: "git worktree has uncommitted changes",
				})
				if !rc.req.AllowDirty && !rc.req.DryRun {
					rc.report.Outcome = domain.OutcomeBlocked
					rc.report.NextStep = "commit or stash changes, or pass --allow-dirty"
					return fmt.Errorf("workspace is dirty")
				}
			}
		}
	}
	if path, ok := runner.Look("go"); ok {
		run := runner.Run(ctx, runner.Request{Argv: []string{path, "version"}, Dir: rc.origRoot, Timeout: 15 * time.Second})
		rc.report.Tools = append(rc.report.Tools, domain.ToolFingerprint{Name: "go", Available: run.ExitCode == 0, Version: strings.TrimSpace(run.Stdout)})
	} else {
		rc.report.Tools = append(rc.report.Tools, domain.ToolFingerprint{Name: "go", Available: false, Detail: "not on PATH"})
	}
	if path, ok := runner.Look("python"); ok {
		run := runner.Run(ctx, runner.Request{Argv: []string{path, "--version"}, Dir: rc.origRoot, Timeout: 15 * time.Second})
		rc.report.Tools = append(rc.report.Tools, domain.ToolFingerprint{Name: "python", Available: true, Version: strings.TrimSpace(run.Stdout + run.Stderr)})
	} else if path, ok := runner.Look("python3"); ok {
		run := runner.Run(ctx, runner.Request{Argv: []string{path, "--version"}, Dir: rc.origRoot, Timeout: 15 * time.Second})
		rc.report.Tools = append(rc.report.Tools, domain.ToolFingerprint{Name: "python", Available: true, Version: strings.TrimSpace(run.Stdout + run.Stderr)})
	} else {
		rc.report.Tools = append(rc.report.Tools, domain.ToolFingerprint{Name: "python", Available: false, Detail: "not on PATH"})
	}
	return nil
}

func (e *Executor) discover(ctx context.Context, rc *runCtx) error {
	inv, err := discovery.Scan(ctx, rc.origRoot, discovery.Options{Workers: rc.req.Config.Execution.Workers, Ignore: rc.req.Config.Repository.Ignore})
	if err != nil {
		return err
	}
	inv.BaselineRevision = rc.report.BaselineRevision
	rc.report.Inventory = inv
	rc.report.DataAsOf = inv.ScannedAt
	if len(inv.CopybaraConfigs) > 0 {
		view := &domain.CopybaraView{Configs: inv.CopybaraConfigs}
		for _, cfg := range inv.CopybaraConfigs {
			b, err := os.ReadFile(filepath.Join(rc.origRoot, filepath.FromSlash(cfg)))
			if err != nil {
				continue
			}
			parsed := skyparse.Parse(cfg, string(b))
			view.Workflows = append(view.Workflows, parsed.Workflows...)
		}
		bin := rc.req.Config.Workspace.Copybara.Binary
		if bin == "" {
			bin = "copybara"
		}
		_, ok := runner.Look(bin)
		view.Tool = domain.ToolFingerprint{Name: bin, Available: ok}
		if !ok {
			view.Tool.Detail = "not on PATH; static explanation only"
		}
		rc.report.Copybara = view
	}
	return nil
}

func (e *Executor) assess(_ context.Context, rc *runCtx) error {
	for i := range rc.report.Inventory.Dependencies {
		d := &rc.report.Inventory.Dependencies[i]
		if d.Status == "" {
			d.Status = domain.StatusRegistryUnavailable
			d.StatusReason = "registry was not queried"
		}
	}
	return nil
}

func (e *Executor) plan(_ context.Context, rc *runCtx) error {
	target := rc.report.Target
	if target.Ecosystem == "" {
		target.Ecosystem = inferEcosystem(rc.report.Inventory, target.Name)
		rc.report.Target.Ecosystem = target.Ecosystem
	}
	p, err := planner.Build(planner.Request{
		Root:         rc.origRoot,
		Inventory:    rc.report.Inventory,
		Target:       target,
		Recipes:      rc.req.Recipes,
		CopybaraSeen: len(rc.report.Inventory.CopybaraConfigs) > 0,
	})
	if err != nil {
		return err
	}
	rc.report.Plan = p
	rc.report.PlanID = p.ID
	rc.report.PlanHash = p.Hash
	rc.report.Checkpoint.PlanHash = p.Hash
	rc.report.Checkpoint.FileHashes = map[string]string{}
	for _, f := range append(append([]string{}, p.Files...), p.Manifests...) {
		sum, err := fsio.HashFile(filepath.Join(rc.origRoot, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		rc.report.Checkpoint.FileHashes[f] = sum
	}
	rc.report.Checkpoint.Stage = "plan"
	return nil
}

func (e *Executor) prepare(ctx context.Context, rc *runCtx) error {
	if rc.req.DryRun {
		dir, err := os.MkdirTemp("", "evolvectl-dry-*")
		if err != nil {
			return err
		}
		if err := copyTree(rc.origRoot, dir); err != nil {
			return err
		}
		rc.workRoot = dir
		rc.report.PolicyDecisions = append(rc.report.PolicyDecisions, domain.PolicyDecision{
			ID: idgen.New("pol_"), Action: "dry-run", Allowed: true, Reason: "changes are applied only in a temporary copy",
		})
		if msg, ok := externalReplaceBlock(rc.origRoot, rc.report.Inventory); !ok {
			rc.report.Outcome = domain.OutcomeBlocked
			rc.report.NextStep = "run upgrade without --dry-run; the replace target is outside this workspace"
			rc.report.ManualReview = appendUnique(rc.report.ManualReview, msg)
			return fmt.Errorf("%s", msg)
		}
	}
	e.captureBaseline(ctx, rc)
	return nil
}

func (e *Executor) apply(ctx context.Context, rc *runCtx) error {
	if err := checkStale(rc); err != nil {
		return err
	}
	return e.mutate(ctx, rc)
}

func checkStale(rc *runCtx) error {
	if rc.report.Plan == nil {
		return fmt.Errorf("missing plan")
	}
	if !rc.req.DryRun && rc.report.Checkpoint.PlanHash != "" && rc.report.Plan.Hash != rc.report.Checkpoint.PlanHash {
		rc.report.Outcome = domain.OutcomeBlocked
		return fmt.Errorf("stale plan hash")
	}
	for rel, want := range rc.report.Checkpoint.FileHashes {
		got, err := fsio.HashFile(filepath.Join(rc.workRoot, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		if got != want {
			rc.report.Outcome = domain.OutcomeBlocked
			rc.report.NextStep = "replan; files changed since the plan checkpoint"
			return fmt.Errorf("stale plan: %s changed", rel)
		}
	}
	return nil
}

func (e *Executor) mutate(ctx context.Context, rc *runCtx) error {
	target := rc.report.Target
	if err := e.rejectMissingSums(rc); err != nil {
		return err
	}
	if !rc.req.DryRun {
		e.snapshotManifests(rc)
	}
	changes, err := manifest.Bump(rc.workRoot, rc.report.Inventory, target.Ecosystem, target.Name, target.To)
	if err != nil {
		rc.report.Outcome = domain.OutcomeFailed
		return err
	}
	if err := e.syncGoSums(ctx, rc, &changes); err != nil {
		return err
	}
	vc := recipe.VersionContext{Ecosystem: target.Ecosystem, Name: target.Name, Target: normalizeTarget(target.Ecosystem, target.To)}
	if len(rc.report.Plan.CurrentVersions) > 0 {
		vc.Current = rc.report.Plan.CurrentVersions[0]
	}
	opt := policy.Options{AllowGenerated: rc.req.Config.Repair.AllowGenerated}
	for _, rel := range rc.report.Plan.Files {
		if !(strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, ".py")) {
			continue
		}
		abs := filepath.Join(rc.workRoot, filepath.FromSlash(rel))
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		dec := policy.AllowPath(rc.workRoot, rel, body, opt)
		rc.report.PolicyDecisions = append(rc.report.PolicyDecisions, domain.PolicyDecision{
			ID: idgen.New("pol_"), Action: "edit", Path: rel, Allowed: dec.Allowed, Reason: dec.Reason,
		})
		if !dec.Allowed {
			rc.report.ManualReview = append(rc.report.ManualReview, rel+": "+dec.Reason)
			continue
		}
		out, chs, err := recipe.ApplySource(rel, body, filterRecipes(rc.req.Recipes, rc.req.Confidence), vc)
		if err != nil {
			return err
		}
		if string(out) == string(body) {
			continue
		}
		if !rc.req.DryRun {
			if err := snapshot(rc.origRoot, rc.report.ID, rel, body); err != nil {
				return err
			}
		}
		if err := fsio.WriteAtomic(abs, out, 0o644); err != nil {
			return err
		}
		for i := range chs {
			chs[i].BeforeHash = fsio.HashBytes(body)
			chs[i].AfterHash = fsio.HashBytes(out)
			chs[i].File = rel
		}
		changes = append(changes, chs...)
		if len(changes) > rc.req.Config.Repair.MaxTotalFiles && rc.req.Config.Repair.MaxTotalFiles > 0 {
			rc.report.Outcome = domain.OutcomePartial
			rc.report.ManualReview = append(rc.report.ManualReview, "stopped: max total files")
			break
		}
	}
	rc.report.Changes = append(rc.report.Changes, changes...)
	if target.Ecosystem == "maven" || target.Ecosystem == "node" {
		rc.report.ManualReview = appendUnique(rc.report.ManualReview, "semantic repair is unavailable for "+target.Ecosystem)
	}
	return nil
}

func (e *Executor) diagnose(ctx context.Context, rc *runCtx) error {
	return e.runValidations(ctx, rc, false)
}

func (e *Executor) repair(ctx context.Context, rc *runCtx) error {
	if len(rc.report.Diagnostics) == 0 {
		return nil
	}
	_, err := repair.Loop(rc.req.MaxIterations, func(iteration int) (repair.Step, error) {
		before := len(rc.report.Changes)
		if err := e.mutate(ctx, rc); err != nil {
			return repair.Step{}, err
		}
		applied := len(rc.report.Changes) - before
		if err := e.runValidations(ctx, rc, false); err != nil {
			return repair.Step{}, err
		}
		var fps []string
		for _, g := range rc.report.Groups {
			fps = append(fps, g.Fingerprint)
		}
		sort.Strings(fps)
		rc.report.Iterations = append(rc.report.Iterations, domain.Iteration{
			Number: iteration, Diagnostics: len(rc.report.Diagnostics), Applied: applied, Fingerprints: fps, NoProgress: applied == 0,
		})
		return repair.Step{Applied: applied, Fingerprints: fps, Diagnostics: len(rc.report.Diagnostics)}, nil
	})
	if err != nil {
		return err
	}
	if len(rc.report.Diagnostics) > 0 {
		rc.report.Unresolved = nil
		for _, d := range rc.report.Diagnostics {
			rc.report.Unresolved = append(rc.report.Unresolved, d.ID+": "+d.Message)
		}
		rc.report.ManualReview = appendUnique(rc.report.ManualReview, "unresolved diagnostics remain")
	}
	return nil
}

func (e *Executor) validate(ctx context.Context, rc *runCtx) error {
	if len(rc.report.Validations) > 0 && len(rc.report.Diagnostics) == 0 && !needsRerun(rc) {
		return nil
	}
	return e.runValidations(ctx, rc, true)
}

func (e *Executor) finalize(_ context.Context, rc *runCtx) error {
	var diffs []string
	for _, c := range rc.report.Changes {
		if c.Diff != "" {
			diffs = append(diffs, c.Diff)
		}
	}
	patchBytes := patch.Join(diffs)
	sum := sha256.Sum256(patchBytes)
	rc.report.Checksums["patch"] = hex.EncodeToString(sum[:])
	if rc.req.DryRun {
		rel, err := e.Store.SavePatch(rc.report.ID, patchBytes)
		if err == nil {
			rc.report.Artifacts.Patch = rel
		}
		if err := ensureUntouched(rc.origRoot, rc.report.Checkpoint.FileHashes); err != nil {
			rc.report.Outcome = domain.OutcomeFailed
			return err
		}
		_ = os.RemoveAll(rc.workRoot)
	} else if e.Store != nil {
		rel, err := e.Store.SavePatch(rc.report.ID, patchBytes)
		if err != nil {
			return err
		}
		rc.report.Artifacts.Patch = rel
	}
	decideOutcome(rc.report)
	annotateQuality(rc.report)
	return nil
}

func (e *Executor) runValidations(ctx context.Context, rc *runCtx, final bool) error {
	_ = final
	rc.report.Quality = quality.ResetAfter(rc.report.Quality)
	rc.report.Validations = nil
	rc.report.Diagnostics = nil
	target := rc.report.Target
	mans := []string{}
	if rc.report.Plan != nil {
		mans = rc.report.Plan.Manifests
	}
	manifestOK := true
	for _, m := range mans {
		if !manifest.ContainsVersion(rc.workRoot, m, target.To) {
			manifestOK = false
		}
	}
	status := domain.GatePass
	reason := "declared version matches the requested target"
	if !manifestOK {
		status = domain.GateFail
		reason = "target version not found in manifest"
	}
	if len(mans) == 0 {
		status = domain.GateFail
		reason = "no manifest recorded"
	}
	rc.report.Validations = append(rc.report.Validations, gate("manifest-target", strings.Join(mans, ","), status, true, nil, 0, reason))
	switch target.Ecosystem {
	case "go":
		e.goTest(ctx, rc)
	case "python":
		e.pythonSyntax(ctx, rc)
	}
	if bazelDetected(rc.report.Inventory) {
		rc.report.Validations = append(rc.report.Validations, gate("bazel test", ".", domain.GateSkipped, false, nil, 0, "bazel is not executed by this campaign; gate is optional"))
		rc.report.ManualReview = appendUnique(rc.report.ManualReview, "bazel files were detected and bazel test was not run")
	}
	adapters, _ := commandadapt.LoadDir(filepath.Join(rc.origRoot, ".evolvectl", "adapters"))
	for _, a := range adapters {
		cmd, ok := a.Spec.Commands["test"]
		if !ok {
			cmd, ok = a.Spec.Commands["build"]
		}
		if !ok {
			continue
		}
		argv, err := commandadapt.Render(cmd.Argv, map[string]string{
			"root": rc.workRoot, "project.root": rc.workRoot, "dependency": target.Name, "version": target.To,
		})
		if err != nil {
			rc.report.Validations = append(rc.report.Validations, gate(a.Metadata.Name, a.Path, domain.GateBlocked, cmd.Required, cmd.Argv, 0, err.Error()))
			continue
		}
		run := runner.Run(ctx, runner.Request{Argv: argv, Dir: rc.workRoot, Timeout: commandadapt.Timeout(cmd.Timeout)})
		st := domain.GatePass
		if run.ExitCode != 0 {
			st = domain.GateFail
		}
		if run.Err != "" && run.ExitCode == 127 {
			st = domain.GateUnavailable
		}
		g := gate(a.Metadata.Name+" "+strings.Join(argv, " "), ".", st, cmd.Required, argv, run.ExitCode, run.Err)
		g.Duration = run.Duration
		g.StartedAt = run.StartedAt
		g.EndedAt = run.EndedAt
		g.LogExcerpt = trimLog(run.Stdout + run.Stderr)
		rc.report.Validations = append(rc.report.Validations, g)
		rc.report.Diagnostics = append(rc.report.Diagnostics, diagnostics.ParseToolOutput(a.Metadata.Name, run.Stdout+"\n"+run.Stderr)...)
		rc.report.Capabilities = append(rc.report.Capabilities, domain.Capability{
			Adapter: a.Metadata.Name, Kind: "command", Operation: "validate.run", Support: domain.SupportDelegated, Depth: "command-only", Confidence: domain.ConfidenceMedium,
			Notes: "delegated command output does not prove semantic repair",
		})
		recordAdapterResult(rc, "after", a.Metadata.Name, st)
	}
	rc.report.Groups = diagnostics.Group(rc.report.Diagnostics)
	return nil
}

func (e *Executor) goTest(ctx context.Context, rc *runCtx) {
	e.runGoSuite(ctx, rc, "after", true)
}

func (e *Executor) pythonSyntax(ctx context.Context, rc *runCtx) {
	var files []string
	if rc.report.Plan != nil {
		for _, f := range rc.report.Plan.Files {
			if strings.HasSuffix(f, ".py") {
				files = append(files, f)
			}
		}
	}
	bad := []string{}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(rc.workRoot, filepath.FromSlash(f)))
		if err != nil {
			bad = append(bad, f+": "+err.Error())
			continue
		}
		if err := balancePython(string(b)); err != nil {
			bad = append(bad, f+": "+err.Error())
		}
	}
	st := domain.GatePass
	reason := "structural delimiter balance passed; this is not a full Python parser"
	if len(bad) > 0 {
		st = domain.GateFail
		reason = strings.Join(bad, "; ")
	}
	if len(files) == 0 {
		st = domain.GateSkipped
		reason = "no Python files in the plan"
	}
	rc.report.Validations = append(rc.report.Validations, gate("python syntax", strings.Join(files, ","), st, true, nil, 0, reason))
	py := ""
	if p, ok := runner.Look("python"); ok {
		py = p
	} else if p, ok := runner.Look("python3"); ok {
		py = p
	}
	if py == "" {
		rc.report.Validations = append(rc.report.Validations, gate("py_compile", ".", domain.GateUnavailable, false, []string{"python", "-m", "py_compile"}, 127, "python is not on PATH"))
		e.runPythonTests(ctx, rc, "after", true)
		return
	}
	for _, f := range files {
		argv := []string{py, "-m", "py_compile", filepath.Join(rc.workRoot, filepath.FromSlash(f))}
		run := runner.Run(ctx, runner.Request{Argv: argv, Dir: rc.workRoot, Timeout: time.Minute})
		st := domain.GatePass
		if run.ExitCode != 0 {
			st = domain.GateFail
		}
		g := gate("py_compile", f, st, false, argv, run.ExitCode, trimLog(run.Stderr))
		g.Duration = run.Duration
		rc.report.Validations = append(rc.report.Validations, g)
	}
	e.runPythonTests(ctx, rc, "after", true)
}

func (e *Executor) event(rc *runCtx, stage, state, msg string) {
	rc.seq++
	ev := domain.RunEvent{
		SchemaVersion: version.ReportSchema,
		RunID:         rc.report.ID,
		Sequence:      rc.seq,
		Timestamp:     time.Now().UTC(),
		Stage:         stage,
		Attempt:       1,
		Operation:     stage,
		State:         state,
		Actor:         "engine",
		Message:       msg,
		Redaction:     "applied",
	}
	if e.Store != nil {
		_ = e.Store.AppendEvent(ev)
	}
}

func (e *Executor) persist(rc *runCtx) error {
	rc.report.UpdatedAt = time.Now().UTC()
	if e.Store == nil {
		return nil
	}
	rel := ".evolvectl/runs/" + rc.report.ID + "/report.html"
	rc.report.Artifacts.HTML = rel
	if err := e.Store.SaveReport(rc.report); err != nil {
		return err
	}
	body, err := report.HTML(rc.report)
	if err != nil {
		return nil
	}
	_ = fsio.WriteAtomic(filepath.Join(rc.origRoot, filepath.FromSlash(rel)), body, 0o644)
	return nil
}

// Rollback restores snapshotted files when the current bytes still match the campaign.
func Rollback(workspace, runID string) error {
	st := state.Open(workspace)
	rep, err := st.LoadReport(runID)
	if err != nil {
		return err
	}
	snap := filepath.Join(workspace, ".evolvectl", "snapshots", runID)
	for _, ch := range rep.Changes {
		cur := filepath.Join(workspace, filepath.FromSlash(ch.File))
		src := filepath.Join(snap, filepath.FromSlash(ch.File))
		body, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) && ch.BeforeHash == "" {
				now, rerr := os.ReadFile(cur)
				if os.IsNotExist(rerr) {
					continue
				}
				if rerr != nil {
					return rerr
				}
				if fsio.HashBytes(now) != ch.AfterHash {
					return fmt.Errorf("refusing to roll back %s: file changed after the campaign", ch.File)
				}
				if err := os.Remove(cur); err != nil {
					return err
				}
				continue
			}
			continue
		}
		now, err := os.ReadFile(cur)
		if err != nil {
			return err
		}
		if fsio.HashBytes(now) != ch.AfterHash {
			return fmt.Errorf("refusing to roll back %s: file changed after the campaign", ch.File)
		}
		if err := fsio.WriteAtomic(cur, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) snapshotManifests(rc *runCtx) {
	if rc.report.Plan == nil {
		return
	}
	for _, rel := range rc.report.Plan.Manifests {
		abs := filepath.Join(rc.origRoot, filepath.FromSlash(rel))
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		_ = snapshot(rc.origRoot, rc.report.ID, rel, body)
		sumRel := fsio.RelSlash(rc.origRoot, filepath.Join(filepath.Dir(abs), "go.sum"))
		if sum, err := os.ReadFile(filepath.Join(rc.origRoot, filepath.FromSlash(sumRel))); err == nil {
			_ = snapshot(rc.origRoot, rc.report.ID, sumRel, sum)
		}
	}
}

func decideOutcome(r *domain.RunReport) {
	if r.Outcome == domain.OutcomeBlocked || r.Outcome == domain.OutcomeCancelled {
		return
	}
	requiredFail := false
	requiredGap := false
	for _, v := range r.Validations {
		if !v.Required {
			continue
		}
		switch v.Status {
		case domain.GateFail:
			requiredFail = true
		case domain.GatePass:
		default:
			requiredGap = true
		}
	}
	switch {
	case requiredFail:
		r.Outcome = domain.OutcomeFailed
		r.OutcomeReason = "a required validation failed"
		r.NextStep = "inspect evolvectl validate --run " + r.ID
	case requiredGap:
		r.Outcome = domain.OutcomePartial
		r.OutcomeReason = "a required validation did not run"
		r.NextStep = "install the missing tool or narrow the campaign"
	case len(r.ManualReview) > 0 || len(r.Unresolved) > 0:
		r.Outcome = domain.OutcomeSucceededWithReview
		r.OutcomeReason = "changes applied; review items remain"
		r.NextStep = "evolvectl diff --run " + r.ID
	default:
		r.Outcome = domain.OutcomeSucceeded
		r.OutcomeReason = "target applied and required validations passed"
		r.NextStep = "evolvectl report --run " + r.ID
	}
}

func gate(name, scope, status string, required bool, cmd []string, exit int, reason string) domain.ValidationResult {
	now := time.Now().UTC()
	return domain.ValidationResult{
		ID: idgen.New("val_"), Validator: name, Scope: scope, Status: status, Required: required,
		Command: cmd, ExitCode: exit, Reason: reason, StartedAt: now, EndedAt: now,
	}
}

func capabilityMatrix(provider string) []domain.Capability {
	return []domain.Capability{
		{Adapter: "filesystem", Kind: "workspace", Operation: "discover", Support: domain.SupportNative, Depth: "native", Confidence: domain.ConfidenceHigh},
		{Adapter: "git", Kind: "workspace", Operation: "scm.status", Support: domain.SupportDelegated, Depth: "command-only", Tool: "git", Confidence: domain.ConfidenceHigh},
		{Adapter: "go", Kind: "language", Operation: "repair", Support: domain.SupportNative, Depth: "semantic", Confidence: domain.ConfidenceHigh, Notes: "import-aware AST recipes"},
		{Adapter: "python", Kind: "language", Operation: "repair", Support: domain.SupportNative, Depth: "structural", Confidence: domain.ConfidenceMedium, Notes: "structural recipes; not a type checker"},
		{Adapter: "maven", Kind: "language", Operation: "repair", Support: domain.SupportUnavailable, Depth: "unavailable", Confidence: domain.ConfidenceLow, Notes: "manifest edit only"},
		{Adapter: "node", Kind: "language", Operation: "repair", Support: domain.SupportUnavailable, Depth: "unavailable", Confidence: domain.ConfidenceLow, Notes: "manifest edit only"},
		{Adapter: provider, Kind: "workspace", Operation: "selected", Support: domain.SupportNative, Depth: "native", Confidence: domain.ConfidenceHigh},
		{Adapter: "ai", Kind: "reasoning", Operation: "propose", Support: domain.SupportUnavailable, Depth: "unavailable", Confidence: domain.ConfidenceLow, Notes: "noop unless explicitly enabled; proposals are not auto-applied"},
	}
}

func inferEcosystem(inv domain.Inventory, name string) string {
	for _, d := range inv.Dependencies {
		if d.Name == name && d.Direct {
			return d.Ecosystem
		}
	}
	for _, d := range inv.Dependencies {
		if d.Name == name {
			return d.Ecosystem
		}
	}
	return ""
}

func filterRecipes(in []recipe.Recipe, threshold string) []recipe.Recipe {
	var out []recipe.Recipe
	for _, r := range in {
		if recipe.MeetsThreshold(recipe.Confidence(r), threshold) {
			out = append(out, r)
		}
	}
	return out
}

func normalizeTarget(eco, version string) string {
	if eco == "go" && version != "" && !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func moduleDirs(rc *runCtx) []string {
	seen := map[string]bool{}
	var out []string
	if rc.report.Plan == nil {
		return []string{rc.workRoot}
	}
	for _, root := range rc.report.Plan.Projects {
		abs := rc.workRoot
		if root != "." && root != "" {
			abs = filepath.Join(rc.workRoot, filepath.FromSlash(root))
		}
		if seen[abs] {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err != nil {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	if len(out) == 0 {
		out = append(out, rc.workRoot)
	}
	return out
}

func bazelDetected(inv domain.Inventory) bool {
	for _, b := range inv.BuildSystems {
		if b == "bazel" {
			return true
		}
	}
	return false
}

func needsRerun(rc *runCtx) bool {
	for _, it := range rc.report.Iterations {
		if it.Applied > 0 {
			return true
		}
	}
	return false
}

func trimLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}

func appendUnique(in []string, v string) []string {
	for _, s := range in {
		if s == v {
			return in
		}
	}
	return append(in, v)
}

func snapshot(root, id, rel string, body []byte) error {
	dest := filepath.Join(root, ".evolvectl", "snapshots", id, filepath.FromSlash(rel))
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	return fsio.WriteAtomic(dest, body, 0o644)
}

func ensureUntouched(root string, hashes map[string]string) error {
	for rel, want := range hashes {
		got, err := fsio.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("dry-run modified %s", rel)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := d.Name()
		if base == ".git" || base == ".evolvectl" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}

func balancePython(src string) error {
	var stack []byte
	inStr := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr != 0 {
			if c == '\\' && i+1 < len(src) {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '#' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '"' || c == '\'' {
			inStr = c
			continue
		}
		switch c {
		case '(', '[', '{':
			stack = append(stack, c)
		case ')', ']', '}':
			if len(stack) == 0 {
				return fmt.Errorf("unbalanced %c", c)
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if (c == ')' && open != '(') || (c == ']' && open != '[') || (c == '}' && open != '{') {
				return fmt.Errorf("mismatched %c", c)
			}
		}
	}
	if len(stack) != 0 {
		return fmt.Errorf("unclosed delimiter")
	}
	return nil
}

// LookBin is a small wrapper so tests can stub later.
func LookBin(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}
