package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// configName is the per-project settings file. Command-line flags always win
// over it, so it only sets the defaults for a project.
const configName = "packmyrepo.json"

// sizeValue accepts 50000, "50k", "1.5m" or "1MB" in JSON.
type sizeValue int64

func (s *sizeValue) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		*s = sizeValue(n)
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return errors.New("must be a number or a string like \"100k\"")
	}
	v, err := parseSize(str)
	if err != nil {
		return err
	}
	*s = sizeValue(v)
	return nil
}

// Config mirrors the pack flags. Pointers tell "not set" apart from false/0.
type Config struct {
	Format         *string    `json:"format,omitempty"`
	Output         *string    `json:"output,omitempty"`
	Include        []string   `json:"include,omitempty"`
	Ignore         []string   `json:"ignore,omitempty"`
	Compress       *bool      `json:"compress,omitempty"`
	RemoveComments *bool      `json:"removeComments,omitempty"`
	LineNumbers    *bool      `json:"lineNumbers,omitempty"`
	Budget         *sizeValue `json:"budget,omitempty"`
	Split          *sizeValue `json:"split,omitempty"`
	MaxSize        *sizeValue `json:"maxSize,omitempty"`
	Prompt         *string    `json:"prompt,omitempty"`
	Ask            *string    `json:"ask,omitempty"`
	Edits          *bool      `json:"edits,omitempty"`
	Copy           *bool      `json:"copy,omitempty"`
	KeepNoise      *bool      `json:"keepNoise,omitempty"`
	Gitignore      *bool      `json:"gitignore,omitempty"`
	Security       *bool      `json:"security,omitempty"`
	Tokenizer      *string    `json:"tokenizer,omitempty"`
	Top            *int       `json:"top,omitempty"`
	Tree           *bool      `json:"tree,omitempty"`
}

// loadConfig reads root/packmyrepo.json. A missing file is not an error; a file
// with a typo in a key is, so mistakes do not get silently ignored.
func loadConfig(root string) (*Config, string, error) {
	p := filepath.Join(root, configName)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, "", fmt.Errorf("%s: %v", configName, friendlyJSONError(data, err))
	}
	return &c, p, nil
}

func friendlyJSONError(data []byte, err error) error {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line := bytes.Count(data[:se.Offset], []byte("\n")) + 1
		return fmt.Errorf("invalid JSON on line %d (%v)", line, se)
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "json: unknown field ") {
		return fmt.Errorf("unknown setting %s (run 'packmyrepo init' to see the valid ones)", strings.TrimPrefix(msg, "json: unknown field "))
	}
	return err
}

// apply copies config values into o, skipping anything given as a flag.
// "ignore" lists are merged; everything else is replaced by the flag.
func (c *Config) apply(o *Options, set map[string]bool) {
	isSet := func(names ...string) bool {
		for _, n := range names {
			if set[n] {
				return true
			}
		}
		return false
	}
	str := func(dst *string, v *string, names ...string) {
		if v != nil && !isSet(names...) {
			*dst = *v
		}
	}
	boolean := func(dst *bool, v *bool, invert bool, names ...string) {
		if v != nil && !isSet(names...) {
			*dst = *v != invert
		}
	}
	size := func(dst *int, v *sizeValue, names ...string) {
		if v != nil && !isSet(names...) {
			*dst = int(*v)
		}
	}
	str(&o.Format, c.Format, "f", "format")
	str(&o.Output, c.Output, "o", "output")
	str(&o.Prompt, c.Prompt, "p", "prompt")
	str(&o.Ask, c.Ask, "ask")
	str(&o.Tokenizer, c.Tokenizer, "tokenizer")
	if len(c.Include) > 0 && !isSet("include") {
		o.Include = c.Include
	}
	o.Ignore = append(append([]string{}, c.Ignore...), o.Ignore...)
	boolean(&o.Compress, c.Compress, false, "c", "compress")
	boolean(&o.RemoveComments, c.RemoveComments, false, "no-comments")
	boolean(&o.LineNumbers, c.LineNumbers, false, "line-numbers")
	boolean(&o.EditFormat, c.Edits, false, "edits")
	boolean(&o.Copy, c.Copy, false, "copy")
	boolean(&o.KeepNoise, c.KeepNoise, false, "keep-noise")
	boolean(&o.NoGitignore, c.Gitignore, true, "no-gitignore")
	boolean(&o.NoSecrets, c.Security, true, "no-security")
	boolean(&o.NoTree, c.Tree, true, "no-tree")
	size(&o.Budget, c.Budget, "budget")
	size(&o.Split, c.Split, "split")
	if c.MaxSize != nil && !isSet("max-size") {
		o.MaxSize = int64(*c.MaxSize)
	}
	if c.Top != nil && !isSet("top") {
		o.Top = *c.Top
	}
}

// setFlags returns the names of flags the user actually typed.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// runInit writes a starter packmyrepo.json for the current project.
func runInit(args []string) error {
	fs := newFlagSet("init")
	var force bool
	fs.BoolVar(&force, "force", false, "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	root, _, err := resolveTargets(fs.Args())
	if err != nil {
		return err
	}
	p := filepath.Join(root, configName)
	if _, err := os.Stat(p); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", p)
	}
	text := `{
  "format": "xml",
  "include": [],
  "ignore": [],
  "compress": false,
  "removeComments": false,
  "budget": 0,
  "prompt": "",
  "copy": false,
  "keepNoise": false,
  "security": true
}
`
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s created %s\n", green("✓"), p)
	fmt.Fprintln(os.Stderr, dim("  Edit it, then just run 'packmyrepo'. Flags you type still win over this file."))
	return nil
}
