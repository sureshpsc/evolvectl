package campaign

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/fsio"
	"github.com/evolvectl/evolvectl/internal/idgen"
	"github.com/evolvectl/evolvectl/internal/manifest"
	"github.com/evolvectl/evolvectl/internal/patch"
	"github.com/evolvectl/evolvectl/internal/runner"
)

func externalReplaceBlock(root string, inv domain.Inventory) (string, bool) {
	var mans []string
	for _, m := range inv.Manifests {
		mans = append(mans, m.Path)
	}
	reps, err := manifest.ExternalReplaces(root, mans)
	if err != nil {
		return err.Error(), false
	}
	if len(reps) == 0 {
		return "", true
	}
	var parts []string
	for _, r := range reps {
		parts = append(parts, manifest.FormatExternalReplace(r))
	}
	return strings.Join(parts, "; "), false
}

// goSumDecision is the offline gate. A local replace does not need a sum entry.
func goSumDecision(offline, localReplace bool, sum []byte, modulePath, version string) error {
	if localReplace || !offline {
		return nil
	}
	if manifest.SumContains(sum, modulePath, version) {
		return nil
	}
	return fmt.Errorf("offline: go.sum was not updated for %s@%s; re-run without --offline or add the sum entry", modulePath, version)
}

func goModFiles(rc *runCtx) []string {
	if rc.report.Plan == nil {
		return nil
	}
	var out []string
	for _, rel := range rc.report.Plan.Manifests {
		if strings.HasSuffix(rel, "go.mod") {
			out = append(out, rel)
		}
	}
	return out
}

func (e *Executor) rejectMissingSums(rc *runCtx) error {
	if rc.report.Target.Ecosystem != "go" || rc.report.Target.VendorDir != "" {
		return nil
	}
	modulePath := rc.report.Target.Module()
	version := normalizeTarget("go", rc.report.Target.To)
	for _, rel := range goModFiles(rc) {
		dir := filepath.Dir(filepath.Join(rc.workRoot, filepath.FromSlash(rel)))
		body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return err
		}
		sum, _ := os.ReadFile(filepath.Join(dir, "go.sum"))
		if err := goSumDecision(rc.req.Offline, manifest.HasLocalReplace(body, modulePath), sum, modulePath, version); err != nil {
			rc.report.Outcome = domain.OutcomeFailed
			rc.report.NextStep = "re-run without --offline so go get can write go.sum"
			return err
		}
	}
	return nil
}

// capturePristine keeps the first bytes seen for each go.mod and go.sum in the plan, so every
// later edit is diffed against the workspace as it was before the run.
func (e *Executor) capturePristine(rc *runCtx) {
	if rc.pristine != nil {
		return
	}
	rc.pristine = map[string][]byte{}
	for _, rel := range goModFiles(rc) {
		for _, f := range []string{rel, sumOf(rel)} {
			b, err := os.ReadFile(filepath.Join(rc.workRoot, filepath.FromSlash(f)))
			if err != nil {
				rc.pristine[f] = nil
				continue
			}
			rc.pristine[f] = b
		}
	}
}

func sumOf(gomod string) string {
	return strings.TrimSuffix(gomod, "go.mod") + "go.sum"
}

