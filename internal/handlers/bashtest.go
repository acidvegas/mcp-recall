// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/bash_test.go

package handlers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	failLineRe1 = regexp.MustCompile(`^(FAILED|FAIL)\s+`)
	failLineRe2 = regexp.MustCompile(`^[✕✗×●]\s`)
	failLineRe3 = regexp.MustCompile(`^\(fail\)\s`)
	failLineRe4 = regexp.MustCompile(`^--- FAIL:`)

	bunPassRe = regexp.MustCompile(`^(\d+)\s+pass$`)
	bunFailRe = regexp.MustCompile(`^(\d+)\s+fail$`)
	pytestRe  = regexp.MustCompile(`(\d+)\s+passed(?:,\s+(\d+)\s+failed)?(?:,\s+(\d+)\s+(?:skipped|warning))?`)
	jestRe    = regexp.MustCompile(`Tests:\s+(?:(\d+)\s+failed,\s+)?(\d+)\s+passed(?:,\s+(\d+)\s+skipped)?`)
	goOkRe    = regexp.MustCompile(`^ok\s+\S+`)
	goFailRe  = regexp.MustCompile(`^FAIL\s+\S+`)
)

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// testRunnerHandler summarises pytest/jest/bun test/vitest/go test output into
// pass/fail counts plus failure names.
func testRunnerHandler(toolName string, output any) Result {
	stdout := extractStdout(output)
	stderr := extractStderr(output)
	combined := strings.TrimSpace(stdout + "\n" + stderr)
	originalSize := byteLen(ExtractText(output))

	var failureLines []string
	for _, line := range strings.Split(combined, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if failLineRe1.MatchString(t) || failLineRe2.MatchString(t) || failLineRe3.MatchString(t) || failLineRe4.MatchString(t) {
			failureLines = append(failureLines, firstChars(t, 120))
		}
	}

	passed, failed, skipped := 0, 0, 0
	foundSummary := false

	for _, line := range strings.Split(combined, "\n") {
		t := strings.TrimSpace(line)

		if m := bunPassRe.FindStringSubmatch(t); m != nil {
			passed = atoi(m[1])
			foundSummary = true
		}
		if m := bunFailRe.FindStringSubmatch(t); m != nil {
			failed = atoi(m[1])
			foundSummary = true
		}
		if m := pytestRe.FindStringSubmatch(t); m != nil {
			passed = atoi(m[1])
			if m[2] != "" {
				failed = atoi(m[2])
			}
			if m[3] != "" {
				skipped = atoi(m[3])
			}
			foundSummary = true
		}
		if m := jestRe.FindStringSubmatch(t); m != nil {
			if m[1] != "" {
				failed = atoi(m[1])
			}
			passed = atoi(m[2])
			if m[3] != "" {
				skipped = atoi(m[3])
			}
			foundSummary = true
		}
		if goOkRe.MatchString(t) {
			passed++
			foundSummary = true
		}
		if goFailRe.MatchString(t) {
			failed++
			foundSummary = true
		}
	}

	if !foundSummary && len(failureLines) == 0 {
		return shellHandler(toolName, output)
	}

	total := passed + failed + skipped
	status := "pass"
	if failed > 0 {
		status = "FAIL"
	}
	var parts []string
	if passed > 0 {
		parts = append(parts, fmt.Sprintf("%d passed", passed))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	summaryStr := "no results"
	if len(parts) > 0 {
		summaryStr = strings.Join(parts, ", ")
	}

	totalStr := ""
	if total > 0 {
		totalStr = fmt.Sprintf(" (%d total)", total)
	}
	lines := []string{fmt.Sprintf("test runner — %s: %s%s", status, summaryStr, totalStr)}
	if len(failureLines) > 0 {
		lines = append(lines, "  failures:")
		n := len(failureLines)
		if n > maxBuildErrors {
			n = maxBuildErrors
		}
		for _, l := range failureLines[:n] {
			lines = append(lines, "    "+l)
		}
		if len(failureLines) > maxBuildErrors {
			lines = append(lines, fmt.Sprintf("    … (+%d more)", len(failureLines)-maxBuildErrors))
		}
	}
	return Result{Summary: strings.Join(lines, "\n"), OriginalSize: originalSize}
}
