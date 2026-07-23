// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/profiles/profiles_port_test.go
//
// Port of tests/profiles.test.ts (upstream v1.9.0) granular assertions.
// Notes on intentional divergence:
//   - The Go loader does not cache by mtime (it re-scans on every Load), so the
//     two "caches result by mtime" reference-identity tests are ported as
//     behavioural freshness checks instead.
//   - Go profile handlers are anonymous closures with no runtime name, so the
//     `handler.name === "profile:mcp__jira"` assertion has no Go equivalent.

package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const jiraProfile = `
[profile]
id          = "mcp__jira"
version     = "1.0.0"
description = "Jira issues"
mcp_pattern = "mcp__jira__*"

[strategy]
type       = "json_extract"
items_path = ["issues"]
fields     = ["key", "fields.summary", "fields.status.name"]
max_items  = 5
`

const exactProfile = `
[profile]
id          = "mcp__jira__search"
version     = "1.0.0"
description = "Exact match for Jira search"
mcp_pattern = "mcp__jira__search_issues"

[strategy]
type   = "text_truncate"
max_chars = 100
`

const truncateProfile = `
[profile]
id          = "mcp__myservice"
version     = "1.0.0"
description = "Plain text truncation"
mcp_pattern = "mcp__myservice__*"

[strategy]
type      = "text_truncate"
max_chars = 50
`

const communityJira = `
[profile]
id          = "mcp__jira__community"
version     = "1.0.0"
description = "Community Jira profile"
mcp_pattern = "mcp__jira__*"
[strategy]
type   = "text_truncate"
max_chars = 200
`

// isolateDirs points all three tiers at controllable temp dirs (bundled at a
// nonexistent path so the embedded profiles don't leak into count assertions).
func isolateDirs(t *testing.T) (userDir, communityDir string) {
	t.Helper()
	userDir = t.TempDir()
	communityDir = t.TempDir()
	os.Setenv("RECALL_USER_PROFILES_PATH", userDir)
	os.Setenv("RECALL_COMMUNITY_PROFILES_PATH", communityDir)
	os.Setenv("RECALL_BUNDLED_PROFILES_PATH", filepath.Join(t.TempDir(), "no-bundled"))
	t.Cleanup(func() {
		os.Unsetenv("RECALL_USER_PROFILES_PATH")
		os.Unsetenv("RECALL_COMMUNITY_PROFILES_PATH")
		os.Unsetenv("RECALL_BUNDLED_PROFILES_PATH")
	})
	return
}

func writeProfileF(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func intptr(i int) *int { return &i }

// ── loader ─────────────────────────────────────────────────────────────────────

func TestLoadProfilesLoader(t *testing.T) {
	t.Run("empty when directory missing", func(t *testing.T) {
		isolateDirs(t)
		os.Setenv("RECALL_USER_PROFILES_PATH", filepath.Join(t.TempDir(), "no-such-dir"))
		if got := Load(); len(got) != 0 {
			t.Fatalf("got %d", len(got))
		}
	})
	t.Run("loads valid profile from user dir", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "jira.toml", jiraProfile)
		profs := Load()
		if len(profs) != 1 {
			t.Fatalf("got %d", len(profs))
		}
		if profs[0].Spec.Profile.ID != "mcp__jira" || profs[0].Tier != TierUser {
			t.Errorf("spec: %+v tier=%v", profs[0].Spec.Profile, profs[0].Tier)
		}
		if len(profs[0].Patterns) != 1 || profs[0].Patterns[0] != "mcp__jira__*" {
			t.Errorf("patterns: %v", profs[0].Patterns)
		}
	})
	t.Run("skips invalid TOML silently", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "broken.toml", "this is not valid = [[[ toml")
		if got := Load(); len(got) != 0 {
			t.Fatalf("got %d", len(got))
		}
	})
	t.Run("skips profiles missing required fields", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "bad.toml", "[profile]\nid = \"missing-fields\"\n[strategy]\ntype = \"text_truncate\"\n")
		if got := Load(); len(got) != 0 {
			t.Fatalf("got %d", len(got))
		}
	})
	t.Run("reflects fresh content on re-read", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		file := writeProfileF(t, userDir, "jira.toml", jiraProfile)
		if id := Load()[0].Spec.Profile.ID; id != "mcp__jira" {
			t.Fatalf("first: %q", id)
		}
		// Rewrite with a different id → next Load reflects it (no stale cache).
		os.WriteFile(file, []byte(strings.Replace(jiraProfile, `"mcp__jira"`, `"mcp__jira_v2"`, 1)), 0o644)
		if id := Load()[0].Spec.Profile.ID; id != "mcp__jira_v2" {
			t.Fatalf("second: %q", id)
		}
	})
}

