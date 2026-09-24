package guide

// Pages is the offline guide opened by evolvectl init.
func Pages() []Page {
	pages := []Page{
		startPage(),
		copybaraPage(),
		languagesPage(),
		howPage(),
		helpPage(),
		recipesPage(),
		examplesPage(),
		troublePage(),
	}
	return pages
}

func startPage() Page {
	return Page{
		ID: "index", File: "index.html", Nav: "Start",
		Kicker: "Start here",
		Title:  "What evolvectl is for",
		Lead:   "Evolvectl upgrades the code in a repository when a dependency version changes. It finds manifests, plans the affected projects, applies deterministic recipes, runs the checks you already have, and writes one report.",
		Sections: []Section{
			{
				ID: "jobs", Title: "Two different jobs",
				Paragraphs: []string{
					"Copybara and evolvectl answer different questions. Copybara copies and transforms files from an origin repository into a destination repository. The workflow lives in `copy.bara.sky`, and `copybara migrate` can open a review on a remote destination.",
					"Evolvectl stays in the checkout you name. You tell it a dependency and a target version. It edits that checkout so the code matches the new version, then records what changed and which tests moved.",
					"A repository can use both. Evolvectl can read a Copybara config so you can see the workflow. The upgrade command leaves `copybara migrate` for you to run yourself.",
				},
				Table: &Table{
					Caption: "What each tool is responsible for",
					Headers: []string{"Question", "Copybara", "Evolvectl"},
					Rows: [][]string{
						{"Where does the work happen?", "Between an origin repo and a destination repo", "Inside the workspace you pass to the command"},
						{"What is the unit of work?", "A workflow in `copy.bara.sky`", "One dependency moved to one target version"},
						{"What changes the bytes?", "Starlark transforms when you run the Copybara binary", "Manifest edits and YAML recipes (Go AST, Python structure)"},
						{"What proves the result?", "The destination review you asked Copybara to create", "The same tests and coverage, run before the edit and after it"},
						{"How do they meet?", "You keep running Copybara when you want a migration", "`copybara explain` reads the config. `upgrade` leaves migrate alone"},
					},
				},
			},
			{
				ID: "next", Title: "Read this next",
				Paragraphs: []string{"The rest of this guide is the same material, split so a new checkout is easier to learn."},
				Links: []Link{
					{Href: "copybara.html", Title: "Copybara and other tools", Text: "How a provider, a language adapter, and a command adapter fit together."},
					{Href: "languages.html", Title: "Languages", Text: "Go, Python, Maven, Node, Bazel, and what each one can actually repair."},
					{Href: "how-it-works.html", Title: "How a run works", Text: "The ten stages, dry-run, offline mode, and the HTML report."},
					{Href: "help.html", Title: "Every command", Text: "What it does, which flags matter, and a copyable example."},
				},
			},
			{
				ID: "first", Title: "First commands",
				Paragraphs: []string{
					"`init` writes `.evolvectl.yaml` and this guide, then opens this page. It leaves the provider for this terminal unset. A later terminal stays unset until you export a provider there.",
					"AI stays off unless you enable it in config. `--no-ai` keeps the deterministic path on that command.",
				},
				Codes: []Code{{
					Title: "Golden path",
					Body: `evolvectl init
evolvectl scan --workspace examples/mixed-monorepo
evolvectl plan google.golang.org/grpc --to v1.75.0 --workspace examples/git-go-grpc
evolvectl upgrade google.golang.org/grpc --to v1.75.0 --no-ai --workspace examples/git-go-grpc
evolvectl report --run <id> --format html --output report.html`,
					Note: "`upgrade` writes the workspace. Add `--dry-run` to apply the same steps in a temporary copy.",
				}},
			},
		},
	}
}

