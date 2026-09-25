// Package diagnostics parses tool output into a common schema and groups it.
package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/idgen"
	"github.com/sureshpsc/evolvectl/internal/redact"
)

var (
	goLine   = regexp.MustCompile(`(?m)^([A-Za-z0-9_./\\-]+\.go):(\d+)(?::(\d+))?: (.*)$`)
	pyLine   = regexp.MustCompile(`(?m)^([A-Za-z0-9_./\\-]+\.py):(\d+)(?::(\d+))?: (.*)$`)
	generic  = regexp.MustCompile(`(?m)^(?P<file>[^:\n]+):(?P<line>\d+):(?P<column>\d+): (?P<message>.+)$`)
	tempPath = regexp.MustCompile(`(?i)((?:/tmp/|/private/tmp/|[A-Za-z]:\\Temp\\)copybara[^\s:'"]+)`)
)

// ParseToolOutput normalizes stdout/stderr from a tool.
func ParseToolOutput(tool, combined string) []domain.Diagnostic {
	combined = redact.Text(combined)
	var out []domain.Diagnostic
	switch strings.ToLower(tool) {
	case "go", "go test", "go build", "go vet":
		out = append(out, parseGo(combined)...)
	case "pytest", "python", "ruff", "pyright":
		out = append(out, parsePy(tool, combined)...)
	case "copybara":
		out = append(out, parseCopybara(combined)...)
	default:
		out = append(out, parseGeneric(tool, combined)...)
	}
	for i := range out {
		out[i].ID = idgen.New("diag_")
		out[i].Fingerprint = Fingerprint(out[i])
		out[i].Raw = combined
		if len(out[i].Raw) > 4000 {
			out[i].Raw = out[i].Raw[:4000]
		}
	}
	return out
}

func parseGo(text string) []domain.Diagnostic {
	var out []domain.Diagnostic
	for _, m := range goLine.FindAllStringSubmatch(text, -1) {
		d := domain.Diagnostic{
			Source: "tool", Tool: "go", Language: "go",
			File: m[1], Message: strings.TrimSpace(m[4]), Category: "compile",
		}
		d.Line = atoi(m[2])
		d.Column = atoi(m[3])
		if strings.Contains(d.Message, "undefined:") {
			d.Code = "undefined"
			d.Symbol = strings.TrimSpace(strings.TrimPrefix(d.Message, "undefined:"))
		}
		out = append(out, d)
	}
	return out
}

func parsePy(tool, text string) []domain.Diagnostic {
	var out []domain.Diagnostic
	for _, m := range pyLine.FindAllStringSubmatch(text, -1) {
		d := domain.Diagnostic{
			Source: "tool", Tool: tool, Language: "python",
			File: m[1], Message: strings.TrimSpace(m[4]), Category: "lint",
		}
		d.Line = atoi(m[2])
		d.Column = atoi(m[3])
		out = append(out, d)
	}
	return out
}

func parseGeneric(tool, text string) []domain.Diagnostic {
	var out []domain.Diagnostic
	for _, m := range generic.FindAllStringSubmatch(text, -1) {
		d := domain.Diagnostic{
			Source: "tool", Tool: tool, File: m[1], Message: strings.TrimSpace(m[4]), Category: "tool",
		}
		d.Line = atoi(m[2])
		d.Column = atoi(m[3])
		out = append(out, d)
	}
	return out
}

func parseCopybara(text string) []domain.Diagnostic {
	var out []domain.Diagnostic
	if !strings.Contains(strings.ToLower(text), "error") && tempPath.FindString(text) == "" {
		return nil
	}
	d := domain.Diagnostic{
		Source: "tool", Tool: "copybara", Category: "workspace",
		Message: firstLine(text),
	}
	if loc := tempPath.FindString(text); loc != "" {
		d.File = loc
		d.MappedFrom = loc
		d.MapConfidence = "unknown"
	}
	out = append(out, d)
	out = append(out, parseGeneric("copybara", text)...)
	return out
}

// Fingerprint collapses repeated messages without line numbers.
func Fingerprint(d domain.Diagnostic) string {
	msg := strings.Join(strings.Fields(d.Message), " ")
	sum := sha256.Sum256([]byte(d.Tool + "|" + d.Code + "|" + d.Category + "|" + msg + "|" + d.Symbol))
	return hex.EncodeToString(sum[:16])
}

// Group clusters diagnostics by fingerprint.
func Group(list []domain.Diagnostic) []domain.DiagnosticGroup {
	type acc struct {
		g     domain.DiagnosticGroup
		files map[string]bool
	}
	m := map[string]*acc{}
	var order []string
	for _, d := range list {
		a, ok := m[d.Fingerprint]
		if !ok {
			a = &acc{files: map[string]bool{}, g: domain.DiagnosticGroup{
				Fingerprint: d.Fingerprint,
				Category:    d.Category,
				Message:     d.Message,
			}}
			m[d.Fingerprint] = a
			order = append(order, d.Fingerprint)
		}
		a.g.Count++
		a.g.DiagnosticIDs = append(a.g.DiagnosticIDs, d.ID)
		if d.File != "" {
			a.files[d.File] = true
		}
	}
	var out []domain.DiagnosticGroup
	for _, fp := range order {
		a := m[fp]
		for f := range a.files {
			a.g.Files = append(a.g.Files, f)
		}
		sort.Strings(a.g.Files)
		out = append(out, a.g)
	}
	return out
}

// MapTempPaths attaches evidenced mappings when a temp prefix is known.
func MapTempPaths(list []domain.Diagnostic, mappings []domain.PathMapping) []domain.Diagnostic {
	if len(mappings) == 0 {
		return list
	}
	for i := range list {
		for _, mp := range mappings {
			if mp.TempPath == "" || mp.Confidence == "unknown" || mp.OriginalPath == "" {
				continue
			}
			if list[i].File == mp.TempPath || strings.HasPrefix(list[i].File, mp.TempPath) {
				list[i].MappedFrom = list[i].File
				list[i].File = mp.OriginalPath
				list[i].MapConfidence = mp.Confidence
				break
			}
		}
	}
	return list
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 240 {
		s = s[:240]
	}
	return s
}
