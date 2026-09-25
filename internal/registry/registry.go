// Package registry looks up the latest published versions and known advisories for declared
// dependencies. Go versions come from the go command, so GOPROXY, GOPRIVATE, and GONOSUMDB
// apply. Advisories come from the OSV API. Nothing here runs unless the caller asks.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/modmove"
	"github.com/sureshpsc/evolvectl/internal/runner"
)

// Client holds endpoints so tests can point them at a local server.
type Client struct {
	HTTP     *http.Client
	OSV      string
	PyPI     string
	NPM      string
	Maven    string
	GoLookup func(ctx context.Context, queries []string) map[string]GoModule
}

// GoModule is one go list -m -json result.
type GoModule struct {
	Path    string
	Version string
	Error   *struct{ Err string }
}

// Finding is the registry result for one dependency.
type Finding struct {
	Dependency string   `json:"dependency"`
	Ecosystem  string   `json:"ecosystem"`
	Current    string   `json:"current"`
	Latest     string   `json:"latest,omitempty"`
	NextMajor  string   `json:"next_major,omitempty"`
	Advisories []string `json:"advisories,omitempty"`
	Status     string   `json:"status"`
	Reason     string   `json:"reason,omitempty"`
}

// New returns a client for the public endpoints.
func New() *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: 20 * time.Second},
		OSV:   "https://api.osv.dev/v1/querybatch",
		PyPI:  "https://pypi.org/pypi",
		NPM:   "https://registry.npmjs.org",
		Maven: "https://search.maven.org/solrsearch/select",
	}
}

// Check fills Status, Latest, and Advisories for direct dependencies and updates the inventory rows.
func (c *Client) Check(ctx context.Context, deps []domain.Dependency) []Finding {
	byKey := map[string]*Finding{}
	var order []string
	for _, d := range deps {
		if !d.Direct {
			continue
		}
		k := d.Ecosystem + "\x00" + d.Name + "\x00" + d.CurrentVersion
		if _, ok := byKey[k]; ok {
			continue
		}
		byKey[k] = &Finding{Dependency: d.Name, Ecosystem: d.Ecosystem, Current: d.CurrentVersion, Status: domain.StatusUnknown}
		order = append(order, k)
	}
	var goFindings []*Finding
	for _, k := range order {
		if byKey[k].Ecosystem == "go" {
			goFindings = append(goFindings, byKey[k])
		}
	}
	c.goLatest(ctx, goFindings)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, k := range order {
		f := byKey[k]
		if f.Ecosystem == "go" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c.otherLatest(ctx, f)
		}()
	}
	wg.Wait()
	var all []*Finding
	for _, k := range order {
		all = append(all, byKey[k])
	}
	c.advisories(ctx, all)
	out := make([]Finding, 0, len(all))
	for _, f := range all {
		classify(f)
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		return out[i].Dependency < out[j].Dependency
	})
	return out
}

// Apply copies findings onto inventory rows.
func Apply(deps []domain.Dependency, findings []Finding, at time.Time) {
	idx := map[string]Finding{}
	for _, f := range findings {
		idx[f.Ecosystem+"\x00"+f.Dependency+"\x00"+f.Current] = f
	}
	for i := range deps {
		f, ok := idx[deps[i].Ecosystem+"\x00"+deps[i].Name+"\x00"+deps[i].CurrentVersion]
		if !ok {
			continue
		}
		deps[i].Status = f.Status
		deps[i].StatusReason = f.Reason
		deps[i].TargetVersion = f.Latest
		t := at
		deps[i].AvailabilityCheckedAt = &t
		deps[i].AdvisoryCheckedAt = &t
	}
}

