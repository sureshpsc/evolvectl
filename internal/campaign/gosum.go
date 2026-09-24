package campaign

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func (e *Executor) rejectMissingSums(rc *runCtx) error {
	if rc.report.Target.Ecosystem != "go" || rc.report.Plan == nil {
		return nil
	}
	modulePath := rc.report.Target.Name
	version := normalizeTarget("go", rc.report.Target.To)
	for _, rel := range rc.report.Plan.Manifests {
		if !strings.HasSuffix(rel, "go.mod") {
			continue
		}
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

func (e *Executor) syncGoSums(ctx context.Context, rc *runCtx, changes *[]domain.Change) error {
	if rc.report.Target.Ecosystem != "go" || rc.report.Plan == nil {
		return nil
	}
	modulePath := rc.report.Target.Name
	version := normalizeTarget("go", rc.report.Target.To)
	for _, rel := range rc.report.Plan.Manifests {
		if !strings.HasSuffix(rel, "go.mod") {
			continue
		}
		dir := filepath.Dir(filepath.Join(rc.workRoot, filepath.FromSlash(rel)))
		body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return err
		}
		sumPath := filepath.Join(dir, "go.sum")
		sumBefore, _ := os.ReadFile(sumPath)
		local := manifest.HasLocalReplace(body, modulePath)
		if local {
			rc.report.PolicyDecisions = append(rc.report.PolicyDecisions, domain.PolicyDecision{
				ID: idgen.New("pol_"), Action: "go-sum", Path: rel, Allowed: true,
				Reason: modulePath + " uses a local replace; go.sum was not fetched",
			})
			continue
		}
		if rc.req.Offline {
			continue
		}
		goBin, ok := runner.Look("go")
		if !ok {
			rc.report.Outcome = domain.OutcomeFailed
			return fmt.Errorf("go is not on PATH; go.sum was not updated for %s@%s", modulePath, version)
		}
		modBefore := body
		run := runner.Run(ctx, runner.Request{
			Argv:    []string{goBin, "get", modulePath + "@" + version},
			Dir:     dir,
			Timeout: 2 * time.Minute,
		})
		if run.ExitCode != 0 {
			rc.report.Outcome = domain.OutcomeFailed
			detail := strings.TrimSpace(run.Stderr + " " + run.Err)
			if detail == "" {
				detail = run.Stdout
			}
			return fmt.Errorf("go get %s@%s: %s", modulePath, version, trimLog(detail))
		}
		modAfter, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return err
		}
		sumAfter, _ := os.ReadFile(sumPath)
		orig := modBefore
		if b, err := os.ReadFile(filepath.Join(rc.origRoot, ".evolvectl", "snapshots", rc.report.ID, filepath.FromSlash(rel))); err == nil {
			orig = b
		}
		updateModChange(changes, rel, orig, modAfter)
		if string(sumBefore) != string(sumAfter) {
			sumRel := fsio.RelSlash(rc.workRoot, sumPath)
			if !rc.req.DryRun && len(sumBefore) > 0 {
				if err := snapshot(rc.origRoot, rc.report.ID, sumRel, sumBefore); err != nil {
					return err
				}
			}
			ch := fileChange(sumRel, sumBefore, sumAfter, "go get wrote go.sum for "+modulePath+"@"+version)
			if len(sumBefore) == 0 {
				ch.BeforeHash = ""
			}
			*changes = append(*changes, ch)
		}
	}
	return nil
}

func updateModChange(changes *[]domain.Change, rel string, before, after []byte) {
	if string(before) == string(after) {
		return
	}
	for i := range *changes {
		if (*changes)[i].File != rel {
			continue
		}
		(*changes)[i].AfterHash = fsio.HashBytes(after)
		(*changes)[i].Diff = patch.Unified(rel, string(before), string(after))
		return
	}
}

func fileChange(rel string, before, after []byte, reason string) domain.Change {
	return domain.Change{
		ID:         idgen.New("chg_"),
		File:       rel,
		Kind:       "manifest",
		RecipeID:   "engine:go-get",
		Confidence: domain.ConfidenceHigh,
		Diff:       patch.Unified(rel, string(before), string(after)),
		BeforeHash: fsio.HashBytes(before),
		AfterHash:  fsio.HashBytes(after),
		Reason:     reason,
	}
}
