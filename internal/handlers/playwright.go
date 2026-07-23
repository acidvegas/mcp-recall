// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/playwright.go

package handlers

import (
	"regexp"
	"strings"
)

var interactiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "input": true,
	"checkbox": true, "radio": true, "select": true, "combobox": true,
	"menuitem": true, "tab": true, "searchbox": true, "spinbutton": true,
	"slider": true, "switch": true,
}

var textRoles = map[string]bool{
	"heading": true, "paragraph": true, "statictext": true, "text": true,
	"label": true, "status": true, "alert": true, "cell": true,
	"columnheader": true, "rowheader": true,
}

const (
	maxInteractive = 20
	maxTextChars   = 400
)

// accessibility tree lines look like:  - role "label" [attr=val]
var playwrightLineRe = regexp.MustCompile(`(?i)^-\s+(\w+)\s+"([^"]*)"(.*)$`)

// playwrightHandler compresses browser_snapshot accessibility trees into
// interactive elements and visible text, discarding raw HTML structure.
func playwrightHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	var interactive []string
	var textChunks []string

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		m := playwrightLineRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		role, label := m[1], m[2]
		roleLower := strings.ToLower(role)

		if interactiveRoles[roleLower] && len(interactive) < maxInteractive {
			interactive = append(interactive, `[`+roleLower+` "`+label+`"]`)
		} else if textRoles[roleLower] && strings.TrimSpace(label) != "" {
			textChunks = append(textChunks, strings.TrimSpace(label))
		}
	}

	textContent := trimEnd(firstChars(strings.Join(textChunks, " "), maxTextChars))

	var parts []string
	if len(interactive) > 0 {
		parts = append(parts, "Interactive: "+strings.Join(interactive, ", "))
	}
	if len(textContent) > 0 {
		parts = append(parts, "Visible text: "+textContent)
	}

	summary := "[snapshot: no interactive elements or visible text extracted]"
	if len(parts) > 0 {
		summary = strings.Join(parts, "\n")
	}
	return Result{Summary: summary, OriginalSize: originalSize}
}
