// Package secrets detects credential-like patterns in tool output so they are
// never persisted. Any match prevents storage regardless of denylist settings.
//
// Go's regexp engine (RE2) has no negative lookahead, which the original's
// OpenAI-key pattern (sk-(?!ant-)…) relies on. That single case is reproduced
// in code (see openAIKey) so detection behaviour is identical.
package secrets

import "regexp"

type pattern struct {
	name  string
	match func(string) bool
}

func re(p string) func(string) bool {
	r := regexp.MustCompile(p)
	return r.MatchString
}

// openAIKey matches sk-<32+ word/hyphen chars> but NOT sk-ant-… (Anthropic),
// reproducing the original's `sk-(?!ant-)[\w-]{32,}` negative lookahead.
var openAICandidate = regexp.MustCompile(`sk-[\w-]{32,}`)

func openAIKey(content string) bool {
	for _, m := range openAICandidate.FindAllString(content, -1) {
		if len(m) < 7 || m[:7] != "sk-ant-" {
			return true
		}
	}
	return false
}

// patterns mirrors SECRET_PATTERNS from the original, in the same order.
var patterns = []pattern{
	{"PEM private key", re(`-----BEGIN .{0,20}PRIVATE KEY-----`)},
	{"GitHub PAT (classic)", re(`ghp_[A-Za-z0-9]{36}`)},
	{"GitHub PAT (fine-grained)", re(`github_pat_[A-Za-z0-9_]{82}`)},
	{"GitHub OAuth token", re(`gho_[A-Za-z0-9]{36}`)},
	{"OpenAI API key", openAIKey},
	{"AWS access key ID", re(`AKIA[0-9A-Z]{16}`)},
	{"AWS secret access key", re(`(?i)aws.{0,20}secret.{0,20}[A-Za-z0-9/+=]{40}`)},
	{"Anthropic API key", re(`sk-ant-[A-Za-z0-9\-_]{32,}`)},
	{"Generic Bearer token", re(`Bearer [A-Za-z0-9\-._~+/]{32,}`)},
	{"SSH private key", re(`-----BEGIN OPENSSH PRIVATE KEY-----`)},
	{"GCP service account key", re(`"type"\s*:\s*"service_account"`)},
	{"Azure storage connection string", re(`DefaultEndpointsProtocol=https?;AccountName=[^;]{1,100};AccountKey=[A-Za-z0-9+/=]{32,}`)},
	{"Stripe secret/restricted key", re(`[sr]k_(?:live|test)_[A-Za-z0-9]{24,}`)},
	{"SendGrid API key", re(`SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`)},
	{"Twilio Account SID", re(`\bAC[0-9a-f]{32}\b`)},
	{"npm publish token", re(`npm_[A-Za-z0-9]{36}`)},
}

// Contains reports whether the content contains any known secret pattern.
func Contains(content string) bool {
	for _, p := range patterns {
		if p.match(content) {
			return true
		}
	}
	return false
}

// Find returns the names of all secret patterns matched in the content, in
// declaration order. Used for logging without exposing the matched value.
func Find(content string) []string {
	var names []string
	for _, p := range patterns {
		if p.match(content) {
			names = append(names, p.name)
		}
	}
	return names
}
