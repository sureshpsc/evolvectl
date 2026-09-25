package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/exitcode"
	"github.com/sureshpsc/evolvectl/internal/state"
)

const (
	gaxOld = "github.com/googleapis/gax-go"
	gaxNew = "github.com/googleapis/gax-go/v2"
)

func TestGaxMajorMoveEndToEnd(t *testing.T) {
	dir := copyExample(t, "go-gax-v2")
	origClient, _ := os.ReadFile(filepath.Join(dir, "speech", "client.go"))
	origMod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))

	var up App
	buf := &strings.Builder{}
	up.Out, up.Err = buf, &strings.Builder{}
	opt := Option{Workspace: dir, NoAI: true, Offline: true, ToModule: gaxNew}
	code, err := up.Upgrade(context.Background(), opt, gaxOld, "v2.0.2", "")
	if err != nil || code != exitcode.NeedsReview {
		t.Fatalf("code=%d err=%v\n%s", code, err, buf)
	}
	client, _ := os.ReadFile(filepath.Join(dir, "speech", "client.go"))
	s := string(client)
	if !strings.Contains(s, `gax "`+gaxNew+`"`) || !strings.Contains(s, "_ gax.CallSettings") {
		t.Fatalf("client.go:\n%s", s)
	}
	mod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(mod), gaxNew+" v2.0.2") || strings.Contains(string(mod), "da06d194a00e") {
		t.Fatalf("go.mod:\n%s", mod)
	}

	runs, err := state.Open(dir).ListReports()
	if err != nil || len(runs) == 0 {
		t.Fatal(err)
	}
	rep := runs[0]
	if rep.Outcome != domain.OutcomeSucceededWithReview {
		t.Fatalf("outcome %s %s validations %+v review %v", rep.Outcome, rep.OutcomeReason, rep.Validations, rep.ManualReview)
	}
	gates := map[string]string{}
	for _, v := range rep.Validations {
		gates[v.Validator] = v.Status
	}
	if gates["module-move"] != domain.GatePass || gates["go test"] != domain.GatePass {
		t.Fatalf("gates %v", gates)
	}
	if rep.Quality == nil || len(rep.Quality.NewlyFailed) != 0 || !rep.Quality.AfterRecorded {
		t.Fatalf("quality %+v", rep.Quality)
	}
	imp := rep.Impact
	if imp == nil || imp.Status != "recorded" {
		t.Fatalf("impact %+v", imp)
	}
	changes := map[string]string{}
	for _, c := range imp.Changes {
		changes[c.Symbol] = c.Change
	}
	if changes["APICall"] != "changed" || changes["Invoke"] != "indirect" || changes["MustCompilePathTemplate"] != "removed" {
		t.Fatalf("impact changes %v", changes)
	}
	if len(imp.Sites) == 0 || imp.Sites[0].File != "speech/client.go" || imp.Sites[0].Symbol != "Invoke" {
		t.Fatalf("sites %+v", imp.Sites)
	}
	review := strings.Join(rep.ManualReview, "\n")
	if !strings.Contains(review, "replace "+gaxOld) {
		t.Fatalf("review %v", rep.ManualReview)
	}
	page, _ := os.ReadFile(filepath.Join(dir, ".evolvectl", "runs", rep.ID, "report.html"))
	if !strings.Contains(string(page), "API impact") || !strings.Contains(string(page), "MustCompilePathTemplate") {
		t.Fatal("report.html has no impact section")
	}

	if code, err := (&App{Out: &strings.Builder{}}).Rollback(Option{Workspace: dir}, rep.ID); err != nil || code != 0 {
		t.Fatal(code, err)
	}
	back, _ := os.ReadFile(filepath.Join(dir, "speech", "client.go"))
	backMod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if string(back) != string(origClient) || string(backMod) != string(origMod) {
		t.Fatalf("rollback left:\n%s\n%s", back, backMod)
	}
}

func TestImpactReportsRemovedAPI(t *testing.T) {
	dir := copyExample(t, "go-gax-v2")
	extra := "package speech\n\nimport gax \"" + gaxOld + "\"\n\nvar tpl = gax.MustCompilePathTemplate(\"projects/{p}\")\n"
	if err := os.WriteFile(filepath.Join(dir, "speech", "paths.go"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	var a App
	buf := &strings.Builder{}
	a.Out, a.Err = buf, &strings.Builder{}
	code, err := a.Impact(context.Background(), Option{Workspace: dir, Offline: true, ToModule: gaxNew}, gaxOld, "v2.0.2")
	if err != nil || code != exitcode.Success {
		t.Fatalf("code=%d err=%v\n%s", code, err, buf)
	}
	out := buf.String()
	for _, want := range []string{"speech/paths.go:5", "MustCompilePathTemplate", "removed", "speech/client.go", "Invoke"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q\n%s", want, out)
		}
	}
}
