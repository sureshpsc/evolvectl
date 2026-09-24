// Package apidiff compares the exported API of two versions of a Go module and finds the
// workspace references that use removed or changed symbols. It reads source with go/parser
// and does not type-check, so method calls on values cannot be attributed to a call site.
package apidiff

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/modmove"
)

// Symbol is one exported declaration. Key is "<rel package>.<Name>" or "<rel package>.<Type>.<Method>".
type Symbol struct {
	Package string
	Name    string
	Kind    string
	Sig     string
}

// Key identifies a symbol independently of the module path.
func (s Symbol) Key() string {
	return s.Package + "." + s.Name
}

const (
	maxChanges = 300
	maxSites   = 500
)

// Exports reads every non-test, non-internal package under dir. dir is the module root.
func Exports(dir string) (map[string]Symbol, error) {
	out := map[string]Symbol{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			base := d.Name()
			if base == "testdata" || base == "vendor" || base == "internal" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, body, parser.SkipObjectResolution)
		if err != nil || f.Name.Name == "main" || strings.HasSuffix(f.Name.Name, "_test") {
			return nil
		}
		pkg := path.Dir(rel)
		if pkg == "." {
			pkg = ""
		}
		for _, s := range declared(fset, f, pkg) {
			if _, dup := out[s.Key()]; !dup {
				out[s.Key()] = s
			}
		}
		return nil
	})
	return out, err
}

func declared(fset *token.FileSet, f *ast.File, pkg string) []Symbol {
	var out []Symbol
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recv := receiverName(d.Recv.List[0].Type)
				if recv == "" || !ast.IsExported(recv) {
					continue
				}
				out = append(out, Symbol{Package: pkg, Name: recv + "." + d.Name.Name, Kind: "method", Sig: funcSig(fset, d.Type)})
				continue
			}
			out = append(out, Symbol{Package: pkg, Name: d.Name.Name, Kind: "func", Sig: funcSig(fset, d.Type)})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					out = append(out, Symbol{Package: pkg, Name: s.Name.Name, Kind: "type", Sig: typeSig(fset, s)})
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, n := range s.Names {
						if !n.IsExported() {
							continue
						}
						sig := ""
						if s.Type != nil {
							sig = expr(fset, s.Type)
						}
						out = append(out, Symbol{Package: pkg, Name: n.Name, Kind: kind, Sig: sig})
					}
				}
			}
		}
	}
	return out
}

func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	}
	return ""
}

// funcSig prints parameter and result types without names, so a renamed parameter is not a change.
func funcSig(fset *token.FileSet, ft *ast.FuncType) string {
	return "func(" + fieldTypes(fset, ft.Params) + ")" + results(fset, ft.Results)
}

func fieldTypes(fset *token.FileSet, fl *ast.FieldList) string {
	if fl == nil {
		return ""
	}
	var parts []string
	for _, f := range fl.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		t := expr(fset, f.Type)
		for i := 0; i < n; i++ {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, ", ")
}

func results(fset *token.FileSet, fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	r := fieldTypes(fset, fl)
	if len(fl.List) == 1 && len(fl.List[0].Names) <= 1 {
		return " " + r
	}
	return " (" + r + ")"
}

// typeSig keeps only the exported surface: exported struct fields and interface methods.
func typeSig(fset *token.FileSet, s *ast.TypeSpec) string {
	prefix := ""
	if s.Assign.IsValid() {
		prefix = "= "
	}
	switch t := s.Type.(type) {
	case *ast.StructType:
		var fields []string
		for _, f := range t.Fields.List {
			ft := expr(fset, f.Type)
			if len(f.Names) == 0 {
				if ast.IsExported(strings.TrimPrefix(receiverName(f.Type), "*")) {
					fields = append(fields, ft)
				}
				continue
			}
			for _, n := range f.Names {
				if n.IsExported() {
					fields = append(fields, n.Name+" "+ft)
				}
			}
		}
		return prefix + "struct{" + strings.Join(fields, "; ") + "}"
	case *ast.InterfaceType:
		var methods []string
		for _, m := range t.Methods.List {
			if len(m.Names) == 0 {
				methods = append(methods, expr(fset, m.Type))
				continue
			}
			for _, n := range m.Names {
				if ft, ok := m.Type.(*ast.FuncType); ok {
					methods = append(methods, n.Name+strings.TrimPrefix(funcSig(fset, ft), "func"))
				}
			}
		}
		return prefix + "interface{" + strings.Join(methods, "; ") + "}"
	case *ast.FuncType:
		return prefix + funcSig(fset, t)
	default:
		return prefix + expr(fset, s.Type)
	}
}