func copybaraPage() Page {
	return Page{
		ID: "copybara", File: "copybara.html", Nav: "Copybara",
		Kicker: "Tools",
		Title:  "Copybara, and any tool in that role",
		Lead:   "The campaign is the same for every workspace. A provider tells evolvectl where the code lives. A language adapter knows how to read and repair one ecosystem. A command adapter names the program that proves the change.",
		Sections: []Section{
			{
				ID: "copybara-file", Title: "What evolvectl does with copy.bara.sky",
				Paragraphs: []string{
					"`evolvectl copybara explain` reads the file as text. It substitutes top-level assignments of the form `name = \"literal\"`. It traces literal globs and `core.move` / `core.replace`. Other calls stay unresolved. Credential-shaped URLs are redacted in the explanation.",
					"The reader executes no Starlark. If the `copybara` binary is missing, explain still prints the static view and says the tool is unavailable.",
					"During discover, a campaign that sees `copy.bara.sky` stores that static view on the run report. You can open it in the HTML report under Copybara.",
				},
				Callout: "Selecting the copybara provider requires the Copybara binary on PATH. Preflight then stops with COPYBARA_NOT_FOUND when the binary is missing, and tells you to install it or `evolvectl session export git`. With the binary present, upgrade still leaves `copybara migrate` for you to run, because that command can create a review on a remote destination.",
				Codes: []Code{{
					Title: "Explain one workflow and one file",
					Body:  "evolvectl copybara explain --config examples/mixed-monorepo/copy.bara.sky --workflow export-go-lib --file go/client/client.go",
					Note:  "In that fixture, `go/client/client.go` is included, moved to `third_party/go/client/client.go`, and left alone by the BUILD-only `core.replace`. `go/vendor/x.go` is excluded.",
				}},
			},
			{
				ID: "providers", Title: "Providers",
				Paragraphs: []string{
					"Precedence for the provider on one command is: `--provider`, then the environment variable `EVOLVECTL_WORKSPACE_PROVIDER`, then `workspace.type` in `.evolvectl.yaml`, then auto.",
					"Auto picks git when the workspace has a `.git` directory, and filesystem otherwise.",
					"`init` can store a provider in the config file when you pass `--provider`. It still leaves other terminals alone. Session export is how you select a provider for the terminal you are in.",
				},
				Table: &Table{
					Caption: "Built-in providers",
					Headers: []string{"Name", "Role"},
					Rows: [][]string{
						{"`filesystem`", "Read the directory. Always available."},
						{"`git`", "Read `git status` before an edit. A dirty worktree blocks `upgrade` until you commit, stash, or pass `--allow-dirty`. Dry-run allows a dirty tree."},
						{"`copybara`", "Require the Copybara binary, and explain `copy.bara.sky`. Migration stays a Copybara command."},
						{"`command`", "Run the validator argv you configured. The exit code is the proof. The core has no claim that it understood the language."},
					},
				},
			},
			{
				ID: "session", Title: "One terminal at a time",
				Paragraphs: []string{
					"A child process can print a shell assignment. It can change the parent shell only if you eval or paste that assignment yourself. `session export` prints the line and writes nothing to the repository.",
					"A new terminal has no selection until you export again.",
				},
				Codes: []Code{
					{
						Title: "PowerShell",
						Body: `evolvectl session export git --shell powershell
# paste the printed line into this same terminal:
# $env:EVOLVECTL_WORKSPACE_PROVIDER='git'`,
					},
					{
						Title: "bash or zsh",
						Body: `eval "$(evolvectl session export git)"
evolvectl session current`,
					},
				},
			},
			{
				ID: "plugins", Title: "A tool evolvectl has never heard of",
				Paragraphs: []string{
					"Two extension points cover a tool that is not built in.",
					"A command adapter is a YAML file under `.evolvectl/adapters`. It names an argv. Evolvectl runs that argv and records the exit code. Coverage is recorded only when the tool printed it. The adapter invents no coverage number.",
					"An executable adapter is a program named `evolvectl-adapter-*`. It speaks a JSON handshake on stdin and stdout. The core records the capabilities the program advertises. The core claims semantic repair for that program only when the program itself does.",
				},
				Links: []Link{
					{Href: "languages.html", Title: "Language matrix", Text: "What Go, Python, Maven, Node, and Bazel can do today."},
					{Href: "examples.html", Title: "Fixtures", Text: "A Copybara config, a Maven command adapter, and a handshake plugin."},
				},
			},
		},
	}
}

