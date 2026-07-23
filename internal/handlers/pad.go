// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/pad.go

package handlers

import "strings"

// padEnd pads s with spaces on the right to at least n characters (JS padEnd).
func padEnd(s string, n int) string {
	if l := runeLen(s); l < n {
		return s + strings.Repeat(" ", n-l)
	}
	return s
}
