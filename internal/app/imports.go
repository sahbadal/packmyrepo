package app

import (
	"path"
	"regexp"
	"strings"
)

// Resolver finds which project files a file imports. It only follows imports
// that point inside the project; packages from npm/pip/etc. are ignored.
type Resolver struct {
	files    map[string]*File
	goModule string
	bySuffix map[string][]string // "com/x/Y.java" -> paths (Java/Kotlin)
}

func newResolver(files []*File, goModule string) *Resolver {
	r := &Resolver{files: map[string]*File{}, goModule: goModule, bySuffix: map[string][]string{}}
	for _, f := range files {
		r.files[f.Path] = f
		ext := path.Ext(f.Path)
		if ext == ".java" || ext == ".kt" || ext == ".scala" {
			parts := strings.Split(f.Path, "/")
			for i := range parts {
				suf := strings.Join(parts[i:], "/")
				r.bySuffix[suf] = append(r.bySuffix[suf], f.Path)
			}
		}
	}
	return r
}

func (r *Resolver) has(p string) bool {
	_, ok := r.files[path.Clean(p)]
	return ok
}

var (
	jsImportRe   = regexp.MustCompile(`(?m)(?:import|export)\s[^'"]*?from\s*['"]([^'"]+)['"]|import\s*\(?\s*['"]([^'"]+)['"]|require\(\s*['"]([^'"]+)['"]\s*\)|@import\s+(?:url\()?['"]([^'"]+)['"]`)
	pyFromRe     = regexp.MustCompile(`(?m)^\s*from\s+(\.*)([\w.]*)\s+import\s+\(?([\w\s,*]+)`)
	pyImportRe   = regexp.MustCompile(`(?m)^\s*import\s+([\w.]+(?:\s*,\s*[\w.]+)*)`)
	goImportRe   = regexp.MustCompile(`"([^"\s]+)"`)
	goBlockRe    = regexp.MustCompile(`(?s)\bimport\s*\((.*?)\)|\bimport\s+(?:\w+\s+)?("[^"]+")`)
	cIncludeRe   = regexp.MustCompile(`(?m)^\s*#\s*include\s*"([^"]+)"`)
	rustModRe    = regexp.MustCompile(`(?m)^\s*(?:pub(?:\([^)]*\))?\s+)?mod\s+(\w+)\s*;`)
	rustUseRe    = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?use\s+crate::([\w:]+)`)
	dartRe       = regexp.MustCompile(`(?m)^\s*(?:import|export|part)\s+['"]([^'"]+)['"]`)
	javaRe       = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.]+)\s*;?`)
	phpIncludeRe = regexp.MustCompile(`(?:require|include)(?:_once)?\s*\(?\s*(?:__DIR__\s*\.\s*)?['"]([^'"]+)['"]`)
)

var jsExts = []string{"", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".d.ts", ".vue", ".svelte", ".json", ".css", ".scss"}

