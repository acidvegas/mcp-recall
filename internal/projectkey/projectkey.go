// Package projectkey resolves a stable per-project key for a working directory.
// Prefers the git root (stable across launch locations), falling back to cwd.
// Ports project-key.ts.
package projectkey

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
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

	// The fallback is absolutised rather than used verbatim. cwd arrives from a
	// hook payload that is only cast, never validated, so it can be relative or
	// "" — and this value is recorded as project_path, which `mcprecall gc` uses
	// to decide whether a project still exists. A relative path there is
	// un-rootable: it reads as "parent survived, project deleted" and would get
	// the database removed. filepath.Abs("") yields the process cwd — plausible
	// rather than certain, but absolute and therefore reasoned about honestly
	// downstream.
	//
	// git's --show-toplevel output is already absolute and normalised, and is
	// left untouched: passing it through Abs could alter the string for some
	// paths and re-key every existing git project's database.
	var resolved string
	if trimmed := strings.TrimSpace(string(out)); err == nil && trimmed != "" {
		resolved = trimmed
	} else if cwd != "" {
		// Abs also cleans, so `/x/a/../b` and `/x/b/` key the same as `/x/b`.
		resolved = cwd
		if abs, absErr := filepath.Abs(cwd); absErr == nil {
			resolved = abs
		}
	} else if wd, wdErr := os.Getwd(); wdErr == nil {
		resolved = wd
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
