package main

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// langByExt maps a file extension to a markdown fence language.
var langByExt = map[string]string{
	".go": "go", ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".jsx": "jsx", ".ts": "typescript", ".tsx": "tsx", ".mts": "typescript", ".cts": "typescript",
	".py": "python", ".pyi": "python", ".rb": "ruby", ".rs": "rust", ".java": "java",
	".kt": "kotlin", ".kts": "kotlin", ".scala": "scala", ".swift": "swift",
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp",
	".cs": "csharp", ".php": "php", ".dart": "dart", ".lua": "lua", ".r": "r",
	".sh": "bash", ".bash": "bash", ".zsh": "bash", ".ps1": "powershell",
	".sql": "sql", ".html": "html", ".htm": "html", ".xml": "xml", ".svg": "xml",
	".css": "css", ".scss": "scss", ".sass": "sass", ".less": "less",
	".vue": "vue", ".svelte": "svelte", ".astro": "astro",
	".json": "json", ".jsonc": "jsonc", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml",
	".ini": "ini", ".cfg": "ini", ".env": "dotenv",
	".md": "markdown", ".mdx": "mdx", ".rst": "rst", ".txt": "text",
	".gradle": "groovy", ".groovy": "groovy", ".tf": "hcl", ".proto": "protobuf",
	".graphql": "graphql", ".gql": "graphql", ".ex": "elixir", ".exs": "elixir",
	".erl": "erlang", ".hs": "haskell", ".ml": "ocaml", ".clj": "clojure", ".zig": "zig",
	".nim": "nim", ".pl": "perl", ".m": "objectivec", ".mm": "objectivec", ".csv": "csv",
}

var langByName = map[string]string{
	"Dockerfile": "dockerfile", "Makefile": "makefile", "makefile": "makefile",
	"CMakeLists.txt": "cmake", "Gemfile": "ruby", "Rakefile": "ruby", "Jenkinsfile": "groovy",
}

func langOf(rel string) string {
	base := path.Base(rel)
	if l, ok := langByName[base]; ok {
		return l
	}
	if strings.HasPrefix(base, ".env") {
		return "dotenv"
	}
	if strings.HasPrefix(base, "Dockerfile") {
		return "dockerfile"
	}
	return langByExt[strings.ToLower(path.Ext(base))]
}

// Comment/compression families.
const (
	famNone = iota
	famCLike
	famHash
	famPython
	famGo
	famRust
	famCSS
	famHTML
	famSQL
)

func familyOf(lang string) int {
	switch lang {
	case "go":
		return famGo
	case "python":
		return famPython
	case "rust":
		return famRust
	case "javascript", "jsx", "typescript", "tsx", "java", "kotlin", "scala", "swift",
		"c", "cpp", "csharp", "php", "dart", "groovy", "protobuf", "jsonc", "scss", "less", "objectivec", "zig":
		return famCLike
	case "ruby", "bash", "powershell", "yaml", "toml", "r", "perl", "dockerfile", "makefile",
		"cmake", "elixir", "nim", "dotenv", "ini", "graphql", "hcl":
		return famHash
	case "css":
		return famCSS
	case "html", "xml", "vue", "svelte", "astro":
		return famHTML
	case "sql", "lua", "haskell":
		return famSQL
	}
	return famNone
}

var binaryExts = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`.png .jpg .jpeg .gif .bmp .ico .webp .avif .tif .tiff .psd .heic
	.mp3 .mp4 .wav .ogg .flac .avi .mov .mkv .webm .m4a
	.zip .tar .gz .tgz .bz2 .xz .7z .rar .jar .war .ear .apk .aab .ipa .dmg .iso
	.exe .dll .so .dylib .a .lib .o .obj .bin .class .pyc .pyo .wasm .node
	.pdf .doc .docx .xls .xlsx .ppt .pptx .odt
	.ttf .otf .woff .woff2 .eot .sqlite .db .sqlite3 .pkl .npy .npz .h5 .onnx .pt .ckpt .safetensors`) {
		binaryExts[e] = true
	}
}

// looksBinary checks the extension and the first bytes of the content.
func looksBinary(rel string, data []byte) bool {
	if binaryExts[strings.ToLower(path.Ext(rel))] {
		return true
	}
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	// allow a cut multi-byte rune at the end of the sample
	for i := 0; i < 4 && len(head) > 0 && !utf8.Valid(head); i++ {
		head = head[:len(head)-1]
	}
	return !utf8.Valid(head)
}

// preset is a set of extra ignore rules for a project type, detected by marker files.
type preset struct {
	name    string
	markers []string
	ignores []string
}

var presets = []preset{
	{"node", []string{"package.json"}, []string{".next/", ".nuxt/", ".output/", "out/", ".turbo/", ".svelte-kit/", ".parcel-cache/", ".vercel/", ".expo/", "storybook-static/"}},
	{"python", []string{"pyproject.toml", "requirements.txt", "setup.py", "Pipfile"}, []string{"__pycache__/", "*.pyc", ".venv/", "venv/", "env/", ".pytest_cache/", ".mypy_cache/", ".ruff_cache/", "*.egg-info/", ".tox/", "htmlcov/", ".ipynb_checkpoints/"}},
	{"go", []string{"go.mod"}, []string{"vendor/", "bin/"}},
	{"rust", []string{"Cargo.toml"}, []string{"target/"}},
	{"flutter", []string{"pubspec.yaml"}, []string{".dart_tool/", "build/", ".flutter-plugins", ".flutter-plugins-dependencies", "*.g.dart", "*.freezed.dart", "ios/Pods/", "android/.gradle/"}},
	{"java", []string{"pom.xml", "build.gradle", "build.gradle.kts"}, []string{"target/", "build/", ".gradle/", "*.class", "out/"}},
	{"php", []string{"composer.json"}, []string{"vendor/"}},
	{"ruby", []string{"Gemfile"}, []string{"vendor/bundle/", ".bundle/", "tmp/", "log/"}},
	{"dotnet", []string{"*.csproj", "*.sln"}, []string{"bin/", "obj/", "*.user"}},
	{"terraform", []string{"main.tf"}, []string{".terraform/", "*.tfstate", "*.tfstate.*"}},
}

// detectPresets returns the presets whose marker files exist in root.
func detectPresets(root string) []preset {
	var out []preset
	for _, p := range presets {
		for _, mk := range p.markers {
			if strings.Contains(mk, "*") {
				if m, _ := filepath.Glob(filepath.Join(root, mk)); len(m) > 0 {
					out = append(out, p)
					break
				}
			} else if _, err := os.Stat(filepath.Join(root, mk)); err == nil {
				out = append(out, p)
				break
			}
		}
	}
	return out
}
