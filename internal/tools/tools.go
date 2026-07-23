// Package tools implements the recall__* MCP tool logic: pure functions that
// take a DB + args and return a formatted text response. Ports src/tools.ts.
package tools

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"mcprecall/internal/config"
	"mcprecall/internal/db"
	"mcprecall/internal/format"
	"mcprecall/internal/jsonx"
)

// ContextEmptyResponse is returned when there is nothing to show / inject.
const ContextEmptyResponse = "[recall: no context available yet — use recall tools to build up your context store]"

const (
	searchExcerptLen  = 120
	noteExcerptLen    = 200
	contextExcerptLen = 100
	snippetMax        = 150
	listToolColWidth  = 40
	listIDColWidth    = 16
)

// ── display helpers ───────────────────────────────────────────────────────────

func formatDate(unixSecs int64) string {
	return time.Unix(unixSecs, 0).UTC().Format("2006-01-02")
}

func runeLen(s string) int { return len([]rune(s)) }

func firstChars(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func oneline(s string) string { return strings.ReplaceAll(s, "\n", " ") }

func padEnd(s string, n int) string {
	if l := runeLen(s); l < n {
		return s + strings.Repeat(" ", n-l)
	}
	return s
}

func padStart(s string, n int) string {
	if l := runeLen(s); l < n {
		return strings.Repeat(" ", n-l) + s
	}
	return s
}

// toFixed mimics JS Number.toFixed: round half away from zero, n decimals.
func toFixed(x float64, n int) string {
	p := math.Pow(10, float64(n))
	return strconv.FormatFloat(math.Round(x*p)/p, 'f', n, 64)
}

// groupInt renders an integer with en-US thousands separators (toLocaleString).
func groupInt(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	out := b.String()
	if neg {
		return "-" + out
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func reductionPct(original, summary int) string {
	if original == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", int(math.Round((1-float64(summary)/float64(original))*100)))
}

func itemHeader(item db.StoredOutput) string {
	return fmt.Sprintf("[recall:%s · %s · %s · %s→%s]", item.ID, item.ToolName, formatDate(item.CreatedAt),
		format.Bytes(item.OriginalSize), format.Bytes(item.SummarySize))
}

// ── recall__retrieve ──────────────────────────────────────────────────────────

type RetrieveArgs struct {
	ID       string
	Query    string
	MaxBytes int    // 0 = use config default
	Mode     string // "summary" | "peek" | "full"; "" = peek when query given, else summary
}

// Retrieve provides graduated retrieval across three tiers: summary (cheapest),
// peek (bounded context window), and full (verbatim, capped). With no explicit
// mode, a query defaults to peek and its absence to summary.
func Retrieve(database *sql.DB, args RetrieveArgs) string {
	cfg := config.Load()
	cap := cfg.Retrieve.DefaultMaxBytes
	if args.MaxBytes != 0 {
		cap = args.MaxBytes
	}

	item, _ := db.RetrieveOutput(database, args.ID)
	if item == nil {
		return fmt.Sprintf(`[recall: no item found with id "%s"]`, args.ID)
	}

	db.RecordAccess(database, args.ID)
	header := itemHeader(*item)

	mode := args.Mode
	if mode == "" {
		if args.Query != "" {
			mode = "peek"
		} else {
			mode = "summary"
		}
	}

	fullCapped := func() string {
		content := firstChars(item.FullContent, cap)
		truncated := ""
		if runeLen(item.FullContent) > cap {
			truncated = fmt.Sprintf("\n…(truncated at %s)", format.Bytes(cap))
		}
		return header + "\n" + content + truncated
	}

	if mode == "summary" {
		return header + "\n" + item.Summary
	}
	if mode == "full" {
		return fullCapped()
	}

	// mode == "peek": bounded context window, with graceful fallbacks.
	if peek := db.RetrievePeek(database, args.ID, args.Query, 3); peek != "" {
		return header + "\n" + peek
	}
	if args.Query != "" {
		if snippet := db.RetrieveSnippet(database, args.ID, args.Query); snippet != "" {
			return header + "\n" + snippet
		}
	}
	return fullCapped()
}

// ── recall__search ────────────────────────────────────────────────────────────

type SearchArgs struct {
	Query string
	Tool  string
	Limit int // 0 = default 5
}

func Search(database *sql.DB, projectKey string, args SearchArgs) string {
	limit := args.Limit
	if limit == 0 {
		limit = 5
	}

	results := db.SearchOutputs(database, args.Query, db.SearchOptions{ProjectKey: projectKey, Limit: limit * 3})

	var filtered []db.StoredOutput
	if args.Tool != "" {
		low := strings.ToLower(args.Tool)
		for _, r := range results {
			if strings.Contains(strings.ToLower(r.ToolName), low) {
				filtered = append(filtered, r)
			}
		}
	} else {
		filtered = results
	}

	items := filtered
	if len(items) > limit {
		items = items[:limit]
	}
	if len(items) == 0 {
		return fmt.Sprintf(`[recall: no results for "%s"]`, args.Query)
	}

	var lines []string
	for i, item := range items {
		excerpt := oneline(firstChars(item.Summary, searchExcerptLen))
		ellipsis := ""
		if runeLen(item.Summary) > searchExcerptLen {
			ellipsis = "…"
		}
		summaryLine := fmt.Sprintf("%d. %s · %s · %s\n   %s%s", i+1, item.ID, item.ToolName, formatDate(item.CreatedAt), excerpt, ellipsis)

		snippet := db.RetrieveSnippet(database, item.ID, args.Query)
		if snippet == "" {
			lines = append(lines, summaryLine)
			continue
		}
		snipText := strings.TrimSpace(oneline(snippet))
		capped := firstChars(snipText, snippetMax)
		trailing := ""
		if runeLen(snipText) > snippetMax {
			trailing = "…"
		}
		lines = append(lines, fmt.Sprintf("%s\n   > …%s%s", summaryLine, capped, trailing))
	}

	return fmt.Sprintf("Found %d result%s for \"%s\":\n\n%s", len(items), plural(len(items)), args.Query, strings.Join(lines, "\n\n"))
}

// ── recall__pin ───────────────────────────────────────────────────────────────

type PinArgs struct {
	ID     string
	Pinned *bool // nil = default true
}

func Pin(database *sql.DB, projectKey string, args PinArgs) string {
	pinned := true
	if args.Pinned != nil {
		pinned = *args.Pinned
	}
	ok, _ := db.PinOutput(database, args.ID, projectKey, pinned)
	if !ok {
		return fmt.Sprintf(`[recall: no item found with id "%s"]`, args.ID)
	}
	verb := "pinned"
	if !pinned {
		verb = "unpinned"
	}
	return fmt.Sprintf("[recall: %s %s]", verb, args.ID)
}

// ── recall__note ──────────────────────────────────────────────────────────────

type NoteArgs struct {
	Text  string
	Title string
}

func Note(database *sql.DB, projectKey string, args NoteArgs) string {
	title := args.Title
	if title == "" {
		title = "(note)"
	}
	excerpt := firstChars(args.Text, noteExcerptLen)
	ellipsis := ""
	if runeLen(args.Text) > noteExcerptLen {
		ellipsis = "…"
	}
	summary := fmt.Sprintf("%s: %s%s", title, excerpt, ellipsis)
	sessionID := time.Now().UTC().Format("2006-01-02")

	stored, err := db.StoreOutput(database, db.StoreInput{
		ProjectKey:   projectKey,
		SessionID:    sessionID,
		ToolName:     "recall__note",
		Summary:      summary,
		FullContent:  args.Text,
		OriginalSize: len(args.Text),
	})
	if err != nil {
		return "[recall: error] " + err.Error()
	}
	return fmt.Sprintf("[recall: note stored as %s]", stored.ID)
}

// ── recall__export ────────────────────────────────────────────────────────────

func storedToObj(s db.StoredOutput) *jsonx.Obj {
	o := jsonx.NewObj()
	o.Set("id", s.ID)
	o.Set("project_key", s.ProjectKey)
	o.Set("session_id", s.SessionID)
	o.Set("tool_name", s.ToolName)
	o.Set("summary", s.Summary)
	o.Set("full_content", s.FullContent)
	o.Set("original_size", float64(s.OriginalSize))
	o.Set("summary_size", float64(s.SummarySize))
	o.Set("created_at", float64(s.CreatedAt))
	o.Set("pinned", float64(s.Pinned))
	o.Set("access_count", float64(s.AccessCount))
	if s.LastAccessed != nil {
		o.Set("last_accessed", float64(*s.LastAccessed))
	} else {
		o.Set("last_accessed", nil)
	}
	if s.InputHash != nil {
		o.Set("input_hash", *s.InputHash)
	} else {
		o.Set("input_hash", nil)
	}
	if s.OutputHash != nil {
		o.Set("output_hash", *s.OutputHash)
	} else {
		o.Set("output_hash", nil)
	}
	return o
}

func Export(database *sql.DB, projectKey string) string {
	items := db.ExportAll(database, projectKey)
	if len(items) == 0 {
		return "[recall: no items to export]"
	}
	arr := make([]any, len(items))
	for i, it := range items {
		arr[i] = storedToObj(it)
	}
	return jsonx.Indent(arr)
}

// ── recall__forget ────────────────────────────────────────────────────────────

type ForgetArgs struct {
	ID            string
	Tool          string
	SessionID     string
	OlderThanDays *int
	All           bool
	Confirmed     bool
	Force         bool
}

func Forget(database *sql.DB, projectKey string, args ForgetArgs) string {
	if args.All && !args.Confirmed {
		return "[recall: clearing all stored items requires confirmed: true]"
	}
	if args.OlderThanDays != nil && *args.OlderThanDays < 1 {
		return "[recall: older_than_days must be at least 1 — use all: true with confirmed: true to delete everything]"
	}

	deleted, _ := db.ForgetOutputs(database, projectKey, db.ForgetOptions{
		ID: args.ID, Tool: args.Tool, SessionID: args.SessionID,
		OlderThanDays: args.OlderThanDays, All: args.All, Force: args.Force,
	})
	if deleted == 0 {
		return "[recall: no items matched — nothing deleted]"
	}
	return fmt.Sprintf("[recall: deleted %d item%s]", deleted, plural(deleted))
}

// ── recall__list_stored ───────────────────────────────────────────────────────

type ListStoredArgs struct {
	Limit  int // 0 = default 10
	Offset int
	Tool   string
	Sort   string // "recent" | "accessed" | "size"
}

func ListStored(database *sql.DB, projectKey string, args ListStoredArgs) string {
	limit := args.Limit
	if limit == 0 {
		limit = 10
	}
	items := db.ListStoredSorted(database, projectKey, args.Tool, args.Sort, limit, args.Offset)

	if len(items) == 0 {
		if args.Offset > 0 {
			return "[recall: no more items]"
		}
		return "[recall: no stored items]"
	}

	var rows []string
	for _, item := range items {
		reduction := reductionPct(item.OriginalSize, item.SummarySize)
		pin := ""
		if item.Pinned != 0 {
			pin = " 📌"
		}
		rows = append(rows, fmt.Sprintf("%s  %s  %s  %s→%s  %s%s",
			item.ID, padEnd(item.ToolName, listToolColWidth), formatDate(item.CreatedAt),
			padStart(format.Bytes(item.OriginalSize), 7), padEnd(format.Bytes(item.SummarySize), 8), reduction, pin))
	}

	header := fmt.Sprintf("%s  %s  %s  %s %s  Red.",
		padEnd("ID", listIDColWidth), padEnd("Tool", listToolColWidth), padEnd("Date", 10),
		padStart("Size", 7), padEnd("→", 9))
	separator := strings.Repeat("-", runeLen(header))

	out := append([]string{header, separator}, rows...)
	return strings.Join(out, "\n")
}

// ── recall__context ───────────────────────────────────────────────────────────

type ContextArgs struct {
	Days  int
	Limit int
}

func Context(database *sql.DB, projectKey string, args ContextArgs) string {
	data := db.GetContext(database, projectKey, args.Days, args.Limit)
	today := time.Now().UTC().Format("2006-01-02")

	isEmpty := len(data.Pinned) == 0 && len(data.Notes) == 0 && len(data.Recent) == 0 &&
		len(data.Hot) == 0 && data.LastSession == nil
	if isEmpty {
		return ContextEmptyResponse
	}

	lines := []string{
		"Context — " + today,
		strings.Repeat("═", 36),
		"Generated " + format.RelativeTime(0),
	}

	if len(data.Pinned) > 0 {
		lines = append(lines, "", fmt.Sprintf("Pinned (%d):", len(data.Pinned)))
		for _, item := range data.Pinned {
			excerpt := oneline(firstChars(item.Summary, contextExcerptLen))
			ellipsis := ""
			if runeLen(item.Summary) > contextExcerptLen {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  📌 %s  %s  %s", item.ID, padEnd(item.ToolName, listToolColWidth), formatDate(item.CreatedAt)))
			lines = append(lines, "    "+excerpt+ellipsis)
		}
	}

	if len(data.Notes) > 0 {
		lines = append(lines, "", fmt.Sprintf("Notes (%d):", len(data.Notes)))
		for _, note := range data.Notes {
			excerpt := oneline(firstChars(note.Summary, 100))
			ellipsis := ""
			if runeLen(note.Summary) > 100 {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  %s  %s", note.ID, formatDate(note.CreatedAt)))
			lines = append(lines, "    "+excerpt+ellipsis)
		}
	}

	if len(data.Recent) > 0 {
		days := args.Days
		if days == 0 {
			days = 7
		}
		lines = append(lines, "", fmt.Sprintf("Recently accessed (last %d day%s, %d item%s):", days, plural(days), len(data.Recent), plural(len(data.Recent))))
		for _, item := range data.Recent {
			excerpt := oneline(firstChars(item.Summary, contextExcerptLen))
			ellipsis := ""
			if runeLen(item.Summary) > contextExcerptLen {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  %s  %s  %s  ×%d", item.ID, padEnd(item.ToolName, listToolColWidth), formatDate(item.CreatedAt), item.AccessCount))
			lines = append(lines, "    "+excerpt+ellipsis)
		}
	}

	if len(data.Hot) > 0 {
		date := ""
		if data.LastSession != nil {
			date = data.LastSession.Date
		}
		lines = append(lines, "", fmt.Sprintf("Hot from last session (%s, %d item%s):", date, len(data.Hot), plural(len(data.Hot))))
		for _, item := range data.Hot {
			excerpt := oneline(firstChars(item.Summary, contextExcerptLen))
			ellipsis := ""
			if runeLen(item.Summary) > contextExcerptLen {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  %s  %s  %s  ×%d", item.ID, padEnd(item.ToolName, listToolColWidth), formatDate(item.CreatedAt), item.AccessCount))
			lines = append(lines, "    "+excerpt+ellipsis)
		}
	}

	if data.LastSession != nil {
		s := data.LastSession
		lines = append(lines, "", fmt.Sprintf("Last session (%s):", s.Date))
		lines = append(lines, fmt.Sprintf("  %d item%s stored · %s → %s (%s reduction)", s.StoredCount, plural(s.StoredCount),
			format.Bytes(s.TotalOriginalBytes), format.Bytes(s.TotalSummaryBytes), reductionPct(s.TotalOriginalBytes, s.TotalSummaryBytes)))
	}

	return strings.Join(lines, "\n")
}

// ── recall__session_summary ───────────────────────────────────────────────────

type SessionSummaryArgs struct {
	SessionID string
	Date      string
}

func SessionSummary(database *sql.DB, projectKey string, args SessionSummaryArgs) string {
	data := db.GetSessionSummary(database, projectKey, db.SessionSummaryOptions{SessionID: args.SessionID, Date: args.Date})
	if data.StoredCount == 0 {
		return fmt.Sprintf("[recall: no items stored for %s]", data.Label)
	}

	accessPlural := "es"
	if data.TotalAccesses == 1 {
		accessPlural = ""
	}
	lines := []string{
		"Session Summary — " + data.Label,
		strings.Repeat("─", 36),
		fmt.Sprintf("Stored: %d item%s · %s → %s (%s reduction)", data.StoredCount, plural(data.StoredCount),
			format.Bytes(data.TotalOriginalBytes), format.Bytes(data.TotalSummaryBytes), reductionPct(data.TotalOriginalBytes, data.TotalSummaryBytes)),
		fmt.Sprintf("Retrieved: %d item%s · %d total access%s", data.AccessedCount, plural(data.AccessedCount), data.TotalAccesses, accessPlural),
		"",
		"Tools stored:",
	}

	const topTools = 5
	for i, t := range data.ToolCounts {
		if i >= topTools {
			break
		}
		lines = append(lines, fmt.Sprintf("  %s×%d", padEnd(t.ToolName, 44), t.Count))
	}
	if len(data.ToolCounts) > topTools {
		lines = append(lines, fmt.Sprintf("  + %d more", len(data.ToolCounts)-topTools))
	}

	if len(data.TopAccessed) > 0 {
		lines = append(lines, "", "Most accessed:")
		for _, item := range data.TopAccessed {
			excerpt := oneline(firstChars(item.Summary, contextExcerptLen))
			ellipsis := ""
			if runeLen(item.Summary) > contextExcerptLen {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  %s (×%d) %s", item.ID, item.AccessCount, item.ToolName))
			lines = append(lines, "    "+excerpt+ellipsis)
		}
	}

	if len(data.Pinned) > 0 {
		lines = append(lines, "", fmt.Sprintf("Pinned: %d", len(data.Pinned)))
		for _, item := range data.Pinned {
			excerpt := oneline(firstChars(item.Summary, contextExcerptLen))
			ellipsis := ""
			if runeLen(item.Summary) > contextExcerptLen {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  📌 %s  %s", item.ID, item.ToolName))
			lines = append(lines, "    "+excerpt+ellipsis)
		}
	}

	if len(data.Notes) > 0 {
		lines = append(lines, "", fmt.Sprintf("Notes: %d", len(data.Notes)))
		for _, note := range data.Notes {
			excerpt := oneline(firstChars(note.Summary, 100))
			ellipsis := ""
			if runeLen(note.Summary) > 100 {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  %s — %s%s", note.ID, excerpt, ellipsis))
		}
	}

	return strings.Join(lines, "\n")
}

// ── recall__stats ─────────────────────────────────────────────────────────────

type StatsArgs struct {
	PinThreshold int
	StaleDays    int
}

func Stats(database *sql.DB, projectKey string, args StatsArgs) string {
	stats := db.GetStats(database, projectKey)
	sessionDays := db.GetSessionDays(database)

	if stats.TotalItems == 0 {
		return "[recall: no data stored for this project yet]"
	}

	saved := stats.TotalOriginalBytes - stats.TotalSummaryBytes
	reductionPctVal := toFixed((1-stats.CompressionRatio)*100, 1)
	tokensSaved := saved / 4

	lines := []string{
		"Session stats for current project:",
		fmt.Sprintf("  Items stored:      %d", stats.TotalItems),
		fmt.Sprintf("  Original size:     %s", format.Bytes(stats.TotalOriginalBytes)),
		fmt.Sprintf("  Compressed size:   %s", format.Bytes(stats.TotalSummaryBytes)),
		fmt.Sprintf("  Saved:             %s (%s%% reduction)", format.Bytes(saved), reductionPctVal),
		fmt.Sprintf("  ~Tokens saved:     ~%s", groupInt(tokensSaved)),
		fmt.Sprintf("  Session days:      %d", len(sessionDays)),
	}

	breakdown := db.GetToolBreakdown(database, projectKey)
	if len(breakdown) > 0 {
		lines = append(lines, "", "By tool (sorted by original size):")
		maxLen := 0
		for _, r := range breakdown {
			if l := runeLen(r.ToolName); l > maxLen {
				maxLen = l
			}
		}
		colW := maxLen
		if colW > 40 {
			colW = 40
		}
		for _, row := range breakdown {
			reduction := " —"
			if row.OriginalBytes > 0 {
				reduction = fmt.Sprintf("%d%%", int(math.Round((1-float64(row.SummaryBytes)/float64(row.OriginalBytes))*100)))
			}
			itemWord := "s"
			if row.Items == 1 {
				itemWord = " "
			}
			lines = append(lines, fmt.Sprintf("  %s  %s item%s  %s → %s  %s",
				padEnd(row.ToolName, colW), padStart(strconv.Itoa(row.Items), 4), itemWord,
				padStart(format.Bytes(row.OriginalBytes), 8), padEnd(format.Bytes(row.SummaryBytes), 8), padStart(reduction, 4)))
		}
	}

	sug := db.GetSuggestions(database, projectKey, db.SuggestionsOptions{PinThreshold: args.PinThreshold, StaleDays: args.StaleDays})
	if len(sug.PinCandidates) > 0 || len(sug.StaleCandidates) > 0 {
		lines = append(lines, "", "Suggestions:")
		if len(sug.PinCandidates) > 0 {
			lines = append(lines, "  📌 Consider pinning:")
			for _, item := range sug.PinCandidates {
				lines = append(lines, fmt.Sprintf("     %s  %s  accessed %d×", item.ID, padEnd(item.ToolName, listToolColWidth), item.AccessCount))
			}
		}
		if len(sug.StaleCandidates) > 0 {
			if len(sug.PinCandidates) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, "  🗑  Never accessed (consider forgetting):")
			now := time.Now().Unix()
			for _, item := range sug.StaleCandidates {
				ageDays := int((now - item.CreatedAt) / 86400)
				lines = append(lines, fmt.Sprintf("     %s  %s  created %d day%s ago", item.ID, padEnd(item.ToolName, listToolColWidth), ageDays, plural(ageDays)))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// ── recall__suggest ───────────────────────────────────────────────────────────

type SuggestArgs struct {
	PinThreshold int
	StaleDays    int
	Limit        int
}

func Suggest(database *sql.DB, projectKey string, args SuggestArgs) string {
	cfg := config.Load()
	pinThreshold := args.PinThreshold
	if pinThreshold == 0 {
		pinThreshold = cfg.Store.PinRecommendationThreshold
	}
	staleDays := args.StaleDays
	if staleDays == 0 {
		staleDays = cfg.Store.StaleItemDays
	}
	sug := db.GetSuggestions(database, projectKey, db.SuggestionsOptions{PinThreshold: pinThreshold, StaleDays: staleDays, Limit: args.Limit})

	if len(sug.PinCandidates) == 0 && len(sug.StaleCandidates) == 0 {
		return "[recall: no suggestions — no frequently accessed unpinned items and no stale items]"
	}

	lines := []string{"Recall suggestions:"}
	if len(sug.PinCandidates) > 0 {
		lines = append(lines, "", "Pin candidates (frequently accessed, not yet pinned):")
		for _, item := range sug.PinCandidates {
			excerpt := oneline(firstChars(item.Summary, 80))
			ellipsis := ""
			if runeLen(item.Summary) > 80 {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  %s  (accessed %d×)  %s", item.ID, item.AccessCount, item.ToolName))
			lines = append(lines, "    "+excerpt+ellipsis)
			lines = append(lines, fmt.Sprintf(`    → recall__pin id="%s"`, item.ID))
		}
	}
	if len(sug.StaleCandidates) > 0 {
		lines = append(lines, "", "Stale items (never accessed, consider forgetting):")
		now := time.Now().Unix()
		for _, item := range sug.StaleCandidates {
			ageDays := int((now - item.CreatedAt) / 86400)
			excerpt := oneline(firstChars(item.Summary, 80))
			ellipsis := ""
			if runeLen(item.Summary) > 80 {
				ellipsis = "…"
			}
			lines = append(lines, fmt.Sprintf("  %s  (%d day%s old)  %s", item.ID, ageDays, plural(ageDays), item.ToolName))
			lines = append(lines, "    "+excerpt+ellipsis)
			lines = append(lines, fmt.Sprintf(`    → recall__forget id="%s"`, item.ID))
		}
	}

	return strings.Join(lines, "\n")
}
