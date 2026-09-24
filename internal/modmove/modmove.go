// Package modmove moves Go code from one module path to another, as in a /vN major-version
// upgrade or a module rename. Import paths are rewritten through the AST. Package
// identifiers in the file are left alone: a /vN suffix keeps the package name, and a rename
// that changes the package name shows up as a compile error instead of a silent edit.
package modmove

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

var majorSuffix = regexp.MustCompile(`^v[0-9]+$`)

// InModule reports whether importPath is a package of module. A path whose next element is
// a major-version suffix such as /v2 belongs to a different module.
func InModule(importPath, module string) bool {
	if importPath == module {
		return true
	}
	if !strings.HasPrefix(importPath, module+"/") {
		return false
	}
	rest := strings.TrimPrefix(importPath, module+"/")
	first := rest
	if i := strings.Index(rest, "/"); i >= 0 {
		first = rest[:i]
	}
	return !majorSuffix.MatchString(first)
}

// Rel returns the package path relative to its module: "" for the root package.
func Rel(importPath, module string) string {
	if importPath == module {
		return ""
	}
	return strings.TrimPrefix(importPath, module+"/")
}

// Join is the inverse of Rel.
func Join(module, rel string) string {
	if rel == "" {
		return module
	}
	return module + "/" + rel
}

// RewriteImports moves every import of oldModule, including subpackages, to newModule.
// It returns the formatted file and the number of import specs changed.
func RewriteImports(filename string, src []byte, oldModule, newModule string) ([]byte, int, error) {
	if oldModule == "" || newModule == "" || oldModule == newModule {
		return src, 0, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, 0, err
	}
	n := 0
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if InModule(p, newModule) && newModule != oldModule && strings.HasPrefix(newModule, oldModule+"/") {
			continue
		}
		if !InModule(p, oldModule) {
			continue
		}
		imp.Path.Value = strconv.Quote(Join(newModule, Rel(p, oldModule)))
		n++
	}
	if n == 0 {
		return src, 0, nil
	}
	ast.SortImports(fset, file)
	var buf strings.Builder
	if err := format.Node(&buf, fset, file); err != nil {
		return nil, 0, err
	}
	return []byte(buf.String()), n, nil
}

// MoveRequire drops the direct requirement on oldModule and requires newModule at version.
// A replace directive for oldModule is reported so the caller can surface it for review.
func MoveRequire(rel string, body []byte, oldModule, newModule, version string) ([]byte, bool, []string, error) {
	f, err := modfile.Parse(rel, body, nil)
	if err != nil {
		return nil, false, nil, err
	}
	var notes []string
	hadOld := false
	for _, r := range f.Require {
		if r.Mod.Path == oldModule {
			hadOld = true
		}
	}
	hasNew := false
	for _, r := range f.Require {
		if r.Mod.Path == newModule && r.Mod.Version == version {
			hasNew = true
		}
	}
	if !hadOld && hasNew {
		return body, false, nil, nil
	}
	if hadOld {
		if err := f.DropRequire(oldModule); err != nil {
			return nil, false, nil, err
		}
	}
	if err := f.AddRequire(newModule, version); err != nil {
		return nil, false, nil, err
	}
	for _, r := range f.Replace {
		if r.Old.Path == oldModule {
			notes = append(notes, fmt.Sprintf("%s: replace %s is still present after the move to %s", rel, oldModule, newModule))
		}
	}
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return nil, false, nil, err
	}
	return out, string(out) != string(body), notes, nil
}

// SetReplace points module at a local directory. dir is slash-separated and relative to the go.mod.
func SetReplace(rel string, body []byte, module, dir string) ([]byte, bool, error) {
	f, err := modfile.Parse(rel, body, nil)
	if err != nil {
		return nil, false, err
	}
	if !strings.HasPrefix(dir, ".") {
		dir = "./" + dir
	}
	for _, r := range f.Replace {
		if r.Old.Path == module && r.New.Path == dir && r.New.Version == "" {
			return body, false, nil
		}
	}
	if err := f.AddReplace(module, "", dir, ""); err != nil {
		return nil, false, err
	}
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return nil, false, err
	}
	return out, string(out) != string(body), nil
}

// GuessName is the package name an unnamed import most likely binds: the last path element
// that is not a major-version suffix, without a gopkg.in ".vN", a "go-"/"go." prefix, or a
// "-go"/".go" suffix. The real name is the package clause, which this does not read.
func GuessName(p string) string {
	parts := strings.Split(p, "/")
	name := parts[len(parts)-1]
	if len(parts) > 1 && majorSuffix.MatchString(name) {
		name = parts[len(parts)-2]
	}
	if i := strings.Index(name, ".v"); i > 0 && digits(name[i+2:]) {
		name = name[:i]
	}
	name = strings.TrimPrefix(name, "go-")
	name = strings.TrimPrefix(name, "go.")
	name = strings.TrimSuffix(name, "-go")
	name = strings.TrimSuffix(name, ".go")
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, ".", "")
	return name
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// SuggestMajorPath returns module/vN when version is a major version of 2 or more and the
// module path has no major suffix yet. It returns "" otherwise.
func SuggestMajorPath(module, version string) string {
	v := strings.TrimPrefix(version, "v")
	v = strings.TrimSuffix(v, "+incompatible")
	major := v
	if i := strings.Index(v, "."); i >= 0 {
		major = v[:i]
	}
	n, err := strconv.Atoi(major)
	if err != nil || n < 2 {
		return ""
	}
	if strings.HasPrefix(module, "gopkg.in/") {
		return ""
	}
	parts := strings.Split(module, "/")
	if majorSuffix.MatchString(parts[len(parts)-1]) {
		return ""
	}
	return fmt.Sprintf("%s/v%d", module, n)
}
