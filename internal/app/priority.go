package app

import (
	"path"
	"regexp"
	"strings"
)

// priorityOf scores how important a file is for understanding a project.
// --budget keeps high scores longest and gives up low scores first.
//
//	100  --entry files (set in Select)
//	 95  a real README
//	 85  manifests: package.json, go.mod, pyproject.toml...
//	 80  framework entry points: Next.js page/layout/route, main.go, App.tsx...
//	 50  ordinary source code (a little less the deeper it sits)
//	 40  project config that explains structure (tsconfig, next.config...)
//	 30  docs, styles
//	 20  tests, template READMEs
//	 15  tooling config (eslint, prettier, postcss, jest...)
//	 10  fixtures, mocks, snapshots, generated files, data dumps
func priorityOf(rel, content string) int {
	low := strings.ToLower(rel)
	base := path.Base(low)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if i := strings.IndexByte(stem, '.'); i > 0 { // "layout.server" -> "layout"
		stem = stem[:i]
	}
	depth := strings.Count(rel, "/")

	switch {
	case isLowValue(low, base):
		return 10
	case isTestFile(low, base):
		return 20
	case strings.HasPrefix(base, "readme"):
		if isTemplateReadme(content) {
			return 20 // the untouched create-next-app / Vite / CRA README says nothing about this project
		}
		return 95
	case manifests[base]:
		return 85
	case isEntryPoint(low, base, stem, ext):
		return 80 - depth
	case isToolingConfig(base):
		return 15
	case projectConfigs[base] || strings.HasPrefix(base, "next.config.") || strings.HasPrefix(base, "vite.config.") ||
		strings.HasPrefix(base, "nuxt.config.") || strings.HasPrefix(base, "astro.config.") || strings.HasPrefix(base, "svelte.config."):
		return 40
	case styleExts[ext]:
		return 30
	case ext == ".md" || ext == ".mdx" || ext == ".rst" || ext == ".txt":
		return 30
	}
	return 50 - depth*2
}

var manifests = map[string]bool{
	"package.json": true, "go.mod": true, "cargo.toml": true, "pyproject.toml": true, "pubspec.yaml": true,
	"requirements.txt": true, "pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "composer.json": true,
	"gemfile": true, "dockerfile": true, "docker-compose.yml": true, "docker-compose.yaml": true, "compose.yaml": true,
}

var projectConfigs = map[string]bool{
	"tsconfig.json": true, "jsconfig.json": true, ".env.example": true, "schema.prisma": true, "openapi.yaml": true,
	"openapi.json": true, "angular.json": true, "vercel.json": true, "netlify.toml": true, "wrangler.toml": true,
}

var styleExts = map[string]bool{".css": true, ".scss": true, ".sass": true, ".less": true, ".styl": true}

// Next.js (app and pages router), Remix, SvelteKit, Nuxt and plain entry files.
var routeStems = map[string]bool{
	"page": true, "layout": true, "route": true, "loading": true, "error": true, "not-found": true,
	"template": true, "default": true, "+page": true, "+layout": true, "+server": true,
}

var codeExts = map[string]bool{
	".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true, ".cjs": true, ".vue": true, ".svelte": true,
	".go": true, ".py": true, ".rs": true, ".java": true, ".kt": true, ".dart": true, ".rb": true, ".php": true, ".cs": true,
}

func isEntryPoint(low, base, stem string, ext string) bool {
	if !codeExts[ext] {
		return false
	}
	inApp := strings.HasPrefix(low, "app/") || strings.Contains(low, "/app/") ||
		strings.HasPrefix(low, "routes/") || strings.Contains(low, "/routes/")
	if inApp && routeStems[stem] {
		return true
	}
	if (strings.HasPrefix(low, "pages/") || strings.Contains(low, "/pages/")) && (stem == "_app" || stem == "_document" || stem == "index") {
		return true
	}
	switch stem {
	case "main", "index", "app", "server", "middleware", "instrumentation", "manage", "__main__", "wsgi", "asgi", "urls", "settings", "lib":
		// only near the top of the project, not every index.ts deep in a folder
		return strings.Count(low, "/") <= 2 || stem == "middleware"
	}
	return false
}

var toolingPrefixes = []string{
	"eslint.config.", ".eslintrc", "prettier.config.", ".prettierrc", "postcss.config.", "tailwind.config.",
	"babel.config.", ".babelrc", "jest.config.", "vitest.config.", "playwright.config.", "cypress.config.",
	"commitlint.config.", ".lintstagedrc", "lint-staged.config.", "stylelint.config.", ".stylelintrc",
	"webpack.config.", "rollup.config.", "turbo.json", "renovate.json", ".editorconfig", "next-env.d.ts",
}

func isToolingConfig(base string) bool {
	for _, p := range toolingPrefixes {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	return false
}

func isTestFile(low, base string) bool {
	return strings.Contains(low, "/test/") || strings.Contains(low, "/tests/") || strings.HasPrefix(low, "test/") ||
		strings.HasPrefix(low, "tests/") || strings.Contains(low, "__tests__") || strings.Contains(base, "_test.") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_") ||
		strings.Contains(low, "/e2e/") || strings.HasPrefix(low, "e2e/")
}

func isLowValue(low, base string) bool {
	return strings.Contains(low, "fixture") || strings.Contains(low, "__snapshots__") || strings.Contains(low, "__mocks__") ||
		strings.Contains(low, "/mocks/") || strings.Contains(low, "/generated/") || strings.Contains(low, "migrations/") ||
		strings.HasSuffix(base, ".csv") || strings.HasSuffix(base, ".svg") || strings.HasSuffix(base, ".snap") ||
		strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".min.js")
}

// Phrases that only appear in the README a project generator writes.
var templateReadmeRe = regexp.MustCompile(`(?i)bootstrapped with \[?` + "`?" + `create-(next|react)-app|this template provides a minimal setup to get react working in vite|^# (react|vue|svelte) \+ (typescript \+ )?vite`)

func isTemplateReadme(content string) bool {
	head := content
	if len(head) > 2000 {
		head = head[:2000]
	}
	return templateReadmeRe.MatchString(head)
}
