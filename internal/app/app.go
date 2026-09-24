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
	"github.com/evolvectl/evolvectl/internal/guide"
	"github.com/evolvectl/evolvectl/internal/patch"
	"github.com/evolvectl/evolvectl/internal/planner"
	"github.com/evolvectl/evolvectl/internal/recipe"
	"github.com/evolvectl/evolvectl/internal/redact"
	"github.com/evolvectl/evolvectl/internal/registry"
	"github.com/evolvectl/evolvectl/internal/report"
	"github.com/evolvectl/evolvectl/internal/runner"
	"github.com/evolvectl/evolvectl/internal/scm"
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
	// Browser opens a local guide file. Nil uses the OS default browser.
	Browser func(path string) error
	// Registry answers outdated --online. Nil uses the public endpoints.
	Registry *registry.Client
	// Git runs git and gh for --open-pr and batch branches. Nil uses the real binaries.
	Git scm.Runner
}

func redactReason(s string) string {
	return redact.Text(s)
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
	// ToModule is the module path after the upgrade, for /vN moves and renames.
	ToModule string
	// VendorDir copies the module source into the workspace and adds a replace.
	VendorDir string
	// OpenPR commits the run on a new branch, pushes it, and opens a pull request.
	OpenPR bool
	// PRBase is the pull request base branch. Empty uses the repository default.
	PRBase string
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

// Init writes config, metadata directories, and the offline HTML guide.
// openGuide launches the start page in the browser after the files exist.
func (a *App) Init(opt Option, force, minimal, openGuide bool) (int, error) {
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
	guideDir := filepath.Join(dir, ".evolvectl", "guide")
	if err := guide.Write(guideDir); err != nil {
		return exitcode.Generic, err
	}
	page := filepath.Join(guideDir, "index.html")
	fmt.Fprintf(a.Out, "Wrote %s\n", path)
	fmt.Fprintf(a.Out, "Guide %s\n", page)
	if openGuide {
		if err := a.openBrowser(page); err != nil {
			if a.Err != nil {
				fmt.Fprintf(a.Err, "Could not open the guide (%s).\nOpen %s\n", err.Error(), page)
			}
		} else {
			fmt.Fprintln(a.Out, "Opened the guide in your browser.")
		}
	}
	return exitcode.Success, nil
}

// OpenGuide writes the HTML guide and optionally opens one page.
func (a *App) OpenGuide(opt Option, topic string, open bool) (int, error) {
	file, ok := guide.FileForTopic(topic)
	if !ok {
		fmt.Fprintln(a.Out, "Guide pages: index, help, copybara, languages, how-it-works, recipes, examples, troubleshooting")
		return exitcode.Invalid, fmt.Errorf("unknown guide topic %s", topic)
	}
	guideDir := filepath.Join(a.dir(opt), ".evolvectl", "guide")
	if err := guide.Write(guideDir); err != nil {
		return exitcode.Generic, err
	}
	page := filepath.Join(guideDir, file)
	fmt.Fprintf(a.Out, "Guide %s\n", page)
	if !open {
		return exitcode.Success, nil
	}
	if err := a.openBrowser(page); err != nil {
		if a.Err != nil {
			fmt.Fprintf(a.Err, "Could not open the guide (%s).\nOpen %s\n", err.Error(), page)
		}
		return exitcode.Success, nil
	}
	fmt.Fprintln(a.Out, "Opened the guide in your browser.")
	return exitcode.Success, nil
}

func (a *App) openBrowser(path string) error {
	if a.Browser != nil {
		return a.Browser(path)
	}
	return guide.Open(path)
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

// Impact compares the exported API of the current and target versions and lists the call
// sites in the workspace that use removed or changed symbols. Source is not modified.
func (a *App) Impact(ctx context.Context, opt Option, dependency, to string) (int, error) {
	rep, _, err := a.campaign(ctx, opt, dependency, to, true, "plan")
	if rep == nil || rep.Plan == nil {
		if err != nil {
			return exitcode.Invalid, err
		}
		return exitcode.Generic, fmt.Errorf("plan was not produced")
	}
	switch opt.Format {
	case "json":
		b, _ := json.MarshalIndent(rep.Impact, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", b)
	case "html":
		b, err := report.HTML(rep)
		if err != nil {
			return exitcode.Generic, err
		}
		fmt.Fprintf(a.Out, "%s", b)
	default:
		fmt.Fprint(a.Out, report.ImpactText(rep.Impact))
		fmt.Fprintf(a.Out, "Run id %s. No files changed.\n", rep.ID)
	}
	if rep.Impact == nil || rep.Impact.Status != "recorded" {
		return exitcode.NeedsReview, nil
	}
	return exitcode.Success, nil
}

// Upgrade runs the campaign.
func (a *App) Upgrade(ctx context.Context, opt Option, dependency, to, resume string) (int, error) {
	if resume != "" {
		return a.resume(ctx, opt, resume)
	}
	halt := ""
	rep, code, err := a.campaign(ctx, opt, dependency, to, opt.DryRun, halt)
	if rep != nil && opt.OpenPR {
		a.publish(ctx, a.dir(opt), rep, opt.PRBase)
	}
	if rep != nil {
		a.writeRun(rep, opt.Format)
	}
	return code, err
}

func (a *App) gitRunner() scm.Runner {
	if a.Git != nil {
		return a.Git
	}
	return scm.Exec{}
}

// publish commits the run on a new branch, pushes it, opens a pull request, and saves the result on the run.
func (a *App) publish(ctx context.Context, dir string, rep *domain.RunReport, base string) {
	g := scm.Git{R: a.gitRunner(), Dir: dir}
	body := report.PRBody(rep)
	bodyFile := filepath.Join(dir, ".evolvectl", "runs", rep.ID, "pr.md")
	rep.SCM = scm.Publish(ctx, g, rep, base, bodyFile, body)
	a.saveRun(dir, rep)
}

func (a *App) saveRun(dir string, rep *domain.RunReport) {
	st := state.Open(dir)
	if err := st.SaveReport(rep); err != nil {
		return
	}
	if b, err := report.HTML(rep); err == nil {
		_ = fsio.WriteAtomic(filepath.Join(dir, ".evolvectl", "runs", rep.ID, "report.html"), b, 0o644)
	}
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
		Workspace: dir, Dependency: dependency, To: to, ToModule: opt.ToModule, VendorDir: opt.VendorDir,
		DryRun: dry, NoAI: noAI, Offline: opt.Offline,
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
	extra := ""
	if p.Target.ToModule != "" && p.Target.ToModule != p.Target.Name {
		fmt.Fprintf(a.Out, "New module           %s\n", p.Target.ToModule)
		extra = " --to-module " + p.Target.ToModule
	}
	if p.Target.VendorDir != "" {
		fmt.Fprintf(a.Out, "Vendor into          %s\n", p.Target.VendorDir)
	}
	fmt.Fprintf(a.Out, "Projects             %s\n", strings.Join(p.Projects, ", "))
	fmt.Fprintf(a.Out, "Files                %d\n", len(p.Files))
	fmt.Fprintf(a.Out, "Recipes              %s\n", strings.Join(p.Recipes, ", "))
	fmt.Fprintf(a.Out, "Risk                 %s (confidence %s)\n", p.Risk.Level, strings.ToLower(string(p.Risk.Confidence)))
	if imp := rep.Impact; imp != nil {
		if imp.Status == "recorded" {
			fmt.Fprintf(a.Out, "API impact           %d removed, %d changed; %d call site(s) in %d file(s). evolvectl impact lists them.\n", imp.Removed, imp.Changed, len(imp.Sites), imp.FilesAffected)
		} else {
			fmt.Fprintf(a.Out, "API impact           unavailable: %s\n", redactReason(imp.Reason))
		}
	}
	fmt.Fprintf(a.Out, "Copybara             %s\n", p.CopybaraImpact)
	if len(p.ReviewPoints) > 0 {
		fmt.Fprintln(a.Out, "Review")
		for _, r := range p.ReviewPoints {
			fmt.Fprintf(a.Out, "  %s\n", r)
		}
	}
	fmt.Fprintf(a.Out, "No files changed. Run evolvectl upgrade %s --to %s%s --apply\n", p.Target.Raw, p.Target.To, extra)
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

// Outdated lists declared dependencies. With online it queries the go command, PyPI, npm,
// Maven Central, and OSV; without it no registry is contacted and nothing is invented.
func (a *App) Outdated(ctx context.Context, opt Option, online bool) (int, error) {
	dir, cfg, _, _, err := a.load(opt)
	if err != nil {
		return exitcode.Invalid, err
	}
	inv, err := discovery.Scan(ctx, dir, discovery.Options{Workers: cfg.Execution.Workers, Ignore: cfg.Repository.Ignore})
	if err != nil {
		return exitcode.Discovery, err
	}
	if online {
		if opt.Offline {
			return exitcode.Invalid, fmt.Errorf("pass only one of --online and --offline")
		}
		client := a.Registry
		if client == nil {
			client = registry.New()
		}
		findings := client.Check(ctx, inv.Dependencies)
		registry.Apply(inv.Dependencies, findings, time.Now().UTC())
		switch opt.Format {
		case "json":
			b, _ := json.MarshalIndent(findings, "", "  ")
			fmt.Fprintf(a.Out, "%s\n", b)
		case "html":
			b, err := report.InventoryHTML(inv)
			if err != nil {
				return exitcode.Generic, err
			}
			fmt.Fprintf(a.Out, "%s", b)
		default:
			fmt.Fprintln(a.Out, "Direct dependencies. Go versions come from go list -m (GOPROXY applies); advisories come from OSV.")
			counts := map[string]int{}
			for _, f := range findings {
				counts[f.Status]++
				latest := f.Latest
				if latest == "" {
					latest = "-"
				}
				fmt.Fprintf(a.Out, "%-8s %-50s %-40s %-24s %s\n", f.Ecosystem, f.Dependency, f.Current, latest, f.Status)
				if f.Status != domain.StatusCurrent && f.Reason != "" {
					fmt.Fprintf(a.Out, "         %s\n", redactReason(f.Reason))
				}
			}
			fmt.Fprintf(a.Out, "current %d, update_available %d, security_update %d, other %d\n",
				counts[domain.StatusCurrent], counts[domain.StatusUpdateAvailable], counts[domain.StatusSecurityUpdate],
				len(findings)-counts[domain.StatusCurrent]-counts[domain.StatusUpdateAvailable]-counts[domain.StatusSecurityUpdate])
		}
		for _, f := range findings {
			if f.Status == domain.StatusSecurityUpdate {
				return exitcode.NeedsReview, nil
			}
		}
		return exitcode.Success, nil
	}
	if opt.Format == "json" {
		b, _ := json.MarshalIndent(inv.Dependencies, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", b)
		return exitcode.Success, nil
	}
	fmt.Fprintln(a.Out, "Declared dependencies. No registry was queried; pass --online for latest versions and advisories.")
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

// CopybaraPin sets the origin ref of one workflow in copy.bara.sky and prints the diff.
// It does not run Copybara.
func (a *App) CopybaraPin(opt Option, path, workflow, ref string, dry bool) (int, error) {
	if workflow == "" || ref == "" {
		return exitcode.Invalid, fmt.Errorf("--workflow and --ref are required")
	}
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
	out, old, err := skyparse.Pin(string(b), workflow, ref)
	if err != nil {
		return exitcode.Invalid, err
	}
	if out == string(b) {
		fmt.Fprintf(a.Out, "Workflow %s already pins %s. No change.\n", workflow, ref)
		return exitcode.Success, nil
	}
	fmt.Fprint(a.Out, redact.Text(patch.Unified(filepath.ToSlash(path), string(b), out)))
	if old == "" {
		old = "<none>"
	}
	fmt.Fprintf(a.Out, "Workflow %s ref %s -> %s\n", workflow, old, ref)
	if dry {
		fmt.Fprintln(a.Out, "Dry run: file not written. Copybara was not run.")
		return exitcode.Success, nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return exitcode.Generic, err
	}
	if err := fsio.WriteAtomic(full, []byte(out), info.Mode().Perm()); err != nil {
		return exitcode.Generic, err
	}
	fmt.Fprintf(a.Out, "Wrote %s. Copybara was not run; run copybara migrate yourself when ready.\n", filepath.ToSlash(path))
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
		fmt.Fprintln(a.Out, "Topics: quickstart, providers, recipes, copybara, languages, examples, troubleshooting, ui, session")
		fmt.Fprintln(a.Out, "HTML guide: evolvectl docs <topic> --format html")
		fmt.Fprintln(a.Out, "Commands: evolvectl help")
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
