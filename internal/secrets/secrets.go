// Package secrets detects credential-like patterns in tool output so they are
// never persisted. Any match prevents storage regardless of denylist settings.
//
// Go's regexp engine (RE2) has no lookbehind or lookahead. Upstream's
// `(?<![A-Za-z0-9_-])` left boundary becomes a consumed `(?:^|[^A-Za-z0-9_-])`
// prefix, which answers "is there a match" identically. The OpenAI pattern's
// `(?!ant-)(?!or-v1-)` lookaheads are reproduced in code (see openAIKey).
package secrets

import (
	"regexp"
	"strings"
)

type pattern struct {
	name  string
	match func(string) bool
}

func re(p string) func(string) bool {
	r := regexp.MustCompile(p)
	return r.MatchString
}

// leftBoundary stands in for upstream's (?<![A-Za-z0-9_-]) lookbehind: it fixed
// "ri|sk-…", "ta|sk-…" slugs matching as keys (upstream #274/#280).
const leftBoundary = `(?:^|[^A-Za-z0-9_-])`

// openAICandidate has upstream's three arms, most-specific first:
//  1. the T3BlbkFJ watermark every modern key embeds, prefix-agnostic so an
//     unknown future sk-<newtype>- key is still caught;
//  2. known prefixes with a permissive base64url body (the backstop for 1);
//  3. legacy bare sk-: 32+ hyphen-free base62, which rejects standalone slugs
//     like "sk-project-notes-…" (upstream #276).
var openAICandidate = regexp.MustCompile(leftBoundary +
	`sk-((?:[A-Za-z0-9_-]{16,120}T3BlbkFJ[A-Za-z0-9_-]{16,120}|(?:proj|svcacct|admin|None)-[A-Za-z0-9_-]{20,}|[A-Za-z0-9]{32,}))`)

// openAIKey reproduces upstream's `sk-(?!ant-)(?!or-v1-)` lookaheads so an
// Anthropic or OpenRouter key is never labelled OpenAI. The watermark arm can
// match "sk-ant-…T3BlbkFJ…", so this check is not redundant. Every arm body is
// in [A-Za-z0-9_-], so a rejected candidate cannot hide a valid key: any sk-
// inside it has no left boundary.
func openAIKey(content string) bool {
	for _, m := range openAICandidate.FindAllStringSubmatch(content, -1) {
		body := m[1]
		if !strings.HasPrefix(body, "ant-") && !strings.HasPrefix(body, "or-v1-") {
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
	{"OpenRouter API key", re(leftBoundary + `sk-or-v1-[A-Za-z0-9_-]{20,}`)},
	{"AWS access key ID", re(`AKIA[0-9A-Z]{16}`)},
	{"AWS secret access key", re(`(?i)aws.{0,20}secret.{0,20}[A-Za-z0-9/+=]{40}`)},
	{"Anthropic API key", re(leftBoundary + `sk-ant-[A-Za-z0-9\-_]{32,}`)},
	// Two arms (upstream #280): a JWT (eyJ = base64 `{"`), or an opaque
	// hyphen-free, slash-free token — those separators are what prose like
	// "Bearer authentication-for-all-…" uses. Residual miss: base64 with `/`.
	{"Generic Bearer token", re(`Bearer (?:eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|[A-Za-z0-9+]{32,}={0,2})`)},
	{"SSH private key", re(`-----BEGIN OPENSSH PRIVATE KEY-----`)},
	{"GCP service account key", re(`"type"\s*:\s*"service_account"`)},
	{"Azure storage connection string", re(`DefaultEndpointsProtocol=https?;AccountName=[^;]{1,100};AccountKey=[A-Za-z0-9+/=]{32,}`)},
	{"Stripe secret/restricted key", re(leftBoundary + `[sr]k_(?:live|test)_[A-Za-z0-9]{24,}`)},
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