func languagesPage() Page {
	return Page{
		ID: "languages", File: "languages.html", Nav: "Languages",
		Kicker: "Ecosystems",
		Title:  "What each language can do",
		Lead:   "Discovery is broad. Repair is specific. A manifest edit and a green delegated command are real results, and they are a different result from a recipe that rewrote the source.",
		Sections: []Section{
			{
				ID: "matrix", Title: "Capability matrix",
				Paragraphs: []string{"`evolvectl adapters` prints this same matrix for the workspace. A delegated command means that program ran. The repair column says whether evolvectl also rewrote source."},
				Table: &Table{
					Headers: []string{"Ecosystem", "Discovers", "Edits", "Repairs source", "Proves it"},
					Rows: [][]string{
						{"Go", "`go.mod`, `go.work` `use` entries", "Module version, including `go get` when you are online", "Go AST recipes", "`go test` when `go` is installed"},
						{"Python", "`pyproject.toml`, `requirements*.txt`", "Declared versions", "Structural recipes (imports, decorators, methods, keywords). Quoted strings stay quoted.", "pytest when test files and pytest exist. coverage.py when it is installed. The pydantic example's required gate is a delimiter balance check."},
						{"Java / Maven", "`pom.xml`", "Version in the POM", "Unavailable. The run finishes as needs-review.", "The command you configured. The Maven example delegates to `go run ./tools`."},
						{"Node", "`package.json`", "Version in package.json", "Unavailable. A successful manifest edit still needs review.", "Only when a command adapter defines one."},
						{"Bazel", "`WORKSPACE`, `MODULE.bazel`, `BUILD`", "Recorded on the inventory", "Unavailable from the core", "Only when you configure that command. A skipped optional gate is recorded as skipped."},
						{"Copybara", "`copy.bara.sky`", "Static explanation", "The explanation traces a few literal transforms", "You run `copybara migrate` yourself when you want a migration."},
					},
				},
			},
			{
				ID: "go", Title: "Go",
				Paragraphs: []string{
					"Recipes address imports, calls, call arguments, and struct fields. The gRPC recipes rewrite `DialContext` to `NewClient` and rename a server config field. Golden files live beside each recipe.",
					"Online, a Go upgrade that is a real module (a local `replace` is the exception) runs `go get <module>@<version>` so `go.sum` matches the bump.",
					"`--offline` keeps `go test` from using a module proxy. The same flag refuses to continue when `go.sum` lacks the target version and the requirement is a downloaded module. A dry-run stops with a named error when a `replace` points outside the workspace.",
				},
				Codes: []Code{{
					Body: "evolvectl upgrade google.golang.org/grpc --to v1.75.0 --no-ai --workspace examples/git-go-grpc",
				}},
			},
			{
				ID: "python", Title: "Python",
				Paragraphs: []string{
					"The bundled pydantic recipes rename the import, `@validator`, `.dict()`, and the `orm_mode` keyword. A quoted `orm_mode=True` string is left unchanged.",
					"Name the dependency as `python:<distribution>`.",
				},
				Codes: []Code{{
					Body: "evolvectl upgrade python:pydantic --to 2.11.0 --dry-run --workspace examples/python-pydantic",
				}},
			},
			{
				ID: "others", Title: "Maven, Node, and Bazel",
				Paragraphs: []string{
					"Maven dependencies are named `maven:group:artifact`. Node dependencies are named `node:<name>`. Both can change the version in the manifest. Semantic repair for Java and JavaScript is unavailable, so the outcome is needs-review when the source still has to be understood by a person.",
					"Bazel files are recorded so a polyglot repo shows up in `scan`. Evolvectl runs `bazel test` only when that command is configured.",
				},
				Codes: []Code{
					{Title: "Maven", Body: "evolvectl upgrade maven:junit:junit --to 4.13.2 --workspace examples/java-maven-command-adapter"},
					{Title: "Node", Body: "evolvectl upgrade node:left-pad --to 1.3.1 --workspace examples/node-pnpm-command-adapter"},
				},
			},
		},
	}
}

