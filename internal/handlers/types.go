// Package handlers implements mcp-recall's compression handlers: one per known
// tool family, plus content-based fallbacks. Each handler turns a large tool
// output into a compact summary. Ports src/handlers/*.ts.
package handlers

import (
	"strings"
	"unicode"

	"mcprecall/internal/jsonx"
)

// Result is a compression result: the summary plus the original output size.
type Result struct {
	Summary      string
	OriginalSize int
}

// Handler compresses a tool output into a Result. output is an order-preserving
// jsonx value (nil, bool, float64, string, []any, *jsonx.Obj).
type Handler func(toolName string, output any) Result

// ExtractText extracts plain text from an MCP tool result. MCP results arrive
// as {content: [{type:"text", text:"..."}, ...]} or as a top-level
// content-block array, either possibly as a JSON string. Image, audio and
// resource blocks are dropped so they are never stored or hashed (upstream
// #270). A text-only top-level array is left serialized so jsonHandler routing
// is unchanged. Falls back to JSON serialization for unrecognized shapes.
// Ports extractText.
func ExtractText(output any) string {
	if blocks := asContentBlocks(output); blocks != nil {
		text := joinTextBlocks(blocks)
		// Non-text blocks: never serialize the image bytes into the store.
		if hasNonText(blocks) {
			return text
		}
		if text != "" && !isTopLevelArrayPayload(output) {
			return text
		}
	}
	if s, ok := output.(string); ok {
		return s
	}
	return jsonx.Compact(output)
}

// byteLen returns the UTF-8 byte length (JS Buffer.byteLength(s, "utf8")).
func byteLen(s string) int { return len(s) }

// firstChars returns the first n characters (JS String.slice(0, n) over runes).
func firstChars(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// runeLen returns the character count (JS String.length, rune-approximated).
func runeLen(s string) int { return len([]rune(s)) }

// trimEnd removes trailing whitespace (JS String.trimEnd).
func trimEnd(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

func objStr(o *jsonx.Obj, key string) (string, bool) {
	v, ok := o.Get(key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func objNum(o *jsonx.Obj, key string) (float64, bool) {
	v, ok := o.Get(key)
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}
