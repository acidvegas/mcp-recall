// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/sentry.go

package handlers

import (
	"fmt"
	"strings"

	"mcprecall/internal/jsonx"
)

const sentryMaxFrames = 8

func sentryFormatFrame(frame *jsonx.Obj) string {
	location := ""
	if fn, ok := objStr(frame, "filename"); ok {
		location = fn
	}
	if ln, ok := objNum(frame, "lineno"); ok && ln != 0 {
		location += ":" + jsonx.Number(ln)
	}
	fn := "<anonymous>"
	if f, ok := objStr(frame, "function"); ok {
		fn = f
	}
	return fmt.Sprintf("  %s in %s", location, fn)
}

func sentryFirstException(event *jsonx.Obj) *jsonx.Obj {
	excVal, ok := event.Get("exception")
	if !ok {
		return nil
	}
	exc, ok := excVal.(*jsonx.Obj)
	if !ok {
		return nil
	}
	values, ok := objArr(exc, "values")
	if !ok || len(values) == 0 {
		return nil
	}
	first, _ := values[0].(*jsonx.Obj)
	return first
}

func sentrySummariseEvent(event *jsonx.Obj) string {
	var parts []string

	firstException := sentryFirstException(event)
	if firstException != nil {
		typ := "Error"
		if t, ok := objStr(firstException, "type"); ok {
			typ = t
		}
		msg := "(no message)"
		if v, ok := objStr(firstException, "value"); ok {
			msg = v
		}
		parts = append(parts, typ+": "+msg)
	}

	var meta []string
	if s, ok := objStr(event, "level"); ok {
		meta = append(meta, "["+s+"]")
	}
	if s, ok := objStr(event, "environment"); ok {
		meta = append(meta, "env:"+s)
	}
	if s, ok := objStr(event, "release"); ok {
		meta = append(meta, "release:"+s)
	}
	if s, ok := objStr(event, "event_id"); ok {
		meta = append(meta, "id:"+firstChars(s, 8))
	}
	if len(meta) > 0 {
		parts = append(parts, strings.Join(meta, " "))
	}

	if firstException != nil {
		if stVal, ok := firstException.Get("stacktrace"); ok {
			if st, ok := stVal.(*jsonx.Obj); ok {
				if frames, ok := objArr(st, "frames"); ok && len(frames) > 0 {
					start := 0
					if len(frames) > sentryMaxFrames {
						start = len(frames) - sentryMaxFrames
					}
					relevant := frames[start:]
					skipped := len(frames) - len(relevant)
					var header string
					if skipped > 0 {
						header = fmt.Sprintf("Stack (last %d of %d frames):", len(relevant), len(frames))
					} else {
						plural := "s"
						if len(frames) == 1 {
							plural = ""
						}
						header = fmt.Sprintf("Stack (%d frame%s):", len(frames), plural)
					}
					parts = append(parts, header)
					for _, f := range relevant {
						if fo, ok := f.(*jsonx.Obj); ok {
							parts = append(parts, sentryFormatFrame(fo))
						}
					}
				}
			}
		}
	}

	return strings.Join(parts, "\n")
}

func sentryHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		return parseErrExcerpt(raw, originalSize)
	}

	if arr, ok := parsed.([]any); ok {
		n := len(arr)
		if n > 5 {
			n = 5
		}
		var lines []string
		for _, e := range arr[:n] {
			if eo, ok := e.(*jsonx.Obj); ok {
				lines = append(lines, sentrySummariseEvent(eo))
			} else {
				lines = append(lines, jsToString(e))
			}
		}
		more := ""
		if len(arr) > 5 {
			more = fmt.Sprintf("\n…and %d more", len(arr)-5)
		}
		return Result{Summary: strings.Join(lines, "\n---\n") + more, OriginalSize: originalSize}
	}

	if o, ok := parsed.(*jsonx.Obj); ok {
		return Result{Summary: sentrySummariseEvent(o), OriginalSize: originalSize}
	}

	return Result{Summary: jsToString(parsed), OriginalSize: originalSize}
}