func howPage() Page {
	return Page{
		ID: "how", File: "how-it-works.html", Nav: "How it works",
		Kicker: "Campaign",
		Title:  "How one upgrade runs",
		Lead:   "A campaign is one dependency, one target version, and one directory of artifacts under `.evolvectl/runs/<id>/`. The directory holds `report.json`, `report.html`, `events.jsonl`, and Go coverage profiles when a tool wrote them.",
		Sections: []Section{
			{
				ID: "stages", Title: "Stages",
				Paragraphs: []string{"`plan` saves a run and skips every stage after plan, because a plan writes no source. `upgrade` runs the full list. A cancelled run can be continued with `upgrade --resume <id>`."},
				Table: &Table{
					Headers: []string{"Stage", "What it does"},
					Rows: [][]string{
						{"`preflight`", "Choose git or filesystem when the provider is auto. Record git HEAD. Block a dirty worktree unless `--allow-dirty` or `--dry-run`. When the provider is copybara, require the Copybara binary."},
						{"`discover`", "Walk the workspace once. Record projects, manifests, dependencies, Bazel files, and any `copy.bara.sky`."},
						{"`assess`", "Mark declared versions as `registry_unavailable` when no registry was queried. The report keeps the version written in the manifest."},
						{"`plan`", "Resolve the dependency, affected projects, matching recipes, and validators. Store a hash of the planned files."},
						{"`prepare`", "On `--dry-run`, copy the workspace to a temp directory first. Then run the existing tests and coverage before any edit."},
						{"`apply`", "Stop if a planned file changed after the plan (`stale plan hash`). Otherwise edit manifests and apply recipes. Snapshot the old bytes."},
						{"`diagnose`", "Group compiler and test diagnostics."},
						{"`repair`", "Apply deterministic recipe repairs inside the same upgrade. The `repair` command itself edits nothing."},
						{"`validate`", "Run the same commands again. Newly failed tests are listed apart from tests that were already failing."},
						{"`finalize`", "Write the report and the next-step hint."},
					},
				},
			},
			{
				ID: "quality", Title: "Tests and coverage",
				Paragraphs: []string{
					"Prepare and validate run the same commands. The report shows passed and failed counts before and after, plus three lists: newly failed, still failing, and fixed since the baseline.",
					"Coverage numbers are recorded only when the tool printed them. A command adapter that exits 0 has run. It has a coverage percent only when its output included one.",
				},
			},
			{
				ID: "safety", Title: "Dry-run, rollback, offline, AI",
				Bullets: []string{
					"`--dry-run` applies the campaign in a temporary copy. The original source stays as it was. A `replace` that points outside the workspace stops the dry-run with a named error.",
					"`rollback --run <id>` restores each snapshotted file when its current bytes still match the campaign. Files the run created, such as a new `go.sum`, are removed.",
					"`--offline` keeps `go test` off the module proxy, and refuses a Go module bump whose target is missing from `go.sum`.",
					"`--no-ai` forces the deterministic path for that command. AI in `.evolvectl.yaml` stays disabled unless you enable it.",
					"`--allow-dirty` lets upgrade continue when `git status` is not clean.",
				},
			},
			{
				ID: "reports", Title: "Reports and the local UI",
				Paragraphs: []string{
					"Text is the default. `--format json`, `--format md`, and `--format html` render the same snapshot. HTML is one file with no remote assets. `scan --format html` renders the dependency inventory. `plan --format html` and `upgrade --format html` render the run.",
					"`evolvectl ui serve` binds `127.0.0.1` only, serves runs already stored under `.evolvectl`, and modifies no source. Stop it by stopping that process. Plain scan, plan, and upgrade start no server.",
					"This guide is a third HTML surface. It describes the tool. The run report describes one campaign.",
				},
				Codes: []Code{{
					Body: `evolvectl ui serve --host 127.0.0.1 --port 0
evolvectl report --run <id> --format html --output report.html`,
				}},
			},
		},
	}
}

