// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/learn/learn_test.go

package learn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateProfileJSONExtract(t *testing.T) {
	tools := []McpTool{
		{Name: "mcp__jira__search_issues", InputProps: []string{"jql", "limit", "fields"}},
		{Name: "mcp__jira__create_issue"},
	}
	toml := generateProfile("jira", tools)
	for _, want := range []string{
		`type = "json_extract"`,
		`id          = "mcp__jira"`,
		`mcp_pattern = "mcp__jira__*"`,
		`"issues",`, // items_path suggestion from "issue" keyword
		`"jql",`,    // input field (limit filtered out)
		`"id",`,     // common field
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("generated TOML missing %q\n%s", want, toml)
		}
	}
	if strings.Contains(toml, `"limit",`) {
		t.Error("limit should be filtered from fields")
	}
}

func TestGenerateProfileJSONTruncate(t *testing.T) {
	tools := []McpTool{{Name: "mcp__x__create_thing"}}
	toml := generateProfile("x", tools)
	if !strings.Contains(toml, `type            = "json_truncate"`) {
		t.Errorf("write-only server should use json_truncate:\n%s", toml)
	}
}

func TestImpliesList(t *testing.T) {
	cases := map[string]bool{
		"mcp__x__list_things":  true,
		"mcp__x__search_docs":  true,
		"mcp__x__get_thing":    false,
		"mcp__x__create_thing": false,
		"mcp__x__update":       false,
	}
	for name, want := range cases {
		if got := impliesList(name, ""); got != want {
			t.Errorf("impliesList(%q) = %v, want %v", name, got, want)
		}
	}
}

// Connects the go-sdk MCP client to the built recall server (a real stdio MCP)
// and verifies tool introspection works end-to-end.
func TestListMcpToolsStdioAgainstServer(t *testing.T) {
	bin, err := filepath.Abs("../../mcprecall")
	if err != nil || fileMissing(bin) {
		t.Skip("mcprecall binary not built; run `go build -o mcprecall ./cmd/mcprecall`")
	}
	tools, err := listMcpToolsStdio(bin, []string{"server"}, map[string]string{"RECALL_DB_PATH": t.TempDir() + "/x.db"}, 15*time.Second)
	if err != nil {
		t.Fatalf("connect/list failed: %v", err)
	}
	if len(tools) < 5 {
		t.Fatalf("expected the recall__ toolset, got %d", len(tools))
	}
	found := false
	for _, tl := range tools {
		if tl.Name == "recall__search" {
			found = true
		}
	}
	if !found {
		t.Errorf("recall__search not among introspected tools")
	}
}

func fileMissing(p string) bool {
	_, err := os.Stat(p)
	return err != nil
}
