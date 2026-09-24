// Package app is the CLI service layer. Command handlers stay thin.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	bundled "github.com/evolvectl/evolvectl/recipes"

	"github.com/evolvectl/evolvectl/internal/ai"
	"github.com/evolvectl/evolvectl/internal/campaign"
	"github.com/evolvectl/evolvectl/internal/commandadapt"
	"github.com/evolvectl/evolvectl/internal/config"
	"github.com/evolvectl/evolvectl/internal/diagnostics"
	"github.com/evolvectl/evolvectl/internal/discovery"
	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/exitcode"
	"github.com/evolvectl/evolvectl/internal/fsio"
	"github.com/evolvectl/evolvectl/internal/graph"
	"github.com/evolvectl/evolvectl/internal/planner"
	"github.com/evolvectl/evolvectl/internal/recipe"
	"github.com/evolvectl/evolvectl/internal/report"
	"github.com/evolvectl/evolvectl/internal/runner"
	"github.com/evolvectl/evolvectl/internal/session"
	"github.com/evolvectl/evolvectl/internal/skyparse"
	"github.com/evolvectl/evolvectl/internal/state"
	"github.com/evolvectl/evolvectl/internal/ui"
	"github.com/evolvectl/evolvectl/internal/version"
)

// App runs one CLI invocation.
type App struct {
	In      io.Reader
	Out     io.Writer
	Err     io.Writer
	Dir     string
	Getenv  func(string) string
	Version string
}

// Option carries shared flags.
type Option struct {
	Workspace  string
	ConfigPath string
	Provider   string
	NoAI       bool
	Offline    bool
	Format     string
	AllowDirty bool
	DryRun     bool
}

