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
	for _, h := range hunks(ops, context) {
		fmt.Fprintf(&b, "@@ -%s +%s @@\n", rangeOf(h.oldStart, h.oldLen), rangeOf(h.newStart, h.newLen))
		for _, op := range ops[h.from:h.to] {
			b.WriteString(op)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

const context = 3

type hunk struct {
	from, to                           int
	oldStart, oldLen, newStart, newLen int
}

// hunks groups changed lines with up to n lines of unchanged context on each side, merging
// groups whose context overlaps, as diff -u does.
func hunks(ops []string, n int) []hunk {
	var out []hunk
	oldLine, newLine := make([]int, len(ops)+1), make([]int, len(ops)+1)
	o, w := 1, 1
	for i, op := range ops {
		oldLine[i], newLine[i] = o, w
		switch op[0] {
		case ' ':
			o++
			w++
		case '-':
			o++
		case '+':
			w++
		}
	}
	oldLine[len(ops)], newLine[len(ops)] = o, w
	for i := 0; i < len(ops); {
		if ops[i][0] == ' ' {
			i++
			continue
		}
		start := max(0, i-n)
		end := i
		for end < len(ops) {
			if ops[end][0] != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run][0] == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*n {
				end = min(len(ops), end+n)
				break
			}
			end = run
		}
		h := hunk{from: start, to: end, oldStart: oldLine[start], newStart: newLine[start]}
		for _, op := range ops[start:end] {
			if op[0] != '+' {
				h.oldLen++
			}
			if op[0] != '-' {
				h.newLen++
			}
		}
		out = append(out, h)
		i = end
	}
	return out
}

func rangeOf(start, length int) string {
	if length == 0 {
		start--
	}
	if length == 1 {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d,%d", start, length)
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
