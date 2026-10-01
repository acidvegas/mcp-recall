// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/fingerprint_test.go
// Port of the commandFingerprint vectors from upstream tests/handlers.test.ts
// (#251 per-command savings attribution, #252 review fixes).

package handlers

import "testing"

func TestCommandFingerprint(t *testing.T) {
	cases := []struct{ in, want string }{
		// leading verb for a plain command
		{"rg foobar src/", "rg"},
		{"tsc --noEmit", "tsc"},
		{"cat file.txt", "cat"},
		// subcommand for dispatcher tools
		{"git diff HEAD~1", "git diff"},
		{"cargo build --release", "cargo build"},
		{"docker ps -a", "docker ps"},
		{"go test ./...", "go test"},
		// env-var assignments stripped, value discarded
		{"RECALL_DEBUG=1 bun test", "bun test"},
		{"TOKEN=sk-secret-xyz rg pattern", "rg"},
		// never captures an argument, URL, header value, or secret
		{`curl -H "Authorization: Bearer sk-live-abc123" https://api`, "curl"},
		{"psql postgres://user:pw@host:5432/db", "psql"},
		{"aws s3 cp s3://bucket/secret .", "aws"},
		{"mysql -uroot -phunter2", "mysql"},
		{"git clone https://tok:x@github.com/o/r", "git clone"},
		// no bare leading token → unknown
		{"./deploy.sh --prod", ""},
		{"$(which node) app.js", ""},
		{"", ""},
		// glued shell operators terminate a token
		{"ls;pwd", "ls"},
		{"make&&./run", "make"},
		{"rg foo|wc -l", "rg"},
		// irregular whitespace before the subcommand
		{"git   diff --stat", "git diff"},
		{"git\tdiff", "git diff"},
		// wrapper prefixes skipped; a flagged wrapper keeps its name
		{"sudo systemctl restart pg", "systemctl"},
		{"sudo git diff", "git diff"},
		{"time cargo build", "cargo build"},
		{"sudo -u www git diff", "sudo"},
	}
	for _, c := range cases {
		if got := CommandFingerprint(c.in); got != c.want {
			t.Errorf("CommandFingerprint(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Pairs with NormalizeCommand so git global flags don't pollute the fingerprint.
func TestCommandFingerprintAfterNormalize(t *testing.T) {
	for _, in := range []string{"git --no-pager diff", "git -C /some/repo diff HEAD~1"} {
		if got := CommandFingerprint(NormalizeCommand(in)); got != "git diff" {
			t.Errorf("%q → %q, want \"git diff\"", in, got)
		}
	}
}
