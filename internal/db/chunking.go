// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/db/chunking.go

package db

import "strings"

const (
	// ChunkSize is the fixed chunk length for FTS retrieval.
	ChunkSize = 512
	// ChunkOverlap is the overlap between consecutive chunks.
	ChunkOverlap = 64
)

// ChunkText splits text into overlapping fixed-size chunks for precise FTS
// retrieval. Short texts (<= ChunkSize) return a single-element slice.
//
// Note: chunking operates on UTF-16 code units in the original (JS string
// .length / .slice). Here it operates on runes so multi-byte content chunks
// on character boundaries; ASCII (the overwhelming majority of tool output)
// is identical to the original.
func ChunkText(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{}
	}
	if len(runes) <= ChunkSize {
		return []string{text}
	}
	var chunks []string
	step := ChunkSize - ChunkOverlap
	for pos := 0; pos < len(runes); pos += step {
		end := pos + ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[pos:end]))
	}
	return chunks
}

// SanitizeFtsQuery escapes an FTS5 query to prevent syntax errors from user
// input, wrapping each whitespace-separated term in double-quotes so FTS5
// treats special characters (AND, OR, NOT, NEAR, brackets) as literals.
func SanitizeFtsQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return `""`
	}
	terms := strings.Fields(trimmed)
	for i, t := range terms {
		terms[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(terms, " ")
}
