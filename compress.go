package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// compressCode keeps the "shape" of a file (imports, types, signatures) and
// replaces function bodies with a placeholder. It returns ok=false when the
// language is not supported, so callers can fall back to other strategies.
func compressCode(src, lang string) (string, bool) {
	switch familyOf(lang) {
	case famGo:
		if out, ok := compressGo(src); ok {
			return out, true
		}
		return src, false
	case famPython:
		return compressPython(src), true
	case famCLike, famRust:
		if lang == "jsonc" || lang == "protobuf" || lang == "scss" || lang == "less" {
			return src, false
		}
		sx, _ := syntaxFor(familyOf(lang), lang)
		return compressBraces(src, sx), true
	}
	return src, false
}

// ---------- Go: exact, using the standard library parser ----------

func compressGo(src string) (string, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		return "", false
	}
	type span struct{ a, b int }
	var spans []span
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		a := fset.Position(fn.Body.Lbrace).Offset
		b := fset.Position(fn.Body.Rbrace).Offset
		if b > a+1 {
			spans = append(spans, span{a + 1, b})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a > spans[j].a })
	out := src
	for _, s := range spans {
		out = out[:s.a] + " /* ... */ " + out[s.b:]
	}
	return out, true
}

// ---------- Brace languages: fast heuristic ----------

var controlWords = map[string]bool{
	"if": true, "else": true, "for": true, "foreach": true, "while": true, "do": true,
	"switch": true, "case": true, "catch": true, "try": true, "finally": true, "with": true,
	"synchronized": true, "using": true, "lock": true, "match": true, "loop": true,
	"unsafe": true, "return": true, "when": true, "select": true, "defer": true, "go": true,
}

var typeWordRe = regexp.MustCompile(`\b(class|struct|interface|enum|namespace|impl|trait|object|record|extension|protocol|union|module|package)\b`)
var afterParenRe = regexp.MustCompile(`\)\s*(->|:|throws\b|const\b|noexcept\b|override\b|final\b|where\b|async\b|mutating\b|rethrows\b)`)
var firstWordRe = regexp.MustCompile(`^[A-Za-z_]+`)

func isFuncHeader(h string) bool {
	h = strings.TrimSpace(h)
	if h == "" {
		return false
	}
	// look only at the last statement-ish line chunk
	first := firstWordRe.FindString(strings.TrimLeft(h, "@)] \t\n"))
	if controlWords[first] {
		return false
	}
	if strings.HasSuffix(h, "=>") {
		return true
	}
	if !strings.Contains(h, "(") {
		return false
	}
	if typeWordRe.MatchString(h[:strings.Index(h, "(")]) {
		return false
	}
	if strings.HasSuffix(h, ")") {
		return true
	}
	return afterParenRe.MatchString(h)
}

// matchBrace finds the '}' matching the '{' at index i in a blanked source.
func matchBrace(s string, i int) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

func compressBraces(src string, sx syntax) string {
	skel := scanSource(src, sx, modeBlank) // same length as src, strings/comments blanked
	if len(skel) != len(src) {
		return src
	}
	type span struct{ a, b int }
	var spans []span
	last := -1
	for i := 0; i < len(skel); i++ {
		switch skel[i] {
		case ';', '}':
			last = i
		case '{':
			header := skel[last+1 : i]
			if isFuncHeader(header) {
				j := matchBrace(skel, i)
				if j < 0 {
					return src
				}
				if strings.TrimSpace(skel[i+1:j]) != "" {
					spans = append(spans, span{i + 1, j})
				}
				i = j
				last = j
				continue
			}
			last = i
		}
	}
	out := src
	for k := len(spans) - 1; k >= 0; k-- {
		s := spans[k]
		out = out[:s.a] + " /* ... */ " + out[s.b:]
	}
	return out
}

// ---------- Python: indentation based ----------

var pyDefRe = regexp.MustCompile(`^(\s*)(async\s+def|def)\s`)

func indentOf(l string) int {
	n := 0
	for _, c := range l {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

func compressPython(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		m := pyDefRe.FindStringSubmatch(l)
		if m == nil {
			out = append(out, l)
			continue
		}
		defIndent := indentOf(l)
		// signature may span several lines: until a line ending with ':' at paren depth 0
		depth := 0
		j := i
		for ; j < len(lines); j++ {
			code := lines[j]
			if h := strings.Index(code, "#"); h >= 0 {
				code = code[:h]
			}
			depth += strings.Count(code, "(") + strings.Count(code, "[") - strings.Count(code, ")") - strings.Count(code, "]")
			out = append(out, lines[j])
			if depth <= 0 {
				tc := strings.TrimSpace(code)
				lp := strings.LastIndex(tc, ")")
				if strings.HasSuffix(tc, ":") || (lp >= 0 && strings.Contains(tc[lp:], ":")) {
					break
				}
			}
		}
		if j >= len(lines) {
			break
		}
		// one-line body like `def f(): return 1` is already emitted as is
		sig := lines[j]
		if h := strings.Index(sig, "#"); h >= 0 {
			sig = sig[:h]
		}
		if !strings.HasSuffix(strings.TrimSpace(sig), ":") {
			i = j
			continue
		}
		k := j + 1
		for k < len(lines) && strings.TrimSpace(lines[k]) == "" {
			k++
		}
		if k >= len(lines) || indentOf(lines[k]) <= defIndent {
			i = j
			continue
		}
		bodyIndent := strings.Repeat(" ", indentOf(lines[k]))
		// keep the docstring
		t := strings.TrimSpace(lines[k])
		if strings.HasPrefix(t, `"""`) || strings.HasPrefix(t, `'''`) {
			q := t[:3]
			out = append(out, lines[k])
			if !(len(t) >= 6 && strings.HasSuffix(t, q)) {
				for k++; k < len(lines); k++ {
					out = append(out, lines[k])
					if strings.Contains(lines[k], q) {
						break
					}
				}
			}
			k++
		}
		out = append(out, bodyIndent+"...")
		// skip the rest of the body
		for k < len(lines) && (strings.TrimSpace(lines[k]) == "" || indentOf(lines[k]) > defIndent) {
			k++
		}
		// keep one blank line for readability
		if k < len(lines) && k > 0 && strings.TrimSpace(lines[k-1]) == "" {
			out = append(out, "")
		}
		i = k - 1
	}
	return strings.Join(out, "\n")
}

// truncateData keeps the head of large data-like files (JSON, CSV, logs...).
func truncateData(src string, keep int) (string, bool) {
	lines := strings.Split(src, "\n")
	if len(lines) <= keep+5 {
		return src, false
	}
	rest := len(lines) - keep
	return strings.Join(lines[:keep], "\n") + "\n... (" + itoa(rest) + " more lines truncated by packmyrepo)", true
}

var dataLangs = map[string]bool{"json": true, "csv": true, "yaml": true, "xml": true, "text": true, "markdown": true, "mdx": true, "rst": true, "sql": true}
