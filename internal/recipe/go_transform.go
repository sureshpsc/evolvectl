package recipe

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/idgen"
	"github.com/evolvectl/evolvectl/internal/modmove"
	"github.com/evolvectl/evolvectl/internal/patch"
)

// ApplySource rewrites one source file with the applicable recipes.
func ApplySource(filename string, src []byte, recipes []Recipe, vc VersionContext) ([]byte, []domain.Change, error) {
	lang := languageFromName(filename)
	var selected []Recipe
	for _, r := range recipes {
		if !Applicable(r, vc) {
			continue
		}
		if r.Spec.Language != "" && r.Spec.Language != lang {
			continue
		}
		selected = append(selected, r)
	}
	if len(selected) == 0 {
		return src, nil, nil
	}
	switch lang {
	case "go":
		return applyGo(filename, src, selected)
	case "python":
		return applyPython(filename, src, selected)
	default:
		return src, nil, nil
	}
}

func languageFromName(name string) string {
	switch {
	case strings.HasSuffix(name, ".go"):
		return "go"
	case strings.HasSuffix(name, ".py"):
		return "python"
	default:
		return ""
	}
}

func applyGo(filename string, src []byte, recipes []Recipe) ([]byte, []domain.Change, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	var changes []domain.Change
	changed := false
	for _, r := range recipes {
		ok, err := rewriteGo(file, r)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", r.Metadata.ID, err)
		}
		if ok {
			changed = true
			changes = append(changes, domain.Change{
				ID:         idgen.New("chg_"),
				File:       filename,
				Kind:       "recipe",
				RecipeID:   r.Metadata.ID,
				Confidence: Confidence(r),
				Reason:     r.Metadata.Title,
			})
		}
	}
	if !changed {
		return src, nil, nil
	}
	var buf strings.Builder
	if err := format.Node(&buf, fset, file); err != nil {
		return nil, nil, err
	}
	out := []byte(buf.String())
	diff := patch.Unified(filename, string(src), string(out))
	for i := range changes {
		changes[i].Diff = diff
	}
	return out, changes, nil
}

func rewriteGo(file *ast.File, r Recipe) (bool, error) {
	switch r.Spec.Transform.Type {
	case "import_path_replace":
		return rewriteImport(file, r), nil
	case "call_rename", "call_argument_remove", "call_argument_insert", "call_rewrite":
		return rewriteCalls(file, r)
	case "struct_field_rename":
		return rewriteFields(file, r), nil
	case "callback_param_append":
		return appendCallbackParam(file, r)
	default:
		return false, nil
	}
}

func importNames(file *ast.File) (named map[string]string, dots []string) {
	named = map[string]string{}
	for _, imp := range file.Imports {
		pathValue, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		local := modmove.GuessName(pathValue)
		if imp.Name != nil {
			if imp.Name.Name == "_" {
				continue
			}
			if imp.Name.Name == "." {
				dots = append(dots, pathValue)
				continue
			}
			local = imp.Name.Name
		}
		named[local] = pathValue
	}
	return named, dots
}

func rewriteImport(file *ast.File, r Recipe) bool {
	oldPath := r.Spec.Match.Package
	newPath := r.Spec.Transform.TargetImport
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return false
	}
	changed := false
	oldBase := path.Base(oldPath)
	newBase := path.Base(newPath)
	for _, imp := range file.Imports {
		pathValue, err := strconv.Unquote(imp.Path.Value)
		if err != nil || pathValue != oldPath {
			continue
		}
		imp.Path.Value = strconv.Quote(newPath)
		changed = true
		if imp.Name == nil && oldBase != newBase {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if ok && id.Name == oldBase {
					id.Name = newBase
				}
				return true
			})
		}
	}
	return changed
}

func rewriteCalls(file *ast.File, r Recipe) (bool, error) {
	named, dots := importNames(file)
	changed := false
	var firstErr error
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !matchCall(call, named, dots, r) {
			return true
		}
		if err := editCall(call, r); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return true
		}
		changed = true
		return true
	})
	return changed, firstErr
}

