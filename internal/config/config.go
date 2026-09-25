// Package config loads .evolvectl.yaml.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// File is the repository configuration.
type File struct {
	Version    int        `yaml:"version" json:"version"`
	Repository Repository `yaml:"repository" json:"repository"`
	Workspace  Workspace  `yaml:"workspace" json:"workspace"`
	Languages  Languages  `yaml:"languages" json:"languages"`
	Repair     Repair     `yaml:"repair" json:"repair"`
	Validation Validation `yaml:"validation" json:"validation"`
	AI         AI         `yaml:"ai" json:"ai"`
	Reporting  Reporting  `yaml:"reporting" json:"reporting"`
	Execution  Execution  `yaml:"execution" json:"execution"`
}

// Repository settings.
type Repository struct {
	Root   string   `yaml:"root" json:"root"`
	Ignore []string `yaml:"ignore" json:"ignore"`
}

// Workspace settings.
type Workspace struct {
	Type     string   `yaml:"type" json:"type"`
	Copybara Copybara `yaml:"copybara" json:"copybara"`
}

// Copybara settings. The binary is optional.
type Copybara struct {
	Binary                string   `yaml:"binary" json:"binary"`
	Config                string   `yaml:"config" json:"config"`
	Workflow              string   `yaml:"workflow" json:"workflow"`
	Args                  []string `yaml:"args" json:"args"`
	PreserveTempOnFailure bool     `yaml:"preserve_temp_on_failure" json:"preserve_temp_on_failure"`
}

// Languages toggles reference adapters.
type Languages struct {
	Go     Lang `yaml:"go" json:"go"`
	Python Lang `yaml:"python" json:"python"`
}

// Lang is one language block.
type Lang struct {
	Enabled     bool   `yaml:"enabled" json:"enabled"`
	Tests       string `yaml:"tests,omitempty" json:"tests,omitempty"`
	TypeChecker string `yaml:"type_checker,omitempty" json:"type_checker,omitempty"`
	Linter      string `yaml:"linter,omitempty" json:"linter,omitempty"`
	TestRunner  string `yaml:"test_runner,omitempty" json:"test_runner,omitempty"`
}

// Repair bounds the loop.
type Repair struct {
	MaxIterations        int    `yaml:"max_iterations" json:"max_iterations"`
	ConfidenceThreshold  string `yaml:"confidence_threshold" json:"confidence_threshold"`
	AllowGenerated       bool   `yaml:"allow_generated" json:"allow_generated"`
	MaxFilesPerIteration int    `yaml:"max_files_per_iteration" json:"max_files_per_iteration"`
	MaxTotalFiles        int    `yaml:"max_total_files" json:"max_total_files"`
}

// Validation commands are argv arrays, never shell strings.
type Validation struct {
	FailFast bool     `yaml:"fail_fast" json:"fail_fast"`
	Commands []string `yaml:"commands" json:"commands"`
	// TestTimeout bounds one test command per module, as a Go duration.
	TestTimeout string `yaml:"test_timeout" json:"test_timeout"`
	// TestCache lets go test reuse cached results for packages whose inputs
	// did not change, instead of forcing -count=1.
	TestCache bool `yaml:"test_cache" json:"test_cache"`
}

// DefaultTestTimeout applies when validation.test_timeout is empty.
const DefaultTestTimeout = 10 * time.Minute

// TestTimeoutDuration returns the parsed timeout or the default.
func (v Validation) TestTimeoutDuration() time.Duration {
	if d, err := time.ParseDuration(v.TestTimeout); err == nil && d > 0 {
		return d
	}
	return DefaultTestTimeout
}

// AI is optional and off by default.
type AI struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	Provider        string   `yaml:"provider" json:"provider"`
	MaxContextFiles int      `yaml:"max_context_files" json:"max_context_files"`
	MaxContextBytes int      `yaml:"max_context_bytes" json:"max_context_bytes"`
	Redact          []string `yaml:"redact" json:"redact"`
}

