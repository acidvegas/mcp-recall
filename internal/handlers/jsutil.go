// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/jsutil.go

package handlers

import (
	"strings"

	"mcprecall/internal/jsonx"
)

// JSToString is the exported form of jsToString, used by the profiles package.
func JSToString(v any) string { return jsToString(v) }

// jsToString reproduces JavaScript's String(v) for the value shapes handlers
// encounter (used for non-object list items and label fallbacks).
func jsToString(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return jsonx.Number(t)
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			if e == nil {
				parts[i] = "" // JS array join renders null/undefined as ""
			} else {
				parts[i] = jsToString(e)
			}
		}
		return strings.Join(parts, ",")
	case *jsonx.Obj:
		return "[object Object]"
	}
	return ""
}

func objArr(o *jsonx.Obj, key string) ([]any, bool) {
	v, ok := o.Get(key)
	if !ok {
		return nil, false
	}
	a, ok := v.([]any)
	return a, ok
}

// truthyStr reports whether a string field is a present non-empty string
// (JS truthiness for `item[key]` where the value is a string).
func nonEmptyStr(o *jsonx.Obj, key string) bool {
	s, ok := objStr(o, key)
	return ok && s != ""
}

// parseErrExcerpt reproduces the common `excerpt.length < raw.length ? excerpt+"\n…" : excerpt`
// fallback used by the JSON-parsing handlers.
func parseErrExcerpt(raw string, originalSize int) Result {
	excerpt := trimEnd(firstChars(raw, 500))
	if runeLen(excerpt) < runeLen(raw) {
		return Result{Summary: excerpt + "\n…", OriginalSize: originalSize}
	}
	return Result{Summary: excerpt, OriginalSize: originalSize}
}
