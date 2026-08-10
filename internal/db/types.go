// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/types.go

package db

// StoredOutput is a fully-hydrated row from the stored_outputs table.
type StoredOutput struct {
	ID           string
	ProjectKey   string
	SessionID    string
	ToolName     string
	Summary      string
	FullContent  string
	OriginalSize int
	SummarySize  int
	CreatedAt    int64
	Pinned       int // 0 | 1
	AccessCount  int
	LastAccessed *int64
	InputHash    *string
	OutputHash   *string
}

// StoreInput is the input required to persist a new compressed tool output.
type StoreInput struct {
	ProjectKey   string
	SessionID    string
	ToolName     string
	Summary      string
	FullContent  string
	OriginalSize int
	InputHash    *string
	// OutputHash is the precomputed sha256 of FullContent; StoreOutput derives
	// it when nil.
	OutputHash *string
}

// SearchOptions configures full-text search across stored outputs.
type SearchOptions struct {
	ProjectKey string
	Tool       string // "" = any
	Limit      int    // 0 = default (10)
}

// ListOptions configures paginated listing of stored outputs.
type ListOptions struct {
	ProjectKey string
	Tool       string // "" = any
	Limit      int    // 0 = default (20)
	Offset     int
	Sort       string // "newest" (default) | "oldest"
}

// ForgetOptions selects stored outputs to delete. Exactly one selector should be set.
type ForgetOptions struct {
	ID            string
	Tool          string
	SessionID     string
	OlderThanDays *int
	All           bool
	Force         bool // override pinned protection
}

// Stats holds aggregate storage statistics for a project.
type Stats struct {
	// Total* and CompressionRatio cover intercepted output only — recall__note
	// memory is excluded so it never dilutes the savings figure.
	TotalItems         int
	TotalOriginalBytes int
	TotalSummaryBytes  int
	CompressionRatio   float64
	// Pinned* are store-wide (notes included): pinned bytes are what eviction
	// cannot reclaim, whatever wrote them.
	PinnedItems int
	PinnedBytes int
	// Note* are recall__note memory — stored memory, not interception.
	NoteItems int
	NoteBytes int
}

// ToolBreakdownRow is a per-tool storage stats row.
type ToolBreakdownRow struct {
	ToolName      string
	Items         int
	OriginalBytes int
	SummaryBytes  int
}

// SuggestionsOptions configures GetSuggestions.
type SuggestionsOptions struct {
	PinThreshold int // 0 = default (5)
	StaleDays    int // 0 = default (3)
	Limit        int // 0 = default (3)
}

// SuggestionsData holds pin and stale candidate lists.
type SuggestionsData struct {
	PinCandidates   []StoredOutput
	StaleCandidates []StoredOutput
}

// LastSession is the last-session headline within ContextData.
type LastSession struct {
	Date               string
	StoredCount        int
	TotalOriginalBytes int
	TotalSummaryBytes  int
}

// ContextData is the session-orientation snapshot in five isolated sections.
type ContextData struct {
	Pinned      []StoredOutput
	Notes       []StoredOutput
	Recent      []StoredOutput
	Hot         []StoredOutput
	LastSession *LastSession
}

// SessionSummaryOptions filters GetSessionSummary. Provide session_id or date.
type SessionSummaryOptions struct {
	SessionID string
	Date      string // YYYY-MM-DD, defaults to today (UTC)
}

// ToolCount is a per-tool count within a session summary.
type ToolCount struct {
	ToolName string
	Count    int
}

// TopAccessed is a most-accessed item within a session summary.
type TopAccessed struct {
	ID          string
	ToolName    string
	Summary     string
	AccessCount int
}

// PinnedItem is a pinned item reference within a session summary.
type PinnedItem struct {
	ID       string
	ToolName string
	Summary  string
}

// NoteItem is a note reference within a session summary.
type NoteItem struct {
	ID      string
	Summary string
}

// SessionSummaryData is the digest returned by GetSessionSummary.
type SessionSummaryData struct {
	Label              string
	StoredCount        int
	TotalOriginalBytes int
	TotalSummaryBytes  int
	ToolCounts         []ToolCount
	AccessedCount      int
	TotalAccesses      int
	TopAccessed        []TopAccessed
	Pinned             []PinnedItem
	Notes              []NoteItem
}
