# ctxpack

Pack your whole codebase into one AI-ready file, then apply the AI's answer back to your files.
One small binary, no Node or Python needed.

## Install

Download the binary for your system from `dist/` (or GitHub Releases), put it somewhere on your PATH, done.

Or build it yourself (Go 1.21+):

```bash
go build -o ctxpack .
# or: go install github.com/sahbadal/ctxpack@latest
```

## Quick start

```bash
ctxpack                        # pack current folder -> ctxpack-output.xml
ctxpack --copy                 # ...and copy to clipboard
ctxpack ui                     # pick files visually in your browser
```

Paste into ChatGPT / Claude / Gemini, then:

```bash
ctxpack apply                  # apply the AI's answer from the clipboard
ctxpack undo                   # changed your mind? restore everything
```

## Everyday recipes

| Goal                                 | Command                                                      |
| ------------------------------------ | ------------------------------------------------------------ |
| Make it smaller (signatures only)    | `ctxpack -c`                                                 |
| Fit into a token limit automatically | `ctxpack --budget 100k`                                      |
| Only a feature and what it imports   | `ctxpack --entry src/app.ts`                                 |
| Only what I changed                  | `ctxpack --diff`                                             |
| Review my branch                     | `ctxpack --since main --with-diff -p review`                 |
| Ask for a fix the tool can apply     | `ctxpack -p bugs` or `ctxpack --ask "add dark mode" --edits` |
| Pack a GitHub repo                   | `ctxpack facebook/react -c`                                  |
| Huge repo, many prompts              | `ctxpack --split 100k`                                       |
| Markdown output                      | `ctxpack -f md`                                              |

Run `ctxpack help` for every flag and `ctxpack prompts` for the prompt templates.

## What makes it different

- **Apply + undo**: AI answers written as `<file path="...">` blocks or as code blocks with a file name are written back to disk. Every touched file is backed up in `.ctxpack/backups`. Files that look incomplete (`// ... existing code ...`) are skipped unless you pass `--force`. Paths outside the project are refused.
- **Token budget**: compresses the least important files first, then shortens big data files, then omits files, then refills any space left.
- **Import following** for JS/TS, Python, Go, Rust, Java/Kotlin, Dart, C/C++, PHP.
- **Git aware**: `--diff`, `--since REF`, `--with-diff`, `--log N`.
- **Secrets redacted, not dropped**: keys and passwords become `[REDACTED:type]`; `.env` keeps variable names and harmless values like `PORT=3000`.
- **Zero config**: detects Node, Python, Go, Rust, Flutter, Java, PHP, Ruby, .NET, Terraform and ignores their build folders.
- **Browser UI** (`ctxpack ui`): file tree with token heat bars, context-window gauge, one-click pack and copy, and an apply screen with preview.

## Which format should I use?

| Format        | Flag      | Best for                                                                          |
| ------------- | --------- | --------------------------------------------------------------------------------- |
| XML (default) | `-f xml`  | Claude, and a safe choice for any AI. Clear `<file path="...">` boundaries.       |
| Markdown      | `-f md`   | ChatGPT, Gemini, Copilot chat, and reading it yourself.                           |
| JSON          | `-f json` | Scripts, APIs and your own tools. Uses about 20% more tokens because of escaping. |
| Plain text    | `-f txt`  | Very simple tools or models that get confused by markup.                          |

Not sure? Keep the default XML.

## Ignoring files

Rules are read in this order: built-in defaults, project presets, `.ctxpackignore`, `.gitignore` (nested too), then `--ignore`. All use gitignore syntax.

## Notes

- Token counts use OpenAI's `o200k_base` tokenizer (`--tokenizer cl100k_base` also works). Other models count a bit differently, so treat numbers as close estimates.
- Compression is exact for Go (uses Go's own parser) and a fast heuristic for other languages.
- On Linux, clipboard needs `wl-clipboard`, `xclip` or `xsel`.

## Development

```bash
go test ./...
make dist        # builds binaries for Windows, macOS and Linux into dist/
```

## License

MIT, see `LICENSE`. Third-party licenses are in `THIRD_PARTY_NOTICES.md`.
