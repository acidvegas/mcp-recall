// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/testutil_test.go

package handlers

import (
	"testing"

	"mcprecall/internal/jsonx"
)

func parseOut(s string) (any, error) { return jsonx.ParseString(s) }

func mustParseT(t *testing.T, s string) any {
	t.Helper()
	v, err := jsonx.ParseString(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return v
}

// mustCompact JSON-quotes a string for embedding in a test payload.
func mustCompact(t *testing.T, s string) string {
	t.Helper()
	return jsonx.Compact(s)
}
