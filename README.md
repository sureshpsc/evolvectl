# Evolvectl

Upgrade codebases, not just dependency files.

Evolvectl discovers a repository, plans a dependency upgrade, applies deterministic recipes, runs validation, and writes one report that the terminal, JSON, Markdown, and offline HTML all render. Copybara is a provider you can inspect. It is not the campaign engine. AI is off unless you enable it, and `--no-ai` keeps the deterministic path.

## Quickstart

```bash
go build -o evolvectl ./cmd/evolvectl
./evolvectl init
./evolvectl scan --workspace examples/mixed-monorepo
./evolvectl plan google.golang.org/grpc --to v1.75.0 --workspace examples/git-go-grpc
./evolvectl upgrade google.golang.org/grpc --to v1.75.0 --workspace examples/git-go-grpc
```

`upgrade` writes the workspace. `--dry-run` applies the same steps in a temporary copy and leaves the original source unchanged.

## What is implemented

- Go CLI for init, scan, graph, outdated, plan, upgrade, validate, explain, diff, history, report, recipe, adapters, provider, session, docs, benchmark, doctor, ui, and copybara explain.
- Go AST recipes and Python structural recipes, with golden fixtures.
- A Maven command adapter that updates `pom.xml` and runs a delegated test command. Java semantic repair is unavailable.
- A partial static reader for `copy.bara.sky`. It does not execute Starlark and `upgrade` does not invoke `copybara migrate`.
- Loopback read-only `evolvectl ui serve` and a single-file HTML report.
- Terminal-scoped provider selection via `evolvectl session export`. A new terminal stays unset.

Registry version checks are not performed. `outdated` reports `registry_unavailable` instead of inventing newer versions.

## License

Apache-2.0. See [LICENSE](LICENSE).
