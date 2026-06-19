package app

import (
	"fmt"
	"strings"

	"github.com/adwinying/sqlcery/internal/db"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/tui"
)

const (
	defaultResultsPaneWidth  = 80
	defaultResultsPaneHeight = 24
	minimumResultsPaneWidth  = 20
	minimumResultsPaneHeight = 8
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

type resultsPaneModeModel struct {
	width           int
	height          int
	selectedRow     int
	selectedColumn  int
	colScrollOffset int
	selectionActive bool
	pendingAction   resultsPanePendingAction
	exportBuffer    string
	cachedPage      *tui.ResultsPanePreparedPage
}

func newResultsPaneModeModel() resultsPaneModeModel {
	return resultsPaneModeModel{
		width:  defaultResultsPaneWidth,
		height: defaultResultsPaneHeight,
	}
}

func (m *resultsPaneModeModel) SetSize(width, height int) {
	m.width = clampEditorSize(width, minimumResultsPaneWidth)
	m.height = clampEditorSize(height, minimumResultsPaneHeight)
}

func (m *resultsPaneModeModel) renderEmptyState(subtitle string) string {
	logoLines := strings.Split(sqlceryLogo, "\n")

	var centeredLogoLines []string
	leftPad := (m.width - sqlceryLogoWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	padStr := strings.Repeat(" ", leftPad)
	for _, line := range logoLines {
		centeredLogoLines = append(centeredLogoLines, padStr+tui.AppTheme.ResultsPaneEmptyLogo.Render(line))
	}

	subtitleWidth := ansi.StringWidth(subtitle)
	subLeftPad := (m.width - subtitleWidth) / 2
	if subLeftPad < 0 {
		subLeftPad = 0
	}
	styledSubtitle := strings.Repeat(" ", subLeftPad) + tui.AppTheme.ResultsPaneEmptySubtitle.Render(subtitle)

	contentLines := append(centeredLogoLines, "", styledSubtitle)
	contentHeight := len(contentLines)

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

// buildViewContext maps InteractionState and the model's mutable navigation
// state into a ResultsPaneViewContext for stateless rendering in internal/tui.
func (m *resultsPaneModeModel) buildViewContext(interaction InteractionState) tui.ResultsPaneViewContext {
	m.syncSelection(interaction)

	var result *db.ResultSet
	var statement string
	if interaction.LatestResult != nil {
		result = interaction.LatestResult.PreservedResult
		statement = interaction.LatestResult.Statement
	}

	return tui.ResultsPaneViewContext{
		Result:          result,
		Page:            interaction.ResultsPanePage,
		MarkedRows:      interaction.MarkedRows,
		Statement:       statement,
		IsSplit:         interaction.Layout == LayoutSplit,
		Width:           m.width,
		Height:          m.height,
		SelectedRow:     m.selectedRow,
		SelectedColumn:  m.selectedColumn,
		SelectionActive: m.selectionActive,
		ColScrollOffset: m.colScrollOffset,
		PendingExport:   m.pendingAction == resultsPanePendingActionExport,
		ExportBuffer:    m.exportBuffer,
	}
}

func (m *resultsPaneModeModel) View(ctx tui.ResultsPaneViewContext) string {
	if ctx.Result == nil {
		if ctx.IsSplit {
			return m.renderEmptyState("Run a query that returns rows to populate this pane")
		}
		return m.renderEmptyState("Run a query that returns rows, then press ctrl+x or ctrl+3.")
	}

	if ctx.IsSplit {
		preparedPage := m.preparePage(ctx.Result, ctx.Page, len(ctx.MarkedRows) > 0)
		body := tui.RenderPreparedResultsPanePage(preparedPage, ctx.Width, ctx.Height, tui.ResultsPaneRenderState{
			Active:          tui.ResultsPaneSelection{Row: ctx.SelectedRow, Column: ctx.SelectedColumn, Active: ctx.SelectionActive},
			SelectedRows:    tui.ResultsPaneSelectedRowSet(ctx.MarkedRows),
			ColScrollOffset: ctx.ColScrollOffset,
		})
		if body == "" {
			body = tui.AppTheme.ResultsPaneEmpty.Render("(no visible rows)")
		}
		return body
	}

	page := tui.ResultsPanePageContextFor(ctx.Page, len(ctx.Result.Rows))
	header := []string{
		tui.AppTheme.ResultsPaneTitle.Render("Results Pane"),
		tui.AppTheme.ResultsPaneMeta.Render(fmt.Sprintf("Query: %s", summarizeResultsPaneStatement(ctx.Statement, ctx.Width))),
		tui.AppTheme.ResultsPaneMeta.Render(fmt.Sprintf("Rows: %d  Columns: %d", len(ctx.Result.Rows), len(ctx.Result.Columns))),
		tui.AppTheme.ResultsPaneMeta.Render(fmt.Sprintf("Page: %d/%d  Showing rows %s", page.Number, page.TotalPages, tui.ResultsPaneFormatRowRange(page))),
	}
	if ctx.PendingExport {
		header = append(header, tui.AppTheme.WarningNotice.Render(fmt.Sprintf("Command: %s", ctx.ExportBuffer)))
	}
	if selectedCount := len(ctx.MarkedRows); selectedCount > 0 {
		header = append(header, tui.AppTheme.ResultsPaneSelection.Render(fmt.Sprintf("Selected: %d", selectedCount)))
	}

	preparedPage := m.preparePage(ctx.Result, ctx.Page, len(ctx.MarkedRows) > 0)
	body := tui.RenderPreparedResultsPanePage(preparedPage, ctx.Width, ctx.Height-len(header)-2, tui.ResultsPaneRenderState{
		Active:          tui.ResultsPaneSelection{Row: ctx.SelectedRow, Column: ctx.SelectedColumn, Active: ctx.SelectionActive},
		SelectedRows:    tui.ResultsPaneSelectedRowSet(ctx.MarkedRows),
		ColScrollOffset: ctx.ColScrollOffset,
	})
	if body == "" {
		body = tui.AppTheme.ResultsPaneEmpty.Render("(no visible rows)")
	}

	return strings.Join(append(header, "", body), "\n")
}

func (m resultsPaneModeModel) FooterHints(interaction InteractionState) string {
	parts := []string{"Results Pane"}
	if latest := interaction.LatestResult; latest != nil && latest.PreservedResult != nil {
		page := tui.ResultsPanePageContextFor(interaction.ResultsPanePage, len(latest.PreservedResult.Rows))
		parts = append(parts, fmt.Sprintf("%d rows", page.TotalRows), fmt.Sprintf("page %d/%d", page.Number, page.TotalPages))
		if selectedCount := len(interaction.MarkedRows); selectedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d selected", selectedCount))
		}
	}
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, running)
	}
	if m.pendingAction == resultsPanePendingActionExport {
		parts = append(parts, ":w [file] export", "enter save", "esc cancel")
	}
	parts = append(parts, "alt+h help", "arrows/hjkl navigate", "space toggle row", "ctrl+u scroll up", "ctrl+d scroll down", "ctrl+p prev page", "ctrl+n next page", "ctrl+x focus", "ctrl+1 results", "ctrl+2 command", "ctrl+3 command-only", "ctrl+c quit")
	return strings.Join(parts, " | ")
}

