// Package planner turns an inventory into an upgrade plan without writing source.
package planner

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/graph"
	"github.com/evolvectl/evolvectl/internal/idgen"
	"github.com/evolvectl/evolvectl/internal/manifest"
	"github.com/evolvectl/evolvectl/internal/modmove"
	"github.com/evolvectl/evolvectl/internal/recipe"
)

// Request is a plan query.
type Request struct {
	Root         string
	Inventory    domain.Inventory
	Target       domain.Target
	Recipes      []recipe.Recipe
	CopybaraSeen bool
}

// Build creates a plan. It does not modify files.
func Build(req Request) (*domain.Plan, error) {
	if req.Target.Name == "" || req.Target.To == "" {
		return nil, fmt.Errorf("dependency name and target version are required")
	}
	var matched []domain.Dependency
	versions := map[string]bool{}
	manifests := map[string]bool{}
	projects := map[string]bool{}
	for _, d := range req.Inventory.Dependencies {
		if d.Name != req.Target.Name {
			continue
		}
		if req.Target.Ecosystem != "" && d.Ecosystem != req.Target.Ecosystem {
			continue
		}
		matched = append(matched, d)
		if d.Direct {
			versions[d.CurrentVersion] = true
			manifests[d.Manifest] = true
			projects[d.ProjectID] = true
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("dependency %s was not found in the inventory", display(req.Target))
	}
	g := graph.Build(req.Root, req.Inventory)
	files := graph.FilesImporting(g, req.Target.Name)
	if len(files) == 0 {
		for _, sf := range req.Inventory.SourceFiles {
			if projects[sf.ProjectID] && !sf.Generated && sf.Language == langOf(req.Target.Ecosystem) {
				files = append(files, sf.Path)
			}
		}
		sort.Strings(files)
	}
	var recipeIDs []string
	unknown := 0
	for _, f := range files {
		hit := false
		for _, r := range req.Recipes {
			vc := recipe.VersionContext{Ecosystem: req.Target.Ecosystem, Name: req.Target.Name, Target: req.Target.To}
			if len(versions) == 1 {
				for v := range versions {
					vc.Current = v
				}
			}
			if recipe.Applicable(r, vc) && fileMightMatch(f, r) {
				hit = true
				recipeIDs = appendUnique(recipeIDs, r.Metadata.ID)
			}
		}
		if !hit && (strings.HasSuffix(f, ".go") || strings.HasSuffix(f, ".py")) {
			unknown++
		}
	}
	sort.Strings(recipeIDs)
	var vers []string
	for v := range versions {
		vers = append(vers, v)
	}
	sort.Strings(vers)
	var mans []string
	for m := range manifests {
		mans = append(mans, m)
	}
	sort.Strings(mans)
	var proj []string
	for _, p := range req.Inventory.Projects {
		if projects[p.ID] {
			proj = append(proj, p.Root)
		}
	}
	sort.Strings(proj)
	risk := domain.Risk{Level: "low", Confidence: domain.ConfidenceHigh}
	if unknown > 0 {
		risk.Level = "medium"
		risk.Confidence = domain.ConfidenceMedium
		risk.UnknownPatterns = unknown
		risk.Reasons = append(risk.Reasons, fmt.Sprintf("%d source files do not match a loaded recipe", unknown))
	}
	if req.Target.Ecosystem == "maven" || req.Target.Ecosystem == "node" {
		risk.Level = "medium"
		risk.Confidence = domain.ConfidenceMedium
		risk.Reasons = append(risk.Reasons, "semantic repair is unavailable; only the manifest declaration is updated")
	}
	validators := validatorsFor(req.Target.Ecosystem, req.Inventory)
	plan := &domain.Plan{
		ID:              idgen.New("plan_"),
		CreatedAt:       time.Now().UTC(),
		Target:          req.Target,
		CurrentVersions: vers,
		Manifests:       mans,
		Projects:        proj,
		Files:           files,
		Recipes:         recipeIDs,
		Validations:     validators,
		Risk:            risk,
		NoWrites:        true,
		BaseRevision:    req.Inventory.BaselineRevision,
		GeneratedImpact: generatedIn(req.Inventory, proj),
	}
	if req.CopybaraSeen {
		plan.CopybaraImpact = "copy.bara.sky is present. This plan does not execute Copybara."
	} else {
		plan.CopybaraImpact = "no copy.bara.sky detected"
	}
	if len(plan.GeneratedImpact) > 0 {
		plan.ReviewPoints = append(plan.ReviewPoints, "generated files are protected and will not be edited")
	}
	if req.Target.Ecosystem == "maven" || req.Target.Ecosystem == "node" {
		plan.ReviewPoints = append(plan.ReviewPoints, "review the manifest change; no semantic codemod is available for this ecosystem")
	}
	if len(req.Inventory.WorkspaceModules) > 0 {
		plan.ReviewPoints = append(plan.ReviewPoints, "go.work modules: "+strings.Join(req.Inventory.WorkspaceModules, ", "))
	}
	if req.Target.Ecosystem == "go" {
		var mans []string
		for _, m := range req.Inventory.Manifests {
			mans = append(mans, m.Path)
		}
		if reps, err := manifest.ExternalReplaces(req.Root, mans); err == nil {
			for _, r := range reps {
				plan.ReviewPoints = append(plan.ReviewPoints, manifest.FormatExternalReplace(r))
			}
		}
	}
	if req.Target.Ecosystem == "go" {
		if req.Target.ToModule != "" && req.Target.ToModule != req.Target.Name {
			plan.ReviewPoints = append(plan.ReviewPoints, fmt.Sprintf("imports move from %s to %s in %d file(s); package identifiers are unchanged, so a renamed package shows up as a compile error", req.Target.Name, req.Target.ToModule, len(files)))
		} else if s := modmove.SuggestMajorPath(req.Target.Name, req.Target.To); s != "" {
			plan.ReviewPoints = append(plan.ReviewPoints, fmt.Sprintf("%s on %s resolves as +incompatible because the path has no /vN suffix; if the project publishes %s, pass --to-module %s", req.Target.To, req.Target.Name, s, s))
		}
		if req.Target.VendorDir != "" {
			plan.ReviewPoints = append(plan.ReviewPoints, fmt.Sprintf("module source is copied into %s and wired with a replace directive; review the license and the vendored diff", req.Target.VendorDir))
		}
	}
	plan.CapabilityNotes = capabilityNotes(req.Target.Ecosystem)
	order := 1
	plan.Steps = append(plan.Steps, domain.PlanStep{ID: "step-manifest", Order: order, Action: "update manifest declaration", Target: strings.Join(mans, ", "), Capability: "dependencies.upgrade", Support: supportFor(req.Target.Ecosystem)})
	order++
	if req.Target.VendorDir != "" {
		plan.Steps = append(plan.Steps, domain.PlanStep{ID: "step-vendor", Order: order, Action: "copy " + req.Target.Module() + "@" + req.Target.To + " into " + req.Target.VendorDir + " and add a replace", Target: req.Target.VendorDir, Capability: "dependencies.vendor", Support: domain.SupportNative})
		order++
	}
	if req.Target.ToModule != "" && req.Target.ToModule != req.Target.Name {
		plan.Steps = append(plan.Steps, domain.PlanStep{ID: "step-move", Order: order, Action: "rewrite imports " + req.Target.Name + " -> " + req.Target.ToModule, Target: fmt.Sprintf("%d file(s)", len(files)), Capability: "module.move", Support: domain.SupportNative})
		order++
	}
	if len(recipeIDs) > 0 {
		plan.Steps = append(plan.Steps, domain.PlanStep{ID: "step-recipes", Order: order, Action: "apply deterministic recipes", Target: strings.Join(recipeIDs, ", "), Capability: "transform.apply", Support: domain.SupportNative})
		order++
	}
	for _, v := range validators {
		plan.Steps = append(plan.Steps, domain.PlanStep{ID: "step-" + v, Order: order, Action: "validate", Target: v, Capability: "validate.run", Support: domain.SupportNative})
		order++
	}
	plan.Hash = idgen.Short(16, plan.Target.Ecosystem, plan.Target.Name, plan.Target.To, plan.Target.ToModule, plan.Target.VendorDir, strings.Join(plan.Manifests, ","), strings.Join(plan.Files, ","), strings.Join(plan.Recipes, ","))
	return plan, nil
}

func display(t domain.Target) string {
	if t.Ecosystem != "" {
		return t.Ecosystem + ":" + t.Name
	}
	return t.Name
}

func langOf(eco string) string {
	switch eco {
	case "go":
		return "go"
	case "python":
		return "python"
	case "java", "maven":
		return "java"
	case "node":
		return "node"
	default:
		return ""
	}
}

func fileMightMatch(file string, r recipe.Recipe) bool {
	switch r.Spec.Language {
	case "go":
		return strings.HasSuffix(file, ".go")
	case "python":
		return strings.HasSuffix(file, ".py")
	default:
		return true
	}
}

func validatorsFor(eco string, inv domain.Inventory) []string {
	var out []string
	switch eco {
	case "go":
		out = append(out, "manifest-target", "go test")
	case "python":
		out = append(out, "manifest-target", "python syntax")
	case "maven":
		out = append(out, "manifest-target")
	case "node":
		out = append(out, "manifest-target")
	default:
		out = append(out, "manifest-target")
	}
	for _, b := range inv.BuildSystems {
		if b == "bazel" {
			out = append(out, "bazel test")
		}
	}
	return out
}

func supportFor(eco string) string {
	switch eco {
	case "go", "python", "maven", "node":
		return domain.SupportNative
	default:
		return domain.SupportUnavailable
	}
}

func capabilityNotes(eco string) []string {
	switch eco {
	case "go":
		return []string{"go semantic recipes: native", "go test: native when the go command is available"}
	case "python":
		return []string{"python structural recipes: native", "type checker: unavailable unless configured and installed"}
	case "maven":
		return []string{"maven manifest edit: native", "java semantic repair: unavailable", "build/test: delegated_command only when a command adapter is configured"}
	case "node":
		return []string{"package.json edit: native", "javascript semantic repair: unavailable"}
	default:
		return []string{"support unknown"}
	}
}

func generatedIn(inv domain.Inventory, projects []string) []string {
	var out []string
	for _, g := range inv.GeneratedPaths {
		for _, p := range projects {
			if p == "." || g == p || strings.HasPrefix(g, strings.TrimPrefix(p, "./")+"/") || strings.HasPrefix(g, p+"/") {
				out = append(out, g)
			}
		}
	}
	if len(projects) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// ParseTarget accepts ecosystem:name or a bare name.
func ParseTarget(raw, explicitEco string) domain.Target {
	t := domain.Target{Raw: raw, Ecosystem: explicitEco}
	if i := strings.Index(raw, ":"); i > 0 && !strings.Contains(raw[:i], "/") {
		t.Ecosystem = raw[:i]
		t.Name = raw[i+1:]
		return t
	}
	t.Name = raw
	if t.Ecosystem == "" {
		if strings.Contains(raw, "/") || strings.HasPrefix(raw, "gopkg.in") || strings.Contains(raw, ".") && !strings.Contains(raw, " ") {
			// go module paths contain dots; python names usually do not contain slashes.
			if strings.Contains(raw, "/") {
				t.Ecosystem = "go"
			}
		}
	}
	return t
}

func appendUnique(in []string, v string) []string {
	for _, s := range in {
		if s == v {
			return in
		}
	}
	return append(in, v)
}

// Rel is a small helper for tests.
func Rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}
