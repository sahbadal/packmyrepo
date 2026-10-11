package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Change is one file operation found in an AI response.
type Change struct {
	Path    string   `json:"path"`
	Content string   `json:"-"`
	Delete  bool     `json:"delete"`
	Kind    string   `json:"kind"` // new, modify, delete, unchanged, invalid
	Partial bool     `json:"partial"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
	Note    string   `json:"note,omitempty"`
	Diff    []string `json:"diff,omitempty"` // unified diff lines, for review before applying
}

var (
	xmlFileOpenRe = regexp.MustCompile(`<file\s+path\s*=\s*"([^"]+)"([^>]*?)(/?)>`)
	xmlDeleteRe   = regexp.MustCompile(`<delete\s+path\s*=\s*"([^"]+)"\s*/?>`)
	fenceRe       = regexp.MustCompile("^(\\s*)(`{3,}|~{3,})(.*)$")
	pathOnlyRe    = regexp.MustCompile(`^[\w.\-/@+\[\]()]+$`)
	infoPathRe    = regexp.MustCompile(`(?:title|file|filename|path)\s*=\s*["']?([^"'\s]+)`)
	partialRes    = []*regexp.Regexp{
		regexp.MustCompile(`(?im)^\s*(?://|#|/\*|<!--|--|\*)\s*(?:\.{3}|…)`),
		regexp.MustCompile(`(?im)^\s*(?://|#|/\*|<!--)\s*(?:the\s+)?(?:rest of|existing|remaining|unchanged|same as|other)\b.*\b(?:code|file|methods?|functions?|unchanged|before|above|here)\b`),
		regexp.MustCompile(`\{\s*/\* \.\.\. \*/\s*\}`),
	}
)

// parseChanges understands two formats:
//  1. <file path="a/b.go"> ... </file>  and  <delete path="x"/>
//  2. Markdown code fences with the path in the info string (```go:a/b.go) or
//     on the line just above the fence (### a/b.go, **a/b.go**, `a/b.go`, File: a/b.go)
func parseChanges(text string) []Change {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []Change

	for _, m := range xmlDeleteRe.FindAllStringSubmatch(text, -1) {
		out = append(out, Change{Path: m[1], Delete: true})
	}
	locs := xmlFileOpenRe.FindAllStringSubmatchIndex(text, -1)
	for _, l := range locs {
		p := text[l[2]:l[3]]
		attrs := text[l[4]:l[5]]
		selfClose := l[7] > l[6]
		if selfClose || strings.Contains(attrs, `action="delete"`) {
			if strings.Contains(attrs, "delete") || selfClose {
				out = append(out, Change{Path: p, Delete: true})
			}
			continue
		}
		rest := text[l[1]:]
		end := strings.Index(rest, "</file>")
		if end < 0 {
			continue
		}
		body := strings.TrimPrefix(rest[:end], "\n")
		body = strings.TrimSuffix(body, "\n")
		out = append(out, Change{Path: p, Content: unwrapFence(body)})
	}
	if len(out) > 0 {
		return out
	}
	return parseMarkdownChanges(text)
}

// unwrapFence removes a ``` wrapper the AI may have put inside a <file> block.
func unwrapFence(body string) string {
	t := strings.TrimSpace(body)
	lines := strings.Split(t, "\n")
	if len(lines) >= 2 && fenceRe.MatchString(lines[0]) {
		lm := fenceRe.FindStringSubmatch(lines[0])
		last := strings.TrimSpace(lines[len(lines)-1])
		if strings.HasPrefix(last, lm[2]) && strings.Trim(last, lm[2][:1]) == "" {
			return strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	return body
}

func cleanPathCandidate(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#>*-+ \t")
	low := strings.ToLower(s)
	for _, pre := range []string{"file:", "filename:", "path:", "file path:", "updated file:", "new file:", "create", "update", "modify"} {
		if strings.HasPrefix(low, pre) {
			s = strings.TrimSpace(s[len(pre):])
			low = strings.ToLower(s)
		}
	}
	s = strings.Trim(s, "*_` :")
	if i := strings.Index(s, " ("); i > 0 { // "src/a.go (new file)"
		s = s[:i]
	}
	s = strings.Trim(s, "*_` :")
	if !pathOnlyRe.MatchString(s) || !(strings.Contains(s, ".") || strings.Contains(s, "/")) {
		return ""
	}
	if strings.HasPrefix(s, "http") || strings.HasSuffix(s, ".") {
		return ""
	}
	return s
}

func parseMarkdownChanges(text string) []Change {
	lines := strings.Split(text, "\n")
	var out []Change
	for i := 0; i < len(lines); i++ {
		m := fenceRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		marker := m[2]
		info := strings.TrimSpace(m[3])
		// find closing fence
		j := i + 1
		for ; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, marker) && strings.Trim(t, marker[:1]) == "" {
				break
			}
		}
		if j >= len(lines) {
			break
		}
		// work out the path
		p := ""
		if im := infoPathRe.FindStringSubmatch(info); im != nil {
			p = cleanPathCandidate(im[1])
		}
		if p == "" && info != "" {
			f := strings.Fields(info)[0]
			if k := strings.IndexByte(f, ':'); k >= 0 {
				p = cleanPathCandidate(f[k+1:])
			} else if strings.Contains(f, "/") || strings.Count(f, ".") >= 1 && len(f) > 3 && !strings.HasPrefix(f, ".") {
				if strings.Contains(f, "/") || langByExt[strings.ToLower(path.Ext(f))] != "" {
					p = cleanPathCandidate(f)
				}
			}
		}
		if p == "" {
			for k := i - 1; k >= 0 && k >= i-2; k-- {
				if strings.TrimSpace(lines[k]) == "" {
					continue
				}
				p = cleanPathCandidate(lines[k])
				break
			}
		}
		if p != "" {
			out = append(out, Change{Path: p, Content: strings.Join(lines[i+1:j], "\n")})
		}
		i = j
	}
	return out
}