func (m resultsPaneModeModel) Footer(connectionName, dialect string, interaction InteractionState) string {
	parts := []string{"Results Pane", fmt.Sprintf("layout %s", layoutLabel(interaction.Layout))}
	if label := strings.TrimSpace(connectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}
	if label := strings.TrimSpace(dialect); label != "" {
		parts = append(parts, label)
	}
	if latest := interaction.LatestResult; latest != nil && latest.PreservedResult != nil {
		page := tui.ResultsPanePageContextFor(interaction.ResultsPanePage, len(latest.PreservedResult.Rows))
		parts = append(parts, fmt.Sprintf("%d rows", page.TotalRows), fmt.Sprintf("page %d/%d", page.Number, page.TotalPages))
		if selectedCount := len(interaction.MarkedRows); selectedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d selected", selectedCount))
		}
	}
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, running)
	}
	if m.pendingAction == resultsPanePendingActionExport {
		parts = append(parts, ":w [file] export", "enter save", "esc cancel")
	}
	parts = append(parts, "alt+h help", "arrows/hjkl navigate", "space toggle row", "yy compose insert", "cc compose update", "dd compose delete", "ctrl+u scroll up", "ctrl+d scroll down", "ctrl+p prev page", "ctrl+n next page", "ctrl+x focus", "ctrl+1 results", "ctrl+2 command", "ctrl+3 command-only", "ctrl+c quit")
	return tui.AppTheme.Footer.Render(strings.Join(parts, " | "))
}

