package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

const (
	defaultRecordViewerWidth  = 80
	defaultRecordViewerHeight = 24
	minimumRecordViewerWidth  = 20
	minimumRecordViewerHeight = 8
)

type recordViewerModeModel struct {
	width  int
	height int
}

func newRecordViewerModeModel() recordViewerModeModel {
	return recordViewerModeModel{
		width:  defaultRecordViewerWidth,
		height: defaultRecordViewerHeight,
	}
}

func (m *recordViewerModeModel) SetSize(width, height int) {
	m.width = clampEditorSize(width, minimumRecordViewerWidth)
	m.height = clampEditorSize(height, minimumRecordViewerHeight)
}

func (m recordViewerModeModel) View(query QueryContext) string {
	latest := query.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		return "Record viewer\n\nRun a query that returns rows, then press ctrl+x."
	}

	result := latest.PreservedResult
	header := []string{
		"Record viewer",
		fmt.Sprintf("Query: %s", summarizeViewerQuery(latest.Query, m.width)),
		fmt.Sprintf("Rows: %d  Columns: %d", len(result.Rows), len(result.Columns)),
	}

	body := renderRecordViewerTable(result, m.width, m.height-len(header)-2)
	if body == "" {
		body = "(no visible rows)"
	}

	return strings.Join(append(header, "", body), "\n")
}

func (m recordViewerModeModel) Footer(connectionName, dialect string, query QueryContext) string {
	parts := []string{"Record viewer"}
	if label := strings.TrimSpace(connectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}
	if label := strings.TrimSpace(dialect); label != "" {
		parts = append(parts, fmt.Sprintf("dialect %s", label))
	}
	if latest := query.LatestResult; latest != nil && latest.PreservedResult != nil {
		parts = append(parts, fmt.Sprintf("%d rows", len(latest.PreservedResult.Rows)))
	}
	parts = append(parts, "ctrl+x mode", "ctrl+c quit")
	return strings.Join(parts, " | ")
}

func renderRecordViewerTable(result *db.ResultSet, width, _ int) string {
	if result == nil {
		return ""
	}

	columns := viewerColumnNames(result.Columns)
	widths := viewerColumnWidths(columns, result.Rows)
	viewerRows := make([][]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		values := make([]string, len(columns))
		for i := range columns {
			if i < len(row.Values) {
				values[i] = formatRecordViewerValue(row.Values[i])
			}
		}
		viewerRows = append(viewerRows, values)
	}

	lines := []string{
		renderInlineResultLine(columns, widths),
		renderInlineSeparator(widths),
	}

	for i := range viewerRows {
		lines = append(lines, renderInlineResultLine(viewerRows[i], widths))
	}

	if len(viewerRows) == 0 {
		lines = append(lines, "(no rows)")
	}

	return trimRenderedWidth(strings.Join(lines, "\n"), width)
}

func viewerColumnNames(columns []db.ResultColumn) []string {
	names := make([]string, 0, len(columns))
	for i, column := range columns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		names = append(names, name)
	}
	return names
}

func viewerColumnWidths(columns []string, rows []db.ResultRow) []int {
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = runeWidth(column)
	}
	for _, row := range rows {
		for i := range columns {
			if i >= len(row.Values) {
				continue
			}
			formatted := formatRecordViewerValue(row.Values[i])
			if width := runeWidth(formatted); width > widths[i] {
				widths[i] = width
			}
		}
	}
	return widths
}

func formatRecordViewerValue(value db.ResultValue) string {
	switch value.Kind {
	case db.ValueKindNull:
		return "NULL"
	case db.ValueKindBool:
		if typed, ok := value.Value.(bool); ok {
			if typed {
				return "true"
			}
			return "false"
		}
	case db.ValueKindInteger, db.ValueKindFloat, db.ValueKindDecimal, db.ValueKindString:
		return fmt.Sprint(value.Value)
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return fmt.Sprintf("0x%x", typed)
		}
	case db.ValueKindTime:
		if typed, ok := value.Value.(time.Time); ok {
			return typed.Format(time.RFC3339)
		}
	}

	if value.Value == nil {
		return "NULL"
	}

	return fmt.Sprint(value.Value)
}

func summarizeViewerQuery(query string, width int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
	if trimmed == "" {
		return "(unknown query)"
	}

	maxWidth := max(20, width-8)
	if runeWidth(trimmed) <= maxWidth {
		return trimmed
	}

	runes := []rune(trimmed)
	if maxWidth <= 3 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-3]) + "..."
}

func trimRenderedWidth(value string, width int) string {
	if width <= 0 {
		return value
	}

	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if runeWidth(line) <= width {
			continue
		}
		runes := []rune(line)
		if width <= 3 {
			lines[i] = string(runes[:width])
			continue
		}
		lines[i] = string(runes[:width-3]) + "..."
	}
	return strings.Join(lines, "\n")
}
