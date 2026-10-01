// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/config/maxpinned_test.go
// Covers store.max_pinned_mb (upstream #205 / PR #239).

package config

import "testing"

func TestMaxPinnedDefault(t *testing.T) {
	withFile(t, "")
	if got := Load().Store.MaxPinnedMB; got != 250 {
		t.Fatalf("max_pinned_mb default = %g, want 250", got)
	}
}

func TestMaxPinnedDerivedFromMaxSize(t *testing.T) {
	// Lowering max_size_mb alone must pull the pinned cap down with it, so the
	// static 250 default can never exceed a user-lowered total cap.
	withFile(t, "[store]\nmax_size_mb = 100\n")
	c := Load()
	if c.Store.MaxSizeMB != 100 {
		t.Fatalf("max_size_mb = %g, want 100", c.Store.MaxSizeMB)
	}
	if c.Store.MaxPinnedMB != 50 {
		t.Fatalf("max_pinned_mb = %g, want 50 (half of max_size_mb)", c.Store.MaxPinnedMB)
	}
}

func TestMaxPinnedExplicitWins(t *testing.T) {
	withFile(t, "[store]\nmax_size_mb = 100\nmax_pinned_mb = 10\n")
	if got := Load().Store.MaxPinnedMB; got != 10 {
		t.Fatalf("max_pinned_mb = %g, want 10", got)
	}
}

func TestMaxPinnedFractionalAccepted(t *testing.T) {
	withFile(t, "[store]\nmax_pinned_mb = 0.5\n")
	if got := Load().Store.MaxPinnedMB; got != 0.5 {
		t.Fatalf("max_pinned_mb = %g, want 0.5", got)
	}
}

func TestMaxPinnedNonPositiveRejected(t *testing.T) {
	withFile(t, "[store]\nmax_pinned_mb = 0\n")
	c := Load()
	if c.Store.MaxPinnedMB != 250 || c.Store.MaxSizeMB != 500 {
		t.Fatalf("non-positive max_pinned_mb should fall back to defaults, got %+v", c.Store)
	}
}

func TestMaxPinnedExceedingMaxSizeRejectsWholeConfig(t *testing.T) {
	// A pinned cap above the total cap is a contradiction: max_pinned_mb could
	// never bind before max_size_mb does. Reject to defaults, not to a clamp.
	withFile(t, "[store]\nmax_size_mb = 10\nmax_pinned_mb = 20\nstale_item_days = 99\n")
	c := Load()
	if c.Store.MaxSizeMB != 500 || c.Store.MaxPinnedMB != 250 {
		t.Fatalf("contradiction should reset store to defaults, got %+v", c.Store)
	}
	if c.Store.StaleItemDays != 3 {
		t.Errorf("whole config should reset, but stale_item_days = %d", c.Store.StaleItemDays)
	}
}

func TestGCReminderDefault(t *testing.T) {
	withFile(t, "")
	if got := Load().Store.GCReminderMB; got != 2048 {
		t.Fatalf("gc_reminder_mb default = %g, want 2048", got)
	}
}

func TestGCReminderZeroAllowedToDisable(t *testing.T) {
	// Non-negative, not positive: 0 is the documented way to turn the nudge off,
	// so it must survive validation rather than resetting the config.
	withFile(t, "[store]\ngc_reminder_mb = 0\nstale_item_days = 9\n")
	c := Load()
	if c.Store.GCReminderMB != 0 {
		t.Fatalf("gc_reminder_mb = %g, want 0", c.Store.GCReminderMB)
	}
	if c.Store.StaleItemDays != 9 {
		t.Errorf("config was wrongly reset to defaults: stale_item_days = %d", c.Store.StaleItemDays)
	}
}

func TestGCReminderNegativeRejected(t *testing.T) {
	withFile(t, "[store]\ngc_reminder_mb = -1\n")
	if got := Load().Store.GCReminderMB; got != 2048 {
		t.Fatalf("negative gc_reminder_mb should fall back to defaults, got %g", got)
	}
}

func TestMaxPinnedEqualToMaxSizeAllowed(t *testing.T) {
	withFile(t, "[store]\nmax_size_mb = 10\nmax_pinned_mb = 10\n")
	c := Load()
	if c.Store.MaxSizeMB != 10 || c.Store.MaxPinnedMB != 10 {
		t.Fatalf("equal caps should be accepted, got %+v", c.Store)
	}
}

// TOML accepts `nan`; NaN fails every comparison, so a `<= 0` check would let it
// through and silently disable the cap. Upstream's zod z.number() rejects NaN.
func TestNaNRejected(t *testing.T) {
	for _, key := range []string{"max_size_mb", "max_pinned_mb", "gc_reminder_mb"} {
		withFile(t, "[store]\n"+key+" = nan\nstale_item_days = 9\n")
		c := Load()
		if c.Store.StaleItemDays != 3 || c.Store.MaxSizeMB != 500 || c.Store.MaxPinnedMB != 250 || c.Store.GCReminderMB != 2048 {
			t.Errorf("%s = nan should reset config to defaults, got %+v", key, c.Store)
		}
	}
}
