package main

import (
	"fmt"
	"sort"
	"strings"
)

// RenderInput is everything the renderer needs.
type RenderInput struct {
	Project   string
	Files     []*File
	Notes     []string
	Prompt    string
	GitDiff   string
	GitLog    string
	Format    string
	LineNums  bool
	NoTree    bool
	Part      int // 1-based, 0 = not split
	Parts     int
	Tokenizer string
	Total     int // filled in later when known

	skipContent bool // used to measure the wrapper cost quickly
}

// buildTree draws a directory tree. Compressed/omitted files are marked.
func buildTree(files []*File) string {
	type node struct {
		name     string
		children map[string]*node
		file     *File
	}
	root := &node{children: map[string]*node{}}
	for _, f := range files {
		cur := root
		parts := strings.Split(f.Path, "/")
		for i, p := range parts {
			nx, ok := cur.children[p]
			if !ok {
				nx = &node{name: p, children: map[string]*node{}}
				cur.children[p] = nx
			}
			if i == len(parts)-1 {
				nx.file = f
			}
			cur = nx
		}
	}
	var b strings.Builder
	var walk func(n *node, prefix string)
	walk = func(n *node, prefix string) {
		names := make([]string, 0, len(n.children))
		for k := range n.children {
			names = append(names, k)
		}
		// folders first, then files, alphabetical
		sort.Slice(names, func(i, j int) bool {
			a, c := n.children[names[i]], n.children[names[j]]
			ad, cd := a.file == nil, c.file == nil
			if ad != cd {
				return ad
			}
			return names[i] < names[j]
		})
		for i, name := range names {
			c := n.children[name]
			last := i == len(names)-1
			branch, next := "├── ", "│   "
			if last {
				branch, next = "└── ", "    "
			}
			label := name
			if c.file == nil {
				label += "/"
			} else if c.file.Mode != ModeFull {
				label += " (" + c.file.Mode.String() + ")"
			}
			b.WriteString(prefix + branch + label + "\n")
			if c.file == nil {
				walk(c, prefix+next)
			}
		}
	}
	walk(root, "")
	return strings.TrimRight(b.String(), "\n")
}

func withLineNumbers(s string) string {
	lines := strings.Split(s, "\n")
	w := len(itoa(len(lines)))
	for i, l := range lines {
		lines[i] = fmt.Sprintf("%*d| %s", w, i+1, l)
	}
	return strings.Join(lines, "\n")
}

func summaryLines(in RenderInput) []string {
	var full, comp, trunc, omit, tokens int
	var secretFiles []string
	for _, f := range in.Files {
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
		tokens += f.Tokens
		if len(f.Secrets) > 0 && f.Mode != ModeOmitted {
			secretFiles = append(secretFiles, f.Path)
		}
	}
	s := []string{
		"Project: " + in.Project,
		fmt.Sprintf("Files: %d included (%d full, %d compressed, %d truncated), %d omitted", full+comp+trunc, full, comp, trunc, omit),
	}
	if in.Total > 0 {
		s = append(s, fmt.Sprintf("Total tokens: ~%d (%s)", in.Total, in.Tokenizer))
	}
	if in.Parts > 1 {
		s = append(s, fmt.Sprintf("This is part %d of %d. Wait for all parts before answering.", in.Part, in.Parts))
	}
	if comp > 0 {
		s = append(s, "Compressed files keep imports, types and signatures; function bodies are replaced with /* ... */ or ...")
	}
	if omit > 0 {
		s = append(s, "Omitted files are listed in the tree but their content is not included. Ask for them if needed.")
	}
	if len(secretFiles) > 0 {
		s = append(s, "Secrets were redacted as [REDACTED:type] in: "+strings.Join(secretFiles, ", "))
	}
	s = append(s, in.Notes...)
	return s
}

