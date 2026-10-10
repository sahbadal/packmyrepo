package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIgnoreMatcher(t *testing.T) {
	m := &Matcher{}
	m.AddLines([]string{"node_modules/", "*.log", "/build", "docs/**/*.png", "!keep.log"}, "")
	m.Add("*.tmp", "sub")
	cases := []struct {
		path  string
		dir   bool
		match bool
	}{
		{"node_modules", true, true},
		{"a/node_modules", true, true},
		{"node_modules", false, false},
		{"x/err.log", false, true},
		{"keep.log", false, false},
		{"build", true, true},
		{"src/build", true, false},
		{"docs/a/b/c.png", false, true},
		{"docs/c.png", false, true},
		{"sub/x.tmp", false, true},
		{"other/x.tmp", false, false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, c.dir); got != c.match {
			t.Errorf("Match(%q, dir=%v) = %v, want %v", c.path, c.dir, got, c.match)
		}
	}
	if !m.MatchPath("node_modules/react/index.js") {
		t.Error("files inside an ignored folder must be ignored")
	}
}

func TestCompressTS(t *testing.T) {
	src := "export function a(x: number): number {\n  if (x) { return 1 }\n  return 2\n}\nclass B {\n  m() { const s = \"}\"; }\n}\n"
	out, ok := compressCode(src, "typescript")
	if !ok || strings.Contains(out, "return 2") || !strings.Contains(out, "class B") || !strings.Contains(out, "m() {") {
		t.Fatalf("bad compression:\n%s", out)
	}
}

func TestCompressGo(t *testing.T) {
	src := "package x\n\n// Doc\nfunc A() int {\n\treturn 1\n}\n\ntype T struct{ X int }\n"
	out, ok := compressCode(src, "go")
	if !ok || strings.Contains(out, "return 1") || !strings.Contains(out, "type T struct") || !strings.Contains(out, "// Doc") {
		t.Fatalf("bad compression:\n%s", out)
	}
}

func TestCompressPython(t *testing.T) {
	src := "def f(a,\n      b):\n    \"\"\"Doc.\"\"\"\n    x = 1\n    return x\n\ndef g(): return 2\n\nclass C:\n    def m(self):\n        pass\n"
	out := compressPython(src)
	for _, want := range []string{"def f(a,", "\"\"\"Doc.\"\"\"", "def g(): return 2", "class C:", "def m(self):"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "x = 1") || strings.Contains(out, "pass") {
		t.Errorf("body not removed:\n%s", out)
	}
}

func TestRemoveComments(t *testing.T) {
	src := "const url = \"http://x.com\"; // note\n/* block\n comment */\nlet y = 'a // b';\n"
	out, _ := removeComments(src, "javascript")
	if strings.Contains(out, "note") || strings.Contains(out, "block") || !strings.Contains(out, "http://x.com") || !strings.Contains(out, "'a // b'") {
		t.Fatalf("bad comment removal:\n%s", out)
	}
	py, _ := removeComments("x = '#not'  # yes\n", "python")
	if strings.Contains(py, "yes") || !strings.Contains(py, "'#not'") {
		t.Fatalf("bad python comment removal: %q", py)
	}
}

func TestSecrets(t *testing.T) {
	src := `key = "sk-ant-api03-abcdefghijklmnopqrstuvwx"
db = "postgres://user:hunter22@localhost/db"
password: "changeme"`
	out, found := redactSecrets("config.py", src)
	if strings.Contains(out, "abcdefghij") || strings.Contains(out, "hunter22") {
		t.Fatalf("secret left in output:\n%s", out)
	}
	if !strings.Contains(out, "changeme") {
		t.Error("placeholder values should stay")
	}
	if len(found) < 2 {
		t.Errorf("expected at least 2 kinds, got %v", found)
	}
	env, _ := redactSecrets(".env", "API_KEY=abc123\nPORT=3000\n")
	if strings.Contains(env, "abc123") || !strings.Contains(env, "PORT=3000") {
		t.Errorf("bad env redaction: %q", env)
	}
}

