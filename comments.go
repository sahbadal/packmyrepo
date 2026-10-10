package main

import (
	"regexp"
	"strings"
)

// syntax describes how comments and strings look for a language family.
type syntax struct {
	line       []string // line comment starters
	blockOpen  string
	blockClose string
	quotes     string // characters that start a string
	triple     bool   // python style """ and ''' strings
	hashSpace  bool   // '#' only starts a comment at line start or after whitespace
}

func syntaxFor(fam int, lang string) (syntax, bool) {
	switch fam {
	case famCLike, famGo:
		s := syntax{line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: "\"'`"}
		if lang == "php" {
			s.line = append(s.line, "#")
			s.hashSpace = true
		}
		return s, true
	case famRust:
		// ' is used for lifetimes in Rust, so it is not treated as a string quote.
		return syntax{line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: "\""}, true
	case famCSS:
		return syntax{blockOpen: "/*", blockClose: "*/", quotes: "\"'"}, true
	case famPython:
		return syntax{line: []string{"#"}, quotes: "\"'", triple: true, hashSpace: true}, true
	case famHash:
		return syntax{line: []string{"#"}, quotes: "\"'", hashSpace: true}, true
	case famHTML:
		return syntax{blockOpen: "<!--", blockClose: "-->"}, true
	case famSQL:
		if lang == "haskell" {
			return syntax{line: []string{"--"}, blockOpen: "{-", blockClose: "-}", quotes: "\""}, true
		}
		return syntax{line: []string{"--"}, blockOpen: "/*", blockClose: "*/", quotes: "\"'"}, true
	}
	return syntax{}, false
}

// scanMode controls what scanSource produces.
const (
	modeStrip = iota // drop comments, keep strings
	modeBlank        // replace comment and string contents with spaces (same length)
)

// scanSource walks src once, understanding strings and comments.
func scanSource(src string, sx syntax, mode int) string {
	var b strings.Builder
	b.Grow(len(src))
	n := len(src)
	blankRange := func(s string) {
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		}
	}
	i := 0
	for i < n {
		c := src[i]

		// shebang line is kept
		if i == 0 && strings.HasPrefix(src, "#!") {
			end := strings.IndexByte(src, '\n')
			if end < 0 {
				end = n
			}
			b.WriteString(src[:end])
			i = end
			continue
		}

		// block comment
		if sx.blockOpen != "" && strings.HasPrefix(src[i:], sx.blockOpen) {
			end := strings.Index(src[i+len(sx.blockOpen):], sx.blockClose)
			stop := n
			if end >= 0 {
				stop = i + len(sx.blockOpen) + end + len(sx.blockClose)
			}
			if mode == modeBlank {
				blankRange(src[i:stop])
			} else {
				// keep the newlines so line structure survives
				b.WriteString(strings.Repeat("\n", strings.Count(src[i:stop], "\n")))
			}
			i = stop
			continue
		}

		// line comment
		isLine := false
		for _, lc := range sx.line {
			if strings.HasPrefix(src[i:], lc) {
				if lc == "#" && sx.hashSpace && i > 0 && src[i-1] != ' ' && src[i-1] != '\t' && src[i-1] != '\n' {
					continue
				}
				isLine = true
				break
			}
		}
		if isLine {
			end := strings.IndexByte(src[i:], '\n')
			stop := n
			if end >= 0 {
				stop = i + end
			}
			if mode == modeBlank {
				blankRange(src[i:stop])
			}
			i = stop
			continue
		}

		// strings
		if sx.quotes != "" && strings.IndexByte(sx.quotes, c) >= 0 {
			stop := stringEnd(src, i, sx)
			if mode == modeBlank {
				b.WriteByte(c)
				if stop-i > 2 {
					blankRange(src[i+1 : stop-1])
				}
				if stop-i >= 2 {
					b.WriteByte(src[stop-1])
				}
			} else {
				b.WriteString(src[i:stop])
			}
			i = stop
			continue
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

// stringEnd returns the index just after the string that starts at i.
func stringEnd(src string, i int, sx syntax) int {
	n := len(src)
	q := src[i]
	if sx.triple && i+2 < n && src[i+1] == q && src[i+2] == q {
		end := strings.Index(src[i+3:], string([]byte{q, q, q}))
		if end < 0 {
			return n
		}
		return i + 3 + end + 3
	}
	j := i + 1
	for j < n {
		ch := src[j]
		if ch == '\\' && q != '`' {
			j += 2
			continue
		}
		if ch == '\\' && q == '`' {
			j += 2
			continue
		}
		if ch == q {
			return j + 1
		}
		// normal quotes do not span lines; template literals do
		if ch == '\n' && q != '`' {
			return j
		}
		j++
	}
	return n
}

var manyBlankLines = regexp.MustCompile(`\n{3,}`)

// removeComments strips comments and tidies the blank lines left behind.
func removeComments(src, lang string) (string, bool) {
	sx, ok := syntaxFor(familyOf(lang), lang)
	if !ok {
		return src, false
	}
	out := scanSource(src, sx, modeStrip)
	origLines := strings.Split(src, "\n")
	lines := strings.Split(out, "\n")
	kept := lines[:0]
	for i, l := range lines {
		l = strings.TrimRight(l, " \t\r")
		// drop lines that only held a comment
		if strings.TrimSpace(l) == "" && i < len(origLines) && strings.TrimSpace(origLines[i]) != "" {
			continue
		}
		kept = append(kept, l)
	}
	res := manyBlankLines.ReplaceAllString(strings.Join(kept, "\n"), "\n\n")
	return strings.TrimLeft(res, "\n"), true
}
