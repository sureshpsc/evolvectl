# Go gax-go v0 to v2 fixture

This module pins `github.com/googleapis/gax-go` at the 2016 pseudo-version `v0.0.0-20161107002406-da06d194a00e`. Two local stubs under `third_party/` have the exported shape of that commit and of `github.com/googleapis/gax-go/v2` v2.0.2, so the move runs offline.

```bash
evolvectl impact github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2 --workspace examples/go-gax-v2
evolvectl upgrade github.com/googleapis/gax-go --to v2.0.2 --to-module github.com/googleapis/gax-go/v2 --offline --workspace examples/go-gax-v2
```

The upgrade moves the import to `/v2`, swaps the `require`, and the `go.gax.invoke-callsettings` recipe adds the `CallSettings` parameter that v2 passes to the `gax.Invoke` callback. `go test ./...` runs before and after.

The run finishes as needs-review because `replace github.com/googleapis/gax-go` is still in `go.mod` after the move. Delete it once nothing else needs the old module.