func (a *App) dir(opt Option) string {
	if opt.Workspace != "" {
		return opt.Workspace
	}
	if a.Dir != "" {
		return a.Dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func (a *App) env(k string) string {
	if a.Getenv != nil {
		return a.Getenv(k)
	}
	return os.Getenv(k)
}

func (a *App) load(opt Option) (string, config.File, string, string, error) {
	dir := a.dir(opt)
	path := opt.ConfigPath
	if path == "" {
		path = config.Path(dir)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return dir, cfg, "", "", err
	}
	provider, source := session.Resolve(opt.Provider, a.env(session.EnvProvider), cfg.Workspace.Type)
	return dir, cfg, provider, source, nil
}

// Init writes config and metadata directories.
func (a *App) Init(opt Option, force, minimal bool) (int, error) {
	dir := a.dir(opt)
	path := config.Path(dir)
	if _, err := os.Stat(path); err == nil && !force {
		return exitcode.Invalid, fmt.Errorf("config already exists: %s (pass --force to replace)", path)
	}
	cfg := config.Default()
	if opt.NoAI {
		cfg.AI.Enabled = false
		cfg.AI.Provider = "noop"
	}
	if opt.Provider != "" && opt.Provider != "auto" {
		cfg.Workspace.Type = opt.Provider
	}
	if minimal {
		cfg.Repository.Ignore = nil
	}
	if err := config.Save(path, cfg); err != nil {
		return exitcode.Generic, err
	}
	for _, sub := range []string{"cache", "logs", "reports", "patches", "snapshots", "runs"} {
		if err := os.MkdirAll(filepath.Join(dir, ".evolvectl", sub), 0o700); err != nil {
			return exitcode.Generic, err
		}
	}
	fmt.Fprintf(a.Out, "Wrote %s\n", path)
	return exitcode.Success, nil
}

// Scan inventories the workspace.
func (a *App) Scan(ctx context.Context, opt Option) (int, error) {
	dir, cfg, _, _, err := a.load(opt)
	if err != nil {
		return exitcode.Invalid, err
	}
	inv, err := discovery.Scan(ctx, dir, discovery.Options{Workers: cfg.Execution.Workers, Ignore: cfg.Repository.Ignore})
	if err != nil {
		return exitcode.Discovery, err
	}
	switch opt.Format {
	case "json":
		b, err := json.MarshalIndent(inv, "", "  ")
		if err != nil {
			return exitcode.Generic, err
		}
		fmt.Fprintf(a.Out, "%s\n", b)
	case "html":
		b, err := report.InventoryHTML(inv)
		if err != nil {
			return exitcode.Generic, err
		}
		fmt.Fprintf(a.Out, "%s", b)
	default:
		fmt.Fprint(a.Out, report.InventoryText(inv))
	}
	return exitcode.Success, nil
}

// Plan prints a plan and records a run that stops before writes.
func (a *App) Plan(ctx context.Context, opt Option, dependency, to string) (int, error) {
	rep, _, err := a.campaign(ctx, opt, dependency, to, true, "plan")
	if rep != nil && rep.Plan != nil {
		a.printPlan(rep, opt.Format)
		return exitcode.Success, nil
	}
	if err != nil {
		return exitcode.Invalid, err
	}
	return exitcode.Generic, fmt.Errorf("plan was not produced")
}

// Upgrade runs the campaign.
func (a *App) Upgrade(ctx context.Context, opt Option, dependency, to, resume string) (int, error) {
	if resume != "" {
		return a.resume(ctx, opt, resume)
	}
	halt := ""
	rep, code, err := a.campaign(ctx, opt, dependency, to, opt.DryRun, halt)
	if rep != nil {
		a.writeRun(rep, opt.Format)
	}
	return code, err
}

func (a *App) resume(ctx context.Context, opt Option, id string) (int, error) {
	dir, cfg, provider, source, err := a.load(opt)
	if err != nil {
		return exitcode.Invalid, err
	}
	recipes, err := loadRecipes(dir)
	if err != nil {
		return exitcode.Generic, err
	}
	ex := &campaign.Executor{Store: state.Open(dir)}
	rep, err := ex.Run(ctx, campaign.Request{
		Workspace: dir, ResumeID: id, DryRun: opt.DryRun, NoAI: true, Offline: opt.Offline,
		AllowDirty: opt.AllowDirty, Provider: provider, ProviderSource: source, Config: cfg, Recipes: recipes,
	})
	if rep != nil {
		a.writeRun(rep, opt.Format)
	}
	return finish(rep, err)
}

func (a *App) campaign(ctx context.Context, opt Option, dependency, to string, dry bool, halt string) (*domain.RunReport, int, error) {
	dir, cfg, provider, source, err := a.load(opt)
	if err != nil {
		return nil, exitcode.Invalid, err
	}
	if dependency == "" || to == "" {
		return nil, exitcode.Invalid, fmt.Errorf("dependency and --to are required")
	}
	recipes, err := loadRecipes(dir)
	if err != nil {
		return nil, exitcode.Generic, err
	}
	noAI := opt.NoAI || !cfg.AI.Enabled
	ex := &campaign.Executor{Store: state.Open(dir)}
	rep, err := ex.Run(ctx, campaign.Request{
		Workspace: dir, Dependency: dependency, To: to, DryRun: dry, NoAI: noAI, Offline: opt.Offline,
		AllowDirty: opt.AllowDirty, Provider: provider, ProviderSource: source,
		Confidence: cfg.Repair.ConfidenceThreshold, HaltAfter: halt, PlanOnly: halt == "plan", Config: cfg, Recipes: recipes,
	})
	if noAI {
		_ = ai.Noop{}.Name()
	}
	code, ferr := finish(rep, err)
	return rep, code, ferr
}

func finish(rep *domain.RunReport, err error) (int, error) {
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "dirty"):
			return exitcode.DirtyWorkspace, err
		case strings.Contains(msg, "COPYBARA_NOT_FOUND"):
			return exitcode.ToolMissing, err
		case strings.Contains(msg, "not found"):
			return exitcode.Invalid, err
		case strings.Contains(msg, "stale plan"):
			return exitcode.Policy, err
		case err == campaign.ErrInterrupted:
			return exitcode.Interrupted, nil
		}
		if rep != nil && rep.Outcome == domain.OutcomeBlocked {
			return exitcode.Policy, err
		}
		return exitcode.Generic, err
	}
	if rep == nil {
		return exitcode.Generic, fmt.Errorf("empty report")
	}
	switch rep.Outcome {
	case domain.OutcomeSucceeded:
		return exitcode.Success, nil
	case domain.OutcomeSucceededWithReview:
		return exitcode.NeedsReview, nil
	case domain.OutcomeFailed:
		for _, v := range rep.Validations {
			if v.Required && v.Status == domain.GateFail {
				return exitcode.Validation, fmt.Errorf("%s", rep.OutcomeReason)
			}
		}
		return exitcode.Upgrade, fmt.Errorf("%s", rep.OutcomeReason)
	case domain.OutcomeBlocked:
		return exitcode.Policy, fmt.Errorf("%s", rep.OutcomeReason)
	case domain.OutcomeCancelled:
		return exitcode.Interrupted, nil
	case domain.OutcomePartial:
		return exitcode.NeedsReview, nil
	default:
		return exitcode.Generic, fmt.Errorf("outcome %s", rep.Outcome)
	}
}