// Reporting selects renderers.
type Reporting struct {
	Formats        []string `yaml:"formats" json:"formats"`
	IncludeRawLogs bool     `yaml:"include_raw_logs" json:"include_raw_logs"`
}

// Execution bounds concurrency.
type Execution struct {
	Workers          int `yaml:"workers" json:"workers"`
	ValidatorWorkers int `yaml:"validator_workers" json:"validator_workers"`
	// DryRunCopy is auto, worktree, or copy. auto uses a git worktree when
	// the workspace is clean and falls back to a full copy otherwise.
	DryRunCopy string `yaml:"dry_run_copy" json:"dry_run_copy"`
}

// Default returns safe local defaults.
func Default() File {
	return File{
		Version: 1,
		Repository: Repository{
			Root: ".",
			Ignore: []string{
				"**/vendor/**",
				"**/.venv/**",
				"**/node_modules/**",
			},
		},
		Workspace: Workspace{
			Type: "auto",
			Copybara: Copybara{
				Binary:                "copybara",
				Config:                "copy.bara.sky",
				Workflow:              "default",
				PreserveTempOnFailure: true,
			},
		},
		Languages: Languages{
			Go:     Lang{Enabled: true, Tests: "affected"},
			Python: Lang{Enabled: true, TypeChecker: "pyright", Linter: "ruff", TestRunner: "pytest"},
		},
		Repair: Repair{
			MaxIterations:        5,
			ConfidenceThreshold:  "high",
			AllowGenerated:       false,
			MaxFilesPerIteration: 100,
			MaxTotalFiles:        500,
		},
		Validation: Validation{FailFast: false, Commands: []string{}, TestTimeout: "10m"},
		AI: AI{
			Enabled:         false,
			Provider:        "noop",
			MaxContextFiles: 8,
			MaxContextBytes: 120000,
			Redact:          []string{"*.pem", ".env*", "**/secrets/**"},
		},
		Reporting: Reporting{Formats: []string{"text", "json"}, IncludeRawLogs: false},
		Execution: Execution{Workers: 8, ValidatorWorkers: 4, DryRunCopy: "auto"},
	}
}

// Validate checks the loaded file.
func Validate(f File) error {
	if f.Version != 1 {
		return errors.New("config version must be 1")
	}
	switch f.Workspace.Type {
	case "", "auto", "git", "copybara", "filesystem":
	default:
		return errors.New("workspace.type must be auto, git, copybara, or filesystem")
	}
	switch f.Repair.ConfidenceThreshold {
	case "", "high", "medium", "low":
	default:
		return errors.New("repair.confidence_threshold must be high, medium, or low")
	}
	if f.Repair.MaxIterations < 1 {
		return errors.New("repair.max_iterations must be >= 1")
	}
	if f.Validation.TestTimeout != "" {
		if d, err := time.ParseDuration(f.Validation.TestTimeout); err != nil || d <= 0 {
			return errors.New("validation.test_timeout must be a positive duration such as 10m or 1h")
		}
	}
	switch f.Execution.DryRunCopy {
	case "", "auto", "worktree", "copy":
	default:
		return errors.New("execution.dry_run_copy must be auto, worktree, or copy")
	}
	if f.Execution.ValidatorWorkers < 0 {
		return errors.New("execution.validator_workers must be >= 0")
	}
	if f.AI.Enabled && f.AI.Provider == "" {
		return errors.New("ai.provider is required when ai.enabled is true")
	}
	return nil
}

// Load reads path, or returns defaults when the file is absent.
func Load(path string) (File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return File{}, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return File{}, err
	}
	if err := Validate(cfg); err != nil {
		return File{}, err
	}
	return cfg, nil
}

// Save writes the config.
func Save(path string, cfg File) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Path is the config file inside a workspace.
func Path(workspace string) string {
	return filepath.Join(workspace, ".evolvectl.yaml")
}
