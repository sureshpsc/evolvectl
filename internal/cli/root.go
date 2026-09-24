// Package cli maps Cobra commands onto the app service.
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/evolvectl/evolvectl/internal/app"
	"github.com/evolvectl/evolvectl/internal/apperr"
	"github.com/evolvectl/evolvectl/internal/exitcode"
	"github.com/evolvectl/evolvectl/internal/session"
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
}

func (f flags) opt() app.Option {
	return app.Option{
		Workspace: f.workspace, ConfigPath: f.config, Provider: f.provider,
		NoAI: f.noAI, Offline: f.offline, Format: f.format, AllowDirty: f.allowDirty, DryRun: f.dryRun,
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
  1. evolvectl session export <provider>
  2. evolvectl scan
  3. evolvectl plan <target> --to <version>
  4. evolvectl upgrade <target> --to <version>
  5. evolvectl report --run <id> --format html

upgrade stores a styled HTML report at .evolvectl/runs/<id>/report.html, including dependency inventory, diffs, grouped failures, and before/after tests and coverage. --format html prints that page. --offline keeps go test from downloading modules.

AI is optional. --no-ai keeps the deterministic path. Copybara is one provider, not the campaign engine.

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
		upgradeCmd(&f),
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
		docsCmd(),
		benchmarkCmd(&f),
		doctorCmd(&f),
		uiCmd(&f),
		copybaraCmd(&f),
		completionCmd(root),
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
	var force, minimal bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create .evolvectl.yaml and local metadata directories",
		Long: `Create .evolvectl.yaml and .evolvectl/.

Does not select a provider for future terminals. Use evolvectl session export for that.

Exit codes: 0 written, 2 config already exists.`,
		Example: "  evolvectl init\n  evolvectl init --force --no-ai",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Init(f.opt(), force, minimal)
			return finish(code, err)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing config")
	cmd.Flags().BoolVar(&minimal, "minimal", false, "write a smaller ignore list")
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
	return &cobra.Command{
		Use:     "outdated",
		Short:   "List declared dependencies without claiming registry freshness",
		Long:    "Prints declared versions. Status stays registry_unavailable until a registry is queried. This build does not query registries during outdated.",
		Example: "  evolvectl outdated\n  evolvectl outdated --format json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Outdated(context.Background(), f.opt())
			return finish(code, err)
		},
	}
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

Exit codes: 0 required gates passed, 5 validation failed, 6 needs review, 7 policy, 9 dirty worktree, 10 interrupted.`,
		Example: "  evolvectl upgrade google.golang.org/grpc --to v1.75.0\n  evolvectl upgrade python:pydantic --to 2.11.0 --dry-run",
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

func docsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs [topic]",
		Short: "Print offline documentation",
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := ""
			if len(args) == 1 {
				topic = args[0]
			}
			a := application()
			a.Out = cmd.OutOrStdout()
			code, err := a.Docs(topic)
			return finish(code, err)
		},
	}
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
	cmd.AddCommand(explain, list)
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
