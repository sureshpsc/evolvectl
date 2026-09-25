// Package domain is the shared run model rendered by every report format.
package domain

import "time"

const (
	ConfidenceHigh    = "HIGH"
	ConfidenceMedium  = "MEDIUM"
	ConfidenceLow     = "LOW"
	ConfidenceBlocked = "BLOCKED"

	OutcomeSucceeded           = "succeeded"
	OutcomeSucceededWithReview = "succeeded_with_review"
	OutcomeFailed              = "failed"
	OutcomeBlocked             = "blocked"
	OutcomeCancelled           = "cancelled"
	OutcomePartial             = "partial"

	StagePending   = "pending"
	StageRunning   = "running"
	StagePassed    = "passed"
	StageFailed    = "failed"
	StageBlocked   = "blocked"
	StageSkipped   = "skipped"
	StageCancelled = "cancelled"
	StageUnknown   = "unknown"

	SupportNative      = "native"
	SupportDelegated   = "delegated_command"
	SupportReadOnly    = "read_only"
	SupportUnavailable = "unavailable"
	SupportUnknown     = "unknown"

	StatusCurrent             = "current"
	StatusUpdateAvailable     = "update_available"
	StatusSecurityUpdate      = "security_update"
	StatusBlocked             = "blocked"
	StatusPinned              = "pinned"
	StatusUnknown             = "unknown"
	StatusRegistryUnavailable = "registry_unavailable"
	StatusUnresolved          = "unresolved"
	StatusNotSupported        = "not_supported"

	GatePass        = "PASS"
	GateFail        = "FAIL"
	GateSkipped     = "SKIPPED"
	GateBlocked     = "BLOCKED"
	GateNeedsReview = "NEEDS_REVIEW"
	GateUnavailable = "UNAVAILABLE"
	GateCancelled   = "CANCELLED"
)

// StageOrder is the campaign lifecycle.
var StageOrder = []string{
	"preflight",
	"discover",
	"assess",
	"plan",
	"prepare",
	"apply",
	"diagnose",
	"repair",
	"validate",
	"finalize",
}

// Project is a discovered language/build unit.
type Project struct {
	ID         string   `json:"id"`
	Root       string   `json:"root"`
	Language   string   `json:"language"`
	Build      string   `json:"build"`
	Manifests  []string `json:"manifests"`
	TestScopes []string `json:"test_scopes"`
}

// Dependency is one declared or resolved package requirement.
type Dependency struct {
	ID                    string     `json:"id"`
	Ecosystem             string     `json:"ecosystem"`
	Name                  string     `json:"name"`
	PackageURL            string     `json:"package_url,omitempty"`
	CurrentVersion        string     `json:"current_version"`
	ResolvedVersion       string     `json:"resolved_version,omitempty"`
	Direct                bool       `json:"direct"`
	Manifest              string     `json:"manifest"`
	ProjectID             string     `json:"project_id"`
	Scope                 string     `json:"scope,omitempty"`
	Status                string     `json:"status"`
	StatusReason          string     `json:"status_reason,omitempty"`
	AvailabilityCheckedAt *time.Time `json:"availability_checked_at,omitempty"`
	AdvisoryCheckedAt     *time.Time `json:"advisory_checked_at,omitempty"`
	Source                string     `json:"source"`
	TargetVersion         string     `json:"target_version,omitempty"`
}

// Manifest is a discovered dependency file.
type Manifest struct {
	Path      string `json:"path"`
	Ecosystem string `json:"ecosystem"`
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id"`
}

