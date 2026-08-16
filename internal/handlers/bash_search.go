// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/bash_search.go
//
// Search / listing handlers — grep|rg, ls, find|fd. These commands emit a list
// of items (matches, entries, paths) whose bulk is length, not structure.
// Ports src/handlers/bash-search.ts.
//
// Safety: the user usually wants the items, so these NEVER drop items silently —
// they always report the total count and show a generous capped sample with an
// explicit overflow line, and fall back to the shell handler when the output
// doesn't match the expected shape. The full output stays retrievable via
// recall__*.

package handlers

import (
	"fmt"
	"regexp"
	"strings"
)

const maxSearchSample = 40

// clipSearch truncates with an ellipsis, unlike firstChars which truncates bare.
func clipSearch(s string, n int) string {
	if runeLen(s) > n {
		return firstChars(s, n) + "…"
	}
	return s
}

func overflowLine(total, shown int, noun string) []string {
	if total > shown {
		return []string{fmt.Sprintf("  … (+%d more %s)", total-shown, noun)}
	}
	return nil
}

// ── grep / ripgrep — inline "file:line:content" form (piped, --no-heading) ─────

var grepLineRe = regexp.MustCompile(`^(.+?):(\d+):(.*)$`)

func grepHandler(toolName string, output any) Result {
	stdout := extractStdout(output)
	originalSize := byteLen(ExtractText(output))

	var lines []string
	for _, l := range strings.Split(stdout, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return Result{Summary: "[grep — no matches]", OriginalSize: originalSize}
	}

	type match struct{ file, line, text string }
	var matches []match
	files := map[string]bool{}
	for _, l := range lines {
		if m := grepLineRe.FindStringSubmatch(l); m != nil {
			matches = append(matches, match{m[1], m[2], m[3]})
			files[m[1]] = true
		}
	}

	// Not predominantly file:line:content (grouped rg, plain grep, binary) — the
	// shell handler's 25-line cap is safer than a wrong parse.
	if len(matches) < ceilHalf(len(lines)) {
		return shellHandler(toolName, output)
	}

	header := fmt.Sprintf("grep — %d %s in %d %s",
		len(matches), plural(len(matches), "match", "matches"),
		len(files), plural(len(files), "file", "files"))
	out := []string{header}
	for i, m := range matches {
		if i >= maxSearchSample {
			break
		}
		out = append(out, fmt.Sprintf("  %s:%s: %s", m.file, m.line, clipSearch(strings.TrimSpace(m.text), 100)))
	}
	out = append(out, overflowLine(len(matches), maxSearchSample, "matches")...)
	return Result{Summary: strings.Join(out, "\n"), OriginalSize: originalSize}
}

// ── ls — long (-l), recursive (-R), and plain forms ───────────────────────────

var (
	lsLongRe             = regexp.MustCompile(`^([-dlbcps])[rwxsStT-]{9}[+@.]?\s+\d+\s`)
	lsRecursiveHeaderRe  = regexp.MustCompile(`^(\.?[^\s:]*):$`)
	lsTotalRe            = regexp.MustCompile(`^total\s+\d+$`)
	lsColumnSeparatorRe  = regexp.MustCompile(`\s{2,}|\t`)
	findDiagnosticLineRe = regexp.MustCompile(`^find: `)
)

