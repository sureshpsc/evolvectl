package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/evolvectl/evolvectl/internal/fsio"
)

// ExternalReplace is a local replace whose target sits outside the workspace.
type ExternalReplace struct {
	Module   string
	Path     string
	GoMod    string
	Resolved string
}

// HasLocalReplace reports a directory replace for modulePath.
func HasLocalReplace(gomod []byte, modulePath string) bool {
	f, err := modfile.Parse("go.mod", gomod, nil)
	if err != nil {
		return false
	}
	for _, r := range f.Replace {
		if r.Old.Path == modulePath && r.New.Version == "" && r.New.Path != "" {
			return true
		}
	}
	return false
}

// SumContains reports whether go.sum lists module at version.
func SumContains(sum []byte, modulePath, version string) bool {
	version = strings.TrimPrefix(version, "v")
	for _, line := range strings.Split(string(sum), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != modulePath {
			continue
		}
		got := strings.TrimSuffix(fields[1], "/go.mod")
		if got == version || got == "v"+version || fields[1] == version {
			return true
		}
	}
	return false
}

// ExternalReplaces lists local replace targets that resolve outside root.
func ExternalReplaces(root string, manifests []string) ([]ExternalReplace, error) {
	var out []ExternalReplace
	for _, rel := range manifests {
		if !strings.HasSuffix(rel, "go.mod") {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		f, err := modfile.Parse(rel, body, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		for _, r := range f.Replace {
			if r.New.Version != "" || r.New.Path == "" {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(abs), r.New.Path))
			if _, ok := fsio.WithinRoot(root, resolved); ok {
				continue
			}
			out = append(out, ExternalReplace{
				Module: r.Old.Path, Path: r.New.Path, GoMod: rel, Resolved: resolved,
			})
		}
	}
	return out, nil
}

// FormatExternalReplace is the dry-run diagnostic for one outside replace.
func FormatExternalReplace(r ExternalReplace) string {
	return fmt.Sprintf("dry-run unsupported: replace %s => %s in %s resolves outside the workspace", r.Module, r.Path, r.GoMod)
}