// safeRel validates a path from an AI response and returns it cleaned.
func safeRel(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) || (len(p) > 1 && p[1] == ':') {
		return "", errors.New("absolute paths are not allowed")
	}
	c := path.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", errors.New("path leaves the project folder")
	}
	if c == ".git" || strings.HasPrefix(c, ".git/") || strings.HasPrefix(c, ".packmyrepo/") {
		return "", errors.New("protected folder")
	}
	return c, nil
}

func lineStats(oldS, newS string) (added, removed int) {
	oldS, newS = strings.TrimRight(oldS, "\n"), strings.TrimRight(newS, "\n")
	count := map[string]int{}
	for _, l := range strings.Split(oldS, "\n") {
		count[l]++
	}
	for _, l := range strings.Split(newS, "\n") {
		if count[l] > 0 {
			count[l]--
		} else {
			added++
		}
	}
	for _, n := range count {
		removed += n
	}
	return
}

// planChanges fills Kind/Partial/stats by comparing with files on disk.
func planChanges(root string, changes []Change) []Change {
	// last change for a path wins (AI sometimes repeats a file)
	idx := map[string]int{}
	var uniq []Change
	for _, c := range changes {
		rel, err := safeRel(c.Path)
		if err != nil {
			c.Kind, c.Note = "invalid", err.Error()
			uniq = append(uniq, c)
			continue
		}
		c.Path = rel
		if k, ok := idx[rel]; ok {
			uniq[k] = c
			continue
		}
		idx[rel] = len(uniq)
		uniq = append(uniq, c)
	}
	for i := range uniq {
		c := &uniq[i]
		if c.Kind == "invalid" {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(c.Path))
		old, err := os.ReadFile(abs)
		exists := err == nil
		oldS := strings.ReplaceAll(string(old), "\r\n", "\n")
		switch {
		case c.Delete && !exists:
			c.Kind, c.Note = "invalid", "file does not exist"
		case c.Delete:
			c.Kind = "delete"
			c.Removed = strings.Count(oldS, "\n") + 1
		case !exists:
			c.Kind = "new"
			c.Added = len(splitLines(c.Content))
			c.Diff = newFileDiff(c.Content, 40)
		case strings.TrimRight(oldS, "\n") == strings.TrimRight(c.Content, "\n"):
			c.Kind = "unchanged"
		default:
			c.Kind = "modify"
			c.Added, c.Removed = lineStats(oldS, c.Content)
			c.Diff = unifiedDiff(oldS, c.Content, 3)
		}
		if !c.Delete {
			for _, re := range partialRes {
				if re.MatchString(c.Content) && !(exists && re.MatchString(oldS)) {
					c.Partial = true
					c.Note = "looks incomplete (contains a '...' placeholder)"
					break
				}
			}
		}
	}
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].Path < uniq[j].Path })
	return uniq
}

type backupManifest struct {
	Time     string   `json:"time"`
	Modified []string `json:"modified"`
	Created  []string `json:"created"`
	Deleted  []string `json:"deleted"`
}

