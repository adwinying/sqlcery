package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/charmbracelet/x/ansi"
)

const ResultsPanePageSize = 300
const ResultsPaneScrollOff = 5

// ResultsPaneViewContext is the complete set of inputs the Results Pane
// rendering functions need; internal/app constructs it from InteractionState.
type ResultsPaneViewContext struct {
	Result          *db.ResultSet
	Page            int
	MarkedRows      []int
	Statement       string
	IsSplit         bool
	Width           int
	Height          int
	SelectedRow     int
	SelectedColumn  int
	SelectionActive bool
	ColScrollOffset int
	ViewportStart   int
}

type ResultsPaneColumn struct {
	Header     string
	PrimaryKey bool
}

type ResultsPanePageContext struct {
	Index      int
	Number     int
	TotalPages int
	StartRow   int
	EndRow     int
	TotalRows  int
}

type ResultsPaneSelection struct {
	Row    int
	Column int
	Active bool
}

type ResultsPaneRenderState struct {
	Active          ResultsPaneSelection
	SelectedRows    map[int]struct{}
	ColScrollOffset int
	ViewportStart   int
}

type ResultsPanePreparedPageKey struct {
	Result *db.ResultSet
	Page   int
}

type ResultsPanePreparedPage struct {
	Key     ResultsPanePreparedPageKey
	Context ResultsPanePageContext
	Headers []string
	Widths  []int
	Rows    [][]string
}

// PrepareResultsPanePage computes a page of results from result, ready to render.
func PrepareResultsPanePage(result *db.ResultSet, page int) *ResultsPanePreparedPage {
	if result == nil {
		return &ResultsPanePreparedPage{}
	}

	columns := resultsPaneColumns(result.Columns)
	pageRows, context := resultsPaneRowsForPage(result.Rows, page)
	prepared := &ResultsPanePreparedPage{
		Key:     ResultsPanePreparedPageKey{Result: result, Page: page},
		Context: context,
		Headers: make([]string, len(columns)),
		Widths:  make([]int, len(columns)),
		Rows:    make([][]string, 0, len(pageRows)),
	}

	for i, column := range columns {
		prepared.Headers[i] = column.Header
		prepared.Widths[i] = ansi.StringWidth(column.Header)
	}

	for _, row := range pageRows {
		values := make([]string, len(columns))
		for i := range columns {
			formatted := ""
			if i < len(row.Values) {
				formatted = resultsPaneFormatValue(row.Values[i])
			}
			values[i] = formatted
			if width := ansi.StringWidth(formatted); width > prepared.Widths[i] {
				prepared.Widths[i] = width
			}
		}
		prepared.Rows = append(prepared.Rows, values)
	}

	return prepared
}

// RenderPreparedResultsPanePage renders a prepared page as a formatted table string.
func RenderPreparedResultsPanePage(prepared *ResultsPanePreparedPage, width, height int, state ResultsPaneRenderState) string {
	if prepared == nil {
		return ""
	}

	colOffset := state.ColScrollOffset
	if colOffset < 0 {
		colOffset = 0
	}
	if colOffset >= len(prepared.Widths) {
		colOffset = max(0, len(prepared.Widths)-1)
	}

	headers := prepared.Headers[colOffset:]
	widths := prepared.Widths[colOffset:]

	lines := []string{
		renderResultsPaneInlineResultLine(headers, widths),
		renderResultsPaneInlineSeparator(widths),
	}

	if len(prepared.Rows) == 0 {
		lines = append(lines, AppTheme.ResultsPaneEmpty.Render("(no rows)"))
		return resultsPaneTrimWidth(strings.Join(lines, "\n"), width)
	}

	start, end := resultsPaneVisibleRowWindow(prepared.Context, len(prepared.Rows), height, state.Active, state.ViewportStart)
	for rowIndex := start; rowIndex < end; rowIndex++ {
		absoluteRowIndex := prepared.Context.StartRow - 1 + rowIndex
		values := append([]string(nil), prepared.Rows[rowIndex][colOffset:]...)
		isActiveRow := state.Active.Active && state.Active.Row == absoluteRowIndex
		isMarked := resultsPaneRowSelectedSet(state.SelectedRows, absoluteRowIndex)
		line := renderResultsPaneInlineResultLine(values, widths)
		if isActiveRow && width > 0 {
			if pad := width - ansi.StringWidth(line); pad > 0 {
				line = line + strings.Repeat(" ", pad)
			}
		}
		switch {
		case isMarked && isActiveRow:
			line = AppTheme.ResultsMarkedActiveRow.Render(line)
		case isMarked:
			line = AppTheme.ResultsMarkedRow.Render(line)
		case isActiveRow:
			line = AppTheme.ResultsActiveRow.Render(line)
		}
		lines = append(lines, line)
	}

	return resultsPaneTrimWidth(strings.Join(lines, "\n"), width)
}

// ResultsPaneSelectedRowSet converts a []int slice of marked rows into a set.
func ResultsPaneSelectedRowSet(rows []int) map[int]struct{} {
	if len(rows) == 0 {
		return nil
	}
	selected := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		selected[row] = struct{}{}
	}
	return selected
}

