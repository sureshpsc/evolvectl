# Copybara + Go

`examples/mixed-monorepo/copy.bara.sky` is the static fixture.

```bash
evolvectl copybara explain --config examples/mixed-monorepo/copy.bara.sky --workflow export-go-lib --file go/client/client.go
```

`go/client/client.go` is included, moved to `third_party/go/client/client.go`, and not rewritten by the BUILD-only `core.replace`. `go/vendor/x.go` is excluded. This is a static reading, not a migration.