func helpPage() Page {
	return Page{
		ID: "help", File: "help.html", Nav: "Commands",
		Kicker: "Reference",
		Title:  "Commands",
		Lead:   "Every command, what it changes, and an example. Shared flags work on all of them. Filter the list if you already know the name.",
		Sections: []Section{
			{
				ID: "shared", Title: "Flags on every command",
				Bullets: []string{
					"`--workspace` — repository root. Default is the current directory.",
					"`--config` — config file. Default is `.evolvectl.yaml`.",
					"`--provider` — provider for this command only. Overrides `EVOLVECTL_WORKSPACE_PROVIDER`.",
					"`--no-ai` — deterministic path for this command.",
					"`--offline` — skip package registries and the module proxy.",
					"`--format` — `text` (default), `json`, `md`, or `html` where that command renders a report.",
					"`--allow-dirty` — continue when the git worktree has uncommitted changes.",
				},
			},
			{
				ID: "exit", Title: "Exit codes",
				Table: &Table{
					Headers: []string{"Code", "Meaning"},
					Rows: [][]string{
						{"0", "Success. Required gates passed, or the command only printed information."},
						{"1", "Generic failure."},
						{"2", "Bad arguments, unknown dependency, or config already exists."},
						{"3", "Discovery failed."},
						{"4", "Upgrade failed."},
						{"5", "Validation failed."},
						{"6", "Needs review. The tool finished and a person still has to look. Maven and Node land here when source repair is unavailable."},
						{"7", "Policy refused the run."},
						{"8", "A required tool is missing."},
						{"9", "Dirty worktree."},
						{"10", "Interrupted."},
					},
				},
			},
		},
		Commands: Commands(),
	}
}

func recipesPage() Page {
	return Page{
		ID: "recipes", File: "recipes.html", Nav: "Recipes",
		Kicker: "Transforms",
		Title:  "Recipes",
		Lead:   "A recipe is a YAML file with a match and a transform. The bundled set is what `upgrade` applies when the dependency and the version range match.",
		Sections: []Section{
			{
				ID: "shape", Title: "Shape",
				Paragraphs: []string{
					"The kind is `MigrationRecipe`, apiVersion `evolvectl.dev/v1alpha1`. Shell transforms and exec transforms are rejected.",
					"Golden files live in `testdata/input` and `testdata/expected` next to `recipe.yaml`. `evolvectl recipe test` runs them.",
				},
				Codes: []Code{{
					Title: "Scaffold and test",
					Body: `evolvectl recipe scaffold
evolvectl recipe list
evolvectl recipe test ./recipes`,
				}},
			},
			{
				ID: "bundled", Title: "Bundled recipes",
				Table: &Table{
					Headers: []string{"ID", "What it changes"},
					Rows: [][]string{
						{"`go.import.path-replace`", "A Go import path"},
						{"`go.call.rename`", "A Go function call"},
						{"`go.call.insert-arg`", "Insert a call argument"},
						{"`go.call.remove-arg`", "Remove a call argument"},
						{"`go.struct.field-rename`", "A struct field"},
						{"`go.grpc.dialcontext-to-newclient`", "`grpc.DialContext` becomes `grpc.NewClient`"},
						{"`go.grpc.serverconfig-field`", "A gRPC server config field"},
						{"`python.import.path-rename`", "`pydantic.v1` imports move onto `pydantic`"},
						{"`python.decorator.validator`", "`@validator` becomes `@field_validator`"},
						{"`python.method.dict`", "`.dict()` becomes `.model_dump()`"},
						{"`python.keyword.orm-mode`", "`orm_mode` becomes `from_attributes`"},
					},
				},
			},
		},
	}
}

