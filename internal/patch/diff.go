// Package patch builds unified diffs and can restore snapshotted files.
package patch

import (
	"bytes"
	"fmt"
	"strings"
)

// Unified returns a git-style unified diff for one file.
func Unified(path, oldSrc, newSrc string) string {
	if oldSrc == newSrc {
		return ""
	}
	oldLines := split(oldSrc)
	newLines := split(newSrc)
	ops := lcs(oldLines, newLines)
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines))
	for _, op := range ops {
		b.WriteString(op)
		if !strings.HasSuffix(op, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func lcs(a, c []string) []string {
	n, m := len(a), len(c)
	if n*m > 2_000_000 {
		var out []string
		for _, line := range a {
			out = append(out, "-"+line)
		}
		for _, line := range c {
			out = append(out, "+"+line)
		}
		return out
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == c[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == c[j] {
			out = append(out, " "+a[i])
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			out = append(out, "-"+a[i])
			i++
		} else {
			out = append(out, "+"+c[j])
			j++
		}
	}
	for i < n {
		out = append(out, "-"+a[i])
		i++
	}
	for j < m {
		out = append(out, "+"+c[j])
		j++
	}
	return out
}

// Join concatenates file diffs.
func Join(diffs []string) []byte {
	var b bytes.Buffer
	for _, d := range diffs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		b.WriteString(d)
		if !strings.HasSuffix(d, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}
