// Package cli maps Cobra commands onto the app service.
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/evolvectl/evolvectl/internal/app"
	"github.com/evolvectl/evolvectl/internal/apperr"
	"github.com/evolvectl/evolvectl/internal/exitcode"
	"github.com/evolvectl/evolvectl/internal/session"
	"github.com/evolvectl/evolvectl/internal/skyparse"
	"github.com/evolvectl/evolvectl/internal/version"
)

type statusError struct {
	code int
	err  error
}

func (e *statusError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return apperr.Format(e.err)
}

func (e *statusError) Unwrap() error { return e.err }

// Execute runs the CLI.
func Execute(args []string) (int, error) {
	root := NewRoot()
	root.SetArgs(args)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.SilenceErrors = true
	root.SilenceUsage = true
	err := root.Execute()
	if err == nil {
		return exitcode.Success, nil
	}
	var se *statusError
	if ok := asStatus(err, &se); ok {
		return se.code, se
	}
	return exitcode.Generic, err
}

func asStatus(err error, target **statusError) bool {
	se, ok := err.(*statusError)
	if ok {
		*target = se
		return true
	}
	return false
}

type flags struct {
	workspace  string
	config     string
	provider   string
	noAI       bool
	offline    bool
	format     string
	allowDirty bool
	dryRun     bool
	apply      bool
	toModule   string
	vendorDir  string
	useDir     string
	openPR     bool
	prBase     string
}

func (f flags) opt() app.Option {
	return app.Option{
		Workspace: f.workspace, ConfigPath: f.config, Provider: f.provider,
		NoAI: f.noAI, Offline: f.offline, Format: f.format, AllowDirty: f.allowDirty, DryRun: f.dryRun,
		ToModule: f.toModule, VendorDir: f.vendorDir, UseDir: f.useDir, OpenPR: f.openPR, PRBase: f.prBase,
	}
}

func application() *app.App {
	return &app.App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Getenv: os.Getenv, Version: version.Version}
}

func finish(code int, err error) error {
	if err == nil && code == 0 {
		return nil
	}
	if code == 0 {
		code = exitcode.Generic
	}
	return &statusError{code: code, err: err}
}

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	var f flags
	root := &cobra.Command{
		Use:   "evolvectl",
		Short: "Upgrade codebases, not just dependency files.",
		Long: `Evolvectl discovers a repository, plans a dependency upgrade, applies deterministic recipes, validates, and writes a report.

Golden workflow:
  1. evolvectl init
  2. evolvectl scan
  3. evolvectl plan <target> --to <version>
  4. evolvectl upgrade <target> --to <version>
  5. evolvectl report --run <id> --format html

More:
  evolvectl outdated --online                 latest versions and known vulnerabilities
  evolvectl impact <target> --to <version>    API differences and the call sites they hit
  evolvectl upgrade <old> --to v2.0.2 --to-module <old>/v2   major version or module move
  evolvectl batch --file upgrades.csv         every row of an upgrade sheet, one report
  evolvectl vendor <module> --to <v> --dir third_party/<name>
  evolvectl copybara pin --workflow <name> --ref <commit-or-tag>

init writes .evolvectl/guide and opens index.html. evolvectl help opens the command reference. evolvectl docs <topic> --format html opens that page.

upgrade stores a styled HTML report at .evolvectl/runs/<id>/report.html, including dependency inventory, diffs, grouped failures, and before/after tests and coverage. --format html prints that page. --offline keeps go test from downloading modules.

AI is optional. --no-ai keeps the deterministic path. copybara explain reads copy.bara.sky. You run copybara migrate yourself. upgrade edits the checkout you named.

Exit codes: 0 success, 2 bad arguments, 5 validation failed, 6 needs review, 7 policy, 8 tool missing, 9 dirty worktree, 10 interrupted.`,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&f.workspace, "workspace", "", "workspace root (default: current directory)")
	root.PersistentFlags().StringVar(&f.config, "config", "", "config file (default: .evolvectl.yaml)")
	root.PersistentFlags().StringVar(&f.provider, "provider", "", "workspace provider for this command; overrides EVOLVECTL_WORKSPACE_PROVIDER")
	root.PersistentFlags().BoolVar(&f.noAI, "no-ai", false, "force deterministic mode")
	root.PersistentFlags().BoolVar(&f.offline, "offline", false, "do not query package registries")
	root.PersistentFlags().StringVar(&f.format, "format", "text", "output format: text, json, md, or html where supported")
	root.PersistentFlags().BoolVar(&f.allowDirty, "allow-dirty", false, "allow a dirty git worktree")

	root.AddCommand(
		versionCmd(),
		initCmd(&f),
		scanCmd(&f),
		graphCmd(&f),
		outdatedCmd(&f),
		planCmd(&f),
		impactCmd(&f),
		upgradeCmd(&f),
		vendorCmd(&f),
		batchCmd(&f),
		analyzeCmd(&f),
		repairCmd(),
		validateCmd(&f),
		explainCmd(&f),
		diffCmd(&f),
		historyCmd(&f),
		rollbackCmd(&f),
		reportCmd(&f),
		recipeCmd(&f),
		adaptersCmd(&f),
		providerCmd(),
		sessionCmd(),
		docsCmd(&f),
		benchmarkCmd(&f),
		doctorCmd(&f),
		uiCmd(&f),
		copybaraCmd(&f),
		completionCmd(root),
		helpCmd(root, &f),
	)
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the evolvectl version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "evolvectl %s\n", version.Version)
			return nil
		},
	}
}

