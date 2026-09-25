package report

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/redact"
)

// JSON renders the run snapshot.
func JSON(r *domain.RunReport) ([]byte, error) {
	cp := *r
	now := time.Now().UTC()
	cp.ExportedAt = &now
	b, err := json.MarshalIndent(&cp, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Markdown renders the run snapshot.
func Markdown(r *domain.RunReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evolvectl run %s\n\n", r.ID)
	fmt.Fprintf(&b, "- Outcome: `%s`\n", r.Outcome)
	fmt.Fprintf(&b, "- Reason: %s\n", redact.Text(r.OutcomeReason))
	fmt.Fprintf(&b, "- Target: `%s` %s -> `%s`\n", r.Target.Ecosystem, r.Target.Name, r.Target.To)
	fmt.Fprintf(&b, "- Provider: %s (%s)\n", r.Provider, r.ProviderSource)
	fmt.Fprintf(&b, "- Dry run: %v\n", r.DryRun)
	fmt.Fprintf(&b, "- No AI: %v\n", r.NoAI)
	fmt.Fprintf(&b, "- Data as of: %s\n", r.DataAsOf.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Created: %s\n\n", r.CreatedAt.Format(time.RFC3339))
	b.WriteString("## Stages\n\n")
	for _, s := range r.Stages {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.State)
	}
	b.WriteString("\n## Changes\n\n")
	if len(r.Changes) == 0 {
		b.WriteString("No changes recorded.\n")
	}
	for _, c := range r.Changes {
		fmt.Fprintf(&b, "- `%s` %s via `%s` (%s) %s\n", c.ID, c.File, c.RecipeID, c.Confidence, c.Reason)
	}
	if r.Quality != nil {
		b.WriteString("\n## Tests and coverage\n\n")
		b.WriteString(qualityText(r.Quality))
		b.WriteString("\n")
	}
	if r.Impact != nil {
		b.WriteString("\n## API impact\n\n```\n")
		b.WriteString(ImpactText(r.Impact))
		b.WriteString("```\n")
	}
	b.WriteString("\n## Validations\n\n")
	for _, v := range r.Validations {
		fmt.Fprintf(&b, "- %s: %s required=%v", v.Validator, v.Status, v.Required)
		if v.Reason != "" {
			fmt.Fprintf(&b, " — %s", redact.Text(v.Reason))
		}
		b.WriteString("\n")
	}
	if len(r.ManualReview) > 0 {
		b.WriteString("\n## Review\n\n")
		for _, item := range r.ManualReview {
			fmt.Fprintf(&b, "- %s\n", redact.Text(item))
		}
	}
	fmt.Fprintf(&b, "\n%s\n", r.RedactionNotice)
	return b.String()
}

// TextSummary renders a terminal summary.
func TextSummary(r *domain.RunReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run %s\n", r.ID)
	fmt.Fprintf(&b, "Outcome              %s\n", r.Outcome)
	if r.OutcomeReason != "" {
		fmt.Fprintf(&b, "Reason               %s\n", r.OutcomeReason)
	}
	fmt.Fprintf(&b, "Dependency           %s:%s\n", r.Target.Ecosystem, r.Target.Name)
	fmt.Fprintf(&b, "Target               %s\n", r.Target.To)
	fmt.Fprintf(&b, "Provider             %s [%s]\n", r.Provider, r.ProviderSource)
	fmt.Fprintf(&b, "Dry run              %v\n", r.DryRun)
	if r.Plan != nil {
		fmt.Fprintf(&b, "Projects             %d\n", len(r.Plan.Projects))
		fmt.Fprintf(&b, "Files                %d\n", len(r.Plan.Files))
		fmt.Fprintf(&b, "Recipes              %d\n", len(r.Plan.Recipes))
		fmt.Fprintf(&b, "Risk                 %s\n", r.Plan.Risk.Level)
	}
	fmt.Fprintf(&b, "Changes              %d\n", len(r.Changes))
	fmt.Fprintf(&b, "Diagnostic groups    %d\n", len(r.Groups))
	if r.Quality != nil {
		b.WriteString(qualityText(r.Quality))
	}
	if r.Impact != nil && r.Impact.Status == "recorded" {
		fmt.Fprintf(&b, "API impact           %d removed, %d changed; %d call site(s) in %d file(s)\n", r.Impact.Removed, r.Impact.Changed, len(r.Impact.Sites), r.Impact.FilesAffected)
	}
	b.WriteString("Validation\n")
	for _, v := range r.Validations {
		fmt.Fprintf(&b, "  %-22s %s\n", v.Validator, v.Status)
	}
	if len(r.ManualReview) > 0 {
		b.WriteString("Review\n")
		for _, item := range r.ManualReview {
			fmt.Fprintf(&b, "  %s\n", redact.Text(item))
		}
	}
	if r.SCM != nil {
		b.WriteString(scmText(r.SCM))
	}
	if r.NextStep != "" {
		fmt.Fprintf(&b, "Next                 %s\n", r.NextStep)
	}
	return b.String()
}

// PRBody is a short Markdown summary for a commit body or pull request. The full report stays local.
func PRBody(r *domain.RunReport) string {
	var b strings.Builder
	t := r.Target
	switch {
	case t.VendorDir != "":
		fmt.Fprintf(&b, "Vendors `%s` %s into `%s` and points `go.mod` at it with a replace.\n\n", t.Module(), t.To, t.VendorDir)
	case t.ToModule != "" && t.ToModule != t.Name:
		fmt.Fprintf(&b, "Moves `%s` to `%s` %s and rewrites the imports.\n\n", t.Name, t.ToModule, t.To)
	default:
		fmt.Fprintf(&b, "Upgrades `%s` to %s.\n\n", t.Name, t.To)
	}
	if t.UseDir != "" {
		fmt.Fprintf(&b, "Uses the copy already imported into `%s`.\n\n", t.UseDir)
	}
	fmt.Fprintf(&b, "- Outcome: `%s` — %s\n", r.Outcome, redact.Text(r.OutcomeReason))
	if r.Plan != nil && len(r.Plan.CurrentVersions) > 0 {
		fmt.Fprintf(&b, "- From: %s\n", strings.Join(r.Plan.CurrentVersions, ", "))
	}
	fmt.Fprintf(&b, "- Files changed: %d\n", countFiles(r.Changes))
	if q := r.Quality; q != nil {
		fmt.Fprintf(&b, "- Tests before: %d passed, %d failed. After: %d passed, %d failed. Newly failed: %d\n", q.BaselinePassed, q.BaselineFailed, q.AfterPassed, q.AfterFailed, len(q.NewlyFailed))
		fmt.Fprintf(&b, "- Coverage before: %s. After: %s\n", coverageSummary(q.Before), coverageSummary(q.After))
	}
	if imp := r.Impact; imp != nil && imp.Status == "recorded" {
		fmt.Fprintf(&b, "- API: %d removed, %d changed exported symbols; %d call site(s) in %d file(s)\n", imp.Removed, imp.Changed, len(imp.Sites), imp.FilesAffected)
	}
	b.WriteString("\n### Gates\n\n")
	for _, v := range r.Validations {
		fmt.Fprintf(&b, "- %s: %s\n", v.Validator, v.Status)
	}
	if len(r.ManualReview) > 0 {
		b.WriteString("\n### Review\n\n")
		for _, item := range r.ManualReview {
			fmt.Fprintf(&b, "- %s\n", redact.Text(item))
		}
	}
	fmt.Fprintf(&b, "\nEvolvectl run `%s`. The full report is `.evolvectl/runs/%s/report.html` in the workspace that ran it.\n", r.ID, r.ID)
	s := b.String()
	if len(s) > 60000 {
		s = s[:60000] + "\n\n(truncated)\n"
	}
	return s
}

func countFiles(changes []domain.Change) int {
	seen := map[string]bool{}
	for _, c := range changes {
		seen[c.File] = true
	}
	return len(seen)
}

// ImpactText renders the API comparison and the affected call sites.
func ImpactText(imp *domain.Impact) string {
	var b strings.Builder
	if imp == nil {
		b.WriteString("API impact           not computed (only Go modules are compared)\n")
		return b.String()
	}
	fmt.Fprintf(&b, "API impact           %s@%s -> %s@%s\n", imp.Module, imp.From, imp.ToModule, imp.To)
	if imp.Status != "recorded" {
		fmt.Fprintf(&b, "  unavailable: %s\n", redact.Text(imp.Reason))
		return b.String()
	}
	fmt.Fprintf(&b, "  %d removed, %d changed, %d added exported symbols; %d method change(s)\n", imp.Removed, imp.Changed, imp.Added, imp.MethodsChanged)
	for _, c := range imp.Changes {
		if c.Change == "added" {
			continue
		}
		name := c.Symbol
		if c.Package != "" {
			name = c.Package + "." + c.Symbol
		}
		fmt.Fprintf(&b, "  %-9s %-6s %s", c.Change, c.Kind, name)
		switch {
		case c.Change == "changed":
			fmt.Fprintf(&b, "\n              was %s\n              now %s\n", c.Before, c.After)
		case c.Change == "indirect":
			fmt.Fprintf(&b, " (%s)\n", c.After)
		default:
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "Call sites           %d in %d file(s)\n", len(imp.Sites), imp.FilesAffected)
	for _, s := range imp.Sites {
		fmt.Fprintf(&b, "  %s:%d:%d %s %s\n", s.File, s.Line, s.Column, s.Symbol, s.Change)
	}
	if imp.Note != "" {
		fmt.Fprintf(&b, "Note                 %s\n", imp.Note)
	}
	return b.String()
}

func scmText(s *domain.SCMResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Branch               %s (base %s)\n", s.Branch, s.Base)
	if s.Commit != "" {
		fmt.Fprintf(&b, "Commit               %s\n", s.Commit)
	}
	if s.PRURL != "" {
		fmt.Fprintf(&b, "Pull request         %s (draft=%v)\n", s.PRURL, s.Draft)
	}
	if s.Note != "" {
		fmt.Fprintf(&b, "Note                 %s\n", redact.Text(s.Note))
	}
	return b.String()
}

func qualityText(q *domain.Quality) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tests before         %d passed, %d failed\n", q.BaselinePassed, q.BaselineFailed)
	if q.AfterRecorded {
		fmt.Fprintf(&b, "Tests after          %d passed, %d failed\n", q.AfterPassed, q.AfterFailed)
	}
	fmt.Fprintf(&b, "Coverage before      %s\n", coverageSummary(q.Before))
	fmt.Fprintf(&b, "Coverage after       %s\n", coverageSummary(q.After))
	fmt.Fprintf(&b, "Newly failed         %d\n", len(q.NewlyFailed))
	for _, t := range q.NewlyFailed {
		fmt.Fprintf(&b, "  %s %s\n", t.Package, t.Name)
	}
	fmt.Fprintf(&b, "Still failing        %d\n", len(q.StillFailing))
	for _, t := range q.StillFailing {
		fmt.Fprintf(&b, "  %s %s\n", t.Package, t.Name)
	}
	return b.String()
}

func coverageSummary(samples []domain.CoverageSample) string {
	for _, s := range samples {
		if s.Tool == "go tool cover" && s.HasPercent {
			return fmt.Sprintf("%.1f%% statements (%s)", s.Percent, s.Scope)
		}
	}
	for _, s := range samples {
		if s.HasPercent {
			return fmt.Sprintf("%.1f%% (%s %s)", s.Percent, s.Tool, s.Scope)
		}
	}
	if len(samples) == 0 {
		return "not recorded"
	}
	return samples[0].Status + " " + samples[0].Reason
}

// InventoryText renders a scan summary.
func InventoryText(inv domain.Inventory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository\n")
	fmt.Fprintf(&b, "  Type               %s\n", inv.RepoType)
	fmt.Fprintf(&b, "  Projects           %d\n", len(inv.Projects))
	fmt.Fprintf(&b, "  Files indexed      %d\n", inv.FilesIndexed)
	fmt.Fprintf(&b, "  Cache hits         %d\n", inv.CacheHits)
	b.WriteString("Languages\n")
	for _, l := range inv.Languages {
		fmt.Fprintf(&b, "  %s\n", l)
	}
	b.WriteString("Workspace\n")
	for _, w := range inv.WorkspaceAdapters {
		fmt.Fprintf(&b, "  %s\n", w)
	}
	b.WriteString("Build systems\n")
	for _, w := range inv.BuildSystems {
		fmt.Fprintf(&b, "  %s\n", w)
	}
	if len(inv.WorkspaceModules) > 0 {
		b.WriteString("Go workspace\n")
		for _, m := range inv.WorkspaceModules {
			fmt.Fprintf(&b, "  %s\n", m)
		}
	}
	fmt.Fprintf(&b, "Dependencies         %d\n", len(inv.Dependencies))
	if len(inv.Warnings) > 0 {
		b.WriteString("Warnings\n")
		for _, w := range inv.Warnings {
			fmt.Fprintf(&b, "  %s\n", w)
		}
	}
	return b.String()
}

// HTML renders a self-contained offline report.
func HTML(r *domain.RunReport) ([]byte, error) {
	now := time.Now().UTC()
	cp := *r
	cp.ExportedAt = &now
	var buf strings.Builder
	if err := htmlTpl.Execute(&buf, &cp); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// InventoryHTML renders a scan as one offline page.
func InventoryHTML(inv domain.Inventory) ([]byte, error) {
	var buf strings.Builder
	if err := invTpl.Execute(&buf, inv); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

var invTpl = template.Must(template.New("inventory").Funcs(template.FuncMap{
	"rfc": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	},
	"redact": redact.Text,
	"join":   strings.Join,
}).Parse(inventoryPage))

const inventoryPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<title>Evolvectl scan</title>
<style>
:root { color-scheme: light dark; --bg:#f7f7f4; --fg:#1c1c1c; --card:#fff; --line:#d9d6cc; }
body[data-theme="dark"] { --bg:#161616; --fg:#f3f3f0; --card:#222; --line:#444; }
body { margin:0; font:16px/1.45 system-ui, sans-serif; background:var(--bg); color:var(--fg); }
header, main { padding:1rem 1.25rem; max-width:1100px; margin:0 auto; }
table { width:100%; border-collapse:collapse; background:var(--card); }
th, td { border:1px solid var(--line); padding:.35rem .5rem; text-align:left; vertical-align:top; }
.meta { color:#555; }
</style>
</head>
<body>
<header>
<h1>Dependency inventory</h1>
<p class="meta">{{.RepoType}} · {{.Root}} · scanned {{rfc .ScannedAt}} · {{.FilesIndexed}} files · {{len .Dependencies}} dependencies</p>
<p class="meta">Versions are the ones declared in manifests. registry_unavailable means no registry was queried.</p>
<label>Filter <input id="q" type="search" aria-label="Filter tables"></label>
<button id="theme" type="button">Toggle theme</button>
</header>
<main>
<section><h2>Projects</h2>
<table><thead><tr><th>Language</th><th>Root</th><th>Build</th><th>Manifests</th></tr></thead><tbody>
{{range .Projects}}<tr><td>{{.Language}}</td><td>{{.Root}}</td><td>{{.Build}}</td><td>{{len .Manifests}}</td></tr>{{end}}
</tbody></table>
{{if .WorkspaceModules}}<p>go.work modules: {{join .WorkspaceModules ", "}}</p>{{end}}
</section>
<section><h2>Dependencies</h2>
<table><thead><tr><th>Ecosystem</th><th>Name</th><th>Version</th><th>Direct</th><th>Status</th><th>Manifest</th></tr></thead><tbody>
{{range .Dependencies}}<tr><td>{{.Ecosystem}}</td><td>{{.Name}}</td><td>{{.CurrentVersion}}</td><td>{{.Direct}}</td><td>{{.Status}}</td><td>{{.Manifest}}</td></tr>{{end}}
</tbody></table></section>
{{if .Warnings}}<section><h2>Warnings</h2><ul>{{range .Warnings}}<li>{{redact .}}</li>{{end}}</ul></section>{{end}}
</main>
<script>
document.getElementById('theme').addEventListener('click', function () {
  var b = document.body;
  b.setAttribute('data-theme', b.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
});
document.getElementById('q').addEventListener('input', function () {
  var v = this.value.toLowerCase();
  var rows = document.querySelectorAll('tbody tr');
  for (var i = 0; i < rows.length; i++) {
    rows[i].hidden = rows[i].innerText.toLowerCase().indexOf(v) === -1;
  }
});
</script>
</body>
</html>
`

var htmlTpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"rfc": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	},
	"ptr": func(t *time.Time) string {
		if t == nil || t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	},
	"join":   strings.Join,
	"plus":   func(n int) int { return n + 1 },
	"redact": redact.Text,
	"pct": func(s domain.CoverageSample) string {
		if !s.HasPercent {
			return s.Status
		}
		return fmt.Sprintf("%.1f%%", s.Percent)
	},
	"testref": func(t domain.TestRef) string {
		if t.Package == "" || t.Package == t.Name {
			return t.Name
		}
		return t.Package + " " + t.Name
	},
}).Parse(htmlPage))

const htmlPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'">
<title>Evolvectl run {{.ID}}</title>
<style>
:root { color-scheme: light dark; --bg:#f7f7f4; --fg:#1c1c1c; --card:#fff; --line:#d9d6cc; --accent:#0b5; }
body[data-theme="dark"] { --bg:#161616; --fg:#f3f3f0; --card:#222; --line:#444; }
body { margin:0; font:16px/1.45 system-ui, sans-serif; background:var(--bg); color:var(--fg); }
header, main { padding:1rem 1.25rem; max-width:1100px; margin:0 auto; }
nav a { margin-right:.75rem; }
table { width:100%; border-collapse:collapse; background:var(--card); }
th, td { border:1px solid var(--line); padding:.35rem .5rem; text-align:left; vertical-align:top; }
h1,h2 { line-height:1.2; }
.meta { color:#555; }
pre { overflow:auto; background:var(--card); padding:.75rem; border:1px solid var(--line); }
.skip { position:absolute; left:-999px; }
.skip:focus { left:.5rem; top:.5rem; background:#fff; }
</style>
</head>
<body>
<a class="skip" href="#content">Skip to content</a>
<header>
<h1>Evolvectl run {{.ID}}</h1>
<p class="meta">Outcome <strong>{{.Outcome}}</strong> — {{redact .OutcomeReason}}</p>
<p class="meta">Data as of {{rfc .DataAsOf}} · Exported {{ptr .ExportedAt}} · Created {{rfc .CreatedAt}}</p>
<p class="meta">Target {{.Target.Ecosystem}}:{{.Target.Name}} → {{.Target.To}} · Provider {{.Provider}} ({{.ProviderSource}}) · dry-run={{.DryRun}} · no-ai={{.NoAI}}</p>
<nav>
<a href="#stages">Stages</a>
<a href="#dependencies">Dependencies</a>
<a href="#plan">Plan</a>
<a href="#changes">Changes</a>
<a href="#diagnostics">Diagnostics</a>
<a href="#validations">Validations</a>
<a href="#quality">Tests and coverage</a>
<a href="#impact">API impact</a>
<a href="#copybara">Copybara</a>
<a href="#artifacts">Artifacts</a>
</nav>
<label>Filter <input id="q" type="search" aria-label="Filter tables"></label>
<button id="theme" type="button">Toggle theme</button>
</header>
<main id="content">
<section id="stages"><h2>Stages</h2>
<table><thead><tr><th>Stage</th><th>State</th><th>Started</th><th>Ended</th><th>Reason</th></tr></thead><tbody>
{{range .Stages}}<tr><td>{{.Name}}</td><td>{{.State}}</td><td>{{ptr .StartedAt}}</td><td>{{ptr .EndedAt}}</td><td>{{redact .Reason}}</td></tr>{{end}}
</tbody></table></section>
<section id="dependencies"><h2>Dependencies <span class="meta">({{len .Inventory.Dependencies}})</span></h2>
<table><thead><tr><th>Ecosystem</th><th>Name</th><th>Version</th><th>Direct</th><th>Status</th><th>Manifest</th></tr></thead><tbody>
{{range .Inventory.Dependencies}}<tr><td>{{.Ecosystem}}</td><td>{{.Name}}</td><td>{{.CurrentVersion}}</td><td>{{.Direct}}</td><td>{{.Status}}</td><td>{{.Manifest}}</td></tr>{{end}}
</tbody></table>
<p class="meta">Status registry_unavailable means no registry was queried. It does not mean the package is safe or current.</p>
</section>
<section id="plan"><h2>Plan</h2>
{{if .Plan}}
<p>Plan {{.Plan.ID}} hash {{.Plan.Hash}} · risk {{.Plan.Risk.Level}} · confidence {{.Plan.Risk.Confidence}} · unknown patterns {{.Plan.Risk.UnknownPatterns}}</p>
<p>Projects: {{join .Plan.Projects ", "}}</p>
<p>Files: {{len .Plan.Files}} · Recipes: {{join .Plan.Recipes ", "}}</p>
<ul>{{range .Plan.ReviewPoints}}<li>{{redact .}}</li>{{end}}</ul>
<ul>{{range .Plan.CapabilityNotes}}<li>{{.}}</li>{{end}}</ul>
{{else}}<p>No plan recorded.</p>{{end}}
</section>
<section id="changes"><h2>Changes <span class="meta">({{len .Changes}})</span></h2>
<table><thead><tr><th>ID</th><th>File</th><th>Recipe</th><th>Confidence</th><th>Reason</th></tr></thead><tbody>
{{range .Changes}}<tr><td>{{.ID}}</td><td>{{.File}}</td><td>{{.RecipeID}}</td><td>{{.Confidence}}</td><td>{{redact .Reason}}</td></tr>{{end}}
</tbody></table>
{{range .Changes}}{{if .Diff}}<h3>{{.File}}</h3><pre>{{redact .Diff}}</pre>{{end}}{{end}}
</section>
<section id="diagnostics"><h2>Diagnostics <span class="meta">({{len .Groups}} groups / {{len .Diagnostics}} rows)</span></h2>
<table><thead><tr><th>Count</th><th>Category</th><th>Message</th><th>Files</th></tr></thead><tbody>
{{range .Groups}}<tr><td>{{.Count}}</td><td>{{.Category}}</td><td>{{redact .Message}}</td><td>{{join .Files ", "}}</td></tr>{{end}}
</tbody></table></section>
<section id="validations"><h2>Validations</h2>
<table><thead><tr><th>Gate</th><th>Status</th><th>Required</th><th>Exit</th><th>Reason</th></tr></thead><tbody>
{{range .Validations}}<tr><td>{{.Validator}}</td><td>{{.Status}}</td><td>{{.Required}}</td><td>{{.ExitCode}}</td><td>{{redact .Reason}}</td></tr>{{end}}
</tbody></table></section>
<section id="quality"><h2>Tests and coverage</h2>
{{if .Quality}}
<p>Before: {{.Quality.BaselinePassed}} passed, {{.Quality.BaselineFailed}} failed. After: {{.Quality.AfterPassed}} passed, {{.Quality.AfterFailed}} failed.</p>
<p>These lists compare the same commands run before the edit and after it. A test that was already failing stays in still failing.</p>
<h3>Coverage</h3>
<table><thead><tr><th>When</th><th>Tool</th><th>Scope</th><th>Coverage</th><th>Profile</th><th>Note</th></tr></thead><tbody>
{{range .Quality.Before}}<tr><td>before</td><td>{{.Tool}}</td><td>{{.Scope}}</td><td>{{pct .}}</td><td>{{.Profile}}</td><td>{{redact .Reason}}</td></tr>{{end}}
{{range .Quality.After}}<tr><td>after</td><td>{{.Tool}}</td><td>{{.Scope}}</td><td>{{pct .}}</td><td>{{.Profile}}</td><td>{{redact .Reason}}</td></tr>{{end}}
</tbody></table>
<h3>Newly failed <span class="meta">({{len .Quality.NewlyFailed}})</span></h3>
<ul>{{range .Quality.NewlyFailed}}<li>{{testref .}}</li>{{else}}<li>None</li>{{end}}</ul>
<h3>Still failing <span class="meta">({{len .Quality.StillFailing}})</span></h3>
<ul>{{range .Quality.StillFailing}}<li>{{testref .}}</li>{{else}}<li>None</li>{{end}}</ul>
<h3>Fixed since the baseline <span class="meta">({{len .Quality.Fixed}})</span></h3>
<ul>{{range .Quality.Fixed}}<li>{{testref .}}</li>{{else}}<li>None</li>{{end}}</ul>
{{if .Quality.Note}}<p>{{.Quality.Note}}</p>{{end}}
{{else}}<p>No before/after test record. Plan-only runs do not execute tests.</p>{{end}}
</section>
<section id="impact"><h2>API impact</h2>
{{with .Impact}}
<p>{{.Module}}@{{.From}} → {{.ToModule}}@{{.To}} · status {{.Status}}{{if .Reason}} — {{redact .Reason}}{{end}}</p>
{{if eq .Status "recorded"}}
<p>{{.Removed}} removed, {{.Changed}} changed, {{.Added}} added exported symbols · {{.MethodsChanged}} method change(s) · {{len .Sites}} call site(s) in {{.FilesAffected}} file(s)</p>
<h3>Call sites</h3>
<table><thead><tr><th>File</th><th>Line</th><th>Symbol</th><th>Change</th></tr></thead><tbody>
{{range .Sites}}<tr><td>{{.File}}</td><td>{{.Line}}</td><td>{{if .Package}}{{.Package}}.{{end}}{{.Symbol}}</td><td>{{.Change}}</td></tr>{{else}}<tr><td colspan="4">No workspace reference to a removed or changed symbol.</td></tr>{{end}}
</tbody></table>
<h3>Exported API differences</h3>
<table><thead><tr><th>Change</th><th>Kind</th><th>Symbol</th><th>Before</th><th>After</th></tr></thead><tbody>
{{range .Changes}}<tr><td>{{.Change}}</td><td>{{.Kind}}</td><td>{{if .Package}}{{.Package}}.{{end}}{{.Symbol}}</td><td><code>{{.Before}}</code></td><td><code>{{.After}}</code></td></tr>{{end}}
</tbody></table>
{{if .Note}}<p class="meta">{{.Note}}</p>{{end}}
{{end}}
{{else}}<p>No API comparison for this run. Only Go modules are compared.</p>{{end}}
</section>
{{with .SCM}}<section id="scm"><h2>Branch and pull request</h2>
<p>Branch {{.Branch}} (base {{.Base}}) · commit {{.Commit}} · pushed={{.Pushed}}</p>
{{if .PRURL}}<p>Pull request {{.PRURL}} · draft={{.Draft}}</p>{{end}}
{{if .Note}}<p>{{redact .Note}}</p>{{end}}
</section>{{end}}
<section id="copybara"><h2>Copybara</h2>
{{if .Copybara}}
<p>Tool available: {{.Copybara.Tool.Available}} {{.Copybara.Tool.Detail}}</p>
{{range .Copybara.Workflows}}
<h3>{{.Name}}</h3>
<p>Confidence {{.Confidence}}</p>
<p>Origin {{redact .Origin}}</p>
<p>Destination {{redact .Destination}}</p>
<p>Origin files {{join .OriginFiles ", "}} exclude {{join .OriginExclude ", "}}</p>
<ol>{{range .Transforms}}<li>{{.Name}}: {{redact .Summary}}</li>{{end}}</ol>
{{if .Unresolved}}<p>Unresolved: {{join .Unresolved "; "}}</p>{{end}}
{{end}}
{{else}}<p>No copy.bara.sky was recorded for this run.</p>{{end}}
</section>
<section id="artifacts"><h2>Artifacts</h2>
<ul>
<li>JSON {{.Artifacts.JSON}}</li>
<li>Markdown {{.Artifacts.Markdown}}</li>
<li>HTML {{.Artifacts.HTML}}</li>
<li>Patch {{.Artifacts.Patch}}</li>
</ul>
{{if .ManualReview}}<h3>Review</h3><ul>{{range .ManualReview}}<li>{{redact .}}</li>{{end}}</ul>{{end}}
<p>{{.RedactionNotice}}</p>
</section>
</main>
<script>
document.getElementById('theme').addEventListener('click', function () {
  var b = document.body;
  b.setAttribute('data-theme', b.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
});
document.getElementById('q').addEventListener('input', function () {
  var v = this.value.toLowerCase();
  var rows = document.querySelectorAll('tbody tr');
  for (var i = 0; i < rows.length; i++) {
    rows[i].hidden = rows[i].innerText.toLowerCase().indexOf(v) === -1;
  }
});
</script>
</body>
</html>
`
