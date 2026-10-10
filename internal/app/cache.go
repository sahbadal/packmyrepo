package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Token counting is the slowest part of a pack. Counts are cached per project
// by content hash, so a second run only tokenizes files that changed.
// The cache lives in the user's cache folder (never inside the project) and
// only keeps entries used in the last run, so it cannot grow without limit.
var tokenCache struct {
	sync.Mutex
	enabled bool
	path    string
	old     map[string]int // loaded from disk
	used    map[string]int // seen in this run (what gets saved)
}

func openTokenCache(root string) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return
	}
	h := sha256.Sum256([]byte(root + "|" + encoderName))
	tokenCache.Lock()
	defer tokenCache.Unlock()
	tokenCache.enabled = true
	tokenCache.path = filepath.Join(dir, "packmyrepo", hex.EncodeToString(h[:8])+".json")
	tokenCache.old = map[string]int{}
	tokenCache.used = map[string]int{}
	if data, err := os.ReadFile(tokenCache.path); err == nil {
		_ = json.Unmarshal(data, &tokenCache.old)
	}
}

func saveTokenCache() {
	tokenCache.Lock()
	defer tokenCache.Unlock()
	if !tokenCache.enabled || len(tokenCache.used) == 0 {
		return
	}
	data, err := json.Marshal(tokenCache.used)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(tokenCache.path), 0o755)
	tmp := tokenCache.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, tokenCache.path) // atomic: a crash never leaves half a file
	}
}

// countTokensCached is countTokens with the per-project cache in front of it.
func countTokensCached(s string) int {
	tokenCache.Lock()
	on := tokenCache.enabled
	tokenCache.Unlock()
	if !on || len(s) < 256 { // tiny strings are cheaper to count than to hash
		return countTokens(s)
	}
	sum := sha256.Sum256([]byte(s))
	key := hex.EncodeToString(sum[:12])
	tokenCache.Lock()
	n, ok := tokenCache.used[key]
	if !ok {
		n, ok = tokenCache.old[key]
	}
	tokenCache.Unlock()
	if !ok {
		n = countTokens(s)
	}
	tokenCache.Lock()
	tokenCache.used[key] = n
	tokenCache.Unlock()
	return n
}