func fence(content string) string {
	longest, run := 0, 0
	for _, c := range content {
		if c == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	if longest < 3 {
		return "```"
	}
	return strings.Repeat("`", longest+1)
}

// Render produces the final text for one part.
func Render(in RenderInput, tree string) string {
	var b strings.Builder
	w := func(s ...string) {
		for _, x := range s {
			b.WriteString(x)
		}
	}
	content := func(f *File) string {
		if in.skipContent {
			return ""
		}
		c := strings.TrimRight(f.Content, "\n")
		if in.LineNums {
			return withLineNumbers(c)
		}
		return c
	}
	sum := summaryLines(in)
	first := in.Part <= 1

	switch in.Format {
	case "md", "markdown":
		w("# ", in.Project, " (packed by ctxpack)\n\n")
		if first {
			w("This file contains the codebase in a single document for AI analysis.\n\n")
		}
		w("## Summary\n\n")
		for _, l := range sum {
			w("- ", l, "\n")
		}
		if first && !in.NoTree {
			w("\n## Directory structure\n\n```\n", tree, "\n```\n")
		}
		if in.GitLog != "" && first {
			w("\n## Recent commits\n\n```\n", in.GitLog, "\n```\n")
		}
		w("\n## Files\n")
		for _, f := range in.Files {
			if f.Mode == ModeOmitted {
				continue
			}
			fc := content(f)
			fn := fence(fc)
			note := ""
			if f.Mode != ModeFull {
				note = " (" + f.Mode.String() + ")"
			}
			w("\n### ", f.Path, note, "\n\n", fn, f.Lang, "\n", fc, "\n", fn, "\n")
		}
		if in.GitDiff != "" {
			fn := fence(in.GitDiff)
			w("\n## Git diff\n\n", fn, "diff\n", in.GitDiff, "\n", fn, "\n")
		}
		if in.Prompt != "" && in.Part == in.Parts || in.Prompt != "" && in.Parts <= 1 {
			w("\n## Instructions\n\n", in.Prompt, "\n")
		}
	case "txt", "plain", "text":
		sep := strings.Repeat("=", 64)
		w(sep, "\n", in.Project, " (packed by ctxpack)\n", sep, "\n")
		for _, l := range sum {
			w(l, "\n")
		}
		if first && !in.NoTree {
			w("\n", sep, "\nDirectory structure\n", sep, "\n", tree, "\n")
		}
		if in.GitLog != "" && first {
			w("\n", sep, "\nRecent commits\n", sep, "\n", in.GitLog, "\n")
		}
		for _, f := range in.Files {
			if f.Mode == ModeOmitted {
				continue
			}
			note := ""
			if f.Mode != ModeFull {
				note = " (" + f.Mode.String() + ")"
			}
			w("\n", sep, "\nFile: ", f.Path, note, "\n", sep, "\n", content(f), "\n")
		}
		if in.GitDiff != "" {
			w("\n", sep, "\nGit diff\n", sep, "\n", in.GitDiff, "\n")
		}
		if in.Prompt != "" && (in.Parts <= 1 || in.Part == in.Parts) {
			w("\n", sep, "\nInstructions\n", sep, "\n", in.Prompt, "\n")
		}
	case "json":
		return renderJSON(in, tree, sum, content, first)
	default: // xml
		w("<codebase name=\"", xmlAttr(in.Project), "\" packed_by=\"ctxpack\">\n")
		if first {
			w("This is the whole codebase packed into one document so an AI can read it.\n")
		}
		w("<summary>\n", strings.Join(sum, "\n"), "\n</summary>\n")
		if first && !in.NoTree {
			w("<directory_structure>\n", tree, "\n</directory_structure>\n")
		}
		if in.GitLog != "" && first {
			w("<recent_commits>\n", in.GitLog, "\n</recent_commits>\n")
		}
		w("<files>\n")
		for _, f := range in.Files {
			if f.Mode == ModeOmitted {
				continue
			}
			attrs := ""
			if f.Mode != ModeFull {
				attrs = " mode=\"" + f.Mode.String() + "\""
			}
			w("<file path=\"", xmlAttr(f.Path), "\"", attrs, ">\n", content(f), "\n</file>\n")
		}
		w("</files>\n")
		if in.GitDiff != "" {
			w("<git_diff>\n", in.GitDiff, "\n</git_diff>\n")
		}
		if in.Prompt != "" && (in.Parts <= 1 || in.Part == in.Parts) {
			w("<instructions>\n", in.Prompt, "\n</instructions>\n")
		}
		w("</codebase>\n")
	}
	return b.String()
}

func xmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// renderAll renders the pack, splitting into parts when maxPart > 0.
// It returns the rendered parts and the total token count.
func renderAll(base RenderInput, maxPart int) ([]string, int) {
	tree := buildTree(base.Files)
	if maxPart <= 0 {
		base.Total = measure(base, tree)
		return []string{Render(base, tree)}, base.Total
	}
	// group files into chunks by token size
	var groups [][]*File
	var cur []*File
	curTok := countTokens(tree) + 300
	for _, f := range base.Files {
		cost := f.Tokens + wrapperCost(f)
		if f.Mode == ModeOmitted {
			cost = 0
		}
		if len(cur) > 0 && curTok+cost > maxPart {
			groups = append(groups, cur)
			cur, curTok = nil, 300
		}
		cur = append(cur, f)
		curTok += cost
	}
	groups = append(groups, cur)
	var parts []string
	total := 0
	for i, g := range groups {
		in := base
		in.Files = g
		in.Part, in.Parts = i+1, len(groups)
		if i > 0 {
			in.GitLog = ""
		}
		if i != len(groups)-1 {
			in.GitDiff = ""
		}
		in.Total = measure(in, tree)
		out := Render(in, tree)
		total += in.Total
		parts = append(parts, out)
	}
	return parts, total
}

// measure counts the tokens of a render without tokenising every file again:
// wrapper text is counted once and file tokens are already known.
func measure(in RenderInput, tree string) int {
	if in.LineNums || in.Format == "json" { // JSON escaping changes the token count
		return countTokens(Render(in, tree))
	}
	in.Total = 999999 // reserve the width of the number in the summary line
	sk := in
	sk.skipContent = true
	n := countTokens(Render(sk, tree))
	for _, f := range in.Files {
		if f.Mode != ModeOmitted {
			n += f.Tokens
		}
	}
	return n
}