func expr(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// Compare lists removed, changed, and added symbols, sorted by package and name.
func Compare(before, after map[string]Symbol) []domain.APIChange {
	var out []domain.APIChange
	for k, b := range before {
		a, ok := after[k]
		switch {
		case !ok:
			out = append(out, domain.APIChange{Package: b.Package, Symbol: b.Name, Kind: b.Kind, Change: "removed", Before: b.Sig})
		case a.Sig != b.Sig || a.Kind != b.Kind:
			out = append(out, domain.APIChange{Package: b.Package, Symbol: b.Name, Kind: a.Kind, Change: "changed", Before: b.Sig, After: a.Sig})
		}
	}
	for k, a := range after {
		if _, ok := before[k]; !ok {
			out = append(out, domain.APIChange{Package: a.Package, Symbol: a.Name, Kind: a.Kind, Change: "added", After: a.Sig})
		}
	}
	out = append(out, indirect(before, after, out)...)
	rank := map[string]int{"removed": 0, "changed": 1, "indirect": 2, "added": 3}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].Change] != rank[out[j].Change] {
			return rank[out[i].Change] < rank[out[j].Change]
		}
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

// indirect flags unchanged symbols whose signature names a type that was removed or changed
// in the same package. One level only.
func indirect(before, after map[string]Symbol, direct []domain.APIChange) []domain.APIChange {
	types := map[string][]string{}
	for _, c := range direct {
		if c.Kind == "type" && (c.Change == "removed" || c.Change == "changed") {
			types[c.Package] = append(types[c.Package], c.Symbol)
		}
	}
	if len(types) == 0 {
		return nil
	}
	var out []domain.APIChange
	for k, a := range after {
		b, ok := before[k]
		if !ok || a.Sig != b.Sig || a.Kind == "type" {
			continue
		}
		for _, t := range types[a.Package] {
			if mentions(a.Sig, t) {
				out = append(out, domain.APIChange{
					Package: a.Package, Symbol: a.Name, Kind: a.Kind, Change: "indirect",
					Before: b.Sig, After: "same text; uses " + t + ", which changed",
				})
				break
			}
		}
	}
	return out
}

func mentions(sig, ident string) bool {
	for i := strings.Index(sig, ident); i >= 0; {
		end := i + len(ident)
		okStart := i == 0 || !isIdentByte(sig[i-1])
		okEnd := end == len(sig) || !isIdentByte(sig[end])
		if okStart && okEnd {
			return true
		}
		next := strings.Index(sig[end:], ident)
		if next < 0 {
			return false
		}
		i = end + next
	}
	return false
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Uses finds package-qualified references (pkg.Symbol) in files that import module or
// movedTo. Only removed and changed symbols are reported.
func Uses(root string, files []string, module, movedTo string, changes []domain.APIChange) ([]domain.CallSite, int) {
	broken := map[string]string{}
	for _, c := range changes {
		if c.Change == "added" || c.Kind == "method" {
			continue
		}
		broken[c.Package+"."+c.Symbol] = c.Change
	}
	var sites []domain.CallSite
	touched := map[string]bool{}
	for _, rel := range files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		locals := map[string]string{}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			var pkgRel string
			switch {
			case modmove.InModule(p, module):
				pkgRel = modmove.Rel(p, module)
			case movedTo != "" && modmove.InModule(p, movedTo):
				pkgRel = modmove.Rel(p, movedTo)
			default:
				continue
			}
			name := importName(p)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "_" || name == "." {
				continue
			}
			locals[name] = pkgRel
		}
		if len(locals) == 0 {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			pkgRel, ok := locals[id.Name]
			if !ok {
				return true
			}
			change, ok := broken[pkgRel+"."+sel.Sel.Name]
			if !ok {
				return true
			}
			pos := fset.Position(sel.Pos())
			touched[rel] = true
			if len(sites) < maxSites {
				sites = append(sites, domain.CallSite{
					File: filepath.ToSlash(rel), Line: pos.Line, Column: pos.Column,
					Package: pkgRel, Symbol: sel.Sel.Name, Change: change,
				})
			}
			return true
		})
	}
	return sites, len(touched)
}

func importName(p string) string {
	return modmove.GuessName(p)
}

// Build fills an Impact from two module source trees.
func Build(root string, files []string, module, from, toModule, to, beforeDir, afterDir string) *domain.Impact {
	imp := &domain.Impact{Module: module, From: from, ToModule: toModule, To: to, Status: "recorded"}
	if toModule == "" {
		imp.ToModule = module
	}
	before, err := Exports(beforeDir)
	if err != nil {
		imp.Status = "unavailable"
		imp.Reason = "reading " + module + "@" + from + ": " + err.Error()
		return imp
	}
	after, err := Exports(afterDir)
	if err != nil {
		imp.Status = "unavailable"
		imp.Reason = "reading " + imp.ToModule + "@" + to + ": " + err.Error()
		return imp
	}
	all := Compare(before, after)
	for _, c := range all {
		switch c.Change {
		case "removed":
			imp.Removed++
		case "changed", "indirect":
			imp.Changed++
		case "added":
			imp.Added++
		}
		if c.Kind == "method" && c.Change != "added" {
			imp.MethodsChanged++
		}
	}
	if len(all) > maxChanges {
		imp.Changes = all[:maxChanges]
	} else {
		imp.Changes = all
	}
	imp.Sites, imp.FilesAffected = Uses(root, files, module, imp.ToModule, all)
	imp.Note = "syntactic comparison of exported declarations; not type-checked. Method changes are counted, but calls on values are not attributed to lines."
	return imp
}
