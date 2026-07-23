// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/filesystem.go

package handlers

import (
	"fmt"
	"strings"
)

const fsHeadLines = 50

// filesystemHandler captures a line-count header and the first 50 lines of file
// content, discarding the remainder.
func filesystemHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	lines := strings.Split(raw, "\n")
	totalLines := len(lines)
	n := totalLines
	if n > fsHeadLines {
		n = fsHeadLines
	}
	head := strings.Join(lines[:n], "\n")
	truncated := totalLines > fsHeadLines

	plural := "s"
	if totalLines == 1 {
		plural = ""
	}
	showing := ""
	if truncated {
		showing = fmt.Sprintf(", showing first %d", fsHeadLines)
	}
	header := fmt.Sprintf("[%d line%s%s]", totalLines, plural, showing)
	summary := header + "\n" + head
	if truncated {
		summary += "\n…"
	}
	return Result{Summary: summary, OriginalSize: originalSize}
}
