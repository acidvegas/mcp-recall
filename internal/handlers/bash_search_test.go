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
	// Long match bodies so the 40×100-char sample is smaller than 25 full lines
	// and the sample cap stays visible (upstream #262).
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "src/file%d.go:%d:  func handler%d() { %s }\n", i%3, i+1, i, strings.Repeat("x", 200))
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
	// Paths longer than the 120-char clip so 40 clipped samples beat 25 full
	// lines and the sample cap stays visible (upstream #262).
	var b strings.Builder
	for i := 0; i < 45; i++ {
		fmt.Fprintf(&b, "./internal/%spkg%d/file.go\n", strings.Repeat("deep/", 40), i)
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
		if got := NormalizeCommand(in); got != want {
			t.Errorf("NormalizeCommand(%q) = %q, want %q", in, got, want)
		}
	}
	// And routing follows the normalised form.
	wrapped := mustParseT(t, `{"command":"cd /tmp && git diff HEAD"}`)
	if got := GetBashHandler(wrapped); !samePtr(got, gitDiffHandler) {
		t.Errorf("wrapped git diff routed to %s", HandlerName(got))
	}
}

// ── never larger than the shell fallback (upstream #262) ────────────────────

func linesOf(n int, f func(i int) string) string {
	out := make([]string, n)
	for i := range out {
		out[i] = f(i)
	}
	return strings.Join(out, "\n")
}

func notLargerThanShell(t *testing.T, name string, h Handler, stdout string) {
	t.Helper()
	input := bashOut(t, stdout, "", 0)
	if s, f := len(h("Bash", input).Summary), len(shellHandler("Bash", input).Summary); s > f {
		t.Errorf("%s: summary %dB > shell %dB", name, s, f)
	}
}

func TestGrepNeverLargerThanShell(t *testing.T) {
	for _, n := range []int{1, 8, 24, 25, 26, 40, 41, 90, 200} {
		for _, pad := range []int{0, 20, 80, 160} {
			stdout := linesOf(n, func(i int) string {
				return fmt.Sprintf("src/f%d.ts:%d:%s match %d", i, i+1, strings.Repeat("x", pad), i)
			})
			notLargerThanShell(t, fmt.Sprintf("grep n=%d pad=%d", n, pad), grepHandler, stdout)
		}
	}
}

func TestGrepKeepsHeaderWhenShrinking(t *testing.T) {
	stdout := linesOf(90, func(i int) string { return fmt.Sprintf("src/f.ts:%d: x", i+1) })
	has(t, grepHandler("Bash", bashOut(t, stdout, "", 0)).Summary, "grep — 90 matches")
	notLargerThanShell(t, "grep 90 short", grepHandler, stdout)
}

func TestGrepKeepsEveryMatchUnderShellCap(t *testing.T) {
	stdout := linesOf(8, func(i int) string { return fmt.Sprintf("src/a.ts:%d: unique_token_%d_here", i+1, i) })
	s := grepHandler("Bash", bashOut(t, stdout, "", 0)).Summary
	for i := 0; i < 8; i++ {
		has(t, s, fmt.Sprintf("unique_token_%d_here", i))
	}
}

func TestGrepHighReductionOnLongLines(t *testing.T) {
	stdout := linesOf(200, func(i int) string {
		return fmt.Sprintf("src/mod%d/file.ts:%d: %s match %d", i%6, i+1, strings.Repeat("body ", 50), i)
	})
	res := grepHandler("Bash", bashOut(t, stdout, "", 0))
	has(t, res.Summary, "grep — 200 matches in 6 files")
	if r := float64(len(res.Summary)) / float64(res.OriginalSize); r >= 0.15 {
		t.Errorf("ratio %.3f, want < 0.15", r)
	}
	notLargerThanShell(t, "grep 200 long", grepHandler, stdout)
}

func TestLsNeverLargerThanShell(t *testing.T) {
	for _, n := range []int{2, 10, 24, 25, 40, 80} {
		notLargerThanShell(t, fmt.Sprintf("ls n=%d", n), lsHandler, linesOf(n, func(i int) string { return fmt.Sprintf("file%d.ts", i) }))
	}
}

func TestLsKeepsEveryNameUnderShellCap(t *testing.T) {
	s := lsHandler("Bash", bashOut(t, linesOf(8, func(i int) string { return fmt.Sprintf("unique_ls_%d.ts", i) }), "", 0)).Summary
	for i := 0; i < 8; i++ {
		has(t, s, fmt.Sprintf("unique_ls_%d.ts", i))
	}
}

func TestFindNeverLargerThanShell(t *testing.T) {
	for _, n := range []int{1, 8, 24, 25, 40, 80, 120} {
		for _, pad := range []int{0, 40, 160} {
			stdout := linesOf(n, func(i int) string { return fmt.Sprintf("./src/%s/file%d.ts", strings.Repeat("x", pad), i) })
			notLargerThanShell(t, fmt.Sprintf("find n=%d pad=%d", n, pad), findHandler, stdout)
		}
	}
}

func TestFindKeepsEveryPathUnderShellCap(t *testing.T) {
	s := findHandler("Bash", bashOut(t, linesOf(8, func(i int) string { return fmt.Sprintf("./src/unique_find_%d.ts", i) }), "", 0)).Summary
	for i := 0; i < 8; i++ {
		has(t, s, fmt.Sprintf("unique_find_%d.ts", i))
	}
}

func TestFindCapVisibleOnLongPaths(t *testing.T) {
	stdout := linesOf(120, func(i int) string { return fmt.Sprintf("./src/%sfile%d.ts", strings.Repeat("deep/", 40), i) })
	res := findHandler("Bash", bashOut(t, stdout, "", 0))
	has(t, res.Summary, "find — 120 paths")
	has(t, res.Summary, "+80 more paths")
	if len(res.Summary) >= res.OriginalSize {
		t.Error("summary not smaller than original")
	}
}
