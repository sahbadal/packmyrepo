package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// gitChanged lists files changed compared to ref (committed + uncommitted + untracked),
// as slash paths relative to dir.
func gitChanged(dir, ref string) (changed, deleted []string, err error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, nil, fmt.Errorf("git is not installed, so --diff/--since cannot be used")
	}
	out, err := git(dir, "diff", "--relative", "--name-status", ref)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		fields := strings.Split(l, "\t")
		if len(fields) < 2 {
			continue
		}
		status, p := fields[0], fields[len(fields)-1]
		if strings.HasPrefix(status, "D") {
			deleted = append(deleted, p)
			continue
		}
		if !seen[p] {
			seen[p] = true
			changed = append(changed, p)
		}
	}
	untracked, err := git(dir, "ls-files", "--others", "--exclude-standard")
	if err == nil {
		for _, p := range strings.Split(untracked, "\n") {
			if p = strings.TrimSpace(p); p != "" && !seen[p] {
				seen[p] = true
				changed = append(changed, p)
			}
		}
	}
	return changed, deleted, nil
}

func gitDiffText(dir, ref string) string {
	out, err := git(dir, "diff", "--relative", ref)
	if err != nil {
		return ""
	}
	return out
}

func gitLogText(dir string, n int) string {
	out, err := git(dir, "log", fmt.Sprintf("-%d", n), "--date=short", "--pretty=format:%h %ad %s")
	if err != nil {
		return ""
	}
	return out
}

var shorthandRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]+/[A-Za-z0-9_.\-]+$`)

// remoteURL returns a clone URL if target looks like a remote repository.
func remoteURL(target string) (string, bool) {
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "git@") {
		return target, true
	}
	if shorthandRe.MatchString(target) {
		if _, err := os.Stat(target); err != nil {
			return "https://github.com/" + strings.TrimSuffix(target, ".git"), true
		}
	}
	return "", false
}

// cloneRemote does a shallow clone into a temp dir. The caller removes it.
func cloneRemote(url, branch string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git is needed to pack a remote repository")
	}
	// GitHub tree URLs: https://github.com/u/r/tree/branch
	if m := regexp.MustCompile(`^(https://github\.com/[^/]+/[^/]+)/tree/([^/]+)`).FindStringSubmatch(url); m != nil {
		url, branch = m[1], m[2]
	}
	dir, err := os.MkdirTemp("", "packmyrepo-*")
	if err != nil {
		return "", err
	}
	args := []string{"clone", "--depth", "1", "--quiet"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, url, dir)
	cmd := exec.Command("git", args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("could not clone %s: %s", url, strings.TrimSpace(errb.String()))
	}
	return dir, nil
}

func repoNameFromURL(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	if i := strings.Index(url, "/tree/"); i > 0 {
		url = url[:i]
	}
	return filepath.Base(strings.ReplaceAll(url, ":", "/"))
}