// ── resolver ─────────────────────────────────────────────────────────────────

func TestResolveProfileFileBased(t *testing.T) {
	t.Run("null when no profile matches", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "jira.toml", jiraProfile)
		if m := resolveProfile("mcp__notion__search", Load(), tierOrder); m != nil {
			t.Errorf("expected nil, got %+v", m)
		}
	})
	t.Run("exact match beats wildcard same tier", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "jira-wildcard.toml", jiraProfile)
		writeProfileF(t, userDir, "jira-exact.toml", exactProfile)
		m := resolveProfile("mcp__jira__search_issues", Load(), tierOrder)
		if m == nil || m.Spec.Profile.ID != "mcp__jira__search" {
			t.Errorf("got %+v", m)
		}
	})
	t.Run("user tier beats community tier", func(t *testing.T) {
		userDir, communityDir := isolateDirs(t)
		writeProfileF(t, userDir, "jira.toml", jiraProfile)
		writeProfileF(t, communityDir, "jira-community.toml", communityJira)
		m := resolveProfile("mcp__jira__search_issues", Load(), tierOrder)
		if m == nil || m.Tier != TierUser || m.Spec.Profile.ID != "mcp__jira" {
			t.Errorf("got %+v", m)
		}
	})
}

// ── applyJSONExtract ───────────────────────────────────────────────────────────

func TestApplyJSONExtractGranular(t *testing.T) {
	base := Strategy{
		Type:             "json_extract",
		ItemsPath:        []string{"issues"},
		Fields:           []string{"key", "fields.summary"},
		MaxItems:         intptr(10),
		MaxCharsPerField: intptr(100),
		FallbackChars:    intptr(500),
	}

	t.Run("extracts fields from items array", func(t *testing.T) {
		out := `{"issues":[{"key":"PROJ-1","fields":{"summary":"Fix bug"}},{"key":"PROJ-2","fields":{"summary":"Add feature"}}]}`
		s := applyJSONExtract(base, out).Summary
		if !strings.Contains(s, "2 items") || !strings.Contains(s, "PROJ-1") || !strings.Contains(s, "Fix bug") {
			t.Errorf("got %q", s)
		}
	})
	t.Run("tries items_path entries in order", func(t *testing.T) {
		st := base
		st.ItemsPath = []string{"missing", "nodes"}
		out := `{"nodes":[{"key":"X-1","fields":{"summary":"hello"}}]}`
		if s := applyJSONExtract(st, out).Summary; !strings.Contains(s, "X-1") {
			t.Errorf("got %q", s)
		}
	})
	t.Run("treats root-level array as items", func(t *testing.T) {
		st := base
		st.ItemsPath = []string{}
		out := `[{"key":"A-1","fields":{"summary":"root item"}}]`
		if s := applyJSONExtract(st, out).Summary; !strings.Contains(s, "A-1") {
			t.Errorf("got %q", s)
		}
	})
	t.Run("treats single object as one-item list", func(t *testing.T) {
		st := base
		st.ItemsPath = []string{"data.issue"}
		out := `{"data":{"issue":{"key":"S-1","fields":{"summary":"single"}}}}`
		if s := applyJSONExtract(st, out).Summary; !strings.Contains(s, "S-1") {
			t.Errorf("got %q", s)
		}
	})
	t.Run("falls back to raw text on parse failure", func(t *testing.T) {
		if s := applyJSONExtract(base, "not json at all").Summary; s != "not json at all" {
			t.Errorf("got %q", s)
		}
	})
	t.Run("respects max_items cap", func(t *testing.T) {
		st := base
		st.MaxItems = intptr(2)
		out := `{"issues":[{"key":"P-0","fields":{"summary":"s0"}},{"key":"P-1","fields":{"summary":"s1"}},{"key":"P-2","fields":{"summary":"s2"}},{"key":"P-3","fields":{"summary":"s3"}},{"key":"P-4","fields":{"summary":"s4"}}]}`
		s := applyJSONExtract(st, out).Summary
		if !strings.Contains(s, "…and 3 more") || strings.Contains(s, "P-2") {
			t.Errorf("got %q", s)
		}
	})
	t.Run("uses custom labels", func(t *testing.T) {
		st := base
		st.Labels = map[string]string{"key": "Ticket", "fields.summary": "Title"}
		out := `{"issues":[{"key":"X-1","fields":{"summary":"test"}}]}`
		s := applyJSONExtract(st, out).Summary
		if !strings.Contains(s, "Ticket: X-1") || !strings.Contains(s, "Title: test") {
			t.Errorf("got %q", s)
		}
	})
}

