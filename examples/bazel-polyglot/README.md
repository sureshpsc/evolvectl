# Bazel polyglot marker

See `examples/mixed-monorepo` for the checked-in Bazel file (`MODULE.bazel`) next to Go and Python manifests. Evolvectl records Bazel and does not run `bazel test` unless you configure that command. A skipped optional gate is not reported as a pass.