func classify(f *Finding) {
	var parts []string
	switch {
	case f.Latest == "":
		if f.Status == domain.StatusUnknown && f.Reason == "" {
			f.Reason = "latest version unavailable"
		}
	case !exact(f.Current):
		f.Status = domain.StatusUnresolved
		parts = append(parts, "latest "+f.Latest+"; declared range "+f.Current+" was not resolved")
	case sameVersion(f.Current, f.Latest):
		f.Status = domain.StatusCurrent
		parts = append(parts, "latest "+f.Latest)
	case newer(f.Ecosystem, f.Current, f.Latest):
		f.Status = domain.StatusUpdateAvailable
		parts = append(parts, "latest "+f.Latest)
	default:
		f.Status = domain.StatusCurrent
		parts = append(parts, "declared "+f.Current+" is not older than latest "+f.Latest)
	}
	if f.NextMajor != "" {
		parts = append(parts, "new major module "+f.NextMajor)
	}
	if len(f.Advisories) > 0 {
		f.Status = domain.StatusSecurityUpdate
		parts = append(parts, fmt.Sprintf("%d advisory(ies): %s", len(f.Advisories), strings.Join(f.Advisories, ", ")))
	}
	if len(parts) > 0 {
		if f.Reason != "" {
			parts = append(parts, f.Reason)
		}
		f.Reason = strings.Join(parts, "; ")
	}
}

func sameVersion(a, b string) bool {
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}

func newer(eco, current, latest string) bool {
	if eco == "go" {
		return semver.Compare(latest, current) > 0
	}
	cv, lv := "v"+strings.TrimPrefix(current, "v"), "v"+strings.TrimPrefix(latest, "v")
	if semver.IsValid(cv) && semver.IsValid(lv) {
		return semver.Compare(lv, cv) > 0
	}
	return current != latest
}

// exact is false for ranges such as ^1.2.0, ~=2.0, or >=1.
func exact(v string) bool {
	return v != "" && v != "latest" && !strings.ContainsAny(v, "^~<>=*|, ") && !strings.HasSuffix(v, ".x")
}

func (c *Client) goLatest(ctx context.Context, fs []*Finding) {
	if len(fs) == 0 {
		return
	}
	var queries []string
	for _, f := range fs {
		queries = append(queries, f.Dependency+"@latest")
		if next := nextMajor(f.Dependency, f.Current); next != "" {
			queries = append(queries, next+"@latest")
		}
	}
	lookup := c.GoLookup
	if lookup == nil {
		lookup = goList
	}
	res := lookup(ctx, queries)
	for _, f := range fs {
		m, ok := res[f.Dependency]
		switch {
		case !ok:
			f.Reason = "go list returned no result"
		case m.Error != nil:
			f.Reason = "go list: " + m.Error.Err
		default:
			f.Latest = m.Version
		}
		if next := nextMajor(f.Dependency, f.Current); next != "" {
			if m, ok := res[next]; ok && m.Error == nil && m.Version != "" {
				f.NextMajor = next + "@" + m.Version
			}
		}
	}
}

// nextMajor is the module path one major version above the current one.
func nextMajor(module, version string) string {
	if strings.HasPrefix(module, "gopkg.in/") {
		return ""
	}
	parts := strings.Split(module, "/")
	last := parts[len(parts)-1]
	if len(last) > 1 && last[0] == 'v' {
		n := 0
		if _, err := fmt.Sscanf(last[1:], "%d", &n); err == nil && fmt.Sprintf("v%d", n) == last {
			return strings.Join(parts[:len(parts)-1], "/") + fmt.Sprintf("/v%d", n+1)
		}
	}
	major := semver.Major(strings.TrimSuffix(version, "+incompatible"))
	if major == "v0" || major == "v1" || major == "" {
		return module + "/v2"
	}
	return modmove.SuggestMajorPath(module, version)
}

// goList runs go list -m -e -json <module@latest>... outside any module.
func goList(ctx context.Context, queries []string) map[string]GoModule {
	out := map[string]GoModule{}
	goBin, ok := runner.Look("go")
	if !ok {
		return out
	}
	tmp, err := os.MkdirTemp("", "evolvectl-list-*")
	if err != nil {
		return out
	}
	defer os.RemoveAll(tmp)
	for start := 0; start < len(queries); start += 50 {
		end := start + 50
		if end > len(queries) {
			end = len(queries)
		}
		argv := append([]string{goBin, "list", "-m", "-e", "-json"}, queries[start:end]...)
		run := runner.Run(ctx, runner.Request{Argv: argv, Dir: tmp, Timeout: 3 * time.Minute, Env: []string{"GOFLAGS=-mod=mod", "GO111MODULE=on"}})
		dec := json.NewDecoder(strings.NewReader(run.Stdout))
		for {
			var m GoModule
			if err := dec.Decode(&m); err != nil {
				break
			}
			out[m.Path] = m
		}
	}
	return out
}