func initCmd(f *flags) *cobra.Command {
	var force, minimal, noBrowser bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create config, metadata directories, and open the HTML guide",
		Long: `Create .evolvectl.yaml, .evolvectl/, and the offline HTML guide.

Opens .evolvectl/guide/index.html in the browser. Pass --no-browser to write the files only.
The guide explains Copybara, other tools in that role, each language, and every command.

Init leaves EVOLVECTL_WORKSPACE_PROVIDER unset. Use evolvectl session export to select a provider in this terminal.

Exit codes: 0 written, 2 config already exists.`,
		Example: "  evolvectl init\n  evolvectl init --no-browser\n  evolvectl init --force --no-ai",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			a.Err = cmd.ErrOrStderr()
			code, err := a.Init(f.opt(), force, minimal, !noBrowser)
			return finish(code, err)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing config")
	cmd.Flags().BoolVar(&minimal, "minimal", false, "write a smaller ignore list")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "write the HTML guide without opening a browser")
	return cmd
}

func scanCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:     "scan [path]",
		Short:   "Discover projects, manifests, and dependencies",
		Long:    "Walk the workspace once and print a dependency inventory. This command does not modify source files and does not start a UI server.",
		Example: "  evolvectl scan\n  evolvectl scan --format html\n  evolvectl scan --format json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.workspace = args[0]
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Scan(context.Background(), f.opt())
			return finish(code, err)
		},
	}
}

func graphCmd(f *flags) *cobra.Command {
	var dep, out string
	cmd := &cobra.Command{
		Use:     "graph",
		Short:   "Show the dependency and impact graph",
		Example: "  evolvectl graph --format json\n  evolvectl graph --dependency google.golang.org/grpc",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Graph(context.Background(), f.opt(), dep, out)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&dep, "dependency", "", "limit the textual summary to one dependency")
	cmd.Flags().StringVar(&out, "out", "", "write JSON graph to this path")
	return cmd
}

func outdatedCmd(f *flags) *cobra.Command {
	var online bool
	cmd := &cobra.Command{
		Use:   "outdated",
		Short: "List declared dependencies, and with --online their latest versions and vulnerabilities",
		Long: `Without --online, prints declared versions and leaves status as registry_unavailable.

With --online, asks the real sources: go list -m <module>@latest (through your GOPROXY, including the next major path such as /v2), PyPI, the npm registry, Maven Central, and OSV for known vulnerabilities of the exact declared version. Ranges are reported as unresolved rather than guessed.

Exit codes: 0 listed, 6 at least one declared version has a known vulnerability.`,
		Example: "  evolvectl outdated\n  evolvectl outdated --online\n  evolvectl outdated --online --format html > outdated.html",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Outdated(context.Background(), f.opt(), online)
			return finish(code, err)
		},
	}
	cmd.Flags().BoolVar(&online, "online", false, "query registries and OSV")
	return cmd
}

