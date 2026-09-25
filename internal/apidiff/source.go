package apidiff

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/sureshpsc/evolvectl/internal/runner"
)

// Download is the module cache entry reported by go mod download -json.
type Download struct {
	Path    string
	Version string
	Dir     string
	Sum     string
	GoMod   string
	Error   string
}

// Fetch runs go mod download -json module@version outside any module, so the workspace go.mod
// and go.sum are not touched. GOPROXY, GOPRIVATE, and GONOSUMDB from the environment apply.
// Offline allows only the local module cache.
func Fetch(ctx context.Context, module, version string, offline bool) (Download, error) {
	goBin, ok := runner.Look("go")
	if !ok {
		return Download{}, fmt.Errorf("go is not on PATH")
	}
	tmp, err := os.MkdirTemp("", "evolvectl-mod-*")
	if err != nil {
		return Download{}, err
	}
	defer os.RemoveAll(tmp)
	env := []string{"GOFLAGS=-mod=mod", "GO111MODULE=on"}
	if offline {
		env = append(env, "GOPROXY=off", "GOSUMDB=off")
	}
	run := runner.Run(ctx, runner.Request{
		Argv: []string{goBin, "mod", "download", "-json", module + "@" + version},
		Dir:  tmp, Timeout: 3 * time.Minute, Env: env,
	})
	var d Download
	if err := json.Unmarshal([]byte(run.Stdout), &d); err != nil {
		msg := strings.TrimSpace(run.Stderr + " " + run.Err)
		if msg == "" {
			msg = err.Error()
		}
		return Download{}, fmt.Errorf("go mod download %s@%s: %s", module, version, msg)
	}
	if d.Error != "" {
		return d, fmt.Errorf("go mod download %s@%s: %s", module, version, d.Error)
	}
	if run.ExitCode != 0 || d.Dir == "" {
		return d, fmt.Errorf("go mod download %s@%s: %s", module, version, strings.TrimSpace(run.Stderr))
	}
	return d, nil
}

// Source returns the directory holding module@version. A local replace in the go.mod at
// gomodPath wins, so offline fixtures and forks resolve without the network.
func Source(ctx context.Context, gomodPath, module, version string, offline bool) (string, error) {
	if body, err := os.ReadFile(gomodPath); err == nil {
		if f, err := modfile.Parse(gomodPath, body, nil); err == nil {
			for _, r := range f.Replace {
				if r.Old.Path != module || r.New.Version != "" || r.New.Path == "" {
					continue
				}
				if r.Old.Version != "" && r.Old.Version != version {
					continue
				}
				dir := r.New.Path
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(filepath.Dir(gomodPath), filepath.FromSlash(dir))
				}
				return dir, nil
			}
		}
	}
	d, err := Fetch(ctx, module, version, offline)
	if err != nil {
		return "", err
	}
	return d.Dir, nil
}
