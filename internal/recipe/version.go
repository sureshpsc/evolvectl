package recipe

import (
	"strconv"
	"strings"
)

// ConstraintsAllow reports whether version satisfies a space-separated constraint list.
// Supported operators are >=, >, <=, <, ==, and =. A leading v is ignored.
func ConstraintsAllow(expr, version string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	for _, part := range strings.Fields(expr) {
		if !oneConstraint(part, version) {
			return false
		}
	}
	return true
}

func oneConstraint(part, version string) bool {
	ops := []string{">=", "<=", "==", "!=", ">", "<", "="}
	op := ""
	rest := part
	for _, candidate := range ops {
		if strings.HasPrefix(part, candidate) {
			op = candidate
			rest = strings.TrimSpace(part[len(candidate):])
			break
		}
	}
	if op == "" {
		op = "=="
		rest = part
	}
	cmp := Compare(version, rest)
	switch op {
	case ">=":
		return cmp >= 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<":
		return cmp < 0
	case "==", "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	default:
		return false
	}
}

// Compare orders two dotted versions. It returns -1, 0, or 1.
func Compare(a, b string) int {
	aa := parts(a)
	bb := parts(b)
	n := len(aa)
	if len(bb) > n {
		n = len(bb)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(aa) {
			av = aa[i]
		}
		if i < len(bb) {
			bv = bb[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parts(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	raw := strings.Split(v, ".")
	out := make([]int, 0, len(raw))
	for _, p := range raw {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		if p == "" {
			continue
		}
		if _, err := strconv.Atoi(trimNum(p)); err == nil {
			n, _ = strconv.Atoi(trimNum(p))
		}
		out = append(out, n)
	}
	return out
}

func trimNum(p string) string {
	i := 0
	for i < len(p) && p[i] >= '0' && p[i] <= '9' {
		i++
	}
	if i == 0 {
		return "0"
	}
	return p[:i]
}
