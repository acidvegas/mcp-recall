// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/csv.go

package handlers

import (
	"fmt"
	"strings"
)

const csvMaxPreviewRows = 5

// splitCsvRow splits a single CSV row, handling double-quoted fields with
// embedded commas.
func splitCsvRow(row string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false
	for _, char := range row {
		switch {
		case char == '"':
			inQuotes = !inQuotes
		case char == ',' && !inQuotes:
			fields = append(fields, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(char)
		}
	}
	fields = append(fields, strings.TrimSpace(current.String()))
	return fields
}

func nonEmptyLines(raw string) []string {
	var out []string
	for _, l := range strings.Split(raw, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// csvHandler emits a header row, the first 5 data rows as key=value pairs, and a
// row/column count summary.
func csvHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	lines := nonEmptyLines(raw)
	if len(lines) == 0 {
		return Result{Summary: "(empty CSV)", OriginalSize: originalSize}
	}

	headerCols := splitCsvRow(lines[0])
	dataRows := lines[1:]
	totalRows := len(dataRows)

	nPreview := totalRows
	if nPreview > csvMaxPreviewRows {
		nPreview = csvMaxPreviewRows
	}
	var previewLines []string
	for i := 0; i < nPreview; i++ {
		vals := splitCsvRow(dataRows[i])
		nCols := len(headerCols)
		if nCols > 5 {
			nCols = 5
		}
		var pairs []string
		for ci := 0; ci < nCols; ci++ {
			v := ""
			if ci < len(vals) {
				v = vals[ci]
			}
			pairs = append(pairs, fmt.Sprintf("%s=%s", headerCols[ci], v))
		}
		overflow := ""
		if len(headerCols) > 5 {
			overflow = fmt.Sprintf(" [+%d cols]", len(headerCols)-5)
		}
		previewLines = append(previewLines, fmt.Sprintf("  row %d: %s%s", i+1, strings.Join(pairs, ", "), overflow))
	}

	more := ""
	if totalRows > csvMaxPreviewRows {
		more = fmt.Sprintf("\n[…%d more rows]", totalRows-csvMaxPreviewRows)
	}

	headerN := len(headerCols)
	if headerN > 10 {
		headerN = 10
	}
	headerExtra := ""
	if len(headerCols) > 10 {
		headerExtra = fmt.Sprintf(" [+%d more]", len(headerCols)-10)
	}

	out := []string{
		fmt.Sprintf("[%d rows × %d cols]", totalRows, len(headerCols)),
		fmt.Sprintf("headers: %s%s", strings.Join(headerCols[:headerN], ", "), headerExtra),
	}
	out = append(out, previewLines...)
	summary := strings.Join(out, "\n") + more

	return Result{Summary: summary, OriginalSize: originalSize}
}

// looksLikeCsv reports whether text looks like a CSV: at least 3 non-empty
// lines, first line has 2+ commas.
func looksLikeCsv(text string) bool {
	lines := nonEmptyLines(text)
	if len(lines) < 3 {
		return false
	}
	return strings.Count(lines[0], ",") >= 2
}
