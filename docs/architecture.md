# Architecture

The CLI is Cobra. Commands call `internal/app`, which calls the campaign executor. The executor moves a run through preflight, discover, assess, plan, prepare, apply, diagnose, repair, validate, and finalize. Each stage is stored in `.evolvectl/runs/<id>/report.json`. The same directory holds `report.html`, `events.jsonl`, and Go coverage profiles.

Prepare runs the existing tests and coverage before any edit. Validate runs the same commands after the edit. The report lists newly failed tests separately from tests that were already failing. Coverage numbers are recorded only when the tool printed them.

`scan --format html` renders the dependency inventory. `plan --format html` and `upgrade --format html` render the run. Text remains the default.

Language recipes, manifest edits, and command adapters are capabilities. The campaign does not branch on Copybara. `copybara explain` is a separate static read of `copy.bara.sky`.

`--offline` keeps `go test` from using a module proxy. A Go upgrade that is not a local `replace` also refuses to continue offline when `go.sum` does not already contain the target version. Without `--offline`, the campaign runs `go get <module>@<version>` so `go.sum` matches the bump. `evolvectl rollback --run <id>` restores the snapshotted files. A dry-run stops with a named error when a `replace` points outside the workspace. `go.work` `use` entries are listed as workspace modules. `copybara explain` substitutes top-level `name = "literal"` assignments and does not execute Starlark.

Registry status for `outdated` stays `registry_unavailable` until a snapshot is supplied. Python runs pytest when test files exist and pytest is installed. Coverage.py is used when it is installed. Command adapters do not invent coverage.

`evolvectl init` writes an offline HTML guide to `.evolvectl/guide/` and opens `index.html`. `evolvectl help` opens the command reference. `evolvectl docs <topic> --format html` opens that topic. The pages load no remote assets.
