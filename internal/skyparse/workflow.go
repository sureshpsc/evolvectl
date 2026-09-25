package skyparse

import (
	"fmt"
	"strings"
)

// Spec is the literal part of one core.workflow: enough to reproduce which files Copybara
// would copy, where they land, and which literal text replacements it applies.
type Spec struct {
	Name          string   `json:"name"`
	OriginURL     string   `json:"origin_url"`
	OriginRef     string   `json:"origin_ref"`
	OriginFiles   []string `json:"origin_files"`
	OriginExclude []string `json:"origin_exclude,omitempty"`
	DestFiles     []string `json:"destination_files"`
	DestExclude   []string `json:"destination_exclude,omitempty"`
	Destination   string   `json:"destination"`
	Steps         []Step   `json:"steps"`
	// Ignored transformations change only the commit message, not files.
	Ignored []string `json:"ignored,omitempty"`
	// Unsupported entries cannot be reproduced without running Copybara.
	Unsupported []string `json:"unsupported,omitempty"`
}

// Step is one file transformation, in workflow order.
type Step struct {
	Kind         string   `json:"kind"`
	Before       string   `json:"before"`
	After        string   `json:"after"`
	Paths        []string `json:"paths,omitempty"`
	PathsExclude []string `json:"paths_exclude,omitempty"`
	Overwrite    bool     `json:"overwrite,omitempty"`
}

// Workflow reads the named workflow. Values bound by top-level name = "literal" assignments
// are resolved; anything computed is listed in Unsupported rather than guessed.
func Workflow(src, name string) (Spec, error) {
	src = substituteAssignments(src)
	m := maskComments(src)
	args, _, err := findWorkflow(m, name)
	if err != nil {
		return Spec{}, err
	}
	sp := Spec{Name: name, OriginFiles: []string{"**"}, DestFiles: []string{"**"}}
	for _, a := range args {
		val := strings.TrimSpace(m[a.start:a.end])
		switch a.key {
		case "origin":
			sp.origin(m, val)
		case "origin_files":
			inc, exc, unknown := parseGlob(val)
			sp.OriginFiles, sp.OriginExclude = inc, exc
			sp.Unsupported = append(sp.Unsupported, prefix("origin_files: ", unknown)...)
		case "destination_files":
			inc, exc, unknown := parseGlob(val)
			sp.DestFiles, sp.DestExclude = inc, exc
			sp.Unsupported = append(sp.Unsupported, prefix("destination_files: ", unknown)...)
		case "destination":
			sp.Destination = callName(resolveName(m, val))
		case "transformations":
			sp.transforms(m, val)
		}
	}
	if sp.OriginURL == "" {
		sp.Unsupported = append(sp.Unsupported, "origin url is not a literal")
	}
	return sp, nil
}

func resolveName(m, val string) string {
	if isName(val) {
		if a, ok := topAssign(m, val); ok {
			return strings.TrimSpace(m[a.start:a.end])
		}
	}
	return val
}

func callName(v string) string {
	if i := strings.IndexByte(v, '('); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	return strings.TrimSpace(v)
}

func callArgs(v string) ([]string, bool) {
	v = strings.TrimSpace(v)
	i := strings.IndexByte(v, '(')
	if i < 0 || !strings.HasSuffix(v, ")") {
		return nil, false
	}
	return splitTop(v[i+1 : len(v)-1]), true
}

func (sp *Spec) origin(m, val string) {
	val = resolveName(m, val)
	fn := callName(val)
	if !strings.HasSuffix(fn, "origin") || fn == "folder.origin" {
		sp.Unsupported = append(sp.Unsupported, "origin "+fn+" is not a git origin")
		return
	}
	args, ok := callArgs(val)
	if !ok {
		sp.Unsupported = append(sp.Unsupported, "origin is not a call")
		return
	}
	for i, a := range args {
		k, v, kw := splitKV(a)
		if !kw {
			k, v = map[int]string{0: "url", 1: "ref"}[i], a
		}
		s, lit := asString(v)
		switch k {
		case "url":
			if lit {
				sp.OriginURL = s
			}
		case "ref":
			if lit {
				sp.OriginRef = s
			} else {
				sp.Unsupported = append(sp.Unsupported, "origin ref is not a literal")
			}
		}
	}
}

func (sp *Spec) transforms(m, val string) {
	val = strings.TrimSpace(resolveName(m, val))
	if !strings.HasPrefix(val, "[") || !strings.HasSuffix(val, "]") {
		sp.Unsupported = append(sp.Unsupported, "transformations is not a literal list")
		return
	}
	for _, item := range splitTop(val[1 : len(val)-1]) {
		item = strings.TrimSpace(item)
		fn := callName(item)
		switch {
		case fn == "core.move" || fn == "core.replace":
			st, why := parseStep(fn, item)
			if why != "" {
				sp.Unsupported = append(sp.Unsupported, why)
				continue
			}
			sp.Steps = append(sp.Steps, st)
		case strings.HasPrefix(fn, "metadata."):
			sp.Ignored = append(sp.Ignored, fn)
		default:
			sp.Unsupported = append(sp.Unsupported, fn+" is not reproduced; run Copybara to see its effect")
		}
	}
}

