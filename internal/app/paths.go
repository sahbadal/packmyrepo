package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findGitRoot walks up from dir until it finds a .git folder (or file, for
// worktrees and submodules). It returns "" when dir is not inside a repo.
func findGitRoot(dir string) string {
	d := dir
	for {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// resolveTargets turns the paths a user typed into the project root and the
// parts of it to pack ("scopes"). Packing `src/api` inside a git repo keeps the
// repo as root, so root .gitignore rules apply and paths stay "src/api/...".
// No scopes means the whole root.
func resolveTargets(targets []string) (root string, scopes []string, err error) {
	if len(targets) == 0 {
		targets = []string{"."}
	}
	var abs []string
	for _, t := range targets {
		a, err := filepath.Abs(t)
		if err != nil {
			return "", nil, err
		}
		if _, err := os.Stat(a); err != nil {
			msg := fmt.Sprintf("path not found: %s", t)
			if shorthandRe.MatchString(t) {
				msg += " (to pack a GitHub repo, use its full link: https://github.com/<user>/<repo>)"
			}
			return "", nil, errors.New(msg)
		}
		abs = append(abs, a)
	}

	root = findGitRoot(dirOf(abs[0]))
	if root == "" {
		// not a git repo: the common parent folder is the project
		root = dirOf(abs[0])
		if len(abs) == 1 && root == abs[0] {
			return root, nil, nil
		}
		for _, a := range abs[1:] {
			for !isInside(root, a) {
				parent := filepath.Dir(root)
				if parent == root {
					break
				}
				root = parent
			}
		}
	}

	for _, a := range abs {
		if !isInside(root, a) {
			return "", nil, fmt.Errorf("%s is outside the project %s (pack one project at a time)", a, root)
		}
		rel, _ := filepath.Rel(root, a)
		if rel == "." {
			return root, nil, nil // the whole project was asked for
		}
		scopes = append(scopes, filepath.ToSlash(rel))
	}
	return root, scopes, nil
}

func dirOf(p string) string {
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return filepath.Dir(p)
	}
	return p
}

func isInside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false // e.g. different drives on Windows
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// inScope reports whether a slash path should be visited. Folders that lead
// to a scope are visited too, so their .gitignore files still apply.
func inScope(rel string, isDir bool, scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if rel == s || strings.HasPrefix(rel, s+"/") {
			return true
		}
		if isDir && strings.HasPrefix(s, rel+"/") {
			return true
		}
	}
	return false
}

// isScopePath is true for a scope itself or a folder leading to it. The user
// asked for these by name, so ignore rules must not hide them.
func isScopePath(rel string, scopes []string) bool {
	for _, s := range scopes {
		if rel == s || strings.HasPrefix(s, rel+"/") {
			return true
		}
	}
	return false
}

// entriesRelTo makes --entry paths relative to root. People usually type
// them relative to the folder they are in, so that is tried first.
func entriesRelTo(root string, entries []string) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = filepath.ToSlash(e)
		if a, err := filepath.Abs(e); err == nil {
			if _, err := os.Stat(a); err == nil && isInside(root, a) {
				rel, _ := filepath.Rel(root, a)
				out[i] = filepath.ToSlash(rel)
			}
		}
	}
	return out
}
