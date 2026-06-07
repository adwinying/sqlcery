package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultRecordViewerWidth          = 80
	defaultRecordViewerHeight         = 24
	minimumRecordViewerWidth          = 20
	minimumRecordViewerHeight         = 8
	recordViewerPageSize              = 300
	recordViewerViewportClipThreshold = 20
)

// sqlceryLogo is the "SQLcery" ASCII art rendered in ANSI Shadow style.
// Each line is 58 characters wide and the art is 6 lines tall.
const sqlceryLogo = `███████╗ ██████╗ ██╗      ██████╗███████╗██████╗ ██╗   ██╗
██╔════╝██╔═══██╗██║     ██╔════╝██╔════╝██╔══██╗╚██╗ ██╔╝
███████╗██║   ██║██║     ██║     █████╗  ██████╔╝ ╚████╔╝ 
╚════██║██║▄▄ ██║██║     ██║     ██╔══╝  ██╔══██╗  ╚██╔╝  
███████║╚██████╔╝███████╗╚██████╗███████╗██║  ██║   ██║   
╚══════╝ ╚══▀▀═╝ ╚══════╝ ╚═════╝╚══════╝╚═╝  ╚═╝   ╚═╝   `

const sqlceryLogoWidth = 58
const sqlceryLogoHeight = 6

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

type recordViewerSelection struct {
	Row    int
	Column int
	Active bool
}

type recordViewerRenderState struct {
	Active          recordViewerSelection
	SelectedRows    map[int]struct{}
	ColScrollOffset int
}

type recordViewerPreparedPageKey struct {
	Result              *db.ResultSet
	Page                int
	ShowSelectionMarker bool
}

type recordViewerPreparedPage struct {
	Key     recordViewerPreparedPageKey
	Context recordViewerPageContext
	Headers []string
	Widths  []int
	Rows    [][]string
}

type recordViewerModeModel struct {
	width            int
	height           int
	selectedRow      int
	selectedColumn   int
	colScrollOffset  int
	selectionActive  bool
	pendingAction    recordViewerPendingAction
	writeBuffer      string
	cachedPage       *recordViewerPreparedPage
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

func (m *recordViewerModeModel) renderEmptyState(subtitle string) string {
	logoLines := strings.Split(sqlceryLogo, "\n")

	// Center the logo horizontally
	var centeredLogoLines []string
	leftPad := (m.width - sqlceryLogoWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	padStr := strings.Repeat(" ", leftPad)
	for _, line := range logoLines {
		centeredLogoLines = append(centeredLogoLines, padStr+appTheme.viewerEmptyLogo.Render(line))
	}

	// Center the subtitle horizontally
	subtitleWidth := ansi.StringWidth(subtitle)
	subLeftPad := (m.width - subtitleWidth) / 2
	if subLeftPad < 0 {
		subLeftPad = 0
	}
	styledSubtitle := strings.Repeat(" ", subLeftPad) + appTheme.viewerEmptySubtitle.Render(subtitle)

	// Build content block: logo + blank line + subtitle
	contentLines := append(centeredLogoLines, "", styledSubtitle)
	contentHeight := len(contentLines)

	// Center vertically
	topPad := (m.height - contentHeight) / 2
	if topPad < 0 {
		topPad = 0
	}

	var lines []string
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, contentLines...)
	return strings.Join(lines, "\n")
}

func (m *recordViewerModeModel) View(interaction InteractionState) string {
	latest := interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		if interaction.Layout == LayoutSplit {
			return m.renderEmptyState("Run a query that returns rows to populate this pane")
		}
		return m.renderEmptyState("Run a query that returns rows, then press ctrl+x or ctrl+3.")
	}

	m.syncSelection(interaction)

	result := latest.PreservedResult

	// In split layout, show just the table with no metadata header
	if interaction.Layout == LayoutSplit {
		preparedPage := m.preparePage(result, interaction.ViewerPage, len(latest.SelectedRows) > 0)
		body := renderPreparedRecordViewerPage(preparedPage, m.width, m.height, recordViewerRenderState{
			Active:          recordViewerSelection{Row: m.selectedRow, Column: m.selectedColumn, Active: m.selectionActive},
			SelectedRows:    selectedRowSet(latest.SelectedRows),
			ColScrollOffset: m.colScrollOffset,
		})
		if body == "" {
			body = appTheme.viewerEmpty.Render("(no visible rows)")
		}
		return body
	}

	page := recordViewerPageContextFor(interaction.ViewerPage, len(result.Rows))
	header := []string{
		appTheme.viewerTitle.Render("Record viewer"),
		appTheme.viewerMeta.Render(fmt.Sprintf("Query: %s", summarizeViewerQuery(latest.Statement, m.width))),
		appTheme.viewerMeta.Render(fmt.Sprintf("Rows: %d  Columns: %d", len(result.Rows), len(result.Columns))),
		appTheme.viewerMeta.Render(fmt.Sprintf("Page: %d/%d  Showing rows %s", page.Number, page.TotalPages, formatRecordViewerRowRange(page))),
	}
	if m.pendingAction == recordViewerPendingActionWrite {
		header = append(header, appTheme.warningNotice.Render(fmt.Sprintf("Command: %s", m.writeBuffer)))
	}
	if selectedCount := len(latest.SelectedRows); selectedCount > 0 {
		header = append(header, appTheme.viewerSelection.Render(fmt.Sprintf("Selected: %d", selectedCount)))
	}

	preparedPage := m.preparePage(result, interaction.ViewerPage, len(latest.SelectedRows) > 0)
	body := renderPreparedRecordViewerPage(preparedPage, m.width, m.height-len(header)-2, recordViewerRenderState{
		Active:          recordViewerSelection{Row: m.selectedRow, Column: m.selectedColumn, Active: m.selectionActive},
		SelectedRows:    selectedRowSet(latest.SelectedRows),
		ColScrollOffset: m.colScrollOffset,
	})
	if body == "" {
		body = appTheme.viewerEmpty.Render("(no visible rows)")
	}

	return strings.Join(append(header, "", body), "\n")
}