// ResultsPaneFormatRowRange formats the row range shown in the pane footer.
func ResultsPaneFormatRowRange(page ResultsPanePageContext) string {
	if page.TotalRows == 0 {
		return "0"
	}
	if page.StartRow == page.EndRow {
		return fmt.Sprintf("%d", page.StartRow)
	}
	return fmt.Sprintf("%d-%d", page.StartRow, page.EndRow)
}

// ResultsPanePageContextFor returns the page context for the given page and total rows.
func ResultsPanePageContextFor(page, totalRows int) ResultsPanePageContext {
	clamped := ClampResultsPanePage(page, totalRows)
	context := ResultsPanePageContext{
		Index:      clamped,
		Number:     clamped + 1,
		TotalPages: resultsPaneTotalPages(totalRows),
		TotalRows:  totalRows,
	}
	if totalRows == 0 {
		return context
	}
	start := clamped * ResultsPanePageSize
	end := min(start+ResultsPanePageSize, totalRows)
	context.StartRow = start + 1
	context.EndRow = end
	return context
}

// ClampResultsPanePage clamps page to the valid range for totalRows.
func ClampResultsPanePage(page, totalRows int) int {
	totalPages := resultsPaneTotalPages(totalRows)
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

func resultsPaneTotalPages(totalRows int) int {
	if totalRows <= 0 {
		return 1
	}
	return (totalRows-1)/ResultsPanePageSize + 1
}

func resultsPaneColumns(columns []db.ResultColumn) []ResultsPaneColumn {
	names := make([]ResultsPaneColumn, 0, len(columns))
	for i, column := range columns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		names = append(names, ResultsPaneColumn{Header: name, PrimaryKey: column.PrimaryKey != nil})
	}
	return names
}

func resultsPaneRowsForPage(rows []db.ResultRow, page int) ([]db.ResultRow, ResultsPanePageContext) {
	context := ResultsPanePageContextFor(page, len(rows))
	if len(rows) == 0 {
		return nil, context
	}
	start := context.StartRow - 1
	end := context.EndRow
	return rows[start:end], context
}

func resultsPaneFormatValue(value db.ResultValue) string {
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
		return resultsPaneTruncateNewlines(fmt.Sprint(value.Value))
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return fmt.Sprintf("0x%x", typed)
		}
	case db.ValueKindTime:
		if typed, ok := value.Value.(time.Time); ok {
			return typed.Format("2006-01-02 15:04:05")
		}
	}
	if value.Value == nil {
		return "NULL"
	}
	return resultsPaneTruncateNewlines(fmt.Sprint(value.Value))
}

func resultsPaneTruncateNewlines(s string) string {
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		return s[:i] + "..."
	}
	return s
}

const resultsPaneViewportClipThreshold = 20

func resultsPaneVisibleRowWindow(context ResultsPanePageContext, totalRows, height int, active ResultsPaneSelection, viewportStart int) (int, int) {
	if totalRows <= 0 {
		return 0, 0
	}

	visibleRows := totalRows
	if height > 0 {
		visibleRows = max(1, height-2)
		visibleRows = min(visibleRows, totalRows)
	}

	if visibleRows >= totalRows {
		return 0, totalRows
	}
	if totalRows <= resultsPaneViewportClipThreshold {
		return 0, totalRows
	}

	start := 0
	if active.Active {
		start = viewportStart
	}
	start = max(0, min(start, totalRows-visibleRows))
	return start, start + visibleRows
}

func resultsPaneRowSelectedSet(rows map[int]struct{}, row int) bool {
	if len(rows) == 0 {
		return false
	}
	_, ok := rows[row]
	return ok
}

// RenderInlineResultLine renders a row of column values padded to their column widths.
func RenderInlineResultLine(values []string, widths []int) string {
	return renderResultsPaneInlineResultLine(values, widths)
}

// RuneWidth returns the display width of s (counting ANSI-aware character widths).
func RuneWidth(value string) int {
	return ansi.StringWidth(value)
}

func renderResultsPaneInlineResultLine(values []string, widths []int) string {
	parts := make([]string, 0, len(values))
	for i, value := range values {
		padding := widths[i] - ansi.StringWidth(value)
		parts = append(parts, value+strings.Repeat(" ", max(0, padding)))
	}
	return strings.Join(parts, " | ")
}

func renderResultsPaneInlineSeparator(widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat("-", max(3, width)))
	}
	return AppTheme.ResultSeparator.Render(strings.Join(parts, "-+-"))
}

func resultsPaneTrimWidth(value string, width int) string {
	if width <= 0 {
		return value
	}
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) <= width {
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

// ResultsPaneRowAtVisibleOffset maps a 0-based offset into the visible data-row
// window (offset 0 = first rendered data row) to an absolute Result Set row
// index, reusing resultsPaneVisibleRowWindow. ok is false if prepared is nil,
// has no rows, or offset is outside [0, end-start).
func ResultsPaneRowAtVisibleOffset(prepared *ResultsPanePreparedPage, height int, active ResultsPaneSelection, viewportStart int, offset int) (int, bool) {
	if prepared == nil || len(prepared.Rows) == 0 {
		return 0, false
	}
	start, end := resultsPaneVisibleRowWindow(prepared.Context, len(prepared.Rows), height, active, viewportStart)
	if offset < 0 || offset >= end-start {
		return 0, false
	}
	// start is a page-local index; prepared.Context.StartRow is 1-based.
	absoluteRow := prepared.Context.StartRow - 1 + start + offset
	return absoluteRow, true
}
