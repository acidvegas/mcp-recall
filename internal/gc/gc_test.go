// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/gc/gc_test.go
// Covers `mcprecall gc` (upstream PR #201): classification, deletion policy,
// vacuum targeting, and the session-start footprint reminder.

package gc

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcprecall/internal/db"
)

var now = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

// mkDB creates a real mcp-recall database at dir/<name>.db. When projectPath is
// non-empty it is recorded in meta, mimicking what session-start does.
func mkDB(t *testing.T, dir, name, projectPath string) string {
	t.Helper()
	file := filepath.Join(dir, name+".db")
	database, err := db.Open(file)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer database.Close()
	if projectPath != "" {
		if err := db.SetMeta(database, "project_path", projectPath); err != nil {
			t.Fatalf("setmeta: %v", err)
		}
	}
	if _, err := db.StoreOutput(database, db.StoreInput{
		ProjectKey: name, SessionID: "s", ToolName: "t",
		Summary: "[s]", FullContent: "content", OriginalSize: 7,
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	return file
}

func setMtime(t *testing.T, file string, when time.Time) {
	t.Helper()
	for _, f := range []string{file, file + "-wal", file + "-shm"} {
		if _, err := os.Stat(f); err == nil {
			if err := os.Chtimes(f, when, when); err != nil {
				t.Fatalf("chtimes %s: %v", f, err)
			}
		}
	}
}

func find(entries []Entry, base string) *Entry {
	for i := range entries {
		if filepath.Base(entries[i].File) == base {
			return &entries[i]
		}
	}
	return nil
}

func TestClassifyActiveProject(t *testing.T) {
	store := t.TempDir()
	project := t.TempDir() // exists
	mkDB(t, store, "alive", project)

	entries := ScanDatabases(store, filepath.Join(store, "none.db"), 90, now)
	e := find(entries, "alive.db")
	if e == nil || e.Status != StatusActive {
		t.Fatalf("want active, got %+v", e)
	}
	if IsDeletionCandidate(e.Status) {
		t.Error("an active project must never be a deletion candidate")
	}
}

func TestClassifyOrphanedWhenProjectDeletedButParentSurvives(t *testing.T) {
	store := t.TempDir()
	parent := t.TempDir()
	project := filepath.Join(parent, "deleted-project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mkDB(t, store, "orphan", project)
	os.RemoveAll(project) // project deleted, parent still there

	entries := ScanDatabases(store, filepath.Join(store, "none.db"), 90, now)
	e := find(entries, "orphan.db")
	if e == nil || e.Status != StatusOrphaned {
		t.Fatalf("want orphaned, got %+v", e)
	}
	if !IsDeletionCandidate(e.Status) {
		t.Error("orphaned must be a deletion candidate")
	}
}

func TestClassifyUnverifiableWhenParentAlsoGone(t *testing.T) {
	// Both path and parent missing looks like an unmounted volume, not a
	// deleted project. Must never be deleted.
	store := t.TempDir()
	gone := filepath.Join(t.TempDir(), "vanished-parent", "project")
	mkDB(t, store, "unmounted", gone)

	entries := ScanDatabases(store, filepath.Join(store, "none.db"), 90, now)
	e := find(entries, "unmounted.db")
	if e == nil || e.Status != StatusUnverifiable {
		t.Fatalf("want unverifiable, got %+v", e)
	}
	if IsDeletionCandidate(e.Status) {
		t.Error("an unmounted-volume DB must never be deleted")
	}
}

// mkDBRecorded is mkDB but always records projectPath, even "".
func mkDBRecorded(t *testing.T, dir, name, projectPath string) string {
	t.Helper()
	file := mkDB(t, dir, name, "")
	database, _ := db.Open(file)
	defer database.Close()
	if err := db.SetMeta(database, "project_path", projectPath); err != nil {
		t.Fatalf("setmeta: %v", err)
	}
	return file
}

var unrootedPaths = []string{"", "myproject", "some/relative/path", "."}

// A relative path resolves against whatever cwd gc runs from, and "." would
// read as "parent survived, project deleted" — so it is never deleted on a
// deleted-project inference. Fresh, it is kept (upstream #214).
func TestClassifyUnrootedFreshIsKept(t *testing.T) {
	for i, rel := range unrootedPaths {
		store := t.TempDir()
		mkDBRecorded(t, store, fmt.Sprintf("rel%d", i), rel)
		e := ScanDatabases(store, filepath.Join(store, "none.db"), 90, time.Now())[0]
		if e.Status != StatusUnrootedFresh || IsDeletionCandidate(e.Status) {
			t.Errorf("%q: status %s, want kept unrooted-fresh", rel, e.Status)
		}
	}
}

// Past the stale window an un-rootable DB falls through to the staleness rule
// instead of staying pinned forever (upstream #214).
func TestClassifyUnrootedStaleIsCandidate(t *testing.T) {
	for i, rel := range unrootedPaths {
		store := t.TempDir()
		mkDBRecorded(t, store, fmt.Sprintf("rel%d", i), rel)
		e := ScanDatabases(store, filepath.Join(store, "none.db"), 90, time.Now().AddDate(0, 0, 200))[0]
		if e.ProjectPath != rel {
			t.Errorf("projectPath = %q, want %q", e.ProjectPath, rel)
		}
		if e.Status != StatusUnrootedStale || !IsDeletionCandidate(e.Status) {
			t.Errorf("%q: status %s, want deletable unrooted-stale", rel, e.Status)
		}
	}
}

// An absolute path whose whole tree is gone is likely an unmounted volume that
// may return intact — never a candidate, however old.
func TestUnmountedAbsolutePathNeverDeletedWhenOld(t *testing.T) {
	store := t.TempDir()
	mkDB(t, store, "unmounted", "/no/such/mount/point/project")
	e := ScanDatabases(store, filepath.Join(store, "none.db"), 90, time.Now().AddDate(0, 0, 500))[0]
	if e.Status != StatusUnverifiable || IsDeletionCandidate(e.Status) {
		t.Errorf("status %s, want unverifiable", e.Status)
	}
}

func TestClassifyLegacyFreshVsStale(t *testing.T) {
	store := t.TempDir()
	fresh := mkDB(t, store, "fresh", "") // no recorded path
	stale := mkDB(t, store, "stale", "")

	setMtime(t, fresh, now.AddDate(0, 0, -10))
	setMtime(t, stale, now.AddDate(0, 0, -200))

	entries := ScanDatabases(store, filepath.Join(store, "none.db"), 90, now)

	f := find(entries, "fresh.db")
	if f == nil || f.Status != StatusLegacyFresh {
		t.Fatalf("want legacy-fresh, got %+v", f)
	}
	if IsDeletionCandidate(f.Status) {
		t.Error("legacy-fresh must not be a candidate")
	}

	s := find(entries, "stale.db")
	if s == nil || s.Status != StatusLegacyStale {
		t.Fatalf("want legacy-stale, got %+v", s)
	}
	if !IsDeletionCandidate(s.Status) {
		t.Error("legacy-stale must be a candidate")
	}
}

func TestStaleDaysIsRespected(t *testing.T) {
	store := t.TempDir()
	f := mkDB(t, store, "aged", "")
	setMtime(t, f, now.AddDate(0, 0, -100))

	// Default 90d window → stale.
	if e := find(ScanDatabases(store, "", 90, now), "aged.db"); e.Status != StatusLegacyStale {
		t.Errorf("at 90d want legacy-stale, got %s", e.Status)
	}
	// Widened to 365d → fresh again.
	if e := find(ScanDatabases(store, "", 365, now), "aged.db"); e.Status != StatusLegacyFresh {
		t.Errorf("at 365d want legacy-fresh, got %s", e.Status)
	}
}

func TestCurrentDBIsNeverTouched(t *testing.T) {
	store := t.TempDir()
	// Record a deleted project path so it would otherwise classify as orphaned.
	parent := t.TempDir()
	project := filepath.Join(parent, "p")
	os.Mkdir(project, 0o755)
	current := mkDB(t, store, "current", project)
	os.RemoveAll(project)

	entries := ScanDatabases(store, current, 90, now)
	e := find(entries, "current.db")
	if e == nil || e.Status != StatusCurrent {
		t.Fatalf("want current, got %+v", e)
	}
	if IsDeletionCandidate(e.Status) {
		t.Error("the live DB must never be a deletion candidate")
	}
	// Still probed, so the report shows its real identity (upstream #265).
	if e.ProjectPath != project || e.Items != 1 {
		t.Errorf("current DB reported path %q items %d, want %q and 1", e.ProjectPath, e.Items, project)
	}
}

// A current DB that probes unreadable must stay current, never unreadable.
func TestCurrentDBStaysCurrentWhenUnreadable(t *testing.T) {
	store := t.TempDir()
	file := filepath.Join(store, "mine.db")
	raw, _ := sql.Open("sqlite", file)
	raw.Exec(`CREATE TABLE something_else (x)`)
	raw.Close()
	e := ScanDatabases(store, file, 90, now)[0]
	if e.Status != StatusCurrent || IsDeletionCandidate(e.Status) {
		t.Errorf("status %s, want current", e.Status)
	}
}

func TestCurrentDBMatchesThroughNonNormalizedPath(t *testing.T) {
	// A RECALL_DB_PATH with a "/./" segment must still match the scanned path.
	store := t.TempDir()
	mkDB(t, store, "live", "")
	weird := filepath.Join(store, ".", "live.db")

	entries := ScanDatabases(store, weird, 90, now)
	if e := find(entries, "live.db"); e == nil || e.Status != StatusCurrent {
		t.Fatalf("non-normalized current path did not match: %+v", e)
	}
}

func TestNonRecallDBIsUnreadableAndNeverDeleted(t *testing.T) {
	store := t.TempDir()
	junk := filepath.Join(store, "notours.db")
	if err := os.WriteFile(junk, []byte("this is not a sqlite database"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := ScanDatabases(store, "", 90, now)
	e := find(entries, "notours.db")
	if e == nil || e.Status != StatusUnreadable {
		t.Fatalf("want unreadable, got %+v", e)
	}
	if IsDeletionCandidate(e.Status) {
		t.Error("a foreign .db must never be deleted")
	}
}

func TestNonDBFilesAreIgnored(t *testing.T) {
	store := t.TempDir()
	mkDB(t, store, "real", "")
	os.WriteFile(filepath.Join(store, "notes.txt"), []byte("x"), 0o644)

	entries := ScanDatabases(store, "", 90, now)
	for _, e := range entries {
		if filepath.Base(e.File) == "notes.txt" {
			t.Fatal("non-.db file was scanned")
		}
	}
}

func TestVacuumTargetsExcludeCandidatesAndCurrent(t *testing.T) {
	entries := []Entry{
		{File: "a.db", Status: StatusCurrent},
		{File: "b.db", Status: StatusActive},
		{File: "c.db", Status: StatusOrphaned},
		{File: "d.db", Status: StatusLegacyFresh},
		{File: "e.db", Status: StatusLegacyStale},
		{File: "f.db", Status: StatusUnverifiable},
		{File: "g.db", Status: StatusUnreadable},
		{File: "h.db", Status: StatusUnrootedFresh},
		{File: "i.db", Status: StatusUnrootedStale},
	}
	got := VacuumTargets(entries)
	if len(got) != 3 {
		t.Fatalf("want 3 vacuum targets, got %d: %+v", len(got), got)
	}
	if got[0].File != "b.db" || got[1].File != "d.db" || got[2].File != "h.db" {
		t.Errorf("want active + legacy-fresh + unrooted-fresh only, got %+v", got)
	}
}

func TestEveryStatusHasAPolicy(t *testing.T) {
	all := []Status{
		StatusCurrent, StatusActive, StatusOrphaned, StatusUnverifiable,
		StatusLegacyFresh, StatusLegacyStale, StatusUnreadable,
		StatusUnrootedFresh, StatusUnrootedStale,
	}
	for _, s := range all {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("status %q has no policy: %v", s, r)
				}
			}()
			_ = IsDeletionCandidate(s)
		}()
	}
	if len(statusPolicy) != len(all) {
		t.Errorf("statusPolicy has %d entries, test covers %d — keep them in sync",
			len(statusPolicy), len(all))
	}
}

func TestEntriesSortedLargestFirst(t *testing.T) {
	store := t.TempDir()
	mkDB(t, store, "small", "")
	big := mkDB(t, store, "big", "")
	// Inflate one DB well past the other.
	database, _ := db.Open(big)
	for i := 0; i < 200; i++ {
		db.StoreOutput(database, db.StoreInput{
			ProjectKey: "big", SessionID: "s", ToolName: "t",
			Summary: "[s]", FullContent: strings.Repeat("y", 2000), OriginalSize: 2000,
		})
	}
	database.Close()

	entries := ScanDatabases(store, "", 90, now)
	if len(entries) < 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].SizeBytes < entries[1].SizeBytes {
		t.Errorf("entries not sorted largest-first: %d then %d",
			entries[0].SizeBytes, entries[1].SizeBytes)
	}
}

// ── deletion ──────────────────────────────────────────────────────────────────

func TestDryRunDeletesNothing(t *testing.T) {
	store := t.TempDir()
	parent := t.TempDir()
	project := filepath.Join(parent, "p")
	os.Mkdir(project, 0o755)
	orphan := mkDB(t, store, "orphan", project)
	os.RemoveAll(project)

	var buf bytes.Buffer
	Run(&buf, store, "", Options{DryRun: true}, now)

	if _, err := os.Stat(orphan); err != nil {
		t.Fatal("dry run deleted a database")
	}
	if !strings.Contains(buf.String(), "Dry run — pass --force") {
		t.Errorf("want dry-run notice, got:\n%s", buf.String())
	}
}

func TestForceDeletesOnlyCandidatesAndSidecars(t *testing.T) {
	store := t.TempDir()
	alive := t.TempDir()
	parent := t.TempDir()
	project := filepath.Join(parent, "p")
	os.Mkdir(project, 0o755)

	keep := mkDB(t, store, "keep", alive)
	orphan := mkDB(t, store, "orphan", project)
	os.RemoveAll(project)
	// Sidecars must go too.
	os.WriteFile(orphan+"-wal", []byte("w"), 0o644)
	os.WriteFile(orphan+"-shm", []byte("s"), 0o644)

	var buf bytes.Buffer
	Run(&buf, store, "", Options{DryRun: false}, now)

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphan .db was not deleted")
	}
	for _, sfx := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(orphan + sfx); !os.IsNotExist(err) {
			t.Errorf("orphan %s sidecar was not deleted", sfx)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("an active project's DB was deleted")
	}
	if !strings.Contains(buf.String(), "Deleted 1 database(s)") {
		t.Errorf("want deletion summary, got:\n%s", buf.String())
	}
}

