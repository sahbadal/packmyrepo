package app

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type Mode int

const (
	ModeFull Mode = iota
	ModeCompressed
	ModeTruncated
	ModeOmitted
)

func (m Mode) String() string {
	return [...]string{"full", "compressed", "truncated", "omitted"}[m]
}

// File is one file of the project.
type File struct {
	Path     string // slash path relative to root
	Lang     string
	Raw      string // content after secret redaction
	Content  string // content that goes into the output
	Tokens   int
	Size     int64
	Mode     Mode
	Priority int
	Secrets  []string
	Depth    int // import depth from --entry (-1 if not used)
}

func (f *File) clone() *File { c := *f; return &c }

type Options struct {
	Root           string
	Format         string
	Output         string
	Include        []string
	Ignore         []string
	Compress       bool
	RemoveComments bool
	Budget         int
	Split          int
	Copy           bool
	Stdout         bool
	Prompt         string
	Ask            string
	EditFormat     bool
	Diff           bool
	Since          string
	WithDiff       bool
	Entries        []string
	Depth          int
	Top            int
	NoSecrets      bool
	NoGitignore    bool
	NoDefaults     bool
	LineNumbers    bool
	MaxSize        int64
	Tokenizer      string
	Quiet          bool
	NoTree         bool
	KeepNoise      bool
	NoCache        bool
	ConfigFile     string
	Scopes         []string // only these folders/files inside Root (empty = all)
}

func defaultOptions() Options {
	return Options{Root: ".", Format: "xml", Top: 5, MaxSize: 1 << 20}
}

type Skipped struct {
	Path   string
	Reason string
}

// Project is the scanned state of a directory.
type Project struct {
	Root     string
	Name     string
	Presets  []string
	Files    []*File
	Skipped  []Skipped
	GoModule string
	Noise    int // config/noise files skipped by default
}

// Scan walks root, applies ignore rules, reads text files, redacts secrets and counts tokens.
func Scan(o Options) (*Project, error) {
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", o.Root)
	}
	p := &Project{Root: root, Name: filepath.Base(root)}
	if !o.NoCache {
		openTokenCache(root)
	}

	m := &Matcher{}
	if !o.NoDefaults {
		m.AddLines(defaultIgnores, "")
		for _, pr := range detectPresets(root) {
			p.Presets = append(p.Presets, pr.name)
			m.AddLines(pr.ignores, "")
		}
	}
	m.AddFile(filepath.Join(root, ".packmyrepoignore"), "")
	for _, ig := range o.Ignore {
		m.Add(ig, "")
	}
	// The output file must never pack itself.
	if o.Output != "" {
		if abs, err := filepath.Abs(o.Output); err == nil {
			if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
				m.Add("/"+filepath.ToSlash(rel), "")
			}
		}
	}
	noise := &Matcher{}
	if !o.NoDefaults && !o.KeepNoise {
		noise.AddLines(noiseFiles, "")
	}
	var incl *Matcher
	if len(o.Include) > 0 {
		incl = &Matcher{}
		for _, in := range o.Include {
			incl.Add(in, "")
		}
	}

	var paths []string
	err = filepath.WalkDir(root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped silently
		}
		rel, _ := filepath.Rel(root, abs)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				if !o.NoGitignore {
					m.AddFile(filepath.Join(abs, ".gitignore"), "")
				}
				return nil
			}
			if !inScope(rel, true, o.Scopes) {
				return filepath.SkipDir
			}
			if m.Match(rel, true) && !isScopePath(rel, o.Scopes) {
				return filepath.SkipDir
			}
			if !o.NoGitignore {
				m.AddFile(filepath.Join(abs, ".gitignore"), rel)
				m.AddFile(filepath.Join(abs, ".packmyrepoignore"), rel)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !inScope(rel, false, o.Scopes) {
			return nil
		}
		if m.Match(rel, false) && !isScopePath(rel, o.Scopes) {
			return nil
		}
		if noise.MatchPath(rel) && !isScopePath(rel, o.Scopes) && (incl == nil || !incl.Match(rel, false)) {
			p.Noise++
			return nil
		}
		if incl != nil && !incl.MatchPath(rel) {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// user --ignore must win over nested negations, so check again at the end
	user := &Matcher{}
	for _, ig := range o.Ignore {
		user.Add(ig, "")
	}

	// Read, check and redact files on every CPU core. Results keep their slot
	// so the output order never depends on which worker finished first.
	type result struct {
		file *File
		skip *Skipped
	}
	results := make([]result, len(paths))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f, sk := loadFile(root, paths[i], o)
				results[i] = result{f, sk}
			}
		}()
	}
	for i, rel := range paths {
		if len(o.Ignore) > 0 && user.MatchPath(rel) {
			continue
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, r := range results {
		if r.file != nil {
			p.Files = append(p.Files, r.file)
		} else if r.skip != nil {
			p.Skipped = append(p.Skipped, *r.skip)
		}
	}
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
	p.GoModule = readGoModule(root)
	countAll(p.Files)
	saveTokenCache()
	return p, nil
}

// loadFile reads one file. It returns the file, or a reason it was skipped,
// or neither for files that are silently dropped (unreadable or empty).
func loadFile(root, rel string, o Options) (*File, *Skipped) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		return nil, nil
	}
	if o.MaxSize > 0 && info.Size() > o.MaxSize {
		return nil, &Skipped{rel, "too large (" + humanBytes(info.Size()) + ")"}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil // empty files add nothing
	}
	if looksBinary(rel, data) {
		return nil, &Skipped{rel, "binary"}
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.TrimPrefix(content, "\ufeff")
	f := &File{Path: rel, Lang: langOf(rel), Size: info.Size(), Depth: -1}
	if !o.NoSecrets {
		content, f.Secrets = redactSecrets(rel, content)
	}
	f.Raw = content
	f.Content = content
	f.Priority = priorityOf(rel)
	return f, nil
}

