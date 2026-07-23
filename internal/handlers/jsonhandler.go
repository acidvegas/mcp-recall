// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/jsonhandler.go

package handlers

import (
	"fmt"

	"mcprecall/internal/jsonx"
)

const (
	jsonMaxDepth      = 3
	jsonMaxArrayItems = 3
)

func truncateJSON(value any, depth int) any {
	if depth > jsonMaxDepth {
		return "…"
	}
	switch t := value.(type) {
	case []any:
		n := len(t)
		if n > jsonMaxArrayItems {
			n = jsonMaxArrayItems
		}
		items := make([]any, 0, n+1)
		for _, v := range t[:n] {
			items = append(items, truncateJSON(v, depth+1))
		}
		if more := len(t) - jsonMaxArrayItems; more > 0 {
			items = append(items, fmt.Sprintf("…%d more", more))
		}
		return items
	case *jsonx.Obj:
		res := jsonx.NewObj()
		for _, k := range t.Keys() {
			v, _ := t.Get(k)
			res.Set(k, truncateJSON(v, depth+1))
		}
		return res
	default:
		return value
	}
}

// jsonHandler truncates deeply nested JSON to depth 3, capping arrays at 3 items
// with an overflow note. Fallback for unrecognised JSON tool outputs.
func jsonHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		excerpt := trimEnd(firstChars(raw, 500))
		if runeLen(excerpt) < runeLen(raw) {
			return Result{Summary: excerpt + "\n…", OriginalSize: originalSize}
		}
		return Result{Summary: excerpt, OriginalSize: originalSize}
	}

	// Compact (no indentation) — smaller inline summary, keys kept verbatim.
	summary := jsonx.Compact(truncateJSON(parsed, 0))
	return Result{Summary: summary, OriginalSize: originalSize}
}
