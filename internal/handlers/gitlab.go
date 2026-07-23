// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/gitlab.go

package handlers

import (
	"fmt"
	"strings"

	"mcprecall/internal/jsonx"
)

const gitlabDescExcerpt = 200

func gitlabSummariseItem(item *jsonx.Obj) string {
	var parts []string

	if iid, ok := objNum(item, "iid"); ok {
		parts = append(parts, "!"+jsonx.Number(iid))
	} else if id, ok := objNum(item, "id"); ok {
		if _, hasIid := item.Get("iid"); !hasIid {
			parts = append(parts, "#"+jsonx.Number(id))
		}
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
	if u, ok := objStr(item, "web_url"); ok {
		parts = append(parts, u)
	}

	if labels, ok := objArr(item, "labels"); ok && len(labels) > 0 {
		names := make([]string, len(labels))
		for i, l := range labels {
			names[i] = jsToString(l)
		}
		parts = append(parts, "labels: "+strings.Join(names, ", "))
	}

	bodyVal, ok := item.Get("description")
	if !ok {
		bodyVal, _ = item.Get("body")
	}
	if body, ok := bodyVal.(string); ok && len(body) > 0 {
		excerpt := trimEnd(firstChars(body, gitlabDescExcerpt))
		truncated := ""
		if runeLen(body) > gitlabDescExcerpt {
			truncated = "…"
		}
		parts = append(parts, "description: "+excerpt+truncated)
	}

	return strings.Join(parts, " · ")
}

func gitlabHandler(_ string, output any) Result {
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
				lines = append(lines, gitlabSummariseItem(o))
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
		return Result{Summary: gitlabSummariseItem(o), OriginalSize: originalSize}
	}

	return Result{Summary: jsToString(parsed), OriginalSize: originalSize}
}