func readGoModule(root string) string {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "module ") {
			return strings.Trim(strings.TrimSpace(l[7:]), `"`)
		}
	}
	return ""
}

// priorityOf scores how important a file usually is for understanding a project.
func priorityOf(rel string) int {
	low := strings.ToLower(rel)
	base := path.Base(low)
	depth := strings.Count(rel, "/")
	score := 50 - depth*2
	switch {
	case strings.HasPrefix(base, "readme"):
		score = 95
	case base == "package.json" || base == "go.mod" || base == "cargo.toml" || base == "pyproject.toml" ||
		base == "pubspec.yaml" || base == "requirements.txt" || base == "pom.xml" || base == "build.gradle" ||
		base == "composer.json" || base == "gemfile" || base == "dockerfile" || base == "docker-compose.yml":
		score = 85
	case strings.HasPrefix(base, "main.") || strings.HasPrefix(base, "index.") || strings.HasPrefix(base, "app.") ||
		strings.HasPrefix(base, "server.") || base == "lib.rs" || base == "__main__.py" || base == "manage.py":
		score = 70 - depth
	}
	isTest := strings.Contains(low, "/test/") || strings.Contains(low, "/tests/") || strings.HasPrefix(low, "test/") ||
		strings.HasPrefix(low, "tests/") || strings.Contains(low, "__tests__") || strings.Contains(base, "_test.") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_")
	if isTest {
		score = 20
	}
	if strings.Contains(low, "fixture") || strings.Contains(low, "__snapshots__") || strings.Contains(low, "mock") ||
		strings.Contains(low, "/generated/") || strings.HasSuffix(base, ".csv") || strings.HasSuffix(base, ".svg") ||
		strings.HasSuffix(base, ".snap") || strings.Contains(low, "migrations/") {
		score = 10
	}
	if strings.HasSuffix(base, ".md") && !strings.HasPrefix(base, "readme") {
		score = 30
	}
	return score
}

// Select picks the files to pack according to --diff/--since/--entry.
func Select(p *Project, o Options) ([]*File, []string, error) {
	var notes []string
	var out []*File
	keep := map[string]bool{}
	useFilter := false

	if o.Diff || o.Since != "" {
		ref := "HEAD"
		if o.Since != "" {
			ref = o.Since
		}
		changed, deleted, err := gitChanged(p.Root, ref)
		if err != nil {
			return nil, nil, err
		}
		useFilter = true
		for _, c := range changed {
			keep[c] = true
		}
		if len(deleted) > 0 {
			notes = append(notes, "Deleted since "+ref+": "+strings.Join(deleted, ", "))
		}
		if len(changed) == 0 {
			notes = append(notes, "No changed files found compared to "+ref)
		}
	}

	depthOf := map[string]int{}
	if len(o.Entries) > 0 {
		r := newResolver(p.Files, p.GoModule)
		var entries []string
		for _, e := range o.Entries {
			e = filepath.ToSlash(strings.TrimPrefix(e, "./"))
			if !r.has(e) {
				return nil, nil, fmt.Errorf("entry file %q not found (or ignored) in %s", e, p.Root)
			}
			entries = append(entries, e)
		}
		depthOf = r.Closure(entries, o.Depth)
		entryKeep := map[string]bool{}
		for k := range depthOf {
			entryKeep[k] = true
		}
		if useFilter {
			// diff + entry: changed files plus everything they import
			for k := range keep {
				entryKeep[k] = true
			}
		}
		keep = entryKeep
		useFilter = true
	}

	for _, f := range p.Files {
		if useFilter && !keep[f.Path] {
			continue
		}
		c := f.clone()
		if d, ok := depthOf[c.Path]; ok {
			c.Depth = d
			c.Priority = 100 - d*5
		}
		out = append(out, c)
	}
	return out, notes, nil
}

