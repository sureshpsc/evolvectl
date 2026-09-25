// Package recipe loads and applies deterministic migration recipes.
package recipe

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sureshpsc/evolvectl/internal/domain"
)

// Recipe is a version-bounded deterministic transform.
type Recipe struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
	Path       string   `yaml:"-" json:"path,omitempty"`
}

// Metadata identifies a recipe.
type Metadata struct {
	ID     string   `yaml:"id" json:"id"`
	Title  string   `yaml:"title" json:"title"`
	Owners []string `yaml:"owners" json:"owners"`
	Tags   []string `yaml:"tags" json:"tags"`
}

// Spec is the match and transform.
type Spec struct {
	Language   string     `yaml:"language" json:"language"`
	Dependency Dependency `yaml:"dependency" json:"dependency"`
	Match      Match      `yaml:"match" json:"match"`
	Transform  Transform  `yaml:"transform" json:"transform"`
	Safety     Safety     `yaml:"safety" json:"safety"`
	Validate   []string   `yaml:"validate" json:"validate"`
}

// Dependency bounds the recipe.
type Dependency struct {
	Ecosystem string `yaml:"ecosystem" json:"ecosystem"`
	Name      string `yaml:"name" json:"name"`
	From      string `yaml:"from" json:"from"`
	To        string `yaml:"to" json:"to"`
}

// Match selects source constructs.
type Match struct {
	Type    string `yaml:"type" json:"type"`
	Package string `yaml:"package" json:"package"`
	Symbol  string `yaml:"symbol" json:"symbol"`
	Field   string `yaml:"field" json:"field"`
	Keyword string `yaml:"keyword" json:"keyword"`
}

// Transform describes the edit. Shell execution is rejected.
type Transform struct {
	Type          string    `yaml:"type" json:"type"`
	TargetSymbol  string    `yaml:"target_symbol" json:"target_symbol"`
	TargetImport  string    `yaml:"target_import" json:"target_import"`
	TargetField   string    `yaml:"target_field" json:"target_field"`
	TargetKeyword string    `yaml:"target_keyword" json:"target_keyword"`
	Arguments     Arguments `yaml:"arguments" json:"arguments"`
	Callback      Callback  `yaml:"callback" json:"callback"`
}

// Callback appends a parameter to a function literal passed at Index. The edit applies only
// when the literal has WhenParams parameters. {pkg} in Type is the file's name for the
// matched import.
type Callback struct {
	Index      int    `yaml:"index" json:"index"`
	Name       string `yaml:"name" json:"name"`
	Type       string `yaml:"type" json:"type"`
	WhenParams int    `yaml:"when_params" json:"when_params"`
}

// Arguments describes call edits.
type Arguments struct {
	Remove []ArgIndex  `yaml:"remove" json:"remove"`
	Insert []ArgInsert `yaml:"insert" json:"insert"`
}

// ArgIndex is a call argument position.
type ArgIndex struct {
	Index int `yaml:"index" json:"index"`
}

// ArgInsert inserts a Go expression at an index.
type ArgInsert struct {
	Index int    `yaml:"index" json:"index"`
	Expr  string `yaml:"expr" json:"expr"`
}

// Safety is policy metadata.
type Safety struct {
	Confidence      string `yaml:"confidence" json:"confidence"`
	GeneratedFiles  string `yaml:"generated_files" json:"generated_files"`
	RequireTypeInfo bool   `yaml:"require_type_info" json:"require_type_info"`
}

// VersionContext is the upgrade coordinate used to filter recipes.
type VersionContext struct {
	Ecosystem string
	Name      string
	Current   string
	Target    string
}

var allowedTransforms = map[string]bool{
	"import_path_replace":     true,
	"call_rename":             true,
	"call_argument_remove":    true,
	"call_argument_insert":    true,
	"call_rewrite":            true,
	"struct_field_rename":     true,
	"callback_param_append":   true,
	"python_import_rename":    true,
	"python_decorator_rename": true,
	"python_symbol_rename":    true,
	"python_method_rename":    true,
	"python_keyword_rename":   true,
}

// Parse decodes one recipe.
func Parse(b []byte) (Recipe, error) {
	var r Recipe
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return Recipe{}, err
	}
	if err := Validate(r); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

// Validate rejects incomplete or unsafe recipes.
func Validate(r Recipe) error {
	if r.APIVersion == "" || r.Kind != "MigrationRecipe" {
		return fmt.Errorf("recipe must be MigrationRecipe")
	}
	if r.Metadata.ID == "" {
		return fmt.Errorf("recipe id is required")
	}
	if strings.Contains(strings.ToLower(r.Spec.Transform.Type), "shell") || strings.Contains(strings.ToLower(r.Spec.Transform.Type), "exec") {
		return fmt.Errorf("recipe %s: shell transforms are not allowed", r.Metadata.ID)
	}
	if !allowedTransforms[r.Spec.Transform.Type] {
		return fmt.Errorf("recipe %s: unsupported transform %q", r.Metadata.ID, r.Spec.Transform.Type)
	}
	return nil
}

// LoadDir reads recipe.yaml files under dir.
func LoadDir(dir string) ([]Recipe, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Recipe
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "recipe.yaml" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		r, err := Parse(b)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		r.Path = path
		out = append(out, r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.ID < out[j].Metadata.ID })
	return out, nil
}

// LoadFS reads recipe.yaml files from fsys.
func LoadFS(fsys fs.FS) ([]Recipe, error) {
	var out []Recipe
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "recipe.yaml" {
			return nil
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		r, err := Parse(b)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		r.Path = path
		out = append(out, r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.ID < out[j].Metadata.ID })
	return out, nil
}

// Applicable reports whether the recipe should run for this coordinate.
func Applicable(r Recipe, vc VersionContext) bool {
	if vc.Name != "" && r.Spec.Dependency.Name != "" && r.Spec.Dependency.Name != vc.Name {
		return false
	}
	if vc.Ecosystem != "" && r.Spec.Dependency.Ecosystem != "" && !ecoMatch(r.Spec.Dependency.Ecosystem, vc.Ecosystem) {
		return false
	}
	if vc.Current != "" && r.Spec.Dependency.From != "" && !ConstraintsAllow(r.Spec.Dependency.From, vc.Current) {
		return false
	}
	if vc.Target != "" && r.Spec.Dependency.To != "" && !ConstraintsAllow(r.Spec.Dependency.To, vc.Target) {
		return false
	}
	return true
}

func ecoMatch(recipeEco, want string) bool {
	if recipeEco == want {
		return true
	}
	if (recipeEco == "gomod" || recipeEco == "go") && want == "go" {
		return true
	}
	if (recipeEco == "pypi" || recipeEco == "python") && want == "python" {
		return true
	}
	return false
}

// Confidence maps recipe safety to the domain scale.
func Confidence(r Recipe) string {
	switch strings.ToLower(r.Spec.Safety.Confidence) {
	case "high":
		return domain.ConfidenceHigh
	case "medium":
		return domain.ConfidenceMedium
	case "low":
		return domain.ConfidenceLow
	default:
		return domain.ConfidenceMedium
	}
}

// MeetsThreshold compares confidence labels.
func MeetsThreshold(got, threshold string) bool {
	rank := map[string]int{domain.ConfidenceLow: 1, domain.ConfidenceMedium: 2, domain.ConfidenceHigh: 3}
	t := strings.ToLower(threshold)
	switch t {
	case "low":
		t = domain.ConfidenceLow
	case "medium":
		t = domain.ConfidenceMedium
	case "high", "":
		t = domain.ConfidenceHigh
	}
	return rank[got] >= rank[t]
}
