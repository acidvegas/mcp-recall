// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/pinbudget_test.go
// Covers PinOutputBounded (upstream #205 / PR #239) and ForeignKeyBreakdown
// (upstream #237 / PR #241).

package db

import (
	"strings"
	"testing"
)

// mkSized builds an input whose original_size is exactly n bytes.
func mkSized(pk, tool string, n int) StoreInput {
	full := strings.Repeat("x", n)
	return StoreInput{
		ProjectKey:   pk,
		SessionID:    "sess1",
		ToolName:     tool,
		Summary:      "[s]",
		FullContent:  full,
		OriginalSize: n,
	}
}

const oneMB = 1024 * 1024

func TestPinBudgetRefusesOverCap(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	a, _ := StoreOutput(database, mkSized("proj", "t1", oneMB))
	b, _ := StoreOutput(database, mkSized("proj", "t2", oneMB))

	// Cap of 1.5 MB: the first 1 MB pin fits, the second would reach 2 MB.
	first, err := PinOutputBounded(database, a.ID, "proj", true, 1.5)
	if err != nil || !first.OK {
		t.Fatalf("first pin should succeed: %+v %v", first, err)
	}
	second, err := PinOutputBounded(database, b.ID, "proj", true, 1.5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if second.OK || second.Reason != PinOverBudget {
		t.Fatalf("second pin should be over budget, got %+v", second)
	}
	if second.PinnedBytes != oneMB || second.ItemBytes != oneMB {
		t.Errorf("byte figures = pinned %d item %d, want %d/%d", second.PinnedBytes, second.ItemBytes, oneMB, oneMB)
	}
	if second.CapBytes != int64(1.5*oneMB) {
		t.Errorf("CapBytes = %d, want %d", second.CapBytes, int64(1.5*oneMB))
	}

	// The refused row must actually be left unpinned — the bound holds at the write.
	got, _ := RetrieveOutput(database, b.ID)
	if got.Pinned != 0 {
		t.Error("over-budget item was pinned anyway")
	}
}

func TestPinBudgetUnpinNeverChecked(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	a, _ := StoreOutput(database, mkSized("proj", "t1", oneMB))
	PinOutput(database, a.ID, "proj", true) // unbounded internal pin

	// Cap far below current usage; unpinning must still succeed.
	out, err := PinOutputBounded(database, a.ID, "proj", false, 0.001)
	if err != nil || !out.OK {
		t.Fatalf("unpin should succeed regardless of cap: %+v %v", out, err)
	}
}

func TestPinBudgetRepinAlreadyPinnedNotChecked(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	a, _ := StoreOutput(database, mkSized("proj", "t1", oneMB))
	PinOutput(database, a.ID, "proj", true)

	// Already pinned: re-pinning adds no bytes, so it must not be refused even
	// when the store is already over the cap.
	out, err := PinOutputBounded(database, a.ID, "proj", true, 0.001)
	if err != nil || !out.OK {
		t.Fatalf("re-pin of an already-pinned item should succeed: %+v %v", out, err)
	}
}

func TestPinBudgetZeroCapDisablesCheck(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	a, _ := StoreOutput(database, mkSized("proj", "t1", oneMB))
	out, err := PinOutputBounded(database, a.ID, "proj", true, 0)
	if err != nil || !out.OK {
		t.Fatalf("cap of 0 should disable the check: %+v %v", out, err)
	}
}

func TestPinBudgetNotFound(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	out, err := PinOutputBounded(database, "recall_00000000", "proj", true, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.OK || out.Reason != PinNotFound {
		t.Fatalf("want not_found, got %+v", out)
	}
}

func TestPinBudgetIsPerProject(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	a, _ := StoreOutput(database, mkSized("projA", "t1", oneMB))
	b, _ := StoreOutput(database, mkSized("projB", "t2", oneMB))
	PinOutput(database, a.ID, "projA", true)

	// projA's pinned megabyte must not count against projB's budget.
	out, err := PinOutputBounded(database, b.ID, "projB", true, 1.5)
	if err != nil || !out.OK {
		t.Fatalf("other project's pins should not consume this budget: %+v %v", out, err)
	}
}

func TestGetStatsReportsPinned(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	a, _ := StoreOutput(database, mkSized("proj", "t1", 1000))
	StoreOutput(database, mkSized("proj", "t2", 500))
	PinOutput(database, a.ID, "proj", true)

	s := GetStats(database, "proj")
	if s.PinnedItems != 1 || s.PinnedBytes != 1000 {
		t.Fatalf("pinned stats = %d items / %d bytes, want 1/1000", s.PinnedItems, s.PinnedBytes)
	}
	if s.TotalItems != 2 || s.TotalOriginalBytes != 1500 {
		t.Errorf("totals regressed: %+v", s)
	}
}

func TestForeignKeyBreakdown(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	StoreOutput(database, mkSized("current", "t", 10))
	StoreOutput(database, mkSized("otherA", "t", 10))
	StoreOutput(database, mkSized("otherA", "t", 10))
	StoreOutput(database, mkSized("otherB", "t", 10))

	got := ForeignKeyBreakdown(database, "current")
	if len(got) != 2 {
		t.Fatalf("want 2 foreign keys, got %+v", got)
	}
	// Most rows first.
	if got[0].ProjectKey != "otherA" || got[0].Count != 2 {
		t.Errorf("first = %+v, want otherA/2", got[0])
	}
	if got[1].ProjectKey != "otherB" || got[1].Count != 1 {
		t.Errorf("second = %+v, want otherB/1", got[1])
	}
}

func TestForeignKeyBreakdownEmptyWhenSingleProject(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()

	StoreOutput(database, mkSized("current", "t", 10))
	if got := ForeignKeyBreakdown(database, "current"); len(got) != 0 {
		t.Fatalf("want no foreign keys, got %+v", got)
	}
}
