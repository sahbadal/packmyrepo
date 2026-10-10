package app

import (
	"path"
	"regexp"
	"strings"
)

type secretPattern struct {
	name string
	re   *regexp.Regexp
	// group is the submatch index that holds the secret value (0 = whole match).
	group int
	// hints: the regex only runs if the file contains one of these words.
	// Checking a word is ~100x cheaper than running the regex on every file.
	hints []string
	lower bool // compare hints against the lowercased file
}

var secretPatterns = []secretPattern{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), 0, []string{"PRIVATE KEY"}, false},
	{"aws-access-key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), 0, []string{"AKIA", "ASIA"}, false},
	{"github-token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`), 0, []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}, false},
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`), 0, []string{"sk-ant-"}, false},
	{"openai-key", regexp.MustCompile(`\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_\-]{20,}`), 0, []string{"sk-"}, false},
	{"stripe-key", regexp.MustCompile(`\b(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{16,}`), 0, []string{"_live_", "_test_"}, false},
	{"slack-token", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9\-]{10,}`), 0, []string{"xox"}, false},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`), 0, []string{"AIza"}, false},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`), 0, []string{"eyJ"}, false},
	{"url-password", regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^:/\s@]+:)([^@\s/]{3,})(@)`), 2, []string{"://"}, false},
	{"assigned-secret", regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|api[_\-]?key|apikey|access[_\-]?token|auth[_\-]?token|client[_\-]?secret|private[_\-]?key)["']?\s*[:=]\s*["'])([^"'\s]{8,})(["'])`), 2, []string{"pass", "pwd", "secret", "key", "token"}, true},
}

// Values that are obviously placeholders are left alone.
var placeholderRe = regexp.MustCompile(`(?i)^(?:x+|\*+|\.+|<.*>|\$\{.*\}|\{\{.*\}\}|your[_\-].*|changeme|example.*|placeholder|redacted|\[REDACTED.*)$`)

// redactSecrets replaces secret values with [REDACTED:type] and returns the kinds found.
func redactSecrets(rel, content string) (string, []string) {
	var found []string
	seen := map[string]bool{}
	note := func(k string) {
		if !seen[k] {
			seen[k] = true
			found = append(found, k)
		}
	}

	if isEnvFile(rel) {
		content = redactEnv(content, note)
	}

	lowered := ""
	for _, sp := range secretPatterns {
		sp := sp
		hay := content
		if sp.lower {
			if lowered == "" {
				lowered = strings.ToLower(content)
			}
			hay = lowered
		}
		if !containsAny(hay, sp.hints) {
			continue
		}
		if sp.group == 0 {
			content = sp.re.ReplaceAllStringFunc(content, func(string) string {
				note(sp.name)
				return "[REDACTED:" + sp.name + "]"
			})
			continue
		}
		// "key = value" patterns live on one line: only run the (slow) regex on
		// lines that contain a hint word and an '=' or ':'.
		lines := strings.Split(content, "\n")
		for i, l := range lines {
			if !strings.ContainsAny(l, "=:") {
				continue
			}
			lh := l
			if sp.lower {
				lh = strings.ToLower(l)
			}
			if !containsAny(lh, sp.hints) {
				continue
			}
			lines[i] = sp.re.ReplaceAllStringFunc(l, func(m string) string {
				sub := sp.re.FindStringSubmatch(m)
				if len(sub) <= sp.group || placeholderRe.MatchString(sub[sp.group]) {
					return m
				}
				note(sp.name)
				var b strings.Builder
				for k := 1; k < len(sub); k++ {
					if k == sp.group {
						b.WriteString("[REDACTED:" + sp.name + "]")
					} else {
						b.WriteString(sub[k])
					}
				}
				return b.String()
			})
		}
		content = strings.Join(lines, "\n")
	}
	return content, found
}

func isEnvFile(rel string) bool {
	b := path.Base(rel)
	if b == ".env.example" || b == ".env.sample" || b == ".env.template" {
		return false
	}
	return b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".env")
}

var (
	envSecretKeyRe   = regexp.MustCompile(`KEY|SECRET|TOKEN|PASS|PWD|AUTH|CRED|PRIVATE|DSN|URL|URI|DATABASE|CONN|SALT|CERT|SESSION|COOKIE|WEBHOOK|SIGN`)
	envHarmlessValRe = regexp.MustCompile(`^(?:\d{1,6}(?:\.\d+)?|true|false|yes|no|on|off|[a-z][a-z_\-]{0,15}|localhost|0\.0\.0\.0|127\.0\.0\.1)$`)
)

// redactEnv keeps every variable name but hides its value, so the AI still knows
// which settings exist.
func redactEnv(content string, note func(string)) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		eq := strings.IndexByte(l, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l[:eq]), "export ")))
		val := strings.TrimSpace(l[eq+1:])
		if val == "" || val == `""` || val == "''" {
			continue
		}
		if !envSecretKeyRe.MatchString(key) && envHarmlessValRe.MatchString(strings.Trim(val, `"'`)) {
			continue // PORT=3000, NODE_ENV=development, DEBUG=true stay readable
		}
		lines[i] = l[:eq+1] + "[REDACTED:env]"
		note("env-value")
	}
	return strings.Join(lines, "\n")
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return len(words) == 0
}
