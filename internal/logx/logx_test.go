// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/logx/logx_test.go

package logx

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func capture(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestLevels(t *testing.T) {
	SetDebugEnabled(false)
	os.Unsetenv("RECALL_DEBUG")
	if got := capture(func() { Info("server started") }); got != "[mcp-recall] info: server started\n" {
		t.Errorf("info = %q", got)
	}
	if got := capture(func() { Warn("VACUUM failed") }); got != "[mcp-recall] warn: VACUUM failed\n" {
		t.Errorf("warn = %q", got)
	}
	if got := capture(func() { Error("unexpected crash") }); got != "[mcp-recall] error: unexpected crash\n" {
		t.Errorf("error = %q", got)
	}
}

func TestDebugGating(t *testing.T) {
	SetDebugEnabled(false)
	os.Unsetenv("RECALL_DEBUG")
	if got := capture(func() { Debug("trace") }); got != "" {
		t.Errorf("debug should be suppressed when unset: %q", got)
	}
	os.Setenv("RECALL_DEBUG", "true")
	if got := capture(func() { Debug("trace") }); got != "" {
		t.Errorf("debug should be suppressed for non-'1': %q", got)
	}
	os.Setenv("RECALL_DEBUG", "1")
	if got := capture(func() { Debug("handler selected") }); got != "[mcp-recall] debug: handler selected\n" {
		t.Errorf("debug = %q", got)
	}
	os.Unsetenv("RECALL_DEBUG")

	// config-based enable (setDebugEnabled) without env var
	SetDebugEnabled(true)
	if got := capture(func() { Debug("setter test") }); got != "[mcp-recall] debug: setter test\n" {
		t.Errorf("setter debug = %q", got)
	}
	SetDebugEnabled(false)
	if got := capture(func() { Debug("after reset") }); got != "" {
		t.Errorf("after disable should be silent: %q", got)
	}
}
