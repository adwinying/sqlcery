package app

import (
	"math/rand"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/adwinying/sqlcery/internal/db"
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

var emptyStateHints = []string{
	"Press ctrl+r to fuzzy-search your query history",
	"Tab or Enter accepts an autocomplete suggestion",
	"Type /select <table> to expand a SELECT template",
	"Can't be bothered to type? /commands helps you build queries",
	"Use ctrl+q/w to switch between Results and Command Panes",
	"In the Results Pane, yy loads an INSERT into the Command Pane",
	"In the Results Pane, cc loads an UPDATE into the Command Pane",
	"In the Results Pane, dd loads a DELETE into the Command Pane",
	"In the Results Pane, space marks rows for targeted export",
	"ctrl+e exports the current results to a file or clipboard",
	"ctrl+z zooms the focused pane to full screen",
	"ctrl+x switches focus between Results and Command Panes",
	"Try /tables to list all tables in your database",
	"Press ctrl+t to show a list of keybindings that can be used",
	"Wondering what queries you ran? Check audit.log in your data directory",
}

type resultsPaneModeModel struct {
	width           int
	height          int
	selectedRow     int
	selectedColumn  int
	colScrollOffset int
	viewportStart   int
	selectionActive bool
	visualMode      bool
	visualAnchor    int
	pendingAction   resultsPanePendingAction
	cachedPage      *tui.ResultsPanePreparedPage
	hintIdx         int
	version         string
}

func newResultsPaneModeModel(version string) resultsPaneModeModel {
	return resultsPaneModeModel{
		width:   defaultResultsPaneWidth,
		height:  defaultResultsPaneHeight,
		hintIdx: rand.Intn(len(emptyStateHints)),
		version: version,
	}
}

func (m *resultsPaneModeModel) SetSize(width, height int) {
	m.width = clampEditorSize(width, minimumResultsPaneWidth)
	m.height = clampEditorSize(height, minimumResultsPaneHeight)
}

func (m *resultsPaneModeModel) renderEmptyState() string {
	hintText := emptyStateHints[m.hintIdx]

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

	versionWidth := ansi.StringWidth(m.version)
	versionLeftPad := (m.width - versionWidth) / 2
	if versionLeftPad < 0 {
		versionLeftPad = 0
	}
	centeredVersion := strings.Repeat(" ", versionLeftPad) + tui.AppTheme.ResultsPaneEmpty.Render(m.version)

	subtitleWidth := len("Hint: ") + ansi.StringWidth(hintText)
	subLeftPad := (m.width - subtitleWidth) / 2
	if subLeftPad < 0 {
		subLeftPad = 0
	}
	styledSubtitle := strings.Repeat(" ", subLeftPad) +
		tui.AppTheme.ResultsPaneEmptySubtitle.Bold(true).Render("Hint:") + " " +
		tui.AppTheme.ResultsPaneEmptySubtitle.Render(hintText)

	contentLines := append(centeredLogoLines, centeredVersion, "", styledSubtitle)
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

	var visualRange *[2]int
	if m.visualMode {
		r := [2]int{min(m.visualAnchor, m.selectedRow), max(m.visualAnchor, m.selectedRow)}
		visualRange = &r
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
		ViewportStart:   m.viewportStart,
		VisualRange:     visualRange,
	}
}

func (m *resultsPaneModeModel) View(ctx tui.ResultsPaneViewContext) string {
	if ctx.Result == nil {
		return m.renderEmptyState()
	}

	preparedPage := m.preparePage(ctx.Result, ctx.Page)
	body := tui.RenderPreparedResultsPanePage(preparedPage, ctx.Width, ctx.Height, tui.ResultsPaneRenderState{
		Active:          tui.ResultsPaneSelection{Row: ctx.SelectedRow, Column: ctx.SelectedColumn, Active: ctx.SelectionActive},
		SelectedRows:    tui.ResultsPaneSelectedRowSet(ctx.MarkedRows),
		ColScrollOffset: ctx.ColScrollOffset,
		ViewportStart:   ctx.ViewportStart,
		VisualRange:     ctx.VisualRange,
	})
	if body == "" {
		body = tui.AppTheme.ResultsPaneEmpty.Render("(no visible rows)")
	}
	return body
}

func (m resultsPaneModeModel) StatusBarHints(interaction InteractionState) []string {
	if m.visualMode {
		return []string{"arrows/hjkl navigate", "space confirm", "esc cancel", "ctrl+c quit"}
	}
	parts := []string{"arrows/hjkl navigate", "space toggle row", "V visual select", "u clear marks", "ctrl+c quit"}
	parts = append(parts, "ctrl+e export", "ctrl+u scroll up", "ctrl+d scroll down", "gg top", "G bottom", "ctrl+p prev page", "ctrl+n next page", "ctrl+x focus", "ctrl+1 results", "ctrl+2 command", "ctrl+3 command-only", "ctrl+t keybindings")
	return parts
}

func (m *resultsPaneModeModel) syncSelection(interaction InteractionState) {
	latest := interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.viewportStart = 0
		m.selectionActive = false
		m.visualMode = false
		return
	}

	result := latest.PreservedResult
	if len(result.Rows) == 0 || len(result.Columns) == 0 {
		m.selectedRow = 0
		m.selectedColumn = 0
		m.colScrollOffset = 0
		m.viewportStart = 0
		m.selectionActive = false
		m.visualMode = false
		return
	}

	page := tui.ResultsPanePageContextFor(interaction.ResultsPanePage, len(result.Rows))
	if m.selectedRow < page.StartRow-1 || m.selectedRow >= page.EndRow {
		m.selectedRow = max(0, page.StartRow-1)
		m.viewportStart = 0
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

func (m *resultsPaneModeModel) preparePage(result *db.ResultSet, page int) *tui.ResultsPanePreparedPage {
	key := tui.ResultsPanePreparedPageKey{Result: result, Page: page}
	if m.cachedPage != nil && m.cachedPage.Key == key {
		return m.cachedPage
	}
	prepared := tui.PrepareResultsPanePage(result, page)
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
	page := tui.ResultsPanePageContextFor(interaction.ResultsPanePage, len(result.Rows))
	m.selectedRow = min(max(m.selectedRow+deltaRow, page.StartRow-1), page.EndRow-1)
	m.selectedColumn = min(max(m.selectedColumn+deltaColumn, 0), len(result.Columns)-1)
	m.selectionActive = true

	if deltaColumn != 0 {
		m.colScrollOffset = m.selectedColumn
	}

	if deltaRow != 0 {
		pageRows := page.EndRow - (page.StartRow - 1)
		pageRow := m.selectedRow - (page.StartRow - 1)
		visibleRows := max(1, min(m.height-2, pageRows))
		m.viewportStart = scrolloffViewport(pageRow, m.viewportStart, visibleRows, pageRows, tui.ResultsPaneScrollOff)
	}

	return interaction.ResultsPanePage, true
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

// EnterVisualMode anchors the visual selection at the current row cursor.
func (m *resultsPaneModeModel) EnterVisualMode() {
	m.visualMode = true
	m.visualAnchor = m.selectedRow
	m.selectionActive = true
}

// CancelVisualMode exits visual mode without modifying Marked Rows.
func (m *resultsPaneModeModel) CancelVisualMode() {
	m.visualMode = false
}

// ConfirmVisualSelection marks every row in the visual range, merges with
// existing Marked Rows (always additive), exits visual mode, and returns the
// updated mark list, the range start/end (absolute indices), and whether the
// operation was handled.
func (m *resultsPaneModeModel) ConfirmVisualSelection(interaction InteractionState) ([]int, int, int, bool) {
	if !m.visualMode {
		return nil, 0, 0, false
	}
	if interaction.LatestResult == nil || interaction.LatestResult.PreservedResult == nil {
		m.visualMode = false
		return nil, 0, 0, false
	}

	start := min(m.visualAnchor, m.selectedRow)
	end := max(m.visualAnchor, m.selectedRow)
	newMarked := mergeVisualRange(interaction.MarkedRows, start, end)
	m.visualMode = false
	return newMarked, start, end, true
}

func mergeVisualRange(marked []int, start, end int) []int {
	existing := make(map[int]struct{}, len(marked))
	for _, r := range marked {
		existing[r] = struct{}{}
	}
	result := append([]int(nil), marked...)
	for i := start; i <= end; i++ {
		if _, ok := existing[i]; !ok {
			result = append(result, i)
		}
	}
	return result
}

func renderResultsPaneTable(result *db.ResultSet, page, width, height int, state tui.ResultsPaneRenderState) string {
	prepared := tui.PrepareResultsPanePage(result, page)
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

// applyScrollOff recomputes viewportStart for the current selectedRow using
// the scrolloff guard. Call after any cursor change that does not already
// invoke scrolloffViewport directly.
func (m *resultsPaneModeModel) applyScrollOff(page tui.ResultsPanePageContext) {
	pageRows := page.EndRow - (page.StartRow - 1)
	pageRow := m.selectedRow - (page.StartRow - 1)
	visibleRows := max(1, min(m.height-2, pageRows))
	m.viewportStart = scrolloffViewport(pageRow, m.viewportStart, visibleRows, pageRows, tui.ResultsPaneScrollOff)
}

// scrolloffViewport returns the smallest viewport start that keeps pageRow
// at least scrolloff rows away from the visible edge, matching vim's scrolloff
// behaviour. pageRow and vp are both page-local (0-indexed within the page).
func scrolloffViewport(pageRow, vp, visibleRows, totalRows, scrolloff int) int {
	so := min(scrolloff, visibleRows/2)
	if pageRow < vp+so {
		vp = pageRow - so
	} else if pageRow >= vp+visibleRows-so {
		vp = pageRow + 1 - visibleRows + so
	}
	return max(0, min(vp, totalRows-visibleRows))
}
