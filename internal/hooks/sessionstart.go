// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/hooks/sessionstart.go

package hooks

import (
	"time"

	"mcprecall/internal/config"
	"mcprecall/internal/db"
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

	today := time.Now().UTC().Format("2006-01-02")
	db.RecordSession(database, today)
	db.PruneExpired(database, projectKey, cfg.Store.ExpireAfterSessionDays)

	snapshot := tools.Context(database, projectKey, tools.ContextArgs{})
	if snapshot == tools.ContextEmptyResponse {
		logx.Debug("session-start · nothing to inject")
		return ""
	}
	if len([]rune(snapshot)) > injectMaxChars {
		snapshot = string([]rune(snapshot)[:injectMaxChars]) + "\n… (truncated — call recall__context for the full view)"
	}
	return snapshot
}
