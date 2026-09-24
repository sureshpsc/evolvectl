// Package session resolves the terminal-scoped workspace provider.
package session

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

const EnvProvider = "EVOLVECTL_WORKSPACE_PROVIDER"

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Resolve applies flag > environment > repository config > auto.
func Resolve(flag, envValue, repoValue string) (value, source string) {
	if flag != "" {
		return flag, "flag"
	}
	if envValue != "" {
		return envValue, "session"
	}
	if repoValue != "" && repoValue != "auto" {
		return repoValue, "repo"
	}
	return "auto", "auto"
}

// ValidateName rejects empty or unsafe provider names.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid provider name %q", name)
	}
	return nil
}

// Export emits a shell assignment and nothing else.
func Export(provider, shell string) (string, error) {
	if err := ValidateName(provider); err != nil {
		return "", err
	}
	if shell == "" {
		shell = DefaultShell()
	}
	switch strings.ToLower(shell) {
	case "bash", "zsh", "sh":
		return fmt.Sprintf("export %s='%s'\n", EnvProvider, provider), nil
	case "fish":
		return fmt.Sprintf("set -gx %s '%s'\n", EnvProvider, provider), nil
	case "powershell", "pwsh":
		return fmt.Sprintf("$env:%s='%s'\n", EnvProvider, provider), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}

// Clear emits an unset appropriate for the shell.
func Clear(shell string) (string, error) {
	if shell == "" {
		shell = DefaultShell()
	}
	switch strings.ToLower(shell) {
	case "bash", "zsh", "sh":
		return fmt.Sprintf("unset %s\n", EnvProvider), nil
	case "fish":
		return fmt.Sprintf("set -e %s\n", EnvProvider), nil
	case "powershell", "pwsh":
		return fmt.Sprintf("Remove-Item Env:%s -ErrorAction SilentlyContinue\n", EnvProvider), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}

// DefaultShell is a hint for the current OS. It does not mutate the shell.
func DefaultShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

// UseHint tells the user how to update the current terminal only.
func UseHint(provider string) string {
	shell := DefaultShell()
	line, err := Export(provider, shell)
	if err != nil {
		return err.Error()
	}
	if shell == "powershell" {
		return "Active provider is terminal-scoped and is not written to config.\nRun this in the current terminal:\n  " + strings.TrimSpace(line)
	}
	return "Active provider is terminal-scoped and is not written to config.\nRun this in the current terminal:\n  eval \"$(evolvectl session export " + provider + ")\""
}
