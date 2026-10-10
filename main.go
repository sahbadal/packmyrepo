package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]
	cmd := "pack"
	if len(args) > 0 {
		switch args[0] {
		case "pack", "serve", "ui", "apply", "undo", "version", "help", "prompts":
			cmd, args = args[0], args[1:]
		case "-h", "--help", "-help":
			cmd = "help"
		case "-v", "--version", "-version":
			cmd = "version"
		}
	}
	var err error
	switch cmd {
	case "pack":
		err = runPack(args)
	case "serve", "ui":
		err = runServe(args)
	case "apply":
		err = runApply(args)
	case "undo":
		err = runApply(append([]string{"--undo"}, args...))
	case "prompts":
		for _, n := range promptNames() {
			fmt.Printf("  %-10s %s\n", n, promptTemplates[n].desc)
		}
	case "version":
		fmt.Println("ctxpack", version, runtime.GOOS+"/"+runtime.GOARCH)
	case "help":
		fmt.Print(helpText)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
		os.Exit(1)
	}
}

const helpText = `ctxpack - pack your codebase into one AI-ready file, and apply the AI's answer back.

USAGE
  ctxpack [path|git-url] [flags]     pack a project (default: current folder)
  ctxpack ui [path]                  open the visual file picker in your browser
  ctxpack apply [file|-]             write files from an AI response into the project
  ctxpack undo                       undo the last apply
  ctxpack prompts                    list built-in prompt templates

COMMON
  ctxpack                            pack everything -> ctxpack-output.xml
  ctxpack --copy                     ...and copy it to the clipboard
  ctxpack -c                         compress: keep signatures, drop function bodies
  ctxpack --budget 100k              fit into 100k tokens automatically
  ctxpack --entry src/app.ts         only this file and what it imports
  ctxpack --diff                     only files you changed (vs HEAD)
  ctxpack --since main --with-diff   files changed since main, plus the diff
  ctxpack -p review                  add a ready-made prompt (see 'ctxpack prompts')
  ctxpack user/repo                  pack a GitHub repo (shallow clone)

PACK FLAGS
  -o, --output FILE        output file (default ctxpack-output.<ext>)
  -f, --format FMT         xml | md | json | txt (default xml)
      --include GLOBS      only these (comma separated, gitignore syntax)
      --ignore GLOBS       skip these too
  -c, --compress           keep code structure, drop function bodies
      --no-comments        remove comments
      --budget N           token budget, e.g. 50000, 50k, 1m
      --split N            split into parts of at most N tokens
      --entry FILES        start from these files and follow imports
      --depth N            how deep to follow imports (0 = all)
      --diff               only changed + untracked files (vs HEAD)
      --since REF          only files changed since REF (branch, tag, commit)
      --with-diff          include the git diff text
      --log N              include the last N commit messages
  -p, --prompt NAME|TEXT   template name or your own instruction
      --ask TEXT           extra request added to the prompt
      --edits              ask the AI to answer in the 'ctxpack apply' format
      --copy               copy the result to the clipboard
      --stdout             print the result instead of writing a file
      --top N              show the N biggest files (default 5)
      --line-numbers       prefix lines with numbers
      --max-size SIZE      skip files bigger than this (default 1MB)
      --no-gitignore       do not read .gitignore files
	  --keep-noise         keep config files like .gitignore, LICENSE, .prettierrc
      --no-default-ignore  do not use built-in ignore rules and presets
      --no-security        do not redact secrets
      --tokenizer NAME     o200k_base (default) | cl100k_base
      --branch NAME        branch for remote repos
  -q, --quiet              print nothing except errors

APPLY FLAGS
      --clipboard          read the AI response from the clipboard
      --dir PATH           project folder (default .)
  -y, --yes                apply without asking
      --dry-run            only show what would change
      --force              also write files that look incomplete
      --undo               restore the last backup

UI FLAGS
      --port N             port (default 7878)
      --no-open            do not open the browser
`

// ---------- flag helpers ----------

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

// sizeFlag accepts 50000, 50k, 1.5m (tokens) or 1MB / 500KB (bytes).
type sizeFlag struct{ v *int64 }

