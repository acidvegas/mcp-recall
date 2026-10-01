// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/config/retention_test.go
// Covers store.retention (upstream PR #246).

package config

import "testing"

func TestRetentionDefaultsToBalanced(t *testing.T) {
	withFile(t, "")
	if got := Load().Store.Retention; got != "balanced" {
		t.Fatalf("retention default = %q, want balanced", got)
	}
}

func TestRetentionAcceptsEachLevel(t *testing.T) {
	for _, level := range []string{"full", "balanced", "minimal"} {
		withFile(t, "[store]\nretention = \""+level+"\"\n")
		if got := Load().Store.Retention; got != level {
			t.Errorf("retention = %q, want %q", got, level)
		}
	}
}

// An unknown level must not silently fall back — a typo'd "none" would
// otherwise read as "keep everything" and quietly defeat the setting.
func TestRetentionRejectsUnknownLevel(t *testing.T) {
	withFile(t, "[store]\nretention = \"none\"\n")
	if got := Load().Store.Retention; got != "balanced" {
		t.Errorf("invalid config should fall back to defaults, got %q", got)
	}
}
