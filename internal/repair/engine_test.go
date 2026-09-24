package repair

import "testing"

func TestNoProgressStops(t *testing.T) {
	res, err := Loop(5, func(iteration int) (Step, error) {
		return Step{Applied: 0, Diagnostics: 2, Fingerprints: []string{"abc"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "needs_review" || res.Reason != "no progress" {
		t.Fatalf("%+v", res)
	}
}

func TestMaxIterations(t *testing.T) {
	res, err := Loop(2, func(iteration int) (Step, error) {
		return Step{Applied: 1, Diagnostics: 1, Fingerprints: []string{string(rune('a' + iteration))}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != 2 || res.Reason != "max iterations" {
		t.Fatalf("%+v", res)
	}
}
