// Package skyparse performs a partial static reading of copy.bara.sky files.
// It does not execute Starlark.
package skyparse

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/fsio"
	"github.com/evolvectl/evolvectl/internal/redact"
)

// FileExplanation is the static view of one config.
type FileExplanation struct {
	Workflows  []domain.WorkflowExplanation
	Unresolved []string
	Loads      []string
}

// Parse extracts workflow structure from source.
func Parse(path, src string) FileExplanation {
	var out FileExplanation
	out.Loads = findLoads(src)
	bodies := extractCalls(src, "core.workflow")
	if len(bodies) == 0 {
		out.Unresolved = append(out.Unresolved, "no core.workflow(...) call found")
		return out
	}
	for _, body := range bodies {
		wf := domain.WorkflowExplanation{
			ConfigPath: path,
			ConfigHash: fsio.HashBytes([]byte(src)),
			Confidence: "partial",
			Loads:      out.Loads,
			Notes:      []string{"static reading only; expressions are not executed"},
		}
		kvs := splitTop(body)
		for _, kv := range kvs {
			key, val, ok := splitKV(kv)
			if !ok {
				out.Unresolved = append(out.Unresolved, "unparsed workflow entry")
				wf.Unresolved = append(wf.Unresolved, truncate(kv))
				continue
			}
			switch key {
			case "name":
				if s, ok := asString(val); ok {
					wf.Name = s
				} else {
					wf.Unresolved = append(wf.Unresolved, "name is not a literal")
				}
			case "mode":
				wf.Mode = strings.TrimSpace(val)
			case "origin":
				wf.Origin = redact.Text(compact(val))
			case "destination":
				wf.Destination = redact.Text(compact(val))
			case "origin_files":
				inc, exc, unknown := parseGlob(val)
				wf.OriginFiles = inc
				wf.OriginExclude = exc
				wf.Unresolved = append(wf.Unresolved, unknown...)
			case "destination_files":
				inc, exc, unknown := parseGlob(val)
				wf.DestFiles = inc
				wf.DestExclude = exc
				wf.Unresolved = append(wf.Unresolved, unknown...)
			case "transformations":
				wf.Transforms = parseTransforms(val)
			case "authoring":
				wf.Notes = append(wf.Notes, "authoring: "+redact.Text(compact(val)))
			default:
				wf.Unresolved = append(wf.Unresolved, key+" is not interpreted")
			}
		}
		if wf.Name == "" {
			wf.Name = "(unnamed)"
			wf.Unresolved = append(wf.Unresolved, "workflow name missing")
		}
		if len(wf.Unresolved) == 0 && len(wf.Transforms) > 0 {
			wf.Confidence = "resolved"
		}
		out.Workflows = append(out.Workflows, wf)
	}
	return out
}

// Trace predicts inclusion for one slash-separated path when globs are literal.
type Trace struct {
	Included        bool
	Stage           string
	OutputPath      string
	ContentModified bool
	Confidence      string
	Notes           []string
}

