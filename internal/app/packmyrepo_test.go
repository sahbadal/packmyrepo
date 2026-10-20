package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Fake secrets are glued together at run time, so this file itself does not
	// look like it leaks a key (to packmyrepo, GitHub or any other scanner).
	fakeKey := "sk-" + "ant-api03-" + "abcdefghijklmnopqrstuvwx"
	fakeURL := "postgres://user:" + "hunter22" + "@localhost/db"
	src := "key = \"" + fakeKey + "\"\ndb = \"" + fakeURL + "\"\npassword: \"changeme\""
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

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"format": "md", "compress": true, "budget": "50k", "ignore": ["docs/**"], "security": false}`
	if err := os.WriteFile(filepath.Join(dir, configName), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _, err := loadConfig(dir)
	if err != nil || c == nil {
		t.Fatalf("load: %v", err)
	}
	o := defaultOptions()
	o.Format = "json"             // typed by the user
	o.Ignore = []string{"tmp/**"} // typed by the user
	c.apply(&o, map[string]bool{"format": true})
	if o.Format != "json" {
		t.Errorf("flag must win over config, got %q", o.Format)
	}
	if !o.Compress || o.Budget != 50000 || !o.NoSecrets {
		t.Errorf("config values not applied: %+v", o)
	}
	if len(o.Ignore) != 2 {
		t.Errorf("ignore lists should merge, got %v", o.Ignore)
	}

	os.WriteFile(filepath.Join(dir, configName), []byte(`{"formatt": "md"}`), 0o644)
	if _, _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "unknown setting") {
		t.Errorf("typo must be reported, got %v", err)
	}
	if c, _, err := loadConfig(t.TempDir()); c != nil || err != nil {
		t.Errorf("missing config must be fine, got %v %v", c, err)
	}
}

func TestRemoteURLvsLocalPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(dir)

	// "src" exists here, so "src/missing" is a mistyped local path, not GitHub.
	if _, ok := remoteURL("src/missing"); ok {
		t.Error("src/missing must be treated as a local path")
	}
	if _, _, err := resolveTargets([]string{"src/missing"}); err == nil || !strings.Contains(err.Error(), "path not found") {
		t.Errorf("want a path-not-found error, got %v", err)
	}
	// Nothing called "facebook" exists here, so this is a GitHub shorthand.
	if url, ok := remoteURL("facebook/react"); !ok || url != "https://github.com/facebook/react" {
		t.Errorf("facebook/react must be a GitHub repo, got %q %v", url, ok)
	}
	if url, ok := remoteURL("https://github.com/a/b"); !ok || url != "https://github.com/a/b" {
		t.Errorf("full links must stay remote, got %q", url)
	}
	if _, ok := remoteURL("./src/missing"); ok {
		t.Error("paths starting with . are always local")
	}
}

func TestPriorityOf(t *testing.T) {
	nextReadme := "This is a [Next.js](https://nextjs.org) project bootstrapped with [`create-next-app`](https://x)."
	order := []struct {
		path, content string
	}{
		{"README.md", "# My real project\nWhat it does."},
		{"package.json", ""},
		{"src/app/(site)/page.tsx", ""},
		{"src/app/api/contact/route.ts", ""},
		{"src/lib/utils.ts", ""},
		{"tsconfig.json", ""},
		{"src/components/Hero.module.css", ""},
		{"src/lib/utils.test.ts", ""},
		{"eslint.config.mjs", ""},
		{"src/__mocks__/api.ts", ""},
	}
	prev := 1000
	for _, c := range order {
		got := priorityOf(c.path, c.content)
		if got > prev {
			t.Errorf("%s scored %d, should not beat the file above it (%d)", c.path, got, prev)
		}
		prev = got
	}
	if priorityOf("README.md", nextReadme) >= priorityOf("src/lib/utils.ts", "") {
		t.Error("an untouched create-next-app README must rank below real code")
	}
	if priorityOf("src/app/(site)/page.tsx", "") <= priorityOf("src/components/sections/Hero.tsx", "") {
		t.Error("Next.js pages must rank above ordinary components")
	}
}

func TestIsPackOutput(t *testing.T) {
	yes := []string{
		"<codebase name=\"web\" packed_by=\"packmyrepo\">\nThis is the whole codebase",
		"{\n  \"project\": \"web\",\n  \"packed_by\": \"packmyrepo 0.1.1\",",
		"# web (packed by packmyrepo)\n\nThis file contains",
		"================================================================\nweb (packed by packmyrepo)\n",
	}
	for _, s := range yes {
		if !isPackOutput([]byte(s)) {
			t.Errorf("should be recognised as an earlier output: %q", s[:30])
		}
	}
	no := []string{
		"package app\n// packed_by=\"packmyrepo\" appears in tests",
		"<codebase>\n</codebase>",
		"# My project\nNot packed by anything",
		"{\"name\": \"web\"}",
	}
	for _, s := range no {
		if isPackOutput([]byte(s)) {
			t.Errorf("must not be treated as an output: %q", s)
		}
	}
}

func TestMatchFileStyle(t *testing.T) {
	cases := []struct {
		name, content, old string
		existed            bool
		want               string
	}{
		{"keeps a missing final newline", "a\nb", "a\nc", true, "a\nb"},
		{"keeps a final newline", "a\nb", "a\nc\n", true, "a\nb\n"},
		{"keeps Windows line endings", "a\nb\n", "a\r\nc\r\n", true, "a\r\nb\r\n"},
		{"new files end with one newline", "a\nb\n\n\n", "", false, "a\nb\n"},
	}
	for _, c := range cases {
		if got := matchFileStyle(c.content, []byte(c.old), c.existed); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestUnifiedDiff(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\n"
	new := "a\nb\nc\nX\ne\nf\ng\nh\n"
	d := unifiedDiff(old, new, 1)
	want := []string{"@@ -3,3 +3,3 @@", " c", "-d", "+X", " e"}
	if strings.Join(d, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", d, want)
	}
	if unifiedDiff("same\n", "same", 3) != nil {
		t.Error("equal texts must give no diff")
	}
}

func TestChangesFromTextTo(t *testing.T) {
	// only the code was copied
	ch, err := changesFromText("export const x = 1;\n", "src/x.ts")
	if err != nil || len(ch) != 1 || ch[0].Path != "src/x.ts" || ch[0].Content != "export const x = 1;" {
		t.Fatalf("code only: %+v %v", ch, err)
	}
	// a code fence around it is removed
	ch, _ = changesFromText("```ts\nexport const y = 2;\n```", "src/y.ts")
	if len(ch) != 1 || ch[0].Content != "export const y = 2;" {
		t.Fatalf("fenced: %+v", ch)
	}
	// the <file> lines were copied after all
	ch, _ = changesFromText("<file path=\"other.ts\">\nconst z = 3;\n</file>", "src/z.ts")
	if len(ch) != 1 || ch[0].Path != "src/z.ts" || ch[0].Content != "const z = 3;" {
		t.Fatalf("with file block: %+v", ch)
	}
	if _, err := changesFromText("just some text", ""); err == nil || !strings.Contains(err.Error(), "--to") {
		t.Errorf("the error should mention --to, got %v", err)
	}
}

func TestLooksLikeWrongText(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	old := b.String()
	if looksLikeWrongText(old, "pmrn apply --to src/lib/utils.ts") == "" {
		t.Error("a one-line command replacing a 40-line file must be flagged")
	}
	if looksLikeWrongText(old, old+"one more line\n") != "" {
		t.Error("adding a line is a normal edit")
	}
	half := strings.Join(strings.Split(old, "\n")[:15], "\n")
	if looksLikeWrongText(old, half) != "" {
		t.Error("keeping 15 of 40 lines is a real (if big) edit, not a wrong paste")
	}
	if looksLikeWrongText("a\nb\n", "x") != "" {
		t.Error("tiny files are never flagged")
	}
}

func TestSplitPartsStayUnderLimit(t *testing.T) {
	var files []*File
	for i := 0; i < 200; i++ {
		body := strings.Repeat("x := 1\n", 40)
		files = append(files, &File{
			Path: fmt.Sprintf("pkg/file%03d.go", i), Lang: "go",
			Raw: body, Content: body, Depth: -1,
		})
	}
	countAll(files)
	const limit = 4000
	parts, _ := renderAll(RenderInput{Project: "t", Files: files, Format: "xml"}, limit)
	if len(parts) < 2 {
		t.Fatalf("expected several parts, got %d", len(parts))
	}
	for i, p := range parts {
		if n := countTokens(p); n > limit {
			t.Errorf("part %d has %d tokens, limit is %d", i+1, n, limit)
		}
	}
}