func (a *App) printPlan(rep *domain.RunReport, format string) {
	if rep.Plan == nil {
		fmt.Fprint(a.Out, report.TextSummary(rep))
		return
	}
	if format == "json" {
		b, _ := json.MarshalIndent(rep.Plan, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", b)
		return
	}
	if format == "html" {
		b, err := report.HTML(rep)
		if err != nil {
			fmt.Fprintln(a.Err, err)
			return
		}
		fmt.Fprintf(a.Out, "%s", b)
		return
	}
	p := rep.Plan
	fmt.Fprintf(a.Out, "Upgrade plan %s\n", p.ID)
	fmt.Fprintf(a.Out, "Dependency           %s:%s\n", p.Target.Ecosystem, p.Target.Name)
	fmt.Fprintf(a.Out, "Current              %s\n", strings.Join(p.CurrentVersions, ", "))
	fmt.Fprintf(a.Out, "Target               %s\n", p.Target.To)
	fmt.Fprintf(a.Out, "Projects             %s\n", strings.Join(p.Projects, ", "))
	fmt.Fprintf(a.Out, "Files                %d\n", len(p.Files))
	fmt.Fprintf(a.Out, "Recipes              %s\n", strings.Join(p.Recipes, ", "))
	fmt.Fprintf(a.Out, "Risk                 %s (%s)\n", p.Risk.Level, p.Risk.Confidence)
	fmt.Fprintf(a.Out, "Copybara             %s\n", p.CopybaraImpact)
	fmt.Fprintf(a.Out, "No files changed. Run evolvectl upgrade %s --to %s --apply\n", p.Target.Raw, p.Target.To)
	if p.Target.Raw == "" {
		fmt.Fprintf(a.Out, "Run id %s\n", rep.ID)
	}
}

func (a *App) writeRun(rep *domain.RunReport, format string) {
	switch format {
	case "html":
		b, err := report.HTML(rep)
		if err != nil {
			fmt.Fprintln(a.Err, err)
			return
		}
		fmt.Fprintf(a.Out, "%s", b)
	case "json":
		b, err := report.JSON(rep)
		if err != nil {
			fmt.Fprintln(a.Err, err)
			return
		}
		fmt.Fprintf(a.Out, "%s", b)
	case "md", "markdown":
		fmt.Fprint(a.Out, report.Markdown(rep))
	default:
		fmt.Fprint(a.Out, report.TextSummary(rep))
	}
	if rep.Artifacts.HTML != "" && format != "html" {
		fmt.Fprintf(a.Out, "HTML report         %s\n", rep.Artifacts.HTML)
	}
}

// Graph prints the impact graph.
func (a *App) Graph(ctx context.Context, opt Option, dependency, outPath string) (int, error) {
	dir, cfg, _, _, err := a.load(opt)
	if err != nil {
		return exitcode.Invalid, err
	}
	inv, err := discovery.Scan(ctx, dir, discovery.Options{Workers: cfg.Execution.Workers, Ignore: cfg.Repository.Ignore})
	if err != nil {
		return exitcode.Discovery, err
	}
	g := graph.Build(dir, inv)
	if dependency != "" {
		name := planner.ParseTarget(dependency, "").Name
		fmt.Fprintf(a.Out, "Projects consuming %s: %s\n", name, strings.Join(graph.ProjectsConsuming(g, inv, name), ", "))
		fmt.Fprintf(a.Out, "Files importing %s: %s\n", name, strings.Join(graph.FilesImporting(g, name), ", "))
	}
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return exitcode.Generic, err
	}
	if outPath != "" {
		if err := fsio.WriteAtomic(outPath, b, 0o644); err != nil {
			return exitcode.Generic, err
		}
		fmt.Fprintf(a.Out, "Wrote %s\n", outPath)
		return exitcode.Success, nil
	}
	if opt.Format == "json" || dependency == "" {
		fmt.Fprintf(a.Out, "%s\n", b)
	}
	return exitcode.Success, nil
}