func lsHandler(toolName string, output any) Result {
	stdout := extractStdout(output)
	originalSize := byteLen(ExtractText(output))

	raw := strings.Split(stdout, "\n")
	var nonEmpty []string
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	if len(nonEmpty) == 0 {
		return Result{Summary: "[ls — empty]", OriginalSize: originalSize}
	}

	// Recursive: directory-header lines like "./src:" grouping entries. `ls -R`
	// always blank-line-separates directory blocks; a plain listing that merely
	// happens to contain colon-suffixed names has no such separator, so require
	// one to avoid misreading a plain listing as recursive (and hiding files).
	var dirHeaders []string
	totals := 0
	for _, l := range nonEmpty {
		trimmed := strings.TrimSpace(l)
		if lsRecursiveHeaderRe.MatchString(trimmed) {
			dirHeaders = append(dirHeaders, l)
		}
		if lsTotalRe.MatchString(trimmed) {
			totals++
		}
	}
	hasBlankSeparator := false
	for _, l := range raw {
		if strings.TrimSpace(l) == "" {
			hasBlankSeparator = true
			break
		}
	}
	if len(dirHeaders) >= 2 && hasBlankSeparator {
		entries := len(nonEmpty) - len(dirHeaders) - totals
		out := []string{fmt.Sprintf("ls -R — %d directories, ~%d entries", len(dirHeaders), entries)}
		for i, d := range dirHeaders {
			if i >= maxSearchSample {
				break
			}
			out = append(out, "  "+strings.TrimSpace(d))
		}
		out = append(out, overflowLine(len(dirHeaders), maxSearchSample, "directories")...)
		return Result{Summary: strings.Join(out, "\n"), OriginalSize: originalSize}
	}

	// Long format: perm-string lines. Count dirs vs files.
	var longLines []string
	for _, l := range nonEmpty {
		if lsLongRe.MatchString(l) {
			longLines = append(longLines, l)
		}
	}
	if len(longLines) >= ceilHalf(len(nonEmpty)) {
		dirs := 0
		var names []string
		for _, l := range longLines {
			if m := lsLongRe.FindStringSubmatch(l); m != nil && m[1] == "d" {
				dirs++
			}
			fields := strings.Fields(l)
			if len(fields) > 8 {
				if name := strings.Join(fields[8:], " "); name != "" {
					names = append(names, name)
				}
			}
		}
		files := len(longLines) - dirs
		out := []string{fmt.Sprintf("ls — %d entries (%d %s, %d %s)",
			len(longLines), dirs, plural(dirs, "dir", "dirs"), files, plural(files, "file", "files"))}
		for i, n := range names {
			if i >= maxSearchSample {
				break
			}
			out = append(out, "  "+clipSearch(n, 100))
		}
		out = append(out, overflowLine(len(names), maxSearchSample, "entries")...)
		return Result{Summary: strings.Join(out, "\n"), OriginalSize: originalSize}
	}

	// Plain listing: names one-per-line or column-wrapped. Flatten to tokens.
	var tokens []string
	for _, l := range nonEmpty {
		for _, tok := range lsColumnSeparatorRe.Split(l, -1) {
			if tok = strings.TrimSpace(tok); tok != "" {
				tokens = append(tokens, tok)
			}
		}
	}
	if len(tokens) < 2 {
		return shellHandler(toolName, output)
	}
	out := []string{fmt.Sprintf("ls — %d entries", len(tokens))}
	for i, n := range tokens {
		if i >= maxSearchSample {
			break
		}
		out = append(out, "  "+clipSearch(n, 100))
	}
	out = append(out, overflowLine(len(tokens), maxSearchSample, "entries")...)
	return Result{Summary: strings.Join(out, "\n"), OriginalSize: originalSize}
}

// ── find / fd — one path per line ─────────────────────────────────────────────

func findHandler(toolName string, output any) Result {
	stdout := extractStdout(output)
	originalSize := byteLen(ExtractText(output))

	var paths []string
	for _, l := range strings.Split(stdout, "\n") {
		if l = strings.TrimRight(l, " \t\r"); l != "" {
			paths = append(paths, l)
		}
	}
	if len(paths) == 0 {
		return Result{Summary: "[find — no results]", OriginalSize: originalSize}
	}
	// Guard against non-find output (e.g. find errors / prompts): require most
	// lines to look like paths (no leading whitespace, no "find: " marker).
	pathLike := 0
	for _, p := range paths {
		if !strings.HasPrefix(p, " ") && !strings.HasPrefix(p, "\t") && !findDiagnosticLineRe.MatchString(p) {
			pathLike++
		}
	}
	if pathLike < ceilFraction(len(paths), 0.7) {
		return shellHandler(toolName, output)
	}

	out := []string{fmt.Sprintf("find — %d %s", len(paths), plural(len(paths), "path", "paths"))}
	for i, p := range paths {
		if i >= maxSearchSample {
			break
		}
		out = append(out, "  "+clipSearch(p, 120))
	}
	out = append(out, overflowLine(len(paths), maxSearchSample, "paths")...)
	return Result{Summary: strings.Join(out, "\n"), OriginalSize: originalSize}
}

// ceilHalf is Math.ceil(n * 0.5).
func ceilHalf(n int) int { return (n + 1) / 2 }

// ceilFraction is Math.ceil(n * f) for the fractions used above.
func ceilFraction(n int, f float64) int {
	v := float64(n) * f
	i := int(v)
	if float64(i) < v {
		i++
	}
	return i
}
