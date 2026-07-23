// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/projectkey/projectkey_test.go

package projectkey

import (
	"regexp"
	"testing"
)

var hex16 = regexp.MustCompile(`^[a-f0-9]{16}$`)

func TestKey(t *testing.T) {
	cwd := t.TempDir()
	k := Key(cwd)
	if !hex16.MatchString(k) {
		t.Errorf("key not 16-hex: %q", k)
	}
	if Key(cwd) != k {
		t.Error("same path should give same key")
	}
	if Key(t.TempDir()) == k {
		t.Error("different paths should give different keys")
	}
}

func TestPath(t *testing.T) {
	if len(Path(t.TempDir())) == 0 {
		t.Error("path should be non-empty")
	}
	// non-git dir → returns cwd unchanged
	nonGit := t.TempDir()
	if got := Path(nonGit); got != nonGit {
		t.Errorf("non-git path = %q, want %q", got, nonGit)
	}
}
