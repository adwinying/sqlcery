package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/db"
)

// testModelWithResult builds a ready Model with a result set loaded, window
// sized, and layout/active pane configured as specified.
func testModelWithResult(t *testing.T, layout AppLayout, activePane Pane, rows int) Model {
	t.Helper()

	model := NewModel(Session{})
	model.state.SetReady("", NotificationNone)

	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = next.(Model)

	// Build result set with the requested number of rows.
	rs := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
	}
	for i := 0; i < rows; i++ {
		rs.Rows = append(rs.Rows, db.ResultRow{
			Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(i + 1)},
				{Kind: db.ValueKindString, Value: "row"},
			},
		})
	}

	model.state.SetLatestResultContext(&LatestResultContext{
		Statement:       "select id, name from widgets;",
		PreservedResult: rs,
		OriginPane:      PaneCommand,
	})

	// Apply the desired layout and active pane directly.
	model.state.SetLayout(layout)
	model.state.SetActivePane(activePane)
	model.syncPaneSizes()

	return model
}

// Geometry (Width=100, Height=40, splitRatio=0.65):
//   contentHeight = 39
//   resultsPaneOuterH = int(39*0.65) = 25
//   commandOuterH = 39 - 25 = 14
//
//   Results pane: top border y=0, interior y=1..23, bottom border y=24
//   Command pane: top border y=25, interior y=26..37, bottom border y=38
//   Status bar: y=39
//
//   Inside results interior, data rows start at y=3 (y=1=header, y=2=sep).
//   visibleOffset = y - 3.

const (
	testWidth  = 100
	testHeight = 40
	// safe results data-row coordinates (y=3 is first data row, visibleOffset=0)
	resultsFirstDataRowY = 3
	// safe command interior coordinate
	commandInteriorY = 26
	// status bar row
	statusBarY = 39
	// results bottom border
	resultsBorderY = 24
)

// clickAt sends a tea.MouseClickMsg at the given coordinates and returns the updated model.
func clickAt(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return next.(Model)
}

// wheelAt sends a tea.MouseWheelMsg with the given button and returns the updated model.
func wheelAt(t *testing.T, m Model, x, y int, button tea.MouseButton) Model {
	t.Helper()
	next, _ := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	return next.(Model)
}

// TestMouseClickResultsRowSwitchesFocusAndMovesSelectedRow covers:
//   - story 1/2: click a results row while Command is active → ActivePane becomes
//     Results AND selectedRow == the clicked row.
func TestMouseClickResultsRowSwitchesFocusAndMovesSelectedRow(t *testing.T) {
	m := testModelWithResult(t, LayoutSplit, PaneCommand, 10)

	if got, want := m.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("precondition: ActivePane = %q, want %q", got, want)
	}

	m = clickAt(t, m, 50, resultsFirstDataRowY) // y=3 → visibleOffset=0 → row 0

	if got, want := m.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("ActivePane = %q, want %q", got, want)
	}
	if got, want := m.resultsPane.selectedRow, 0; got != want {
		t.Fatalf("selectedRow = %d, want %d", got, want)
	}
	if !m.resultsPane.selectionActive {
		t.Fatal("selectionActive = false, want true")
	}

	// Click a different row (y=5 → visibleOffset=2 → row 2).
	m = clickAt(t, m, 50, 5)
	if got, want := m.resultsPane.selectedRow, 2; got != want {
		t.Fatalf("selectedRow after y=5 = %d, want %d", got, want)
	}
}

// TestMouseClickResultsRowAlreadyFocusedMovesOnly covers story 3:
// click a results row while Results already active → selectedRow moves,
// ActivePane stays Results.
func TestMouseClickResultsRowAlreadyFocusedMovesOnly(t *testing.T) {
	m := testModelWithResult(t, LayoutSplit, PaneResults, 10)

	// Focus is already Results; click row at y=4 → visibleOffset=1 → row 1.
	m = clickAt(t, m, 50, 4)
	if got, want := m.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("ActivePane = %q, want %q", got, want)
	}
	if got, want := m.resultsPane.selectedRow, 1; got != want {
		t.Fatalf("selectedRow = %d, want %d", got, want)
	}
}