func TestRunOnEmptyDirReportsNothing(t *testing.T) {
	var buf bytes.Buffer
	Run(&buf, t.TempDir(), "", Options{DryRun: true}, now)
	if !strings.Contains(buf.String(), "No databases found") {
		t.Errorf("got:\n%s", buf.String())
	}
}

func TestVacuumRunsOnSurvivorsAndKeepsData(t *testing.T) {
	store := t.TempDir()
	alive := t.TempDir()
	keep := mkDB(t, store, "keep", alive)

	// Add then delete rows so there are free pages to reclaim.
	database, _ := db.Open(keep)
	var ids []string
	for i := 0; i < 300; i++ {
		s, _ := db.StoreOutput(database, db.StoreInput{
			ProjectKey: "keep", SessionID: "s", ToolName: "t",
			Summary: "[s]", FullContent: strings.Repeat("z", 3000), OriginalSize: 3000,
		})
		ids = append(ids, s.ID)
	}
	database.Close()
	database, _ = db.Open(keep)
	db.ForgetOutputs(database, "keep", db.ForgetOptions{All: true, Force: true})
	database.Close()

	var buf bytes.Buffer
	Run(&buf, store, "", Options{DryRun: true, Vacuum: true}, now)

	if !strings.Contains(buf.String(), "Vacuumed") {
		t.Fatalf("want vacuum summary, got:\n%s", buf.String())
	}
	// The DB must still open and be intact after VACUUM.
	database, err := db.Open(keep)
	if err != nil {
		t.Fatalf("DB unusable after vacuum: %v", err)
	}
	defer database.Close()
	if _, err := db.GetMeta(database, "project_path"); err != nil {
		t.Errorf("meta unreadable after vacuum: %v", err)
	}
}