func matchCall(call *ast.CallExpr, named map[string]string, dots []string, r Recipe) bool {
	wantPkg := r.Spec.Match.Package
	wantSym := r.Spec.Match.Symbol
	if wantSym == "" {
		return false
	}
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		sel, ok := fun.X.(*ast.Ident)
		if !ok {
			return false
		}
		pkg, ok := named[sel.Name]
		if !ok {
			return false
		}
		if wantPkg != "" && pkg != wantPkg {
			return false
		}
		return fun.Sel.Name == wantSym
	case *ast.Ident:
		if fun.Name != wantSym {
			return false
		}
		if wantPkg == "" {
			return true
		}
		for _, d := range dots {
			if d == wantPkg {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func editCall(call *ast.CallExpr, r Recipe) error {
	if r.Spec.Transform.TargetSymbol != "" {
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			fun.Sel.Name = r.Spec.Transform.TargetSymbol
		case *ast.Ident:
			fun.Name = r.Spec.Transform.TargetSymbol
		}
	}
	if len(r.Spec.Transform.Arguments.Remove) > 0 {
		idx := map[int]bool{}
		for _, rm := range r.Spec.Transform.Arguments.Remove {
			if rm.Index < 0 || rm.Index >= len(call.Args) {
				return fmt.Errorf("argument index %d out of range", rm.Index)
			}
			idx[rm.Index] = true
		}
		var keep []ast.Expr
		for i, arg := range call.Args {
			if !idx[i] {
				keep = append(keep, arg)
			}
		}
		call.Args = keep
	}
	for _, ins := range r.Spec.Transform.Arguments.Insert {
		expr, err := parser.ParseExpr(ins.Expr)
		if err != nil {
			return fmt.Errorf("insert expr: %w", err)
		}
		placeNear(expr, call.Lparen)
		if ins.Index < 0 || ins.Index > len(call.Args) {
			return fmt.Errorf("insert index %d out of range", ins.Index)
		}
		call.Args = append(call.Args, nil)
		copy(call.Args[ins.Index+1:], call.Args[ins.Index:])
		call.Args[ins.Index] = expr
	}
	return nil
}

func appendCallbackParam(file *ast.File, r Recipe) (bool, error) {
	cb := r.Spec.Transform.Callback
	if cb.Type == "" {
		return false, fmt.Errorf("callback.type is required")
	}
	named, dots := importNames(file)
	local := ""
	for name, p := range named {
		if p == r.Spec.Match.Package {
			local = name
		}
	}
	typeSrc := cb.Type
	if strings.Contains(typeSrc, "{pkg}") {
		if local == "" {
			return false, nil
		}
		typeSrc = strings.ReplaceAll(typeSrc, "{pkg}", local)
	}
	name := cb.Name
	if name == "" {
		name = "_"
	}
	changed := false
	var firstErr error
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !matchCall(call, named, dots, r) || cb.Index < 0 || cb.Index >= len(call.Args) {
			return true
		}
		lit, ok := call.Args[cb.Index].(*ast.FuncLit)
		if !ok {
			return true
		}
		count := 0
		if lit.Type.Params != nil {
			for _, f := range lit.Type.Params.List {
				if len(f.Names) == 0 {
					count++
				}
				count += len(f.Names)
			}
		}
		if count != cb.WhenParams {
			return true
		}
		typ, err := parser.ParseExpr(typeSrc)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("callback type: %w", err)
			}
			return true
		}
		pos := lit.Type.Params.Closing
		placeNear(typ, pos)
		field := &ast.Field{Names: []*ast.Ident{{Name: name, NamePos: pos}}, Type: typ}
		lit.Type.Params.List = append(lit.Type.Params.List, field)
		changed = true
		return true
	})
	return changed, firstErr
}

func placeNear(n ast.Node, pos token.Pos) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			n.NamePos = pos
		case *ast.BasicLit:
			n.ValuePos = pos
		case *ast.SelectorExpr:
			n.Sel.NamePos = pos
		}
		return true
	})
}

func rewriteFields(file *ast.File, r Recipe) bool {
	oldName := r.Spec.Match.Field
	newName := r.Spec.Transform.TargetField
	if oldName == "" || newName == "" {
		oldName = r.Spec.Match.Symbol
		newName = r.Spec.Transform.TargetSymbol
	}
	if oldName == "" || newName == "" {
		return false
	}
	named, _ := importNames(file)
	changed := false
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			id, ok := n.Key.(*ast.Ident)
			if ok && id.Name == oldName {
				id.Name = newName
				changed = true
			}
		case *ast.SelectorExpr:
			if n.Sel.Name != oldName {
				return true
			}
			if id, ok := n.X.(*ast.Ident); ok {
				if _, isPkg := named[id.Name]; isPkg {
					return true
				}
			}
			n.Sel.Name = newName
			changed = true
		}
		return true
	})
	return changed
}
