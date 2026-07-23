// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/tavily.go

package handlers

import (
	"fmt"
	"strings"

	"mcprecall/internal/jsonx"
)

const (
	tavilySnippetChars = 150
	tavilyMaxResults   = 10
)

func tavilySummariseResult(result *jsonx.Obj) string {
	var parts []string
	if t, ok := objStr(result, "title"); ok && len(t) > 0 {
		parts = append(parts, t)
	}
	if u, ok := objStr(result, "url"); ok {
		parts = append(parts, u)
	}
	if content, ok := objStr(result, "content"); ok && len(content) > 0 {
		snippet := trimEnd(firstChars(content, tavilySnippetChars))
		truncated := ""
		if runeLen(content) > tavilySnippetChars {
			truncated = "…"
		}
		parts = append(parts, snippet+truncated)
	}
	return strings.Join(parts, " · ")
}

func tavilyHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		return parseErrExcerpt(raw, originalSize)
	}

	obj, ok := parsed.(*jsonx.Obj)
	if !ok {
		return Result{Summary: jsToString(parsed), OriginalSize: originalSize}
	}

	var lines []string
	if q, ok := objStr(obj, "query"); ok && len(q) > 0 {
		lines = append(lines, "Query: "+q)
	}
	if a, ok := objStr(obj, "answer"); ok && len(a) > 0 {
		lines = append(lines, "Answer: "+a)
	}

	results, _ := objArr(obj, "results")
	if len(results) > 0 {
		n := len(results)
		if n > tavilyMaxResults {
			n = tavilyMaxResults
		}
		more := 0
		if len(results) > tavilyMaxResults {
			more = len(results) - tavilyMaxResults
		}
		lines = append(lines, fmt.Sprintf("Results (%d):", len(results)))
		for _, r := range results[:n] {
			if ro, ok := r.(*jsonx.Obj); ok {
				lines = append(lines, "  "+tavilySummariseResult(ro))
			}
		}
		if more > 0 {
			lines = append(lines, fmt.Sprintf("  …and %d more", more))
		}
	}

	if len(lines) == 0 {
		return parseErrExcerpt(raw, originalSize)
	}
	return Result{Summary: strings.Join(lines, "\n"), OriginalSize: originalSize}
}
