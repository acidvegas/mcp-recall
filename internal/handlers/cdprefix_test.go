// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/cdprefix_test.go
// Port of the upstream #260 vectors: newline-separated, quoted and chained
// `cd <dir>` prefixes are unwrapped before routing and fingerprinting.

package handlers

import (
	"strings"
	"testing"
	"time"
)

func cmdInput(t *testing.T, command string) any {
	t.Helper()
	b := strings.Builder{}
	b.WriteString(`{"command":`)
	b.WriteString(jsonQuote(command))
	b.WriteString(`}`)
	return mustParseT(t, b.String())
}

func jsonQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

func TestCdPrefixRouting(t *testing.T) {
	cases := []struct {
		command string
		want    Handler
	}{
		{"cd /home/u/repo\ngit diff", gitDiffHandler},
		{"cd /home/u/repo\ngrep -rn foo src/", grepHandler},
		{"cd /home/u/repo\ngh pr list", ghHandler},
		{`cd "/home/u/my repo" && git diff`, gitDiffHandler},
		{"cd '/home/u/my repo' && git log", gitLogHandler},
		{`cd /home/u/my\ repo && git status`, gitStatusHandler},
		{"cd /home/u/repo", shellHandler},
	}
	for _, c := range cases {
		if got := GetBashHandler(cmdInput(t, c.command)); !samePtr(got, c.want) {
			t.Errorf("GetBashHandler(%q) routed to %s", c.command, HandlerName(got))
		}
	}
}

func TestCdPrefixNormalize(t *testing.T) {
	cases := map[string]string{
		"cd x\ngit --no-pager show abc": "git show abc",
		"cd /home/u/repo":               "cd /home/u/repo",
		"cd /a && cd /b\ngit diff":      "git diff",
		"cd /a\ncd /b && git log":       "git log",
		"cd /a\r\ngit status":           "git status",
	}
	for in, want := range cases {
		if got := NormalizeCommand(in); got != want {
			t.Errorf("NormalizeCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

// A bare `cd` with many escapes must be disproved quickly (RE2 is linear, but
// keep upstream's guard).
func TestCdPrefixNoBacktracking(t *testing.T) {
	input := "cd /repo" + strings.Repeat(`\a`, 40)
	start := time.Now()
	if got := NormalizeCommand(input); got != input {
		t.Errorf("bare cd changed: %q", got)
	}
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Errorf("took %v", d)
	}
}

func TestCdPrefixFingerprint(t *testing.T) {
	cases := map[string]string{
		"cd /repo\ngit diff":          "git diff",
		"cd /repo\ngrep -rn foo src/": "grep",
		`cd "/my repo" && rg foo`:     "rg",
		"cd /repo":                    "cd",
	}
	for in, want := range cases {
		if got := CommandFingerprint(NormalizeCommand(in)); got != want {
			t.Errorf("fingerprint(%q) = %q, want %q", in, got, want)
		}
	}
}
