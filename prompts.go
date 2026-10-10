package main

import (
	"sort"
	"strings"
)

// editFormat tells the AI how to return changed files so `ctxpack apply` can
// write them back into the project.
const editFormat = `When you change or create files, return each one in full (never "rest of code unchanged") inside this exact format so the changes can be applied automatically:

<file path="relative/path/to/file.ext">
...complete new file content...
</file>

To delete a file, write: <delete path="relative/path/to/file.ext"/>
Explain your changes briefly outside these blocks.`

type promptTemplate struct {
	desc  string
	text  string
	edits bool // include editFormat
}

var promptTemplates = map[string]promptTemplate{
	"review":   {"Code review: bugs, risks, readability", `Review this codebase like a senior engineer. List the most important problems first: bugs, security issues, race conditions, error handling gaps, and confusing code. For each, give the file path, why it matters, and a concrete fix. Finish with 3-5 quick wins.`, false},
	"bugs":     {"Find and fix bugs", `Find real bugs in this code (not style nits). For each bug: file path, what goes wrong, how to reproduce it, and the fix. Then apply the fixes.`, true},
	"explain":  {"Explain the project to a newcomer", `Explain this project to a developer who just joined. Cover: what it does, how the folders are organised, the main data flow from entry point to output, key files to read first, and anything surprising. Use simple language.`, false},
	"docs":     {"Write a README / documentation", `Write clear documentation for this project: a README with what it is, install, usage examples, configuration, and project structure. Return it as README.md.`, true},
	"tests":    {"Write missing tests", `Find the most important untested logic and write tests for it using the testing framework this project already uses. Cover edge cases and failure paths.`, true},
	"refactor": {"Refactor for clarity", `Suggest and apply refactors that make this code simpler and easier to maintain without changing behaviour: remove duplication, split long functions, improve names. Keep changes focused.`, true},
	"feature": {"Implement a feature (add your request)", `Implement the following feature in this codebase, following its existing style and structure:

`, true},
	"security": {"Security audit", `Audit this code for security problems: injection, auth/authz mistakes, secrets handling, unsafe deserialization, path traversal, SSRF, XSS, insecure defaults, and vulnerable patterns. Rank findings by severity with file paths and fixes.`, false},
}

// buildPrompt turns --prompt / --ask into the final instruction text.
func buildPrompt(name, extra string, wantEdits bool) string {
	name = strings.TrimSpace(name)
	extra = strings.TrimSpace(extra)
	if name == "" && extra == "" {
		return ""
	}
	if t, ok := promptTemplates[name]; ok {
		text := t.text
		if extra != "" {
			text = strings.TrimRight(text, "\n") + "\n" + extra
		}
		if t.edits || wantEdits {
			text += "\n\n" + editFormat
		}
		return text
	}
	// not a template name: treat it as a custom instruction
	text := strings.TrimSpace(name + "\n" + extra)
	if wantEdits {
		text += "\n\n" + editFormat
	}
	return text
}

func promptNames() []string {
	var n []string
	for k := range promptTemplates {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