func (s sizeFlag) String() string {
	if s.v == nil {
		return "0"
	}
	return strconv.FormatInt(*s.v, 10)
}
func (s sizeFlag) Set(v string) error {
	n, err := parseSize(v)
	if err != nil {
		return err
	}
	*s.v = n
	return nil
}

func parseSize(v string) (int64, error) {
	t := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(v, "_", "")))
	t = strings.TrimSuffix(strings.TrimSuffix(t, "b"), "i")
	mult := 1.0
	switch {
	case strings.HasSuffix(t, "k"):
		mult, t = 1000, t[:len(t)-1]
	case strings.HasSuffix(t, "m"):
		mult, t = 1_000_000, t[:len(t)-1]
	case strings.HasSuffix(t, "g"):
		mult, t = 1_000_000_000, t[:len(t)-1]
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("invalid size %q", v)
	}
	return int64(f * mult), nil
}

// reorder lets flags appear after positional args: `ctxpack . -c`.
func reorder(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue
			}
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	err := fs.Parse(reorder(fs, args))
	if errors.Is(err, flag.ErrHelp) {
		fmt.Print(helpText)
		os.Exit(0)
	}
	if err != nil {
		return fmt.Errorf("%v (run 'ctxpack help')", err)
	}
	return nil
}

// ---------- pack ----------

