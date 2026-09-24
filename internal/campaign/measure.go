package campaign

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/commandadapt"
	"github.com/evolvectl/evolvectl/internal/diagnostics"
	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/fsio"
	"github.com/evolvectl/evolvectl/internal/quality"
	"github.com/evolvectl/evolvectl/internal/runner"
)

func (e *Executor) captureBaseline(ctx context.Context, rc *runCtx) {
	switch rc.report.Target.Ecosystem {
	case "go":
		e.runGoSuite(ctx, rc, "before", false)
	case "python":
		e.runPythonTests(ctx, rc, "before", false)
	}
	e.runAdapterSuite(ctx, rc, "before", false)
}

func (e *Executor) runGoSuite(ctx context.Context, rc *runCtx, phase string, gates bool) {
	goBin, ok := runner.Look("go")
	dirs := moduleDirs(rc)
	if !ok {
		if gates {
			rc.report.Validations = append(rc.report.Validations, gate("go test", strings.Join(dirs, ","), domain.GateUnavailable, true, []string{"go", "test", "./..."}, 127, "go is not on PATH"))
		}
		rc.report.Quality = quality.Add(rc.report.Quality, phase, []domain.CoverageSample{{
			Phase: phase, Tool: "go test", Scope: ".", Status: domain.GateUnavailable, Reason: "go is not on PATH",
		}}, nil, 0)
		return
	}
	for i, dir := range dirs {
		profileAbs, profileRel := coverageProfile(rc, phase, i)
		argv := []string{goBin, "test", "-count=1", "-cover", "-coverprofile", profileAbs, "./..."}
		run := runner.Run(ctx, runner.Request{Argv: argv, Dir: dir, Timeout: 3 * time.Minute, Env: moduleEnv(rc)})
		combined := run.Stdout + "\n" + run.Stderr
		samples, fails, passed := quality.ParseGoTest(combined)
		for i := range samples {
			samples[i].Phase = phase
			samples[i].Profile = profileRel
		}
		if st, err := os.Stat(profileAbs); err == nil && st.Size() > 0 {
			total := runner.Run(ctx, runner.Request{
				Argv: []string{goBin, "tool", "cover", "-func=" + profileAbs}, Dir: dir, Timeout: time.Minute, Env: moduleEnv(rc),
			})
			if pct, ok := quality.ParseCoverFunc(total.Stdout + "\n" + total.Stderr); ok {
				samples = append(samples, domain.CoverageSample{
					Phase: phase, Tool: "go tool cover", Scope: fsio.RelSlash(rc.workRoot, dir), Status: "recorded",
					Percent: pct, HasPercent: true, Profile: profileRel,
					Reason: "statement-weighted total from the cover profile",
				})
			}
		} else if len(samples) == 0 {
			samples = append(samples, domain.CoverageSample{
				Phase: phase, Tool: "go test", Scope: fsio.RelSlash(rc.workRoot, dir), Status: "partial",
				Reason: "go test did not write a coverage figure for this module",
			})
		}
		rc.report.Quality = quality.Add(rc.report.Quality, phase, samples, fails, passed)
		if !gates {
			continue
		}
		st := domain.GatePass
		if run.ExitCode != 0 {
			st = domain.GateFail
		}
		g := gate("go test", fsio.RelSlash(rc.workRoot, dir), st, true, argv, run.ExitCode, run.Err)
		g.Duration = run.Duration
		g.StartedAt = run.StartedAt
		g.EndedAt = run.EndedAt
		g.LogExcerpt = trimLog(combined)
		rc.report.Validations = append(rc.report.Validations, g)
		rc.report.Diagnostics = append(rc.report.Diagnostics, diagnostics.ParseToolOutput("go test", combined)...)
	}
}

