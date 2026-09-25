package recipe

import (
	"strings"
	"unicode"

	"github.com/sureshpsc/evolvectl/internal/domain"
	"github.com/sureshpsc/evolvectl/internal/idgen"
	"github.com/sureshpsc/evolvectl/internal/patch"
)

func applyPython(filename string, src []byte, recipes []Recipe) ([]byte, []domain.Change, error) {
	text := string(src)
	var changes []domain.Change
	cur := text
	for _, r := range recipes {
		next, ok := rewritePython(cur, r)
		if !ok || next == cur {
			continue
		}
		changes = append(changes, domain.Change{
			ID:         idgen.New("chg_"),
			File:       filename,
			Kind:       "recipe",
			RecipeID:   r.Metadata.ID,
			Confidence: Confidence(r),
			Reason:     r.Metadata.Title,
		})
		cur = next
	}
	if cur == text {
		return src, nil, nil
	}
	out := []byte(cur)
	diff := patch.Unified(filename, text, cur)
	for i := range changes {
		changes[i].Diff = diff
	}
	return out, changes, nil
}

func rewritePython(src string, r Recipe) (string, bool) {
	switch r.Spec.Transform.Type {
	case "python_import_rename":
		return replaceImportPath(src, r.Spec.Match.Package, r.Spec.Transform.TargetImport)
	case "python_decorator_rename", "python_symbol_rename":
		from := r.Spec.Match.Symbol
		to := r.Spec.Transform.TargetSymbol
		return replaceCode(src, func(code string) (string, bool) {
			return replaceSymbolUses(code, from, to)
		})
	case "python_method_rename":
		from := "." + r.Spec.Match.Symbol + "("
		to := "." + r.Spec.Transform.TargetSymbol + "("
		return replaceCode(src, func(code string) (string, bool) {
			if !strings.Contains(code, from) {
				return code, false
			}
			return strings.ReplaceAll(code, from, to), true
		})
	case "python_keyword_rename":
		from := r.Spec.Match.Keyword
		to := r.Spec.Transform.TargetKeyword
		if from == "" {
			from = r.Spec.Match.Symbol
			to = r.Spec.Transform.TargetSymbol
		}
		return replaceCode(src, func(code string) (string, bool) {
			return replaceKeyword(code, from, to)
		})
	default:
		return src, false
	}
}

func replaceImportPath(src, oldPath, newPath string) (string, bool) {
	if oldPath == "" || newPath == "" {
		return src, false
	}
	return replaceCode(src, func(code string) (string, bool) {
		changed := false
		lines := strings.Split(code, "\n")
		for i, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "from "+oldPath+" ") || trim == "from "+oldPath || strings.HasPrefix(trim, "import "+oldPath) || strings.Contains(line, " "+oldPath+" ") || strings.HasSuffix(trim, " "+oldPath) {
				updated := strings.ReplaceAll(line, oldPath, newPath)
				if updated != line {
					lines[i] = updated
					changed = true
				}
			}
		}
		return strings.Join(lines, "\n"), changed
	})
}

func replaceSymbolUses(code, from, to string) (string, bool) {
	if from == "" || to == "" || from == to {
		return code, false
	}
	var b strings.Builder
	changed := false
	for i := 0; i < len(code); {
		if strings.HasPrefix(code[i:], from) && identBoundary(code, i, i+len(from)) {
			b.WriteString(to)
			i += len(from)
			changed = true
			continue
		}
		b.WriteByte(code[i])
		i++
	}
	return b.String(), changed
}

func identBoundary(code string, start, end int) bool {
	if start > 0 {
		r := rune(code[start-1])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return false
		}
	}
	if end < len(code) {
		r := rune(code[end])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return false
		}
	}
	return true
}

func replaceKeyword(code, from, to string) (string, bool) {
	if from == "" || to == "" {
		return code, false
	}
	needle := from + "="
	repl := to + "="
	var b strings.Builder
	changed := false
	for i := 0; i < len(code); {
		if strings.HasPrefix(code[i:], needle) && keywordBoundary(code, i) {
			b.WriteString(repl)
			i += len(needle)
			changed = true
			continue
		}
		b.WriteByte(code[i])
		i++
	}
	return b.String(), changed
}

func keywordBoundary(code string, i int) bool {
	if i == 0 {
		return true
	}
	prev := code[i-1]
	return prev == '(' || prev == ',' || prev == ' ' || prev == '\n' || prev == '\t'
}

type pySeg struct {
	code bool
	text string
}

func scanPython(src string) []pySeg {
	var segs []pySeg
	var code strings.Builder
	flush := func() {
		if code.Len() == 0 {
			return
		}
		segs = append(segs, pySeg{code: true, text: code.String()})
		code.Reset()
	}
	for i := 0; i < len(src); {
		c := src[i]
		if c == '#' {
			flush()
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				segs = append(segs, pySeg{text: src[i:]})
				return segs
			}
			segs = append(segs, pySeg{text: src[i : i+j+1]})
			i += j + 1
			continue
		}
		if c == '"' || c == '\'' {
			flush()
			end := consumePyString(src, i)
			segs = append(segs, pySeg{text: src[i:end]})
			i = end
			continue
		}
		code.WriteByte(c)
		i++
	}
	flush()
	return segs
}

func consumePyString(src string, i int) int {
	q := src[i]
	if i+2 < len(src) && src[i+1] == q && src[i+2] == q {
		needle := strings.Repeat(string(q), 3)
		j := strings.Index(src[i+3:], needle)
		if j < 0 {
			return len(src)
		}
		return i + 3 + j + 3
	}
	for j := i + 1; j < len(src); j++ {
		if src[j] == '\\' && j+1 < len(src) {
			j++
			continue
		}
		if src[j] == q || src[j] == '\n' {
			if src[j] == q {
				return j + 1
			}
			return j
		}
	}
	return len(src)
}

// replaceCode rewrites only code outside strings and comments.
func replaceCode(src string, fn func(string) (string, bool)) (string, bool) {
	segs := scanPython(src)
	var b strings.Builder
	changed := false
	for _, seg := range segs {
		if !seg.code {
			b.WriteString(seg.text)
			continue
		}
		next, ok := fn(seg.text)
		if ok {
			changed = true
			b.WriteString(next)
		} else {
			b.WriteString(seg.text)
		}
	}
	return b.String(), changed
}