func parseStep(fn, item string) (Step, string) {
	st := Step{Kind: strings.TrimPrefix(fn, "core.")}
	args, ok := callArgs(item)
	if !ok {
		return st, fn + " is not a call"
	}
	pos := 0
	for _, a := range args {
		k, v, kw := splitKV(a)
		if !kw {
			k, v = map[int]string{0: "before", 1: "after"}[pos], a
			pos++
		}
		switch k {
		case "before", "after":
			s, lit := asString(v)
			if !lit {
				return st, fn + " " + k + " is not a literal"
			}
			if k == "before" {
				st.Before = s
			} else {
				st.After = s
			}
		case "paths":
			inc, exc, unknown := parseGlob(v)
			if len(unknown) > 0 {
				return st, fn + " paths is not a literal glob"
			}
			st.Paths, st.PathsExclude = inc, exc
		case "overwrite":
			st.Overwrite = strings.TrimSpace(v) == "True"
		case "regex_groups", "multiline", "repeated_groups", "first_only", "ignore":
			if st.Kind == "replace" && strings.TrimSpace(v) != "{}" && strings.TrimSpace(v) != "False" && strings.TrimSpace(v) != "[]" {
				return st, fn + " uses " + k + "; only literal replacements are reproduced"
			}
		}
	}
	if st.Kind == "replace" && (strings.Contains(st.Before, "${") || strings.Contains(st.After, "${")) {
		return st, fn + " uses ${...} templates; only literal replacements are reproduced"
	}
	if st.Kind == "replace" && st.Before == "" {
		return st, fn + " has an empty before"
	}
	return st, ""
}

func prefix(p string, list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, p+s)
	}
	return out
}

// Match reports whether a slash path matches one of the include globs and none of the excludes,
// with Copybara's glob rules: * stays within a directory, ** crosses directories, and **/ may
// match nothing.
func Match(include, exclude []string, path string) bool {
	for _, p := range exclude {
		if matchGlob(p, path) {
			return false
		}
	}
	for _, p := range include {
		if matchGlob(p, path) {
			return true
		}
	}
	return false
}

// InitOptions describe a single-library import workflow.
type InitOptions struct {
	Name           string
	URL            string
	Ref            string
	From           string
	To             string
	Exclude        []string
	Replace        [][2]string
	DestinationURL string
	License        bool
}

// Template writes a core.workflow that imports one directory of an upstream repository into
// one directory of this repository. destination_files covers only that directory, so an
// import never deletes files elsewhere in the destination.
func Template(o InitOptions) (string, error) {
	o.From = strings.Trim(filepathToSlash(o.From), "/")
	o.To = strings.Trim(filepathToSlash(o.To), "/")
	switch {
	case o.Name == "" || !isName(strings.ReplaceAll(o.Name, "-", "_")):
		return "", fmt.Errorf("--workflow must be letters, digits, _ or -")
	case o.URL == "" || o.Ref == "":
		return "", fmt.Errorf("--url and --ref are required")
	case o.To == "" || strings.Contains(o.To, ".."):
		return "", fmt.Errorf("--to must be a directory inside the destination, such as third_party/gax-go/v2")
	}
	for _, v := range []string{o.Name, o.URL, o.Ref, o.From, o.To, o.DestinationURL} {
		if strings.ContainsAny(v, "\"\\\n") {
			return "", fmt.Errorf("values cannot contain quotes, backslashes, or newlines: %s", v)
		}
	}
	q := quoteLiteral
	include := []string{"**"}
	if o.From != "" {
		include = []string{o.From + "/**"}
	}
	license := o.License && o.From != ""
	if license {
		include = append(include, "LICENSE")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Import %s at %s into %s.\n", o.URL, o.Ref, o.To)
	fmt.Fprintf(&b, "# Written by evolvectl copybara init. Preview it with:\n#   evolvectl copybara preview --workflow %s\n", o.Name)
	fmt.Fprintf(&b, "core.workflow(\n    name = %s,\n", q(o.Name))
	fmt.Fprintf(&b, "    origin = git.origin(\n        url = %s,\n        ref = %s,\n    ),\n", q(o.URL), q(o.Ref))
	fmt.Fprintf(&b, "    origin_files = glob(%s", list(include))
	if len(o.Exclude) > 0 {
		fmt.Fprintf(&b, ", exclude = %s", list(o.Exclude))
	}
	b.WriteString("),\n")
	if o.DestinationURL != "" {
		fmt.Fprintf(&b, "    destination = git.destination(\n        url = %s,\n        fetch = \"main\",\n        push = \"main\",\n    ),\n", q(o.DestinationURL))
	} else {
		b.WriteString("    destination = folder.destination(),\n")
	}
	fmt.Fprintf(&b, "    # Only this directory is owned by the import; the rest of the destination is left alone.\n")
	fmt.Fprintf(&b, "    destination_files = glob([%s]),\n", q(o.To+"/**"))
	b.WriteString("    authoring = authoring.pass_thru(\"Import Bot <import-bot@example.com>\"),\n")
	b.WriteString("    mode = \"SQUASH\",\n    transformations = [\n")
	if license {
		fmt.Fprintf(&b, "        core.move(%s, %s, overwrite = True),\n", q("LICENSE"), q(o.To+"/LICENSE"))
	}
	fmt.Fprintf(&b, "        core.move(%s, %s", q(o.From), q(o.To))
	if license {
		b.WriteString(", overwrite = True")
	}
	b.WriteString("),\n")
	for _, r := range o.Replace {
		if strings.ContainsAny(r[0]+r[1], "\"\\\n") || strings.Contains(r[0]+r[1], "${") {
			return "", fmt.Errorf("--replace values cannot contain quotes, backslashes, newlines, or ${")
		}
		fmt.Fprintf(&b, "        core.replace(\n            before = %s,\n            after = %s,\n            paths = glob([%s]),\n        ),\n", q(r[0]), q(r[1]), q(o.To+"/**"))
	}
	b.WriteString("    ],\n)\n")
	return b.String(), nil
}

func list(items []string) string {
	q := make([]string, 0, len(items))
	for _, s := range items {
		q = append(q, quoteLiteral(s))
	}
	return "[" + strings.Join(q, ", ") + "]"
}