// TraceFile explains one file against a workflow. Dynamic globs stay unknown.
func TraceFile(wf domain.WorkflowExplanation, file string) Trace {
	file = strings.TrimPrefix(filepathToSlash(file), "./")
	tr := Trace{Confidence: wf.Confidence, OutputPath: file}
	if len(wf.Unresolved) > 0 {
		tr.Confidence = "partial"
		tr.Notes = append(tr.Notes, "workflow has unresolved expressions; trace is incomplete")
	}
	excluded := false
	for _, p := range wf.OriginExclude {
		if matchGlob(p, file) {
			excluded = true
		}
	}
	included := len(wf.OriginFiles) == 0
	for _, p := range wf.OriginFiles {
		if matchGlob(p, file) {
			included = true
		}
	}
	if excluded {
		tr.Included = false
		tr.Stage = "origin_files exclude"
		tr.OutputPath = ""
		tr.Notes = append(tr.Notes, "excluded before transformations")
		return tr
	}
	if !included {
		tr.Included = false
		tr.Stage = "origin_files"
		tr.OutputPath = ""
		tr.Notes = append(tr.Notes, "not selected by origin_files")
		return tr
	}
	tr.Included = true
	tr.Stage = "origin_files"
	path := file
	for _, step := range wf.Transforms {
		switch step.Name {
		case "core.move":
			if len(step.Args) >= 2 && step.Known {
				from, to := step.Args[0], step.Args[1]
				if path == from {
					path = to
				} else if strings.HasPrefix(path, from+"/") {
					path = to + strings.TrimPrefix(path, from)
				}
				tr.Stage = "core.move"
			} else {
				tr.Confidence = "partial"
				tr.Notes = append(tr.Notes, "core.move arguments were not both literals")
			}
		case "core.replace":
			applies := true
			if len(step.Args) > 0 {
				applies = false
				for _, a := range step.Args {
					if matchGlob(a, path) || matchGlob(a, file) {
						applies = true
					}
				}
			}
			if applies {
				tr.ContentModified = true
				tr.Stage = "core.replace"
			}
		default:
			if !step.Known {
				tr.Confidence = "unknown"
				tr.Notes = append(tr.Notes, step.Name+" was not evaluated")
			}
		}
	}
	tr.OutputPath = path
	return tr
}

func findLoads(src string) []string {
	var out []string
	for _, body := range extractCalls(stripComments(src), "load") {
		if s, ok := asString(strings.TrimSpace(body)); ok {
			out = append(out, s)
			continue
		}
		parts := splitTop(body)
		if len(parts) > 0 {
			if s, ok := asString(strings.TrimSpace(parts[0])); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func parseTransforms(val string) []domain.TransformStep {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		val = val[1 : len(val)-1]
	}
	parts := splitTop(val)
	var steps []domain.TransformStep
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		name := p
		args := ""
		if i := strings.Index(p, "("); i >= 0 {
			name = strings.TrimSpace(p[:i])
			args = p[i:]
		}
		step := domain.TransformStep{Order: i + 1, Name: name, Known: knownTransform(name), Summary: compact(p)}
		step.Args = literalArgs(args)
		steps = append(steps, step)
	}
	return steps
}

func knownTransform(name string) bool {
	switch name {
	case "core.move", "core.replace", "core.transform":
		return true
	default:
		return false
	}
}

func literalArgs(call string) []string {
	call = strings.TrimSpace(call)
	if !strings.HasPrefix(call, "(") {
		return nil
	}
	var out []string
	for _, p := range splitTop(call[1 : len(call)-1]) {
		p = strings.TrimSpace(p)
		if s, ok := asString(p); ok {
			out = append(out, s)
			continue
		}
		if strings.Contains(p, "glob(") {
			inc, _, _ := parseGlob(p)
			out = append(out, inc...)
		}
	}
	return out
}

func parseGlob(val string) (inc, exc, unknown []string) {
	val = strings.TrimSpace(val)
	if i := strings.Index(val, "glob("); i >= 0 {
		open := i + len("glob")
		end := matchParen(val, open)
		if end < 0 {
			return nil, nil, []string{"unclosed glob"}
		}
		body := val[open+1 : end]
		parts := splitTop(body)
		for idx, p := range parts {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "exclude") {
				_, list, _ := splitKV(p)
				exc = append(exc, stringList(list)...)
				continue
			}
			if idx == 0 {
				inc = append(inc, stringList(p)...)
				continue
			}
			if _, ok := asString(p); ok {
				inc = append(inc, mustString(p))
				continue
			}
			unknown = append(unknown, "non-literal glob entry")
		}
		return inc, exc, unknown
	}
	if s, ok := asString(val); ok {
		return []string{s}, nil, nil
	}
	return nil, nil, []string{"glob expression is not literal: " + truncate(val)}
}

func stringList(v string) []string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		var out []string
		for _, p := range splitTop(v[1 : len(v)-1]) {
			if s, ok := asString(strings.TrimSpace(p)); ok {
				out = append(out, s)
			}
		}
		return out
	}
	if s, ok := asString(v); ok {
		return []string{s}
	}
	return nil
}