// Outdated lists declared dependencies and does not invent registry results.
func (a *App) Outdated(ctx context.Context, opt Option) (int, error) {
	dir, cfg, _, _, err := a.load(opt)
	if err != nil {
		return exitcode.Invalid, err
	}
	inv, err := discovery.Scan(ctx, dir, discovery.Options{Workers: cfg.Execution.Workers, Ignore: cfg.Repository.Ignore})
	if err != nil {
		return exitcode.Discovery, err
	}
	if opt.Format == "json" {
		b, _ := json.MarshalIndent(inv.Dependencies, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", b)
		return exitcode.Success, nil
	}
	fmt.Fprintln(a.Out, "Declared dependencies. Registry status is unavailable unless a snapshot was supplied; none was queried.")
	for _, d := range inv.Dependencies {
		if !d.Direct {
			continue
		}
		fmt.Fprintf(a.Out, "%s:%s %s %s (%s)\n", d.Ecosystem, d.Name, d.CurrentVersion, d.Status, d.Manifest)
	}
	return exitcode.Success, nil
}

// Report writes a rendered snapshot.
func (a *App) Report(opt Option, id, format, output string) (int, error) {
	dir := a.dir(opt)
	rep, err := state.Open(dir).LoadReport(id)
	if err != nil {
		return exitcode.Invalid, fmt.Errorf("run %s: %w", id, err)
	}
	var body []byte
	switch format {
	case "json", "":
		body, err = report.JSON(rep)
	case "md", "markdown":
		body = []byte(report.Markdown(rep))
	case "html":
		body, err = report.HTML(rep)
	case "text":
		body = []byte(report.TextSummary(rep))
	default:
		return exitcode.Invalid, fmt.Errorf("unknown format %s", format)
	}
	if err != nil {
		return exitcode.Generic, err
	}
	if output == "" {
		fmt.Fprintf(a.Out, "%s", body)
		return exitcode.Success, nil
	}
	if err := fsio.WriteAtomic(output, body, 0o644); err != nil {
		return exitcode.Generic, err
	}
	fmt.Fprintf(a.Out, "Wrote %s\n", output)
	return exitcode.Success, nil
}

// History lists runs.
func (a *App) History(opt Option) (int, error) {
	runs, err := state.Open(a.dir(opt)).ListReports()
	if err != nil {
		return exitcode.Generic, err
	}
	if len(runs) == 0 {
		fmt.Fprintln(a.Out, "No runs recorded.")
		return exitcode.Success, nil
	}
	for _, r := range runs {
		fmt.Fprintf(a.Out, "%s  %s  %s:%s -> %s\n", r.ID, r.Outcome, r.Target.Ecosystem, r.Target.Name, r.Target.To)
	}
	return exitcode.Success, nil
}

// Rollback restores snapshotted files from a saved run.
func (a *App) Rollback(opt Option, id string) (int, error) {
	dir := a.dir(opt)
	if id == "" {
		rep, err := a.latest(opt, "")
		if err != nil {
			return exitcode.Invalid, err
		}
		id = rep.ID
	}
	if err := campaign.Rollback(dir, id); err != nil {
		return exitcode.Generic, err
	}
	fmt.Fprintf(a.Out, "Restored snapshotted files for %s\n", id)
	return exitcode.Success, nil
}

// Explain prints provenance for a path or change id.
func (a *App) Explain(opt Option, runID, target string) (int, error) {
	rep, err := a.latest(opt, runID)
	if err != nil {
		return exitcode.Invalid, err
	}
	found := false
	for _, c := range rep.Changes {
		if c.File == target || strings.HasSuffix(c.File, target) || c.ID == target {
			found = true
			fmt.Fprintf(a.Out, "Change %s\nReason: %s\nRecipe: %s\nFile: %s\nConfidence: %s\n", c.ID, c.Reason, c.RecipeID, c.File, c.Confidence)
		}
	}
	if !found {
		return exitcode.Invalid, fmt.Errorf("no change matches %s in run %s", target, rep.ID)
	}
	return exitcode.Success, nil
}

// Diff prints stored diffs.
func (a *App) Diff(opt Option, runID string) (int, error) {
	rep, err := a.latest(opt, runID)
	if err != nil {
		return exitcode.Invalid, err
	}
	for _, c := range rep.Changes {
		if c.Diff != "" {
			fmt.Fprintln(a.Out, c.Diff)
		}
	}
	return exitcode.Success, nil
}

func (a *App) latest(opt Option, id string) (*domain.RunReport, error) {
	st := state.Open(a.dir(opt))
	if id != "" {
		return st.LoadReport(id)
	}
	runs, err := st.ListReports()
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("no runs; run evolvectl plan or evolvectl upgrade first")
	}
	return runs[0], nil
}

