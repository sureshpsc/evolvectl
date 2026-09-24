// Package graph builds a dependency and impact graph from an inventory.
package graph

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evolvectl/evolvectl/internal/domain"
)

var pyImport = regexp.MustCompile(`(?m)^(?:from|import)\s+([A-Za-z0-9_\.]+)`)

// Build constructs nodes and edges. Import edges are parsed for source files.
func Build(root string, inv domain.Inventory) domain.Graph {
	var g domain.Graph
	g.Nodes = append(g.Nodes, domain.Node{ID: "repo:.", Kind: "repository", Label: "repository"})
	for _, p := range inv.Projects {
		g.Nodes = append(g.Nodes, domain.Node{ID: p.ID, Kind: "project", Label: p.Root})
		g.Edges = append(g.Edges, domain.Edge{From: "repo:.", To: p.ID, Kind: "contains"})
	}
	for _, m := range inv.Manifests {
		id := "manifest:" + m.Path
		g.Nodes = append(g.Nodes, domain.Node{ID: id, Kind: "manifest", Label: m.Path})
		if m.ProjectID != "" {
			g.Edges = append(g.Edges, domain.Edge{From: m.ProjectID, To: id, Kind: "contains"})
		}
	}
	for _, d := range inv.Dependencies {
		g.Nodes = append(g.Nodes, domain.Node{ID: d.ID, Kind: "dependency", Label: d.Name})
		g.Edges = append(g.Edges, domain.Edge{From: "manifest:" + d.Manifest, To: d.ID, Kind: "declared_in"})
		if d.ProjectID != "" {
			g.Edges = append(g.Edges, domain.Edge{From: d.ProjectID, To: d.ID, Kind: "depends_on"})
		}
	}
	for _, f := range inv.SourceFiles {
		if f.Generated {
			continue
		}
		fid := "file:" + f.Path
		g.Nodes = append(g.Nodes, domain.Node{ID: fid, Kind: "source_file", Label: f.Path})
		if f.ProjectID != "" {
			g.Edges = append(g.Edges, domain.Edge{From: f.ProjectID, To: fid, Kind: "contains"})
		}
		imports := f.Imports
		if len(imports) == 0 {
			imports = readImports(root, f)
		}
		for _, imp := range imports {
			for _, d := range inv.Dependencies {
				if importMatches(d, imp) {
					g.Edges = append(g.Edges, domain.Edge{From: fid, To: d.ID, Kind: "imports"})
				}
			}
		}
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		if g.Edges[i].To != g.Edges[j].To {
			return g.Edges[i].To < g.Edges[j].To
		}
		return g.Edges[i].Kind < g.Edges[j].Kind
	})
	return g
}

func readImports(root string, f domain.SourceFile) []string {
	abs := filepath.Join(root, filepath.FromSlash(f.Path))
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	switch f.Language {
	case "go":
		parsed, err := parser.ParseFile(token.NewFileSet(), abs, body, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		var out []string
		for _, imp := range parsed.Imports {
			s := strings.Trim(imp.Path.Value, `"`)
			out = append(out, s)
		}
		return out
	case "python":
		var out []string
		for _, m := range pyImport.FindAllStringSubmatch(string(body), -1) {
			out = append(out, m[1])
		}
		return out
	default:
		return nil
	}
}

func importMatches(d domain.Dependency, imp string) bool {
	switch d.Ecosystem {
	case "go":
		return imp == d.Name || strings.HasPrefix(imp, d.Name+"/")
	case "python":
		base := strings.Split(d.Name, "[")[0]
		base = strings.ReplaceAll(base, "-", "_")
		return imp == base || strings.HasPrefix(imp, base+".") || imp == d.Name
	default:
		return imp == d.Name
	}
}

// FilesImporting returns source files with an imports edge to the dependency name.
func FilesImporting(g domain.Graph, depName string) []string {
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Kind == "dependency" && (n.Label == depName || strings.HasSuffix(n.ID, ":"+depName) || strings.Contains(n.ID, ":"+depName+":")) {
			ids[n.ID] = true
		}
	}
	var files []string
	seen := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind == "imports" && ids[e.To] && strings.HasPrefix(e.From, "file:") && !seen[e.From] {
			seen[e.From] = true
			files = append(files, strings.TrimPrefix(e.From, "file:"))
		}
	}
	sort.Strings(files)
	return files
}

// ProjectsConsuming returns project roots that depend on the name.
func ProjectsConsuming(g domain.Graph, inv domain.Inventory, depName string) []string {
	idset := map[string]bool{}
	for _, d := range inv.Dependencies {
		if d.Name == depName {
			idset[d.ProjectID] = true
		}
	}
	var roots []string
	seen := map[string]bool{}
	for _, p := range inv.Projects {
		if idset[p.ID] && !seen[p.Root] {
			seen[p.Root] = true
			roots = append(roots, p.Root)
		}
	}
	sort.Strings(roots)
	return roots
}