func (e *Executor) runPythonTests(ctx context.Context, rc *runCtx, phase string, gates bool) {
	files := pythonTestFiles(rc)
	if len(files) == 0 {
		rc.report.Quality = quality.Add(rc.report.Quality, phase, []domain.CoverageSample{{
			Phase: phase, Tool: "pytest", Scope: ".", Status: domain.GateSkipped, Reason: "no Python test files in the inventory",
		}}, nil, 0)
		return
	}
	py := pythonBin()
	if py == "" {
		if gates {
			rc.report.Validations = append(rc.report.Validations, gate("pytest", strings.Join(files, ","), domain.GateUnavailable, true, []string{"python", "-m", "pytest"}, 127, "python is not on PATH"))
		}
		rc.report.Quality = quality.Add(rc.report.Quality, phase, []domain.CoverageSample{{
			Phase: phase, Tool: "pytest", Scope: ".", Status: domain.GateUnavailable, Reason: "python is not on PATH",
		}}, nil, 0)
		return
	}
	argv := []string{py, "-m", "pytest", "-v", "--tb=line"}
	tool := "pytest"
	if cov := runner.Run(ctx, runner.Request{Argv: []string{py, "-m", "coverage", "--version"}, Dir: rc.workRoot, Timeout: 20 * time.Second}); cov.ExitCode == 0 {
		argv = []string{py, "-m", "coverage", "run", "-m", "pytest", "-v", "--tb=line"}
		tool = "coverage"
	}
	run := runner.Run(ctx, runner.Request{Argv: argv, Dir: rc.workRoot, Timeout: 3 * time.Minute})
	combined := run.Stdout + "\n" + run.Stderr
	fails, passed := quality.ParsePytest(combined)
	sample := domain.CoverageSample{Phase: phase, Tool: tool, Scope: ".", Status: "recorded"}
	if strings.Contains(combined, "No module named pytest") {
		sample.Status = domain.GateUnavailable
		sample.Reason = "pytest is not installed"
		fails, passed = nil, 0
	} else if tool == "coverage" {
		rep := runner.Run(ctx, runner.Request{Argv: []string{py, "-m", "coverage", "report"}, Dir: rc.workRoot, Timeout: time.Minute})
		if pct, ok := quality.ParseCoverageTotal(rep.Stdout + "\n" + rep.Stderr); ok {
			sample.HasPercent = true
			sample.Percent = pct
			sample.Reason = "statement coverage from coverage.py"
		} else {
			sample.Status = "partial"
			sample.Reason = "pytest ran; coverage.py did not print a TOTAL row"
		}
	} else {
		sample.Status = domain.GateUnavailable
		sample.Reason = "pytest ran; coverage.py is not installed"
	}
	rc.report.Quality = quality.Add(rc.report.Quality, phase, []domain.CoverageSample{sample}, fails, passed)
	if !gates {
		return
	}
	st := domain.GatePass
	reason := ""
	if sample.Status == domain.GateUnavailable && sample.Reason == "pytest is not installed" {
		st = domain.GateUnavailable
		reason = sample.Reason
	} else if run.ExitCode != 0 {
		st = domain.GateFail
		reason = run.Err
	}
	g := gate("pytest", ".", st, true, argv, run.ExitCode, reason)
	g.Duration = run.Duration
	g.StartedAt = run.StartedAt
	g.EndedAt = run.EndedAt
	g.LogExcerpt = trimLog(combined)
	rc.report.Validations = append(rc.report.Validations, g)
	rc.report.Diagnostics = append(rc.report.Diagnostics, diagnostics.ParseToolOutput("pytest", combined)...)
}