// syncGoSums asks the go command to make go.mod and go.sum consistent with the new requirement.
// A module move or a vendored copy also runs go mod tidy, so the old module's sums drop out
// and the new module's dependencies are added.
func (e *Executor) syncGoSums(ctx context.Context, rc *runCtx) error {
	t := rc.report.Target
	if t.Ecosystem != "go" || rc.report.Plan == nil {
		return nil
	}
	modulePath := t.Module()
	version := normalizeTarget("go", t.To)
	tidy := t.VendorDir != "" || (t.ToModule != "" && t.ToModule != t.Name)
	for _, rel := range goModFiles(rc) {
		dir := filepath.Dir(filepath.Join(rc.workRoot, filepath.FromSlash(rel)))
		body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return err
		}
		local := manifest.HasLocalReplace(body, modulePath)
		if local {
			rc.report.PolicyDecisions = append(rc.report.PolicyDecisions, domain.PolicyDecision{
				ID: idgen.New("pol_"), Action: "go-sum", Path: rel, Allowed: true,
				Reason: modulePath + " uses a local replace; go get was not run",
			})
		}
		if rc.req.Offline {
			continue
		}
		goBin, ok := runner.Look("go")
		if !ok {
			rc.report.Outcome = domain.OutcomeFailed
			return fmt.Errorf("go is not on PATH; go.sum was not updated for %s@%s", modulePath, version)
		}
		if !local {
			run := runner.Run(ctx, runner.Request{
				Argv: []string{goBin, "get", modulePath + "@" + version}, Dir: dir, Timeout: 3 * time.Minute,
			})
			if run.ExitCode != 0 {
				rc.report.Outcome = domain.OutcomeFailed
				return fmt.Errorf("go get %s@%s: %s", modulePath, version, trimLog(firstNonEmpty(run.Stderr+" "+run.Err, run.Stdout)))
			}
		}
		if !tidy {
			continue
		}
		run := runner.Run(ctx, runner.Request{Argv: []string{goBin, "mod", "tidy"}, Dir: dir, Timeout: 5 * time.Minute})
		if run.ExitCode != 0 {
			rc.report.ManualReview = appendUnique(rc.report.ManualReview, rel+": go mod tidy failed: "+trimLog(firstNonEmpty(run.Stderr, run.Err)))
		}
	}
	return nil
}

// recordManifests writes one change per go.mod and go.sum, diffed against the pristine bytes.
// A repeat call during repair updates the existing change instead of adding another.
func (e *Executor) recordManifests(rc *runCtx, changes *[]domain.Change, reason string) error {
	for _, rel := range goModFiles(rc) {
		for _, f := range []string{rel, sumOf(rel)} {
			before, known := rc.pristine[f]
			if !known {
				continue
			}
			abs := filepath.Join(rc.workRoot, filepath.FromSlash(f))
			after, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			if string(after) == string(before) {
				continue
			}
			if !rc.req.DryRun && before != nil {
				if err := snapshot(rc.origRoot, rc.report.ID, f, before); err != nil {
					return err
				}
			}
			why := reason
			id := "engine:manifest-bump"
			if strings.HasSuffix(f, "go.sum") {
				why = "go.sum updated by the go command"
				id = "engine:go-get"
			} else {
				for _, d := range lowered(f, before, after, rc.report.Target.Name) {
					rc.report.ManualReview = appendUnique(rc.report.ManualReview, d)
				}
			}
			upsertChange(rc, changes, f, before, after, id, why)
		}
	}
	return nil
}

// lowered lists requirements whose version went down between two go.mod files. The go command
// can do this when a module move drops the dependency that held a version up.
func lowered(rel string, before, after []byte, target string) []string {
	if before == nil {
		return nil
	}
	old, err := modfile.ParseLax(rel, before, nil)
	if err != nil {
		return nil
	}
	cur, err := modfile.ParseLax(rel, after, nil)
	if err != nil {
		return nil
	}
	was := map[string]string{}
	for _, r := range old.Require {
		was[r.Mod.Path] = r.Mod.Version
	}
	var out []string
	for _, r := range cur.Require {
		prev, ok := was[r.Mod.Path]
		if !ok || r.Mod.Path == target || semver.Compare(r.Mod.Version, prev) >= 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s went down from %s to %s; check that nothing relied on the newer version", rel, r.Mod.Path, prev, r.Mod.Version))
	}
	return out
}

func upsertChange(rc *runCtx, changes *[]domain.Change, rel string, before, after []byte, recipeID, reason string) {
	diff := patch.Unified(rel, string(before), string(after))
	beforeHash := ""
	if before != nil {
		beforeHash = fsio.HashBytes(before)
	}
	for _, list := range []*[]domain.Change{changes, &rc.report.Changes} {
		for i := range *list {
			c := &(*list)[i]
			if c.File == rel && c.Kind == "manifest" {
				c.Diff = diff
				c.AfterHash = fsio.HashBytes(after)
				c.Reason = reason
				return
			}
		}
	}
	*changes = append(*changes, domain.Change{
		ID: idgen.New("chg_"), File: rel, Kind: "manifest", RecipeID: recipeID, Confidence: domain.ConfidenceHigh,
		Diff: diff, BeforeHash: beforeHash, AfterHash: fsio.HashBytes(after), Reason: reason,
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