// TestMouseDoubleClickTogglesMarkThenUnmarks covers story 4:
// double-click a results row → marks it; double-click again → unmarks.
func TestMouseDoubleClickTogglesMarkThenUnmarks(t *testing.T) {
	m := testModelWithResult(t, LayoutSplit, PaneResults, 10)

	// Two rapid clicks at the same coordinates → double-click marks row 0.
	m = clickAt(t, m, 50, resultsFirstDataRowY)
	m = clickAt(t, m, 50, resultsFirstDataRowY)
	if got := m.state.Interaction.MarkedRows; len(got) != 1 || got[0] != 0 {
		t.Fatalf("MarkedRows after double-click = %v, want [0]", got)
	}

	// Two more rapid clicks → unmarks row 0.
	m = clickAt(t, m, 50, resultsFirstDataRowY)
	m = clickAt(t, m, 50, resultsFirstDataRowY)
	if got := m.state.Interaction.MarkedRows; len(got) != 0 {
		t.Fatalf("MarkedRows after second double-click = %v, want []", got)
	}
}

// TestMouseClickCommandSwitchesFocus covers story 7:
// click in the Command pane → ActivePane becomes Command, editor value unchanged.
func TestMouseClickCommandSwitchesFocus(t *testing.T) {
	m := testModelWithResult(t, LayoutSplit, PaneResults, 5)
	m.command.editor.SetValue("select 1;")
	m.syncCurrentSQL()
	before := m.command.Value()

	m = clickAt(t, m, 50, commandInteriorY)

	if got, want := m.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("ActivePane = %q, want %q", got, want)
	}
	if got := m.command.Value(); got != before {
		t.Fatalf("command.Value() = %q, want %q (editor must not change)", got, before)
	}
}

// TestMouseWheelDownResultsNoFocusChange covers story 10:
// wheel down over results while Command is active → selectedRow increases,
// ActivePane STAYS Command.
func TestMouseWheelDownResultsNoFocusChange(t *testing.T) {
	m := testModelWithResult(t, LayoutSplit, PaneCommand, 20)

	initialRow := m.resultsPane.selectedRow
	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelDown)

	if got, want := m.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("ActivePane = %q after wheel, want %q (no focus change on wheel)", got, want)
	}
	if m.resultsPane.selectedRow <= initialRow {
		t.Fatalf("selectedRow = %d, want > %d (wheel down must move cursor)", m.resultsPane.selectedRow, initialRow)
	}
}

// TestMouseWheelClampsAtBoundaries covers story 22:
// wheel up at row 0 → stays at 0; wheel down at last row → stays at last row.
func TestMouseWheelClampsAtBoundaries(t *testing.T) {
	const nRows = 5
	m := testModelWithResult(t, LayoutSplit, PaneResults, nRows)

	// Ensure cursor is at row 0.
	m.resultsPane.selectedRow = 0
	m.resultsPane.selectionActive = true

	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelUp)
	if got, want := m.resultsPane.selectedRow, 0; got != want {
		t.Fatalf("selectedRow after wheel-up at boundary = %d, want %d", got, want)
	}

	// Move to last row.
	m.resultsPane.selectedRow = nRows - 1
	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelDown)
	if got, want := m.resultsPane.selectedRow, nRows-1; got != want {
		t.Fatalf("selectedRow after wheel-down at boundary = %d, want %d", got, want)
	}
}

// TestMouseClickBorderIsNoOp covers story: click on a border row (y=24, bottom
// border of results) or the status bar (y=39) → no state change.
func TestMouseClickBorderIsNoOp(t *testing.T) {
	m := testModelWithResult(t, LayoutSplit, PaneCommand, 5)
	initialPane := m.state.Interaction.ActivePane
	initialRow := m.resultsPane.selectedRow

	// Click the results bottom border row.
	m = clickAt(t, m, 50, resultsBorderY)
	if got := m.state.Interaction.ActivePane; got != initialPane {
		t.Fatalf("ActivePane after border click = %q, want %q", got, initialPane)
	}
	if got := m.resultsPane.selectedRow; got != initialRow {
		t.Fatalf("selectedRow after border click = %d, want %d", got, initialRow)
	}

	// Click the status bar row.
	m = clickAt(t, m, 50, statusBarY)
	if got := m.state.Interaction.ActivePane; got != initialPane {
		t.Fatalf("ActivePane after status-bar click = %q, want %q", got, initialPane)
	}
}