func (e *Executor) runAdapterSuite(ctx context.Context, rc *runCtx, phase string, gates bool) {
	adapters, _ := commandadapt.LoadDir(filepath.Join(rc.origRoot, ".evolvectl", "adapters"))
	target := rc.report.Target
	for _, a := range adapters {
		cmd, ok := a.Spec.Commands["test"]
		if !ok {
			cmd, ok = a.Spec.Commands["build"]
		}
		if !ok {
			continue
		}
		argv, err := commandadapt.Render(cmd.Argv, map[string]string{
			"root": rc.workRoot, "project.root": rc.workRoot, "dependency": target.Name, "version": target.To,
		})
		if err != nil {
			if gates {
				rc.report.Validations = append(rc.report.Validations, gate(a.Metadata.Name, a.Path, domain.GateBlocked, cmd.Required, cmd.Argv, 0, err.Error()))
			}
			continue
		}
		run := runner.Run(ctx, runner.Request{Argv: argv, Dir: rc.workRoot, Timeout: commandadapt.Timeout(cmd.Timeout)})
		st := domain.GatePass
		if run.ExitCode != 0 {
			st = domain.GateFail
		}
		if run.Err != "" && run.ExitCode == 127 {
			st = domain.GateUnavailable
		}
		recordAdapterResult(rc, phase, a.Metadata.Name, st)
		if !gates {
			continue
		}
		g := gate(a.Metadata.Name+" "+strings.Join(argv, " "), ".", st, cmd.Required, argv, run.ExitCode, run.Err)
		g.Duration = run.Duration
		g.StartedAt = run.StartedAt
		g.EndedAt = run.EndedAt
		g.LogExcerpt = trimLog(run.Stdout + run.Stderr)
		rc.report.Validations = append(rc.report.Validations, g)
		rc.report.Diagnostics = append(rc.report.Diagnostics, diagnostics.ParseToolOutput(a.Metadata.Name, run.Stdout+"\n"+run.Stderr)...)
	}
}

func recordAdapterResult(rc *runCtx, phase, name, status string) {
	var fails []domain.TestRef
	passed := 0
	switch status {
	case domain.GateFail:
		fails = []domain.TestRef{{Package: name, Name: "adapter test", Status: "FAIL"}}
	case domain.GatePass:
		passed = 1
	default:
		rc.report.Quality = quality.Add(rc.report.Quality, phase, []domain.CoverageSample{{
			Phase: phase, Tool: name, Scope: ".", Status: status, Reason: "command adapter did not produce a pass or fail",
		}}, nil, 0)
		return
	}
	rc.report.Quality = quality.Add(rc.report.Quality, phase, []domain.CoverageSample{{
		Phase: phase, Tool: name, Scope: ".", Status: domain.GateUnavailable, Reason: "command adapter does not report coverage",
	}}, fails, passed)
}

func coverageProfile(rc *runCtx, phase string, index int) (abs, rel string) {
	rel = fmt.Sprintf(".evolvectl/runs/%s/cover-%s-%d.out", rc.report.ID, phase, index)
	abs = filepath.Join(rc.origRoot, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o700)
	return abs, rel
}

func moduleEnv(rc *runCtx) []string {
	if rc.req.Offline {
		return []string{"GOSUMDB=off", "GOPROXY=off"}
	}
	return nil
}

func pythonBin() string {
	if p, ok := runner.Look("python"); ok {
		return p
	}
	if p, ok := runner.Look("python3"); ok {
		return p
	}
	return ""
}

func pythonTestFiles(rc *runCtx) []string {
	var out []string
	for _, f := range rc.report.Inventory.SourceFiles {
		if f.Language != "" && f.Language != "python" {
			continue
		}
		slash := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(slash, ".py") {
			continue
		}
		base := slash
		if i := strings.LastIndex(slash, "/"); i >= 0 {
			base = slash[i+1:]
		}
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || strings.Contains(slash, "/tests/") {
			out = append(out, slash)
		}
	}
	return out
}

func annotateQuality(r *domain.RunReport) {
	q := r.Quality
	if q == nil || !q.AfterRecorded {
		return
	}
	switch {
	case len(q.NewlyFailed) > 0:
		extra := fmt.Sprintf("%d test(s) failed only after the upgrade", len(q.NewlyFailed))
		if r.OutcomeReason == "" {
			r.OutcomeReason = extra
		} else if !strings.Contains(r.OutcomeReason, "only after the upgrade") {
			r.OutcomeReason += "; " + extra
		}
		r.NextStep = "open .evolvectl/runs/" + r.ID + "/report.html#quality"
	case r.Outcome == domain.OutcomeFailed && len(q.StillFailing) > 0:
		r.OutcomeReason = "required validation failed; the same tests were already failing before the upgrade"
	}
}
