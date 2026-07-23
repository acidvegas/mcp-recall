// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// cmd/bench/corpus.go
//
// The benchmark corpus: representative real-world-shaped tool outputs, embedded
// into the bench binary so runs are fully reproducible. Each entry pairs a raw
// output file with the tool name that drives handler dispatch. Drop additional
// files in and add a manifest row to extend coverage.

package main

import "embed"

//go:embed all:corpus
var corpusFS embed.FS

type manifestEntry struct {
	category string
	name     string
	tool     string
	path     string
}

// manifest is ordered by category for stable display.
var manifest = []manifestEntry{
	// Typed per-tool handlers
	{"api-json", "github issues", "mcp__github__list_issues", "corpus/github_issues.json"},
	{"api-json", "gitlab MRs", "mcp__gitlab__list_merge_requests", "corpus/gitlab_mrs.json"},
	{"api-json", "stripe events", "mcp__stripe__list_events", "corpus/stripe_events.json"},
	{"api-json", "linear issues", "mcp__linear__list_issues", "corpus/linear_issues.json"},
	{"api-json", "sentry issues", "mcp__sentry__list_issues", "corpus/sentry_issues.json"},
	{"chat", "slack history", "mcp__slack__conversations_history", "corpus/slack_history.json"},
	{"search", "tavily results", "mcp__tavily__search", "corpus/tavily_search.json"},
	{"web-dom", "playwright snapshot", "mcp__playwright__browser_snapshot", "corpus/playwright_snapshot.txt"},
	{"tabular", "csv export", "mcp__reporting__csv_export", "corpus/report.csv"},
	{"filesystem", "read_file", "mcp__filesystem__read_file", "corpus/read_file.txt"},
	{"cli", "run_command (npm)", "mcp__server__run_command", "corpus/npm_install.txt"},
	{"database", "postgres query", "mcp__postgres__query", "corpus/postgres_query.json"},

	// Declarative profile (bundled jira, json_extract)
	{"profile", "jira search", "mcp__jira__search_issues", "corpus/jira_search.json"},

	// Content-based fallbacks
	{"fallback", "unknown JSON", "mcp__unknown__get_data", "corpus/unknown_json.json"},
	{"fallback", "generic log", "mcp__unknown__fetch_logs", "corpus/generic_log.txt"},

	// Edge / robustness
	{"edge", "tiny (passthrough)", "mcp__unknown__ping", "corpus/tiny.txt"},
	{"edge", "secret (blocked)", "mcp__unknown__dump_env", "corpus/secret_env.txt"},
	{"edge", "malformed JSON", "mcp__unknown__get_data", "corpus/malformed.json"},
	{"edge", "deeply nested JSON", "mcp__unknown__get_tree", "corpus/nested.json"},
	{"edge", "unicode prose", "mcp__unknown__translate", "corpus/unicode.txt"},
}

// LoadCorpus reads every manifest fixture from the embedded filesystem.
func LoadCorpus() ([]Fixture, error) {
	out := make([]Fixture, 0, len(manifest))
	for _, m := range manifest {
		data, err := corpusFS.ReadFile(m.path)
		if err != nil {
			return nil, err
		}
		out = append(out, Fixture{
			Category: m.category,
			Name:     m.name,
			Tool:     m.tool,
			Content:  string(data),
		})
	}
	out = append(out, generatedFixtures()...)
	return out, nil
}
