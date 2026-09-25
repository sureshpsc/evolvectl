package patch

import (
	"fmt"
	"strings"
	"testing"
)

func numbered(n int, change map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if v, ok := change[i]; ok {
			if v != "" {
				b.WriteString(v + "\n")
			}
			continue
		}
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func TestUnifiedHunks(t *testing.T) {
	old := numbered(30, nil)
	cur := numbered(30, map[int]string{5: "line five", 25: ""})
	got := Unified("f.txt", old, cur)
	want := "--- a/f.txt\n+++ b/f.txt\n" +
		"@@ -2,7 +2,7 @@\n line 2\n line 3\n line 4\n-line 5\n+line five\n line 6\n line 7\n line 8\n" +
		"@@ -22,7 +22,6 @@\n line 22\n line 23\n line 24\n-line 25\n line 26\n line 27\n line 28\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUnifiedMergesNearbyChangesAndNewFile(t *testing.T) {
	old := numbered(10, nil)
	cur := numbered(10, map[int]string{3: "three", 8: "eight"})
	got := Unified("f", old, cur)
	if strings.Count(got, "@@ ") != 1 || !strings.Contains(got, "@@ -1,10 +1,10 @@") {
		t.Fatalf("%s", got)
	}
	created := Unified("new.go", "", "package x\n")
	if !strings.Contains(created, "@@ -0,0 +1 @@\n+package x\n") {
		t.Fatalf("%q", created)
	}
	if Unified("same", "a\n", "a\n") != "" {
		t.Fatal("no-op diff")
	}
}