func runPack(args []string) error {
	o := defaultOptions()
	fs := newFlagSet("pack")
	var include, ignore, entries listFlag
	var budget, split int64
	var logN int
	var branch string
	fs.StringVar(&o.Output, "o", "", "")
	fs.StringVar(&o.Output, "output", "", "")
	fs.StringVar(&o.Format, "f", "xml", "")
	fs.StringVar(&o.Format, "format", "xml", "")
	fs.Var(&include, "include", "")
	fs.Var(&ignore, "ignore", "")
	fs.BoolVar(&o.Compress, "c", false, "")
	fs.BoolVar(&o.Compress, "compress", false, "")
	fs.BoolVar(&o.RemoveComments, "no-comments", false, "")
	fs.Var(sizeFlag{&budget}, "budget", "")
	fs.Var(sizeFlag{&split}, "split", "")
	fs.Var(&entries, "entry", "")
	fs.IntVar(&o.Depth, "depth", 0, "")
	fs.BoolVar(&o.Diff, "diff", false, "")
	fs.StringVar(&o.Since, "since", "", "")
	fs.BoolVar(&o.WithDiff, "with-diff", false, "")
	fs.IntVar(&logN, "log", 0, "")
	fs.StringVar(&o.Prompt, "p", "", "")
	fs.StringVar(&o.Prompt, "prompt", "", "")
	fs.StringVar(&o.Ask, "ask", "", "")
	fs.BoolVar(&o.EditFormat, "edits", false, "")
	fs.BoolVar(&o.Copy, "copy", false, "")
	fs.BoolVar(&o.Stdout, "stdout", false, "")
	fs.IntVar(&o.Top, "top", 5, "")
	fs.BoolVar(&o.LineNumbers, "line-numbers", false, "")
	fs.Var(sizeFlag{&o.MaxSize}, "max-size", "")
	fs.BoolVar(&o.NoGitignore, "no-gitignore", false, "")
	fs.BoolVar(&o.NoDefaults, "no-default-ignore", false, "")
	fs.BoolVar(&o.NoSecrets, "no-security", false, "")
	fs.StringVar(&o.Tokenizer, "tokenizer", "o200k_base", "")
	fs.StringVar(&branch, "branch", "", "")
	fs.BoolVar(&o.Quiet, "q", false, "")
	fs.BoolVar(&o.Quiet, "quiet", false, "")
	fs.BoolVar(&o.NoTree, "no-tree", false, "")
	fs.BoolVar(&o.KeepNoise, "keep-noise", false, "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	o.Include, o.Ignore, o.Entries = include, ignore, entries
	o.Budget, o.Split = int(budget), int(split)
	o.Format = strings.ToLower(o.Format)
	switch o.Format {
	case "xml", "md", "markdown", "txt", "plain", "text", "json":
	default:
		return fmt.Errorf("unknown format %q (use xml, md, json or txt)", o.Format)
	}
	setTokenizer(o.Tokenizer)

	logf := func(format string, a ...any) {
		if !o.Quiet {
			fmt.Fprintf(os.Stderr, format, a...)
		}
	}

	target := "."
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	projectName := ""
	if url, ok := remoteURL(target); ok {
		logf("%s %s\n", dim("cloning"), url)
		dir, err := cloneRemote(url, branch)
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		projectName = repoNameFromURL(url)
		o.Root = dir
		if o.Output == "" && !o.Stdout {
			o.Output = projectName + "-ctxpack" + extFor(o.Format)
		}
	} else {
		root, scopes, err := resolveTargets(fs.Args())
		if err != nil {
			return err
		}
		o.Root, o.Scopes = root, scopes
		if len(o.Entries) > 0 {
			// --entry picks files by imports, which may live outside the folder
			o.Entries = entriesRelTo(root, o.Entries)
			o.Scopes = nil
		}
	}
	if o.Output == "" {
		o.Output = "ctxpack-output" + extFor(o.Format) // written to the current folder
	}

	p, err := Scan(o)
	if err != nil {
		return err
	}
	if projectName != "" {
		p.Name = projectName
	}
	files, notes, err := Select(p, o)
	if len(o.Scopes) > 0 {
		notes = append(notes, "Only part of the project is included: "+strings.Join(o.Scopes, ", "))
	}
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no files to pack (everything was ignored or filtered out)")
	}
	Transform(files, o)

	ref := "HEAD"
	if o.Since != "" {
		ref = o.Since
	}
	in := RenderInput{
		Project: p.Name, Files: files, Notes: notes, Format: o.Format,
		LineNums: o.LineNumbers, NoTree: o.NoTree, Tokenizer: encoderName,
		Prompt: buildPrompt(o.Prompt, o.Ask, o.EditFormat),
	}
	if o.WithDiff {
		in.GitDiff = gitDiffText(p.Root, ref)
	}
	if logN > 0 {
		in.GitLog = gitLogText(p.Root, logN)
	}
	if o.Budget > 0 {
		overhead := measure(RenderInput{Project: in.Project, Format: in.Format, Prompt: in.Prompt, GitDiff: in.GitDiff, GitLog: in.GitLog, Tokenizer: in.Tokenizer}, buildTree(files)) + 150
		notes := FitBudget(files, o.Budget, overhead)
		in.Notes = append(in.Notes, notes...)
	}
	parts, total := renderAll(in, o.Split)

	// ---- write results ----
	var written []string
	if o.Stdout {
		fmt.Print(strings.Join(parts, "\n"))
	} else {
		names := partNames(o.Output, len(parts))
		for i, part := range parts {
			if dir := filepath.Dir(names[i]); dir != "." {
				_ = os.MkdirAll(dir, 0o755)
			}
			if err := os.WriteFile(names[i], []byte(part), 0o644); err != nil {
				return err
			}
			written = append(written, names[i])
		}
	}
	copied := false
	if o.Copy {
		if len(parts) > 1 {
			logf("%s only part 1 was copied; open the other part files to paste them one by one\n", yellow("note:"))
		}
		if err := copyToClipboard(parts[0]); err != nil {
			logf("%s %v\n", yellow("clipboard:"), err)
		} else {
			copied = true
		}
	}
	if !o.Quiet {
		printReport(p, files, total, written, copied, in.Notes, o)
	}
	return nil
}

func extFor(format string) string {
	switch format {
	case "md", "markdown":
		return ".md"
	case "txt", "plain", "text":
		return ".txt"
	case "json":
		return ".json"
	}
	return ".xml"
}

func partNames(out string, n int) []string {
	if n <= 1 {
		return []string{out}
	}
	ext := filepath.Ext(out)
	base := strings.TrimSuffix(out, ext)
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("%s-%d%s", base, i+1, ext)
	}
	return names
}

