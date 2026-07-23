// Package server runs the mcp-recall MCP stdio server exposing the recall__*
// tools. Ports src/server.ts onto the Go MCP SDK.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcprecall/internal/config"
	"mcprecall/internal/db"
	"mcprecall/internal/logx"
	"mcprecall/internal/projectkey"
	"mcprecall/internal/tools"
)

// Version is the reported server version.
const Version = "1.0.0"

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// safe wraps a tool body so a panic becomes a text error instead of crashing.
func safe(fn func() string) (res *mcp.CallToolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			res = textResult(fmt.Sprintf("[recall: error] %v", r))
			err = nil
		}
	}()
	return textResult(fn()), nil
}

func unmarshalArgs(req *mcp.CallToolRequest, dst any) {
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, dst)
	}
}

// Run opens the project DB, registers all recall__* tools, and serves stdio.
func Run(ctx context.Context) error {
	cwd, _ := os.Getwd()
	projectKey := projectkey.Key(cwd)
	database, err := db.Open(db.DefaultDBPath(projectKey))
	if err != nil {
		return err
	}
	defer database.Close()

	srv := mcp.NewServer(&mcp.Implementation{Name: "recall", Version: Version}, nil)

	add := func(name, desc, schema string, handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
		srv.AddTool(&mcp.Tool{Name: name, Description: desc, InputSchema: json.RawMessage(schema)}, handler)
	}

	add("recall__retrieve",
		"Fetch stored content from a previous tool call, in graduated tiers. mode='summary' (default without a query) returns the compressed summary; mode='peek' (default with a query) returns a bounded context window — top matching chunks for a query, or head chunks without — far cheaper than full; mode='full' returns the verbatim content (capped by max_bytes). Escalate summary → peek → full only as needed.",
		`{"type":"object","properties":{"id":{"type":"string","description":"recall_* item ID"},"query":{"type":"string","description":"FTS query to focus a peek on matching chunks"},"mode":{"type":"string","enum":["summary","peek","full"],"description":"Retrieval tier. Defaults to 'peek' when a query is given, else 'summary'."},"max_bytes":{"type":"number","description":"Override default 8KB cap on returned bytes (applies to mode='full')"}},"required":["id"]}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				ID       string `json:"id"`
				Query    string `json:"query"`
				Mode     string `json:"mode"`
				MaxBytes int    `json:"max_bytes"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string {
				return tools.Retrieve(database, tools.RetrieveArgs{ID: a.ID, Query: a.Query, Mode: a.Mode, MaxBytes: a.MaxBytes})
			})
		})

	add("recall__search",
		"Search across all stored tool outputs by content. Use when you don't have an ID but know what you're looking for. Returns matching items with IDs for retrieval.",
		`{"type":"object","properties":{"query":{"type":"string","description":"FTS search query"},"tool":{"type":"string","description":"Filter by tool name (substring match)"},"limit":{"type":"number","description":"Max results to return (default 5)"}},"required":["query"]}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				Query string `json:"query"`
				Tool  string `json:"tool"`
				Limit int    `json:"limit"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string {
				return tools.Search(database, projectKey, tools.SearchArgs{Query: a.Query, Tool: a.Tool, Limit: a.Limit})
			})
		})

	add("recall__pin",
		"Pin an item to protect it from expiry and eviction. Use for important results you want to keep indefinitely. Pass pinned: false to unpin.",
		`{"type":"object","properties":{"id":{"type":"string","description":"Item ID to pin or unpin"},"pinned":{"type":"boolean","description":"true to pin (default), false to unpin"}},"required":["id"]}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				ID     string `json:"id"`
				Pinned *bool  `json:"pinned"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string { return tools.Pin(database, projectKey, tools.PinArgs{ID: a.ID, Pinned: a.Pinned}) })
		})

	add("recall__note",
		"Store arbitrary text as a recall note — conclusions, findings, context that should survive context resets. Use for project memory.",
		`{"type":"object","properties":{"text":{"type":"string","description":"Note content to store"},"title":{"type":"string","description":"Short title for the note"}},"required":["text"]}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				Text  string `json:"text"`
				Title string `json:"title"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string { return tools.Note(database, projectKey, tools.NoteArgs{Text: a.Text, Title: a.Title}) })
		})

	add("recall__export",
		"Export all stored items for this project as JSON. Use before a full clear to preserve data.",
		`{"type":"object","properties":{}}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return safe(func() string { return tools.Export(database, projectKey) })
		})

	add("recall__forget",
		"Delete stored items by ID, tool pattern, session, age, or clear all. Pinned items are skipped unless force: true. Single-ID deletes always bypass pin protection.",
		`{"type":"object","properties":{"id":{"type":"string"},"tool":{"type":"string"},"session_id":{"type":"string"},"older_than_days":{"type":"number"},"all":{"type":"boolean"},"confirmed":{"type":"boolean"},"force":{"type":"boolean"}}}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				ID            string `json:"id"`
				Tool          string `json:"tool"`
				SessionID     string `json:"session_id"`
				OlderThanDays *int   `json:"older_than_days"`
				All           bool   `json:"all"`
				Confirmed     bool   `json:"confirmed"`
				Force         bool   `json:"force"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string {
				return tools.Forget(database, projectKey, tools.ForgetArgs{
					ID: a.ID, Tool: a.Tool, SessionID: a.SessionID, OlderThanDays: a.OlderThanDays,
					All: a.All, Confirmed: a.Confirmed, Force: a.Force,
				})
			})
		})

	add("recall__list_stored",
		"Browse stored items by recency, access frequency, or size. Use to find a specific item to retrieve or forget.",
		`{"type":"object","properties":{"limit":{"type":"number"},"offset":{"type":"number"},"tool":{"type":"string"},"sort":{"type":"string","enum":["recent","accessed","size"]}}}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				Limit  int    `json:"limit"`
				Offset int    `json:"offset"`
				Tool   string `json:"tool"`
				Sort   string `json:"sort"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string {
				return tools.ListStored(database, projectKey, tools.ListStoredArgs{Limit: a.Limit, Offset: a.Offset, Tool: a.Tool, Sort: a.Sort})
			})
		})

	add("recall__stats",
		"Aggregate session efficiency stats — total savings, compression ratio, token savings, session days.",
		`{"type":"object","properties":{}}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			cfg := config.Load()
			return safe(func() string {
				return tools.Stats(database, projectKey, tools.StatsArgs{PinThreshold: cfg.Store.PinRecommendationThreshold, StaleDays: cfg.Store.StaleItemDays})
			})
		})

	add("recall__session_summary",
		"Digest of a single session's activity — tools called, compression savings, most-accessed items, pinned items, notes. Defaults to today.",
		`{"type":"object","properties":{"session_id":{"type":"string"},"date":{"type":"string","description":"YYYY-MM-DD (defaults to today)"}}}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				SessionID string `json:"session_id"`
				Date      string `json:"date"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string {
				return tools.SessionSummary(database, projectKey, tools.SessionSummaryArgs{SessionID: a.SessionID, Date: a.Date})
			})
		})

	add("recall__context",
		"Session orientation: pinned items, recent notes, recently accessed items, and last session headline.",
		`{"type":"object","properties":{"days":{"type":"number"},"limit":{"type":"number"}}}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				Days  int `json:"days"`
				Limit int `json:"limit"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string {
				return tools.Context(database, projectKey, tools.ContextArgs{Days: a.Days, Limit: a.Limit})
			})
		})

	add("recall__suggest",
		"Surface actionable maintenance suggestions: items worth pinning and stale items to consider forgetting.",
		`{"type":"object","properties":{"pin_threshold":{"type":"number"},"stale_days":{"type":"number"},"limit":{"type":"number"}}}`,
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				PinThreshold int `json:"pin_threshold"`
				StaleDays    int `json:"stale_days"`
				Limit        int `json:"limit"`
			}
			unmarshalArgs(req, &a)
			return safe(func() string {
				return tools.Suggest(database, projectKey, tools.SuggestArgs{PinThreshold: a.PinThreshold, StaleDays: a.StaleDays, Limit: a.Limit})
			})
		})

	logx.Debug("recall MCP server starting")
	return srv.Run(ctx, &mcp.StdioTransport{})
}
