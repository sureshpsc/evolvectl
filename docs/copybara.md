# Copybara

`evolvectl copybara explain` reads `copy.bara.sky` without executing it. Literal globs and `core.move` / `core.replace` can be traced. Other calls stay unresolved.

`evolvectl upgrade` does not run `copybara migrate`. A Copybara dry-run can still create a review on a remote destination, so migration stays an explicit Copybara invocation outside this command.

Credential-shaped URLs are redacted in the explanation.
