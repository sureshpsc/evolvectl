// Package repair applies the bounded diagnostic repair loop.
package repair

// Step is one loop observation.
type Step struct {
	Applied      int
	Fingerprints []string
	Diagnostics  int
}

// Result is the loop outcome.
type Result struct {
	Iterations int
	Status     string
	Reason     string
}

// Loop runs fn until diagnostics are clear, no progress, or max iterations.
// fn performs one diagnose-and-repair attempt and returns what changed.
func Loop(max int, fn func(iteration int) (Step, error)) (Result, error) {
	if max < 1 {
		max = 1
	}
	var prev string
	res := Result{}
	for i := 1; i <= max; i++ {
		step, err := fn(i)
		if err != nil {
			return res, err
		}
		res.Iterations = i
		key := join(step.Fingerprints)
		if step.Diagnostics == 0 {
			res.Status = "clear"
			res.Reason = "no diagnostics"
			return res, nil
		}
		if step.Applied == 0 || key == prev {
			res.Status = "needs_review"
			res.Reason = "no progress"
			return res, nil
		}
		prev = key
	}
	res.Status = "needs_review"
	res.Reason = "max iterations"
	return res, nil
}

func join(in []string) string {
	out := ""
	for _, s := range in {
		out += s + "\n"
	}
	return out
}
