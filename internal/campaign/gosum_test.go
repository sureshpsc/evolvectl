package campaign

import (
	"strings"
	"testing"
)

func TestLoweredReportsDowngrades(t *testing.T) {
	before := []byte("module m\n\ngo 1.21\n\nrequire (\n\tgithub.com/googleapis/gax-go v0.0.0-20161107002406-da06d194a00e\n\tgoogle.golang.org/grpc v1.84.0 // indirect\n\tgolang.org/x/net v0.59.0 // indirect\n)\n")
	after := []byte("module m\n\ngo 1.21\n\nrequire github.com/googleapis/gax-go/v2 v2.0.2\n\nrequire (\n\tgoogle.golang.org/grpc v1.83.0-dev // indirect\n\tgolang.org/x/net v0.60.0 // indirect\n)\n")
	got := lowered("go.mod", before, after, "github.com/googleapis/gax-go")
	if len(got) != 1 || !strings.Contains(got[0], "google.golang.org/grpc went down from v1.84.0 to v1.83.0-dev") {
		t.Fatalf("%q", got)
	}
	if got := lowered("go.mod", nil, after, "x"); got != nil {
		t.Fatalf("new file %q", got)
	}
}
