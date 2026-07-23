// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/queries.go

package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"math"
	"sort"
	"strings"
	"time"

	"mcprecall/internal/logx"
)

// storedColumns is the explicit column list matching StoredOutput scan order.
const storedColumns = `id, project_key, session_id, tool_name, summary, full_content,
	original_size, summary_size, created_at, pinned, access_count, last_accessed, input_hash, output_hash`

// VacuumThreshold is the minimum number of deleted rows that triggers
// incremental_vacuum to reclaim disk space.
const VacuumThreshold = 50

func scanOutput(s interface {
	Scan(dest ...any) error
}) (StoredOutput, error) {
	var o StoredOutput
	var lastAccessed sql.NullInt64
	var inputHash, outputHash sql.NullString
	err := s.Scan(
		&o.ID, &o.ProjectKey, &o.SessionID, &o.ToolName, &o.Summary, &o.FullContent,
		&o.OriginalSize, &o.SummarySize, &o.CreatedAt, &o.Pinned, &o.AccessCount,
		&lastAccessed, &inputHash, &outputHash,
	)
	if err != nil {
		return o, err
	}
	if lastAccessed.Valid {
		v := lastAccessed.Int64
		o.LastAccessed = &v
	}
	if inputHash.Valid {
		v := inputHash.String
		o.InputHash = &v
	}
	if outputHash.Valid {
		v := outputHash.String
		o.OutputHash = &v
	}
	return o, nil
}