// Imports returns project-local files imported by f.
func (r *Resolver) Imports(f *File) []string {
	dir := path.Dir(f.Path)
	if dir == "." {
		dir = ""
	}
	src := f.Raw
	var out []string
	add := func(p string) {
		p = path.Clean(strings.TrimPrefix(p, "./"))
		if p != f.Path && r.has(p) {
			out = append(out, p)
		}
	}
	tryJS := func(base string) {
		for _, e := range jsExts {
			if r.has(base + e) {
				add(base + e)
				return
			}
		}
		for _, e := range jsExts[1:] {
			if r.has(base + "/index" + e) {
				add(base + "/index" + e)
				return
			}
		}
	}

	switch langOf(f.Path) {
	case "javascript", "jsx", "typescript", "tsx", "vue", "svelte", "astro", "css", "scss", "less":
		for _, m := range jsImportRe.FindAllStringSubmatch(src, -1) {
			spec := firstNonEmpty(m[1:]...)
			switch {
			case strings.HasPrefix(spec, "."):
				tryJS(path.Join(dir, spec))
			case strings.HasPrefix(spec, "@/") || strings.HasPrefix(spec, "~/"):
				tryJS(path.Join("src", spec[2:]))
				tryJS(spec[2:])
			case strings.HasPrefix(spec, "/"):
				tryJS(strings.TrimPrefix(spec, "/"))
			}
		}
	case "python":
		pyMod := func(base, mod string) {
			p := path.Join(base, strings.ReplaceAll(mod, ".", "/"))
			if r.has(p + ".py") {
				add(p + ".py")
			} else if r.has(p + "/__init__.py") {
				add(p + "/__init__.py")
			}
		}
		roots := []string{"", "src", dir}
		for _, m := range pyFromRe.FindAllStringSubmatch(src, -1) {
			dots, mod, names := m[1], m[2], m[3]
			bases := roots
			if dots != "" {
				b := dir
				for i := 1; i < len(dots); i++ {
					b = path.Dir(b)
				}
				if b == "." {
					b = ""
				}
				bases = []string{b}
			}
			for _, b := range bases {
				if mod != "" {
					pyMod(b, mod)
				}
				for _, n := range strings.Split(names, ",") {
					fl := strings.Fields(n)
					if len(fl) == 0 {
						continue
					}
					if n = fl[0]; n != "*" {
						sub := n
						if mod != "" {
							sub = mod + "." + n
						}
						pyMod(b, sub)
					}
				}
			}
		}
		for _, m := range pyImportRe.FindAllStringSubmatch(src, -1) {
			for _, mod := range strings.Split(m[1], ",") {
				for _, b := range roots {
					pyMod(b, strings.TrimSpace(mod))
				}
			}
		}
	case "go":
		// every non-test file in the same package
		for p := range r.files {
			if path.Dir(p) == path.Dir(f.Path) && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				add(p)
			}
		}
		if r.goModule != "" {
			for _, blk := range goBlockRe.FindAllStringSubmatch(src, -1) {
				for _, m := range goImportRe.FindAllStringSubmatch(blk[1]+blk[2], -1) {
					ip := m[1]
					if ip == r.goModule || strings.HasPrefix(ip, r.goModule+"/") {
						pkgDir := strings.TrimPrefix(strings.TrimPrefix(ip, r.goModule), "/")
						for p := range r.files {
							d := path.Dir(p)
							if d == "." {
								d = ""
							}
							if d == pkgDir && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
								add(p)
							}
						}
					}
				}
			}
		}
	case "c", "cpp", "objectivec":
		for _, m := range cIncludeRe.FindAllStringSubmatch(src, -1) {
			for _, b := range []string{dir, "", "include", "src"} {
				if p := path.Join(b, m[1]); r.has(p) {
					add(p)
					break
				}
			}
		}
	case "rust":
		modDir := dir
		base := path.Base(f.Path)
		if base != "main.rs" && base != "lib.rs" && base != "mod.rs" {
			modDir = path.Join(dir, strings.TrimSuffix(base, ".rs"))
		}
		for _, m := range rustModRe.FindAllStringSubmatch(src, -1) {
			for _, p := range []string{path.Join(modDir, m[1]+".rs"), path.Join(modDir, m[1], "mod.rs")} {
				add(p)
			}
		}
		for _, m := range rustUseRe.FindAllStringSubmatch(src, -1) {
			parts := strings.Split(strings.TrimRight(m[1], ":"), "::")
			for i := len(parts); i > 0; i-- {
				p := path.Join(append([]string{"src"}, parts[:i]...)...)
				if r.has(p + ".rs") {
					add(p + ".rs")
					break
				}
				if r.has(p + "/mod.rs") {
					add(p + "/mod.rs")
					break
				}
			}
		}
	case "dart":
		for _, m := range dartRe.FindAllStringSubmatch(src, -1) {
			spec := m[1]
			if strings.HasPrefix(spec, "package:") {
				if i := strings.IndexByte(spec, '/'); i > 0 {
					add(path.Join("lib", spec[i+1:]))
				}
			} else if !strings.HasPrefix(spec, "dart:") {
				add(path.Join(dir, spec))
			}
		}
	case "java", "kotlin", "scala":
		for _, m := range javaRe.FindAllStringSubmatch(src, -1) {
			p := strings.ReplaceAll(m[1], ".", "/")
			for _, ext := range []string{".java", ".kt", ".scala"} {
				for _, hit := range r.bySuffix[p+ext] {
					add(hit)
				}
			}
		}
	case "php":
		for _, m := range phpIncludeRe.FindAllStringSubmatch(src, -1) {
			add(path.Join(dir, m[1]))
		}
	}
	return dedupe(out)
}

// Closure walks imports breadth-first from the entry files up to maxDepth
// (0 = unlimited). It returns the files reached with their depth.
func (r *Resolver) Closure(entries []string, maxDepth int) map[string]int {
	seen := map[string]int{}
	queue := []string{}
	for _, e := range entries {
		e = path.Clean(e)
		if r.has(e) {
			seen[e] = 0
			queue = append(queue, e)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		d := seen[cur]
		if maxDepth > 0 && d >= maxDepth {
			continue
		}
		for _, dep := range r.Imports(r.files[cur]) {
			if _, ok := seen[dep]; !ok {
				seen[dep] = d + 1
				queue = append(queue, dep)
			}
		}
	}
	return seen
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
