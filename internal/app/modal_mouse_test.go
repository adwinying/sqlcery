package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/tui"
)

// ---- modalListClickOffset unit tests ----

// Geometry for width=100, height=40:
//   contentHeight = 39
//   maxW = min(64, 96) = 64
//   modalH = 18
//   OverlayCenter guard: 100 >= 64+2=66 ✓; 39 >= 18+2=20 ✓
//   startX = (100-64)/2 = 18
//   startY = (39-18)/2 = 10
//
//   No filter:  listTopScreenY = 10+1 = 11,  listRows = 16  → y=[11,26), x=[18,82)
//   With filter: listTopScreenY = 10+4 = 14,  listRows = 13  → y=[14,27), x=[18,82)

const (
	testModalWidth  = 100
	testModalHeight = 40
	testModalStartX = 18 // (100-64)/2
	testModalStartY = 10 // (39-18)/2
	testModalMidX   = 50 // safely inside [18, 82)
)

func TestModalListClickOffset_NoFilter(t *testing.T) {
	tests := []struct {
		name string
		x, y int
		want int
	}{
		// List region: y=[11,27), x=[18,82)   (listTopScreenY=11, listRows=16)
		{"first list row", testModalMidX, 11, 0},
		{"second list row", testModalMidX, 12, 1},
		{"last valid list row (y=26)", testModalMidX, 26, 15}, // 26-11=15, last valid offset
		{"past end (y=27)", testModalMidX, 27, -1},            // 27 is the bottom border
		{"top border (y=10)", testModalMidX, 10, -1},
		{"left of box (x=17)", 17, 15, -1},
		{"right of box (x=82)", 82, 15, -1},
		{"inside box x=18", 18, 15, 4},
		{"inside box x=81", 81, 15, 4},
		{"outside totally (0,0)", 0, 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modalListClickOffset(testModalWidth, testModalHeight, false, tt.x, tt.y)
			if got != tt.want {
				t.Errorf("modalListClickOffset(no-filter, x=%d, y=%d) = %d, want %d", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestModalListClickOffset_WithFilter(t *testing.T) {
	tests := []struct {
		name string
		x, y int
		want int
	}{
		// List region: y=[14,27), x=[18,82)
		{"first list row", testModalMidX, 14, 0},
		{"second list row", testModalMidX, 15, 1},
		{"filter area (y=10)", testModalMidX, 10, -1},
		{"filter area (y=13)", testModalMidX, 13, -1}, // row 13 is the suggestions top border
		{"last list row (y=26)", testModalMidX, 26, 12},
		{"past end (y=27)", testModalMidX, 27, -1},
		{"x left of box", 17, 16, -1},
		{"x right of box", 82, 16, -1},
		{"inside y=14", 18, 14, 0},
		{"outside (0,0)", 0, 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modalListClickOffset(testModalWidth, testModalHeight, true, tt.x, tt.y)
			if got != tt.want {
				t.Errorf("modalListClickOffset(filter, x=%d, y=%d) = %d, want %d", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestModalListClickOffset_TooNarrowTerminal(t *testing.T) {
	// width=30: maxW=min(64,26)=26 < ModalMinWidth(30) → -1
	got := modalListClickOffset(30, 40, false, 15, 15)
	if got != -1 {
		t.Errorf("too-narrow terminal: want -1, got %d", got)
	}
}

func TestModalListClickOffset_TooShortTerminal(t *testing.T) {
	// height=20: contentHeight=19; 19 < 18+2=20 → -1
	got := modalListClickOffset(100, 20, false, 50, 10)
	if got != -1 {
		t.Errorf("too-short terminal: want -1, got %d", got)
	}
}

// ---- helper: build ready Model with a modal open ----

// testModelWithModal returns a Ready model with the given modal pushed,
// sized at width=100, height=40.
func testModelWithModal(t *testing.T, modal Modal) Model {
	t.Helper()
	m := NewModel(Session{})
	m.state.SetReady("", NotificationNone)
	next, _ := m.Update(tea.WindowSizeMsg{Width: testModalWidth, Height: testModalHeight})
	m = next.(Model)
	m.pushModal(modal)
	return m
}

// ---- historySearchModal tests ----

func TestHistoryModalSingleClick_MovesSelection(t *testing.T) {
	m := testModelWithModal(t, &historySearchModal{})

	// Populate history via InteractionState.
	for i := 0; i < 5; i++ {
		m.state.Interaction.History = append(m.state.Interaction.History, HistoryEntryContext{Statement: "SELECT " + string(rune('a'+i))})
	}

	// No-filter: list at y=[11,26), click y=12 → offset=1.
	// historySearchModal has filter, so hasFilter=true (filter text is "█").
	// With filter: list at y=[14,27), click y=15 → offset=1.
	next, _ := m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 15, Button: tea.MouseLeft})
	m = next.(Model)

	modal := m.currentModal().(*historySearchModal)
	if modal.selectedIndex != 1 {
		t.Errorf("selectedIndex = %d, want 1", modal.selectedIndex)
	}
}

func TestHistoryModalOutsideClick_IsNoop(t *testing.T) {
	m := testModelWithModal(t, &historySearchModal{})
	for i := 0; i < 3; i++ {
		m.state.Interaction.History = append(m.state.Interaction.History, HistoryEntryContext{Statement: "SELECT " + string(rune('a'+i))})
	}

	initial := m.currentModal().(*historySearchModal).selectedIndex
	// Click outside (0,0).
	next, _ := m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m = next.(Model)

	if m.currentModal() == nil {
		t.Fatal("modal was dismissed by outside click — expected no-op")
	}
	modal := m.currentModal().(*historySearchModal)
	if modal.selectedIndex != initial {
		t.Errorf("selectedIndex changed from %d to %d on outside click", initial, modal.selectedIndex)
	}
}

func TestHistoryModalWheelDown_MovesSelection(t *testing.T) {
	m := testModelWithModal(t, &historySearchModal{})
	for i := 0; i < 5; i++ {
		m.state.Interaction.History = append(m.state.Interaction.History, HistoryEntryContext{Statement: "SELECT " + string(rune('a'+i))})
	}

	before := m.currentModal().(*historySearchModal).selectedIndex
	next, _ := m.Update(tea.MouseWheelMsg{X: testModalMidX, Y: 15, Button: tea.MouseWheelDown})
	m = next.(Model)

	modal := m.currentModal().(*historySearchModal)
	if modal.selectedIndex != before+1 {
		t.Errorf("selectedIndex after wheel-down = %d, want %d", modal.selectedIndex, before+1)
	}
}

func TestHistoryModalWheelUp_MovesSelection(t *testing.T) {
	modal := &historySearchModal{selectedIndex: 2}
	m := testModelWithModal(t, modal)
	for i := 0; i < 5; i++ {
		m.state.Interaction.History = append(m.state.Interaction.History, HistoryEntryContext{Statement: "SELECT " + string(rune('a'+i))})
	}

	next, _ := m.Update(tea.MouseWheelMsg{X: testModalMidX, Y: 15, Button: tea.MouseWheelUp})
	m = next.(Model)

	got := m.currentModal().(*historySearchModal).selectedIndex
	if got != 1 {
		t.Errorf("selectedIndex after wheel-up = %d, want 1", got)
	}
}

// ---- helpModal tests ----

func TestHelpModalSingleClick_MovesSelection(t *testing.T) {
	h := &helpModal{contextPane: PaneCommand}
	m := testModelWithModal(t, h)

	// helpModal has filter (FilterText returns "█"), so hasFilter=true.
	// List at y=[14,27). Click y=14 → offset=0 → vpStart=0 → idx=0.
	next, _ := m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 14, Button: tea.MouseLeft})
	m = next.(Model)

	got := m.currentModal().(*helpModal).selectedIndex
	if got != 0 {
		t.Errorf("selectedIndex = %d, want 0", got)
	}

	// Click y=15 → offset=1 → idx=1.
	next, _ = m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 15, Button: tea.MouseLeft})
	m = next.(Model)
	got = m.currentModal().(*helpModal).selectedIndex
	if got != 1 {
		t.Errorf("selectedIndex after y=15 = %d, want 1", got)
	}
}

func TestHelpModalOutsideClick_IsNoop(t *testing.T) {
	h := &helpModal{contextPane: PaneCommand}
	m := testModelWithModal(t, h)

	initial := m.currentModal().(*helpModal).selectedIndex
	next, _ := m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m = next.(Model)

	if m.currentModal() == nil {
		t.Fatal("modal dismissed by outside click")
	}
	got := m.currentModal().(*helpModal).selectedIndex
	if got != initial {
		t.Errorf("selectedIndex changed %d→%d on outside click", initial, got)
	}
}

func TestHelpModalWheelDown_MovesSelection(t *testing.T) {
	h := &helpModal{contextPane: PaneCommand}
	m := testModelWithModal(t, h)

	before := m.currentModal().(*helpModal).selectedIndex
	next, _ := m.Update(tea.MouseWheelMsg{X: testModalMidX, Y: 15, Button: tea.MouseWheelDown})
	m = next.(Model)

	got := m.currentModal().(*helpModal).selectedIndex
	if got != before+1 {
		t.Errorf("selectedIndex after wheel-down = %d, want %d", got, before+1)
	}
}

func TestHelpModalWheelUp_AtTop_Clamps(t *testing.T) {
	h := &helpModal{contextPane: PaneCommand, selectedIndex: 0}
	m := testModelWithModal(t, h)

	// Wheel up at index 0 should stay at 0 (clamped, no wrap).
	next, _ := m.Update(tea.MouseWheelMsg{X: testModalMidX, Y: 15, Button: tea.MouseWheelUp})
	m = next.(Model)

	got := m.currentModal().(*helpModal).selectedIndex
	if got != 0 {
		t.Errorf("selectedIndex after wheel-up at top = %d, want 0", got)
	}
}

// ---- slashWizardModal tests ----

func TestSlashWizardModalSingleClick_MovesCommandSelection(t *testing.T) {
	commands := []SlashCommandWizardCommand{
		{Name: "tables", DisplayName: "/tables"},
		{Name: "views", DisplayName: "/views"},
		{Name: "indexes", DisplayName: "/indexes"},
	}
	wiz := &slashWizardModal{
		wizard: SlashCommandWizardContext{
			Step:     SlashCommandWizardStepCommand,
			Commands: commands,
		},
	}
	m := testModelWithModal(t, wiz)

	// slashWizardModal in command step has no filter (FilterText="") → hasFilter=false.
	// No-filter list at y=[11,26). Click y=12 → offset=1 → idx=1.
	next, _ := m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 12, Button: tea.MouseLeft})
	m = next.(Model)

	got := m.currentModal().(*slashWizardModal).wizard.SelectedCommand
	if got != 1 {
		t.Errorf("SelectedCommand = %d, want 1", got)
	}
}

func TestSlashWizardModalOutsideClick_IsNoop(t *testing.T) {
	commands := []SlashCommandWizardCommand{{Name: "tables", DisplayName: "/tables"}}
	wiz := &slashWizardModal{
		wizard: SlashCommandWizardContext{
			Step:     SlashCommandWizardStepCommand,
			Commands: commands,
		},
	}
	m := testModelWithModal(t, wiz)

	initial := m.currentModal().(*slashWizardModal).wizard.SelectedCommand
	next, _ := m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m = next.(Model)

	if m.currentModal() == nil {
		t.Fatal("modal dismissed by outside click")
	}
	got := m.currentModal().(*slashWizardModal).wizard.SelectedCommand
	if got != initial {
		t.Errorf("SelectedCommand changed %d→%d on outside click", initial, got)
	}
}

func TestSlashWizardModalWheelDown_MovesSelection(t *testing.T) {
	commands := []SlashCommandWizardCommand{
		{Name: "tables", DisplayName: "/tables"},
		{Name: "views", DisplayName: "/views"},
	}
	wiz := &slashWizardModal{
		wizard: SlashCommandWizardContext{
			Step:     SlashCommandWizardStepCommand,
			Commands: commands,
		},
	}
	m := testModelWithModal(t, wiz)

	before := m.currentModal().(*slashWizardModal).wizard.SelectedCommand
	next, _ := m.Update(tea.MouseWheelMsg{X: testModalMidX, Y: 15, Button: tea.MouseWheelDown})
	m = next.(Model)

	got := m.currentModal().(*slashWizardModal).wizard.SelectedCommand
	want := min(before+1, len(commands)-1)
	if got != want {
		t.Errorf("SelectedCommand after wheel-down = %d, want %d", got, want)
	}
}

// ---- exportWizardModal tests ----

func TestExportWizardModalSingleClick_MovesFormatSelection(t *testing.T) {
	e := &exportWizardModal{step: exportWizardStepFormat}
	m := testModelWithModal(t, e)

	// exportWizardModal format step has filter (FilterText="█") → hasFilter=true.
	// With filter: list at y=[14,27). Click y=15 → offset=1.
	next, _ := m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 15, Button: tea.MouseLeft})
	m = next.(Model)

	got := m.currentModal().(*exportWizardModal).selectedFormat
	if got != 1 {
		t.Errorf("selectedFormat = %d, want 1", got)
	}
	// Should not have advanced to path step.
	if m.currentModal().(*exportWizardModal).step != exportWizardStepFormat {
		t.Error("step advanced to path on single click — expected no advance")
	}
}

