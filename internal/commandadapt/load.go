// Package commandadapt loads declarative argv adapters.
package commandadapt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Adapter is a declarative command integration.
type Adapter struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Capabilities []string           `yaml:"capabilities"`
		Commands     map[string]Command `yaml:"commands"`
		Diagnostics  struct {
			Patterns []Pattern `yaml:"patterns"`
		} `yaml:"diagnostics"`
	} `yaml:"spec"`
	Path string `yaml:"-"`
}

// Command is an argv template.
type Command struct {
	Argv     []string `yaml:"argv"`
	Timeout  string   `yaml:"timeout"`
	Required bool     `yaml:"required"`
}

// Pattern is a diagnostic regex supplied by the user. It is not executed as shell.
type Pattern struct {
	Name  string `yaml:"name"`
	Regex string `yaml:"regex"`
}

// LoadDir reads adapters from dir.
func LoadDir(dir string) ([]Adapter, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Adapter
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var a Adapter
		if err := yaml.Unmarshal(b, &a); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if a.Kind != "CommandAdapter" {
			continue
		}
		if err := Validate(a); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		a.Path = filepath.ToSlash(filepath.Join(dir, e.Name()))
		out = append(out, a)
	}
	return out, nil
}

// Validate rejects shell strings and unknown placeholders.
func Validate(a Adapter) error {
	if a.Metadata.Name == "" {
		return fmt.Errorf("command adapter name is required")
	}
	for key, cmd := range a.Spec.Commands {
		if len(cmd.Argv) == 0 {
			return fmt.Errorf("%s: argv is empty", key)
		}
		for _, arg := range cmd.Argv {
			if strings.Contains(arg, "sh -c") || strings.Contains(arg, "cmd /c") {
				return fmt.Errorf("%s: shell execution is not allowed", key)
			}
			if err := checkPlaceholders(arg); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	return nil
}

func checkPlaceholders(arg string) error {
	rest := arg
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			return nil
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			return fmt.Errorf("unclosed placeholder")
		}
		name := rest[i+2 : i+j]
		switch name {
		case "root", "project.root", "dependency", "version":
		default:
			return fmt.Errorf("unknown placeholder %s", name)
		}
		rest = rest[i+j+2:]
	}
}

// Render substitutes trusted placeholders.
func Render(argv []string, vars map[string]string) ([]string, error) {
	out := make([]string, len(argv))
	for i, a := range argv {
		s := a
		for k, v := range vars {
			if strings.ContainsAny(v, "\n\r") {
				return nil, fmt.Errorf("invalid value for %s", k)
			}
			s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		}
		if strings.Contains(s, "{{") {
			return nil, fmt.Errorf("unresolved placeholder in %s", a)
		}
		out[i] = s
	}
	return out, nil
}

// Timeout parses a Go duration, defaulting to 2 minutes.
func Timeout(s string) time.Duration {
	if s == "" {
		return 2 * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 2 * time.Minute
	}
	return d
}
