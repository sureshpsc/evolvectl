package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evolvectl/evolvectl/internal/exitcode"
	"github.com/evolvectl/evolvectl/internal/registry"
)

func TestOutdatedOnlineReportsNextMajor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Queries []any }
		_ = json.NewDecoder(r.Body).Decode(&in)
		results := make([]map[string]any, len(in.Queries))
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer srv.Close()
	ws := copyExample(t, "go-gax-v2")
	a := App{Registry: &registry.Client{
		HTTP: srv.Client(), OSV: srv.URL,
		GoLookup: func(_ context.Context, qs []string) map[string]registry.GoModule {
			return map[string]registry.GoModule{
				"github.com/googleapis/gax-go":    {Path: "github.com/googleapis/gax-go", Version: "v2.0.2+incompatible"},
				"github.com/googleapis/gax-go/v2": {Path: "github.com/googleapis/gax-go/v2", Version: "v2.21.0"},
			}
		},
	}}
	buf := &strings.Builder{}
	a.Out, a.Err = buf, &strings.Builder{}
	code, err := a.Outdated(context.Background(), Option{Workspace: ws}, true)
	if err != nil || code != exitcode.Success {
		t.Fatalf("code=%d err=%v\n%s", code, err, buf)
	}
	out := buf.String()
	if !strings.Contains(out, "github.com/googleapis/gax-go") || !strings.Contains(out, "update_available") || !strings.Contains(out, "gax-go/v2@v2.21.0") {
		t.Fatal(out)
	}
	if code, err := a.Outdated(context.Background(), Option{Workspace: ws, Offline: true}, true); err == nil || code != exitcode.Invalid {
		t.Fatalf("online+offline code=%d err=%v", code, err)
	}
}