// SourceFile is a path recorded for later import analysis.
type SourceFile struct {
	Path      string   `json:"path"`
	Language  string   `json:"language"`
	Generated bool     `json:"generated"`
	Hash      string   `json:"hash,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	Imports   []string `json:"imports,omitempty"`
}

// Inventory is the discovery snapshot.
type Inventory struct {
	ScannedAt         time.Time    `json:"scanned_at"`
	RepoType          string       `json:"repo_type"`
	Root              string       `json:"root"`
	Projects          []Project    `json:"projects"`
	Dependencies      []Dependency `json:"dependencies"`
	Manifests         []Manifest   `json:"manifests"`
	SourceFiles       []SourceFile `json:"source_files"`
	BuildSystems      []string     `json:"build_systems"`
	Languages         []string     `json:"languages"`
	WorkspaceAdapters []string     `json:"workspace_adapters"`
	GeneratedPaths    []string     `json:"generated_paths"`
	VendorPaths       []string     `json:"vendor_paths"`
	Warnings          []string     `json:"warnings"`
	FilesIndexed      int          `json:"files_indexed"`
	CacheHits         int          `json:"cache_hits"`
	CacheMisses       int          `json:"cache_misses"`
	CopybaraConfigs   []string     `json:"copybara_configs"`
	WorkspaceModules  []string     `json:"workspace_modules,omitempty"`
	CommandAdapters   []string     `json:"command_adapters"`
	HasGit            bool         `json:"has_git"`
	GitDirty          bool         `json:"git_dirty"`
	BaselineRevision  string       `json:"baseline_revision,omitempty"`
}

// Target is the requested upgrade coordinate. ToModule is set when the module path changes,
// as in a /vN major-version move or a rename. VendorDir is set when the module source is
// copied into the workspace and wired with a replace directive. UseDir is set when a sync
// tool already put the new version in the workspace; it is wired with a replace and never
// downloaded.
type Target struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	To        string `json:"to"`
	Raw       string `json:"raw,omitempty"`
	ToModule  string `json:"to_module,omitempty"`
	VendorDir string `json:"vendor_dir,omitempty"`
	UseDir    string `json:"use_dir,omitempty"`
}

// Module is the module path the workspace should require after the run.
func (t Target) Module() string {
	if t.ToModule != "" {
		return t.ToModule
	}
	return t.Name
}

// APIChange is one exported symbol that differs between two versions of a module.
type APIChange struct {
	Package string `json:"package"`
	Symbol  string `json:"symbol"`
	Kind    string `json:"kind"`
	Change  string `json:"change"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
}

// CallSite is one workspace reference to a removed or changed symbol.
type CallSite struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Package string `json:"package"`
	Symbol  string `json:"symbol"`
	Change  string `json:"change"`
}

// Impact is a syntactic comparison of the exported API of two module versions, plus
// the workspace references that touch removed or changed symbols. It is not type-checked.
type Impact struct {
	Module         string      `json:"module"`
	From           string      `json:"from"`
	ToModule       string      `json:"to_module"`
	To             string      `json:"to"`
	Status         string      `json:"status"`
	Reason         string      `json:"reason,omitempty"`
	Removed        int         `json:"removed"`
	Changed        int         `json:"changed"`
	Added          int         `json:"added"`
	Changes        []APIChange `json:"changes"`
	Sites          []CallSite  `json:"sites"`
	FilesAffected  int         `json:"files_affected"`
	MethodsChanged int         `json:"methods_changed"`
	Note           string      `json:"note,omitempty"`
}

// SCMResult records the branch, commit, and pull request created after a run.
type SCMResult struct {
	Branch string `json:"branch,omitempty"`
	Base   string `json:"base,omitempty"`
	Commit string `json:"commit,omitempty"`
	Pushed bool   `json:"pushed"`
	PRURL  string `json:"pr_url,omitempty"`
	Draft  bool   `json:"draft"`
	Note   string `json:"note,omitempty"`
}

// BatchRow is one upgrade in a batch file and the run that executed it.
type BatchRow struct {
	Index          int    `json:"index"`
	Dependency     string `json:"dependency"`
	To             string `json:"to"`
	ToModule       string `json:"to_module,omitempty"`
	Workspace      string `json:"workspace,omitempty"`
	Owner          string `json:"owner,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	Outcome        string `json:"outcome"`
	Reason         string `json:"reason,omitempty"`
	ExitCode       int    `json:"exit_code"`
	Changes        int    `json:"changes"`
	NewlyFailed    int    `json:"newly_failed"`
	CoverageBefore string `json:"coverage_before,omitempty"`
	CoverageAfter  string `json:"coverage_after,omitempty"`
	ImpactSites    int    `json:"impact_sites"`
	Branch         string `json:"branch,omitempty"`
	PRURL          string `json:"pr_url,omitempty"`
	Report         string `json:"report,omitempty"`
}

// BatchReport is the combined record of one batch invocation.
type BatchReport struct {
	SchemaVersion string     `json:"schema_version"`
	ID            string     `json:"id"`
	CreatedAt     time.Time  `json:"created_at"`
	Source        string     `json:"source"`
	Mode          string     `json:"mode"`
	WorkspaceRoot string     `json:"workspace_root"`
	Rows          []BatchRow `json:"rows"`
	Succeeded     int        `json:"succeeded"`
	NeedsReview   int        `json:"needs_review"`
	Failed        int        `json:"failed"`
}

// PlanStep is one ordered campaign action.
type PlanStep struct {
	ID         string `json:"id"`
	Order      int    `json:"order"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Capability string `json:"capability"`
	Support    string `json:"support"`
}

