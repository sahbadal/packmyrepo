package app

import (
	"fmt"
	"strings"
)

// diffLimit caps how many diff lines are kept per file, so a rewritten
// 5,000-line file does not flood the terminal or the browser.
const diffLimit = 400

// unifiedDiff returns the changes from oldS to newS as unified-diff lines
// ("@@ -a,b +c,d @@", " same", "-removed", "+added") with ctx lines of
// context. It returns nil when the texts are equal, and a one-line summary
// when the files are too large to compare line by line.
func unifiedDiff(oldS, newS string, ctx int) []string {
	a := splitLines(oldS)
	b := splitLines(newS)

	// Most edits touch a small part of a file: skip the shared start and end
	// so the expensive comparison only runs on the part that changed.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	midA, midB := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if len(midA) == 0 && len(midB) == 0 {
		return nil
	}
	if len(midA)*len(midB) > 4_000_000 {
		return []string{fmt.Sprintf("@@ too large to show line by line: %d lines removed or changed, %d lines added @@", len(midA), len(midB))}
	}

	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var ops []op
	for i := 0; i < pre; i++ {
		ops = append(ops, op{' ', a[i]})
	}
	// longest common subsequence of the changed middle part
	n, m := len(midA), len(midB)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if midA[i] == midB[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && midA[i] == midB[j]:
			ops = append(ops, op{' ', midA[i]})
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] > lcs[i+1][j]):
			ops = append(ops, op{'+', midB[j]})
			j++
		default:
			ops = append(ops, op{'-', midA[i]})
			i++
		}
	}
	for k := len(a) - suf; k < len(a); k++ {
		ops = append(ops, op{' ', a[k]})
	}

	// group the changes into hunks with ctx lines of context around them
	var out []string
	oldLine, newLine := make([]int, len(ops)), make([]int, len(ops))
	o, nw := 1, 1
	for k, p := range ops {
		oldLine[k], newLine[k] = o, nw
		if p.kind != '+' {
			o++
		}
		if p.kind != '-' {
			nw++
		}
	}
	for k := 0; k < len(ops); {
		if ops[k].kind == ' ' {
			k++
			continue
		}
		start := k - ctx
		if start < 0 {
			start = 0
		}
		end := k
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			// stop when the next change is more than 2*ctx lines away
			gap := end
			for gap < len(ops) && ops[gap].kind == ' ' {
				gap++
			}
			if gap == len(ops) || gap-end > 2*ctx {
				end += ctx
				if end > len(ops) {
					end = len(ops)
				}
				break
			}
			end = gap
		}
		var oldCount, newCount int
		for _, p := range ops[start:end] {
			if p.kind != '+' {
				oldCount++
			}
			if p.kind != '-' {
				newCount++
			}
		}
		out = append(out, fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldLine[start], oldCount, newLine[start], newCount))
		for _, p := range ops[start:end] {
			out = append(out, string(p.kind)+p.text)
		}
		k = end
	}
	if len(out) > diffLimit {
		more := len(out) - diffLimit
		out = append(out[:diffLimit], fmt.Sprintf("@@ … %d more diff lines not shown @@", more))
	}
	return out
}

// newFileDiff shows the start of a file that does not exist yet.
func newFileDiff(content string, max int) []string {
	lines := splitLines(content)
	word := "lines"
	if len(lines) == 1 {
		word = "line"
	}
	out := []string{fmt.Sprintf("@@ new file, %d %s @@", len(lines), word)}
	for k, l := range lines {
		if k == max {
			out = append(out, fmt.Sprintf("@@ … %d more lines @@", len(lines)-max))
			break
		}
		out = append(out, "+"+l)
	}
	return out
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