func (m recordViewerModeModel) FooterHints(interaction InteractionState) string {
	parts := []string{"Record viewer"}
	if latest := interaction.LatestResult; latest != nil && latest.PreservedResult != nil {
		page := recordViewerPageContextFor(interaction.ViewerPage, len(latest.PreservedResult.Rows))
		parts = append(parts, fmt.Sprintf("%d rows", page.TotalRows), fmt.Sprintf("page %d/%d", page.Number, page.TotalPages))
		if selectedCount := len(latest.SelectedRows); selectedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d selected", selectedCount))
		}
	}
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, running)
	}
	if m.pendingAction == recordViewerPendingActionWrite {
		parts = append(parts, ":w [file] export", "enter save", "esc cancel")
	}
	parts = append(parts, "alt+h help", "arrows/hjkl navigate", "space toggle row", "ctrl+u scroll up", "ctrl+d scroll down", "ctrl+p prev page", "ctrl+n next page", "ctrl+x focus", "ctrl+1 results", "ctrl+2 command", "ctrl+3 command-only", "ctrl+c quit")
	return strings.Join(parts, " | ")
}

func (m recordViewerModeModel) Footer(connectionName, dialect string, interaction InteractionState) string {
	parts := []string{"Record viewer", fmt.Sprintf("layout %s", layoutLabel(interaction.Layout))}
	if label := strings.TrimSpace(connectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}
	if label := strings.TrimSpace(dialect); label != "" {
		parts = append(parts, label)
	}
	if latest := interaction.LatestResult; latest != nil && latest.PreservedResult != nil {
		page := recordViewerPageContextFor(interaction.ViewerPage, len(latest.PreservedResult.Rows))
		parts = append(parts, fmt.Sprintf("%d rows", page.TotalRows), fmt.Sprintf("page %d/%d", page.Number, page.TotalPages))
		if selectedCount := len(latest.SelectedRows); selectedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d selected", selectedCount))
		}
	}
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, running)
	}
	if m.pendingAction == recordViewerPendingActionWrite {
		parts = append(parts, ":w [file] export", "enter save", "esc cancel")
	}
	parts = append(parts, "alt+h help", "arrows/hjkl navigate", "space toggle row", "yy compose insert", "cc compose update", "dd compose delete", "ctrl+u scroll up", "ctrl+d scroll down", "ctrl+p prev page", "ctrl+n next page", "ctrl+x focus", "ctrl+1 results", "ctrl+2 command", "ctrl+3 command-only", "ctrl+c quit")
	return appTheme.footer.Render(strings.Join(parts, " | "))
}

func (m *recordViewerModeModel) syncSelection(interaction InteractionState) {
	latest := interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.selectionActive = false
		return
	}

	result := latest.PreservedResult
	if len(result.Rows) == 0 || len(result.Columns) == 0 {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.colScrollOffset = 0
		m.selectionActive = false
		return
	}

	page := recordViewerPageContextFor(interaction.ViewerPage, len(result.Rows))
	if m.selectedRow < page.StartRow-1 || m.selectedRow >= page.EndRow {
		m.selectedRow = max(0, page.StartRow-1)
	}
	if m.selectedRow >= len(result.Rows) {
		m.selectedRow = len(result.Rows) - 1
	}
	if m.selectedColumn >= len(result.Columns) {
		m.selectedColumn = len(result.Columns) - 1
	}
	if m.selectedColumn < 0 {
		m.selectedColumn = 0
	}
}

