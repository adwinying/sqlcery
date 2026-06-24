package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/tui"
)

// modalListClickOffset computes the 0-based list-row offset for a mouse click
// (x, y in absolute screen coordinates) against the modal's on-screen geometry.
// Returns -1 when the click does not land on a selectable list row.
//
// Parameters mirror EXACTLY the render math in readyStateView and OverlayCenter:
//   - width/height: full terminal dimensions (m.width, m.height)
//   - hasFilter: whether the modal's FilterText() is non-empty (filter box present)
//   - x, y: absolute 0-based screen coordinates of the click
//
// The modal is always 18 rows tall:
//   - no filter: 1 border + 16 list rows (ModalFixedRows) + 1 border = 18 rows
//   - with filter: 3 filter-box rows + 1 border + 13 list rows (ModalSplitListRows) + 1 border = 18 rows
//
// The content area passed to OverlayCenter is height-1 rows (status bar excluded).
func modalListClickOffset(width, height int, hasFilter bool, x, y int) int {
	contentHeight := height - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	maxW := min(tui.ModalMaxWidth, width-4)
	if maxW < tui.ModalMinWidth {
		return -1
	}

	// Total modal height is always 18 rows.
	const modalH = 18

	// Mirror OverlayCenter's guard: modal must fit with >=1 margin on each side.
	if width < maxW+2 || contentHeight < modalH+2 {
		return -1
	}

	startX := (width - maxW) / 2
	startY := (contentHeight - modalH) / 2

	// Determine list region within the modal box.
	var listTopScreenY, listRows int
	if hasFilter {
		// filter box: rows 0-2 (3 rows), suggestions top border: row 3,
		// list content: rows 4-16 (13 rows = ModalSplitListRows), bottom border: row 17.
		listTopScreenY = startY + 4
		listRows = tui.ModalSplitListRows
	} else {
		// top border: row 0, list content: rows 1-16 (16 rows = ModalFixedRows),
		// bottom border: row 17.
		listTopScreenY = startY + 1
		listRows = tui.ModalFixedRows
	}

	if x < startX || x >= startX+maxW {
		return -1
	}
	if y < listTopScreenY || y >= listTopScreenY+listRows {
		return -1
	}
	return y - listTopScreenY
}

// mouseWheelRowStep is the number of rows to move per wheel tick in the
// results pane.
const mouseWheelRowStep = 3

// handleMouseClick routes a mouse click event to the appropriate pane handler.
// Guards: non-Ready state and open modals are short-circuited; border/status
// clicks are no-ops.
func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.state.App.Current != StateReady {
		return m, nil
	}
	if modal := m.currentModal(); modal != nil {
		hasFilter := modal.FilterText() != ""
		offset := modalListClickOffset(m.width, m.height, hasFilter, msg.X, msg.Y)
		now := time.Now()
		doubleClick := offset >= 0 && isDoubleClick(m.lastClickRow, m.lastClickTime, offset, now)
		ctx := ModalContext{
			Interaction:      m.state.Interaction.snapshot(),
			Session:          m.session,
			Dialect:          m.adapterDialect(),
			MouseListOffset:  offset,
			MouseDoubleClick: doubleClick,
		}
		result := modal.HandleMouse(msg, ctx)
		cmd := m.applyModalResult(result)
		m.lastClickRow = offset
		m.lastClickTime = now
		return m, cmd
	}

	region := hitTestPane(m.state.Interaction.Layout, m.width, m.height, m.splitRatio, msg.X, msg.Y)
	switch region {
	case mouseRegionNone:
		return m, nil
	case mouseRegionResults:
		return m.handleMouseClickResults(msg)
	case mouseRegionCommand:
		return m.handleMouseClickCommand()
	}
	return m, nil
}