// TestMouseClickResultsOnlyLayout verifies clicking results in ResultsOnly layout.
func TestMouseClickResultsOnlyLayout(t *testing.T) {
	m := testModelWithResult(t, LayoutResultsOnly, PaneResults, 10)

	// In ResultsOnly, interior starts at y=1. Data rows start at y=3.
	m = clickAt(t, m, 50, resultsFirstDataRowY)
	if got, want := m.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("ActivePane = %q, want %q", got, want)
	}
	if got, want := m.resultsPane.selectedRow, 0; got != want {
		t.Fatalf("selectedRow = %d, want %d", got, want)
	}
}

// TestMouseClickCommandOnlyLayoutResultsCoordIsCommand verifies that in
// CommandOnly layout, a coordinate that would be the results area in Split
// actually hits the command pane.
func TestMouseClickCommandOnlyLayoutResultsCoordIsCommand(t *testing.T) {
	m := testModelWithResult(t, LayoutCommandOnly, PaneCommand, 5)

	// In CommandOnly, y=3 is inside the command pane interior (y=0 is top border,
	// y=38 is bottom border, y=39 is status bar).
	// So clicking at y=3 should switch to PaneCommand.
	m = clickAt(t, m, 50, 3)
	if got, want := m.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("ActivePane = %q, want %q", got, want)
	}
}

// TestMouseWheelHorizontalScrollsColumns covers story 9:
// wheel left/right over results adjusts colScrollOffset.
func TestMouseWheelHorizontalScrollsColumns(t *testing.T) {
	// Need a result with multiple columns.
	model := NewModel(Session{})
	model.state.SetReady("", NotificationNone)
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m := next.(Model)

	rs := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}},
		Rows: []db.ResultRow{
			{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(1)},
				{Kind: db.ValueKindInteger, Value: int64(2)},
				{Kind: db.ValueKindInteger, Value: int64(3)},
				{Kind: db.ValueKindInteger, Value: int64(4)},
			}},
		},
	}
	m.state.SetLatestResultContext(&LatestResultContext{
		Statement:       "select a,b,c,d from t;",
		PreservedResult: rs,
		OriginPane:      PaneCommand,
	})
	m.state.SetLayout(LayoutSplit)
	m.state.SetActivePane(PaneResults)
	m.syncPaneSizes()

	// Wheel right increments colScrollOffset.
	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelRight)
	if got, want := m.resultsPane.colScrollOffset, 1; got != want {
		t.Fatalf("colScrollOffset after wheel-right = %d, want %d", got, want)
	}

	// Wheel right again.
	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelRight)
	if got, want := m.resultsPane.colScrollOffset, 2; got != want {
		t.Fatalf("colScrollOffset after 2nd wheel-right = %d, want %d", got, want)
	}

	// Wheel left decrements.
	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelLeft)
	if got, want := m.resultsPane.colScrollOffset, 1; got != want {
		t.Fatalf("colScrollOffset after wheel-left = %d, want %d", got, want)
	}

	// Clamped at 0.
	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelLeft)
	m = wheelAt(t, m, 50, resultsFirstDataRowY, tea.MouseWheelLeft)
	if got, want := m.resultsPane.colScrollOffset, 0; got != want {
		t.Fatalf("colScrollOffset after wheel-left clamp = %d, want %d", got, want)
	}
}

// TestMouseWheelCommandScrollsTranscript covers story 12:
// wheel over command pane scrolls transcript only (no focus change from Results).
func TestMouseWheelCommandScrollsTranscript(t *testing.T) {
	m := testModelWithResult(t, LayoutSplit, PaneResults, 5)

	// Wheel up/down over command pane must not switch focus.
	m = wheelAt(t, m, 50, commandInteriorY, tea.MouseWheelDown)
	if got, want := m.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("ActivePane after command-wheel = %q, want %q (no focus change)", got, want)
	}

	m = wheelAt(t, m, 50, commandInteriorY, tea.MouseWheelUp)
	if got, want := m.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("ActivePane after command-wheel-up = %q, want %q", got, want)
	}
}

// TestMouseIgnoredWhenNotReady verifies that mouse events are ignored in non-Ready states.
func TestMouseIgnoredWhenNotReady(t *testing.T) {
	model := NewModel(Session{})
	// State is Startup (not Ready).
	initialPane := model.state.Interaction.ActivePane

	next, _ := model.Update(tea.MouseClickMsg{X: 50, Y: 3, Button: tea.MouseLeft})
	m := next.(Model)
	if got := m.state.Interaction.ActivePane; got != initialPane {
		t.Fatalf("ActivePane changed in non-ready state = %q, want %q", got, initialPane)
	}
}
