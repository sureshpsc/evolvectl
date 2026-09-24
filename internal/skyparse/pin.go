package skyparse

import (
	"fmt"
	"strings"
)

// Pin sets the origin ref of one core.workflow in a copy.bara.sky source.
//
// It edits text in place and keeps everything else byte-for-byte. The origin may be written
// inline (origin = git.origin(...)) or as a top-level name bound to a call, and ref may be a
// string literal or a top-level name bound to a string; each is updated where it is defined.
// When the origin has no ref, one is added. It returns the new source and the previous ref.
func Pin(src, workflow, ref string) (string, string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", "", fmt.Errorf("ref is empty")
	}
	if strings.ContainsAny(ref, "\"'\\\n") {
		return "", "", fmt.Errorf("ref contains a quote, backslash, or newline")
	}
	m := maskComments(src)
	wf, _, err := findWorkflow(m, workflow)
	if err != nil {
		return "", "", err
	}
	origin, ok := wf.kw("origin")
	if !ok {
		return "", "", fmt.Errorf("workflow %q has no origin", workflow)
	}
	open, close := origin.start, origin.end
	val := strings.TrimSpace(m[open:close])
	if isName(val) {
		a, ok := topAssign(m, val)
		if !ok {
			return "", "", fmt.Errorf("origin %s is not assigned at top level in this file", val)
		}
		open, close = a.start, a.end
		val = strings.TrimSpace(m[open:close])
	}
	lp := strings.IndexByte(m[open:close], '(')
	if lp < 0 || !strings.HasSuffix(val, ")") {
		return "", "", fmt.Errorf("origin of workflow %q is not a call", workflow)
	}
	lp += open
	rp := matchParen(m, lp)
	if rp < 0 {
		return "", "", fmt.Errorf("unbalanced parentheses in origin")
	}
	fn := strings.TrimSpace(m[open:lp])
	if !strings.HasSuffix(fn, "origin") {
		return "", "", fmt.Errorf("origin of workflow %q is %s, not an origin call", workflow, fn)
	}
	args := splitArgs(m, lp+1, rp)
	lit := quoteLiteral(ref)
	if r, ok := args.kw("ref"); ok {
		rv := strings.TrimSpace(m[r.start:r.end])
		s, e := trimSpan(m, r.start, r.end)
		if isName(rv) {
			a, ok := topAssign(m, rv)
			if !ok {
				return "", "", fmt.Errorf("ref %s is not assigned at top level in this file", rv)
			}
			s, e = trimSpan(m, a.start, a.end)
			rv = m[s:e]
		}
		old, ok := asString(rv)
		if !ok {
			return "", "", fmt.Errorf("ref of workflow %q is %s, not a string; edit it by hand", workflow, rv)
		}
		return src[:s] + lit + src[e:], old, nil
	}
	return insertKwarg(src, m, lp, rp, args, "ref = "+lit), "", nil
}

type span struct {
	key        string
	start, end int
}

type argList []span

func (a argList) kw(name string) (span, bool) {
	for _, s := range a {
		if s.key == name {
			return s, true
		}
	}
	return span{}, false
}

func maskComments(src string) string {
	b := []byte(src)
	quote := byte(0)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(b) {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '#' {
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
		}
	}
	return string(b)
}

// splitArgs splits m[start:end] at top-level commas; each span covers the value only.
func splitArgs(m string, start, end int) argList {
	var out argList
	depth := 0
	quote := byte(0)
	from := start
	emit := func(to int) {
		part := m[from:to]
		if strings.TrimSpace(part) == "" {
			return
		}
		key, vs := "", from
		if k, _, ok := splitKV(part); ok && isName(k) {
			key = k
			vs = from + strings.IndexByte(part, '=') + 1
		}
		out = append(out, span{key: key, start: vs, end: to})
	}
	for i := start; i < end; i++ {
		c := m[i]
		if quote != 0 {
			if c == '\\' && i+1 < end {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				emit(i)
				from = i + 1
			}
		}
	}
	emit(end)
	return out
}

