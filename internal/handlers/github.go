// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/github.go

package handlers

import (
	"fmt"
	"strings"

	"mcprecall/internal/jsonx"
)

const githubBodyExcerpt = 200

func githubSummariseItem(item *jsonx.Obj) string {
	var parts []string

	if n, ok := objNum(item, "number"); ok {
		parts = append(parts, "#"+jsonx.Number(n))
	}
	if t, ok := objStr(item, "title"); ok {
		parts = append(parts, `"`+t+`"`)
	}
	if s, ok := objStr(item, "state"); ok {
		parts = append(parts, "["+s+"]")
	}
	if name, ok := objStr(item, "name"); ok && !nonEmptyStr(item, "title") {
		parts = append(parts, name)
	}

	urlVal, ok := item.Get("html_url")
	if !ok {
		urlVal, _ = item.Get("url")
	}
	if u, ok := urlVal.(string); ok {
		parts = append(parts, u)
	}

	if labels, ok := objArr(item, "labels"); ok && len(labels) > 0 {
		names := make([]string, len(labels))
		for i, l := range labels {
			if lo, ok := l.(*jsonx.Obj); ok {
				if n, ok := objStr(lo, "name"); ok {
					names[i] = n
					continue
				}
			}
			names[i] = jsToString(l)
		}
		parts = append(parts, "labels: "+strings.Join(names, ", "))
	}

	if body, ok := objStr(item, "body"); ok && len(body) > 0 {
		excerpt := trimEnd(firstChars(body, githubBodyExcerpt))
		truncated := ""
		if runeLen(body) > githubBodyExcerpt {
			truncated = "…"
		}
		parts = append(parts, "body: "+excerpt+truncated)
	}

	return strings.Join(parts, " · ")
}

func githubHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		return parseErrExcerpt(raw, originalSize)
	}

	if arr, ok := parsed.([]any); ok {
		n := len(arr)
		if n > 10 {
			n = 10
		}
		var lines []string
		for _, item := range arr[:n] {
			if o, ok := item.(*jsonx.Obj); ok {
				lines = append(lines, githubSummariseItem(o))
			} else {
				lines = append(lines, jsToString(item))
			}
		}
		more := ""
		if len(arr) > 10 {
			more = fmt.Sprintf("\n…and %d more", len(arr)-10)
		}
		return Result{Summary: strings.Join(lines, "\n") + more, OriginalSize: originalSize}
	}

	if o, ok := parsed.(*jsonx.Obj); ok {
		return Result{Summary: githubSummariseItem(o), OriginalSize: originalSize}
	}

	return Result{Summary: jsToString(parsed), OriginalSize: originalSize}
}
