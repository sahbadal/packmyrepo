package app

import (
	"encoding/json"
	"strings"
)

type jsonFile struct {
	Path    string `json:"path"`
	Lang    string `json:"language,omitempty"`
	Mode    string `json:"mode"`
	Tokens  int    `json:"tokens"`
	Content string `json:"content"`
}

type jsonPack struct {
	Project      string     `json:"project"`
	PackedBy     string     `json:"packed_by"`
	Part         int        `json:"part,omitempty"`
	Parts        int        `json:"parts,omitempty"`
	Summary      []string   `json:"summary"`
	Tree         string     `json:"directory_structure,omitempty"`
	Commits      string     `json:"recent_commits,omitempty"`
	Files        []jsonFile `json:"files"`
	Omitted      []string   `json:"omitted_files,omitempty"`
	GitDiff      string     `json:"git_diff,omitempty"`
	Instructions string     `json:"instructions,omitempty"`
}

// renderJSON writes the pack as one JSON object. It is meant for scripts and
// pipelines; for chatting with an AI, XML or Markdown use fewer tokens because
// JSON has to escape every newline and quote.
func renderJSON(in RenderInput, tree string, sum []string, content func(*File) string, first bool) string {
	out := jsonPack{
		Project:  in.Project,
		PackedBy: "packmyrepo " + version,
		Summary:  sum,
		Files:    []jsonFile{},
		GitDiff:  in.GitDiff,
	}
	if in.Parts > 1 {
		out.Part, out.Parts = in.Part, in.Parts
	}
	if first && !in.NoTree {
		out.Tree = tree
	}
	if first {
		out.Commits = in.GitLog
	}
	for _, f := range in.Files {
		if f.Mode == ModeOmitted {
			out.Omitted = append(out.Omitted, f.Path)
			continue
		}
		out.Files = append(out.Files, jsonFile{Path: f.Path, Lang: f.Lang, Mode: f.Mode.String(), Tokens: f.Tokens, Content: content(f)})
	}
	if in.Prompt != "" && (in.Parts <= 1 || in.Part == in.Parts) {
		out.Instructions = in.Prompt
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep <, > and & readable in code
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return b.String()
}
