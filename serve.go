package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

//go:embed web/index.html
var webFS embed.FS

type server struct {
	opts Options
	mu   sync.Mutex
	proj *Project
	host string
}

func runServe(args []string) error {
	o := defaultOptions()
	fs := newFlagSet("ui")
	port := 7878
	var noOpen bool
	var ignore listFlag
	fs.IntVar(&port, "port", 7878, "")
	fs.BoolVar(&noOpen, "no-open", false, "")
	fs.Var(&ignore, "ignore", "")
	fs.BoolVar(&o.NoGitignore, "no-gitignore", false, "")
	fs.BoolVar(&o.NoSecrets, "no-security", false, "")
	fs.StringVar(&o.Tokenizer, "tokenizer", "o200k_base", "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	o.Ignore = ignore
	root, scopes, err := resolveTargets(fs.Args())
	if err != nil {
		return err
	}
	o.Root, o.Scopes = root, scopes
	cfg, _, err := loadConfig(root)
	if err != nil {
		return err
	}
	if cfg != nil {
		cfg.apply(&o, setFlags(fs))
	}
	setTokenizer(o.Tokenizer)
	abs, err := filepath.Abs(o.Root)
	if err != nil {
		return err
	}
	o.Root = abs

	s := &server{opts: o}
	fmt.Fprintf(os.Stderr, "%s scanning %s ...\n", dim("packmyrepo"), abs)
	if err := s.rescan(); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0") // port busy: pick a free one
		if err != nil {
			return err
		}
	}
	s.host = ln.Addr().String()
	url := "http://" + s.host + "/"

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/api/scan", s.scan)
	mux.HandleFunc("/api/related", s.related)
	mux.HandleFunc("/api/pack", s.pack)
	mux.HandleFunc("/api/apply/preview", s.applyPreview)
	mux.HandleFunc("/api/apply/run", s.applyRun)
	mux.HandleFunc("/api/undo", s.undo)

	fmt.Fprintf(os.Stderr, "%s %s  %s\n", green("●"), bold(url), dim("(Ctrl+C to stop)"))
	if !noOpen {
		openBrowser(url)
	}
	return http.Serve(ln, s.guard(mux))
}

// guard only allows requests addressed to this local server (blocks DNS
// rebinding and cross-site form posts).
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, port, _ := net.SplitHostPort(s.host)
		okHost := r.Host == s.host || r.Host == "localhost:"+port
		if !okHost {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "json only", http.StatusUnsupportedMediaType)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) rescan() error {
	p, err := Scan(s.opts)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.proj = p
	s.mu.Unlock()
	return nil
}

func (s *server) project() *Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proj
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	if r.Method != http.MethodPost {
		return errors.New("use POST")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<20)
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

type fileInfo struct {
	Path     string   `json:"path"`
	Tokens   int      `json:"tokens"`
	Priority int      `json:"priority"`
	Secrets  []string `json:"secrets,omitempty"`
	CanZip   bool     `json:"compressible"`
}

func (s *server) scan(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		if err := s.rescan(); err != nil {
			writeErr(w, err)
			return
		}
	}
	p := s.project()
	files := make([]fileInfo, 0, len(p.Files))
	total := 0
	for _, f := range p.Files {
		fam := familyOf(f.Lang)
		files = append(files, fileInfo{f.Path, f.Tokens, f.Priority, f.Secrets, fam == famGo || fam == famPython || fam == famCLike || fam == famRust})
		total += f.Tokens
	}
	type pr struct {
		Name string `json:"name"`
		Desc string `json:"desc"`
	}
	var prompts []pr
	for _, n := range promptNames() {
		prompts = append(prompts, pr{n, promptTemplates[n].desc})
	}
	writeJSON(w, map[string]any{
		"name": p.Name, "root": p.Root, "presets": p.Presets, "tokenizer": encoderName,
		"files": files, "total": total, "skipped": len(p.Skipped), "prompts": prompts,
		"version": version,
	})
}

func (s *server) related(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
		Depth int      `json:"depth"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	p := s.project()
	res := newResolver(p.Files, p.GoModule)
	closure := res.Closure(req.Paths, req.Depth)
	out := make([]string, 0, len(closure))
	for k := range closure {
		out = append(out, k)
	}
	writeJSON(w, map[string]any{"paths": out})
}

type packReq struct {
	Paths          []string `json:"paths"`
	Format         string   `json:"format"`
	Compress       bool     `json:"compress"`
	RemoveComments bool     `json:"removeComments"`
	LineNumbers    bool     `json:"lineNumbers"`
	Prompt         string   `json:"prompt"`
	Ask            string   `json:"ask"`
	Edits          bool     `json:"edits"`
	Budget         int      `json:"budget"`
}

func (s *server) pack(w http.ResponseWriter, r *http.Request) {
	var req packReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	p := s.project()
	want := map[string]bool{}
	for _, x := range req.Paths {
		want[x] = true
	}
	var files []*File
	for _, f := range p.Files {
		if want[f.Path] {
			files = append(files, f.clone())
		}
	}
	if len(files) == 0 {
		writeErr(w, errors.New("select at least one file"))
		return
	}
	o := s.opts
	o.Compress, o.RemoveComments, o.LineNumbers = req.Compress, req.RemoveComments, req.LineNumbers
	if req.Format == "" {
		req.Format = "xml"
	}
	Transform(files, o)
	in := RenderInput{Project: p.Name, Files: files, Format: req.Format, LineNums: req.LineNumbers,
		Tokenizer: encoderName, Prompt: buildPrompt(req.Prompt, req.Ask, req.Edits)}
	if req.Budget > 0 {
		overhead := measure(RenderInput{Project: in.Project, Format: in.Format, Prompt: in.Prompt, Tokenizer: in.Tokenizer}, buildTree(files)) + 150
		in.Notes = FitBudget(files, req.Budget, overhead)
	}
	parts, total := renderAll(in, 0)
	counts := map[string]int{}
	for _, f := range files {
		counts[f.Mode.String()]++
	}
	writeJSON(w, map[string]any{"output": parts[0], "tokens": total, "modes": counts, "notes": in.Notes, "ext": extFor(req.Format)})
}

func (s *server) applyPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	plan := planChanges(s.opts.Root, parseChanges(req.Text))
	writeJSON(w, map[string]any{"changes": plan})
}

func (s *server) applyRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text  string `json:"text"`
		Force bool   `json:"force"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	plan := planChanges(s.opts.Root, parseChanges(req.Text))
	backup, n, err := applyChanges(s.opts.Root, plan, req.Force)
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = s.rescan()
	rel, _ := filepath.Rel(s.opts.Root, backup)
	writeJSON(w, map[string]any{"applied": n, "backup": filepath.ToSlash(rel)})
}

func (s *server) undo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, errors.New("use POST"))
		return
	}
	rep, err := undoLast(s.opts.Root)
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = s.rescan()
	writeJSON(w, map[string]any{"report": rep})
}
