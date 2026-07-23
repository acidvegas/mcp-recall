// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/slack.go

package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mcprecall/internal/jsonx"
)

const (
	slackMsgChars = 200
	slackMaxMsgs  = 10
)

func formatSlackTs(ts any) string {
	var secs float64
	switch t := ts.(type) {
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return ""
		}
		secs = f
	case float64:
		secs = t
	default:
		return ""
	}
	ms := int64(secs * 1000)
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}

func slackResolveUser(msg *jsonx.Obj) string {
	profVal, ok := msg.Get("user_profile")
	if !ok {
		profVal, _ = msg.Get("profile")
	}
	if prof, ok := profVal.(*jsonx.Obj); ok {
		for _, key := range []string{"display_name", "real_name", "name"} {
			if n, ok := objStr(prof, key); ok && len(n) > 0 {
				return n
			}
		}
	}
	if u, ok := objStr(msg, "username"); ok {
		return u
	}
	if u, ok := objStr(msg, "user"); ok {
		return u
	}
	return "unknown"
}

func slackSummariseMessage(msg *jsonx.Obj) string {
	tsVal, _ := msg.Get("ts")
	ts := formatSlackTs(tsVal)
	user := slackResolveUser(msg)
	text, _ := objStr(msg, "text")
	excerpt := trimEnd(strings.ReplaceAll(firstChars(text, slackMsgChars), "\n", " "))
	truncated := ""
	if runeLen(text) > slackMsgChars {
		truncated = "…"
	}
	prefix := ""
	if ts != "" {
		prefix = "[" + ts + "] "
	}
	return fmt.Sprintf("%s%s: %s%s", prefix, user, excerpt, truncated)
}

func slackResolveChannel(obj *jsonx.Obj) (string, bool) {
	if ch, ok := obj.Get("channel"); ok {
		if s, ok := ch.(string); ok {
			return s, true
		}
		if co, ok := ch.(*jsonx.Obj); ok {
			nameVal, ok := co.Get("name")
			if !ok {
				nameVal, _ = co.Get("id")
			}
			if s, ok := nameVal.(string); ok {
				return "#" + s, true
			}
		}
	}
	if s, ok := objStr(obj, "channelId"); ok {
		return s, true
	}
	return "", false
}

type slackResult struct {
	messages []*jsonx.Obj
	channel  string
}

func slackExtractMessages(parsed any) (*slackResult, bool) {
	if arr, ok := parsed.([]any); ok {
		msgs := filterObjs(arr)
		if len(msgs) > 0 && (notNull(msgs[0], "ts") || notNull(msgs[0], "text")) {
			return &slackResult{messages: msgs, channel: ""}, true
		}
		return nil, false
	}
	obj, ok := parsed.(*jsonx.Obj)
	if !ok {
		return nil, false
	}
	if notNull(obj, "ts") && notNull(obj, "text") {
		ch, _ := slackResolveChannel(obj)
		return &slackResult{messages: []*jsonx.Obj{obj}, channel: ch}, true
	}
	if arr, ok := objArr(obj, "messages"); ok {
		ch, _ := slackResolveChannel(obj)
		return &slackResult{messages: filterObjs(arr), channel: ch}, true
	}
	return nil, false
}

func slackHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		return Result{Summary: firstChars(raw, 500), OriginalSize: originalSize}
	}

	result, ok := slackExtractMessages(parsed)
	if !ok || len(result.messages) == 0 {
		return Result{Summary: firstChars(raw, 500), OriginalSize: originalSize}
	}

	channelPrefix := ""
	if result.channel != "" {
		channelPrefix = result.channel + " — "
	}
	count := len(result.messages)
	n := count
	if n > slackMaxMsgs {
		n = slackMaxMsgs
	}
	var lines []string
	for i, msg := range result.messages[:n] {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, slackSummariseMessage(msg)))
	}
	more := ""
	if count > slackMaxMsgs {
		more = fmt.Sprintf("\n…and %d more messages", count-slackMaxMsgs)
	}
	plural := "s"
	if count == 1 {
		plural = ""
	}
	return Result{
		Summary:      fmt.Sprintf("%s%d message%s:\n%s%s", channelPrefix, count, plural, strings.Join(lines, "\n"), more),
		OriginalSize: originalSize,
	}
}
