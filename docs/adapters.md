# Adapters

| Adapter | Discover | Manifest edit | Semantic or structural repair | Test |
| --- | --- | --- | --- | --- |
| Go | native | native | native AST recipes | `go test` when `go` is installed |
| Python | native | native | structural recipes | delimiter balance; `py_compile` when Python is installed |
| Maven | native `pom.xml` | native version edit | unavailable | delegated command when configured |
| Node | native `package.json` | native version edit | unavailable | only if a command adapter defines one |
| Git | status via `git` | n/a | n/a | n/a |
| Copybara | find `copy.bara.sky` | n/a | static explain only | not invoked by upgrade |
| Command adapter | declarative YAML | n/a | unavailable | argv you configure |
| Executable adapter | handshake | depends on the adapter | must not be claimed by the core | depends on the adapter |

`delegated_command` means the tool ran. It does not mean the source was understood.
