// Package denylist decides which tool outputs must never be stored, based on
// built-in and user-configured glob patterns. Ports denylist.ts.
package denylist

import (
	"regexp"
	"strings"
	"sync"

	"mcprecall/internal/config"
)

// BuiltinPatterns are the built-in glob patterns for tools whose outputs must
// never be stored. Glob syntax: * matches any sequence of characters.
var BuiltinPatterns = []string{
	// own tools — never intercept
	"mcp__recall__*",
	// password managers — explicit entries
	"mcp__1password__*",
	"mcp__bitwarden__*",
	"mcp__lastpass__*",
	"mcp__dashlane__*",
	"mcp__keeper__*",
	"mcp__hashicorp_vault__*",
	"mcp__vault__*",
	"mcp__doppler__*",
	"mcp__infisical__*",
	// keyword patterns — credential-adjacent tool names
	"*secret*",
	"*password*",
	"*credential*",
	"*token*",
	"*api_key*",
	"*access_key*",
	"*private_key*",
	"*signing_key*",
	"*encrypt*key*",
	"*oauth*",
	"*auth_token*",
	"*authenticate*",
	"*env_var*",
	"*dotenv*",
}

var (
	regexCache   = map[string]*regexp.Regexp{}
	regexCacheMu sync.Mutex
)

// MatchesPattern matches a tool name against a glob pattern where * is a
// wildcard matching any sequence of characters. Case-sensitive; regexes cached.
func MatchesPattern(toolName, pattern string) bool {
	regexCacheMu.Lock()
	re, ok := regexCache[pattern]
	if !ok {
		parts := strings.Split(pattern, "*")
		for i, s := range parts {
			parts[i] = regexp.QuoteMeta(s)
		}
		re = regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
		regexCache[pattern] = re
	}
	regexCacheMu.Unlock()
	return re.MatchString(toolName)
}

// IsDenied reports whether the tool output should not be stored.
//
// Resolution:
//  1. If denylist.allowlist matches, the tool is always allowed.
//  2. If denylist.override_defaults is non-empty, it replaces BuiltinPatterns.
//  3. denylist.additional is always appended.
func IsDenied(toolName string, cfg config.Config) bool {
	for _, p := range cfg.Denylist.Allowlist {
		if MatchesPattern(toolName, p) {
			return false
		}
	}

	base := BuiltinPatterns
	if len(cfg.Denylist.OverrideDefaults) > 0 {
		base = cfg.Denylist.OverrideDefaults
	}

	for _, p := range base {
		if MatchesPattern(toolName, p) {
			return true
		}
	}
	for _, p := range cfg.Denylist.Additional {
		if MatchesPattern(toolName, p) {
			return true
		}
	}
	return false
}
