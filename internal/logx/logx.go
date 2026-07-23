// Package logx provides standardized diagnostic logging for mcp-recall.
//
// All output goes to stderr using the format:
//
//	[mcp-recall] <level>: <message>
//
// Debug messages are gated on RECALL_DEBUG=1 or config.debug.enabled.
// Call SetDebugEnabled after loading config to activate config-based debug.
package logx

import (
	"fmt"
	"os"
	"sync/atomic"
)

var configDebugEnabled atomic.Bool

// SetDebugEnabled is called by the config loader to sync config.debug.enabled
// into the log module.
func SetDebugEnabled(enabled bool) {
	configDebugEnabled.Store(enabled)
}

func write(level, msg string) {
	fmt.Fprintf(os.Stderr, "[mcp-recall] %s: %s\n", level, msg)
}

// Info writes an informational message to stderr.
func Info(msg string) { write("info", msg) }

// Warn writes a warning message to stderr.
func Warn(msg string) { write("warn", msg) }

// Error writes an error message to stderr.
func Error(msg string) { write("error", msg) }

// Debug writes a debug message to stderr when RECALL_DEBUG=1 or config debug is on.
func Debug(msg string) {
	if os.Getenv("RECALL_DEBUG") == "1" || configDebugEnabled.Load() {
		write("debug", msg)
	}
}
