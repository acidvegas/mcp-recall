// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/database.go

package handlers

import (
	"fmt"
	"strings"

	"mcprecall/internal/jsonx"
)

const (
	dbMaxPreviewRows = 10
	dbMaxColsDisplay = 8
)

func dbFormatRow(index int, row *jsonx.Obj, cols []string) string {
	n := len(cols)
	if n > dbMaxColsDisplay {
		n = dbMaxColsDisplay
	}
	var pairs []string
	for _, col := range cols[:n] {
		v, ok := row.Get(col)
		var str string
		if !ok || v == nil {
			str = "NULL"
		} else {
			str = jsToString(v)
		}
		if runeLen(str) > 50 {
			str = firstChars(str, 50) + "…"
		}
		pairs = append(pairs, col+"="+str)
	}
	overflow := ""
	if len(cols) > dbMaxColsDisplay {
		overflow = fmt.Sprintf(" [+%d cols]", len(cols)-dbMaxColsDisplay)
	}
	return fmt.Sprintf("  row %d: %s%s", index+1, strings.Join(pairs, ", "), overflow)
}

func dbExtractRows(parsed any) ([]*jsonx.Obj, []string, bool) {
	if obj, ok := parsed.(*jsonx.Obj); ok {
		if rowsArr, ok := objArr(obj, "rows"); ok {
			rows := filterObjs(rowsArr)
			var cols []string
			if fields, ok := objArr(obj, "fields"); ok && len(fields) > 0 {
				for _, f := range fields {
					if fo, ok := f.(*jsonx.Obj); ok {
						if name, ok := objStr(fo, "name"); ok && name != "" {
							cols = append(cols, name)
						}
					}
				}
			} else if len(rows) > 0 {
				cols = rows[0].Keys()
			}
			return rows, cols, true
		}
		if resultsArr, ok := objArr(obj, "results"); ok {
			rows := filterObjs(resultsArr)
			var cols []string
			if len(rows) > 0 {
				cols = rows[0].Keys()
			}
			return rows, cols, true
		}
	}
	if arr, ok := parsed.([]any); ok {
		rows := filterObjs(arr)
		var cols []string
		if len(rows) > 0 {
			cols = rows[0].Keys()
		}
		return rows, cols, true
	}
	return nil, nil, false
}

func databaseHandler(_ string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		return parseErrExcerpt(raw, originalSize)
	}

	rows, cols, ok := dbExtractRows(parsed)
	if !ok {
		return parseErrExcerpt(raw, originalSize)
	}

	if len(rows) == 0 {
		return Result{Summary: fmt.Sprintf("[0 rows × %d cols]\n(empty result set)", len(cols)), OriginalSize: originalSize}
	}

	nCols := len(cols)
	if nCols > dbMaxColsDisplay {
		nCols = dbMaxColsDisplay
	}
	colHeader := "headers: " + strings.Join(cols[:nCols], ", ")
	if len(cols) > dbMaxColsDisplay {
		colHeader += fmt.Sprintf(" [+%d more]", len(cols)-dbMaxColsDisplay)
	}

	nRows := len(rows)
	if nRows > dbMaxPreviewRows {
		nRows = dbMaxPreviewRows
	}
	var previewRows []string
	for i, row := range rows[:nRows] {
		previewRows = append(previewRows, dbFormatRow(i, row, cols))
	}

	more := ""
	if len(rows) > dbMaxPreviewRows {
		more = fmt.Sprintf("\n[…%d more rows]", len(rows)-dbMaxPreviewRows)
	}

	out := []string{fmt.Sprintf("[%d rows × %d cols]", len(rows), len(cols)), colHeader}
	out = append(out, previewRows...)
	return Result{Summary: strings.Join(out, "\n") + more, OriginalSize: originalSize}
}
