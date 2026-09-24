package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
)

func TestCheckClassifiesAndQueriesOSV(t *testing.T) {
	var osvBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/osv":
			b, _ := io.ReadAll(r.Body)
			osvBody = string(b)
			var in struct {
				Queries []struct {
					Package struct{ Name string }
					Version string
				}
			}
			_ = json.Unmarshal(b, &in)
			var results []map[string]any
			for _, q := range in.Queries {
				if q.Package.Name == "golang.org/x/text" {
					results = append(results, map[string]any{"vulns": []map[string]string{{"id": "GO-2022-1059"}}})
				} else {
					results = append(results, map[string]any{})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		case strings.HasPrefix(r.URL.Path, "/pypi/pydantic/json"):
			_, _ = w.Write([]byte(`{"info":{"version":"2.11.7"}}`))
		case strings.HasPrefix(r.URL.Path, "/npm/left-pad/latest"):
			_, _ = w.Write([]byte(`{"version":"1.3.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{
		HTTP: srv.Client(), OSV: srv.URL + "/osv", PyPI: srv.URL + "/pypi", NPM: srv.URL + "/npm", Maven: srv.URL + "/maven",
		GoLookup: func(_ context.Context, qs []string) map[string]GoModule {
			out := map[string]GoModule{
				"github.com/googleapis/gax-go":    {Path: "github.com/googleapis/gax-go", Version: "v2.0.2+incompatible"},
				"github.com/googleapis/gax-go/v2": {Path: "github.com/googleapis/gax-go/v2", Version: "v2.21.0"},
				"golang.org/x/text":               {Path: "golang.org/x/text", Version: "v0.3.8"},
				"github.com/stretchr/testify":     {Path: "github.com/stretchr/testify", Version: "v1.12.1"},
			}
			return out
		},
	}
	deps := []domain.Dependency{
		{Ecosystem: "go", Name: "github.com/googleapis/gax-go", CurrentVersion: "v0.0.0-20161107002406-da06d194a00e", Direct: true},
		{Ecosystem: "go", Name: "golang.org/x/text", CurrentVersion: "v0.3.7", Direct: true},
		{Ecosystem: "go", Name: "github.com/stretchr/testify", CurrentVersion: "v1.12.1", Direct: true},
		{Ecosystem: "python", Name: "pydantic", CurrentVersion: "1.10.2", Direct: true},
		{Ecosystem: "node", Name: "left-pad", CurrentVersion: "^1.1.0", Direct: true},
	}
	got := map[string]Finding{}
	for _, f := range c.Check(context.Background(), deps) {
		got[f.Dependency] = f
	}
	if f := got["github.com/googleapis/gax-go"]; f.Status != domain.StatusUpdateAvailable || f.NextMajor != "github.com/googleapis/gax-go/v2@v2.21.0" {
		t.Fatalf("gax %+v", f)
	}
	if f := got["golang.org/x/text"]; f.Status != domain.StatusSecurityUpdate || len(f.Advisories) != 1 {
		t.Fatalf("text %+v", f)
	}
	if f := got["github.com/stretchr/testify"]; f.Status != domain.StatusCurrent {
		t.Fatalf("testify %+v", f)
	}
	if f := got["pydantic"]; f.Status != domain.StatusUpdateAvailable || f.Latest != "2.11.7" {
		t.Fatalf("pydantic %+v", f)
	}
	if f := got["left-pad"]; f.Status != domain.StatusUnresolved {
		t.Fatalf("left-pad %+v", f)
	}
	if !strings.Contains(osvBody, `"version":"0.3.7"`) || strings.Contains(osvBody, "^1.1.0") {
		t.Fatalf("osv query %s", osvBody)
	}
	Apply(deps, c.Check(context.Background(), deps), time.Now())
	if deps[1].Status != domain.StatusSecurityUpdate || deps[1].AdvisoryCheckedAt == nil {
		t.Fatalf("apply %+v", deps[1])
	}
}

func TestNextMajor(t *testing.T) {
	cases := map[[2]string]string{
		{"github.com/go-chi/chi/v5", "v5.0.12"}:         "github.com/go-chi/chi/v6",
		{"github.com/googleapis/gax-go", "v0.0.0-2016"}: "github.com/googleapis/gax-go/v2",
		{"gopkg.in/yaml.v3", "v3.0.1"}:                  "",
		{"github.com/alecthomas/chroma/v2", "v2.13.0"}:  "github.com/alecthomas/chroma/v3",
	}
	for in, want := range cases {
		if got := nextMajor(in[0], in[1]); got != want {
			t.Errorf("nextMajor(%v) = %q, want %q", in, got, want)
		}
	}
}
