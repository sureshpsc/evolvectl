# git + Go gRPC fixture

This module depends on a local replacement for `google.golang.org/grpc` so the upgrade can run offline.

```bash
evolvectl plan google.golang.org/grpc --to v1.75.0 --workspace examples/git-go-grpc
evolvectl upgrade google.golang.org/grpc --to v1.75.0 --workspace examples/git-go-grpc
```

The recipes rename `DialContext` to `NewClient` and `OldField` to `NewField`. `go test ./...` is the required gate.
