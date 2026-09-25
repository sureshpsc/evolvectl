package report

import (
	"strings"
	"testing"
	"time"

	"github.com/sureshpsc/evolvectl/internal/domain"
)

func TestHTMLEscapesAndRedacts(t *testing.T) {
	r := &domain.RunReport{
		ID: "run_test", CreatedAt: time.Now().UTC(), DataAsOf: time.Now().UTC(),
		Outcome: "partial", Target: domain.Target{Ecosystem: "go", Name: "<script>alert(1)</script>", To: "v1"},
		RedactionNotice: "redacted",
		Inventory: domain.Inventory{Dependencies: []domain.Dependency{{
			Name: "<script>alert(1)</script>", Ecosystem: "go", CurrentVersion: "v1", Status: domain.StatusRegistryUnavailable,
		}}},
		Copybara: &domain.CopybaraView{Workflows: []domain.WorkflowExplanation{{
			Name: "w", Destination: "https://user:ghp_abcdefghijklmnopqrst@github.com/example/private.git",
		}}},
	}
	b, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "<script>alert") {
		t.Fatal("xss")
	}
	if strings.Contains(s, "ghp_abcdefghijklmnopqrst") {
		t.Fatal("token leaked")
	}
	if strings.Contains(s, "https://cdn") {
		t.Fatal("cdn")
	}
	if !strings.Contains(s, "Tests and coverage") {
		t.Fatal("missing quality section")
	}
}