func impactCmd(f *flags) *cobra.Command {
	var dep, to string
	cmd := &cobra.Command{
		Use:   "impact [dependency]",
		Short: "List the API differences between two versions and the call sites they affect",
		Long: `Downloads both versions (go mod download, or a local replace directory), compares their exported API, and lists every file and line in this workspace that uses a removed or changed symbol.

Use --to-module for a major-version move such as github.com/googleapis/gax-go to github.com/googleapis/gax-go/v2.
The comparison reads declarations and is not type-checked. Source is not modified. Go only.

Exit codes: 0 impact recorded, 6 impact unavailable (for example offline with an empty module cache).`,
		Example: "  evolvectl impact github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2\n  evolvectl impact google.golang.org/grpc --to v1.75.0 --format json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				dep = args[0]
			}
			if dep == "" {
				return finish(exitcode.Invalid, fmt.Errorf("dependency is required"))
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Impact(context.Background(), f.opt(), dep, to)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&dep, "dependency", "", "module path")
	cmd.Flags().StringVar(&to, "to", "", "target version")
	cmd.Flags().StringVar(&f.toModule, "to-module", "", "new module path for a major-version move")
	addUseDirFlag(cmd, f)
	return cmd
}

func addUseDirFlag(cmd *cobra.Command, f *flags) {
	cmd.Flags().StringVar(&f.useDir, "use-dir", "", "new version already in the workspace (for example imported by Copybara); adds a replace and never downloads")
}

func vendorCmd(f *flags) *cobra.Command {
	var dep, to string
	cmd := &cobra.Command{
		Use:   "vendor [module]",
		Short: "Copy a module version into a third_party directory and point go.mod at it",
		Long: `Downloads the module version, replaces the directory named by --dir with its files, writes METADATA.evolvectl.json (module, version, go.sum hash, license file), adds a replace directive to each go.mod, then validates like upgrade.

A missing license file ends the run as needs-review. Rollback restores the previous directory. --dry-run works on a copy.
This is the open-source-repository version of a third_party import. It does not write Google BUILD or METADATA files.`,
		Example: "  evolvectl vendor github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2 --dir third_party/gax\n  evolvectl vendor golang.org/x/text --to v0.21.0 --dir third_party/text --dry-run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				dep = args[0]
			}
			if dep == "" || f.vendorDir == "" {
				return finish(exitcode.Invalid, fmt.Errorf("module and --dir are required"))
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Upgrade(context.Background(), f.opt(), dep, to, "")
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&dep, "dependency", "", "module path")
	cmd.Flags().StringVar(&to, "to", "", "version to vendor")
	cmd.Flags().StringVar(&f.toModule, "to-module", "", "new module path for a major-version move")
	cmd.Flags().StringVar(&f.vendorDir, "dir", "", "destination directory, relative to the workspace")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "do not modify the original workspace")
	addPRFlags(cmd, f)
	return cmd
}

func batchCmd(f *flags) *cobra.Command {
	var file, mode string
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Run every upgrade in a CSV or YAML sheet and write one combined report",
		Long: `Reads rows of dependency, target version, optional new module, workspace, and owner.

CSV headers are matched loosely: Dependency/Package/Module, To/Target Version, To Module, Workspace/Path/Directory, Owner/Developer Owner. YAML uses an upgrades: list with the same keys.

Modes:
  dry-run   (default) each row runs on its own temporary copy; nothing in the workspace changes
  apply     rows edit the workspace one after another
  branches  each row runs on a new branch from the current commit and is committed there; needs a clean worktree

--open-pr (branches mode only) pushes each committed branch and opens a pull request with gh.
The combined report is written to .evolvectl/batches/<id>/batch.html, batch.md, and batch.json.

Exit codes: 0 all rows succeeded, 6 some need review, 4 some failed.`,
		Example: "  evolvectl batch --file upgrades.csv\n  evolvectl batch --file upgrades.yaml --mode branches\n  evolvectl batch --file upgrades.csv --mode branches --open-pr --pr-base main",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return finish(exitcode.Invalid, fmt.Errorf("--file is required"))
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Batch(context.Background(), f.opt(), file, mode, f.openPR)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "CSV or YAML upgrade sheet")
	cmd.Flags().StringVar(&mode, "mode", "dry-run", "dry-run, apply, or branches")
	addPRFlags(cmd, f)
	return cmd
}

