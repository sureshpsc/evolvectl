package guide

// Commands is the help page. Names match the Cobra command paths.
func Commands() []Command {
	return []Command{
		{
			Name: "version", Summary: "Print the evolvectl version.",
			Paragraphs: []string{"This talks to no repository and writes nothing."},
			Examples:   []Code{{Body: "evolvectl version"}},
		},
		{
			Name: "init", Summary: "Create `.evolvectl.yaml`, the local metadata directories, and this HTML guide.",
			Paragraphs: []string{
				"The guide is written to `.evolvectl/guide/` and `index.html` opens in your browser. Pass `--no-browser` to write the files and leave the browser alone.",
				"Init leaves `EVOLVECTL_WORKSPACE_PROVIDER` unset. Other terminals stay unset until you export a provider there. Pass `--provider` when you want that name stored in the new config file.",
			},
			Flags: []string{
				"`--force` — replace an existing config and rewrite the guide.",
				"`--minimal` — write a smaller ignore list.",
				"`--no-browser` — skip opening the guide.",
			},
			Examples: []Code{{
				Body: "evolvectl init\nevolvectl init --force --no-ai --no-browser",
				Note: "Exit 2 when the config already exists and `--force` is absent. `evolvectl help` reopens the guide without replacing the config.",
			}},
		},
		{
			Name: "scan", Summary: "Discover projects, manifests, and dependencies.",
			Paragraphs: []string{
				"Walks the workspace once and prints a dependency inventory. Scan modifies no source and starts no UI server.",
				"Versions are the ones declared in manifests. `registry_unavailable` means this command queried no registry.",
			},
			Flags: []string{"A single path argument sets the workspace, the same as `--workspace`."},
			Examples: []Code{{
				Body: "evolvectl scan --workspace examples/mixed-monorepo\nevolvectl scan --format html\nevolvectl scan --format json",
				Note: "`--format html` prints one offline inventory page on stdout.",
			}},
		},
		{
			Name: "graph", Summary: "Show the dependency and impact graph.",
			Flags: []string{
				"`--dependency` — limit the textual summary to one dependency.",
				"`--out` — write the JSON graph to this path.",
			},
			Examples: []Code{{Body: "evolvectl graph --format json\nevolvectl graph --dependency google.golang.org/grpc"}},
		},
		{
			Name: "outdated", Summary: "List declared dependencies, and with --online their latest versions and known vulnerabilities.",
			Paragraphs: []string{
				"Without `--online`, prints the versions in the manifests and leaves status as `registry_unavailable`.",
				"With `--online`, Go modules are resolved with `go list -m <module>@latest` through your GOPROXY, including the next major path such as `/v2`. Python, npm, and Maven use PyPI, the npm registry, and Maven Central. OSV is asked about the exact declared version. Ranges are reported as unresolved.",
			},
			Flags: []string{"`--online` — query registries and OSV."},
			Examples: []Code{{
				Body: "evolvectl outdated --online\nevolvectl outdated --online --format html > outdated.html",
				Note: "Exit 6 when a declared version has a known vulnerability.",
			}},
		},
		{
			Name: "impact", Summary: "List API differences between two versions and the call sites they affect.",
			Paragraphs: []string{
				"Downloads both versions, or uses a local replace directory, compares their exported declarations, and lists each file and line in the workspace that uses a removed or changed symbol. A symbol whose signature text is unchanged but mentions a changed type is reported as indirect.",
				"The comparison is not type-checked. Source is not modified. Go only.",
			},
			Flags: []string{
				"`--to` — target version.",
				"`--to-module` — new module path for a major-version move.",
				"`--use-dir` — compare with a copy of the new version already in the workspace, for example one Copybara imported.",
			},
			Examples: []Code{{
				Body: "evolvectl impact github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2 --workspace examples/go-gax-v2",
				Note: "Exit 6 when the impact could not be computed, for example offline with an empty module cache.",
			}},
		},
		{
			Name: "plan", Summary: "Plan an upgrade without modifying source.",
			Paragraphs: []string{
				"Resolves the current declaration, affected projects, matching recipes, and validators. The plan-only run is saved under `.evolvectl/runs`. Stages after plan are skipped.",
				"Pass a Go module path, or an ecosystem form such as `python:pydantic`, `maven:junit:junit`, or `node:left-pad`.",
			},
			Flags: []string{
				"`--dependency` — the same value as the positional argument.",
				"`--to` — target version.",
				"`--to-module` — new module path for a major-version move.",
				"`--use-dir` — the new version is already in the workspace at this folder.",
			},
			Examples: []Code{{
				Body: "evolvectl plan google.golang.org/grpc --to v1.75.0 --workspace examples/git-go-grpc\nevolvectl plan python:pydantic --to 2.11.0 --workspace examples/python-pydantic",
				Note: "Exit 2 when the dependency is missing or unknown.",
			}},
		},
		{
			Name: "upgrade", Summary: "Apply an upgrade, repair with recipes, validate, and write the report.",
			Paragraphs: []string{
				"Runs preflight, discover, assess, plan, prepare, apply, diagnose, repair, validate, and finalize. The original workspace is edited.",
				"`--dry-run` works in a temporary git worktree at HEAD when the workspace is clean, or a full copy otherwise (`execution.dry_run_copy`), and leaves the original source unchanged. Pass only one of `--dry-run` and `--apply`.",
				"Go tests run per module in the plan, up to `execution.validator_workers` at once, each limited by `validation.test_timeout` (default 10m).",
				"Ecosystems without a source recipe finish as needs-review (exit 6) when the manifest change is the whole edit. That outcome means a person still has to read the diff.",
			},
			Flags: []string{
				"`--dependency` / positional argument — module path or `ecosystem:name`.",
				"`--to` — target version.",
				"`--resume` — continue a cancelled run id.",
				"`--dry-run` — write only inside a temporary worktree or copy.",
				"`--test-timeout` — limit for one module's tests, such as `30m`. Overrides `validation.test_timeout`.",
				"`--test-cache` — let `go test` reuse cached results for unchanged packages instead of forcing `-count=1`.",
				"`--apply` — accepted for explicitness. Upgrade already writes unless `--dry-run` is set.",
				"`--to-module` — move to a new module path, such as a `/v2` major version. The go.mod require, every import, and go.sum are updated, then recipes for the new API run. Package names used in code are not renamed.",
				"`--use-dir` — use a copy of the new version that a sync tool such as Copybara already wrote into the workspace, for example `third_party/gax-go/v2`. go.mod gets a replace to it and nothing is downloaded. If `go mod tidy` raises the required version, the run lists it for review; the folder still decides the code.",
				"`--open-pr` — commit the changed files on a new branch, push, and open a pull request with `gh`, using the report as the body. Never force-pushes. Off by default.",
				"`--pr-base` — pull request base branch.",
			},
			Examples: []Code{{
				Body: "evolvectl upgrade google.golang.org/grpc --to v1.75.0 --no-ai --workspace examples/git-go-grpc\nevolvectl upgrade github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2 --workspace examples/go-gax-v2\nevolvectl upgrade python:pydantic --to 2.11.0 --dry-run --workspace examples/python-pydantic\nevolvectl upgrade maven:junit:junit --to 4.13.2 --workspace examples/java-maven-command-adapter",
				Note: "Exit 0 when required gates passed, 5 when validation failed, 6 when the run needs review, 9 when the worktree is dirty.",
			}},
		},
		{
			Name: "vendor", Summary: "Copy a module version into a third_party directory and point go.mod at it.",
			Paragraphs: []string{
				"Downloads the version, replaces the `--dir` directory with its files, writes `METADATA.evolvectl.json` with the module, version, go.sum hash, and license file, adds a replace directive to each go.mod, then validates like upgrade.",
				"A missing license file ends the run as needs-review. Rollback restores the previous directory. It does not write Google BUILD or METADATA files.",
			},
			Flags: []string{
				"`--to` — version to vendor.",
				"`--dir` — destination directory relative to the workspace.",
				"`--to-module` — new module path for a major-version move.",
				"`--dry-run`, `--open-pr`, `--pr-base` — as for upgrade.",
			},
			Examples: []Code{{Body: "evolvectl vendor github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2 --dir third_party/gax"}},
		},
		{
			Name: "batch", Summary: "Run every upgrade in a CSV or YAML sheet and write one combined report.",
			Paragraphs: []string{
				"Each row names a dependency, a target version, and optionally a new module, a workspace directory, and an owner. CSV headers such as `Target Version` and `Developer Owner` are recognised. YAML uses an `upgrades:` list.",
				"`dry-run` (the default) runs each row on its own temporary copy. `apply` edits the workspace row after row. `branches` runs each row on its own branch from the current commit, commits it there, and returns to the starting branch; it needs a clean worktree.",
				"The combined report is `.evolvectl/batches/<id>/batch.html`, with `batch.md` and `batch.json` beside it.",
			},
			Flags: []string{
				"`--file` — the sheet.",
				"`--mode` — `dry-run`, `apply`, or `branches`.",
				"`--open-pr` — with `branches`, push each committed branch and open a pull request.",
			},
			Examples: []Code{{
				Body: "evolvectl batch --file upgrades.csv\nevolvectl batch --file upgrades.yaml --mode branches --open-pr --pr-base main",
				Note: "Exit 0 when every row succeeded, 6 when some need review, 4 when some failed.",
			}},
		},
		{
			Name: "analyze", Summary: "Group diagnostics from a log or the latest run.",
			Flags:    []string{"`--log` — parser input. Go compiler output is the default parser."},
			Examples: []Code{{Body: "evolvectl analyze\nevolvectl analyze --log build.log"}},
		},
		{
			Name: "repair", Summary: "Remind you that repair already ran inside upgrade.",
			Paragraphs: []string{"Deterministic repair is a stage of `evolvectl upgrade`. This command applies no second edit. Re-run upgrade, or inspect the latest run with `explain`."},
			Examples:   []Code{{Body: "evolvectl repair"}},
		},
		{
			Name: "validate", Summary: "Show the validation gates recorded for a run.",
			Flags:    []string{"`--run` — run id. Default is the latest run."},
			Examples: []Code{{Body: "evolvectl validate --run <id>"}},
		},
		{
			Name: "explain", Summary: "Explain one recorded change.",
			Paragraphs: []string{"Pass a file path that the run edited, or a change id."},
			Flags: []string{
				"`--run` — run id.",
				"`--change` — change id such as `chg_…`.",
			},
			Examples: []Code{{Body: "evolvectl explain client/client.go\nevolvectl explain --change chg_123 --run <id>"}},
		},
		{
			Name: "diff", Summary: "Print the diffs recorded for a run.",
			Flags:    []string{"`--run` — run id."},
			Examples: []Code{{Body: "evolvectl diff --run <id>"}},
		},
		{
			Name: "history", Summary: "List local runs stored under `.evolvectl/runs`.",
			Examples: []Code{{Body: "evolvectl history"}},
		},
		{
			Name: "rollback", Summary: "Restore files snapshotted by a run.",
			Paragraphs: []string{"Each snapshotted file is restored when its current bytes still match the campaign. Files the run created, such as a new `go.sum`, are removed."},
			Flags:      []string{"`--run` — run id. Default is the latest run."},
			Examples:   []Code{{Body: "evolvectl rollback --run <id>"}},
		},
		{
			Name: "report", Summary: "Render a saved run as text, JSON, Markdown, or HTML.",
			Paragraphs: []string{"HTML is a single offline file. It renders the saved snapshot and rescans nothing. `--run` is required."},
			Flags: []string{
				"`--run` — run id.",
				"`--output` — write the report to this path.",
				"`--format` — `text`, `json`, `md`, or `html`.",
			},
			Examples: []Code{{
				Body: "evolvectl report --run <id> --format json\nevolvectl report --run <id> --format html --output report.html",
			}},
		},
		{
			Name: "recipe", Summary: "Inspect and test migration recipes.",
			Paragraphs: []string{"The subcommands list the bundled recipes, print a template, and run golden files. See the Recipes page for the bundled ids."},
			Examples:   []Code{{Body: "evolvectl recipe list"}},
		},
		{
			Name: "recipe list", Summary: "Print each recipe id and title.",
			Examples: []Code{{Body: "evolvectl recipe list"}},
		},
		{
			Name: "recipe scaffold", Summary: "Print a recipe template with a call rename and no shell transform.",
			Examples: []Code{{Body: "evolvectl recipe scaffold"}},
		},
		{
			Name: "recipe test", Summary: "Run recipe golden files.",
			Paragraphs: []string{"The default directory is `recipes`. One argument selects another directory. Input and expected files live in `testdata` beside `recipe.yaml`."},
			Examples:   []Code{{Body: "evolvectl recipe test ./recipes"}},
		},
		{
			Name: "adapters", Summary: "Show adapter capability levels.",
			Paragraphs: []string{"Prints the built-in matrix (Go semantic, Python structural, Maven and Node manifest-only, git status, filesystem discovery, Copybara explain, command validate). YAML files under `.evolvectl/adapters` are listed after the matrix."},
			Examples:   []Code{{Body: "evolvectl adapters\nevolvectl adapters --format json"}},
		},
		{
			Name: "provider", Summary: "List built-in providers.",
			Paragraphs: []string{"The parent command groups `provider list`. The built-in names are filesystem, git, copybara, and command."},
			Examples:   []Code{{Body: "evolvectl provider list"}},
		},
		{
			Name: "provider list", Summary: "Print the built-in provider names and the operation each one owns.",
			Examples: []Code{{Body: "evolvectl provider list"}},
		},
		{
			Name: "session", Summary: "Terminal-scoped provider selection.",
			Paragraphs: []string{"Session commands print shell text. They write no repository config. A new terminal has no selection until you export again. See the Copybara page for the precedence rules."},
			Examples:   []Code{{Body: "evolvectl session current"}},
		},
		{
			Name: "session current", Summary: "Print the provider visible in this process.",
			Paragraphs: []string{"Prints `workspace=<unset>` when `EVOLVECTL_WORKSPACE_PROVIDER` is empty."},
			Examples:   []Code{{Body: "evolvectl session current"}},
		},
		{
			Name: "session use", Summary: "Print how to select a provider in this terminal.",
			Paragraphs: []string{"The provider name is required. The hint matches the OS shell: PowerShell on Windows, bash elsewhere."},
			Examples:   []Code{{Body: "evolvectl session use git"}},
		},
		{
			Name: "session clear", Summary: "Print a shell snippet that clears the selection.",
			Flags:    []string{"`--shell` — `bash`, `zsh`, `fish`, or `powershell`."},
			Examples: []Code{{Body: "evolvectl session clear --shell powershell"}},
		},
		{
			Name: "session export", Summary: "Print a shell assignment for the current terminal.",
			Paragraphs: []string{"Eval it in bash or zsh, or paste it into PowerShell. The parent shell changes only after you do that."},
			Flags:      []string{"`--shell` — `bash`, `zsh`, `fish`, or `powershell`."},
			Examples: []Code{{
				Body: "evolvectl session export git --shell powershell\neval \"$(evolvectl session export git)\"",
			}},
		},
		{
			Name: "docs", Summary: "Print a short topic, or open that topic as an HTML page.",
			Paragraphs: []string{
				"Text topics: quickstart, providers, recipes, copybara, languages, examples, troubleshooting, ui, session.",
				"With `--format html`, the matching guide page is written under `.evolvectl/guide` and opened. `--no-browser` writes the page and prints the path.",
			},
			Examples: []Code{{
				Body: "evolvectl docs copybara\nevolvectl docs languages --format html\nevolvectl docs --format html --no-browser",
			}},
		},
		{
			Name: "benchmark", Summary: "Time a local operation.",
			Paragraphs: []string{"The only subcommand today times discovery."},
			Examples:   []Code{{Body: "evolvectl benchmark scan"}},
		},
		{
			Name: "benchmark scan", Summary: "Time a workspace scan.",
			Paragraphs: []string{"One optional path sets the workspace."},
			Examples:   []Code{{Body: "evolvectl benchmark scan examples/mixed-monorepo"}},
		},
		{
			Name: "doctor", Summary: "Check config and external tools.",
			Paragraphs: []string{
				"Prints config, the provider and where it came from, whether AI is enabled, and whether git, go, python, and the configured Copybara binary are on PATH.",
				"A missing optional tool is `UNAVAILABLE`. Doctor still exits 0. Copybara is optional unless you select that provider.",
			},
			Examples: []Code{{Body: "evolvectl doctor"}},
		},
		{
			Name: "ui", Summary: "Local read-only dashboard for saved runs.",
			Paragraphs: []string{"`ui serve` is the server. `ui stop` explains how to stop it. There is no global PID file."},
			Examples:   []Code{{Body: "evolvectl ui serve --host 127.0.0.1 --port 0"}},
		},
		{
			Name: "ui serve", Summary: "Serve saved runs on loopback.",
			Paragraphs: []string{"Binds `127.0.0.1` by default and refuses other hosts. Reads runs already under `.evolvectl`. Modifies no source. Stop the process in the terminal that started it."},
			Flags: []string{
				"`--host` — loopback host. Default `127.0.0.1`.",
				"`--port` — port. `0` picks a free port.",
			},
			Examples: []Code{{Body: "evolvectl ui serve --host 127.0.0.1 --port 0"}},
		},
		{
			Name: "ui stop", Summary: "Explain how to stop the dashboard.",
			Paragraphs: []string{"Stop the `evolvectl ui serve` process in the terminal that started it."},
			Examples:   []Code{{Body: "evolvectl ui stop"}},
		},
		{
			Name: "copybara", Summary: "Inspect `copy.bara.sky` without migrating.",
			Paragraphs: []string{"Subcommands write a starter workflow, preview its output, explain or list workflows, and pin a ref. None of them runs the Copybara binary or migrates."},
			Examples:   []Code{{Body: "evolvectl copybara list --config examples/mixed-monorepo/copy.bara.sky"}},
		},
		{
			Name: "copybara explain", Summary: "Statically explain a Copybara config.",
			Paragraphs: []string{
				"Literal globs and `core.move` / `core.replace` can be traced. Unresolved expressions stay unresolved. Top-level `name = \"literal\"` assignments are substituted. Starlark is not executed.",
			},
			Flags: []string{
				"`--config` — path to `copy.bara.sky`. Default `copy.bara.sky`.",
				"`--workflow` — workflow name.",
				"`--file` — optional file to trace through the workflow.",
			},
			Examples: []Code{{
				Body: "evolvectl copybara explain --config examples/mixed-monorepo/copy.bara.sky --workflow export-go-lib --file go/client/client.go",
			}},
		},
		{
			Name: "copybara list", Summary: "List workflow names in a config.",
			Flags:    []string{"`--config` — path to `copy.bara.sky`. Default `copy.bara.sky`."},
			Examples: []Code{{Body: "evolvectl copybara list --config examples/mixed-monorepo/copy.bara.sky"}},
		},
		{
			Name: "copybara pin", Summary: "Set the origin ref of one workflow in copy.bara.sky.",
			Paragraphs: []string{
				"Edits the ref in place and prints the diff; the rest of the file is kept byte-for-byte. The origin can be inline or a top-level name, and ref can be a string or a top-level name bound to a string. A missing ref is added and a computed ref is refused.",
				"Copybara is not run. Run `copybara migrate` yourself after reviewing the change.",
			},
			Flags: []string{
				"`--config` — path to `copy.bara.sky`.",
				"`--workflow` — workflow name.",
				"`--ref` — commit, tag, or branch.",
				"`--dry-run` — print the diff without writing.",
			},
			Examples: []Code{{Body: "evolvectl copybara pin --config examples/mixed-monorepo/copy.bara.sky --workflow export-go-lib --ref v1.2.0 --dry-run"}},
		},
		{
			Name: "copybara init", Summary: "Write a starter import workflow.",
			Paragraphs: []string{
				"Generates one `core.workflow` that copies `--from` in the origin to `--to` in the destination, with `destination_files` limited to `--to`. Without `--destination-url` it uses `folder.destination()`. The config is printed unless `--out` is given; `--append` adds the workflow to an existing file and refuses a duplicate name.",
			},
			Flags: []string{
				"`--workflow`, `--url`, `--ref` — workflow name, origin repository, and ref.",
				"`--from`, `--to` — origin folder and destination folder.",
				"`--exclude` — origin globs to leave out. `--replace before=after` — literal text replacement.",
				"`--destination-url` — git destination. `--license` — also copy the top-level LICENSE (default true).",
				"`--out`, `--append` — write to a file, or append to it.",
			},
			Examples: []Code{{Body: "evolvectl copybara init --workflow import_gax_go --url <gax-go git URL> --ref v2.24.1 --from v2 --to third_party/gax-go/v2"}},
		},
		{
			Name: "copybara preview", Summary: "Show the files a workflow would write, without Copybara.",
			Paragraphs: []string{
				"Fetches the origin ref with git, applies `origin_files`, `core.move`, and literal `core.replace`, and writes the result under `.evolvectl/copybara/<workflow>`. Other transformations are listed as unsupported and the preview is marked incomplete. `--against` compares with a destination checkout inside `destination_files`.",
				"Exit code 6 means the preview is incomplete, has warnings, or writes files outside `destination_files`.",
			},
			Flags: []string{
				"`--config`, `--workflow` — config and workflow.",
				"`--ref` — use this ref instead of the one in the config.",
				"`--out` — output folder. `--against` — destination checkout to compare with.",
			},
			Examples: []Code{{Body: "evolvectl copybara preview --config copy.bara.sky --workflow import_gax_go --ref v2.0.2"}},
		},
		{
			Name: "completion", Summary: "Generate a shell completion script.",
			Paragraphs: []string{"The argument is `bash`, `zsh`, `fish`, or `powershell`. The default is bash."},
			Examples:   []Code{{Body: "evolvectl completion powershell"}},
		},
		{
			Name: "help", Summary: "Open this command page, or print terminal help for one command.",
			Paragraphs: []string{
				"With no arguments, rewrite `.evolvectl/guide` and open `help.html`. `--no-browser` writes the files and prints the path.",
				"With a command name, print that command's terminal help instead of opening the browser. `evolvectl --help` is the short terminal summary of the whole tool.",
			},
			Flags: []string{"`--no-browser` — write the guide and skip the browser."},
			Examples: []Code{{
				Body: "evolvectl help\nevolvectl help upgrade\nevolvectl help copybara explain\nevolvectl help --no-browser",
			}},
		},
	}
}