func findWorkflow(m, workflow string) (argList, []string, error) {
	var names []string
	for i := 0; ; {
		j := strings.Index(m[i:], "core.workflow")
		if j < 0 {
			break
		}
		j += i
		i = j + len("core.workflow")
		if j > 0 && (isIdent(m[j-1]) || m[j-1] == '.') {
			continue
		}
		k := i
		for k < len(m) && (m[k] == ' ' || m[k] == '\t' || m[k] == '\n' || m[k] == '\r') {
			k++
		}
		if k >= len(m) || m[k] != '(' {
			continue
		}
		end := matchParen(m, k)
		if end < 0 {
			return nil, names, fmt.Errorf("unbalanced parentheses in core.workflow")
		}
		args := splitArgs(m, k+1, end)
		if n, ok := args.kw("name"); ok {
			name, _ := asString(m[n.start:n.end])
			names = append(names, name)
			if name == workflow {
				return args, names, nil
			}
		}
		i = end
	}
	if len(names) == 0 {
		return nil, names, fmt.Errorf("no core.workflow found")
	}
	return nil, names, fmt.Errorf("workflow %q not found; workflows: %s", workflow, strings.Join(names, ", "))
}

// topAssign finds name = <value> at depth 0 and returns the value span.
func topAssign(m, name string) (span, bool) {
	depth := 0
	quote := byte(0)
	for i := 0; i < len(m); i++ {
		c := m[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(m) {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
			continue
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			depth--
			continue
		}
		if depth != 0 || !isIdentStart(c) || (i > 0 && (isIdent(m[i-1]) || m[i-1] == '.')) {
			continue
		}
		j := i + 1
		for j < len(m) && isIdent(m[j]) {
			j++
		}
		k := j
		for k < len(m) && (m[k] == ' ' || m[k] == '\t') {
			k++
		}
		if m[i:j] != name || k >= len(m) || m[k] != '=' || (k+1 < len(m) && m[k+1] == '=') {
			i = j - 1
			continue
		}
		vs := k + 1
		ve := valueEnd(m, vs)
		return span{key: name, start: vs, end: ve}, true
	}
	return span{}, false
}

// valueEnd returns the end of an expression starting at i: the first newline at depth 0.
func valueEnd(m string, i int) int {
	depth := 0
	quote := byte(0)
	for ; i < len(m); i++ {
		c := m[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(m) {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\n':
			if depth <= 0 {
				return i
			}
		}
	}
	return len(m)
}

func trimSpan(m string, s, e int) (int, int) {
	for s < e && strings.ContainsRune(" \t\r\n", rune(m[s])) {
		s++
	}
	for e > s && strings.ContainsRune(" \t\r\n", rune(m[e-1])) {
		e--
	}
	return s, e
}

func insertKwarg(src, m string, lp, rp int, args argList, kv string) string {
	if len(args) == 0 {
		return src[:lp+1] + kv + src[rp:]
	}
	last := args[len(args)-1]
	_, le := trimSpan(m, last.start, last.end)
	body := m[lp+1 : rp]
	if !strings.Contains(body, "\n") {
		return src[:le] + ", " + kv + src[le:]
	}
	ls, _ := trimSpan(m, last.start, last.end)
	lineStart := strings.LastIndexByte(m[:ls], '\n') + 1
	indent := ""
	for p := lineStart; p < len(m) && (m[p] == ' ' || m[p] == '\t'); p++ {
		indent += string(m[p])
	}
	after := le
	for after < rp && (m[after] == ' ' || m[after] == '\t') {
		after++
	}
	if after < rp && m[after] == ',' {
		return src[:after+1] + "\n" + indent + kv + "," + src[after+1:]
	}
	return src[:le] + ",\n" + indent + kv + "," + src[le:]
}

func isName(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdent(s[i]) {
			return false
		}
	}
	return true
}