func (m *recordViewerModeModel) preparePage(result *db.ResultSet, page int, showSelectionMarker bool) *recordViewerPreparedPage {
	key := recordViewerPreparedPageKey{Result: result, Page: page, ShowSelectionMarker: showSelectionMarker}
	if m.cachedPage != nil && m.cachedPage.Key == key {
		return m.cachedPage
	}

	prepared := prepareRecordViewerPage(result, page, showSelectionMarker)
	m.cachedPage = prepared
	return prepared
}

func (m *recordViewerModeModel) Navigate(msg tea.KeyPressMsg, interaction InteractionState) (int, bool) {
	deltaRow, deltaColumn, ok := recordViewerNavigationDelta(msg)
	if !ok {
		return interaction.ViewerPage, false
	}

	latest := interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil || len(latest.PreservedResult.Rows) == 0 || len(latest.PreservedResult.Columns) == 0 {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.selectionActive = false
		return interaction.ViewerPage, true
	}

	m.syncSelection(interaction)
	result := latest.PreservedResult
	m.selectedRow = min(max(m.selectedRow+deltaRow, 0), len(result.Rows)-1)
	m.selectedColumn = min(max(m.selectedColumn+deltaColumn, 0), len(result.Columns)-1)
	m.selectionActive = true

	// Update horizontal scroll offset to keep the selected column visible.
	if deltaColumn != 0 {
		preparedPage := m.preparePage(result, interaction.ViewerPage, false)
		m.colScrollOffset = recordViewerColumnScrollOffset(m.colScrollOffset, m.selectedColumn, preparedPage.Widths, m.width)
	}

	return clampRecordViewerPage(m.selectedRow/recordViewerPageSize, len(result.Rows)), true
}

func (m *recordViewerModeModel) ToggleSelectedRow(interaction *InteractionState) (int, bool, bool) {
	if interaction == nil || interaction.LatestResult == nil || interaction.LatestResult.PreservedResult == nil {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.selectionActive = false
		return 0, false, false
	}

	result := interaction.LatestResult.PreservedResult
	if len(result.Rows) == 0 || len(result.Columns) == 0 {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.selectionActive = false
		return 0, false, false
	}

	m.syncSelection(*interaction)
	interaction.LatestResult.SelectedRows = toggleSelectedRowIndices(interaction.LatestResult.SelectedRows, m.selectedRow)
	m.selectionActive = true
	return m.selectedRow, rowIndexSelected(interaction.LatestResult.SelectedRows, m.selectedRow), true
}

func renderRecordViewerTable(result *db.ResultSet, page, width, height int, state recordViewerRenderState) string {
	prepared := prepareRecordViewerPage(result, page, len(state.SelectedRows) > 0)
	return renderPreparedRecordViewerPage(prepared, width, recordViewerPageHeightHint(height), state)
}

func renderPreparedRecordViewerPage(prepared *recordViewerPreparedPage, width, height int, state recordViewerRenderState) string {
	if prepared == nil {
		return ""
	}

	// Apply horizontal column scroll offset.
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
		renderInlineResultLine(headers, widths),
		renderInlineSeparator(widths),
	}

	if len(prepared.Rows) == 0 {
		lines = append(lines, appTheme.viewerEmpty.Render("(no rows)"))
		return trimRenderedWidth(strings.Join(lines, "\n"), width)
	}

	start, end := recordViewerVisibleRowWindow(prepared.Context, len(prepared.Rows), height, state.Active)
	for rowIndex := start; rowIndex < end; rowIndex++ {
		absoluteRowIndex := prepared.Context.StartRow - 1 + rowIndex
		values := append([]string(nil), prepared.Rows[rowIndex][colOffset:]...)
		isActiveRow := state.Active.Active && state.Active.Row == absoluteRowIndex
		for columnIndex := range values {
			absoluteColumnIndex := colOffset + columnIndex
			if absoluteColumnIndex == 0 && rowIndexSelectedSet(state.SelectedRows, absoluteRowIndex) {
				values[columnIndex] = appTheme.selectedRowMarker.Render("* ") + values[columnIndex]
			}
			if isActiveRow {
				values[columnIndex] = renderRecordViewerActiveRowCell(values[columnIndex])
			}
		}
		lines = append(lines, renderInlineResultLine(values, widths))
	}

	ctx := prepared.Context
	lines = append(lines, appTheme.panelHint.Render(fmt.Sprintf("Showing rows %s of %d", formatRecordViewerRowRange(ctx), ctx.TotalRows)))

	return trimRenderedWidth(strings.Join(lines, "\n"), width)
}

