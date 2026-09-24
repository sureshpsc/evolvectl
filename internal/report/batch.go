package report

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/redact"
)

// Coverage is the one-line coverage summary used in tables.
func Coverage(samples []domain.CoverageSample) string {
	return coverageSummary(samples)
}

// BatchText renders a terminal table.
func BatchText(b *domain.BatchReport) string {
	var s strings.Builder
	fmt.Fprintf(&s, "Batch %s (%s, %d rows) from %s\n", b.ID, b.Mode, len(b.Rows), b.Source)
	for _, r := range b.Rows {
		fmt.Fprintf(&s, "%3d  %-24s %s -> %s", r.Index, r.Outcome, r.Dependency, r.To)
		if r.ToModule != "" {
			fmt.Fprintf(&s, " (%s)", r.ToModule)
		}
		if r.Workspace != "" {
			fmt.Fprintf(&s, " in %s", r.Workspace)
		}
		s.WriteString("\n")
		fmt.Fprintf(&s, "     changes %d, newly failed %d, API call sites %d, coverage %s -> %s\n", r.Changes, r.NewlyFailed, r.ImpactSites, dash(r.CoverageBefore), dash(r.CoverageAfter))
		if r.Reason != "" {
			fmt.Fprintf(&s, "     %s\n", redact.Text(r.Reason))
		}
		if r.Branch != "" || r.PRURL != "" {
			fmt.Fprintf(&s, "     branch %s %s\n", r.Branch, r.PRURL)
		}
		if r.RunID != "" {
			fmt.Fprintf(&s, "     run %s\n", r.RunID)
		}
	}
	fmt.Fprintf(&s, "succeeded %d, needs review %d, failed %d\n", b.Succeeded, b.NeedsReview, b.Failed)
	return s.String()
}

// BatchMarkdown renders a table for a tracker or an issue.
func BatchMarkdown(b *domain.BatchReport) string {
	var s strings.Builder
	fmt.Fprintf(&s, "# Evolvectl batch %s\n\n", b.ID)
	fmt.Fprintf(&s, "Mode `%s` · source `%s` · %s\n\n", b.Mode, b.Source, b.CreatedAt.Format(time.RFC3339))
	s.WriteString("| # | Dependency | Target | Owner | Outcome | Newly failed | Coverage | API sites | Run | PR |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range b.Rows {
		target := r.To
		if r.ToModule != "" {
			target = r.ToModule + " " + r.To
		}
		fmt.Fprintf(&s, "| %d | `%s` | %s | %s | `%s` | %d | %s → %s | %d | %s | %s |\n",
			r.Index, r.Dependency, target, r.Owner, r.Outcome, r.NewlyFailed, dash(r.CoverageBefore), dash(r.CoverageAfter), r.ImpactSites, r.RunID, r.PRURL)
	}
	fmt.Fprintf(&s, "\nsucceeded %d, needs review %d, failed %d\n", b.Succeeded, b.NeedsReview, b.Failed)
	return s.String()
}

// BatchHTML renders one offline page.
func BatchHTML(b *domain.BatchReport) ([]byte, error) {
	var buf strings.Builder
	if err := batchTpl.Execute(&buf, b); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

var batchTpl = template.Must(template.New("batch").Funcs(template.FuncMap{
	"rfc":    func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
	"redact": redact.Text,
	"dash":   dash,
}).Parse(batchPage))

const batchPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<title>Evolvectl batch {{.ID}}</title>
<style>
:root { color-scheme: light dark; --bg:#f7f7f4; --fg:#1c1c1c; --card:#fff; --line:#d9d6cc; }
body[data-theme="dark"] { --bg:#161616; --fg:#f3f3f0; --card:#222; --line:#444; }
body { margin:0; font:16px/1.45 system-ui, sans-serif; background:var(--bg); color:var(--fg); }
header, main { padding:1rem 1.25rem; max-width:1300px; margin:0 auto; }
table { width:100%; border-collapse:collapse; background:var(--card); }
th, td { border:1px solid var(--line); padding:.35rem .5rem; text-align:left; vertical-align:top; }
.meta { color:#777; }
.succeeded { color:#0a7a33; font-weight:600; }
.succeeded_with_review, .partial { color:#9a6700; font-weight:600; }
.failed, .blocked, .cancelled { color:#b42318; font-weight:600; }
</style>
</head>
<body>
<header>
<h1>Batch {{.ID}}</h1>
<p class="meta">{{.Mode}} · {{.Source}} · {{rfc .CreatedAt}} · succeeded {{.Succeeded}} · needs review {{.NeedsReview}} · failed {{.Failed}}</p>
<label>Filter <input id="q" type="search" aria-label="Filter rows"></label>
<button id="theme" type="button">Toggle theme</button>
</header>
<main>
<table><thead><tr><th>#</th><th>Dependency</th><th>Target</th><th>Workspace</th><th>Owner</th><th>Outcome</th><th>Changes</th><th>Newly failed</th><th>Coverage before → after</th><th>API sites</th><th>Branch / PR</th><th>Run</th></tr></thead><tbody>
{{range .Rows}}<tr><td>{{.Index}}</td><td>{{.Dependency}}</td><td>{{if .ToModule}}{{.ToModule}} {{end}}{{.To}}</td><td>{{dash .Workspace}}</td><td>{{dash .Owner}}</td><td class="{{.Outcome}}">{{.Outcome}}<br><span class="meta">{{redact .Reason}}</span></td><td>{{.Changes}}</td><td>{{.NewlyFailed}}</td><td>{{dash .CoverageBefore}} → {{dash .CoverageAfter}}</td><td>{{.ImpactSites}}</td><td>{{.Branch}} {{.PRURL}}</td><td>{{.RunID}}<br><span class="meta">{{.Report}}</span></td></tr>{{end}}
</tbody></table>
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
