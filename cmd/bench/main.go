// Command bench runs the mcp-recall compression benchmark: it feeds a corpus of
// representative tool outputs through the real compression pipeline, measures
// exact byte savings and estimated token savings, verifies losslessness, and
// renders the results either as a live TUI or a static report.
//
// This is a development/showcase tool. Its dependencies (Bubble Tea, Lipgloss)
// are linked only into this binary, never into the mcprecall server binary.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
)

func main() {
	report := flag.Bool("report", false, "print a static report instead of the live TUI")
	flag.Parse()

	fixtures, err := LoadCorpus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bench: failed to load corpus: %v\n", err)
		os.Exit(1)
	}

	tok := newTokenizer()

	// Fall back to the static report when there's no interactive terminal
	// (pipes, CI, redirected output) so the tool is always usable.
	useReport := *report || !isatty.IsTerminal(os.Stdout.Fd())

	if useReport {
		results := Run(fixtures, tok)
		summary := Summarize(results, tok.Name())
		RenderReport(summary)
		if summary.Failures > 0 {
			os.Exit(1)
		}
		return
	}

	if err := RunTUI(fixtures, tok); err != nil {
		fmt.Fprintf(os.Stderr, "bench: %v\n", err)
		os.Exit(1)
	}
}
