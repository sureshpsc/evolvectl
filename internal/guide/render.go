package guide

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// Write renders every guide page into dir.
func Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	pages := Pages()
	for _, p := range pages {
		var buf strings.Builder
		if err := layout.Execute(&buf, struct {
			Page
			All []Page
		}{p, pages}); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, p.File), []byte(buf.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func rich(s string) template.HTML {
	var b strings.Builder
	parts := strings.Split(s, "`")
	for i, p := range parts {
		if i%2 == 1 {
			b.WriteString("<code>")
			b.WriteString(template.HTMLEscapeString(p))
			b.WriteString("</code>")
			continue
		}
		b.WriteString(template.HTMLEscapeString(p))
	}
	return template.HTML(b.String())
}

var layout = template.Must(template.New("guide").Funcs(template.FuncMap{
	"rich": rich,
	"anchor": func(s string) string {
		return strings.ReplaceAll(s, " ", "-")
	},
}).Parse(layoutHTML))

const layoutHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="{{.Lead}}">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<title>{{.Title}} · evolvectl</title>
<style>
:root {
  color-scheme: light dark;
  --bg: #f3efe6;
  --fg: #1c1915;
  --muted: #5e584e;
  --card: #fffdf8;
  --line: #e3d9c8;
  --accent: #0c6b52;
  --accent-soft: #e4f3ee;
  --code: #f6f1e8;
  --warn: #8a4b12;
  --warn-bg: #fff4e5;
  --shadow: 0 1px 2px rgba(40, 30, 10, .06);
}
body[data-theme="dark"] {
  --bg: #141311;
  --fg: #f4f0e6;
  --muted: #b7b0a4;
  --card: #211f1b;
  --line: #3a352c;
  --accent: #7ddec4;
  --accent-soft: #1c3330;
  --code: #1a1916;
  --warn: #f0c48a;
  --warn-bg: #2c2418;
  --shadow: none;
}
* { box-sizing: border-box; }
body { margin: 0; font: 17px/1.55 "Segoe UI", system-ui, sans-serif; background: var(--bg); color: var(--fg); }
a { color: var(--accent); }
.skip { position: absolute; left: -999px; }
.skip:focus { left: .5rem; top: .5rem; background: var(--card); padding: .4rem .6rem; z-index: 3; }
.top {
  position: sticky; top: 0; z-index: 2;
  background: var(--bg);
  border-bottom: 1px solid var(--line);
  padding: .75rem 1.25rem .6rem;
}
.brand { font-weight: 700; text-decoration: none; letter-spacing: -.02em; font-size: 1.15rem; }
.tag { margin: .1rem 0 .45rem; color: var(--muted); font-size: .92rem; }
.top nav { display: flex; flex-wrap: wrap; gap: .35rem .8rem; }
.top nav a { text-decoration: none; color: var(--fg); font-size: .95rem; }
.top nav a[aria-current="page"] { color: var(--accent); font-weight: 650; }
.wrap { max-width: 920px; margin: 0 auto; padding: 1.5rem 1.25rem 3rem; }
.kicker { margin: 0; color: var(--accent); font-size: .78rem; letter-spacing: .08em; text-transform: uppercase; font-weight: 700; }
h1 { margin: .2rem 0 .4rem; font-size: 2rem; line-height: 1.15; letter-spacing: -.03em; }
.lead { font-size: 1.12rem; margin-top: 0; }
h2 { margin: 2rem 0 .4rem; font-size: 1.35rem; letter-spacing: -.02em; }
h3 { margin: 1.2rem 0 .3rem; font-size: 1.05rem; }
p { margin: .45rem 0; }
.muted, .note { color: var(--muted); }
.callout { background: var(--warn-bg); border: 1px solid var(--line); border-left: 4px solid var(--warn); padding: .7rem .9rem; border-radius: 8px; margin: .8rem 0; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: .75rem; margin: .8rem 0 0; }
.card {
  display: flex; flex-direction: column; gap: .25rem;
  background: var(--card); border: 1px solid var(--line); border-radius: 12px;
  padding: .9rem 1rem; text-decoration: none; color: inherit; box-shadow: var(--shadow);
}
.card:hover { border-color: var(--accent); }
.card strong { color: var(--accent); }
.card span { color: var(--muted); font-size: .95rem; }
table { width: 100%; border-collapse: collapse; background: var(--card); margin: .6rem 0 1rem; }
th, td { border: 1px solid var(--line); padding: .4rem .55rem; text-align: left; vertical-align: top; }
th { background: var(--accent-soft); }
caption { text-align: left; font-weight: 650; margin-bottom: .35rem; }
pre {
  overflow: auto; background: var(--code); border: 1px solid var(--line);
  border-radius: 10px; padding: .75rem .9rem; margin: .35rem 0;
}
code { font-family: ui-monospace, "Cascadia Code", Consolas, monospace; font-size: .9em; }
p code, li code, td code { background: var(--code); padding: .05rem .3rem; border-radius: 4px; }
figure { margin: .8rem 0 1rem; }
figcaption { font-size: .85rem; color: var(--muted); }
.toc { display: flex; flex-wrap: wrap; gap: .35rem .7rem; padding: 0; margin: .4rem 0 0; list-style: none; }
.toc a { text-decoration: none; }
.toolbar { display: flex; flex-wrap: wrap; gap: .6rem; align-items: center; margin: 1rem 0; }
input[type="search"] { font: inherit; padding: .4rem .6rem; border: 1px solid var(--line); border-radius: 8px; background: var(--card); color: inherit; min-width: 16rem; }
button { font: inherit; background: var(--card); color: inherit; border: 1px solid var(--line); border-radius: 8px; padding: .35rem .7rem; cursor: pointer; }
article.cmd { background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: .2rem 1rem .8rem; margin: .85rem 0; box-shadow: var(--shadow); }
article.cmd h2 { font-size: 1.15rem; }
.flags { margin: .3rem 0; padding-left: 1.1rem; }
footer { margin-top: 2.5rem; color: var(--muted); font-size: .9rem; border-top: 1px solid var(--line); padding-top: 1rem; }
</style>
</head>
<body>
<a class="skip" href="#content">Skip to content</a>
<header class="top">
<a class="brand" href="index.html">evolvectl</a>
<p class="tag">Upgrade codebases, not just dependency files.</p>
<nav aria-label="Guide">
{{range .All}}<a href="{{.File}}"{{if eq .File $.File}} aria-current="page"{{end}}>{{.Nav}}</a>{{end}}
</nav>
</header>
<main class="wrap" id="content">
<p class="kicker">{{.Kicker}}</p>
<h1>{{.Title}}</h1>
<p class="lead">{{rich .Lead}}</p>
{{if .Sections}}
<ul class="toc">{{range .Sections}}<li><a href="#{{.ID}}">{{.Title}}</a></li>{{end}}</ul>
{{end}}
{{range .Sections}}
<section id="{{.ID}}">
<h2>{{.Title}}</h2>
{{range .Paragraphs}}<p>{{rich .}}</p>{{end}}
{{if .Callout}}<aside class="callout">{{rich .Callout}}</aside>{{end}}
{{if .Bullets}}<ul>{{range .Bullets}}<li>{{rich .}}</li>{{end}}</ul>{{end}}
{{if .Table}}
<table>
{{if .Table.Caption}}<caption>{{.Table.Caption}}</caption>{{end}}
<thead><tr>{{range .Table.Headers}}<th>{{.}}</th>{{end}}</tr></thead>
<tbody>{{range .Table.Rows}}<tr>{{range .}}<td>{{rich .}}</td>{{end}}</tr>{{end}}</tbody>
</table>
{{end}}
{{range .Codes}}
<figure>
{{if .Title}}<figcaption>{{.Title}}</figcaption>{{end}}
<pre><code>{{.Body}}</code></pre>
{{if .Note}}<p class="note">{{rich .Note}}</p>{{end}}
</figure>
{{end}}
{{if .Links}}<div class="cards">{{range .Links}}<a class="card" href="{{.Href}}"><strong>{{.Title}}</strong><span>{{.Text}}</span></a>{{end}}</div>{{end}}
</section>
{{end}}
{{if .Commands}}
<div class="toolbar">
<label>Find a command <input id="q" type="search" placeholder="upgrade, copybara, session…" aria-label="Filter commands"></label>
<button id="theme" type="button">Toggle theme</button>
</div>
<ul class="toc">{{range .Commands}}<li><a href="#cmd-{{anchor .Name}}">{{.Name}}</a></li>{{end}}</ul>
{{range .Commands}}
<article class="cmd" id="cmd-{{anchor .Name}}" data-command="{{.Name}} {{.Summary}}">
<h2><code>evolvectl {{.Name}}</code></h2>
<p>{{rich .Summary}}</p>
{{range .Paragraphs}}<p>{{rich .}}</p>{{end}}
{{if .Flags}}<h3>Flags</h3><ul class="flags">{{range .Flags}}<li>{{rich .}}</li>{{end}}</ul>{{end}}
{{range .Examples}}
<figure>
{{if .Title}}<figcaption>{{.Title}}</figcaption>{{end}}
<pre><code>{{.Body}}</code></pre>
{{if .Note}}<p class="note">{{rich .Note}}</p>{{end}}
</figure>
{{end}}
</article>
{{end}}
{{else}}
<p><button id="theme" type="button">Toggle theme</button></p>
{{end}}
<footer>
These pages are written to <code>.evolvectl/guide</code> on your machine. They load no remote images, fonts, or scripts.
Run <code>evolvectl help</code> again after an upgrade of the evolvectl binary to refresh them.
</footer>
</main>
<script>
(function () {
  var btn = document.getElementById('theme');
  if (btn) btn.addEventListener('click', function () {
    var b = document.body;
    b.setAttribute('data-theme', b.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
  });
  var q = document.getElementById('q');
  if (!q) return;
  q.addEventListener('input', function () {
    var v = q.value.toLowerCase();
    var nodes = document.querySelectorAll('article.cmd');
    for (var i = 0; i < nodes.length; i++) {
      nodes[i].hidden = nodes[i].getAttribute('data-command').toLowerCase().indexOf(v) === -1;
    }
  });
})();
</script>
</body>
</html>
`
