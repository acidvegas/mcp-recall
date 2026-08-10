// internal/tools/scope_test.go
// Covers the recall__pin budget message (upstream #205 / PR #239) and the
// recall__forget / recall__list_stored project_key override (upstream #237 / PR #241).

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcprecall/internal/config"
	"mcprecall/internal/db"
)

func mkBig(pk, tool string, n int) db.StoreInput {
	full := strings.Repeat("x", n)
	return db.StoreInput{
		ProjectKey: pk, SessionID: "sess1", ToolName: tool,
		Summary: "[s]", FullContent: full, OriginalSize: n,
	}
}

// withConfig points config at a temp TOML file for this test.
func withConfig(t *testing.T, content string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Setenv("RECALL_CONFIG_PATH", p)
	config.Reset()
	t.Cleanup(func() { os.Unsetenv("RECALL_CONFIG_PATH"); config.Reset() })
}

func TestPinRefusedOverBudgetMessage(t *testing.T) {
	d := setup(t)
	// 1 MB total cap, so the derived pinned cap is 0.5 MB.
	withConfig(t, "[store]\nmax_size_mb = 1\n")

	big, _ := db.StoreOutput(d, mkBig(projectKey, "t1", 1024*1024))
	out := Pin(d, projectKey, PinArgs{ID: big.ID})

	if !strings.Contains(out, "cannot pin") || !strings.Contains(out, "store.max_pinned_mb") {
		t.Fatalf("want over-budget refusal, got: %s", out)
	}
	stored, _ := db.RetrieveOutput(d, big.ID)
	if stored.Pinned != 0 {
		t.Error("item was pinned despite refusal")
	}
}

func TestPinSucceedsUnderBudget(t *testing.T) {
	d := setup(t)
	withConfig(t, "[store]\nmax_size_mb = 100\n")

	small, _ := db.StoreOutput(d, mkBig(projectKey, "t1", 1000))
	out := Pin(d, projectKey, PinArgs{ID: small.ID})
	if !strings.Contains(out, "pinned "+small.ID) {
		t.Fatalf("want success, got: %s", out)
	}
}

func TestStatsShowsPinBudget(t *testing.T) {
	d := setup(t)
	withConfig(t, "[store]\nmax_size_mb = 100\n")

	s, _ := db.StoreOutput(d, mkBig(projectKey, "t1", 1000))
	db.PinOutput(d, s.ID, projectKey, true)

	out := Stats(d, projectKey, StatsArgs{PinThreshold: 5, StaleDays: 3})
	if !strings.Contains(out, "Pinned:") || !strings.Contains(out, "max_pinned_mb") {
		t.Fatalf("stats should report pin budget, got:\n%s", out)
	}
}

func TestStatsWarnsNearPinCap(t *testing.T) {
	d := setup(t)
	// Pinned cap = 0.5 MB derived; pin ~0.5 MB to cross the 80% warn line.
	withConfig(t, "[store]\nmax_size_mb = 1\n")

	s, _ := db.StoreOutput(d, mkBig(projectKey, "t1", 500*1024))
	db.PinOutput(d, s.ID, projectKey, true)

	out := Stats(d, projectKey, StatsArgs{PinThreshold: 5, StaleDays: 3})
	if !strings.Contains(out, "⚠ Pinned data is") {
		t.Fatalf("want pin-cap warning, got:\n%s", out)
	}
}

// ── project_key override ──────────────────────────────────────────────────────

func TestListStoredShowsForeignKeyFooter(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig(projectKey, "t1", 100))
	db.StoreOutput(d, mkBig("strandedkey", "t2", 100))

	out := ListStored(d, projectKey, ListStoredArgs{})
	if !strings.Contains(out, "under other project key") || !strings.Contains(out, "strandedkey (1)") {
		t.Fatalf("want foreign-key footer, got:\n%s", out)
	}
}

func TestListStoredNoFooterWhenNoForeignRows(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig(projectKey, "t1", 100))

	out := ListStored(d, projectKey, ListStoredArgs{})
	if strings.Contains(out, "under other project key") {
		t.Fatalf("unexpected footer:\n%s", out)
	}
}