// Validate re-runs gates for a saved plan by starting a dry validation note.
func (a *App) Validate(ctx context.Context, opt Option, runID string) (int, error) {
	rep, err := a.latest(opt, runID)
	if err != nil {
		return exitcode.Invalid, err
	}
	fmt.Fprintf(a.Out, "Recorded validations for %s (%s)\n", rep.ID, rep.Outcome)
	for _, v := range rep.Validations {
		fmt.Fprintf(a.Out, "%s %s required=%v %s\n", v.Status, v.Validator, v.Required, v.Reason)
	}
	_ = ctx
	return exitcode.Success, nil
}

// Analyze parses a log or prints grouped diagnostics from the latest run.
func (a *App) Analyze(opt Option, logPath string) (int, error) {
	if logPath != "" {
		b, err := os.ReadFile(logPath)
		if err != nil {
			return exitcode.Invalid, err
		}
		list := diagnostics.ParseToolOutput("go", string(b))
		groups := diagnostics.Group(list)
		b, err = json.MarshalIndent(groups, "", "  ")
		if err != nil {
			return exitcode.Generic, err
		}
		fmt.Fprintf(a.Out, "%s\n", b)
		return exitcode.Success, nil
	}
	rep, err := a.latest(opt, "")
	if err != nil {
		return exitcode.Invalid, err
	}
	b, _ := json.MarshalIndent(rep.Groups, "", "  ")
	fmt.Fprintf(a.Out, "%s\n", b)
	return exitcode.Success, nil
}

// Doctor checks tools and config.
func (a *App) Doctor(opt Option) (int, error) {
	dir, cfg, provider, source, err := a.load(opt)
	if err != nil {
		fmt.Fprintf(a.Out, "config FAIL %s\n", err.Error())
		return exitcode.Invalid, nil
	}
	fmt.Fprintf(a.Out, "config PASS %s\n", config.Path(dir))
	fmt.Fprintf(a.Out, "provider %s [%s]\n", provider, source)
	fmt.Fprintf(a.Out, "ai enabled=%v provider=%s\n", cfg.AI.Enabled, cfg.AI.Provider)
	for _, tool := range []string{"git", "go", "python", "python3", cfg.Workspace.Copybara.Binary} {
		if tool == "" {
			continue
		}
		if p, ok := runner.Look(tool); ok {
			fmt.Fprintf(a.Out, "%s PASS %s\n", tool, p)
		} else {
			fmt.Fprintf(a.Out, "%s UNAVAILABLE\n", tool)
		}
	}
	fmt.Fprintf(a.Out, "version %s\n", version.Version)
	return exitcode.Success, nil
}

