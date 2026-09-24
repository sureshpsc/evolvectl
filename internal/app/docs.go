package app

var docTopics = map[string]string{
	"quickstart": `Evolvectl quickstart
1. evolvectl init
2. evolvectl scan
3. evolvectl plan <dependency> --to <version>
4. evolvectl upgrade <dependency> --to <version>
5. evolvectl report --run <id> --format html

Source stays local. AI is off unless you enable it. --no-ai forces the deterministic path.
Upgrade writes the workspace unless you pass --dry-run.`,
	"providers": `Providers
Precedence: flag > EVOLVECTL_WORKSPACE_PROVIDER > repository config > auto.
Session selection is terminal-scoped:
  evolvectl session export git
A new terminal has no selection unless you export again.
copybara, git, and filesystem are built in. Command adapters and evolvectl-adapter-* executables extend the set.`,
	"recipes": `Recipes
Recipes are YAML files with a match and a transform. Shell transforms are rejected.
  evolvectl recipe list
  evolvectl recipe test ./recipes
Golden files live in testdata/input and testdata/expected next to recipe.yaml.`,
	"copybara": `Copybara
evolvectl copybara explain reads copy.bara.sky statically. It does not execute Starlark and it does not migrate.
copybara migrate can have remote effects. upgrade does not invoke it.
If the copybara binary is missing, explain still works and says the tool is unavailable.`,
	"examples": `Examples
examples/git-go-grpc
examples/python-pydantic
examples/java-maven-command-adapter
examples/node-pnpm-command-adapter
examples/mixed-monorepo
examples/custom-tool-plugin`,
	"troubleshooting": `Troubleshooting
COPYBARA_NOT_FOUND: install Copybara or select git.
Dirty worktree: commit, stash, or pass --allow-dirty. Dry-run does not require a clean tree.
Stale plan: files changed after planning. Run plan again.
registry_unavailable is not a claim that a package is current.`,
	"ui": `Local UI
evolvectl ui serve --host 127.0.0.1 --port 0
The server is read-only, binds loopback only, and reads runs already stored under .evolvectl.
Plain scan/plan/upgrade do not start a server.
Stop the server by stopping that process.`,
	"session": `Session
evolvectl session export <provider> --shell powershell|bash|zsh|fish
The command prints an assignment. It does not change the parent shell by itself.
eval "$(evolvectl session export git)" is the bash/zsh form.`,
}