func (m *resultsPaneModeModel) syncSelection(interaction InteractionState) {
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

	page := tui.ResultsPanePageContextFor(interaction.ResultsPanePage, len(result.Rows))
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

func (m *resultsPaneModeModel) preparePage(result *db.ResultSet, page int, showSelectionMarker bool) *tui.ResultsPanePreparedPage {
	key := tui.ResultsPanePreparedPageKey{Result: result, Page: page, ShowSelectionMarker: showSelectionMarker}
	if m.cachedPage != nil && m.cachedPage.Key == key {
		return m.cachedPage
	}
	prepared := tui.PrepareResultsPanePage(result, page, showSelectionMarker)
	m.cachedPage = prepared
	return prepared
}

func (m *resultsPaneModeModel) Navigate(msg tea.KeyPressMsg, interaction InteractionState) (int, bool) {
	deltaRow, deltaColumn, ok := resultsPaneNavigationDelta(msg)
	if !ok {
		return interaction.ResultsPanePage, false
	}

	latest := interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil || len(latest.PreservedResult.Rows) == 0 || len(latest.PreservedResult.Columns) == 0 {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.selectionActive = false
		return interaction.ResultsPanePage, true
	}

	m.syncSelection(interaction)
	result := latest.PreservedResult
	m.selectedRow = min(max(m.selectedRow+deltaRow, 0), len(result.Rows)-1)
	m.selectedColumn = min(max(m.selectedColumn+deltaColumn, 0), len(result.Columns)-1)
	m.selectionActive = true

	if deltaColumn != 0 {
		m.colScrollOffset = m.selectedColumn
	}

	return tui.ClampResultsPanePage(m.selectedRow/tui.ResultsPanePageSize, len(result.Rows)), true
}

// ToggleSelectedRow marks or unmarks the current cursor row and returns
// the cursor row, the updated marked-row slice, whether the row is now
// selected, and whether the operation was handled.
func (m *resultsPaneModeModel) ToggleSelectedRow(interaction InteractionState) (int, []int, bool, bool) {
	if interaction.LatestResult == nil || interaction.LatestResult.PreservedResult == nil {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.selectionActive = false
		return 0, nil, false, false
	}

	result := interaction.LatestResult.PreservedResult
	if len(result.Rows) == 0 || len(result.Columns) == 0 {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.selectionActive = false
		return 0, nil, false, false
	}

	m.syncSelection(interaction)
	newMarked := toggleSelectedRowIndices(interaction.MarkedRows, m.selectedRow)
	m.selectionActive = true
	return m.selectedRow, newMarked, rowIndexSelected(newMarked, m.selectedRow), true
}

func renderResultsPaneTable(result *db.ResultSet, page, width, height int, state tui.ResultsPaneRenderState) string {
	prepared := tui.PrepareResultsPanePage(result, page, len(state.SelectedRows) > 0)
	return tui.RenderPreparedResultsPanePage(prepared, width, height, state)
}

func resultsPaneNavigationDelta(msg tea.KeyPressMsg) (int, int, bool) {
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

func resultsPaneRowCount(latest *LatestResultContext) int {
	if latest == nil || latest.PreservedResult == nil {
		return 0
	}
	return len(latest.PreservedResult.Rows)
}

func summarizeResultsPaneStatement(statement string, width int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(statement)), " ")
	if trimmed == "" {
		return "(unknown query)"
	}

	maxWidth := max(20, width-8)
	if ansi.StringWidth(trimmed) <= maxWidth {
		return trimmed
	}

	if maxWidth <= 3 {
		return ansi.Truncate(trimmed, maxWidth, "")
	}
	return ansi.Truncate(trimmed, maxWidth, "...")
}

func truncateNewlines(s string) string {
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		return s[:i] + "..."
	}
	return s
}