// Risk is an explainable classification, not a probability.
type Risk struct {
	Level           string   `json:"level"`
	Confidence      string   `json:"confidence"`
	UnknownPatterns int      `json:"unknown_patterns"`
	Reasons         []string `json:"reasons"`
}

// Plan is an immutable upgrade plan.
type Plan struct {
	ID              string     `json:"id"`
	Hash            string     `json:"hash"`
	CreatedAt       time.Time  `json:"created_at"`
	Target          Target     `json:"target"`
	CurrentVersions []string   `json:"current_versions"`
	Manifests       []string   `json:"manifests"`
	Projects        []string   `json:"projects"`
	Files           []string   `json:"files"`
	Recipes         []string   `json:"recipes"`
	Validations     []string   `json:"validations"`
	Risk            Risk       `json:"risk"`
	Steps           []PlanStep `json:"steps"`
	ReviewPoints    []string   `json:"review_points"`
	CopybaraImpact  string     `json:"copybara_impact,omitempty"`
	GeneratedImpact []string   `json:"generated_impact"`
	NoWrites        bool       `json:"no_writes"`
	BaseRevision    string     `json:"base_revision,omitempty"`
	CapabilityNotes []string   `json:"capability_notes"`
}

// Diagnostic is one normalized tool finding.
type Diagnostic struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	Tool          string `json:"tool"`
	Language      string `json:"language"`
	File          string `json:"file"`
	Line          int    `json:"line"`
	Column        int    `json:"column"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Category      string `json:"category"`
	Symbol        string `json:"symbol,omitempty"`
	Dependency    string `json:"dependency,omitempty"`
	Fingerprint   string `json:"fingerprint"`
	Raw           string `json:"raw,omitempty"`
	MappedFrom    string `json:"mapped_from,omitempty"`
	MapConfidence string `json:"map_confidence,omitempty"`
}

// DiagnosticGroup collapses repeated fingerprints.
type DiagnosticGroup struct {
	Fingerprint   string   `json:"fingerprint"`
	Category      string   `json:"category"`
	Message       string   `json:"message"`
	Count         int      `json:"count"`
	Files         []string `json:"files"`
	DiagnosticIDs []string `json:"diagnostic_ids"`
}

// Change is one provenance-bearing edit.
type Change struct {
	ID           string `json:"id"`
	File         string `json:"file"`
	Kind         string `json:"kind"`
	RecipeID     string `json:"recipe_id,omitempty"`
	DiagnosticID string `json:"diagnostic_id,omitempty"`
	Confidence   string `json:"confidence"`
	Diff         string `json:"diff,omitempty"`
	BeforeHash   string `json:"before_hash"`
	AfterHash    string `json:"after_hash"`
	Reason       string `json:"reason"`
}

// TestRef is one named test result used to compare before and after.
type TestRef struct {
	Package string `json:"package"`
	Name    string `json:"name"`
	Status  string `json:"status"`
}

// CoverageSample is one measured coverage figure. HasPercent is false when the tool did not produce a number.
type CoverageSample struct {
	Phase      string  `json:"phase"`
	Tool       string  `json:"tool"`
	Scope      string  `json:"scope"`
	Status     string  `json:"status"`
	Percent    float64 `json:"percent,omitempty"`
	HasPercent bool    `json:"has_percent"`
	Profile    string  `json:"profile,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// Quality compares tests and coverage from before the edit with the same commands after it.
