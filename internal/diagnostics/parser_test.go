package diagnostics

import "testing"

func TestGroupRepeatedGoErrors(t *testing.T) {
	log := "" +
		"client.go:10:2: undefined: grpc.DialContext\n" +
		"client.go:20:2: undefined: grpc.DialContext\n" +
		"other.go:3:1: undefined: grpc.DialContext\n"
	list := ParseToolOutput("go", log)
	if len(list) != 3 {
		t.Fatalf("diags %d", len(list))
	}
	groups := Group(list)
	if len(groups) != 1 || groups[0].Count != 3 {
		t.Fatalf("groups %+v", groups)
	}
}

func FuzzParseGo(f *testing.F) {
	f.Add("main.go:1:2: undefined: x")
	f.Fuzz(func(t *testing.T, s string) {
		_ = ParseToolOutput("go", s)
		_ = ParseToolOutput("copybara", s)
	})
}
