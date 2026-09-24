# Java/Maven via a command adapter

Evolvectl edits `pom.xml`. It does not claim Java semantic repair. The test command is delegated to `go run ./tools`, which only proves that the configured command exited 0.

```bash
evolvectl upgrade maven:junit:junit --to 4.13.2 --workspace examples/java-maven-command-adapter
```