func TestParseChanges(t *testing.T) {
	xml := "text\n<file path=\"a/b.go\">\npackage b\n</file>\n<delete path=\"old.txt\"/>"
	ch := parseChanges(xml)
	if len(ch) != 2 {
		t.Fatalf("want 2 changes, got %d", len(ch))
	}
	md := "### `src/x.ts`\n```ts\nexport const x = 1\n```\n\n```go:cmd/main.go\npackage main\n```\n\n```bash\nnpm i\n```\n"
	ch = parseChanges(md)
	if len(ch) != 2 || ch[0].Path != "src/x.ts" || ch[1].Path != "cmd/main.go" {
		t.Fatalf("bad markdown parse: %+v", ch)
	}
	if _, err := safeRel("../x"); err == nil {
		t.Error("path traversal must be rejected")
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"50k": 50000, "1.5m": 1500000, "1MB": 1000000, "200": 200} {
		if got, _ := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestNoiseFiles(t *testing.T) {
	m := &Matcher{}
	m.AddLines(noiseFiles, "")
	for _, p := range []string{".gitignore", "sub/.gitignore", "LICENSE", "CHANGELOG.md", ".vscode/settings.json", ".prettierrc.json", "client/src/logo.svg", "server/uploads/011b94"} {
		if !m.MatchPath(p) {
			t.Errorf("%s should be noise", p)
		}
	}
	for _, p := range []string{"src/license.go", "README.md", ".github/workflows/ci.yml", "package.json"} {
		if m.MatchPath(p) {
			t.Errorf("%s should not be noise", p)
		}
	}
}

func TestScopes(t *testing.T) {
	sc := []string{"src/api"}
	cases := []struct {
		rel  string
		dir  bool
		want bool
	}{
		{"src", true, true},
		{"src/api", true, true},
		{"src/api/user.go", false, true},
		{"src/web", true, false},
		{"src/apix/a.go", false, false},
		{"README.md", false, false},
	}
	for _, c := range cases {
		if got := inScope(c.rel, c.dir, sc); got != c.want {
			t.Errorf("inScope(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
	if !inScope("anything", false, nil) {
		t.Error("no scopes must mean everything")
	}
}

func TestJSONOutput(t *testing.T) {
	files := []*File{
		{Path: "a.go", Lang: "go", Content: "package a\n// \"quote\" <tag>\n", Tokens: 5},
		{Path: "b.go", Mode: ModeOmitted},
	}
	in := RenderInput{Project: "demo", Files: files, Format: "json", Prompt: "explain"}
	out := Render(in, buildTree(files))
	var got struct {
		Project      string   `json:"project"`
		Omitted      []string `json:"omitted_files"`
		Instructions string   `json:"instructions"`
		Files        []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Project != "demo" || len(got.Files) != 1 || got.Files[0].Path != "a.go" ||
		!strings.Contains(got.Files[0].Content, `"quote" <tag>`) || len(got.Omitted) != 1 || got.Instructions != "explain" {
		t.Fatalf("unexpected JSON: %+v", got)
	}
}

func TestTokenCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir) // Linux
	t.Setenv("LocalAppData", dir)   // Windows
	t.Setenv("HOME", dir)           // macOS uses ~/Library/Caches
	defer func() { tokenCache.enabled = false }()

	text := strings.Repeat("func main() { fmt.Println(\"hello\") }\n", 20)
	want := countTokens(text)
	openTokenCache("/some/project")
	if got := countTokensCached(text); got != want {
		t.Fatalf("cached count %d, want %d", got, want)
	}
	saveTokenCache()

	openTokenCache("/some/project")
	if len(tokenCache.old) != 1 {
		t.Fatalf("cache was not saved and loaded back: %v", tokenCache.old)
	}
	if got := countTokensCached(text); got != want {
		t.Fatalf("count from disk %d, want %d", got, want)
	}
}

func TestSecretsSkipCleanFiles(t *testing.T) {
	src := "const total = items.reduce((a, b) => a + b, 0);\n"
	out, found := redactSecrets("a.js", src)
	if out != src || len(found) != 0 {
		t.Fatalf("clean file was changed: %q %v", out, found)
	}
}