func TestExportWizardModalDoubleClick_AdvancesToPathStep(t *testing.T) {
	e := &exportWizardModal{step: exportWizardStepFormat}
	m := testModelWithModal(t, e)

	// Two rapid clicks at the same offset.
	next, _ := m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 14, Button: tea.MouseLeft})
	m = next.(Model)
	next, _ = m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 14, Button: tea.MouseLeft})
	m = next.(Model)

	// After double-click, modal should have advanced to path step.
	if m.currentModal() == nil {
		t.Fatal("modal was closed — expected to advance to path step")
	}
	if got := m.currentModal().(*exportWizardModal).step; got != exportWizardStepPath {
		t.Errorf("step = %v, want exportWizardStepPath", got)
	}
}

func TestExportWizardModalOutsideClick_IsNoop(t *testing.T) {
	e := &exportWizardModal{step: exportWizardStepFormat}
	m := testModelWithModal(t, e)

	initial := m.currentModal().(*exportWizardModal).selectedFormat
	next, _ := m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m = next.(Model)

	if m.currentModal() == nil {
		t.Fatal("modal dismissed by outside click")
	}
	got := m.currentModal().(*exportWizardModal).selectedFormat
	if got != initial {
		t.Errorf("selectedFormat changed %d→%d on outside click", initial, got)
	}
}

