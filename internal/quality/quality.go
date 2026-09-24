// Package quality compares test and coverage results taken before and after an upgrade.
package quality

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/evolvectl/evolvectl/internal/domain"
)

const listCap = 500

// Add records one command's results for a phase and refreshes the before/after diff.
func Add(q *domain.Quality, phase string, samples []domain.CoverageSample, fails []domain.TestRef, passed int) *domain.Quality {
	if q == nil {
		q = &domain.Quality{}
	}
	if phase == "before" {
		q.Before = append(q.Before, samples...)
		q.BaselineFailures = append(q.BaselineFailures, fails...)
		q.BaselinePassed += passed
	} else {
		q.After = append(q.After, samples...)
		q.AfterFailures = append(q.AfterFailures, fails...)
		q.AfterPassed += passed
		q.AfterRecorded = true
	}
	q.BaselineFailures = capRefs(q, q.BaselineFailures)
	q.AfterFailures = capRefs(q, q.AfterFailures)
	q.BaselineFailed = len(q.BaselineFailures)
	q.AfterFailed = len(q.AfterFailures)
	Recompute(q)
	return q
}

// ResetAfter clears post-upgrade results so a later validation pass replaces them.
func ResetAfter(q *domain.Quality) *domain.Quality {
	if q == nil {
		return &domain.Quality{}
	}
	q.After = nil
	q.AfterFailures = nil
	q.AfterPassed = 0
	q.AfterFailed = 0
	q.AfterRecorded = false
	q.NewlyFailed = nil
	q.StillFailing = nil
	q.Fixed = nil
	return q
}

// Recompute fills newly failed, still failing, and fixed from the stored failure lists.
func Recompute(q *domain.Quality) {
	if q == nil || !q.AfterRecorded {
		return
	}
	before := map[string]domain.TestRef{}
	for _, t := range q.BaselineFailures {
		before[key(t)] = t
	}
	after := map[string]domain.TestRef{}
	for _, t := range q.AfterFailures {
		after[key(t)] = t
	}
	var newly, still, fixed []domain.TestRef
	for k, t := range after {
		if _, ok := before[k]; ok {
			still = append(still, t)
		} else {
			newly = append(newly, t)
		}
	}
	for k, t := range before {
		if _, ok := after[k]; !ok {
			fixed = append(fixed, t)
		}
	}
	sortRefs(newly)
	sortRefs(still)
	sortRefs(fixed)
	q.NewlyFailed = capRefs(q, newly)
	q.StillFailing = capRefs(q, still)
	q.Fixed = capRefs(q, fixed)
	q.BaselineFailed = len(q.BaselineFailures)
	q.AfterFailed = len(q.AfterFailures)
}

// ParseGoTest reads ordinary `go test` text, including coverage lines.
func ParseGoTest(text string) (samples []domain.CoverageSample, fails []domain.TestRef, passed int) {
	var pending []domain.TestRef
	flush := func(pkg, kind string) {
		if kind == "FAIL" && len(pending) == 0 {
			fails = append(fails, domain.TestRef{Package: pkg, Name: "(build)", Status: "FAIL"})
		}
		for _, c := range pending {
			c.Package = pkg
			switch c.Status {
			case "FAIL":
				fails = append(fails, c)
			case "PASS":
				passed++
			}
		}
		pending = nil
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "--- PASS: "), strings.HasPrefix(line, "--- FAIL: "), strings.HasPrefix(line, "--- SKIP: "):
			rest := line[len("--- "):]
			status, name, _ := strings.Cut(rest, ": ")
			name = strings.TrimSpace(name)
			if i := strings.IndexByte(name, ' '); i >= 0 {
				name = name[:i]
			}
			if name == "" {
				continue
			}
			pending = append(pending, domain.TestRef{Name: name, Status: status})
		case strings.HasPrefix(line, "ok\t"), strings.HasPrefix(line, "FAIL\t"), strings.HasPrefix(line, "?\t"):
			parts := strings.Split(line, "\t")
			kind := parts[0]
			pkg := ""
			if len(parts) > 1 {
				pkg = strings.TrimSpace(parts[1])
			}
			if kind == "?" {
				pending = nil
				continue
			}
			for _, p := range parts {
				if pct, ok := coveragePercent(p); ok {
					samples = append(samples, domain.CoverageSample{
						Tool: "go test", Scope: pkg, Status: "recorded", Percent: pct, HasPercent: true,
					})
				}
			}
			flush(pkg, kind)
		}
	}
	return samples, fails, passed
}

// ParseCoverFunc reads the total line from `go tool cover -func`.
func ParseCoverFunc(text string) (float64, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "total:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return 0, false
		}
		raw := strings.TrimSuffix(fields[len(fields)-1], "%")
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// ParsePytest reads `pytest -v` lines.
func ParsePytest(text string) (fails []domain.TestRef, passed int) {
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		status := ""
		name := ""
		switch {
		case strings.Contains(line, " PASSED"):
			status = "PASS"
			name = strings.TrimSpace(strings.TrimSuffix(line, "PASSED"))
		case strings.Contains(line, " FAILED"):
			status = "FAIL"
			name = strings.TrimSpace(strings.TrimSuffix(line, "FAILED"))
		case strings.Contains(line, " ERROR"):
			status = "FAIL"
			name = strings.TrimSpace(strings.TrimSuffix(line, "ERROR"))
		case strings.HasPrefix(line, "FAILED "):
			status = "FAIL"
			name = strings.TrimSpace(strings.TrimPrefix(line, "FAILED "))
			if i := strings.Index(name, " - "); i >= 0 {
				name = name[:i]
			}
		}
		if name == "" || seen[status+"\x00"+name] {
			continue
		}
		seen[status+"\x00"+name] = true
		ref := domain.TestRef{Package: name, Name: name, Status: status}
		if i := strings.LastIndex(name, "::"); i >= 0 {
			ref.Package = name[:i]
			ref.Name = name[i+2:]
		}
		if status == "FAIL" {
			fails = append(fails, ref)
		} else if status == "PASS" {
			passed++
		}
	}
	return fails, passed
}

// ParseCoverageTotal reads the TOTAL row from `coverage report`.
func ParseCoverageTotal(text string) (float64, bool) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 || fields[0] != "TOTAL" {
			continue
		}
		raw := strings.TrimSuffix(fields[len(fields)-1], "%")
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func coveragePercent(field string) (float64, bool) {
	field = strings.TrimSpace(field)
	if !strings.HasPrefix(field, "coverage:") {
		return 0, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(field, "coverage:"))
	raw, _, _ := strings.Cut(rest, "%")
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func key(t domain.TestRef) string {
	return t.Package + "\x00" + t.Name
}

func sortRefs(list []domain.TestRef) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Package == list[j].Package {
			return list[i].Name < list[j].Name
		}
		return list[i].Package < list[j].Package
	})
}

func capRefs(q *domain.Quality, list []domain.TestRef) []domain.TestRef {
	if len(list) <= listCap {
		return list
	}
	q.Note = fmt.Sprintf("failure lists are capped at %d entries", listCap)
	return list[:listCap]
}
