// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/bash_search_test.go
//
// Covers command-aware search/listing compression (upstream PR #244).

package handlers

import (
	"fmt"
	"strings"
	"testing"
)

func TestGrepHandlerCountsMatchesAndFiles(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "src/file%d.go:%d:  func handler%d() {\n", i%3, i+1, i)
	}
	s := grepHandler("Bash", bashOut(t, b.String(), "", 0)).Summary
	has(t, s, "grep — 60 matches in 3 files")
	has(t, s, "src/file0.go:1: func handler0()")
	// Never drops silently: the overflow beyond the 40-line sample is stated.
	has(t, s, "… (+20 more matches)")
}

func TestGrepHandlerNoMatches(t *testing.T) {
	has(t, grepHandler("Bash", bashOut(t, "", "", 1)).Summary, "no matches")
}

// Output that isn't predominantly file:line:content must fall back to shell
// rather than be parsed wrongly.
func TestGrepHandlerFallsBackOnGroupedOutput(t *testing.T) {
	body := "src/main.go\n12: matched line\n\nsrc/other.go\n40: another\n"
	s := grepHandler("Bash", bashOut(t, body, "", 0)).Summary
	hasNot(t, s, "grep —")
}

func TestLsHandlerLongFormat(t *testing.T) {
	body := "total 24\n" +
		"drwxr-xr-x  5 me staff  160 Aug  1 10:00 internal\n" +
		"-rw-r--r--  1 me staff 1024 Aug  1 10:00 main.go\n" +
		"-rw-r--r--  1 me staff  512 Aug  1 10:00 README.md\n"
	s := lsHandler("Bash", bashOut(t, body, "", 0)).Summary
	has(t, s, "ls — 3 entries (1 dir, 2 files)")
	has(t, s, "internal")
	has(t, s, "main.go")
}

func TestLsHandlerRecursive(t *testing.T) {
	body := ".:\nmain.go\nREADME.md\n\n./internal:\ndb.go\ntools.go\n"
	s := lsHandler("Bash", bashOut(t, body, "", 0)).Summary
	has(t, s, "ls -R — 2 directories")
	has(t, s, "./internal:")
}

// A plain listing whose names merely end in ":" must not be misread as
// recursive — that would hide the files. The separator guard is what prevents
// it, so this fixture has no blank line (note: a trailing newline counts as
// one, upstream included).
func TestLsHandlerPlainListingNotTreatedAsRecursive(t *testing.T) {
	body := "alpha:\nbeta:\ngamma"
	s := lsHandler("Bash", bashOut(t, body, "", 0)).Summary
	has(t, s, "ls — 3 entries")
	hasNot(t, s, "ls -R")
}

func TestLsHandlerEmpty(t *testing.T) {
	has(t, lsHandler("Bash", bashOut(t, "", "", 0)).Summary, "ls — empty")
}

func TestFindHandlerCountsPaths(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 45; i++ {
		fmt.Fprintf(&b, "./internal/pkg%d/file.go\n", i)
	}
	s := findHandler("Bash", bashOut(t, b.String(), "", 0)).Summary
	has(t, s, "find — 45 paths")
	has(t, s, "… (+5 more paths)")
}

func TestFindHandlerFallsBackOnDiagnostics(t *testing.T) {
	body := "find: ‘/root’: Permission denied\nfind: ‘/proc’: Permission denied\n./ok.go\n"
	s := findHandler("Bash", bashOut(t, body, "", 1)).Summary
	hasNot(t, s, "find — 3 paths")
}

func TestFindHandlerNoResults(t *testing.T) {
	has(t, findHandler("Bash", bashOut(t, "", "", 1)).Summary, "no results")
}

// ── git ref listings ─────────────────────────────────────────────────────────

func TestGitRefsBranch(t *testing.T) {
	body := "* main\n  feat/one\n  feat/two\n  remotes/origin/main\n"
	s := gitRefsHandler("Bash", bashOut(t, body, "", 0)).Summary
	has(t, s, "git branch — 3 local, 1 remote (on main)")
	has(t, s, "feat/one")
}

func TestGitRefsStashList(t *testing.T) {
	body := "stash@{0}: WIP on main: abc123 msg\nstash@{1}: WIP on dev: def456 msg\n"
	s := gitRefsHandler("Bash", bashOut(t, body, "", 0)).Summary
	has(t, s, "git stash — 2 entries")
}

func TestGitRefsRemote(t *testing.T) {
	body := "origin\thttps://github.com/o/r.git (fetch)\norigin\thttps://github.com/o/r.git (push)\n"
	s := gitRefsHandler("Bash", bashOut(t, body, "", 0)).Summary
	has(t, s, "git remote — 1 remote")
	has(t, s, "origin → https://github.com/o/r.git")
}

// ── dispatch ─────────────────────────────────────────────────────────────────

func TestBashDispatchNewHandlers(t *testing.T) {
	cmd := func(c string) any { return mustParseT(t, `{"command":`+mustCompact(t, c)+`}`) }
	cases := []struct {
		command string
		want    Handler
	}{
		{"cargo build --release", compilerDiagnosticsHandler},
		{"cargo clippy", compilerDiagnosticsHandler},
		{"go build ./...", compilerDiagnosticsHandler},
		{"go vet ./...", compilerDiagnosticsHandler},
		{"tsc --noEmit", compilerDiagnosticsHandler},
		{"npx eslint src/", compilerDiagnosticsHandler},
		{"ruff check .", compilerDiagnosticsHandler},
		{"pnpm run typecheck", compilerDiagnosticsHandler},
		{"grep -rn foo .", grepHandler},
		{"rg pattern", grepHandler},
		{"git grep pattern", grepHandler},
		{"ls -la", lsHandler},
		{"find . -name '*.go'", findHandler},
		{"fd '\\.go$'", findHandler},
		{"git branch -a", gitRefsHandler},
		{"git stash list", gitRefsHandler},
		{"git remote -v", gitRefsHandler},
		// go test must still route to the test runner, not the compiler handler.
		{"go test ./...", testRunnerHandler},
	}
	for _, c := range cases {
		if got := GetBashHandler(cmd(c.command)); !samePtr(got, c.want) {
			t.Errorf("GetBashHandler(%q) routed to %s", c.command, HandlerName(got))
		}
	}
}

// normalizeCommand unwraps `cd … &&` and git global options so routing sees the
// real subcommand.
func TestNormalizeCommand(t *testing.T) {
	cases := map[string]string{
		"cd /tmp && git diff HEAD":       "git diff HEAD",
		"cd /tmp; git status":            "git status",
		"git --no-pager diff":            "git diff",
		"git -C /repo log --oneline":     "git log --oneline",
		"git -c core.pager=cat log":      "git log",
		"git --no-pager -C /repo status": "git status",
		"ls -la":                         "ls -la",
	}
	for in, want := range cases {
		if got := normalizeCommand(in); got != want {
			t.Errorf("normalizeCommand(%q) = %q, want %q", in, got, want)
		}
	}
	// And routing follows the normalised form.
	wrapped := mustParseT(t, `{"command":"cd /tmp && git diff HEAD"}`)
	if got := GetBashHandler(wrapped); !samePtr(got, gitDiffHandler) {
		t.Errorf("wrapped git diff routed to %s", HandlerName(got))
	}
}
