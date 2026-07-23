// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/importer/importer_test.go

package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcprecall/internal/db"
)

const srcProject = "import_test_source_key"

func mkRow(id, tool, summary, content string) importRow {
	return importRow{
		ID: id, ProjectKey: srcProject, SessionID: "sess-abc", ToolName: tool,
		Summary: summary, FullContent: content, OriginalSize: 512,
		SummarySize: len(summary), CreatedAt: time.Now().Unix(), Pinned: 0, AccessCount: 0,
	}
}

func query(t *testing.T, path, sql string, args ...any) [][]any {
	t.Helper()
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()
	rows, err := database.Query(sql, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		out = append(out, vals)
	}
	return out
}

func TestImportRoundTripAndOrdering(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	a := mkRow("recall_aaaaaaaaaaaaaaa1", "mcp__github__list_issues", "Issue #1", `[{"number":1}]`)
	a.CreatedAt = 1000
	b := mkRow("recall_bbbbbbbbbbbbbbb2", "mcp__github__get_issue", "Issue #2", `[{"number":2}]`)
	b.CreatedAt = 2000
	res := importItems(dbPath, []importRow{a, b}, false, "") // keep-project-key
	if res.imported != 2 {
		t.Fatalf("imported = %d", res.imported)
	}
	rows := query(t, dbPath, "SELECT tool_name FROM stored_outputs ORDER BY created_at ASC")
	if len(rows) != 2 || rows[0][0] != "mcp__github__list_issues" || rows[1][0] != "mcp__github__get_issue" {
		t.Errorf("order/content wrong: %v", rows)
	}
}

func TestImportProjectKeyRemapAndKeep(t *testing.T) {
	// remap (targetKey = pk)
	p1 := filepath.Join(t.TempDir(), "t.db")
	importItems(p1, []importRow{mkRow("recall_1111111111111111", "mcp__x", "s", "c")}, false, "current_project_key")
	if got := query(t, p1, "SELECT project_key FROM stored_outputs LIMIT 1"); got[0][0] == srcProject {
		t.Error("default should remap project key")
	}
	// keep (targetKey = "")
	p2 := filepath.Join(t.TempDir(), "t.db")
	importItems(p2, []importRow{mkRow("recall_2222222222222222", "mcp__x", "s", "c")}, false, "")
	if got := query(t, p2, "SELECT project_key FROM stored_outputs LIMIT 1"); got[0][0] != srcProject {
		t.Errorf("--keep-project-key should preserve: %v", got[0][0])
	}
}

func TestImportPreservesPinAndFTS(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	r := mkRow("recall_3333333333333333", "mcp__github__list_issues", "Issue", "The quick brown fox jumps over the lazy dog")
	r.Pinned = 1
	importItems(p, []importRow{r}, false, "")
	if got := query(t, p, "SELECT pinned FROM stored_outputs WHERE id = ?", r.ID); got[0][0].(int64) != 1 {
		t.Errorf("pin not preserved: %v", got[0][0])
	}
	fts := query(t, p, `SELECT o.id FROM stored_outputs o JOIN outputs_fts f ON f.rowid=o.rowid WHERE outputs_fts MATCH ?`, `"fox"`)
	if len(fts) == 0 {
		t.Error("content not searchable after import")
	}
}

func TestImportSkipAndOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	r := mkRow("recall_4444444444444444", "mcp__x", "Issue #1", "original content")
	importItems(p, []importRow{r}, false, "")

	// skip existing by default (mutated summary should NOT apply)
	r.Summary = "UPDATED"
	res := importItems(p, []importRow{r}, false, "")
	if res.skipped != 1 {
		t.Errorf("expected skip, got %+v", res)
	}
	if got := query(t, p, "SELECT summary FROM stored_outputs WHERE id = ?", r.ID); got[0][0] != "Issue #1" {
		t.Errorf("skip failed, summary = %v", got[0][0])
	}

	// overwrite replaces + refreshes chunks (no stale "original")
	r.Summary = "OVERWRITTEN"
	r.FullContent = "replacement content"
	res2 := importItems(p, []importRow{r}, true, "")
	if res2.overwritten != 1 {
		t.Errorf("expected overwrite, got %+v", res2)
	}
	if got := query(t, p, "SELECT summary FROM stored_outputs WHERE id = ?", r.ID); got[0][0] != "OVERWRITTEN" {
		t.Errorf("overwrite failed: %v", got[0][0])
	}
	chunks := query(t, p, "SELECT content FROM content_chunks WHERE output_id = ?", r.ID)
	if len(chunks) == 0 {
		t.Fatal("no chunks after overwrite")
	}
	for _, c := range chunks {
		if strings.Contains(c[0].(string), "original") {
			t.Error("stale chunk content survived overwrite")
		}
	}
}

func TestImportDryRunCounts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	items := []importRow{mkRow("recall_5555555555555555", "mcp__x", "s1", "c1"), mkRow("recall_6666666666666666", "mcp__y", "s2", "c2")}
	// non-existent DB path → all imported
	if r := dryRunCount(filepath.Join(t.TempDir(), "missing.db"), items, false); r.imported != 2 {
		t.Errorf("dry-run on missing db: %+v", r)
	}
	// after a real import, dry-run counts skips
	importItems(p, items, false, "")
	if r := dryRunCount(p, items, false); r.skipped != 2 {
		t.Errorf("dry-run skip count: %+v", r)
	}
}

// CLI validation (exit codes) via the built binary, mirroring the original's subprocess tests.
func TestImportCLIValidation(t *testing.T) {
	bin, _ := filepath.Abs("../../mcprecall")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("binary not built")
	}
	run := func(content string) (int, string) {
		f := filepath.Join(t.TempDir(), "dump.json")
		os.WriteFile(f, []byte(content), 0o644)
		cmd := exec.Command(bin, "import", f)
		cmd.Env = append(os.Environ(), "RECALL_DB_PATH="+filepath.Join(t.TempDir(), "x.db"))
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out)
	}
	if code, out := run("not json at all"); code != 1 || !strings.Contains(out, "Invalid JSON") {
		t.Errorf("invalid json: code=%d out=%q", code, out)
	}
	if code, out := run(`[{"foo":"bar"}]`); code != 1 || !strings.Contains(out, "recall__export") {
		t.Errorf("bad schema: code=%d out=%q", code, out)
	}
	if code, out := run("[]"); code != 0 || !strings.Contains(out, "Nothing to import") {
		t.Errorf("empty export should be graceful: code=%d out=%q", code, out)
	}
}