func addPRFlags(cmd *cobra.Command, f *flags) {
	cmd.Flags().BoolVar(&f.openPR, "open-pr", false, "commit on a new branch, push it, and open a pull request with gh; off by default")
	cmd.Flags().StringVar(&f.prBase, "pr-base", "", "pull request base branch (default: the repository default)")
}

func planCmd(f *flags) *cobra.Command {
	var dep, to string
	cmd := &cobra.Command{
		Use:   "plan [dependency]",
		Short: "Plan an upgrade without modifying source",
		Long: `Resolve the current declaration, affected projects, matching recipes, and validators.

A plan-only run is saved under .evolvectl/runs and does not edit source files.
Exit codes: 0 plan written, 2 unknown dependency or bad arguments.`,
		Example: "  evolvectl plan google.golang.org/grpc --to v1.75.0\n  evolvectl plan python:pydantic --to 2.11.0",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				dep = args[0]
			}
			if dep == "" {
				return finish(exitcode.Invalid, fmt.Errorf("dependency is required"))
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Plan(context.Background(), f.opt(), dep, to)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&dep, "dependency", "", "ecosystem:name or module path")
	cmd.Flags().StringVar(&to, "to", "", "target version")
	cmd.Flags().StringVar(&f.toModule, "to-module", "", "new module path for a major-version move, such as <module>/v2")
	addUseDirFlag(cmd, f)
	return cmd
}

func upgradeCmd(f *flags) *cobra.Command {
	var dep, to, resume string
	cmd := &cobra.Command{
		Use:   "upgrade [dependency]",
		Short: "Apply an upgrade, repair with recipes, and validate",
		Long: `Runs preflight, discovery, plan, apply, diagnosis, recipe repair, and validation.

--dry-run copies the workspace to a temp directory and leaves the original source unchanged.
--no-ai is the deterministic path. Semantic repair is unavailable for ecosystems without a recipe adapter; those runs finish as needs-review instead of pretending tests passed.

--to-module moves Go code to a new module path, for example a /v2 major version: go.mod require, every import, go.sum, and recipes for the new API. Package names used in code are not renamed.
--use-dir names a copy of the new version that a sync tool such as Copybara already wrote into the workspace, for example third_party/gax-go/v2. go.mod gets a replace to it, impact compares against it, and nothing is downloaded.
--open-pr commits the changed files on a new branch, pushes it, and opens a pull request with the report as its body. It never force-pushes and is off by default.

Exit codes: 0 required gates passed, 5 validation failed, 6 needs review, 7 policy, 9 dirty worktree, 10 interrupted.`,
		Example: "  evolvectl upgrade google.golang.org/grpc --to v1.75.0\n  evolvectl upgrade github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2\n  evolvectl upgrade python:pydantic --to 2.11.0 --dry-run\n  evolvectl upgrade google.golang.org/grpc --to v1.75.0 --open-pr --pr-base main",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				dep = args[0]
			}
			if f.dryRun && f.apply {
				return finish(exitcode.Invalid, fmt.Errorf("pass only one of --dry-run and --apply"))
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			opt := f.opt()
			opt.DryRun = f.dryRun
			code, err := a.Upgrade(context.Background(), opt, dep, to, resume)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&dep, "dependency", "", "ecosystem:name or module path")
	cmd.Flags().StringVar(&to, "to", "", "target version")
	cmd.Flags().StringVar(&resume, "resume", "", "resume a cancelled run id")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "do not modify the original workspace")
	cmd.Flags().BoolVar(&f.apply, "apply", false, "accepted for explicitness; upgrade already writes unless --dry-run is set")
	cmd.Flags().StringVar(&f.toModule, "to-module", "", "new module path for a major-version move, such as <module>/v2")
	addUseDirFlag(cmd, f)
	addPRFlags(cmd, f)
	return cmd
}

func analyzeCmd(f *flags) *cobra.Command {
	var log string
	cmd := &cobra.Command{
		Use:     "analyze",
		Short:   "Group diagnostics from a log or the latest run",
		Example: "  evolvectl analyze\n  evolvectl analyze --log build.log",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Analyze(f.opt(), log)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&log, "log", "", "parser input; go compiler output is the default parser")
	return cmd
}

func repairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Show that repair runs inside upgrade",
		Long:  "Deterministic repair runs during evolvectl upgrade. This command does not apply a second untracked edit.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Repair is part of evolvectl upgrade. Re-run upgrade, or inspect the latest run with evolvectl explain.")
			return nil
		},
	}
}

func validateCmd(f *flags) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:     "validate",
		Short:   "Show recorded validation gates for a run",
		Example: "  evolvectl validate --run <id>",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Validate(context.Background(), f.opt(), id)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&id, "run", "", "run id (default: latest)")
	return cmd
}

func explainCmd(f *flags) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:     "explain <file-or-change>",
		Short:   "Explain a recorded change",
		Example: "  evolvectl explain client/client.go\n  evolvectl explain --change chg_123",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			if c, _ := cmd.Flags().GetString("change"); c != "" {
				target = c
			}
			if target == "" {
				return finish(exitcode.Invalid, fmt.Errorf("file or --change is required"))
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Explain(f.opt(), id, target)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&id, "run", "", "run id")
	cmd.Flags().String("change", "", "change id")
	return cmd
}

func diffCmd(f *flags) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Print diffs recorded for a run",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Diff(f.opt(), id)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&id, "run", "", "run id")
	return cmd
}

func rollbackCmd(f *flags) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:     "rollback",
		Short:   "Restore files snapshotted by a run",
		Long:    "Restores each snapshotted file when its current bytes still match the campaign. Files created by the run, such as a new go.sum, are removed.",
		Example: "  evolvectl rollback --run <id>",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Rollback(f.opt(), id)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&id, "run", "", "run id (default: latest)")
	return cmd
}

func historyCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "List local runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.History(f.opt())
			return finish(code, err)
		},
	}
}

func reportCmd(f *flags) *cobra.Command {
	var id, output string
	cmd := &cobra.Command{
		Use:     "report",
		Short:   "Render a saved run as text, JSON, Markdown, or HTML",
		Long:    "HTML is a single offline file with no remote assets. It renders the saved snapshot and does not rescan the repository.",
		Example: "  evolvectl report --run <id> --format json\n  evolvectl report --run <id> --format html --output report.html",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if id == "" {
				return finish(exitcode.Invalid, fmt.Errorf("--run is required"))
			}
			format := f.format
			if format == "text" && cmd.Flags().Changed("format") == false {
				format = "text"
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Report(f.opt(), id, format, output)
			return finish(code, err)
		},
	}
	cmd.Flags().StringVar(&id, "run", "", "run id")
	cmd.Flags().StringVar(&output, "output", "", "write the report to this path")
	return cmd
}

func recipeCmd(f *flags) *cobra.Command {
	cmd := &cobra.Command{Use: "recipe", Short: "Inspect and test migration recipes"}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List recipes", RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.RecipeList(context.Background(), f.opt())
			return finish(code, err)
		}},
		&cobra.Command{Use: "scaffold", Short: "Print a recipe template", RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.OutOrStdout(), recipeScaffold())
			return nil
		}},
		&cobra.Command{Use: "test [dir]", Short: "Run recipe golden files", RunE: func(cmd *cobra.Command, args []string) error {
			dir := "recipes"
			if len(args) == 1 {
				dir = args[0]
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.RecipeTest(dir)
			return finish(code, err)
		}},
	)
	return cmd
}

func recipeScaffold() string {
	return "apiVersion: evolvectl.dev/v1alpha1\nkind: MigrationRecipe\nmetadata:\n  id: example.call-rename\n  title: Rename an example call\nspec:\n  language: go\n  transform:\n    type: call_rename\n"
}

func adaptersCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "adapters",
		Short: "Show adapter capability levels",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Adapters(f.opt())
			return finish(code, err)
		},
	}
}

func providerCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "provider", Short: "List built-in providers"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List providers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.OutOrStdout(), "filesystem built-in discover\ngit built-in scm.status\ncopybara built-in explain read-only\ncommand declarative validate\n")
			return nil
		},
	})
	return cmd
}

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "session",
		Short:   "Terminal-scoped provider selection",
		Long:    "A child process cannot change the parent shell. session export prints an assignment you can eval. It never writes repository config.",
		Example: "  evolvectl session current\n  evolvectl session export git --shell bash",
	}
	cmd.AddCommand(
		&cobra.Command{Use: "current", Short: "Print the provider visible in this process", RunE: func(cmd *cobra.Command, _ []string) error {
			v := os.Getenv(session.EnvProvider)
			if v == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "workspace=<unset>")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "workspace=%s\n", v)
			return nil
		}},
		&cobra.Command{Use: "use [provider]", Short: "Print how to select a provider in this terminal", RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return finish(exitcode.Invalid, fmt.Errorf("provider name is required"))
			}
			fmt.Fprintln(cmd.OutOrStdout(), session.UseHint(args[0]))
			return nil
		}},
		&cobra.Command{Use: "clear", Short: "Print a shell snippet that clears the selection", RunE: func(cmd *cobra.Command, _ []string) error {
			shell, _ := cmd.Flags().GetString("shell")
			text, err := session.Clear(shell)
			if err != nil {
				return finish(exitcode.Invalid, err)
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		}},
	)
	export := &cobra.Command{Use: "export [provider]", Short: "Print a shell assignment for the current terminal", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return finish(exitcode.Invalid, fmt.Errorf("provider name is required"))
		}
		shell, _ := cmd.Flags().GetString("shell")
		text, err := session.Export(args[0], shell)
		if err != nil {
			return finish(exitcode.Invalid, err)
		}
		fmt.Fprint(cmd.OutOrStdout(), text)
		return nil
	}}
	export.Flags().String("shell", "", "bash, zsh, fish, or powershell")
	cmd.PersistentFlags().String("shell", "", "bash, zsh, fish, or powershell")
	cmd.AddCommand(export)
	return cmd
}

func docsCmd(f *flags) *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "docs [topic]",
		Short: "Print a short topic, or open it as HTML",
		Long: `With the default text format, print a short topic.

With --format html, write .evolvectl/guide and open that page.
Topics: quickstart, providers, recipes, copybara, languages, examples, troubleshooting, ui, session.
evolvectl help opens the command reference.`,
		Example: "  evolvectl docs copybara\n  evolvectl docs languages --format html\n  evolvectl docs --format html --no-browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := ""
			if len(args) == 1 {
				topic = args[0]
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			a.Err = cmd.ErrOrStderr()
			if f.format == "html" {
				code, err := a.OpenGuide(f.opt(), topic, !noBrowser)
				return finish(code, err)
			}
			code, err := a.Docs(topic)
			return finish(code, err)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "write the HTML guide without opening a browser")
	return cmd
}

func helpCmd(root *cobra.Command, f *flags) *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Open the HTML command guide, or print help for one command",
		Long: `With no arguments, write .evolvectl/guide and open help.html.

That page lists every command with an explanation and an example. The start page explains how evolvectl differs from Copybara and how each language is handled.

With a command name, print that command's terminal help.
evolvectl --help prints the short terminal summary.`,
		Example: "  evolvectl help\n  evolvectl help upgrade\n  evolvectl help copybara explain\n  evolvectl help --no-browser",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				sub, _, err := root.Find(args)
				if err != nil || sub == nil || sub == root {
					return finish(exitcode.Invalid, fmt.Errorf("unknown command %s", strings.Join(args, " ")))
				}
				if sub != cmd {
					sub.SetOut(cmd.OutOrStdout())
					sub.SetErr(cmd.ErrOrStderr())
					return sub.Help()
				}
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			a.Err = cmd.ErrOrStderr()
			code, err := a.OpenGuide(f.opt(), "help", !noBrowser)
			return finish(code, err)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "write the HTML guide without opening a browser")
	return cmd
}

func benchmarkCmd(f *flags) *cobra.Command {
	cmd := &cobra.Command{Use: "benchmark", Short: "Time a local operation"}
	cmd.AddCommand(&cobra.Command{
		Use:   "scan [path]",
		Short: "Time discovery",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.workspace = args[0]
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.BenchmarkScan(context.Background(), f.opt())
			return finish(code, err)
		},
	})
	return cmd
}

func doctorCmd(f *flags) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check config and external tools",
		Long:  "A missing optional tool is UNAVAILABLE, not a failed doctor run. Copybara is optional unless you select it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Doctor(f.opt())
			return finish(code, err)
		},
	}
}