func printReport(p *Project, files []*File, total int, written []string, copied bool, notes []string, o Options) {
	e := os.Stderr
	var full, comp, trunc, omit int
	var secretFiles []string
	for _, f := range files {
		switch f.Mode {
		case ModeFull:
			full++
		case ModeCompressed:
			comp++
		case ModeTruncated:
			trunc++
		case ModeOmitted:
			omit++
		}
		if len(f.Secrets) > 0 && f.Mode != ModeOmitted {
			secretFiles = append(secretFiles, f.Path+" ("+strings.Join(f.Secrets, ", ")+")")
		}
	}
	fmt.Fprintln(e)
	fmt.Fprintf(e, "%s %s\n", bold("ctxpack"), dim("v"+version))
	fmt.Fprintf(e, "  %-12s %s\n", "project", p.Root)
	if len(o.Scopes) > 0 {
		fmt.Fprintf(e, "  %-12s %s\n", "only", strings.Join(o.Scopes, ", "))
	}
	if len(p.Presets) > 0 {
		fmt.Fprintf(e, "  %-12s %s\n", "detected", strings.Join(p.Presets, ", "))
	}
	fmt.Fprintf(e, "  %-12s %d packed", "files", full+comp+trunc)
	if comp+trunc+omit > 0 {
		fmt.Fprintf(e, dim(" (%d compressed, %d truncated, %d omitted)"), comp, trunc, omit)
	}
	fmt.Fprintln(e)
	if len(p.Skipped) > 0 {
		fmt.Fprintf(e, "  %-12s %d binary/large files\n", "skipped", len(p.Skipped))
	}
	if p.Noise > 0 {
		fmt.Fprintf(e, "  %-12s %d config files %s\n", "skipped", p.Noise, dim("(.gitignore, LICENSE... add --keep-noise to include)"))
	}
	fmt.Fprintf(e, "  %-12s %s %s\n", "tokens", bold(humanTokens(total)), dim("("+encoderName+")"))
	fmt.Fprintf(e, "  %-12s %s\n", "fits in", fitText(total))

	// heatmap of the biggest files
	if o.Top > 0 {
		sorted := make([]*File, 0, len(files))
		for _, f := range files {
			if f.Mode != ModeOmitted {
				sorted = append(sorted, f)
			}
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tokens > sorted[j].Tokens })
		if len(sorted) > o.Top {
			sorted = sorted[:o.Top]
		}
		if len(sorted) > 0 {
			fmt.Fprintf(e, "\n  %s\n", bold("biggest files"))
			max := sorted[0].Tokens
			for _, f := range sorted {
				pct := 0.0
				if total > 0 {
					pct = float64(f.Tokens) * 100 / float64(total)
				}
				bar := 1
				if max > 0 {
					bar = f.Tokens * 20 / max
				}
				if bar < 1 {
					bar = 1
				}
				name := f.Path
				if len(name) > 44 {
					name = "…" + name[len(name)-43:]
				}
				color := green
				if pct > 25 {
					color = red
				} else if pct > 10 {
					color = yellow
				}
				fmt.Fprintf(e, "  %s %-44s %7s %5.1f%%\n", color(strings.Repeat("█", bar)+strings.Repeat("░", 20-bar)), name, humanTokens(f.Tokens), pct)
			}
		}
	}
	if len(secretFiles) > 0 {
		fmt.Fprintf(e, "\n  %s secrets were redacted in:\n", yellow("!"))
		for _, s := range secretFiles {
			fmt.Fprintf(e, "    %s\n", s)
		}
	}
	for _, n := range notes {
		fmt.Fprintf(e, "\n  %s %s\n", cyan("i"), n)
	}
	fmt.Fprintln(e)
	for _, w := range written {
		fmt.Fprintf(e, "  %s wrote %s\n", green("✓"), w)
	}
	if copied {
		fmt.Fprintf(e, "  %s copied to clipboard\n", green("✓"))
	}
	fmt.Fprintln(e)
}

func fitText(total int) string {
	windows := []struct {
		name string
		n    int
	}{{"32k", 32_000}, {"128k", 128_000}, {"200k", 200_000}, {"1M", 1_000_000}}
	var fits []string
	for _, w := range windows {
		if total <= w.n {
			fits = append(fits, w.name)
		}
	}
	if len(fits) == 0 {
		return red("too big for one prompt") + dim(" - try -c, --budget or --split")
	}
	return strings.Join(fits, " / ") + dim(" context windows")
}