// applyChanges writes the plan to disk, backing up everything it touches.
// Partial files are skipped unless force is set.
func applyChanges(root string, plan []Change, force bool) (backupDir string, applied int, err error) {
	stamp := time.Now().Format("20060102-150405.000")
	backupDir = filepath.Join(root, ".packmyrepo", "backups", stamp)
	man := backupManifest{Time: time.Now().Format(time.RFC3339)}
	backup := func(rel string) error {
		src := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		dst := filepath.Join(backupDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	}
	for _, c := range plan {
		if c.Kind == "invalid" || c.Kind == "unchanged" || (c.Partial && !force) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(c.Path))
		switch c.Kind {
		case "delete":
			if err := backup(c.Path); err != nil {
				return backupDir, applied, err
			}
			if err := os.Remove(abs); err != nil {
				return backupDir, applied, err
			}
			man.Deleted = append(man.Deleted, c.Path)
		case "new", "modify":
			mode := os.FileMode(0o644)
			var old []byte
			if c.Kind == "modify" {
				if err := backup(c.Path); err != nil {
					return backupDir, applied, err
				}
				old, _ = os.ReadFile(abs)
				if st, err := os.Stat(abs); err == nil {
					mode = st.Mode().Perm()
				}
				man.Modified = append(man.Modified, c.Path)
			} else {
				man.Created = append(man.Created, c.Path)
			}
			content := matchFileStyle(c.Content, old, c.Kind == "modify")
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return backupDir, applied, err
			}
			if err := os.WriteFile(abs, []byte(content), mode); err != nil {
				return backupDir, applied, err
			}
		}
		applied++
	}
	if applied > 0 {
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return backupDir, applied, err
		}
		data, _ := json.MarshalIndent(man, "", "  ")
		_ = os.WriteFile(filepath.Join(backupDir, "manifest.json"), data, 0o644)
		ensureGitignored(root)
	}
	return backupDir, applied, nil
}

// ensureGitignored adds .packmyrepo/ to .gitignore when the project uses git.
func ensureGitignored(root string) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return
	}
	gi := filepath.Join(root, ".gitignore")
	data, _ := os.ReadFile(gi)
	for _, l := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(l); t == ".packmyrepo/" || t == ".packmyrepo" || t == "/.packmyrepo/" {
			return
		}
	}
	f, err := os.OpenFile(gi, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	fmt.Fprint(f, prefix+".packmyrepo/\n")
}

// undoLast restores the most recent backup.
func undoLast(root string) (string, error) {
	base := filepath.Join(root, ".packmyrepo", "backups")
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", errors.New("nothing to undo (no backups found)")
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasSuffix(e.Name(), ".undone") {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		return "", errors.New("nothing to undo")
	}
	sort.Strings(dirs)
	dir := filepath.Join(base, dirs[len(dirs)-1])
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", err
	}
	var man backupManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return "", err
	}
	var report []string
	for _, rel := range append(append([]string{}, man.Modified...), man.Deleted...) {
		src := filepath.Join(dir, filepath.FromSlash(rel))
		b, err := os.ReadFile(src)
		if err != nil {
			report = append(report, "could not restore "+rel)
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			report = append(report, "could not restore "+rel)
			continue
		}
		report = append(report, "restored "+rel)
	}
	for _, rel := range man.Created {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.Remove(abs); err == nil {
			report = append(report, "removed "+rel)
			// remove folders that became empty
			for d := filepath.Dir(abs); d != root && strings.HasPrefix(d, root); d = filepath.Dir(d) {
				if os.Remove(d) != nil {
					break
				}
			}
		}
	}
	_ = os.Rename(dir, dir+".undone")
	return strings.Join(report, "\n"), nil
}

// matchFileStyle keeps the file's own conventions so the only differences
// are the ones the AI actually made: Windows line endings stay Windows line
// endings, and a file that had no newline at the end does not gain one.
// New files get the usual single newline at the end.
func matchFileStyle(content string, old []byte, existed bool) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	body := strings.TrimRight(content, "\n")
	if body == "" {
		return content
	}
	endsWithNewline := true
	crlf := false
	if existed {
		o := string(old)
		endsWithNewline = strings.HasSuffix(o, "\n")
		crlf = strings.Contains(o, "\r\n")
	}
	if endsWithNewline {
		body += "\n"
	}
	if crlf {
		body = strings.ReplaceAll(body, "\n", "\r\n")
	}
	return body
}

// changesFromText turns an AI reply into changes. With to set, the reply is
// treated as the new content of that one file, for when only the code was
// copied (without the <file path="..."> lines around it).
func changesFromText(text, to string) ([]Change, error) {
	if to == "" {
		changes := parseChanges(text)
		if len(changes) == 0 {
			return nil, errors.New("no file blocks found in the answer.\n" +
				"  Copy the AI's whole reply (or the code box that starts with <file path=...>).\n" +
				"  If you copied only the code of one file, run: packmyrepo apply --to path/to/that/file")
		}
		return changes, nil
	}
	content := text
	if ch := parseChanges(text); len(ch) == 1 && !ch[0].Delete {
		content = ch[0].Content // the reply had the <file> lines after all
	} else if len(ch) > 1 {
		return nil, fmt.Errorf("the answer contains %d files; drop --to to apply all of them", len(ch))
	} else {
		content = unwrapFence(strings.Trim(text, "\r\n"))
	}
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("the copied text is empty")
	}
	return []Change{{Path: to, Content: content}}, nil
}