// Transform applies comment removal and compression, then recounts tokens.
func Transform(files []*File, o Options) {
	var changed []*File
	for _, f := range files {
		c := f.Raw
		if o.RemoveComments {
			if s, ok := removeComments(c, f.Lang); ok {
				c = s
			}
		}
		f.Content = c
		f.Mode = ModeFull
		if o.Compress && f.Depth != 0 { // never compress an entry file
			if s, ok := compressCode(c, f.Lang); ok && s != c {
				f.Content = s
				f.Mode = ModeCompressed
			}
		}
		if f.Content != f.Raw {
			changed = append(changed, f)
		}
	}
	countAll(changed) // unchanged files keep the count from Scan
}

// FitBudget shrinks the pack until it fits: compress least important files first,
// then truncate big data files, then omit files. Returns a human summary.
func FitBudget(files []*File, budget, overhead int) []string {
	total := overhead
	for _, f := range files {
		total += f.Tokens + wrapperCost(f)
	}
	if budget <= 0 || total <= budget {
		return nil
	}
	start := total
	order := make([]*File, len(files))
	copy(order, files)
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].Priority != order[j].Priority {
			return order[i].Priority < order[j].Priority
		}
		return order[i].Tokens > order[j].Tokens
	})
	var compressed, truncated, omitted int
	// pass 1: compress code
	for _, f := range order {
		if total <= budget {
			break
		}
		if f.Mode != ModeFull || f.Depth == 0 {
			continue
		}
		if s, ok := compressCode(f.Content, f.Lang); ok && s != f.Content {
			t := countTokens(s)
			total += t - f.Tokens
			f.Content, f.Tokens, f.Mode = s, t, ModeCompressed
			compressed++
		}
	}
	// pass 2: truncate big data/doc files
	for _, f := range order {
		if total <= budget {
			break
		}
		if f.Mode != ModeFull || !dataLangs[f.Lang] || f.Tokens < 800 {
			continue
		}
		if s, ok := truncateData(f.Content, 40); ok {
			t := countTokens(s)
			total += t - f.Tokens
			f.Content, f.Tokens, f.Mode = s, t, ModeTruncated
			truncated++
		}
	}
	// pass 3: omit
	type saved struct {
		content string
		tokens  int
		mode    Mode
	}
	backup := map[*File]saved{}
	for _, f := range order {
		if total <= budget {
			break
		}
		if f.Depth == 0 {
			continue
		}
		backup[f] = saved{f.Content, f.Tokens, f.Mode}
		total -= f.Tokens + wrapperCost(f)
		f.Content, f.Tokens, f.Mode = "", 0, ModeOmitted
		omitted++
	}
	// pass 4: refill - dropping one big file may have freed room for smaller ones
	for i := len(order) - 1; i >= 0; i-- {
		f := order[i]
		b, ok := backup[f]
		if !ok {
			continue
		}
		if cost := b.tokens + wrapperCost(f); total+cost <= budget {
			f.Content, f.Tokens, f.Mode = b.content, b.tokens, b.mode
			total += cost
			omitted--
		}
	}
	compressed, truncated, omitted = 0, 0, 0
	for _, f := range files {
		switch f.Mode {
		case ModeCompressed:
			compressed++
		case ModeTruncated:
			truncated++
		case ModeOmitted:
			omitted++
		}
	}
	msg := fmt.Sprintf("Budget %s: %s -> %s tokens (compressed %d, truncated %d, omitted %d files)",
		humanTokens(budget), humanTokens(start), humanTokens(total), compressed, truncated, omitted)
	out := []string{msg}
	if total > budget {
		out = append(out, "Still over budget: entry files alone are larger than the budget")
	}
	return out
}

func wrapperCost(f *File) int { return len(f.Path)/3 + 12 }

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