func uiCmd(f *flags) *cobra.Command {
	cmd := &cobra.Command{Use: "ui", Short: "Local read-only dashboard"}
	var host, port string
	serve := &cobra.Command{
		Use:     "serve",
		Short:   "Serve saved runs on loopback",
		Long:    "Binds 127.0.0.1 by default. Refuses other hosts. Does not modify source. Stop the process to stop the server.",
		Example: "  evolvectl ui serve --host 127.0.0.1 --port 0",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.UIServe(f.opt(), host, port)
			return finish(code, err)
		},
	}
	serve.Flags().StringVar(&host, "host", "127.0.0.1", "loopback host")
	serve.Flags().StringVar(&port, "port", "0", "port; 0 picks a free port")
	cmd.AddCommand(serve, &cobra.Command{
		Use:   "stop",
		Short: "Explain how to stop the dashboard",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Stop the evolvectl ui serve process in the terminal that started it. There is no global PID file.")
			return nil
		},
	})
	return cmd
}

func copybaraCmd(f *flags) *cobra.Command {
	cmd := &cobra.Command{Use: "copybara", Short: "Inspect copy.bara.sky without migrating"}
	var path, workflow, file string
	explain := &cobra.Command{
		Use:     "explain",
		Short:   "Statically explain a Copybara config",
		Long:    "This does not run the copybara binary and does not migrate. Unresolved expressions stay unresolved.",
		Example: "  evolvectl copybara explain --config copy.bara.sky --workflow export-go-lib --file go/client/client.go",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.CopybaraExplain(f.opt(), path, workflow, file, f.format)
			return finish(code, err)
		},
	}
	explain.Flags().StringVar(&path, "config", "copy.bara.sky", "path to copy.bara.sky")
	explain.Flags().StringVar(&workflow, "workflow", "", "workflow name")
	explain.Flags().StringVar(&file, "file", "", "optional file to trace")
	list := &cobra.Command{
		Use:   "list",
		Short: "List workflow names in a config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.CopybaraExplain(f.opt(), path, "", "", "text")
			return finish(code, err)
		},
	}
	list.Flags().StringVar(&path, "config", "copy.bara.sky", "path to copy.bara.sky")
	var ref string
	var dry bool
	pin := &cobra.Command{
		Use:   "pin",
		Short: "Set the origin ref of one workflow in copy.bara.sky",
		Long: `Edits the ref of the workflow's origin in place and prints the diff. Everything else in the file is kept byte-for-byte.

The origin may be inline or a top-level name, and ref may be a string or a top-level name bound to a string; each is changed where it is defined. A missing ref is added. A computed ref is refused.
This does not run Copybara. Run copybara migrate yourself after reviewing the change.`,
		Example: "  evolvectl copybara pin --workflow default --ref v2.0.2\n  evolvectl copybara pin --config third_party/gax/copy.bara.sky --workflow import --ref 3c9b8f1 --dry-run",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.CopybaraPin(f.opt(), path, workflow, ref, dry)
			return finish(code, err)
		},
	}
	pin.Flags().StringVar(&path, "config", "copy.bara.sky", "path to copy.bara.sky")
	pin.Flags().StringVar(&workflow, "workflow", "", "workflow name")
	pin.Flags().StringVar(&ref, "ref", "", "commit, tag, or branch to pin")
	pin.Flags().BoolVar(&dry, "dry-run", false, "print the diff without writing")

	var io skyparse.InitOptions
	var initOut string
	var appendTo bool
	var replaces []string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write a Copybara workflow that imports one library directory",
		Long: `Writes a core.workflow that copies one directory of an upstream repository into one directory of yours.

destination_files is limited to --to, so the import never deletes anything else in the destination. With --license (the default) the upstream LICENSE file is copied next to the code.
Without --destination-url the workflow uses folder.destination(), which writes to a local folder and needs no destination repository.

Prints the config, or writes it with --out. --append adds the workflow to an existing config.`,
		Example: "  evolvectl copybara init --workflow import_gax_go --url https://github.com/googleapis/gax-go.git --ref v2.24.1 --from v2 --to third_party/gax-go/v2\n  evolvectl copybara init --workflow import_gax_go --url https://github.com/googleapis/gax-go.git --ref v2.24.1 --from v2 --to third_party/gax-go/v2 --out copy.bara.sky --append",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, r := range replaces {
				before, after, ok := strings.Cut(r, "=")
				if !ok || before == "" {
					return finish(exitcode.Invalid, fmt.Errorf("--replace takes before=after, got %q", r))
				}
				io.Replace = append(io.Replace, [2]string{before, after})
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.CopybaraInit(f.opt(), io, initOut, appendTo)
			return finish(code, err)
		},
	}
	initCmd.Flags().StringVar(&io.Name, "workflow", "", "workflow name, such as import_gax_go")
	initCmd.Flags().StringVar(&io.URL, "url", "", "upstream git URL")
	initCmd.Flags().StringVar(&io.Ref, "ref", "", "tag, branch, or commit to import")
	initCmd.Flags().StringVar(&io.From, "from", "", "directory in the upstream repository (default: the whole repository)")
	initCmd.Flags().StringVar(&io.To, "to", "", "directory in the destination, such as third_party/gax-go/v2")
	initCmd.Flags().StringSliceVar(&io.Exclude, "exclude", nil, "upstream globs to leave out, such as v2/**/*_test.go")
	initCmd.Flags().StringArrayVar(&replaces, "replace", nil, "literal text replacement before=after inside --to; repeatable")
	initCmd.Flags().StringVar(&io.DestinationURL, "destination-url", "", "git destination URL; empty uses folder.destination()")
	initCmd.Flags().BoolVar(&io.License, "license", true, "copy the upstream LICENSE next to the code")
	initCmd.Flags().StringVar(&initOut, "out", "", "write to this config file instead of printing")
	initCmd.Flags().BoolVar(&appendTo, "append", false, "add the workflow to an existing --out file")

	var previewRef, previewOut, against string
	preview := &cobra.Command{
		Use:   "preview",
		Short: "Show which files a workflow would write, without Copybara",
		Long: `Fetches the workflow's origin ref with git, selects origin_files, applies core.move and literal core.replace, and writes the result to .evolvectl/copybara/<workflow>/.

--against <dir> compares the result with a destination checkout and lists what the import would add, modify, and delete inside destination_files.
Transformations that are not plain moves or literal replaces are listed as not reproduced; for those, run copybara migrate --dry-run. No Java, no Copybara, nothing pushed.

Exit codes: 0 every step reproduced, 6 something needs a look (a step not reproduced, a no-op step, or a file outside destination_files).`,
		Example: "  evolvectl copybara preview --config copy.bara.sky --workflow import_gax_go\n  evolvectl copybara preview --workflow import_gax_go --ref v2.0.2 --against ../internal-otel-contrib-checkout",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.CopybaraPreview(context.Background(), f.opt(), path, workflow, previewRef, previewOut, against)
			return finish(code, err)
		},
	}
	preview.Flags().StringVar(&path, "config", "copy.bara.sky", "path to copy.bara.sky")
	preview.Flags().StringVar(&workflow, "workflow", "", "workflow name")
	preview.Flags().StringVar(&previewRef, "ref", "", "try another ref without editing the config")
	preview.Flags().StringVar(&previewOut, "out", "", "where to write the result tree (default .evolvectl/copybara/<workflow>)")
	preview.Flags().StringVar(&against, "against", "", "destination checkout to compare with")

	cmd.Short = "Write, preview, explain, and pin Copybara workflows without migrating"
	cmd.AddCommand(explain, list, pin, initCmd, preview)
	return cmd
}

func completionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: "completion [bash|zsh|fish|powershell]", Short: "Generate shell completion"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		shell := "bash"
		if len(args) == 1 {
			shell = args[0]
		}
		var err error
		switch shell {
		case "bash":
			err = root.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			err = root.GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			err = root.GenFishCompletion(cmd.OutOrStdout(), true)
		case "powershell":
			err = root.GenPowerShellCompletion(cmd.OutOrStdout())
		default:
			return finish(exitcode.Invalid, fmt.Errorf("unknown shell %s", shell))
		}
		if err != nil {
			return finish(exitcode.Generic, err)
		}
		return nil
	}
	cmd.ValidArgs = []string{"bash", "zsh", "fish", "powershell"}
	return cmd
}
