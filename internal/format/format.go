// Package format provides shared human-readable formatting helpers.
package format

import "fmt"

// Bytes renders a byte count as B / KB / MB, matching the original's formatBytes.
func Bytes(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

// RelativeTime renders a human-readable relative time from a duration in
// milliseconds, matching the original's formatRelativeTime.
func RelativeTime(ms int64) string {
	seconds := ms / 1000
	if seconds < 60 {
		return "just now"
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm ago", minutes)
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if hours < 24 {
		if remainingMinutes > 0 {
			return fmt.Sprintf("%dh %dm ago", hours, remainingMinutes)
		}
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	plural := "s"
	if days == 1 {
		plural = ""
	}
	return fmt.Sprintf("%d day%s ago", days, plural)
}