func TestVacuumResultMeasuresAfterWALCheckpoint(t *testing.T) {
	// db.Open DBs are WAL: VACUUM writes the rewrite into -wal, which is only
	// checkpointed away on close. The reported After must be the settled size.
	file := mkDB(t, t.TempDir(), "wal", "")
	database, _ := db.Open(file)
	for i := 0; i < 300; i++ {
		db.StoreOutput(database, db.StoreInput{
			ProjectKey: "wal", SessionID: "s", ToolName: "t",
			Summary: "[s]", FullContent: strings.Repeat(fmt.Sprint(i), 3000), OriginalSize: 3000,
		})
	}
	database.Close()
	database, _ = db.Open(file)
	if _, err := database.Exec(`PRAGMA auto_vacuum=NONE; VACUUM`); err != nil {
		t.Fatal(err)
	}
	db.ForgetOutputs(database, "wal", db.ForgetOptions{All: true, Force: true})
	database.Close()

	result := VacuumFile(file)
	if result.Err != nil {
		t.Fatalf("vacuum failed: %v", result.Err)
	}
	if settled := dbFootprint(file); result.After != settled {
		t.Errorf("VacuumResult.After = %d, settled footprint = %d", result.After, settled)
	}
	if result.After >= result.Before {
		t.Errorf("reported no reclaim: %d → %d bytes", result.Before, result.After)
	}
}

