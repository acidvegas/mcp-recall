// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/hooks/sessionstart.go

package hooks

import (
	"os"
	"strings"
	"time"

	"mcprecall/internal/config"
	"mcprecall/internal/db"
	"mcprecall/internal/gc"
	"mcprecall/internal/jsonx"
	"mcprecall/internal/logx"
	"mcprecall/internal/projectkey"
	"mcprecall/internal/tools"
)

const injectMaxChars = 2000

// HandleSessionStart records today as an active session day, prunes expired
// items, and returns a compact context snapshot to inject (or "" if nothing).
func HandleSessionStart(raw string) string {
	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		logx.Error("session-start received invalid JSON — skipping")
		return ""
	}
	obj, ok := parsed.(*jsonx.Obj)
	if !ok {
		logx.Error("session-start received unexpected input shape — skipping")
		return ""
	}
	cwd, _ := obj.Str("cwd")

	cfg := config.Load()
	projectKey := projectkey.Key(cwd)
	database, err := db.Open(db.DefaultDBPath(projectKey))
	if err != nil {
		logx.Error("session-start db open failed: " + err.Error())
		return ""
	}
	defer database.Close()

	// Record the resolved project path so `mcprecall gc` can tell whether this
	// project still exists on disk (orphan detection is path-existence based).
	//
	// Only recorded when it resolves to a real directory. The path is absolute by
	// construction, but absolute is not the same as true: a relative payload cwd
	// gets rooted against this process's cwd, which may not be where it was meant
	// to be rooted. This hook runs *inside* the project, so a resolved path that
	// does not exist means the guess was wrong — and recording it would let gc
	// read a live project as "path gone, parent present" and delete its database.
	//
	// Skipping is not the same as clearing: SetMeta upserts, so a database that
	// already holds a verified path keeps it. That is the better evidence — a
	// path that once resolved beats a guess that just failed. Only a database
	// that never recorded one stays pathless, which gc keeps as legacy-fresh and
	// reclaims solely on the untouched-for-N-days rule, never on a
	// deleted-project inference.
	projectPath := projectkey.Path(cwd)
	if isExistingDir(projectPath) {
		if err := db.SetMeta(database, "project_path", projectPath); err != nil {
			logx.Warn("session-start could not record project_path — " + err.Error())
		}
	} else {
		logx.Debug("session-start · resolved project path is not a directory, leaving meta as-is: " + projectPath)
	}

	today := time.Now().UTC().Format("2006-01-02")
	db.RecordSession(database, today)
	db.PruneExpired(database, projectKey, cfg.Store.ExpireAfterSessionDays)

	// A store-maintenance reminder (if any) leads, so it survives snapshot truncation.
	var parts []string
	if reminder := gc.ReminderText(gc.StoreFootprint(db.DataDir()), cfg.Store.GCReminderMB); reminder != "" {
		parts = append(parts, reminder)
	}

	snapshot := tools.Context(database, projectKey, tools.ContextArgs{})
	if snapshot != tools.ContextEmptyResponse {
		if len([]rune(snapshot)) > injectMaxChars {
			snapshot = string([]rune(snapshot)[:injectMaxChars]) + "\n… (truncated — call recall__context for the full view)"
		}
		parts = append(parts, snapshot)
	}

	if len(parts) == 0 {
		logx.Debug("session-start · nothing to inject")
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// isExistingDir is true only for a path that exists AND is a directory — a
// project path is never a file.
func isExistingDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