type Quality struct {
	Before           []CoverageSample `json:"before"`
	After            []CoverageSample `json:"after"`
	BaselinePassed   int              `json:"baseline_passed"`
	BaselineFailed   int              `json:"baseline_failed"`
	BaselineFailures []TestRef        `json:"baseline_failures"`
	AfterPassed      int              `json:"after_passed"`
	AfterFailed      int              `json:"after_failed"`
	AfterFailures    []TestRef        `json:"after_failures"`
	AfterRecorded    bool             `json:"after_recorded"`
	NewlyFailed      []TestRef        `json:"newly_failed"`
	StillFailing     []TestRef        `json:"still_failing"`
	Fixed            []TestRef        `json:"fixed"`
	Note             string           `json:"note,omitempty"`
}

// ValidationResult is one gate execution.
type ValidationResult struct {
	ID         string        `json:"id"`
	Validator  string        `json:"validator"`
	Scope      string        `json:"scope"`
	Status     string        `json:"status"`
	Required   bool          `json:"required"`
	Command    []string      `json:"command,omitempty"`
	ExitCode   int           `json:"exit_code"`
	Duration   time.Duration `json:"duration"`
	StartedAt  time.Time     `json:"started_at"`
	EndedAt    time.Time     `json:"ended_at"`
	OutputRef  string        `json:"output_ref,omitempty"`
	LogExcerpt string        `json:"log_excerpt,omitempty"`
	Reason     string        `json:"reason,omitempty"`
}

// PolicyDecision records an allow or deny.
type PolicyDecision struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	Path    string `json:"path,omitempty"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// Capability is one adapter operation and its support level.
type Capability struct {
	Adapter    string `json:"adapter"`
	Kind       string `json:"kind"`
	Operation  string `json:"operation"`
	Support    string `json:"support"`
	Depth      string `json:"depth"`
	Tool       string `json:"tool,omitempty"`
	Version    string `json:"version,omitempty"`
	Confidence string `json:"confidence"`
	Notes      string `json:"notes,omitempty"`
}

// ToolFingerprint records an external tool observation.
type ToolFingerprint struct {
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

// StageRecord is one lifecycle stage.
type StageRecord struct {
	Name      string     `json:"name"`
	State     string     `json:"state"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	SkippedBy string     `json:"skipped_by,omitempty"`
}

// RunEvent is an append-only lifecycle fact.
type RunEvent struct {
	SchemaVersion string    `json:"schema_version"`
	RunID         string    `json:"run_id"`
	Sequence      int64     `json:"sequence"`
	Timestamp     time.Time `json:"timestamp"`
	Stage         string    `json:"stage"`
	Attempt       int       `json:"attempt"`
	Operation     string    `json:"operation"`
	State         string    `json:"state"`
	Actor         string    `json:"actor"`
	Message       string    `json:"message"`
	EvidenceRef   string    `json:"evidence_ref,omitempty"`
	Redaction     string    `json:"redaction"`
}

// Iteration is one repair-loop pass.
type Iteration struct {
	Number       int      `json:"number"`
	Diagnostics  int      `json:"diagnostics"`
	Applied      int      `json:"applied"`
	Fingerprints []string `json:"fingerprints"`
	NoProgress   bool     `json:"no_progress"`
	Stopped      string   `json:"stopped,omitempty"`
}