func TestVacuumReclaimsOnLegacyAutoVacuumNoneDB(t *testing.T) {
	// The case --vacuum exists for: a database created without
	// auto_vacuum=INCREMENTAL, where incremental_vacuum is a no-op and only a
	// full VACUUM returns free pages. db.Open() always sets INCREMENTAL, so this
	// builds the legacy shape by hand.
	store := t.TempDir()
	file := filepath.Join(store, "legacy.db")

	raw, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec("PRAGMA auto_vacuum=NONE"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE stored_outputs (id TEXT PRIMARY KEY, blob TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		if _, err := raw.Exec(`INSERT INTO stored_outputs VALUES (?, ?)`,
			fmt.Sprintf("id%d", i), strings.Repeat("q", 4000)); err != nil {
			t.Fatal(err)
		}
	}
	raw.Exec(`DELETE FROM stored_outputs`)
	// Confirm the premise: incremental_vacuum cannot reclaim here.
	raw.Exec("PRAGMA incremental_vacuum")
	raw.Close()

	before := dbFootprint(file)
	result := VacuumFile(file)
	if result.Err != nil {
		t.Fatalf("vacuum failed: %v", result.Err)
	}
	after := dbFootprint(file)
	if after >= before {
		t.Fatalf("VACUUM reclaimed nothing on a legacy DB: %d → %d bytes", before, after)
	}

	// And it must have upgraded the DB to incremental auto-vacuum.
	check, _ := sql.Open("sqlite", file)
	defer check.Close()
	var mode int
	if err := check.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatalf("read auto_vacuum: %v", err)
	}
	if mode != 2 { // 2 = INCREMENTAL
		t.Errorf("auto_vacuum = %d, want 2 (INCREMENTAL) after vacuum upgrade", mode)
	}
}