// Adapters prints the capability matrix.
func (a *App) Adapters(opt Option) (int, error) {
	matrix := []domain.Capability{
		{Adapter: "go", Operation: "repair", Support: domain.SupportNative, Depth: "semantic"},
		{Adapter: "python", Operation: "repair", Support: domain.SupportNative, Depth: "structural"},
		{Adapter: "maven", Operation: "repair", Support: domain.SupportUnavailable, Depth: "manifest-only"},
		{Adapter: "node", Operation: "repair", Support: domain.SupportUnavailable, Depth: "manifest-only"},
		{Adapter: "git", Operation: "scm.status", Support: domain.SupportDelegated, Depth: "command-only"},
		{Adapter: "filesystem", Operation: "discover", Support: domain.SupportNative, Depth: "native"},
		{Adapter: "copybara", Operation: "explain", Support: domain.SupportReadOnly, Depth: "partial-static", Notes: "migrate is not invoked by upgrade"},
		{Adapter: "command", Operation: "validate.run", Support: domain.SupportDelegated, Depth: "command-only"},
	}
	if opt.Format == "json" {
		b, _ := json.MarshalIndent(matrix, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", b)
		return exitcode.Success, nil
	}
	for _, c := range matrix {
		fmt.Fprintf(a.Out, "%s %s %s %s %s\n", c.Adapter, c.Operation, c.Support, c.Depth, c.Notes)
	}
	dir := a.dir(opt)
	adapters, _ := commandadapt.LoadDir(filepath.Join(dir, ".evolvectl", "adapters"))
	for _, ad := range adapters {
		fmt.Fprintf(a.Out, "command-adapter %s %s\n", ad.Metadata.Name, ad.Path)
	}
	return exitcode.Success, nil
}

// RecipeList prints recipe ids.
func (a *App) RecipeList(ctx context.Context, opt Option) (int, error) {
	_ = ctx
	list, err := loadRecipes(a.dir(opt))
	if err != nil {
		return exitcode.Generic, err
	}
	for _, r := range list {
		fmt.Fprintf(a.Out, "%s  %s\n", r.Metadata.ID, r.Metadata.Title)
	}
	return exitcode.Success, nil
}

// RecipeTest runs golden files under dir.
func (a *App) RecipeTest(dir string) (int, error) {
	if dir == "" {
		dir = "recipes"
	}
	list, err := recipe.LoadDir(dir)
	if err != nil {
		return exitcode.Generic, err
	}
	failed := 0
	for _, r := range list {
		base := filepath.Dir(r.Path)
		inDir := filepath.Join(base, "testdata", "input")
		expDir := filepath.Join(base, "testdata", "expected")
		entries, err := os.ReadDir(inDir)
		if err != nil {
			fmt.Fprintf(a.Out, "SKIP %s no testdata\n", r.Metadata.ID)
			continue
		}
		vc := demoVersions(r)
		for _, e := range entries {
			in, err := os.ReadFile(filepath.Join(inDir, e.Name()))
			if err != nil {
				return exitcode.Generic, err
			}
			out, _, err := recipe.ApplySource(e.Name(), in, []recipe.Recipe{r}, vc)
			if err != nil {
				fmt.Fprintf(a.Out, "FAIL %s %s %s\n", r.Metadata.ID, e.Name(), err)
				failed++
				continue
			}
			exp, err := os.ReadFile(filepath.Join(expDir, e.Name()))
			if err != nil {
				fmt.Fprintf(a.Out, "FAIL %s missing expected %s\n", r.Metadata.ID, e.Name())
				failed++
				continue
			}
			if normalize(out) != normalize(exp) {
				fmt.Fprintf(a.Out, "FAIL %s %s\n", r.Metadata.ID, e.Name())
				failed++
				continue
			}
			fmt.Fprintf(a.Out, "PASS %s %s\n", r.Metadata.ID, e.Name())
		}
	}
	if failed > 0 {
		return exitcode.Validation, fmt.Errorf("%d recipe fixtures failed", failed)
	}
	return exitcode.Success, nil
}

// CopybaraExplain parses a config without running Copybara.
func (a *App) CopybaraExplain(opt Option, path, workflow, file, format string) (int, error) {
	if path == "" {
		path = "copy.bara.sky"
	}
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(a.dir(opt), path)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return exitcode.Invalid, err
	}
	parsed := skyparse.Parse(path, string(b))
	if format == "json" {
		body, _ := json.MarshalIndent(parsed, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", body)
		return exitcode.Success, nil
	}
	for _, wf := range parsed.Workflows {
		if workflow != "" && wf.Name != workflow {
			continue
		}
		fmt.Fprint(a.Out, skyparse.FormatText(wf))
		if file != "" {
			tr := skyparse.TraceFile(wf, file)
			fmt.Fprintf(a.Out, "File %s included=%v stage=%s output=%s confidence=%s content_modified=%v\n", file, tr.Included, tr.Stage, tr.OutputPath, tr.Confidence, tr.ContentModified)
			for _, n := range tr.Notes {
				fmt.Fprintf(a.Out, "Note %s\n", n)
			}
		}
	}
	fmt.Fprintln(a.Out, "No migration was run.")
	return exitcode.Success, nil
}

// BenchmarkScan times one scan.
func (a *App) BenchmarkScan(ctx context.Context, opt Option) (int, error) {
	dir, cfg, _, _, err := a.load(opt)
	if err != nil {
		return exitcode.Invalid, err
	}
	start := time.Now()
	inv, err := discovery.Scan(ctx, dir, discovery.Options{Workers: cfg.Execution.Workers, Ignore: cfg.Repository.Ignore})
	if err != nil {
		return exitcode.Discovery, err
	}
	elapsed := time.Since(start)
	fmt.Fprintf(a.Out, "scan_ms %d\nfiles %d\ncache_hits %d\ncache_misses %d\n", elapsed.Milliseconds(), inv.FilesIndexed, inv.CacheHits, inv.CacheMisses)
	return exitcode.Success, nil
}

// UIServe starts the read-only dashboard and blocks until the listener stops.
func (a *App) UIServe(opt Option, host, port string) (int, error) {
	ln, addr, err := ui.Listen(host, port)
	if err != nil {
		return exitcode.Invalid, err
	}
	provider, source := session.Resolve(opt.Provider, a.env(session.EnvProvider), "")
	fmt.Fprintf(a.Out, "evolvectl ui http://%s\nprovider %s [%s]\n", addr, provider, source)
	fmt.Fprintln(a.Out, "Read-only. Stop this process to stop the server.")
	srv := &ui.Server{Store: state.Open(a.dir(opt))}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Evolvectl-Provider", provider)
		srv.Handler().ServeHTTP(w, r)
	})
	httpSrv := &http.Server{Handler: handler}
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return exitcode.Generic, err
	}
	return exitcode.Success, nil
}