// Artifact paths are relative to the workspace when possible.
type Artifacts struct {
	JSON     string `json:"json,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	HTML     string `json:"html,omitempty"`
	Patch    string `json:"patch,omitempty"`
}

// Checkpoint captures hashes used for resume.
type Checkpoint struct {
	PlanHash   string            `json:"plan_hash"`
	FileHashes map[string]string `json:"file_hashes"`
	Stage      string            `json:"stage"`
}

// PathMapping is an evidenced temp-to-source mapping.
type PathMapping struct {
	TempPath        string `json:"temp_path"`
	TransformedPath string `json:"transformed_path,omitempty"`
	OriginalPath    string `json:"original_path,omitempty"`
	Confidence      string `json:"confidence"`
	Evidence        string `json:"evidence"`
}

// WorkflowExplanation is a partial static Copybara view.
type WorkflowExplanation struct {
	ConfigPath    string          `json:"config_path"`
	ConfigHash    string          `json:"config_hash"`
	Name          string          `json:"name"`
	Mode          string          `json:"mode,omitempty"`
	Origin        string          `json:"origin,omitempty"`
	Destination   string          `json:"destination,omitempty"`
	OriginFiles   []string        `json:"origin_files"`
	OriginExclude []string        `json:"origin_exclude"`
	DestFiles     []string        `json:"dest_files"`
	DestExclude   []string        `json:"dest_exclude"`
	Transforms    []TransformStep `json:"transforms"`
	Loads         []string        `json:"loads"`
	Unresolved    []string        `json:"unresolved"`
	Confidence    string          `json:"confidence"`
	Notes         []string        `json:"notes"`
}

// TransformStep is one ordered transformation.
type TransformStep struct {
	Order   int      `json:"order"`
	Name    string   `json:"name"`
	Args    []string `json:"args"`
	Known   bool     `json:"known"`
	Summary string   `json:"summary"`
}

// CopybaraView is the static explanation stored on a run.
type CopybaraView struct {
	Configs   []string              `json:"configs"`
	Workflows []WorkflowExplanation `json:"workflows"`
	Mappings  []PathMapping         `json:"mappings"`
	Tool      ToolFingerprint       `json:"tool"`
}

// AIProposal is a review item. It is not a file write.
type AIProposal struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Summary    string `json:"summary"`
	Applied    bool   `json:"applied"`
	Reason     string `json:"reason"`
	Confidence string `json:"confidence"`
}

// RunReport is the single source of truth for a campaign.
type RunReport struct {
	SchemaVersion    string             `json:"schema_version"`
	ID               string             `json:"id"`
	PlanID           string             `json:"plan_id,omitempty"`
	PlanHash         string             `json:"plan_hash,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	ExportedAt       *time.Time         `json:"exported_at,omitempty"`
	DataAsOf         time.Time          `json:"data_as_of"`
	Outcome          string             `json:"outcome"`
	OutcomeReason    string             `json:"outcome_reason,omitempty"`
	DryRun           bool               `json:"dry_run"`
	NoAI             bool               `json:"no_ai"`
	Offline          bool               `json:"offline"`
	WorkspaceRoot    string             `json:"workspace_root"`
	Provider         string             `json:"provider"`
	ProviderSource   string             `json:"provider_source"`
	Target           Target             `json:"target"`
	BaselineRevision string             `json:"baseline_revision,omitempty"`
	Stages           []StageRecord      `json:"stages"`
	Inventory        Inventory          `json:"inventory"`
	Plan             *Plan              `json:"plan,omitempty"`
	Changes          []Change           `json:"changes"`
	Diagnostics      []Diagnostic       `json:"diagnostics"`
	Groups           []DiagnosticGroup  `json:"groups"`
	Iterations       []Iteration        `json:"iterations"`
	Validations      []ValidationResult `json:"validations"`
	Quality          *Quality           `json:"quality,omitempty"`
	Impact           *Impact            `json:"impact,omitempty"`
	SCM              *SCMResult         `json:"scm,omitempty"`
	PolicyDecisions  []PolicyDecision   `json:"policy_decisions"`
	Capabilities     []Capability       `json:"capabilities"`
	Tools            []ToolFingerprint  `json:"tools"`
	Artifacts        Artifacts          `json:"artifacts"`
	Checksums        map[string]string  `json:"checksums"`
	Unresolved       []string           `json:"unresolved"`
	ManualReview     []string           `json:"manual_review"`
	NextStep         string             `json:"next_step"`
	Checkpoint       Checkpoint         `json:"checkpoint"`
	Copybara         *CopybaraView      `json:"copybara,omitempty"`
	AIProposals      []AIProposal       `json:"ai_proposals"`
	RedactionNotice  string             `json:"redaction_notice"`
	ResumeOf         string             `json:"resume_of,omitempty"`
}

// StageState returns the recorded state or pending.
func (r *RunReport) StageState(name string) string {
	for _, s := range r.Stages {
		if s.Name == name {
			return s.State
		}
	}
	return StagePending
}

// Node and Edge are graph elements.
type Node struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// Edge connects two nodes.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// Graph is a dependency and impact graph.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// ToolRun is one process execution.
type ToolRun struct {
	Argv      []string      `json:"argv"`
	Dir       string        `json:"dir"`
	ExitCode  int           `json:"exit_code"`
	Stdout    string        `json:"stdout,omitempty"`
	Stderr    string        `json:"stderr,omitempty"`
	Duration  time.Duration `json:"duration"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
	Err       string        `json:"err,omitempty"`
}