// ── footprint + reminder ──────────────────────────────────────────────────────

func TestStoreFootprintCountsDBsOnly(t *testing.T) {
	store := t.TempDir()
	mkDB(t, store, "one", "")
	mkDB(t, store, "two", "")
	os.WriteFile(filepath.Join(store, "readme.txt"), []byte("ignore me"), 0o644)

	fp := StoreFootprint(store)
	if fp.DBCount != 2 {
		t.Errorf("DBCount = %d, want 2", fp.DBCount)
	}
	if fp.TotalBytes <= 0 {
		t.Errorf("TotalBytes = %d, want > 0", fp.TotalBytes)
	}
}

func TestStoreFootprintMissingDirIsZero(t *testing.T) {
	fp := StoreFootprint(filepath.Join(t.TempDir(), "does-not-exist"))
	if fp.DBCount != 0 || fp.TotalBytes != 0 {
		t.Errorf("want zero footprint, got %+v", fp)
	}
}

func TestReminderText(t *testing.T) {
	over := Footprint{TotalBytes: 3 * 1024 * 1024, DBCount: 4}
	under := Footprint{TotalBytes: 1 * 1024 * 1024, DBCount: 4}

	if got := ReminderText(over, 2); !strings.Contains(got, "mcprecall gc") {
		t.Errorf("want reminder over threshold, got %q", got)
	}
	if got := ReminderText(under, 2); got != "" {
		t.Errorf("want no reminder under threshold, got %q", got)
	}
	// 0 disables entirely, even for a huge store.
	if got := ReminderText(Footprint{TotalBytes: 1 << 40, DBCount: 99}, 0); got != "" {
		t.Errorf("gc_reminder_mb=0 must disable the reminder, got %q", got)
	}
}

func TestProbeHandlesURISyntaxInPath(t *testing.T) {
	// `#` and `%` are URI syntax; an unescaped file: URI would misread the path
	// and report a healthy DB as unreadable.
	for _, name := range []string{"a#b", "100%", "a%20b"} {
		dir := filepath.Join(t.TempDir(), name)
		os.MkdirAll(dir, 0o755)
		file := mkDB(t, dir, "p", "/some/project")
		p := probeDB(file)
		if !p.readable || p.items != 1 || p.projectPath != "/some/project" {
			t.Errorf("%s: probe = %+v, want readable with 1 item", name, p)
		}
	}
}
