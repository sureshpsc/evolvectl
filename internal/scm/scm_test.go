package scm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/runner"
)

// fakeRemote runs real git locally and fakes push and gh.
type fakeRemote struct {
	calls []string
}

func (f *fakeRemote) Run(ctx context.Context, dir string, argv ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(argv, " "))
	if argv[0] == "gh" {
		return "Creating pull request\nhttps://github.com/example/repo/pull/7\n", nil
	}
	if argv[0] == "git" && len(argv) > 1 && argv[1] == "push" {
		return "", nil
	}
	return Exec{}.Run(ctx, dir, argv...)
}

func gitInit(t *testing.T) string {
	t.Helper()
	if _, ok := runner.Look("git"); !ok {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "test"},
		{"git", "config", "commit.gpgsign", "false"},
	} {
		if _, err := (Exec{}).Run(context.Background(), dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"git", "add", "-A"}, {"git", "commit", "-q", "-m", "init"}} {
		if _, err := (Exec{}).Run(context.Background(), dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPublishCommitsOnlyRunFiles(t *testing.T) {
	dir := gitInit(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/a\n\nrequire x v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("edited by a person\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := &domain.RunReport{
		ID: "run_1", Outcome: domain.OutcomeSucceededWithReview,
		Target:  domain.Target{Ecosystem: "go", Name: "github.com/googleapis/gax-go", ToModule: "github.com/googleapis/gax-go/v2", To: "v2.0.2"},
		Changes: []domain.Change{{File: "go.mod"}},
	}
	fr := &fakeRemote{}
	g := Git{R: fr, Dir: dir}
	res := Publish(context.Background(), g, rep, "", filepath.Join(dir, ".evolvectl", "runs", "run_1", "pr.md"), "body text")
	if res.PRURL != "https://github.com/example/repo/pull/7" || !res.Pushed || res.Commit == "" || !res.Draft {
		t.Fatalf("%+v\n%s", res, strings.Join(fr.calls, "\n"))
	}
	if res.Branch != "evolvectl/gax-go-v2-v2.0.2" || res.Base != "main" {
		t.Fatalf("branch %s base %s", res.Branch, res.Base)
	}
	files, err := (Exec{}).Run(context.Background(), dir, "git", "show", "--name-only", "--format=%s", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files, "Move github.com/googleapis/gax-go to github.com/googleapis/gax-go/v2 v2.0.2") || !strings.Contains(files, "go.mod") || strings.Contains(files, "unrelated.txt") {
		t.Fatalf("commit:\n%s", files)
	}
	joined := strings.Join(fr.calls, "\n")
	if !strings.Contains(joined, "--draft") || strings.Contains(joined, "--force") {
		t.Fatal(joined)
	}
}

func TestEligibleRefusesFailedAndDryRuns(t *testing.T) {
	if Eligible(&domain.RunReport{Outcome: domain.OutcomeFailed, Changes: []domain.Change{{File: "a"}}}) == "" {
		t.Fatal("failed run is publishable")
	}
	if Eligible(&domain.RunReport{Outcome: domain.OutcomeSucceeded, DryRun: true, Changes: []domain.Change{{File: "a"}}}) == "" {
		t.Fatal("dry run is publishable")
	}
}