func TestExportWizardModalPathStep_ClickIsNoop(t *testing.T) {
	e := &exportWizardModal{step: exportWizardStepPath}
	m := testModelWithModal(t, e)

	// Path step: click on what would be the list area → no-op.
	next, _ := m.Update(tea.MouseClickMsg{X: testModalMidX, Y: 14, Button: tea.MouseLeft})
	m = next.(Model)

	if m.currentModal() == nil {
		t.Fatal("modal dismissed unexpectedly")
	}
	if got := m.currentModal().(*exportWizardModal).step; got != exportWizardStepPath {
		t.Errorf("step changed from path on click: got %v", got)
	}
}

func TestExportWizardModalWheelDown_MovesFormatSelection(t *testing.T) {
	e := &exportWizardModal{step: exportWizardStepFormat, selectedFormat: 0}
	m := testModelWithModal(t, e)

	next, _ := m.Update(tea.MouseWheelMsg{X: testModalMidX, Y: 15, Button: tea.MouseWheelDown})
	m = next.(Model)

	got := m.currentModal().(*exportWizardModal).selectedFormat
	if got != 1 {
		t.Errorf("selectedFormat after wheel-down = %d, want 1", got)
	}
}

// ---- verify modalListClickOffset matches expected constants ----

func TestModalListClickOffset_Constants(t *testing.T) {
	// Verify the constants derived from tui package match expectations.
	if tui.ModalFixedRows != 16 {
		t.Errorf("ModalFixedRows = %d, want 16", tui.ModalFixedRows)
	}
	if tui.ModalSplitListRows != 13 {
		t.Errorf("ModalSplitListRows = %d, want 13", tui.ModalSplitListRows)
	}
	if tui.ModalMaxWidth != 64 {
		t.Errorf("ModalMaxWidth = %d, want 64", tui.ModalMaxWidth)
	}
}

// ---- verify wheel events don't affect pane when modal is open ----

func TestModalOpenWheelRoutesToModal(t *testing.T) {
	// Open a helpModal, then send a wheel event and verify that modal
	// selection moves (not pane scroll).
	h := &helpModal{contextPane: PaneCommand}
	m := testModelWithModal(t, h)
	// Load some results so results pane could potentially be scrolled.
	m.state.SetReady("", NotificationNone)

	before := m.currentModal().(*helpModal).selectedIndex
	next, _ := m.Update(tea.MouseWheelMsg{X: testModalMidX, Y: 15, Button: tea.MouseWheelDown})
	m = next.(Model)

	// Modal must still be open.
	if m.currentModal() == nil {
		t.Fatal("modal closed by wheel event")
	}
	got := m.currentModal().(*helpModal).selectedIndex
	if got != before+1 {
		t.Errorf("selectedIndex after wheel = %d, want %d", got, before+1)
	}
}
