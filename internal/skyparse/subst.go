package skyparse

import "strings"

// substituteAssignments replaces top-level name = "literal" uses with the literal.
// Assignments inside parentheses are left alone. This does not execute Starlark.
func substituteAssignments(src string) string {
	values := topLevelStrings(src)
	if len(values) == 0 {
		return src
	}
	var b strings.Builder
	quote := byte(0)
	i := 0
	for i < len(src) {
		c := src[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
			} else if c == quote {
				quote = 0
			}
			i++
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			b.WriteByte(c)
			i++
			continue
		}
		if c == '#' {
			for i < len(src) && src[i] != '\n' {
				b.WriteByte(src[i])
				i++
			}
			continue
		}
		if isIdentStart(c) {
			j := i + 1
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			name := src[i:j]
			k := j
			for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			if lit, ok := values[name]; ok && (k >= len(src) || src[k] != '=') {
				b.WriteString(quoteLiteral(lit))
				i = j
				continue
			}
			b.WriteString(name)
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func topLevelStrings(src string) map[string]string {
	out := map[string]string{}
	depth := 0
	quote := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(src) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '#' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
			continue
		}
		if c == ')' || c == ']' || c == '}' {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth != 0 || !isIdentStart(c) {
			continue
		}
		j := i + 1
		for j < len(src) && isIdent(src[j]) {
			j++
		}
		name := src[i:j]
		k := j
		for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
			k++
		}
		if k >= len(src) || src[k] != '=' {
			i = j - 1
			continue
		}
		k++
		for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
			k++
		}
		if k >= len(src) || (src[k] != '"' && src[k] != '\'') {
			i = j - 1
			continue
		}
		lit, end := readQuoted(src, k)
		if end < 0 {
			break
		}
		out[name] = lit
		i = end
	}
	return out
}

func readQuoted(src string, start int) (string, int) {
	q := src[start]
	var b strings.Builder
	for i := start + 1; i < len(src); i++ {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			b.WriteByte(src[i+1])
			i++
			continue
		}
		if c == q {
			return b.String(), i
		}
		b.WriteByte(c)
	}
	return "", -1
}

func quoteLiteral(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
