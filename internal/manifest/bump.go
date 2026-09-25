// Package manifest edits dependency declarations in place.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/fsio"
	"github.com/sureshpsc/evolvectl/internal/idgen"
	"github.com/sureshpsc/evolvectl/internal/patch"
)

// Result is one manifest edit.
type Result struct {
	Change  domain.Change
	Updated bool
}

// Bump sets the declared version of ecosystem/name in the inventory manifests.
func Bump(root string, inv domain.Inventory, ecosystem, name, version string) ([]domain.Change, error) {
	var changes []domain.Change
	seen := map[string]bool{}
	for _, dep := range inv.Dependencies {
		if dep.Ecosystem != ecosystem || dep.Name != name || !dep.Direct {
			continue
		}
		if seen[dep.Manifest] {
			continue
		}
		seen[dep.Manifest] = true
		abs := filepath.Join(root, filepath.FromSlash(dep.Manifest))
		before, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		after, err := rewrite(dep.Manifest, before, ecosystem, name, version)
		if err != nil {
			return nil, err
		}
		if string(after) == string(before) {
			continue
		}
		if err := fsio.WriteAtomic(abs, after, 0o644); err != nil {
			return nil, err
		}
		rel := fsio.RelSlash(root, abs)
		changes = append(changes, domain.Change{
			ID:         idgen.New("chg_"),
			File:       rel,
			Kind:       "manifest",
			RecipeID:   "engine:manifest-bump",
			Confidence: domain.ConfidenceHigh,
			Diff:       patch.Unified(rel, string(before), string(after)),
			BeforeHash: fsio.HashBytes(before),
			AfterHash:  fsio.HashBytes(after),
			Reason:     fmt.Sprintf("set %s to %s", name, version),
		})
	}
	return changes, nil
}

func rewrite(rel string, before []byte, ecosystem, name, version string) ([]byte, error) {
	switch ecosystem {
	case "go":
		return rewriteGoMod(rel, before, name, version)
	case "python":
		return rewritePython(before, name, version)
	case "node":
		return rewritePackageJSON(before, name, version)
	case "maven":
		return rewritePom(before, name, version)
	default:
		return nil, fmt.Errorf("no manifest writer for ecosystem %s", ecosystem)
	}
}

func rewriteGoMod(rel string, before []byte, modulePath, version string) ([]byte, error) {
	if !strings.HasPrefix(version, "v") && strings.Count(version, ".") >= 1 {
		version = "v" + strings.TrimPrefix(version, "v")
	}
	f, err := modfile.Parse(rel, before, nil)
	if err != nil {
		return nil, err
	}
	found := false
	for _, req := range f.Require {
		if req.Mod.Path == modulePath && !req.Indirect {
			found = true
		}
	}
	if !found {
		return before, nil
	}
	if err := f.DropRequire(modulePath); err != nil {
		return nil, err
	}
	if err := f.AddRequire(modulePath, version); err != nil {
		return nil, err
	}
	f.Cleanup()
	return f.Format()
}

func rewritePython(before []byte, name, version string) ([]byte, error) {
	lines := strings.Split(string(before), "\n")
	changed := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.Contains(trim, `"`+name+"==") || strings.HasPrefix(trim, name+"==") || strings.HasPrefix(trim, name+" =") || strings.HasPrefix(trim, `"`+name+`"`) {
			lines[i] = replaceVersionToken(line, name, version)
			if lines[i] != line {
				changed = true
			}
		}
	}
	if !changed {
		return before, nil
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func replaceVersionToken(line, name, version string) string {
	if i := strings.Index(line, name+"=="); i >= 0 {
		rest := line[i+len(name)+2:]
		end := 0
		for end < len(rest) && rest[end] != '"' && rest[end] != '\'' && rest[end] != ' ' && rest[end] != ',' {
			end++
		}
		return line[:i+len(name)+2] + version + rest[end:]
	}
	if i := strings.Index(line, `"`+name+`"`); i >= 0 && strings.Contains(line, "==") {
		return replaceVersionToken(line, name, version)
	}
	if strings.Contains(line, name) && strings.Contains(line, "=") {
		parts := strings.SplitN(line, "=", 2)
		if strings.TrimSpace(parts[0]) == name {
			quote := `"`
			body := strings.TrimSpace(parts[1])
			if strings.HasPrefix(body, "'") {
				quote = "'"
			}
			return parts[0] + "= " + quote + version + quote
		}
	}
	return line
}

func rewritePackageJSON(before []byte, name, version string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(before, &doc); err != nil {
		return nil, err
	}
	updated := false
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies"} {
		raw, ok := doc[key].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := raw[name]; ok {
			raw[name] = version
			updated = true
		}
	}
	if !updated {
		return before, nil
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func rewritePom(before []byte, name, version string) ([]byte, error) {
	text := string(before)
	group, artifact := name, name
	if i := strings.LastIndex(name, ":"); i >= 0 {
		group = name[:i]
		artifact = name[i+1:]
	}
	chunks := strings.Split(text, "<dependency>")
	if len(chunks) < 2 {
		return before, nil
	}
	var b strings.Builder
	b.WriteString(chunks[0])
	changed := false
	for _, c := range chunks[1:] {
		block := "<dependency>" + c
		end := strings.Index(c, "</dependency>")
		segment := c
		tail := ""
		if end >= 0 {
			segment = c[:end]
			tail = c[end:]
		}
		if strings.Contains(segment, "<artifactId>"+artifact+"</artifactId>") && (group == artifact || strings.Contains(segment, "<groupId>"+group+"</groupId>")) {
			segment = replaceXMLVersion(segment, version)
			changed = true
		}
		b.WriteString("<dependency>")
		b.WriteString(segment)
		b.WriteString(tail)
		_ = block
	}
	if !changed {
		return before, nil
	}
	return []byte(b.String()), nil
}

func replaceXMLVersion(segment, version string) string {
	open := "<version>"
	close := "</version>"
	i := strings.Index(segment, open)
	j := strings.Index(segment, close)
	if i < 0 || j < 0 || j < i {
		return segment
	}
	return segment[:i+len(open)] + version + segment[j:]
}

// DeclaredVersion reports the version now on disk for a manifest path.
func ContainsVersion(root, manifest, version string) bool {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifest)))
	if err != nil {
		return false
	}
	s := string(b)
	if strings.Contains(s, version) {
		return true
	}
	if !strings.HasPrefix(version, "v") && strings.Contains(s, "v"+version) {
		return true
	}
	return false
}
