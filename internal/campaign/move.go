package campaign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/sureshpsc/evolvectl/internal/apidiff"
	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/fsio"
	"github.com/sureshpsc/evolvectl/internal/idgen"
	"github.com/sureshpsc/evolvectl/internal/modmove"
	"github.com/sureshpsc/evolvectl/internal/patch"
	"github.com/sureshpsc/evolvectl/internal/policy"
)

// VendorMetadata is written beside a vendored module so the copy can be traced to its source.
const VendorMetadata = "METADATA.evolvectl.json"

func moving(t domain.Target) bool {
	return t.Ecosystem == "go" && t.ToModule != "" && t.ToModule != t.Name
}

// moveRequires swaps the requirement on the old module path for the new one in every planned go.mod.
func (e *Executor) moveRequires(rc *runCtx) error {
	t := rc.report.Target
	version := normalizeTarget("go", t.To)
	for _, rel := range goModFiles(rc) {
		abs := filepath.Join(rc.workRoot, filepath.FromSlash(rel))
		body, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		out, changed, notes, err := modmove.MoveRequire(rel, body, t.Name, t.ToModule, version)
		if err != nil {
			rc.report.Outcome = domain.OutcomeFailed
			return err
		}
		for _, n := range notes {
			rc.report.ManualReview = appendUnique(rc.report.ManualReview, n)
		}
		if changed {
			if err := fsio.WriteAtomic(abs, out, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// rewriteMovedImports moves import paths in every planned Go file that policy allows.
func (e *Executor) rewriteMovedImports(rc *runCtx, changes *[]domain.Change) error {
	t := rc.report.Target
	opt := policy.Options{AllowGenerated: rc.req.Config.Repair.AllowGenerated}
	for _, rel := range rc.report.Plan.Files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		abs := filepath.Join(rc.workRoot, filepath.FromSlash(rel))
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		out, n, err := modmove.RewriteImports(rel, body, t.Name, t.ToModule)
		if err != nil {
			rc.report.ManualReview = appendUnique(rc.report.ManualReview, rel+": imports not moved: "+err.Error())
			continue
		}
		if n == 0 {
			continue
		}
		dec := policy.AllowPath(rc.workRoot, rel, body, opt)
		if !dec.Allowed {
			rc.report.ManualReview = appendUnique(rc.report.ManualReview, rel+": still imports "+t.Name+" ("+dec.Reason+")")
			continue
		}
		if !rc.req.DryRun {
			if err := snapshot(rc.origRoot, rc.report.ID, rel, body); err != nil {
				return err
			}
		}
		if err := fsio.WriteAtomic(abs, out, 0o644); err != nil {
			return err
		}
		*changes = append(*changes, domain.Change{
			ID: idgen.New("chg_"), File: rel, Kind: "move", RecipeID: "engine:module-move", Confidence: domain.ConfidenceHigh,
			Diff: patch.Unified(rel, string(body), string(out)), BeforeHash: fsio.HashBytes(body), AfterHash: fsio.HashBytes(out),
			Reason: fmt.Sprintf("moved %d import(s) from %s to %s", n, t.Name, t.ToModule),
		})
	}
	return nil
}

// vendorModule copies module@version into the vendor directory and points each planned go.mod at it.
func (e *Executor) vendorModule(ctx context.Context, rc *runCtx, changes *[]domain.Change) error {
	t := rc.report.Target
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(t.VendorDir)))
	if filepath.IsAbs(t.VendorDir) || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		rc.report.Outcome = domain.OutcomeBlocked
		return fmt.Errorf("vendor directory must be a relative path inside the workspace: %s", t.VendorDir)
	}
	if strings.HasPrefix(rel, ".git/") || strings.HasPrefix(rel, ".evolvectl/") || rel == ".git" || rel == ".evolvectl" {
		rc.report.Outcome = domain.OutcomeBlocked
		return fmt.Errorf("vendor directory %s is reserved", rel)
	}
	module := t.Module()
	version := normalizeTarget("go", t.To)
	dest := filepath.Join(rc.workRoot, filepath.FromSlash(rel))
	if rc.vendored {
		return nil
	}
	dl, err := apidiff.Fetch(ctx, module, version, rc.req.Offline)
	if err != nil {
		rc.report.Outcome = domain.OutcomeFailed
		return err
	}
	beforeHash, beforeFiles, err := treeHash(dest)
	if err != nil {
		return err
	}
	if !rc.req.DryRun && beforeFiles != nil {
		snap := filepath.Join(rc.origRoot, ".evolvectl", "snapshots", rc.report.ID, filepath.FromSlash(rel))
		if _, err := os.Stat(snap); os.IsNotExist(err) {
			if err := copyTree(dest, snap); err != nil {
				return err
			}
		}
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := copyTree(dl.Dir, dest); err != nil {
		return err
	}
	var licenses []string
	entries, _ := os.ReadDir(dest)
	for _, en := range entries {
		up := strings.ToUpper(en.Name())
		if !en.IsDir() && (strings.HasPrefix(up, "LICENSE") || strings.HasPrefix(up, "LICENCE") || strings.HasPrefix(up, "COPYING")) {
			licenses = append(licenses, en.Name())
		}
	}
	if len(licenses) == 0 {
		rc.report.ManualReview = appendUnique(rc.report.ManualReview, rel+": no LICENSE file in "+module+"@"+version)
	}
	meta, _ := json.MarshalIndent(map[string]any{
		"module": module, "version": version, "sum": dl.Sum, "source": "go mod download " + module + "@" + version,
		"license_files": licenses, "run_id": rc.report.ID, "vendored_at": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err := fsio.WriteAtomic(filepath.Join(dest, VendorMetadata), append(meta, '\n'), 0o644); err != nil {
		return err
	}
	for _, mod := range goModFiles(rc) {
		abs := filepath.Join(rc.workRoot, filepath.FromSlash(mod))
		body, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		target, err := filepath.Rel(filepath.Dir(abs), dest)
		if err != nil {
			return err
		}
		out, changed, err := modmove.SetReplace(mod, body, module, filepath.ToSlash(target))
		if err != nil {
			return err
		}
		if changed {
			if err := fsio.WriteAtomic(abs, out, 0o644); err != nil {
				return err
			}
		}
	}
	afterHash, afterFiles, err := treeHash(dest)
	if err != nil {
		return err
	}
	*changes = append(*changes, domain.Change{
		ID: idgen.New("chg_"), File: rel, Kind: "vendor-tree", RecipeID: "engine:vendor", Confidence: domain.ConfidenceHigh,
		Diff: treeDiff(rel, beforeFiles, afterFiles), BeforeHash: beforeHash, AfterHash: afterHash,
		Reason: fmt.Sprintf("copied %s@%s into %s (%s)", module, version, rel, dl.Sum),
	})
	rc.vendored = true
	return nil
}

// useLocalCopy points each planned go.mod at a copy of the new version that a sync tool
// already wrote into the workspace. Nothing is downloaded and the copy is not modified.
func (e *Executor) useLocalCopy(rc *runCtx) error {
	t := rc.report.Target
	dir, err := checkUseDir(rc)
	if err != nil {
		return err
	}
	for _, mod := range goModFiles(rc) {
		abs := filepath.Join(rc.workRoot, filepath.FromSlash(mod))
		b, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		target, err := filepath.Rel(filepath.Dir(abs), dir)
		if err != nil {
			return err
		}
		out, changed, err := modmove.SetReplace(mod, b, t.Module(), filepath.ToSlash(target))
		if err != nil {
			return err
		}
		if changed {
			if err := fsio.WriteAtomic(abs, out, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkUseDir returns the absolute --use-dir directory once it is inside the workspace and
// holds a go.mod for the target module.
func checkUseDir(rc *runCtx) (string, error) {
	t := rc.report.Target
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(t.UseDir)))
	if filepath.IsAbs(t.UseDir) || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		rc.report.Outcome = domain.OutcomeBlocked
		return "", fmt.Errorf("--use-dir must be a relative path inside the workspace: %s", t.UseDir)
	}
	dir := filepath.Join(rc.workRoot, filepath.FromSlash(rel))
	body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		rc.report.Outcome = domain.OutcomeBlocked
		rc.report.NextStep = "run the import first so " + rel + " holds the new version with its go.mod"
		return "", fmt.Errorf("--use-dir %s has no go.mod: %w", rel, err)
	}
	if got := modfile.ModulePath(body); got != t.Module() {
		rc.report.Outcome = domain.OutcomeBlocked
		return "", fmt.Errorf("--use-dir %s declares module %q, want %q", rel, got, t.Module())
	}
	return dir, nil
}

// treeHash hashes every regular file under dir. A missing dir returns "" and nil files.
func treeHash(dir string) (string, map[string]string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", nil, nil
	}
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		h, err := fsio.HashFile(p)
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(r)] = h
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sum := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(sum, "%s %s\n", k, files[k])
	}
	return hex.EncodeToString(sum.Sum(nil)), files, nil
}

func treeDiff(root string, before, after map[string]string) string {
	var lines []string
	for k, h := range after {
		if old, ok := before[k]; !ok {
			lines = append(lines, "A "+root+"/"+k)
		} else if old != h {
			lines = append(lines, "M "+root+"/"+k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			lines = append(lines, "D "+root+"/"+k)
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i][2:] < lines[j][2:] })
	return fmt.Sprintf("vendored tree %s: %d file(s) changed\n%s\n", root, len(lines), strings.Join(lines, "\n"))
}

// restoreTree puts a vendored directory back the way the snapshot recorded it.
func restoreTree(workspace, runID string, ch domain.Change) error {
	dest := filepath.Join(workspace, filepath.FromSlash(ch.File))
	now, _, err := treeHash(dest)
	if err != nil {
		return err
	}
	if now != ch.AfterHash {
		return fmt.Errorf("refusing to roll back %s: the vendored tree changed after the campaign", ch.File)
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if ch.BeforeHash == "" {
		return nil
	}
	snap := filepath.Join(workspace, ".evolvectl", "snapshots", runID, filepath.FromSlash(ch.File))
	return copyTree(snap, dest)
}