func scanOutputs(rows *sql.Rows) ([]StoredOutput, error) {
	defer rows.Close()
	var out []StoredOutput
	for rows.Next() {
		o, err := scanOutput(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "recall_" + hex.EncodeToString(b)
}

func nowUnix() int64 { return time.Now().Unix() }

func storeChunks(tx *sql.Tx, id, fullContent string) error {
	chunks := ChunkText(fullContent)
	stmt, err := tx.Prepare(`INSERT INTO content_chunks (output_id, chunk_index, content) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, c := range chunks {
		if _, err := stmt.Exec(id, i, c); err != nil {
			return err
		}
	}
	return nil
}

func countAndDelete(database *sql.DB, where string, params ...any) (int, error) {
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM stored_outputs WHERE `+where, params...).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		if _, err := database.Exec(`DELETE FROM stored_outputs WHERE `+where, params...); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// StoreOutput persists a compressed tool output and populates the FTS index and
// chunk table. Returns the fully-hydrated row including the generated id.
func StoreOutput(database *sql.DB, in StoreInput) (StoredOutput, error) {
	id := generateID()
	summarySize := len([]byte(in.Summary))
	createdAt := nowUnix()
	// Content hash enables dedup of identical output across different calls.
	// Reuse the caller's hash when provided (the hook already computed it).
	outputHash := in.OutputHash
	if outputHash == nil {
		h := HashContent(in.FullContent)
		outputHash = &h
	}

	tx, err := database.Begin()
	if err != nil {
		return StoredOutput{}, err
	}
	_, err = tx.Exec(`
		INSERT INTO stored_outputs
			(id, project_key, session_id, tool_name, summary, full_content,
			 original_size, summary_size, created_at, input_hash, output_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.ProjectKey, in.SessionID, in.ToolName,
		in.Summary, in.FullContent, in.OriginalSize,
		summarySize, createdAt, nullString(in.InputHash), nullString(outputHash),
	)
	if err != nil {
		tx.Rollback()
		return StoredOutput{}, err
	}
	if err := storeChunks(tx, id, in.FullContent); err != nil {
		tx.Rollback()
		return StoredOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return StoredOutput{}, err
	}

	return StoredOutput{
		ID: id, ProjectKey: in.ProjectKey, SessionID: in.SessionID,
		ToolName: in.ToolName, Summary: in.Summary, FullContent: in.FullContent,
		OriginalSize: in.OriginalSize, SummarySize: summarySize, CreatedAt: createdAt,
		Pinned: 0, AccessCount: 0, LastAccessed: nil, InputHash: in.InputHash, OutputHash: outputHash,
	}, nil
}

// HashContent returns the sha256 hex of tool output content — a path-independent
// dedup key.
func HashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func nullString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// RetrieveOutput fetches a single stored output by id, or nil if not found.
func RetrieveOutput(database *sql.DB, id string) (*StoredOutput, error) {
	row := database.QueryRow(`SELECT `+storedColumns+` FROM stored_outputs WHERE id = ?`, id)
	o, err := scanOutput(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// RecordAccess increments access_count and updates last_accessed for an item.
func RecordAccess(database *sql.DB, id string) error {
	_, err := database.Exec(`
		UPDATE stored_outputs
		SET access_count = access_count + 1, last_accessed = ?
		WHERE id = ?`, nowUnix(), id)
	return err
}

// PinOutput pins or unpins an item. Returns true if the item was found/updated.
func PinOutput(database *sql.DB, id, projectKey string, pinned bool) (bool, error) {
	p := 0
	if pinned {
		p = 1
	}
	res, err := database.Exec(`UPDATE stored_outputs SET pinned = ? WHERE id = ? AND project_key = ?`, p, id, projectKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CheckDedup looks up the most recent stored output with a matching input_hash
// for the project. Returns nil on a miss.
func CheckDedup(database *sql.DB, projectKey, inputHash string) (*StoredOutput, error) {
	row := database.QueryRow(`
		SELECT `+storedColumns+` FROM stored_outputs
		WHERE project_key = ? AND input_hash = ?
		ORDER BY created_at DESC
		LIMIT 1`, projectKey, inputHash)
	o, err := scanOutput(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// CheckOutputDedup looks up the most recent stored output whose content matches
// output_hash — catching identical output produced by a different call (or a
// call with no tool_input to hash). Returns nil on a miss.
func CheckOutputDedup(database *sql.DB, projectKey, outputHash string) (*StoredOutput, error) {
	row := database.QueryRow(`
		SELECT `+storedColumns+` FROM stored_outputs
		WHERE project_key = ? AND output_hash = ?
		ORDER BY created_at DESC
		LIMIT 1`, projectKey, outputHash)
	o, err := scanOutput(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

const secondsPerDay = 86400

// EvictIfNeeded enforces the project store size cap by evicting the
// lowest-value non-pinned items until total original_size is within maxSizeMB.
// Value is a recency-weighted frequency score: (access_count + 1) decayed by an
// exponential half-life on the time since last access (falling back to creation
// time for never-accessed items). nowSecs is injectable for deterministic tests.
func EvictIfNeeded(database *sql.DB, projectKey string, maxSizeMB float64, halfLifeDays int, nowSecs int64) (int, error) {
	maxBytes := int64(maxSizeMB * 1024 * 1024)

	var total int64
	if err := database.QueryRow(`
		SELECT COALESCE(SUM(original_size), 0) FROM stored_outputs WHERE project_key = ?`, projectKey).Scan(&total); err != nil {
		return 0, err
	}
	if total <= maxBytes {
		return 0, nil
	}
	bytesToShed := total - maxBytes

	rows, err := database.Query(`
		SELECT id, original_size, access_count, last_accessed, created_at
		FROM stored_outputs
		WHERE project_key = ? AND pinned = 0`, projectKey)
	if err != nil {
		return 0, err
	}
	type cand struct {
		id        string
		size      int64
		score     float64
		createdAt int64
	}
	// Math.max(1, …) guards a non-positive half-life.
	halfLifeSecs := math.Max(1, float64(halfLifeDays)*secondsPerDay)
	var candidates []cand
	for rows.Next() {
		var id string
		var size, createdAt int64
		var accessCount int
		var lastAccessed sql.NullInt64
		if err := rows.Scan(&id, &size, &accessCount, &lastAccessed, &createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		lastActive := createdAt
		if lastAccessed.Valid {
			lastActive = lastAccessed.Int64
		}
		age := float64(nowSecs - lastActive)
		if age < 0 {
			age = 0
		}
		recency := math.Pow(0.5, age/halfLifeSecs)
		candidates = append(candidates, cand{id, size, (float64(accessCount) + 1) * recency, createdAt})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	// Evict lowest value first; tiebreak oldest creation, then id.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score < b.score
		}
		if a.createdAt != b.createdAt {
			return a.createdAt < b.createdAt
		}
		return a.id < b.id
	})

	var toEvict []any
	var shed int64
	for _, c := range candidates {
		if shed >= bytesToShed {
			break
		}
		toEvict = append(toEvict, c.id)
		shed += c.size
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(toEvict)), ",")
	if _, err := database.Exec(`DELETE FROM stored_outputs WHERE id IN (`+placeholders+`)`, toEvict...); err != nil {
		return 0, err
	}
	return len(toEvict), nil
}

// RetrieveSnippet returns a relevant excerpt from a stored item's full content
// using FTS. Prefers chunk-based retrieval (precise, verbatim) over the legacy
// snippet function. Returns "" if the item doesn't exist or has no match.
func RetrieveSnippet(database *sql.DB, id, query string) string {
	var rowid int64
	err := database.QueryRow(`SELECT rowid FROM stored_outputs WHERE id = ?`, id).Scan(&rowid)
	if err != nil {
		return ""
	}
	safe := SanitizeFtsQuery(query)

	var content string
	err = database.QueryRow(`
		SELECT content FROM content_chunks
		WHERE content_chunks MATCH ? AND output_id = ?
		ORDER BY rank
		LIMIT 1`, safe, id).Scan(&content)
	if err == nil {
		return content
	}

	var excerpt string
	err = database.QueryRow(`
		SELECT snippet(outputs_fts, 3, '', '', ' [...] ', 64)
		FROM outputs_fts
		WHERE outputs_fts MATCH ?
		AND rowid = ?`, safe, rowid).Scan(&excerpt)
	if err != nil {
		return ""
	}
	return excerpt
}

const (
	peekMaxChunks   = 3
	peekChunkJoiner = "\n […] \n"
)

// RetrievePeek returns a bounded, multi-chunk context window into a stored item
// — the middle tier between SearchOutputs and full content. With a query, the
// top matching chunks (in document order); without one, the first chunks as a
// head preview. Returns "" when the item has no stored chunks (caller falls back).
func RetrievePeek(database *sql.DB, id, query string, maxChunks int) string {
	var one int
	if database.QueryRow(`SELECT 1 FROM stored_outputs WHERE id = ?`, id).Scan(&one) != nil {
		return ""
	}

	if query != "" {
		safe := SanitizeFtsQuery(query)
		rows, err := database.Query(`
			SELECT content, chunk_index FROM content_chunks
			WHERE content_chunks MATCH ? AND output_id = ?
			ORDER BY rank
			LIMIT ?`, safe, id, maxChunks)
		if err != nil {
			return ""
		}
		type chunk struct {
			content string
			index   int
		}
		var chunks []chunk
		for rows.Next() {
			var c chunk
			if rows.Scan(&c.content, &c.index) == nil {
				chunks = append(chunks, c)
			}
		}
		rows.Close()
		if len(chunks) == 0 {
			return ""
		}
		// Present in document order so the joiner reflects real gaps.
		sort.SliceStable(chunks, func(i, j int) bool { return chunks[i].index < chunks[j].index })
		parts := make([]string, len(chunks))
		for i, c := range chunks {
			parts[i] = c.content
		}
		return strings.Join(parts, peekChunkJoiner)
	}

	rows, err := database.Query(`
		SELECT content FROM content_chunks
		WHERE output_id = ?
		ORDER BY chunk_index
		LIMIT ?`, id, maxChunks)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			parts = append(parts, c)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, peekChunkJoiner)
}

// SearchOutputs full-text searches stored outputs for a project, ordered by FTS
// rank (best match first), capped at options.Limit (default 10).
func SearchOutputs(database *sql.DB, query string, options SearchOptions) []StoredOutput {
	limit := options.Limit
	if limit == 0 {
		limit = 10
	}
	safe := SanitizeFtsQuery(query)
	sqlStr := `
		SELECT ` + prefixCols("s.") + ` FROM outputs_fts f
		JOIN stored_outputs s ON s.rowid = f.rowid
		WHERE outputs_fts MATCH ?
		AND s.project_key = ?`
	params := []any{safe, options.ProjectKey}
	if options.Tool != "" {
		sqlStr += ` AND s.tool_name = ?`
		params = append(params, options.Tool)
	}
	sqlStr += ` ORDER BY rank LIMIT ?`
	params = append(params, limit)

	rows, err := database.Query(sqlStr, params...)
	if err != nil {
		return []StoredOutput{}
	}
	out, err := scanOutputs(rows)
	if err != nil {
		return []StoredOutput{}
	}
	return out
}

// prefixCols returns storedColumns with each column prefixed (e.g. "s.").
func prefixCols(prefix string) string {
	parts := strings.Split(storedColumns, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// ListOutputs returns a paginated list of stored outputs, optionally filtered by tool.
func ListOutputs(database *sql.DB, options ListOptions) []StoredOutput {
	limit := options.Limit
	if limit == 0 {
		limit = 20
	}
	order := "DESC"
	if options.Sort == "oldest" {
		order = "ASC"
	}
	sqlStr := `SELECT ` + storedColumns + ` FROM stored_outputs WHERE project_key = ?`
	params := []any{options.ProjectKey}
	if options.Tool != "" {
		sqlStr += ` AND tool_name = ?`
		params = append(params, options.Tool)
	}
	sqlStr += ` ORDER BY created_at ` + order + ` LIMIT ? OFFSET ?`
	params = append(params, limit, options.Offset)

	rows, err := database.Query(sqlStr, params...)
	if err != nil {
		return []StoredOutput{}
	}
	out, err := scanOutputs(rows)
	if err != nil {
		return []StoredOutput{}
	}
	return out
}

// ForgetOutputs deletes stored outputs matching the given criteria. Pinned items
// are skipped unless Force is true — except single-ID deletes, which always
// bypass pin protection. Returns the number of items deleted.
func ForgetOutputs(database *sql.DB, projectKey string, options ForgetOptions) (int, error) {
	pinGuard := " AND pinned = 0"
	if options.Force {
		pinGuard = ""
	}
	var deleted int
	var err error

	switch {
	case options.All:
		deleted, err = countAndDelete(database, "project_key = ?"+pinGuard, projectKey)
	case options.ID != "":
		deleted, err = countAndDelete(database, "id = ? AND project_key = ?", options.ID, projectKey)
	case options.Tool != "":
		deleted, err = countAndDelete(database, "tool_name = ? AND project_key = ?"+pinGuard, options.Tool, projectKey)
	case options.SessionID != "":
		deleted, err = countAndDelete(database, "session_id = ? AND project_key = ?"+pinGuard, options.SessionID, projectKey)
	case options.OlderThanDays != nil:
		cutoff := nowUnix() - int64(*options.OlderThanDays)*86400
		deleted, err = countAndDelete(database, "created_at < ? AND project_key = ?"+pinGuard, cutoff, projectKey)
	}
	if err != nil {
		return 0, err
	}

	if deleted >= VacuumThreshold {
		if _, err := database.Exec("PRAGMA incremental_vacuum"); err != nil {
			logx.Warn("incremental_vacuum failed — " + err.Error())
		}
	}
	return deleted, nil
}

// PruneExpired deletes non-pinned items created more than calendarDays ago.
func PruneExpired(database *sql.DB, projectKey string, calendarDays int) (int, error) {
	cutoff := nowUnix() - int64(calendarDays)*86400
	return countAndDelete(database, "created_at < ? AND project_key = ? AND pinned = 0", cutoff, projectKey)
}

// RecordSession records a session date (YYYY-MM-DD). No-op if already present.
func RecordSession(database *sql.DB, date string) error {
	_, err := database.Exec(`INSERT OR IGNORE INTO sessions (date) VALUES (?)`, date)
	return err
}

// GetSessionDays returns all recorded session dates in descending order.
func GetSessionDays(database *sql.DB) []string {
	rows, err := database.Query(`SELECT date FROM sessions ORDER BY date DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return out
		}
		out = append(out, d)
	}
	return out
}
