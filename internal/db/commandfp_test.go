// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/commandfp_test.go
// Covers command_fp persistence and GetBashCommandBreakdown (upstream #251).

package db

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func mkFP(tool, fp string, originalSize int, summary string) StoreInput {
	in := mkSized("proj", tool, originalSize)
	in.Summary = summary
	if fp != "" {
		in.CommandFP = &fp
	}
	return in
}

func TestCommandFPPersists(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()
	s, _ := StoreOutput(database, mkFP("Bash", "git diff", 100, "s"))
	if s.CommandFP == nil || *s.CommandFP != "git diff" {
		t.Errorf("returned CommandFP = %v", s.CommandFP)
	}
	got, _ := RetrieveOutput(database, s.ID)
	if got.CommandFP == nil || *got.CommandFP != "git diff" {
		t.Errorf("retrieved CommandFP = %v", got.CommandFP)
	}

	n, _ := StoreOutput(database, mkFP("mcp__github__list_issues", "", 100, "s"))
	if got, _ := RetrieveOutput(database, n.ID); n.CommandFP != nil || got.CommandFP != nil {
		t.Error("omitted command_fp should be NULL")
	}
}

func TestBashCommandBreakdown(t *testing.T) {
	database, _ := Open(":memory:")
	defer database.Close()
	StoreOutput(database, mkFP("Bash", "git diff", 5000, "a"))
	StoreOutput(database, mkFP("Bash", "git diff", 3000, "b"))
	StoreOutput(database, mkFP("Bash", "rg", 1000, "c"))
	StoreOutput(database, mkFP("Bash", "", 900, "x"))
	StoreOutput(database, mkFP("mcp__github__list_issues", "", 99999, "gh"))

	rows := GetBashCommandBreakdown(database, "proj")
	var fps []string
	for _, r := range rows {
		fps = append(fps, r.CommandFP)
	}
	if !reflect.DeepEqual(fps, []string{"git diff", "rg", "unknown"}) {
		t.Fatalf("families = %v, want [git diff rg unknown] (non-Bash excluded, sorted by size)", fps)
	}
	if rows[0].Items != 2 || rows[0].OriginalBytes != 8000 {
		t.Errorf("git diff row = %+v, want 2 items / 8000 bytes", rows[0])
	}
}

// An existing database (the 15e81ea schema: no full_retained, no command_fp)
// gains both columns on open; its rows read as unknown and stay readable.
func TestCommandFPMigratesExistingDB(t *testing.T) {
	file := filepath.Join(t.TempDir(), "old.db")
	database, _ := Open(file)
	StoreOutput(database, mkSized("proj", "Bash", 500))
	database.Close()
	raw, _ := sql.Open("sqlite", file)
	for _, stmt := range []string{
		"DROP INDEX idx_so_command_fp",
		"ALTER TABLE stored_outputs DROP COLUMN command_fp",
		"ALTER TABLE stored_outputs DROP COLUMN full_retained",
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	raw.Close()

	database, err := Open(file)
	if err != nil {
		t.Fatalf("open migrated: %v", err)
	}
	defer database.Close()
	items := ExportAll(database, "proj")
	if len(items) != 1 || items[0].CommandFP != nil || items[0].FullRetained != 1 {
		t.Fatalf("old row = %+v", items)
	}
	if rows := GetBashCommandBreakdown(database, "proj"); len(rows) != 1 || rows[0].CommandFP != "unknown" {
		t.Errorf("breakdown = %+v, want one unknown row", rows)
	}
}
