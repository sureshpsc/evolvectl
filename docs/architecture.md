# Architecture

The CLI is Cobra. Commands call `internal/app`, which calls the campaign executor. The executor moves a run through preflight, discover, assess, plan, prepare, apply, diagnose, repair, validate, and finalize. Each stage is stored in `.evolvectl/runs/<id>/report.json`. The same directory holds `report.html`, `events.jsonl`, and Go coverage profiles.

Prepare runs the existing tests and coverage before any edit. Validate runs the same commands after the edit. The report lists newly failed tests separately from tests that were already failing. Coverage numbers are recorded only when the tool printed them.

`scan --format html` renders the dependency inventory. `plan --format html` and `upgrade --format html` render the run. Text remains the default.

Language recipes, manifest edits, and command adapters are capabilities. The campaign does not branch on Copybara. `copybara explain` is a separate static read of `copy.bara.sky`.

`--offline` keeps `go test` from using a module proxy. Without it, Go may download the module it is upgrading. Registry status for `outdated` stays `registry_unavailable` until a snapshot is supplied. Python runs pytest when test files exist and pytest is installed. Coverage.py is used when it is installed. Command adapters do not invent coverage.