// handleMouseClickResults handles a left click inside the results pane.
func (m Model) handleMouseClickResults(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}

	// Row mapping: results top border is screen row 0, interior starts at row 1.
	// Body layout: line 0 = column header, line 1 = separator, line 2+ = data rows.
	// So visibleOffset = (y - 1) - 2 = y - 3.
	visibleOffset := msg.Y - 3

	// Step 1: switch focus to Results (handles layout changes, blurs editor).
	m.handleFocusPane(PaneResults)
	m.syncPaneSizes()

	// Step 2: resolve the clicked row and move the cursor (skip on header/separator click).
	clickedRow := -1
	if visibleOffset >= 0 && m.state.Interaction.LatestResult != nil && m.state.Interaction.LatestResult.PreservedResult != nil {
		result := m.state.Interaction.LatestResult.PreservedResult
		page := m.state.Interaction.ResultsPanePage
		prepared := m.resultsPane.preparePage(result, page)
		active := tui.ResultsPaneSelection{
			Row:    m.resultsPane.selectedRow,
			Column: m.resultsPane.selectedColumn,
			Active: m.resultsPane.selectionActive,
		}
		if row, ok := tui.ResultsPaneRowAtVisibleOffset(prepared, m.resultsPane.height, active, m.resultsPane.viewportStart, visibleOffset); ok {
			clickedRow = row
			m.resultsPane.selectedRow = row
			m.resultsPane.selectionActive = true
			m.resultsPane.syncSelection(m.state.Interaction)
		}
	}

	// Step 3: check for double-click and toggle mark if so.
	now := time.Now()
	if clickedRow >= 0 && isDoubleClick(m.lastClickRow, m.lastClickTime, clickedRow, now) {
		row, newMarked, selected, handled := m.resultsPane.ToggleSelectedRow(m.state.Interaction)
		if handled {
			m.state.SetMarkedRows(newMarked)
			prevCreatedAt := m.state.Notification.CreatedAt
			if selected {
				m.state.SetPendingIntent(IntentNone, "mouse-select", fmt.Sprintf("Selected row %d (%d total).", row+1, len(m.state.Interaction.MarkedRows)), NotificationSuccess)
			} else {
				m.state.SetPendingIntent(IntentNone, "mouse-select", fmt.Sprintf("Unselected row %d (%d total).", row+1, len(m.state.Interaction.MarkedRows)), NotificationSuccess)
			}
			// Reset lastClickTime so the next click does not accidentally
			// re-trigger a double-click for the same gesture.
			m.lastClickRow = clickedRow
			m.lastClickTime = time.Time{}
			return m, m.newNotificationClearCmdIfChanged(prevCreatedAt)
		}
	}

	if clickedRow >= 0 {
		m.lastClickRow = clickedRow
		m.lastClickTime = now
	}
	return m, nil
}

// handleMouseClickCommand handles a left click inside the command pane: switches
// focus to the command pane only. Does not move the text cursor.
func (m Model) handleMouseClickCommand() (tea.Model, tea.Cmd) {
	m.handleFocusPane(PaneCommand)
	m.syncPaneSizes()
	return m, nil
}

// handleMouseWheel routes a mouse wheel event to the appropriate pane handler.
// Guards: non-Ready state and open modals are short-circuited.
// Wheel events do NOT change the active pane.
func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.state.App.Current != StateReady {
		return m, nil
	}
	if modal := m.currentModal(); modal != nil {
		result := modal.HandleMouseWheel(ModalContext{
			Interaction: m.state.Interaction.snapshot(),
			Session:     m.session,
			Dialect:     m.adapterDialect(),
		}, msg)
		return m, m.applyModalResult(result)
	}

	region := hitTestPane(m.state.Interaction.Layout, m.width, m.height, m.splitRatio, msg.X, msg.Y)
	switch region {
	case mouseRegionNone:
		return m, nil
	case mouseRegionResults:
		return m.handleMouseWheelResults(msg)
	case mouseRegionCommand:
		return m.handleMouseWheelCommand(msg)
	}
	return m, nil
}

// handleMouseWheelResults handles wheel events over the results pane.
// No focus change; just navigates rows or columns.
func (m Model) handleMouseWheelResults(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	latest := m.state.Interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		return m, nil
	}
	result := latest.PreservedResult
	if len(result.Rows) == 0 {
		return m, nil
	}

	page := tui.ResultsPanePageContextFor(m.state.Interaction.ResultsPanePage, len(result.Rows))
	switch msg.Button {
	case tea.MouseWheelUp:
		m.resultsPane.selectedRow = max(page.StartRow-1, m.resultsPane.selectedRow-mouseWheelRowStep)
		m.resultsPane.selectionActive = true
		m.resultsPane.syncSelection(m.state.Interaction)
		m.resultsPane.applyScrollOff(page)
	case tea.MouseWheelDown:
		m.resultsPane.selectedRow = min(page.EndRow-1, m.resultsPane.selectedRow+mouseWheelRowStep)
		m.resultsPane.selectionActive = true
		m.resultsPane.syncSelection(m.state.Interaction)
		m.resultsPane.applyScrollOff(page)
	case tea.MouseWheelLeft:
		if len(result.Columns) > 0 {
			m.resultsPane.colScrollOffset = max(0, m.resultsPane.colScrollOffset-1)
		}
	case tea.MouseWheelRight:
		if len(result.Columns) > 0 {
			m.resultsPane.colScrollOffset = min(len(result.Columns)-1, m.resultsPane.colScrollOffset+1)
		}
	}
	return m, nil
}

// handleMouseWheelCommand handles wheel events over the command pane.
// Scrolls the REPL transcript only; no focus change. Horizontal wheel is a no-op.
func (m Model) handleMouseWheelCommand(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	const step = 3
	switch msg.Button {
	case tea.MouseWheelUp:
		m.command.ScrollTranscriptUp(step, m.state.Interaction)
	case tea.MouseWheelDown:
		m.command.ScrollTranscriptDown(step)
	}
	return m, nil
}
