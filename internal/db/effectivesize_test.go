// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/effectivesize_test.go
// Covers effective-size accounting under store.retention (upstream #247): a
// summary-only row counts at summary_size for eviction and the pin budget,
// while the savings figures keep reporting original_size.

package db

import (
	"fmt"
	"testing"
)

func mkRetained(pk, summary string, originalSize, retained int) StoreInput {
	in := mkSized(pk, "t", originalSize)
	in.Summary = summary
	in.FullRetained = &retained
	return in
}

func TestEvictionCountsSummaryOnlyAtEffectiveSize(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()
	const capMB = 0.0008 // ~838 bytes

	for i := 0; i < 3; i++ {
		StoreOutput(database, mkRetained("fullkey", fmt.Sprintf("f%d", i), 400, 1))
	}
	if n, _ := EvictIfNeeded(database, "fullkey", capMB, 7, 1_000_000_000); n == 0 {
		t.Error("full-body rows over the cap should evict")
	}

	var ids []string
	for i := 0; i < 3; i++ {
		s, _ := StoreOutput(database, mkRetained("summkey", fmt.Sprintf("s%d", i), 400, 0))
		ids = append(ids, s.ID)
	}
	if n, _ := EvictIfNeeded(database, "summkey", capMB, 7, 1_000_000_000); n != 0 {
		t.Errorf("summary-only rows under the effective cap evicted %d", n)
	}
	for _, id := range ids {
		if got, _ := RetrieveOutput(database, id); got == nil {
			t.Errorf("%s was evicted", id)
		}
	}
}

// Shedding a summary-only row frees ~its summary, not its original size, so a
// full row must still go when the summary-only one doesn't cover the shortfall.
func TestEvictionShedsByEffectiveSize(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()
	const now = 1_000_000_000
	s, _ := StoreOutput(database, mkRetained("proj", "s", 1000, 0))
	f, _ := StoreOutput(database, mkRetained("proj", "a full body row", 1000, 1))
	database.Exec("UPDATE stored_outputs SET access_count = 0, last_accessed = ? WHERE id = ?", now-100000, s.ID)
	database.Exec("UPDATE stored_outputs SET access_count = 3, last_accessed = ? WHERE id = ?", now, f.ID)

	EvictIfNeeded(database, "proj", 501.0/(1024*1024), 7, now)
	if got, _ := RetrieveOutput(database, f.ID); got != nil {
		t.Error("full row survived; eviction shed by original_size")
	}
}

func TestPinBudgetCountsSummaryOnlyAtEffectiveSize(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()
	const capMB = 0.001 // ~1049 bytes; 3 × 800 original would blow it
	for i := 0; i < 3; i++ {
		s, _ := StoreOutput(database, mkRetained("proj", fmt.Sprintf("s%d", i), 800, 0))
		if o, err := PinOutputBounded(database, s.ID, "proj", true, capMB); err != nil || !o.OK {
			t.Errorf("pin %d refused: %+v %v", i, o, err)
		}
	}
}

func TestPinBudgetSumsMixedRows(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()
	const capMB = 0.001
	full, _ := StoreOutput(database, mkRetained("proj", "f", 600, 1))
	if o, _ := PinOutputBounded(database, full.ID, "proj", true, capMB); !o.OK {
		t.Fatalf("full pin refused: %+v", o)
	}
	summ, _ := StoreOutput(database, mkRetained("proj", "s", 900, 0))
	if o, _ := PinOutputBounded(database, summ.ID, "proj", true, capMB); !o.OK {
		t.Errorf("summary-only pin refused: %+v", o)
	}
}

func TestStatsPinnedBytesEffectiveSavingsOriginal(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()
	const summary = "short summary"
	s, _ := StoreOutput(database, mkRetained("proj", summary, 5000, 0))
	PinOutput(database, s.ID, "proj", true)
	st := GetStats(database, "proj")
	if st.PinnedBytes != len(summary) {
		t.Errorf("PinnedBytes = %d, want %d (effective)", st.PinnedBytes, len(summary))
	}
	if st.TotalOriginalBytes != 5000 {
		t.Errorf("TotalOriginalBytes = %d, want 5000 (savings use original)", st.TotalOriginalBytes)
	}
}
