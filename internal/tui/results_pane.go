package tui

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/charmbracelet/x/ansi"
)

const ResultsPanePageSize = 300

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

	start, end := resultsPaneVisibleRowWindow(prepared.Context, len(prepared.Rows), height, state.Active)
	for rowIndex := start; rowIndex < end; rowIndex++ {
		absoluteRowIndex := prepared.Context.StartRow - 1 + rowIndex
		values := append([]string(nil), prepared.Rows[rowIndex][colOffset:]...)
		isActiveRow := state.Active.Active && state.Active.Row == absoluteRowIndex
		isMarked := resultsPaneRowSelectedSet(state.SelectedRows, absoluteRowIndex)
		line := renderResultsPaneInlineResultLine(values, widths)
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

	ctx := prepared.Context
	lines = append(lines, AppTheme.PanelHint.Render(fmt.Sprintf("Showing rows %s of %d", ResultsPaneFormatRowRange(ctx), ctx.TotalRows)))

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

func resultsPaneVisibleRowWindow(context ResultsPanePageContext, totalRows, height int, active ResultsPaneSelection) (int, int) {
	if totalRows <= 0 {
		return 0, 0
	}

	visibleRows := totalRows
	if height > 0 {
		visibleRows = max(1, height-2)
		if totalRows > visibleRows && height > 3 {
			visibleRows = max(1, height-3)
		}
		visibleRows = min(visibleRows, totalRows)
	}

	if visibleRows >= totalRows {
		return 0, totalRows
	}
	if totalRows <= resultsPaneViewportClipThreshold {
		return 0, totalRows
	}

	start := 0
	if active.Active && context.TotalRows > 0 && active.Row >= context.StartRow-1 && active.Row < context.EndRow {
		pageRow := active.Row - (context.StartRow - 1)
		start = pageRow - visibleRows/2
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

// RenderInlineSeparator renders a separator line for the given column widths.
func RenderInlineSeparator(widths []int) string {
	return renderResultsPaneInlineSeparator(widths)
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

// resultsPaneExtractTimeValue unwraps a driver-specific timestamp value.
// Kept here for value formatting parity with internal/app's compose functions.
func resultsPaneExtractTimeValue(value any) (time.Time, int) {
	const (
		unknown = 0
		valid   = 1
		null    = 2
	)
	switch v := value.(type) {
	case nil:
		return time.Time{}, unknown
	case time.Time:
		return v, valid
	case *time.Time:
		if v == nil {
			return time.Time{}, null
		}
		return *v, valid
	case sql.NullTime:
		if !v.Valid {
			return time.Time{}, null
		}
		return v.Time, valid
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return time.Time{}, null
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return time.Time{}, unknown
	}
	timeField := rv.FieldByName("Time")
	if !timeField.IsValid() {
		return time.Time{}, unknown
	}
	if _, ok := timeField.Interface().(time.Time); !ok {
		return time.Time{}, unknown
	}
	if validField := rv.FieldByName("Valid"); validField.IsValid() && validField.Kind() == reflect.Bool && !validField.Bool() {
		return time.Time{}, null
	}
	return timeField.Interface().(time.Time), valid
}