func (c *Client) otherLatest(ctx context.Context, f *Finding) {
	var err error
	switch f.Ecosystem {
	case "python":
		name := strings.Split(f.Dependency, "[")[0]
		var doc struct {
			Info struct{ Version string } `json:"info"`
		}
		err = c.getJSON(ctx, c.PyPI+"/"+url.PathEscape(name)+"/json", &doc)
		f.Latest = doc.Info.Version
	case "node":
		var doc struct{ Version string }
		err = c.getJSON(ctx, c.NPM+"/"+strings.ReplaceAll(url.PathEscape(f.Dependency), "%40", "@")+"/latest", &doc)
		f.Latest = doc.Version
	case "maven":
		g, a, ok := strings.Cut(f.Dependency, ":")
		if !ok {
			f.Reason = "maven coordinate is not group:artifact"
			return
		}
		q := url.Values{"q": {fmt.Sprintf(`g:"%s" AND a:"%s"`, g, a)}, "rows": {"1"}, "wt": {"json"}}
		var doc struct {
			Response struct {
				Docs []struct {
					LatestVersion string `json:"latestVersion"`
				} `json:"docs"`
			} `json:"response"`
		}
		err = c.getJSON(ctx, c.Maven+"?"+q.Encode(), &doc)
		if len(doc.Response.Docs) > 0 {
			f.Latest = doc.Response.Docs[0].LatestVersion
		}
	default:
		f.Status = domain.StatusNotSupported
		f.Reason = "no registry lookup for " + f.Ecosystem
		return
	}
	if err != nil {
		f.Status = domain.StatusRegistryUnavailable
		f.Reason = err.Error()
	}
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}

var osvEcosystem = map[string]string{"go": "Go", "python": "PyPI", "node": "npm", "maven": "Maven"}

// advisories queries OSV for every finding with an exact version.
func (c *Client) advisories(ctx context.Context, fs []*Finding) {
	type pkg struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	}
	type query struct {
		Package pkg    `json:"package"`
		Version string `json:"version"`
	}
	var qs []query
	var targets []*Finding
	for _, f := range fs {
		eco, ok := osvEcosystem[f.Ecosystem]
		if !ok || !exact(f.Current) {
			continue
		}
		v := f.Current
		if f.Ecosystem == "go" {
			v = strings.TrimPrefix(strings.TrimSuffix(v, "+incompatible"), "v")
		}
		name := f.Dependency
		if f.Ecosystem == "python" {
			name = strings.Split(name, "[")[0]
		}
		qs = append(qs, query{Package: pkg{Ecosystem: eco, Name: name}, Version: v})
		targets = append(targets, f)
	}
	for start := 0; start < len(qs); start += 500 {
		end := start + 500
		if end > len(qs) {
			end = len(qs)
		}
		body, _ := json.Marshal(map[string]any{"queries": qs[start:end]})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.OSV, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			for _, f := range targets[start:end] {
				f.Reason = strings.TrimPrefix(f.Reason+"; advisories unavailable: "+err.Error(), "; ")
			}
			continue
		}
		var doc struct {
			Results []struct {
				Vulns []struct {
					ID string `json:"id"`
				} `json:"vulns"`
			} `json:"results"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&doc)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			for _, f := range targets[start:end] {
				f.Reason = strings.TrimPrefix(f.Reason+fmt.Sprintf("; advisories unavailable: HTTP %d", resp.StatusCode), "; ")
			}
			continue
		}
		for i, r := range doc.Results {
			if start+i >= len(targets) {
				break
			}
			for _, v := range r.Vulns {
				targets[start+i].Advisories = append(targets[start+i].Advisories, v.ID)
			}
		}
	}
}