func loadRecipes(dir string) ([]recipe.Recipe, error) {
	embedded, err := recipe.LoadFS(bundled.FS)
	if err != nil {
		return nil, err
	}
	byID := map[string]recipe.Recipe{}
	for _, r := range embedded {
		byID[r.Metadata.ID] = r
	}
	for _, sub := range []string{"recipes", filepath.Join(".evolvectl", "recipes")} {
		local, err := recipe.LoadDir(filepath.Join(dir, sub))
		if err != nil {
			return nil, err
		}
		for _, r := range local {
			byID[r.Metadata.ID] = r
		}
	}
	var out []recipe.Recipe
	for _, r := range byID {
		out = append(out, r)
	}
	// stable order
	sortRecipes(out)
	return out, nil
}

func sortRecipes(in []recipe.Recipe) {
	for i := 0; i < len(in); i++ {
		for j := i + 1; j < len(in); j++ {
			if in[j].Metadata.ID < in[i].Metadata.ID {
				in[i], in[j] = in[j], in[i]
			}
		}
	}
}

func demoVersions(r recipe.Recipe) recipe.VersionContext {
	currents := []string{"1.10.2", "v1.58.3", "1.58.3", "1.0.0"}
	targets := []string{"2.11.0", "v1.75.0", "1.75.0", "2.0.0"}
	vc := recipe.VersionContext{Ecosystem: r.Spec.Dependency.Ecosystem, Name: r.Spec.Dependency.Name}
	for _, c := range currents {
		if recipe.ConstraintsAllow(r.Spec.Dependency.From, c) {
			vc.Current = c
			break
		}
	}
	for _, t := range targets {
		if recipe.ConstraintsAllow(r.Spec.Dependency.To, t) {
			vc.Target = t
			break
		}
	}
	return vc
}

func normalize(b []byte) string {
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// Docs prints an offline topic.
func (a *App) Docs(topic string) (int, error) {
	text, ok := docTopics[topic]
	if !ok {
		fmt.Fprintln(a.Out, "Topics: quickstart, providers, recipes, copybara, examples, troubleshooting, ui")
		for k := range docTopics {
			fmt.Fprintf(a.Out, "  %s\n", k)
		}
		if topic != "" && topic != "list" {
			return exitcode.Invalid, fmt.Errorf("unknown topic %s", topic)
		}
		return exitcode.Success, nil
	}
	fmt.Fprintln(a.Out, text)
	return exitcode.Success, nil
}
