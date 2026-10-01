// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/projectkey/projectkey_test.go

package projectkey

import (
	"path/filepath"
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

// The path is recorded as project_path and drives gc's orphan decision, so it
// must be absolute for any input (upstream #213/#219). These reach the non-git
// fallback because the directories don't exist.
func TestPathAbsoluteForAnyInput(t *testing.T) {
	for _, in := range []string{"definitely-not-a-real-dir-xyz", "./also/not/real", ""} {
		if got := Path(in); !filepath.IsAbs(got) {
			t.Errorf("Path(%q) = %q, want absolute", in, got)
		}
	}
}

// Built by concatenation, not filepath.Join, which would clean the input before
// it reached Path.
func TestPathNormalisesNonGitPath(t *testing.T) {
	base := t.TempDir()
	want := filepath.Join(base, "b")
	for _, messy := range []string{base + "/a/../b", base + "/b/", base + "//b"} {
		if got := Path(messy); got != want {
			t.Errorf("Path(%q) = %q, want %q", messy, got, want)
		}
	}
	if Key(base+"/b/") != Key(want) {
		t.Error("a trailing slash must not change the key")
	}
}