// ---------- apply ----------

func runApply(args []string) error {
	fs := newFlagSet("apply")
	var clip, yes, dry, force, undo bool
	dir := "."
	fs.BoolVar(&clip, "clipboard", false, "")
	fs.StringVar(&dir, "dir", ".", "")
	fs.BoolVar(&yes, "y", false, "")
	fs.BoolVar(&yes, "yes", false, "")
	fs.BoolVar(&dry, "dry-run", false, "")
	fs.BoolVar(&force, "force", false, "")
	fs.BoolVar(&undo, "undo", false, "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if undo {
		rep, err := undoLast(root)
		if err != nil {
			return err
		}
		fmt.Println(rep)
		fmt.Println(green("✓") + " undo complete")
		return nil
	}

	var text string
	readStdin := false
	switch {
	case fs.NArg() > 0 && fs.Arg(0) != "-":
		b, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			return err
		}
		text = string(b)
	case fs.Arg(0) == "-" || (!clip && stdinIsPiped()):
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text, readStdin = string(b), true
	default:
		t, err := readClipboard()
		if err != nil {
			return fmt.Errorf("%v; pass a file instead: ctxpack apply answer.md", err)
		}
		text = t
		fmt.Println(dim("reading the AI response from the clipboard"))
	}

	changes := parseChanges(text)
	if len(changes) == 0 {
		return errors.New("no file blocks found. Ask the AI to answer with <file path=\"...\"> blocks (pack with --edits or a prompt like -p bugs)")
	}
	plan := planChanges(root, changes)
	var todo, partial int
	for _, c := range plan {
		label := map[string]string{"new": green("new   "), "modify": yellow("modify"), "delete": red("delete"), "unchanged": dim("same  "), "invalid": red("skip  ")}[c.Kind]
		stats := ""
		switch c.Kind {
		case "new":
			stats = green(fmt.Sprintf("+%d", c.Added))
		case "modify":
			stats = green(fmt.Sprintf("+%d", c.Added)) + " " + red(fmt.Sprintf("-%d", c.Removed))
		case "delete":
			stats = red(fmt.Sprintf("-%d", c.Removed))
		}
		note := ""
		if c.Note != "" {
			note = "  " + yellow(c.Note)
		}
		fmt.Printf("  %s %s %s%s\n", label, c.Path, stats, note)
		if c.Kind == "new" || c.Kind == "modify" || c.Kind == "delete" {
			if c.Partial && !force {
				partial++
			} else {
				todo++
			}
		}
	}
	if partial > 0 {
		fmt.Printf("\n  %s %d file(s) look incomplete and will be skipped (use --force to write them anyway)\n", yellow("!"), partial)
	}
	if todo == 0 {
		fmt.Println("\nNothing to apply.")
		return nil
	}
	if dry {
		fmt.Printf("\n%d change(s) would be applied (dry run).\n", todo)
		return nil
	}
	if !yes {
		ok, err := confirm(fmt.Sprintf("\nApply %d change(s)? [y/N] ", todo), readStdin)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	backup, n, err := applyChanges(root, plan, force)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, backup)
	fmt.Printf("%s applied %d change(s). Backup: %s  (undo with: ctxpack undo)\n", green("✓"), n, rel)
	return nil
}

// confirm asks on the terminal; when stdin was used for input it opens the tty directly.
func confirm(q string, stdinUsed bool) (bool, error) {
	fmt.Print(q)
	var r io.Reader = os.Stdin
	if stdinUsed {
		name := "/dev/tty"
		if runtime.GOOS == "windows" {
			name = "CONIN$"
		}
		tty, err := os.Open(name)
		if err != nil {
			return false, errors.New("cannot ask for confirmation; re-run with --yes")
		}
		defer tty.Close()
		r = tty
	}
	line, _ := bufio.NewReader(r).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes" || a == "haan" || a == "ha", nil
}