func examplesPage() Page {
	return Page{
		ID: "examples", File: "examples.html", Nav: "Examples",
		Kicker: "Fixtures",
		Title:  "Examples in this repository",
		Lead:   "Each directory is a small workspace you can point `--workspace` at. They show one capability each.",
		Sections: []Section{
			{
				ID: "list", Title: "What to run",
				Table: &Table{
					Headers: []string{"Directory", "Shows"},
					Rows: [][]string{
						{"`examples/git-go-grpc`", "Go AST recipes for gRPC, with a local replace so the upgrade can run offline. `go test ./...` is the required gate."},
						{"`examples/python-pydantic`", "Python structural recipes. The required gate is delimiter balance."},
						{"`examples/java-maven-command-adapter`", "A `pom.xml` version edit and a delegated test command. Java semantic repair is unavailable."},
						{"`examples/node-pnpm-command-adapter`", "A `package.json` version edit. The run still needs review."},
						{"`examples/mixed-monorepo`", "Discovery across Go, Python, Maven, Node, Bazel, and `copy.bara.sky` in one tree."},
						{"`examples/copybara-go`", "The static explain command against that Copybara fixture."},
						{"`examples/bazel-polyglot`", "A pointer at the Bazel markers. Bazel tests run only when configured."},
						{"`examples/custom-tool-plugin`", "An `evolvectl-adapter-*` program that answers the JSON handshake."},
					},
				},
				Codes: []Code{{
					Title: "Scan the mixed tree, then explain the Copybara workflow",
					Body: `evolvectl scan --workspace examples/mixed-monorepo
evolvectl copybara explain --config examples/mixed-monorepo/copy.bara.sky --workflow export-go-lib --file go/client/client.go
go run ./examples/custom-tool-plugin`,
				}},
			},
		},
	}
}

func troublePage() Page {
	return Page{
		ID: "trouble", File: "troubleshooting.html", Nav: "Troubleshooting",
		Kicker: "When a run stops",
		Title:  "Troubleshooting",
		Lead:   "The exit code and the next-step line are the diagnosis. The HTML report repeats them next to the stage that stopped.",
		Sections: []Section{
			{
				ID: "cases", Title: "Messages you will actually see",
				Bullets: []string{
					"`config already exists` — `init` found `.evolvectl.yaml`. Pass `--force` to replace it, or run `evolvectl help` to reopen this guide.",
					"`workspace is dirty` — commit, stash, or pass `--allow-dirty`. Dry-run allows a dirty tree.",
					"`COPYBARA_NOT_FOUND` — the selected provider is copybara and the binary is missing. Install it, or `evolvectl session export git`. `copybara explain` still works without the binary.",
					"`stale plan hash` — a planned file changed after the plan was saved. Run `plan` again.",
					"`registry_unavailable` — this run queried no package registry. It is a statement that the declared version was left as declared.",
					"Exit 6 — the campaign finished and source repair for that ecosystem is unavailable, or another review point was recorded. Read the report before treating the tree as done.",
					"A missing optional tool in `doctor` prints `UNAVAILABLE`. Doctor itself still exits 0.",
				},
			},
			{
				ID: "look", Title: "Where to look",
				Codes: []Code{{
					Body: `evolvectl doctor
evolvectl history
evolvectl explain client/client.go --run <id>
evolvectl diff --run <id>
evolvectl rollback --run <id>
evolvectl validate --run <id>`,
				}},
				Links: []Link{
					{Href: "help.html", Title: "Command reference", Text: "Flags and examples for each of those commands."},
					{Href: "how-it-works.html", Title: "Stages", Text: "Which stage produced the error."},
				},
			},
		},
	}
}