func TestListStoredOverrideListsForeignRows(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig(projectKey, "t1", 100))
	db.StoreOutput(d, mkBig("strandedkey", "mcp__foreign__tool", 100))

	out := ListStored(d, projectKey, ListStoredArgs{ProjectKey: "strandedkey"})
	if !strings.Contains(out, "mcp__foreign__tool") {
		t.Fatalf("override should list the foreign row, got:\n%s", out)
	}
	// Already scoped to the foreign key — no discovery footer.
	if strings.Contains(out, "under other project key") {
		t.Errorf("footer should be suppressed under an explicit override:\n%s", out)
	}
}

func TestListStoredOverrideUnknownKeyNamesRealKeys(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig(projectKey, "t1", 100))
	db.StoreOutput(d, mkBig("strandedkey", "t2", 100))

	out := ListStored(d, projectKey, ListStoredArgs{ProjectKey: "typoed"})
	if !strings.Contains(out, "no stored items under project key typoed") {
		t.Fatalf("want scoped empty message, got:\n%s", out)
	}
	if !strings.Contains(out, "strandedkey (1)") {
		t.Errorf("a typo should be distinguishable from empty — want key hint, got:\n%s", out)
	}
}

func TestForgetOverrideRequiresSelector(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig("strandedkey", "t2", 100))

	out := Forget(d, projectKey, ForgetArgs{ProjectKey: "strandedkey"})
	if !strings.Contains(out, "project_key needs a selector") {
		t.Fatalf("want selector-required error, got: %s", out)
	}
	// Nothing may be deleted by a rejected call.
	if got := db.ForeignKeyBreakdown(d, projectKey); len(got) != 1 || got[0].Count != 1 {
		t.Errorf("rejected call must not delete, breakdown = %+v", got)
	}
}

func TestForgetOverrideDeletesForeignRows(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig(projectKey, "t1", 100))
	db.StoreOutput(d, mkBig("strandedkey", "t2", 100))
	db.StoreOutput(d, mkBig("strandedkey", "t3", 100))

	out := Forget(d, projectKey, ForgetArgs{ProjectKey: "strandedkey", All: true, Confirmed: true})
	if !strings.Contains(out, "deleted 2 items under project key strandedkey") {
		t.Fatalf("want scoped delete confirmation, got: %s", out)
	}
	if got := db.ForeignKeyBreakdown(d, projectKey); len(got) != 0 {
		t.Errorf("foreign rows should be gone, got %+v", got)
	}
	// The current project's row must be untouched.
	if items := db.ListStoredSorted(d, projectKey, "", "", 10, 0); len(items) != 1 {
		t.Errorf("current project rows were affected: %d remain", len(items))
	}
}

func TestForgetOverrideUnknownKeyNamesRealKeys(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig("strandedkey", "t2", 100))

	out := Forget(d, projectKey, ForgetArgs{ProjectKey: "typoed", All: true, Confirmed: true})
	if !strings.Contains(out, "no items matched under project key typoed") {
		t.Fatalf("want scoped empty message, got: %s", out)
	}
	if !strings.Contains(out, "strandedkey (1)") {
		t.Errorf("want key hint on a likely typo, got: %s", out)
	}
}

func TestForgetBlankOverrideRejected(t *testing.T) {
	d := setup(t)
	out := Forget(d, projectKey, ForgetArgs{ProjectKey: "   ", All: true, Confirmed: true})
	if !strings.Contains(out, "project_key must be a non-empty key") {
		t.Fatalf("want blank-key rejection, got: %s", out)
	}
}

func TestForgetWithoutOverrideUnchanged(t *testing.T) {
	d := setup(t)
	db.StoreOutput(d, mkBig(projectKey, "t1", 100))

	out := Forget(d, projectKey, ForgetArgs{All: true, Confirmed: true})
	if !strings.Contains(out, "deleted 1 item]") {
		t.Fatalf("default scope message regressed: %s", out)
	}
}