func mustString(p string) string {
	s, _ := asString(p)
	return s
}

func asString(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return "", false
	}
	if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1], true
	}
	return "", false
}

func splitKV(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	depth := 0
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
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
		if c == '(' || c == '[' || c == '{' {
			depth++
			continue
		}
		if c == ')' || c == ']' || c == '}' {
			depth--
			continue
		}
		if c == '=' && depth == 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
		}
	}
	return "", "", false
}

func splitTop(s string) []string {
	var out []string
	s = strings.TrimSpace(s)
	depth := 0
	quote := byte(0)
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
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
		if c == '(' || c == '[' || c == '{' {
			depth++
			continue
		}
		if c == ')' || c == ']' || c == '}' {
			depth--
			continue
		}
		if c == ',' && depth == 0 {
			part := strings.TrimSpace(s[start:i])
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	if tail := strings.TrimSpace(s[start:]); tail != "" {
		out = append(out, tail)
	}
	return out
}

func extractCalls(src, name string) []string {
	var out []string
	for i := 0; i < len(src); {
		j := strings.Index(src[i:], name)
		if j < 0 {
			break
		}
		j += i
		if j > 0 && isIdent(src[j-1]) {
			i = j + len(name)
			continue
		}
		k := j + len(name)
		for k < len(src) && (src[k] == ' ' || src[k] == '\n' || src[k] == '\t') {
			k++
		}
		if k >= len(src) || src[k] != '(' {
			i = j + len(name)
			continue
		}
		end := matchParen(src, k)
		if end < 0 {
			out = append(out, src[k:])
			break
		}
		out = append(out, src[k+1:end])
		i = end + 1
	}
	return out
}

func matchParen(s string, open int) int {
	depth := 0
	quote := byte(0)
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
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
		if c == '(' {
			depth++
		}
		if c == ')' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func stripComments(src string) string {
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			b.WriteByte(c)
			continue
		}
		if c == '#' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				b.WriteByte('\n')
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isIdent(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func compact(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 180 {
		return s[:180] + "..."
	}
	return s
}

func truncate(s string) string {
	s = compact(s)
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

var globMeta = regexp.MustCompile(`[\*\?]`)

func matchGlob(pattern, file string) bool {
	pattern = filepathToSlash(pattern)
	file = filepathToSlash(file)
	if !globMeta.MatchString(pattern) {
		return pattern == file
	}
	re := "^" + globToRegex(pattern) + "$"
	ok, err := regexp.MatchString(re, file)
	if err != nil {
		return false
	}
	return ok
}

func globToRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if strings.HasPrefix(pattern[i:], "**") {
			b.WriteString(".*")
			i++
			continue
		}
		switch pattern[i] {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		default:
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}

// FormatText renders one workflow for the terminal.
func FormatText(wf domain.WorkflowExplanation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workflow %s\n", wf.Name)
	fmt.Fprintf(&b, "Config   %s\n", wf.ConfigPath)
	fmt.Fprintf(&b, "Confidence %s\n", wf.Confidence)
	if wf.Origin != "" {
		fmt.Fprintf(&b, "Origin   %s\n", wf.Origin)
	}
	if wf.Destination != "" {
		fmt.Fprintf(&b, "Destination %s\n", wf.Destination)
	}
	if len(wf.OriginFiles) > 0 {
		fmt.Fprintf(&b, "Origin files %s\n", strings.Join(wf.OriginFiles, ", "))
	}
	if len(wf.OriginExclude) > 0 {
		fmt.Fprintf(&b, "Origin exclude %s\n", strings.Join(wf.OriginExclude, ", "))
	}
	for _, t := range wf.Transforms {
		fmt.Fprintf(&b, "Transform %d %s\n", t.Order, t.Summary)
	}
	for _, u := range wf.Unresolved {
		fmt.Fprintf(&b, "Unresolved %s\n", u)
	}
	return b.String()
}
