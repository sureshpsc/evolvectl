// Package copybaralocal reproduces the literal part of a Copybara workflow on the local disk:
// fetch the origin ref, select origin_files, apply core.move and literal core.replace, and
// compare the result with a destination directory. It never pushes and never runs Copybara.
package copybaralocal

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/fsio"
	"github.com/evolvectl/evolvectl/internal/runner"
	"github.com/evolvectl/evolvectl/internal/skyparse"
)

// Marker is written into every preview directory so a later preview may replace it.
const Marker = ".evolvectl-copybara-preview"

// StepResult says what one transformation did.
type StepResult struct {
	Step        string `json:"step"`
	Files       int    `json:"files"`
	Occurrences int    `json:"occurrences,omitempty"`
}

// Result is the preview of one workflow.
type Result struct {
	Workflow    string       `json:"workflow"`
	OriginURL   string       `json:"origin_url"`
	OriginRef   string       `json:"origin_ref"`
	Commit      string       `json:"commit"`
	Selected    int          `json:"selected"`
	Files       []string     `json:"files"`
	Steps       []StepResult `json:"steps"`
	OutsideDest []string     `json:"outside_destination_files,omitempty"`
	SkippedBin  []string     `json:"binary_files_not_replaced,omitempty"`
	Added       []string     `json:"added,omitempty"`
	Modified    []string     `json:"modified,omitempty"`
	Deleted     []string     `json:"deleted,omitempty"`
	Compared    string       `json:"compared_with,omitempty"`
	Output      string       `json:"output,omitempty"`
	Warnings    []string     `json:"warnings,omitempty"`
	Ignored     []string     `json:"ignored,omitempty"`
	Unsupported []string     `json:"unsupported,omitempty"`
	Destination string       `json:"destination"`
	Complete    bool         `json:"complete"`
	files       map[string][]byte
}

// Fetch checks out ref from url into a new temporary directory with a shallow git fetch.
// Branches, tags, and commit ids all work. The caller removes the directory.
func Fetch(ctx context.Context, url, ref string) (dir, commit string, err error) {
	git, ok := runner.Look("git")
	if !ok {
		return "", "", fmt.Errorf("git is not on PATH")
	}
	if ref == "" {
		ref = "HEAD"
	}
	dir, err = os.MkdirTemp("", "evolvectl-origin-*")
	if err != nil {
		return "", "", err
	}
	run := func(args ...string) (string, error) {
		r := runner.Run(ctx, runner.Request{Argv: append([]string{git}, args...), Dir: dir, Timeout: 10 * time.Minute, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
		if r.ExitCode != 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(r.Stderr+" "+r.Err))
		}
		return strings.TrimSpace(r.Stdout), nil
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", url, ref},
		{"-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"},
	} {
		if _, err := run(args...); err != nil {
			os.RemoveAll(dir)
			return "", "", err
		}
	}
	commit, err = run("rev-parse", "HEAD")
	if err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	return dir, commit, nil
}