func prepareRecordViewerPage(result *db.ResultSet, page int, showSelectionMarker bool) *recordViewerPreparedPage {
	if result == nil {
		return &recordViewerPreparedPage{}
	}

	columns := viewerColumns(result.Columns)
	pageRows, context := recordViewerRowsForPage(result.Rows, page)
	prepared := &recordViewerPreparedPage{
		Key:     recordViewerPreparedPageKey{Result: result, Page: page, ShowSelectionMarker: showSelectionMarker},
		Context: context,
		Headers: make([]string, len(columns)),
		Widths:  make([]int, len(columns)),
		Rows:    make([][]string, 0, len(pageRows)),
	}

	for i, column := range columns {
		prepared.Headers[i] = column.Header
		prepared.Widths[i] = ansi.StringWidth(column.Header)
	}
	if showSelectionMarker && len(prepared.Widths) > 0 {
		prepared.Widths[0] += 2
	}

	for _, row := range pageRows {
		values := make([]string, len(columns))
		for i := range columns {
			formatted := ""
			if i < len(row.Values) {
				formatted = formatRecordViewerValue(row.Values[i])
			}
			values[i] = formatted

			widthValue := formatted
			if showSelectionMarker && i == 0 {
				widthValue = "  " + widthValue
			}
			if width := runeWidth(widthValue); width > prepared.Widths[i] {
				prepared.Widths[i] = width
			}
		}
		prepared.Rows = append(prepared.Rows, values)
	}

	return prepared
}

func recordViewerVisibleRowWindow(context recordViewerPageContext, totalRows, height int, active recordViewerSelection) (int, int) {
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
	if totalRows <= recordViewerViewportClipThreshold {
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

func formatRecordViewerViewportRange(start, end int) string {
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func recordViewerPageHeightHint(height int) int {
	return height
}

func recordViewerNavigationDelta(msg tea.KeyPressMsg) (int, int, bool) {
	switch msg.String() {
	case "up", "k":
		return -1, 0, true
	case "down", "j":
		return 1, 0, true
	case "left", "h":
		return 0, -1, true
	case "right", "l":
		return 0, 1, true
	default:
		return 0, 0, false
	}
}

func renderRecordViewerActiveRowCell(value string) string {
	// Use raw ANSI bold + foreground color 221 (accentWarm dark) to highlight the entire row via text color.
	return "\x1b[1;38;5;221m" + value + "\x1b[0m"
}

func selectedRowSet(rows []int) map[int]struct{} {
	if len(rows) == 0 {
		return nil
	}

	selected := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		selected[row] = struct{}{}
	}
	return selected
}

func toggleSelectedRowIndices(rows []int, row int) []int {
	for i, selected := range rows {
		if selected != row {
			continue
		}
		updated := append([]int(nil), rows[:i]...)
		return append(updated, rows[i+1:]...)
	}
	return append(append([]int(nil), rows...), row)
}

func rowIndexSelected(rows []int, row int) bool {
	for _, selected := range rows {
		if selected == row {
			return true
		}
	}
	return false
}

func rowIndexSelectedSet(rows map[int]struct{}, row int) bool {
	if len(rows) == 0 {
		return false
	}
	_, ok := rows[row]
	return ok
}

func viewerColumns(columns []db.ResultColumn) []recordViewerColumn {
	names := make([]recordViewerColumn, 0, len(columns))
	for i, column := range columns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		names = append(names, recordViewerColumn{Header: name, PrimaryKey: column.PrimaryKey != nil})
	}
	return names
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
		return truncateNewlines(fmt.Sprint(value.Value))
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

	return truncateNewlines(fmt.Sprint(value.Value))
}

func truncateNewlines(s string) string {
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		return s[:i] + "..."
	}
	return s
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

	if maxWidth <= 3 {
		return ansi.Truncate(trimmed, maxWidth, "")
	}
	return ansi.Truncate(trimmed, maxWidth, "...")
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

// recordViewerColumnScrollOffset computes a new column scroll offset that ensures
// the selected column is visible within the given display width.
// It returns the smallest offset such that the selected column fits within the viewport.
func recordViewerColumnScrollOffset(current, selectedColumn int, widths []int, viewWidth int) int {
	if len(widths) == 0 || viewWidth <= 0 {
		return 0
	}

	// Clamp current offset.
	if current < 0 {
		current = 0
	}
	if current >= len(widths) {
		current = len(widths) - 1
	}

	// If selected column is to the left of the offset, scroll left.
	if selectedColumn < current {
		return selectedColumn
	}

	// Find the rightmost column offset such that selected column is still visible.
	// Walk from current offset forward and measure how many columns fit.
	offset := current
	for {
		// Measure total width of columns from offset to selectedColumn (inclusive).
		totalWidth := 0
		for i := offset; i <= selectedColumn && i < len(widths); i++ {
			if i > offset {
				totalWidth += 3 // " | " separator
			}
			totalWidth += widths[i]
		}
		if totalWidth <= viewWidth {
			break
		}
		// Selected column doesn't fit; advance offset by one.
		offset++
		if offset >= selectedColumn {
			offset = selectedColumn
			break
		}
	}

	return offset
}
