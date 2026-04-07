package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultRecordViewerWidth  = 80
	defaultRecordViewerHeight = 24
	minimumRecordViewerWidth  = 20
	minimumRecordViewerHeight = 8
	recordViewerPageSize      = 300
	recordViewerPrimaryKeyTag = "[pk] "
)

var (
	recordViewerPrimaryKeyHeaderStyle = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "214"})
	recordViewerPrimaryKeyValueStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "214"})
)

type recordViewerColumn struct {
	Header     string
	PrimaryKey bool
}

type recordViewerPageContext struct {
	Index      int
	Number     int
	TotalPages int
	StartRow   int
	EndRow     int
	TotalRows  int
}

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
		return "Record viewer\n\nRun a query that returns rows, then press ctrl+x or ctrl+3."
	}

	result := latest.PreservedResult
	page := recordViewerPageContextFor(query.ViewerPage, len(result.Rows))
	header := []string{
		"Record viewer",
		fmt.Sprintf("Query: %s", summarizeViewerQuery(latest.Query, m.width)),
		fmt.Sprintf("Rows: %d  Columns: %d", len(result.Rows), len(result.Columns)),
		fmt.Sprintf("Page: %d/%d  Showing rows %s", page.Number, page.TotalPages, formatRecordViewerRowRange(page)),
	}

	body := renderRecordViewerTable(result, query.ViewerPage, m.width, m.height-len(header)-2)
	if body == "" {
		body = "(no visible rows)"
	}

	return strings.Join(append(header, "", body), "\n")
}

func (m recordViewerModeModel) Footer(connectionName, dialect string, query QueryContext) string {
	parts := []string{"Record viewer", fmt.Sprintf("layout %s", layoutLabel(query.Layout))}
	if label := strings.TrimSpace(connectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}
	if label := strings.TrimSpace(dialect); label != "" {
		parts = append(parts, fmt.Sprintf("dialect %s", label))
	}
	if latest := query.LatestResult; latest != nil && latest.PreservedResult != nil {
		page := recordViewerPageContextFor(query.ViewerPage, len(latest.PreservedResult.Rows))
		parts = append(parts, fmt.Sprintf("%d rows", page.TotalRows), fmt.Sprintf("page %d/%d", page.Number, page.TotalPages))
	}
	if running := formatRunningIndicator(query.Running); running != "" {
		parts = append(parts, running)
	}
	parts = append(parts, "ctrl+u prev page", "ctrl+d next page", "ctrl+x focus", "ctrl+1 split", "ctrl+2 command", "ctrl+3 viewer", "ctrl+c quit")
	return strings.Join(parts, " | ")
}

func renderRecordViewerTable(result *db.ResultSet, page, width, _ int) string {
	if result == nil {
		return ""
	}

	columns := viewerColumns(result.Columns)
	pageRows, _ := recordViewerRowsForPage(result.Rows, page)
	widths := viewerColumnWidths(columns, pageRows)
	viewerRows := make([][]string, 0, len(pageRows))
	for _, row := range pageRows {
		values := make([]string, len(columns))
		for i := range columns {
			formatted := ""
			if i < len(row.Values) {
				formatted = formatRecordViewerValue(row.Values[i])
			}
			if columns[i].PrimaryKey {
				values[i] = recordViewerPrimaryKeyValueStyle.Render(formatted)
				continue
			}
			values[i] = formatted
		}
		viewerRows = append(viewerRows, values)
	}

	headers := make([]string, len(columns))
	for i, column := range columns {
		headers[i] = column.Header
		if column.PrimaryKey {
			headers[i] = recordViewerPrimaryKeyHeaderStyle.Render(column.Header)
		}
	}

	lines := []string{
		renderInlineResultLine(headers, widths),
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

func viewerColumns(columns []db.ResultColumn) []recordViewerColumn {
	names := make([]recordViewerColumn, 0, len(columns))
	for i, column := range columns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		header := name
		if column.PrimaryKey != nil {
			header = recordViewerPrimaryKeyTag + header
		}
		names = append(names, recordViewerColumn{Header: header, PrimaryKey: column.PrimaryKey != nil})
	}
	return names
}

func viewerColumnWidths(columns []recordViewerColumn, rows []db.ResultRow) []int {
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = ansi.StringWidth(column.Header)
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

func recordViewerRowCount(latest *LatestResultContext) int {
	if latest == nil || latest.PreservedResult == nil {
		return 0
	}

	return len(latest.PreservedResult.Rows)
}

func clampRecordViewerPage(page, totalRows int) int {
	totalPages := recordViewerTotalPages(totalRows)
	if totalPages <= 1 {
		return 0
	}
	if page < 0 {
		return 0
	}
	if page >= totalPages {
		return totalPages - 1
	}
	return page
}

func recordViewerTotalPages(totalRows int) int {
	if totalRows <= 0 {
		return 1
	}

	return (totalRows-1)/recordViewerPageSize + 1
}

func recordViewerPageContextFor(page, totalRows int) recordViewerPageContext {
	clamped := clampRecordViewerPage(page, totalRows)
	context := recordViewerPageContext{
		Index:      clamped,
		Number:     clamped + 1,
		TotalPages: recordViewerTotalPages(totalRows),
		TotalRows:  totalRows,
	}
	if totalRows == 0 {
		return context
	}

	start := clamped * recordViewerPageSize
	end := min(start+recordViewerPageSize, totalRows)
	context.StartRow = start + 1
	context.EndRow = end
	return context
}

func recordViewerRowsForPage(rows []db.ResultRow, page int) ([]db.ResultRow, recordViewerPageContext) {
	context := recordViewerPageContextFor(page, len(rows))
	if len(rows) == 0 {
		return nil, context
	}

	start := context.StartRow - 1
	end := context.EndRow
	return rows[start:end], context
}

func formatRecordViewerRowRange(page recordViewerPageContext) string {
	if page.TotalRows == 0 {
		return "0"
	}

	if page.StartRow == page.EndRow {
		return fmt.Sprintf("%d", page.StartRow)
	}

	return fmt.Sprintf("%d-%d", page.StartRow, page.EndRow)
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
		if width <= 3 {
			lines[i] = ansi.Truncate(line, width, "")
			continue
		}
		lines[i] = ansi.Truncate(line, width, "...")
	}
	return strings.Join(lines, "\n")
}
