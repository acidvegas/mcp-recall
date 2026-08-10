// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/retention/retention_test.go
//
// Port of tests/retention.test.ts.

package retention

import "testing"

func TestShouldRetainFullBody(t *testing.T) {
	cases := []struct {
		name    string
		level   string
		tool    string
		command string
		want    bool
	}{
		{"full keeps reproducible bash", "full", "Bash", "git status", true},
		{"full keeps everything", "full", "Read", "", true},
		{"minimal drops mcp results", "minimal", "mcp__github__list_issues", "", false},
		{"minimal drops network bash", "minimal", "Bash", "curl https://example.com", false},

		{"balanced keeps mcp results", "balanced", "mcp__github__list_issues", "", true},
		{"balanced keeps curl", "balanced", "Bash", "curl https://example.com/api", true},
		{"balanced keeps wget", "balanced", "Bash", "wget https://example.com/f.tgz", true},
		{"balanced keeps gh api", "balanced", "Bash", "gh api repos/o/r/commits", true},
		{"balanced drops git status", "balanced", "Bash", "git status", false},
		{"balanced drops tests", "balanced", "Bash", "go test ./...", false},
		{"balanced drops grep", "balanced", "Bash", "grep -rn foo .", false},
		// gh api is a fetch; the rest of gh is local/reproducible enough.
		{"balanced drops gh pr list", "balanced", "Bash", "gh pr list", false},
		// A fetch chained behind directory changes is still a fetch.
		{"balanced unwraps single cd", "balanced", "Bash", "cd /tmp && curl https://x.dev", true},
		{"balanced unwraps chained cd", "balanced", "Bash", "cd a && cd b; curl https://x.dev", true},
		{"balanced unwraps cd before git", "balanced", "Bash", "cd /tmp && git log", false},
		// Never silently drop a tool we don't understand.
		{"balanced keeps unknown tool", "balanced", "SomeFutureTool", "", true},
		{"balanced keeps bash with no command", "balanced", "Bash", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldRetainFullBody(c.level, c.tool, c.command); got != c.want {
				t.Errorf("ShouldRetainFullBody(%q, %q, %q) = %v, want %v", c.level, c.tool, c.command, got, c.want)
			}
		})
	}
}
