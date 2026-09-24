# Contributing

Fork the repository, open a branch, and send a pull request to the canonical repository. Do not push directly to `main`.

A recipe change needs:

- `recipe.yaml` with version bounds
- `testdata/input` and `testdata/expected`
- no shell transform

Run `go test ./...` and `go vet ./...` before opening the pull request.
