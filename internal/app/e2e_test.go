package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/exitcode"
	"github.com/sureshpsc/evolvectl/internal/fsio"
	"github.com/sureshpsc/evolvectl/internal/state"
)

func TestGoUpgradeEndToEnd(t *testing.T) {
	dir := copyExample(t, "git-go-grpc")
	before, err := fsio.HashFile(filepath.Join(dir, "client", "client.go"))
	if err != nil {
		t.Fatal(err)
	}
	var dry App
	dry.Out, dry.Err = &strings.Builder{}, &strings.Builder{}
	code, err := dry.Plan(context.Background(), Option{Workspace: dir, Offline: true}, "google.golang.org/grpc", "v1.75.0")
	if err != nil || code != 0 {
		t.Fatalf("plan code=%d err=%v", code, err)
	}
	afterPlan, _ := fsio.HashFile(filepath.Join(dir, "client", "client.go"))
	if afterPlan != before {
		t.Fatal("plan modified source")
	}

	var up App
	buf := &strings.Builder{}
	up.Out, up.Err = buf, &strings.Builder{}
	code, err = up.Upgrade(context.Background(), Option{Workspace: dir, NoAI: true, Offline: true}, "google.golang.org/grpc", "v1.75.0", "")
	if err != nil || code != exitcode.Success {
		t.Fatalf("upgrade code=%d err=%v\n%s", code, err, buf.String())
	}
	body, _ := os.ReadFile(filepath.Join(dir, "client", "client.go"))
	if !strings.Contains(string(body), "NewClient") || strings.Contains(string(body), "DialContext") {
		t.Fatalf("client.go:\n%s", body)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "client", "config.go"))
	if !strings.Contains(string(cfg), "NewField") {
		t.Fatalf("config.go:\n%s", cfg)
	}
	mod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(mod), "v1.75.0") {
		t.Fatalf("go.mod:\n%s", mod)
	}
	runs, err := state.Open(dir).ListReports()
	if err != nil || len(runs) == 0 {
		t.Fatal(err)
	}
	rep := runs[0]
	if rep.Outcome != "succeeded" {
		t.Fatalf("outcome %s reason %s validations %+v review %v", rep.Outcome, rep.OutcomeReason, rep.Validations, rep.ManualReview)
	}
	if rep.Quality == nil || !rep.Quality.AfterRecorded || len(rep.Quality.NewlyFailed) != 0 {
		t.Fatalf("quality %+v", rep.Quality)
	}
	if !hasCoverage(rep.Quality.Before) || !hasCoverage(rep.Quality.After) {
		t.Fatalf("coverage before %+v after %+v", rep.Quality.Before, rep.Quality.After)
	}
	saved := filepath.Join(dir, ".evolvectl", "runs", rep.ID, "report.html")
	page, err := os.ReadFile(saved)
	if err != nil || !strings.Contains(string(page), "Tests and coverage") || !strings.Contains(string(page), "Newly failed") {
		t.Fatalf("saved html: %v", err)
	}
	htmlPath := filepath.Join(dir, "report.html")
	if code, err := (&App{Out: &strings.Builder{}, Err: &strings.Builder{}}).Report(Option{Workspace: dir}, rep.ID, "html", htmlPath); err != nil || code != 0 {
		t.Fatal(code, err)
	}
	html, _ := os.ReadFile(htmlPath)
	if !strings.Contains(string(html), rep.ID) || strings.Contains(string(html), "cdn.") {
		t.Fatal("html report")
	}
}

func TestDryRunDoesNotWriteSource(t *testing.T) {
	dir := copyExample(t, "git-go-grpc")
	before, _ := fsio.HashFile(filepath.Join(dir, "client", "client.go"))
	var up App
	up.Out, up.Err = &strings.Builder{}, &strings.Builder{}
	code, err := up.Upgrade(context.Background(), Option{Workspace: dir, DryRun: true, NoAI: true, Offline: true}, "google.golang.org/grpc", "v1.75.0", "")
	if err != nil || code != exitcode.Success {
		t.Fatalf("code=%d err=%v out=%s", code, err, up.Out)
	}
	after, _ := fsio.HashFile(filepath.Join(dir, "client", "client.go"))
	if before != after {
		t.Fatal("dry-run changed source")
	}
}

func TestPythonUpgrade(t *testing.T) {
	dir := copyExample(t, "python-pydantic")
	var up App
	up.Out, up.Err = &strings.Builder{}, &strings.Builder{}
	code, err := up.Upgrade(context.Background(), Option{Workspace: dir, NoAI: true, Offline: true}, "python:pydantic", "2.11.0", "")
	if err != nil || code != exitcode.Success {
		t.Fatalf("code=%d err=%v out=%s", code, err, up.Out)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "app", "models.py"))
	s := string(body)
	for _, want := range []string{"from pydantic import BaseModel", "@field_validator", "model_dump()", "from_attributes=True", `note = "orm_mode=True"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s\n%s", want, s)
		}
	}
	if strings.Contains(s, "pydantic.v1") || strings.Contains(s, "@validator") {
		t.Fatalf("old syntax remains\n%s", s)
	}
}

func TestMavenCommandAdapter(t *testing.T) {
	dir := copyExample(t, "java-maven-command-adapter")
	var up App
	buf := &strings.Builder{}
	up.Out, up.Err = buf, &strings.Builder{}
	code, err := up.Upgrade(context.Background(), Option{Workspace: dir, NoAI: true, Offline: true}, "maven:junit:junit", "4.13.2", "")
	if err != nil {
		t.Fatal(err)
	}
	if code != exitcode.NeedsReview {
		t.Fatalf("code=%d out=%s", code, buf)
	}
	pom, _ := os.ReadFile(filepath.Join(dir, "pom.xml"))
	if !strings.Contains(string(pom), "4.13.2") {
		t.Fatalf("pom:\n%s", pom)
	}
	runs, _ := state.Open(dir).ListReports()
	if len(runs) == 0 || runs[0].Outcome != "succeeded_with_review" {
		t.Fatalf("outcome %+v", runs)
	}
}

func TestScanMixed(t *testing.T) {
	dir := copyExample(t, "mixed-monorepo")
	var a App
	buf := &strings.Builder{}
	a.Out, a.Err = buf, &strings.Builder{}
	code, err := a.Scan(context.Background(), Option{Workspace: dir, Format: "json"})
	if err != nil || code != 0 {
		t.Fatal(code, err)
	}
	text := buf.String()
	for _, needle := range []string{"pydantic", "junit", "left-pad", "example.com/demo", "copy.bara.sky"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %s\n%s", needle, text)
		}
	}
}

func hasCoverage(samples []domain.CoverageSample) bool {
	for _, s := range samples {
		if s.HasPercent {
			return true
		}
	}
	return false
}

func copyExample(t *testing.T, example string) string {
	t.Helper()
	src := filepath.Join(moduleRoot(t), "examples", example)
	dst := t.TempDir()
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	return dst
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(b), "module github.com/sureshpsc/evolvectl") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}