// ── applyJSONTruncate ──────────────────────────────────────────────────────────

func TestApplyJSONTruncateGranular(t *testing.T) {
	t.Run("limits nesting depth", func(t *testing.T) {
		s := Strategy{Type: "json_truncate", MaxDepth: intptr(1), MaxArrayItems: intptr(10)}
		if got := applyJSONTruncate(s, `{"a":{"b":{"c":"deep"}}}`).Summary; !strings.Contains(got, "…") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("limits array items", func(t *testing.T) {
		s := Strategy{Type: "json_truncate", MaxDepth: intptr(3), MaxArrayItems: intptr(2)}
		if got := applyJSONTruncate(s, `{"items":[1,2,3,4,5]}`).Summary; !strings.Contains(got, "3 more") {
			t.Errorf("got %q", got)
		}
	})
}

// ── applyTextTruncate ──────────────────────────────────────────────────────────

func TestApplyTextTruncateGranular(t *testing.T) {
	t.Run("truncates at max_chars", func(t *testing.T) {
		s := Strategy{Type: "text_truncate", MaxChars: intptr(10)}
		if got := applyTextTruncate(s, "hello world this is long").Summary; got != "hello worl\n…" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("full text under limit", func(t *testing.T) {
		s := Strategy{Type: "text_truncate", MaxChars: intptr(100)}
		if got := applyTextTruncate(s, "short").Summary; got != "short" {
			t.Errorf("got %q", got)
		}
	})
}

// ── integration: LookupHandler (getProfileHandler equivalent) ─────────────────

func TestLookupHandlerIntegration(t *testing.T) {
	allTiers := []string{"user", "community", "bundled"}
	t.Run("nil for unmatched tool", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "jira.toml", jiraProfile)
		if h := LookupHandler("mcp__notion__search", allTiers); h != nil {
			t.Error("expected nil")
		}
	})
	t.Run("handler for matched tool", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "jira.toml", jiraProfile)
		if h := LookupHandler("mcp__jira__search_issues", allTiers); h == nil {
			t.Error("expected a handler")
		}
	})
	t.Run("handler produces a summary", func(t *testing.T) {
		userDir, _ := isolateDirs(t)
		writeProfileF(t, userDir, "truncate.toml", truncateProfile)
		h := LookupHandler("mcp__myservice__list", allTiers)
		if h == nil {
			t.Fatal("expected a handler")
		}
		res := h("mcp__myservice__list", "hello world this is a long response")
		if !strings.Contains(res.Summary, "hello world") || res.OriginalSize <= 0 {
			t.Errorf("res: %+v", res)
		}
	})
}
