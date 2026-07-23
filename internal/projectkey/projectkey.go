// Package projectkey resolves a stable per-project key for a working directory.
// Prefers the git root (stable across launch locations), falling back to cwd.
// Ports project-key.ts.
package projectkey

import (
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
	"sync"
)

var (
	pathCache   = map[string]string{}
	pathCacheMu sync.Mutex
)

// Key returns a 16-char hex hash of the resolved project path.
func Key(cwd string) string {
	return hashPath(resolvePath(cwd))
}

// Path returns the raw project path (git root or cwd) without hashing.
func Path(cwd string) string {
	return resolvePath(cwd)
}

func resolvePath(cwd string) string {
	pathCacheMu.Lock()
	if cached, ok := pathCache[cwd]; ok {
		pathCacheMu.Unlock()
		return cached
	}
	pathCacheMu.Unlock()

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	resolved := cwd
	if err == nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			resolved = trimmed
		}
	}

	pathCacheMu.Lock()
	pathCache[cwd] = resolved
	pathCacheMu.Unlock()
	return resolved
}

func hashPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:16]
}
