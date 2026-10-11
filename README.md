<h1 align="center">packmyrepo</h1>

<p align="center">
  <b>Ship your whole codebase to any AI in one file. Then apply its answer back to your code.</b>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/packmyrepo"><img alt="npm" src="https://img.shields.io/npm/v/packmyrepo?color=F5B700&label=npm"></a>
  <a href="https://github.com/sahbadal/packmyrepo/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/sahbadal/packmyrepo/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/badge/license-MIT-blue"></a>
</p>

---

ChatGPT, Claude and Gemini give much better answers when they can see your **whole project**, not one pasted file.
packmyrepo turns a project into a single, clean, AI-ready file in milliseconds, keeps your secrets out of it,
and when the AI answers with changed files, writes them back for you with a backup.

```bash
npx packmyrepo --copy
```

That's it. Your project is now on the clipboard. Paste it into any AI chat.
![packmyrepo ui](docs/screenshot.png)

## How it works

1. **Pack**: `packmyrepo` reads your project, skips junk (`node_modules`, builds, lock files, images), hides secrets, and writes one file.
2. **Ask**: paste it into ChatGPT, Claude or Gemini with your question.
3. **Apply**: copy the AI's reply and run `packmyrepo apply`. Changed files are written back, and `packmyrepo undo` restores everything.

Prefer clicking? `packmyrepo ui` opens a local page where you tick files, watch the token count and pack with one button.

## Install

Pick whichever is easiest for you. All of them install the same small native program.

| You have      | Run                                                                                               |
| ------------- | ------------------------------------------------------------------------------------------------- |
| Node.js       | `npx packmyrepo` (no install) or `npm install -g packmyrepo`                                      |
| Windows       | `irm https://raw.githubusercontent.com/sahbadal/packmyrepo/main/install.ps1 \| iex` in PowerShell |
| macOS / Linux | `curl -fsSL https://raw.githubusercontent.com/sahbadal/packmyrepo/main/install.sh \| sh`          |
| Go            | `go install github.com/sahbadal/packmyrepo@latest`                                                |

Or download a zip from the [Releases page](https://github.com/sahbadal/packmyrepo/releases). The installers check every download against its SHA-256 checksum before installing.
Tip: `npx` can keep using an old cached copy. Run `npx packmyrepo@latest` to be sure you get the newest version.

## Quick start

```bash
packmyrepo                 # pack this folder -> packmyrepo-output.xml
packmyrepo --copy          # ...and copy it to the clipboard
packmyrepo src/api         # only one folder (paths stay correct)
packmyrepo ui              # choose files in your browser
packmyrepo apply           # write the AI's answer back (reads the clipboard)
packmyrepo undo            # undo the last apply
```

## Everyday recipes

| I want to…                                              | Command                                                            |
| ------------------------------------------------------- | ------------------------------------------------------------------ |
| Make it smaller (keep signatures, drop function bodies) | `packmyrepo -c`                                                    |
| Fit a token limit automatically                         | `packmyrepo --budget 100k`                                         |
| Send one feature and everything it imports              | `packmyrepo --entry src/app.ts`                                    |
| Send only what I changed                                | `packmyrepo --diff`                                                |
| Get my branch reviewed                                  | `packmyrepo --since main --with-diff -p review`                    |
| Get fixes I can apply in one command                    | `packmyrepo -p bugs` or `packmyrepo --ask "add dark mode" --edits` |
| Pack a GitHub repo without cloning it myself            | `packmyrepo facebook/react -c`                                     |
| Split a huge repo into several prompts                  | `packmyrepo --split 100k`                                          |
| Get Markdown instead of XML                             | `packmyrepo -f md`                                                 |

Run `packmyrepo help` for every option and `packmyrepo prompts` for the ready-made prompts
(review, bugs, explain, docs, tests, refactor, feature, security).

## Why packmyrepo

- **Apply and undo.** The AI's answer goes straight back into your files. Every changed file is backed up first, files that look incomplete (`// ... existing code ...`) are skipped, and paths outside your project are refused.
- **Smart token budget.** Give a limit and packmyrepo fits it: it compresses the least important files first, then shortens large data files, then leaves out what is left, and refills any space it freed.
- **Follows imports.** Start from one file and get exactly the code it depends on, for JS/TS, Python, Go, Rust, Java/Kotlin, Dart, C/C++ and PHP.
- **Git aware.** Pack only changed files, everything since a branch, and the diff itself.
- **Secrets hidden, not dropped.** API keys, tokens and passwords become `[REDACTED:type]`. `.env` files keep their variable names, so the AI still knows which settings exist.
- **Zero config, less noise.** Detects Node, Python, Go, Rust, Flutter, Java, PHP, Ruby, .NET and Terraform projects and skips their build folders, plus files AI never needs (`.gitignore`, `LICENSE`, `*.svg`, upload folders).
- **Fast.** A native binary that reads files on every CPU core and remembers token counts between runs, so the second pack of a big repo takes a few seconds.

## Which format should I use?

| Format        | Flag      | Best for                                                                    |
| ------------- | --------- | --------------------------------------------------------------------------- |
| XML (default) | `-f xml`  | Claude, and a safe choice for any AI. Clear `<file path="...">` boundaries. |
| Markdown      | `-f md`   | ChatGPT, Gemini, Copilot chat, and reading it yourself.                     |
| JSON          | `-f json` | Scripts and your own tools. Uses about 20% more tokens because of escaping. |
| Plain text    | `-f txt`  | Very simple tools.                                                          |

Not sure? Keep the default.

## Project settings

Run `packmyrepo init` once to create `packmyrepo.json`, then just run `packmyrepo`:

```json
{
  "format": "md",
  "compress": true,
  "budget": "100k",
  "ignore": ["tests/**"],
  "copy": true
}
```

Flags you type always win over the file, and `ignore` lists are combined. A typo in a setting name is reported instead of silently ignored.

## Choosing files

Rules are applied in this order, all in `.gitignore` syntax: built-in defaults, project presets, `.packmyrepoignore`,
`.gitignore` (nested ones too), then `--ignore`. Use `--include` to pack only matching files and `--keep-noise` to bring back config files.

## Privacy

packmyrepo runs entirely on your computer. It does not upload your code or collect usage data.
`packmyrepo ui` listens only on `127.0.0.1` and refuses requests from other websites.

## Notes

- Token counts use OpenAI's `o200k_base` tokenizer (`--tokenizer cl100k_base` also works). Other models count slightly differently, so treat numbers as close estimates.
- Compression is exact for Go (it uses Go's own parser) and a fast heuristic for other languages.
- On Linux, clipboard support needs `wl-clipboard`, `xclip` or `xsel`.

## Development

```bash
go test ./...                    # run the tests
go run . --help                  # run from source
make npm VERSION=0.1.0           # build the npm packages locally (nothing is published)
```

Releases are automatic: pushing a tag like `v0.2.0` builds binaries for every platform, creates a GitHub Release and publishes to npm.

Issues and pull requests are welcome.

## License

MIT, see [LICENSE](LICENSE). Third-party licenses are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
