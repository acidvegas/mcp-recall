// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/db_concurrent_test.go
//
// Port of tests/db-concurrent.test.ts (upstream v1.9.0): verifies WAL-mode
// multi-connection durability. Each Open() is an independent connection pool to
// the same on-disk file (MaxOpenConns(1), journal_mode=WAL, busy_timeout=5000).

package db

import (
	"context"
	"path/filepath"
	"testing"
)

const concurrentPK = "concurrent-test-proj"

func concurrentInput(summary string) StoreInput {
	return StoreInput{
		ProjectKey:   concurrentPK,
		SessionID:    "2026-03-01",
		ToolName:     "mcp__test__tool",
		Summary:      summary,
		FullContent:  "full content",
		OriginalSize: 100,
	}
}

func TestTwoConnectionsPreserveAllRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 15; i++ {
		mustStore(t, db1, concurrentInput("db1 item"))
	}
	if _, err := db1.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	db1.Close()

	db2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 15; i++ {
		mustStore(t, db2, concurrentInput("db2 item"))
	}
	db2.Close()

	verify, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close()
	if got := ListOutputs(verify, ListOptions{ProjectKey: concurrentPK, Limit: 100}); len(got) != 30 {
		t.Fatalf("expected 30 rows across both connections, got %d", len(got))
	}
}

func TestReaderNotBlockedDuringBulkDelete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	for i := 0; i < 10; i++ {
		mustStore(t, db1, concurrentInput("item"))
	}

	if before := ListOutputs(db2, ListOptions{ProjectKey: concurrentPK}); len(before) != 10 {
		t.Fatalf("reader before delete = %d, want 10", len(before))
	}
	if _, err := ForgetOutputs(db1, concurrentPK, ForgetOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if after := ListOutputs(db2, ListOptions{ProjectKey: concurrentPK}); len(after) != 0 {
		t.Fatalf("reader after delete = %d, want 0", len(after))
	}
}

func TestSecondWriterFailsWhenExclusivelyLocked(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	// No retry on db2 → immediate failure under contention.
	if _, err := db2.Exec("PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	conn, err := db1.Conn(ctx) // hold the single pooled connection
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("acquire exclusive: %v", err)
	}

	// db2 must fail immediately rather than block.
	_, werr := db2.Exec("INSERT INTO sessions (date) VALUES ('2026-03-01')")
	if werr == nil {
		t.Error("expected a busy error while db1 holds an exclusive lock")
	}

	conn.ExecContext(ctx, "ROLLBACK")
	conn.Close()
}

func TestInterleavedWritesPreserveAllRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	for i := 0; i < 5; i++ {
		mustStore(t, db1, concurrentInput("db1 item"))
		mustStore(t, db2, concurrentInput("db2 item"))
	}
	db2.Close()
	if got := ListOutputs(db1, ListOptions{ProjectKey: concurrentPK}); len(got) != 10 {
		t.Fatalf("interleaved writes = %d, want 10", len(got))
	}
}
