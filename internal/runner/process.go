// Package runner executes argv arrays with context, timeout, and bounded output.
package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/redact"
)

const maxCapture = 1 << 20

// Request is one command execution.
type Request struct {
	Argv    []string
	Dir     string
	Timeout time.Duration
	Env     []string
}

// Run executes req.Argv[0] without a shell.
func Run(ctx context.Context, req Request) domain.ToolRun {
	start := time.Now()
	run := domain.ToolRun{Argv: append([]string(nil), req.Argv...), Dir: req.Dir, StartedAt: start.UTC()}
	if len(req.Argv) == 0 {
		run.Err = "empty argv"
		run.ExitCode = 127
		run.EndedAt = time.Now().UTC()
		run.Duration = run.EndedAt.Sub(start)
		return run
	}
	cctx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		cctx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(cctx, req.Argv[0], req.Argv[1:]...)
	cmd.Dir = req.Dir
	if len(req.Env) > 0 {
		cmd.Env = append(os.Environ(), req.Env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{buf: &stdout, limit: maxCapture}
	cmd.Stderr = &limitWriter{buf: &stderr, limit: maxCapture}
	err := cmd.Run()
	run.EndedAt = time.Now().UTC()
	run.Duration = run.EndedAt.Sub(start)
	run.Stdout = redact.Text(stdout.String())
	run.Stderr = redact.Text(stderr.String())
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			run.ExitCode = ee.ExitCode()
		} else if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			run.ExitCode = 124
			run.Err = "timeout"
		} else if errors.Is(cctx.Err(), context.Canceled) {
			run.ExitCode = 130
			run.Err = "cancelled"
		} else {
			run.ExitCode = 127
			run.Err = err.Error()
		}
	}
	return run
}

// Look reports whether name is on PATH.
func Look(name string) (string, bool) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return p, true
}

type limitWriter struct {
	buf     *bytes.Buffer
	limit   int
	dropped bool
}

func (w *limitWriter) Write(p []byte) (int, error) {
	remain := w.limit - w.buf.Len()
	if remain > 0 {
		if len(p) > remain {
			_, _ = w.buf.Write(p[:remain])
			w.dropped = true
		} else {
			_, _ = w.buf.Write(p)
		}
	} else {
		w.dropped = true
	}
	if w.dropped && !bytes.HasSuffix(w.buf.Bytes(), []byte("[truncated]\n")) {
		_, _ = w.buf.WriteString("\n[truncated]\n")
	}
	return len(p), nil
}
