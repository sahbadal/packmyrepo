package main

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// rule is one compiled gitignore-style pattern.
type rule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
	base    string // directory (slash path, "" = root) the pattern is relative to
	// fast paths for the most common patterns, so no regex is needed:
	// kindName = "node_modules" (any depth), kindSuffix = "*.log"
	kind int
	lit  string
}

const (
	kindRegex = iota
	kindName
	kindSuffix
)

// Matcher evaluates gitignore-style rules. Later rules win, like git.
type Matcher struct {
	rules []rule
}

// Add compiles a single pattern relative to base ("" for project root).
func (m *Matcher) Add(pattern, base string) {
	if r, ok := compilePattern(pattern, base); ok {
		m.rules = append(m.rules, r)
	}
}

// AddLines adds every pattern from a slice.
func (m *Matcher) AddLines(lines []string, base string) {
	for _, l := range lines {
		m.Add(l, base)
	}
}

// AddFile loads an ignore file (like .gitignore) whose rules apply under base.
func (m *Matcher) AddFile(file, base string) bool {
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m.Add(sc.Text(), base)
	}
	return true
}

// Match reports whether the slash path rel (relative to project root) is matched.
func (m *Matcher) Match(rel string, isDir bool) bool {
	matched := false
	for _, r := range m.rules {
		if r.dirOnly && !isDir {
			continue
		}
		p := rel
		if r.base != "" {
			if !strings.HasPrefix(rel, r.base+"/") {
				continue
			}
			p = strings.TrimPrefix(rel, r.base+"/")
		}
		var hit bool
		switch r.kind {
		case kindName:
			hit = baseName(p) == r.lit
		case kindSuffix:
			hit = strings.HasSuffix(baseName(p), r.lit)
		default:
			hit = r.re.MatchString(p)
		}
		if hit {
			matched = !r.negate
		}
	}
	return matched
}

// MatchPath checks a file path and all of its parent directories, so a file inside
// an ignored folder is reported as ignored even when it was not found by walking.
func (m *Matcher) MatchPath(rel string) bool {
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if m.Match(strings.Join(parts[:i], "/"), true) {
			return true
		}
	}
	return m.Match(rel, false)
}

func compilePattern(p, base string) (rule, bool) {
	p = strings.TrimRight(p, " \t\r")
	if p == "" || strings.HasPrefix(p, "#") {
		return rule{}, false
	}
	r := rule{base: strings.Trim(base, "/")}
	if strings.HasPrefix(p, "!") {
		r.negate = true
		p = p[1:]
	} else if strings.HasPrefix(p, `\!`) || strings.HasPrefix(p, `\#`) {
		p = p[1:]
	}
	if strings.HasSuffix(p, "/") {
		r.dirOnly = true
		p = strings.TrimRight(p, "/")
	}
	if p == "" {
		return rule{}, false
	}
	anchored := strings.Contains(p, "/")
	p = strings.TrimPrefix(p, "/")
	if !anchored {
		if !strings.ContainsAny(p, "*?[\\") {
			r.kind, r.lit = kindName, p
		} else if strings.HasPrefix(p, "*") && !strings.ContainsAny(p[1:], "*?[\\") {
			r.kind, r.lit = kindSuffix, p[1:]
		}
	}

	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		b.WriteString("^(?:.*/)?")
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			// "**"
			atStart := i == 0
			atEnd := i+2 == len(p)
			nextSlash := i+2 < len(p) && p[i+2] == '/'
			prevSlash := i > 0 && p[i-1] == '/'
			switch {
			case atStart && nextSlash:
				b.WriteString("(?:.*/)?")
				i += 2
			case prevSlash && nextSlash:
				b.WriteString("(?:.*/)?")
				i += 2
			case prevSlash && atEnd:
				b.WriteString(".*")
				i++
			default:
				b.WriteString(".*")
				i++
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			j := strings.IndexByte(p[i:], ']')
			if j > 1 {
				class := p[i+1 : i+j]
				if strings.HasPrefix(class, "!") {
					class = "^" + class[1:]
				}
				b.WriteString("[" + class + "]")
				i += j
			} else {
				b.WriteString(`\[`)
			}
		case c == '\\' && i+1 < len(p):
			i++
			b.WriteString(regexp.QuoteMeta(string(p[i])))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return rule{}, false
	}
	r.re = re
	return r, true
}

func baseName(p string) string {
	return p[strings.LastIndexByte(p, '/')+1:]
}

// defaultIgnores are always skipped (unless --no-default-ignore).
var defaultIgnores = []string{
	".git/", ".hg/", ".svn/", "node_modules/", "bower_components/",
	".DS_Store", "Thumbs.db", "*.log", ".idea/", "*.swp", "*.swo",
	"dist/", "coverage/", ".nyc_output/", ".cache/", ".ctxpack/",
	"*.min.js", "*.min.css", "*.map",
	"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb", "bun.lock",
	"go.sum", "Cargo.lock", "poetry.lock", "Pipfile.lock", "uv.lock",
	"composer.lock", "Gemfile.lock", "pubspec.lock", "packages.lock.json",
	"ctxpack-output.*", "ctxpack-output-*", "repomix-output.*",
}

// noiseFiles rarely help an AI understand code. They are skipped by default;
// --keep-noise brings them back, and --include can pick single ones.
var noiseFiles = []string{
	".gitignore", ".gitattributes", ".gitmodules", ".gitkeep", ".keep",
	".dockerignore", ".npmignore", ".prettierignore", ".eslintignore", ".stylelintignore",
	".editorconfig", ".prettierrc", ".prettierrc.*", "prettier.config.*",
	".nvmrc", ".node-version", ".python-version", ".ruby-version", ".tool-versions",
	".npmrc", ".yarnrc", ".yarnrc.yml",
	"LICENSE", "LICENSE.*", "LICENCE", "LICENCE.*", "COPYING", "NOTICE", "THIRD_PARTY_NOTICES*",
	"CHANGELOG*", "CODE_OF_CONDUCT*", "CONTRIBUTING*", "SECURITY.md",
	".vscode/", ".github/ISSUE_TEMPLATE/", ".github/PULL_REQUEST_TEMPLATE*", ".github/FUNDING.yml",
	".husky/_/",
}
