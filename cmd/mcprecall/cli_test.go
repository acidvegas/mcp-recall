// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// cmd/mcprecall/cli_test.go
//
// Port of tests/cli.test.ts (upstream v1.9.0). The Go binary is named
// `mcprecall` (one word) rather than `mcp-recall`, so binary-name literals in
// the upstream assertions are adjusted accordingly.

package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"mcprecall/internal/gc"
	"mcprecall/internal/server"
)

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// ── version ─────────────────────────────────────────────────────────────────

func TestVersionIsSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(server.Version) {
		t.Errorf("version not semver: %q", server.Version)
	}
}

// ── printHelp ───────────────────────────────────────────────────────────────

func TestPrintHelpTopLevel(t *testing.T) {
	out := captureStdout(printHelp)
	for _, want := range []string{
		"Usage: mcprecall <command>",
		"install", "uninstall", "status", "profiles", "learn", "completions",
		"--help", "--version",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestPrintHelpProfilesSubcommands(t *testing.T) {
	out := captureStdout(printHelp)
	for _, sub := range []string{"seed", "list", "install", "update", "remove", "feed", "check", "retrain", "test"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help missing profiles subcommand %q", sub)
		}
	}
}

// ── completionScript ──────────────────────────────────────────────────────────

func TestCompletionBash(t *testing.T) {
	s, err := completionScript("bash")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "complete -F _mcprecall mcprecall") || !strings.Contains(s, "--machine-readable") {
		t.Error("bash script missing complete command or --machine-readable")
	}
	for _, cmd := range []string{"install", "uninstall", "status", "profiles", "learn", "completions"} {
		if !strings.Contains(s, cmd) {
			t.Errorf("bash script missing %q", cmd)
		}
	}
}

func TestCompletionZsh(t *testing.T) {
	s, err := completionScript("zsh")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^#compdef mcprecall`).MatchString(s) {
		t.Error("zsh script should start with #compdef mcprecall")
	}
	if !strings.Contains(s, "--machine-readable") {
		t.Error("zsh script missing dynamic profile lookup")
	}
}

func TestCompletionFish(t *testing.T) {
	s, err := completionScript("fish")
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"install", "uninstall", "status", "profiles", "learn", "completions"} {
		if !strings.Contains(s, "-a "+cmd) {
			t.Errorf("fish script missing -a %q", cmd)
		}
	}
	if !strings.Contains(s, "--machine-readable") {
		t.Error("fish script missing dynamic profile lookup")
	}
}

func TestCompletionUnknownShell(t *testing.T) {
	if _, err := completionScript("elvish"); err == nil || !strings.Contains(err.Error(), "unknown shell") {
		t.Errorf("expected unknown-shell error, got %v", err)
	}
}

// ── status store footprint ──────────────────────────────────────────────────

func TestPrintStoreFootprint(t *testing.T) {
	small := captureStdout(func() { printStoreFootprint(gc.Footprint{TotalBytes: 1024, DBCount: 1}, 2048) })
	if !strings.Contains(small, "✓ Store: 1.0KB across 1 project database\n") || strings.Contains(small, "gc") {
		t.Errorf("small store: %q", small)
	}
	big := captureStdout(func() { printStoreFootprint(gc.Footprint{TotalBytes: 3 << 30, DBCount: 7}, 2048) })
	if !strings.Contains(big, "! Store:") || !strings.Contains(big, "7 project databases") || !strings.Contains(big, "mcprecall gc") {
		t.Errorf("large store: %q", big)
	}
	off := captureStdout(func() { printStoreFootprint(gc.Footprint{TotalBytes: 3 << 30, DBCount: 7}, 0) })
	if strings.Contains(off, "!") {
		t.Errorf("gc_reminder_mb=0 should disable the nudge: %q", off)
	}
}