// Apply runs the workflow's literal steps over the files in src.
func Apply(sp skyparse.Spec, src string) (*Result, error) {
	res := &Result{
		Workflow: sp.Name, OriginURL: sp.OriginURL, OriginRef: sp.OriginRef, Destination: sp.Destination,
		Ignored: sp.Ignored, Unsupported: sp.Unsupported, Complete: len(sp.Unsupported) == 0,
		files: map[string][]byte{},
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(mustRel(src, p))
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !skyparse.Match(sp.OriginFiles, sp.OriginExclude, rel) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		res.files[rel] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.Selected = len(res.files)
	for _, st := range sp.Steps {
		var sr StepResult
		var err error
		switch st.Kind {
		case "move":
			sr, err = res.move(st)
		case "replace":
			sr = res.replace(st)
		}
		if err != nil {
			return res, err
		}
		res.Steps = append(res.Steps, sr)
	}
	for p := range res.files {
		res.Files = append(res.Files, p)
		if !skyparse.Match(sp.DestFiles, sp.DestExclude, p) {
			res.OutsideDest = append(res.OutsideDest, p)
		}
	}
	sort.Strings(res.Files)
	sort.Strings(res.OutsideDest)
	sort.Strings(res.SkippedBin)
	return res, nil
}

func (r *Result) move(st skyparse.Step) (StepResult, error) {
	sr := StepResult{Step: fmt.Sprintf("core.move(%q, %q)", st.Before, st.After)}
	from := strings.Trim(st.Before, "/")
	to := strings.Trim(st.After, "/")
	next := make(map[string][]byte, len(r.files))
	moved := map[string]string{}
	for p, b := range r.files {
		rel, ok := under(p, from)
		if ok && len(st.Paths) > 0 && !skyparse.Match(st.Paths, st.PathsExclude, rel) {
			ok = false
		}
		if !ok {
			next[p] = b
			continue
		}
		moved[p] = join(to, rel)
	}
	keys := make([]string, 0, len(moved))
	for p := range moved {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		dst := moved[p]
		if _, taken := next[dst]; taken && !st.Overwrite {
			return sr, fmt.Errorf("%s: %s would overwrite %s; Copybara fails here unless overwrite = True", sr.Step, p, dst)
		}
		next[dst] = r.files[p]
		sr.Files++
	}
	if sr.Files == 0 {
		r.noop(sr.Step)
	}
	r.files = next
	return sr, nil
}

func (r *Result) replace(st skyparse.Step) StepResult {
	sr := StepResult{Step: fmt.Sprintf("core.replace(%q -> %q)", st.Before, st.After)}
	before, after := []byte(st.Before), []byte(st.After)
	for p, b := range r.files {
		if len(st.Paths) > 0 && !skyparse.Match(st.Paths, st.PathsExclude, p) {
			continue
		}
		n := bytes.Count(b, before)
		if n == 0 {
			continue
		}
		if bytes.IndexByte(b, 0) >= 0 {
			r.SkippedBin = appendOnce(r.SkippedBin, p)
			continue
		}
		r.files[p] = bytes.ReplaceAll(b, before, after)
		sr.Files++
		sr.Occurrences += n
	}
	if sr.Files == 0 {
		r.noop(sr.Step)
	}
	return sr
}

func (r *Result) noop(step string) {
	r.Warnings = append(r.Warnings, step+" changed nothing; Copybara stops on a no-op transformation unless you pass --ignore-noop")
}

// Compare lists what the import would add, change, and delete inside destination_files of dest.
func (r *Result) Compare(sp skyparse.Spec, dest string) error {
	r.Compared = filepath.ToSlash(dest)
	seen := map[string]bool{}
	err := filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == dest {
				return filepath.SkipAll
			}
			return err
		}
		rel := filepath.ToSlash(mustRel(dest, p))
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".evolvectl" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !skyparse.Match(sp.DestFiles, sp.DestExclude, rel) {
			return nil
		}
		seen[rel] = true
		want, ok := r.files[rel]
		if !ok {
			r.Deleted = append(r.Deleted, rel)
			return nil
		}
		have, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Equal(have, want) {
			r.Modified = append(r.Modified, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for p := range r.files {
		if !seen[p] && skyparse.Match(sp.DestFiles, sp.DestExclude, p) {
			r.Added = append(r.Added, p)
		}
	}
	sort.Strings(r.Added)
	sort.Strings(r.Modified)
	sort.Strings(r.Deleted)
	return nil
}

// Write puts the result tree in dir. An existing dir is replaced only when an earlier preview
// wrote it; anything else must be empty.
func (r *Result) Write(dir string) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(dir, Marker)); err != nil {
			return fmt.Errorf("%s is not empty and was not written by a preview; pick another --out", dir)
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	for p, b := range r.files {
		if err := fsio.WriteAtomic(filepath.Join(dir, filepath.FromSlash(p)), b, 0o644); err != nil {
			return err
		}
	}
	note := fmt.Sprintf("workflow %s\norigin %s\nref %s\ncommit %s\n", r.Workflow, r.OriginURL, r.OriginRef, r.Commit)
	if err := os.WriteFile(filepath.Join(dir, Marker), []byte(note), 0o644); err != nil {
		return err
	}
	r.Output = filepath.ToSlash(dir)
	return nil
}

func under(p, dir string) (string, bool) {
	if dir == "" {
		return p, true
	}
	if p == dir {
		return "", true
	}
	if strings.HasPrefix(p, dir+"/") {
		return p[len(dir)+1:], true
	}
	return "", false
}

func join(dir, rel string) string {
	switch {
	case dir == "":
		return rel
	case rel == "":
		return dir
	}
	return dir + "/" + rel
}

func mustRel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return r
}

func appendOnce(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
