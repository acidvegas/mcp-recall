// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/linear.go

package handlers

import (
	"fmt"
	"strings"

	"mcprecall/internal/jsonx"
)

var linearPriority = map[float64]string{
	0: "No Priority", 1: "Urgent", 2: "High", 3: "Medium", 4: "Low",
}

const (
	linearDescChars = 200
	linearMaxList   = 10
)

func notNull(o *jsonx.Obj, key string) bool {
	v, ok := o.Get(key)
	return ok && v != nil
}

func linearPriorityLabel(v any) (string, bool) {
	if f, ok := v.(float64); ok {
		if lbl, ok := linearPriority[f]; ok {
			return lbl, true
		}
	}
	if s, ok := v.(string); ok && s != "" {
		return s, true
	}
	return "", false
}

func linearStateLabel(v any) (string, bool) {
	if o, ok := v.(*jsonx.Obj); ok {
		if n, ok := objStr(o, "name"); ok {
			return n, true
		}
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}

func linearSummariseIssue(issue *jsonx.Obj, includeDesc bool) string {
	var parts []string

	idVal, ok := issue.Get("identifier")
	if !ok || idVal == nil {
		idVal, _ = issue.Get("id")
	}
	if idVal != nil {
		parts = append(parts, jsToString(idVal))
	}
	if t, ok := objStr(issue, "title"); ok {
		parts = append(parts, `"`+t+`"`)
	}

	stateVal, ok := issue.Get("state")
	if !ok {
		stateVal, _ = issue.Get("stateName")
	}
	if s, ok := linearStateLabel(stateVal); ok {
		parts = append(parts, "["+s+"]")
	}

	if pv, ok := issue.Get("priority"); ok {
		if p, ok := linearPriorityLabel(pv); ok {
			parts = append(parts, "Priority: "+p)
		}
	}

	urlVal, ok := issue.Get("url")
	if !ok {
		urlVal, _ = issue.Get("branchName")
	}
	if u, ok := urlVal.(string); ok && strings.HasPrefix(u, "http") {
		parts = append(parts, u)
	}

	lines := []string{strings.Join(parts, " · ")}
	if includeDesc {
		if desc, ok := objStr(issue, "description"); ok && len(desc) > 0 {
			excerpt := trimEnd(firstChars(desc, linearDescChars))
			truncated := ""
			if runeLen(desc) > linearDescChars {
				truncated = "…"
			}
			lines = append(lines, "Description: "+excerpt+truncated)
		}
	}
	return strings.Join(lines, "\n")
}

func linearExtractIssues(parsed any) []*jsonx.Obj {
	if arr, ok := parsed.([]any); ok {
		return filterObjs(arr)
	}
	obj, ok := parsed.(*jsonx.Obj)
	if !ok {
		return nil
	}
	if dataV, ok := obj.Get("data"); ok {
		if data, ok := dataV.(*jsonx.Obj); ok {
			if iv, ok := data.Get("issue"); ok {
				if issue, ok := iv.(*jsonx.Obj); ok {
					return []*jsonx.Obj{issue}
				}
			}
			if isv, ok := data.Get("issues"); ok {
				if issues, ok := isv.(*jsonx.Obj); ok {
					if nodes, ok := objArr(issues, "nodes"); ok {
						return filterObjs(nodes)
					}
				}
			}
		}
	}
	if nodes, ok := objArr(obj, "nodes"); ok {
		return filterObjs(nodes)
	}
	if notNull(obj, "identifier") || nonEmptyStr(obj, "title") {
		return []*jsonx.Obj{obj}
	}
	return nil
}

func filterObjs(arr []any) []*jsonx.Obj {
	var out []*jsonx.Obj
	for _, v := range arr {
		if o, ok := v.(*jsonx.Obj); ok {
			out = append(out, o)
		}
	}
	return out
}

func linearHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		return Result{Summary: firstChars(raw, 500), OriginalSize: originalSize}
	}

	issues := linearExtractIssues(parsed)
	if len(issues) == 0 {
		return Result{Summary: firstChars(raw, 500), OriginalSize: originalSize}
	}
	if len(issues) == 1 {
		return Result{Summary: linearSummariseIssue(issues[0], true), OriginalSize: originalSize}
	}

	n := len(issues)
	if n > linearMaxList {
		n = linearMaxList
	}
	var lines []string
	for i, issue := range issues[:n] {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, linearSummariseIssue(issue, false)))
	}
	more := ""
	if len(issues) > linearMaxList {
		more = fmt.Sprintf("\n…and %d more", len(issues)-linearMaxList)
	}
	return Result{Summary: fmt.Sprintf("%d Linear issues:\n%s%s", len(issues), strings.Join(lines, "\n"), more), OriginalSize: originalSize}
}
