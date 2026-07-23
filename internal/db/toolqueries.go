// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/toolqueries.go

package db

import "database/sql"

// ExportAll returns every stored output for a project, oldest first.
func ExportAll(database *sql.DB, projectKey string) []StoredOutput {
	rows, err := database.Query(`SELECT `+storedColumns+` FROM stored_outputs WHERE project_key = ? ORDER BY created_at ASC`, projectKey)
	if err != nil {
		return nil
	}
	out, err := scanOutputs(rows)
	if err != nil {
		return nil
	}
	return out
}

// ListStoredSorted powers recall__list_stored: sort by recent (default),
// accessed, or size, with an optional tool-name substring (LIKE) filter.
func ListStoredSorted(database *sql.DB, projectKey, tool, sort string, limit, offset int) []StoredOutput {
	order := "created_at DESC"
	switch sort {
	case "size":
		order = "original_size DESC"
	case "accessed":
		order = "access_count DESC, last_accessed DESC NULLS LAST, created_at DESC"
	}
	sqlStr := `SELECT ` + storedColumns + ` FROM stored_outputs WHERE project_key = ?`
	params := []any{projectKey}
	if tool != "" {
		sqlStr += ` AND tool_name LIKE ?`
		params = append(params, "%"+tool+"%")
	}
	sqlStr += ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	params = append(params, limit, offset)

	rows, err := database.Query(sqlStr, params...)
	if err != nil {
		return nil
	}
	out, err := scanOutputs(rows)
	if err != nil {
		return nil
	}
	return out
}
