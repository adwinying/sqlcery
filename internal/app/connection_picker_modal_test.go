package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
	"github.com/adwinying/sqlcery/internal/tui"
)

// ---- Modal seam tests: HandleKey → ModalResult ----

func TestConnectionPickerModalOpenViaSlashConnect(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	adapter := openTestAdapter(t)
	defer adapter.Close()

	model := newReadyModel(t, adapter, "alpha", connections)

	// Type /connect and press Enter to trigger openConnectionPickerModal.
	model = typeCommandAndSubmit(t, model, "/connect")

	// The modal should now be open.
	if modal := model.currentModal(); modal == nil {
		t.Fatal("currentModal() = nil, want connectionPickerModal after /connect")
	}
	if got, want := model.currentModal().Name(), ModalConnectionPicker; got != want {
		t.Fatalf("currentModal().Name() = %q, want %q", got, want)
	}
}

func TestConnectionPickerModalActiveConnectionMarked(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	adapter := openTestAdapter(t)
	defer adapter.Close()

	model := newReadyModel(t, adapter, "alpha", connections)
	model = typeCommandAndSubmit(t, model, "/connect")

	modal, ok := model.currentModal().(*connectionPickerModal)
	if !ok {
		t.Fatalf("currentModal() = %T, want *connectionPickerModal", model.currentModal())
	}

	if modal.activeConnection != "alpha" {
		t.Fatalf("modal.activeConnection = %q, want %q", modal.activeConnection, "alpha")
	}
}

func TestConnectionPickerModalFilterNarrowsCandidates(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	adapter := openTestAdapter(t)
	defer adapter.Close()

	model := newReadyModel(t, adapter, "alpha", connections)
	model = typeCommandAndSubmit(t, model, "/connect")

	// Type "beta" to filter.
	for _, ch := range "beta" {
		next, _ := model.Update(tea.KeyPressMsg{Text: string(ch)})
		model = next.(Model)
	}

	modal, ok := model.currentModal().(*connectionPickerModal)
	if !ok {
		t.Fatalf("currentModal() = %T, want *connectionPickerModal", model.currentModal())
	}

	filtered := pickerFilteredCandidates(modal.candidates, modal.filter)
	if len(filtered) != 1 || filtered[0] != "beta" {
		t.Fatalf("filtered candidates = %v, want [beta]", filtered)
	}
}

func TestConnectionPickerModalSelectActiveConnectionIsNoop(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
		},
	}
	adapter := openTestAdapter(t)
	defer adapter.Close()

	model := newReadyModel(t, adapter, "alpha", connections)
	model = typeCommandAndSubmit(t, model, "/connect")

	// Press Enter to select "alpha" (the only candidate, which is the active one).
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)

	// Modal should be closed (no-op dismiss).
	if modal := model.currentModal(); modal != nil {
		t.Fatalf("currentModal() = %T, want nil (no-op close)", modal)
	}

	// No connect command should have been emitted.
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(midRunConnectMsg); ok {
			t.Fatal("selecting active connection should be NO-OP, got midRunConnectMsg")
		}
	}
}

func TestConnectionPickerModalSelectDifferentConnectionEmitsConnect(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	adapter := openTestAdapter(t)
	defer adapter.Close()

	model := newReadyModel(t, adapter, "alpha", connections)
	model = typeCommandAndSubmit(t, model, "/connect")

	// Navigate to beta (the modal has alpha active and candidates sorted alpha, beta).
	// alpha is candidate[0], beta is candidate[1] — move down once.
	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = next.(Model)

	// Press Enter to select beta.
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("Update(Enter on beta) cmd = nil, want midRunConnectMsg command")
	}

	msgs := collectCommandMessagesForTest(t, cmd)
	var found *midRunConnectMsg
	for _, m := range msgs {
		if mm, ok := m.(midRunConnectMsg); ok {
			found = &mm
			break
		}
	}
	if found == nil {
		t.Fatalf("no midRunConnectMsg found in cmd messages %v", msgs)
	}
	if found.name != "beta" {
		t.Fatalf("midRunConnectMsg.name = %q, want %q", found.name, "beta")
	}
}

func TestConnectionPickerModalEscCancelsWithoutConnecting(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	adapter := openTestAdapter(t)
	defer adapter.Close()

	model := newReadyModel(t, adapter, "alpha", connections)
	model = typeCommandAndSubmit(t, model, "/connect")

	// Esc should close the modal without initiating any connect.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)

	if modal := model.currentModal(); modal != nil {
		t.Fatalf("currentModal() = %T after Esc, want nil", modal)
	}

	// No connect command should be emitted.
	if cmd != nil {
		for _, m := range collectCommandMessagesForTest(t, cmd) {
			if _, ok := m.(midRunConnectMsg); ok {
				t.Fatal("Esc should not emit midRunConnectMsg")
			}
		}
	}

	// Session should be unchanged.
	if model.session.ConnectionName != "alpha" {
		t.Fatalf("session.ConnectionName = %q after Esc, want %q", model.session.ConnectionName, "alpha")
	}
}

// ---- Model-level transaction tests ----

func TestMidRunSwapSuccessNewSessionOldAdapterClosed(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	betaAdapter := openTestAdapter(t)
	defer func() {
		if err := betaAdapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	fs := &fakeFrecencyStore{}

	// Track which adapters are closed and how many times via the injectable seam.
	var closedAdapters []*db.SQLAdapter
	trackingClose := func(a *db.SQLAdapter) error {
		closedAdapters = append(closedAdapters, a)
		return a.Close()
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.frecencyStore = fs
	model.closeAdapter = trackingClose
	model.newHistory = func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil }

	// Inject an open function that returns betaAdapter.
	model.open = func(_ context.Context, conn config.Connection) (*db.SQLAdapter, error) {
		return betaAdapter, nil
	}

	// Feed midRunConnectMsg for "beta".
	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Update(midRunConnectMsg) cmd = nil")
	}

	// Execute the async open command.
	successMsg := cmd()
	smsg, ok := successMsg.(midRunConnectSuccessMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want midRunConnectSuccessMsg", successMsg)
	}

	// Before processing the success message: old adapter must NOT be closed yet.
	if len(closedAdapters) != 0 {
		t.Fatalf("old adapter closed before success processed — want 0 closes before Update, got %d", len(closedAdapters))
	}

	// Process the success message. This returns a tea.Batch that includes the close cmd.
	next, batchCmd := model.Update(smsg)
	model = next.(Model)

	// New session should be on beta immediately.
	if model.session.ConnectionName != "beta" {
		t.Fatalf("session.ConnectionName = %q, want %q", model.session.ConnectionName, "beta")
	}
	if model.session.Adapter != betaAdapter {
		t.Fatalf("session.Adapter is not betaAdapter after swap")
	}

	// Drain the batch to trigger the close goroutine.
	if batchCmd != nil {
		msgs := collectCommandMessagesForTest(t, batchCmd)
		for _, msg := range msgs {
			if msg != nil {
				next2, _ := model.Update(msg)
				model = next2.(Model)
			}
		}
	}

	// Old adapter (alpha) must be closed exactly once.
	if len(closedAdapters) != 1 {
		t.Fatalf("old adapter close count = %d, want exactly 1", len(closedAdapters))
	}
	if closedAdapters[0] != alphaAdapter {
		t.Fatal("closed adapter is not alphaAdapter")
	}

	// New adapter (beta) must NOT be closed.
	for _, a := range closedAdapters {
		if a == betaAdapter {
			t.Fatal("betaAdapter (new adapter) was closed — should not be")
		}
	}

	// Frecency recorded exactly once for beta.
	if len(fs.opens) != 1 || fs.opens[0] != "beta" {
		t.Fatalf("frecency opens = %v, want [beta]", fs.opens)
	}

	// State should be Ready.
	if model.state.App.Current != StateReady {
		t.Fatalf("state = %q, want %q", model.state.App.Current, StateReady)
	}

	// Modal must be dismissed after successful swap.
	if modal := model.currentModal(); modal != nil && modal.Name() == ModalConnectionPicker {
		t.Fatal("connectionPickerModal must be dismissed after successful swap")
	}

	// Per-session UI reset.
	if model.state.Interaction.LatestResult != nil {
		t.Fatal("LatestResult should be nil after session swap")
	}
}

func TestMidRunSwapFailureOldSessionUntouched(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	defer func() {
		if err := alphaAdapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	// Track close calls — must be zero on failure.
	var closedAdapters []*db.SQLAdapter
	trackingClose := func(a *db.SQLAdapter) error {
		closedAdapters = append(closedAdapters, a)
		return a.Close()
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.closeAdapter = trackingClose
	connectErr := errors.New("network unreachable")
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return nil, connectErr
	}

	// Trigger mid-run connect for "beta".
	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)

	// Execute the open (returns failure).
	failMsg := cmd()
	if _, ok := failMsg.(midRunConnectFailedMsg); !ok {
		t.Fatalf("cmd() = %T, want midRunConnectFailedMsg", failMsg)
	}

	// Process the failure.
	next, _ = model.Update(failMsg)
	model = next.(Model)

	// Session is still on alpha.
	if model.session.ConnectionName != "alpha" {
		t.Fatalf("session.ConnectionName = %q, want still %q", model.session.ConnectionName, "alpha")
	}
	if model.session.Adapter != alphaAdapter {
		t.Fatalf("session.Adapter changed after failure — old adapter was replaced")
	}

	// Old adapter must NOT be closed on failure.
	if len(closedAdapters) != 0 {
		t.Fatalf("old adapter was closed %d time(s) on failure — must be 0", len(closedAdapters))
	}

	// State should still be Ready (error surfaced as notification).
	if model.state.App.Current != StateReady {
		t.Fatalf("state = %q, want %q", model.state.App.Current, StateReady)
	}
	// Error surfaced in notification.
	if model.state.Notification.Level != NotificationError {
		t.Fatalf("notification level = %d, want NotificationError", model.state.Notification.Level)
	}
	if model.cancelConnect != nil {
		t.Fatal("cancelConnect should be nil after failure")
	}
}

func TestMidRunSwapTransactional_OldAdapterInSuccessMsg(t *testing.T) {
	// This test specifically proves the transactional guarantee:
	// the old adapter reference is bundled in the success message and is
	// NOT closed on the failure path.
	alphaAdapter := openTestAdapter(t)
	betaAdapter := openTestAdapter(t)
	defer func() {
		if err := betaAdapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return betaAdapter, nil
	}

	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	_ = next
	if cmd == nil {
		t.Fatal("cmd = nil")
	}

	msg := cmd()
	smsg, ok := msg.(midRunConnectSuccessMsg)
	if !ok {
		t.Fatalf("msg = %T, want midRunConnectSuccessMsg", msg)
	}

	// Old adapter is in the success message, ready to be closed only on success.
	if smsg.oldAdapter != alphaAdapter {
		t.Fatal("oldAdapter not captured in success message")
	}

	// alphaAdapter is closed by the handler after success — do it manually here
	// since we don't run the full Update cycle.
	alphaAdapter.Close()
}

func TestMidRunSwapFrecencyRecordedExactlyOnce(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	betaAdapter := openTestAdapter(t)
	defer alphaAdapter.Close()
	defer betaAdapter.Close()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	fs := &fakeFrecencyStore{}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.frecencyStore = fs
	model.newHistory = func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil }
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return betaAdapter, nil
	}

	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)
	successMsg := cmd()

	next, _ = model.Update(successMsg)
	model = next.(Model)

	if len(fs.opens) != 1 {
		t.Fatalf("frecency RecordOpen called %d times, want exactly 1", len(fs.opens))
	}
	if fs.opens[0] != "beta" {
		t.Fatalf("frecency opens[0] = %q, want %q", fs.opens[0], "beta")
	}
}

// ---- Mid-run switch reset (per-session UI) ----

// TestMidRunSwitchResetsCommandPane asserts that a successful mid-run
// connection switch leaves the Command Pane in its fresh-boot state: empty
// editor, no autocomplete navigation/suppression, no history navigation, no
// cached suggestions, no transcript entries, and the inner geometry preserved
// from the terminal size established before the switch.
func TestMidRunSwitchResetsCommandPane(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	betaAdapter := openTestAdapter(t)
	defer func() {
		if err := betaAdapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.newHistory = func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil }
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return betaAdapter, nil
	}

	// Establish real pane geometry before the switch.
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	wantInnerWidth := model.command.innerWidth
	wantInnerHeight := model.command.innerHeight

	// Seed stale Command Pane state from connection alpha.
	model.command.SetEditorValue("select stale; -- from alpha")
	model.command.widget.AppendEntry("> ", "select stale;", "1 row.")
	model.command.acNav = &acNavState{origValue: "x", origCursor: 1, frozenList: nil}
	model.command.acSuppressed = &acSuppressedAt{value: "x", cursor: 1}
	model.command.historyNavIndex = 0
	model.command.historyNavDraft = "draft"
	model.command.cachedSuggestions = []tui.AutocompleteSuggestion{{Label: "stale"}}

	// Perform the mid-run swap to beta.
	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)
	successMsg := cmd()
	if _, ok := successMsg.(midRunConnectSuccessMsg); !ok {
		t.Fatalf("cmd() = %T, want midRunConnectSuccessMsg", successMsg)
	}
	next, _ = model.Update(successMsg)
	model = next.(Model)

	// Assert Command Pane is indistinguishable from a fresh newCommandModeModel()
	// (geometry re-applied from the parent model after the swap).
	fresh := newCommandModeModel()
	fresh.SetSize(wantInnerWidth, wantInnerHeight)

	if got, want := model.command.Value(), fresh.Value(); got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
	if got, want := model.command.historyNavIndex, fresh.historyNavIndex; got != want {
		t.Fatalf("command.historyNavIndex = %v, want %v", got, want)
	}
	if got, want := model.command.historyNavDraft, fresh.historyNavDraft; got != want {
		t.Fatalf("command.historyNavDraft = %q, want %q", got, want)
	}
	if got, want := model.command.acNav, fresh.acNav; got != want {
		t.Fatalf("command.acNav = %v, want %v", got, want)
	}
	if got, want := model.command.acSuppressed, fresh.acSuppressed; got != want {
		t.Fatalf("command.acSuppressed = %v, want %v", got, want)
	}
	if got, want := model.command.cachedSuggestions, fresh.cachedSuggestions; got != nil || want != nil {
		t.Fatalf("command.cachedSuggestions = %v, want nil", got)
	}
	if got, want := model.command.widget.TranscriptLen(), fresh.widget.TranscriptLen(); got != want {
		t.Fatalf("command.widget.TranscriptLen() = %d, want %d", got, want)
	}
	if got, want := model.command.widget.ScrollOffset(), fresh.widget.ScrollOffset(); got != want {
		t.Fatalf("command.widget.ScrollOffset() = %d, want %d", got, want)
	}
	if got, want := model.command.innerWidth, fresh.innerWidth; got != want {
		t.Fatalf("command.innerWidth = %d, want %d", got, want)
	}
	if got, want := model.command.innerHeight, fresh.innerHeight; got != want {
		t.Fatalf("command.innerHeight = %d, want %d", got, want)
	}
}

// TestMidRunSwitchResetsResultsPane asserts that a successful mid-run
// connection swap leaves the Results Pane model indistinguishable from a fresh
// newResultsPaneModeModel given the same terminal geometry: cursor reset to
// the first row/column, scroll offsets cleared, selection-mode flag cleared,
// any pending action (Statement Expansion / goto-top) cleared, and cached
// prepared page dropped. Geometry (width/height) and the version banner are
// preserved.
func TestMidRunSwitchResetsResultsPane(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	betaAdapter := openTestAdapter(t)
	defer func() {
		if err := betaAdapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.newHistory = func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil }
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return betaAdapter, nil
	}

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	wantWidth := model.resultsPane.width
	wantHeight := model.resultsPane.height

	// Seed stale Results Pane state from connection alpha.
	model.resultsPane.selectedRow = 12
	model.resultsPane.selectedColumn = 3
	model.resultsPane.colScrollOffset = 17
	model.resultsPane.viewportStart = 50
	model.resultsPane.selectionActive = true
	model.resultsPane.pendingAction = resultsPanePendingActionComposeInsert
	model.resultsPane.cachedPage = &tui.ResultsPanePreparedPage{}

	// Perform the mid-run swap to beta.
	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)
	successMsg := cmd()
	if _, ok := successMsg.(midRunConnectSuccessMsg); !ok {
		t.Fatalf("cmd() = %T, want midRunConnectSuccessMsg", successMsg)
	}
	next, _ = model.Update(successMsg)
	model = next.(Model)

	// Compare to a freshly-booted Results Pane re-sized the same way.
	fresh := newResultsPaneModeModel("")
	fresh.SetSize(wantWidth, wantHeight)

	if got, want := model.resultsPane.selectedRow, fresh.selectedRow; got != want {
		t.Fatalf("resultsPane.selectedRow = %d, want %d", got, want)
	}
	if got, want := model.resultsPane.selectedColumn, fresh.selectedColumn; got != want {
		t.Fatalf("resultsPane.selectedColumn = %d, want %d", got, want)
	}
	if got, want := model.resultsPane.colScrollOffset, fresh.colScrollOffset; got != want {
		t.Fatalf("resultsPane.colScrollOffset = %d, want %d", got, want)
	}
	if got, want := model.resultsPane.viewportStart, fresh.viewportStart; got != want {
		t.Fatalf("resultsPane.viewportStart = %d, want %d", got, want)
	}
	if got, want := model.resultsPane.selectionActive, fresh.selectionActive; got != want {
		t.Fatalf("resultsPane.selectionActive = %v, want %v", got, want)
	}
	if got, want := model.resultsPane.pendingAction, fresh.pendingAction; got != want {
		t.Fatalf("resultsPane.pendingAction = %q, want %q", got, want)
	}
	if got, want := model.resultsPane.cachedPage, fresh.cachedPage; got != nil || want != nil {
		t.Fatalf("resultsPane.cachedPage = %v, want nil", got)
	}
	if got, want := model.resultsPane.width, fresh.width; got != want {
		t.Fatalf("resultsPane.width = %d, want %d", got, want)
	}
	if got, want := model.resultsPane.height, fresh.height; got != want {
		t.Fatalf("resultsPane.height = %d, want %d", got, want)
	}
	if got, want := model.resultsPane.version, fresh.version; got != want {
		t.Fatalf("resultsPane.version = %q, want %q", got, want)
	}
	if got, want := model.resultsPane.hintIdx, 0; got < want || got >= len(emptyStateHints) {
		t.Fatalf("resultsPane.hintIdx = %d, want in [0, %d)", got, len(emptyStateHints))
	}
}

// TestMidRunSwitchResetsLayoutAndActivePane asserts that a successful mid-run
// connection swap returns Layout and Active Pane to their cold-boot defaults
// (Split layout, Command Pane focused), irrespective of the layout the user
// had chosen before opening the Picker.
func TestMidRunSwitchResetsLayoutAndActivePane(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	betaAdapter := openTestAdapter(t)
	defer func() {
		if err := betaAdapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.newHistory = func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil }
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return betaAdapter, nil
	}

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	// Seed a non-default layout/active pane — user had maximized the Command
	// Pane and was focused on the Results Pane.
	model.state.SetLayout(LayoutCommandOnly)
	model.state.SetActivePane(PaneResults)

	// Perform the mid-run swap to beta.
	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)
	successMsg := cmd()
	if _, ok := successMsg.(midRunConnectSuccessMsg); !ok {
		t.Fatalf("cmd() = %T, want midRunConnectSuccessMsg", successMsg)
	}
	next, _ = model.Update(successMsg)
	model = next.(Model)

	if got, want := model.state.Interaction.Layout, LayoutSplit; got != want {
		t.Fatalf("Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("Interaction.ActivePane = %q, want %q", got, want)
	}
}

// TestMidRunSwitchClearsStaleInteractionState asserts that the five
// InteractionState fields that can carry stale prior-Session content
// (CurrentSQL, LastSubmittedSQL, PendingIntent, LastAction, PendingPaneSwitch)
// are reset to their zero values on a successful mid-run swap. CurrentSQL is
// re-mirrored from the freshly-empty editor so it matches the post-reset
// Command Pane.
func TestMidRunSwitchClearsStaleInteractionState(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	betaAdapter := openTestAdapter(t)
	defer func() {
		if err := betaAdapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.newHistory = func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil }
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return betaAdapter, nil
	}

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	// Seed stale prior-Session state.
	model.command.SetEditorValue("select stale; -- from alpha")
	model.syncCurrentSQL()
	model.state.SetLastSubmittedSQL("select prior; -- from alpha")
	model.state.SetPendingIntent(IntentSubmit, "submit", "pending", NotificationInfo)
	model.state.SetPendingPaneSwitch(&PaneSwitchContext{
		FromLayout: LayoutSplit,
		ToLayout:   LayoutResultsOnly,
		FromPane:   PaneCommand,
		ToPane:     PaneResults,
	})

	// Perform the mid-run swap to beta.
	next, cmd := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)
	successMsg := cmd()
	if _, ok := successMsg.(midRunConnectSuccessMsg); !ok {
		t.Fatalf("cmd() = %T, want midRunConnectSuccessMsg", successMsg)
	}
	next, _ = model.Update(successMsg)
	model = next.(Model)

	if got, want := model.state.Interaction.CurrentSQL, ""; got != want {
		t.Fatalf("Interaction.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.LastSubmittedSQL, ""; got != want {
		t.Fatalf("Interaction.LastSubmittedSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.PendingIntent, IntentNone; got != want {
		t.Fatalf("Interaction.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.LastAction, ""; got != want {
		t.Fatalf("Interaction.LastAction = %q, want %q", got, want)
	}
	if got := model.state.Interaction.PendingPaneSwitch; got != nil {
		t.Fatalf("Interaction.PendingPaneSwitch = %#v, want nil", got)
	}
}

// ---- Mid-run connect abort (double-Esc) ----

func TestMidRunConnectDoubleEscAborts(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	defer alphaAdapter.Close()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return alphaAdapter, nil
	}

	// Trigger mid-run connect for "beta".
	next, _ := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)
	if model.cancelConnect == nil {
		t.Fatal("cancelConnect = nil after starting mid-run connect")
	}

	// First Esc arms PendingAbort.
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if !model.pendingConnectAbort {
		t.Fatal("PendingAbort = false after first Esc, want true")
	}

	// Second Esc cancels the connect.
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if model.pendingConnectAbort {
		t.Fatal("PendingAbort = true after second Esc, want false (aborted)")
	}
}

func TestMidRunConnectAbortDisarmsOnOtherKey(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	defer alphaAdapter.Close()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return alphaAdapter, nil
	}

	// Trigger mid-run connect for "beta".
	next, _ := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)

	// First Esc arms PendingAbort.
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if !model.pendingConnectAbort {
		t.Fatal("PendingAbort = false after first Esc, want true")
	}

	// Any other key disarms it.
	next, _ = model.Update(tea.KeyPressMsg{Text: "x"})
	model = next.(Model)
	if model.pendingConnectAbort {
		t.Fatal("PendingAbort = true after non-Esc key, want false")
	}
}

func TestMidRunConnectCtrlCQuits(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	defer alphaAdapter.Close()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return alphaAdapter, nil
	}

	// Trigger mid-run connect for "beta".
	next, _ := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)

	// ctrl+c should quit even during a mid-run connect.
	_, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+c) cmd = nil, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Update(ctrl+c) cmd() type = %T, want tea.QuitMsg", cmd())
	}
}

func TestMidRunConnectAbortedSilentlyOnContextCanceled(t *testing.T) {
	alphaAdapter := openTestAdapter(t)
	defer alphaAdapter.Close()

	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newReadyModel(t, alphaAdapter, "alpha", connections)
	model.open = func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
		return alphaAdapter, nil
	}

	// Trigger mid-run connect for "beta".
	next, _ := model.Update(midRunConnectMsg{name: "beta"})
	model = next.(Model)

	// Simulate the abort: context.Canceled failure.
	next, _ = model.Update(midRunConnectFailedMsg{err: context.Canceled})
	model = next.(Model)

	// State should be Ready (not Error), and the notification should NOT say
	// "Connection failed:" — the abort should be silent.
	if model.state.App.Current != StateReady {
		t.Fatalf("state = %q, want %q", model.state.App.Current, StateReady)
	}
	if containsString(model.state.Notification.Text, "Connection failed:") {
		t.Fatalf("notification = %q, should not contain 'Connection failed:' for aborted connect", model.state.Notification.Text)
	}
	// Old session should still be on alpha.
	if model.session.ConnectionName != "alpha" {
		t.Fatalf("session.ConnectionName = %q, want %q", model.session.ConnectionName, "alpha")
	}
}

// ---- UX fix: startup auto-connect failure with empty candidates → quit ----

func TestAutoConnectStringArgFailureQuitsWhenNoCandidates(t *testing.T) {
	// A bare connection-string arg has no Name and no named candidates.
	connectErr := errors.New("connection refused")
	model := newModelWithDependencies(Session{}, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, connectErr
		},
		autoConnectTarget: config.ResolvedConnection{
			Name:       "", // bare connection string — not a named connection
			Raw:        "postgres://localhost/noexist",
			Connection: config.Connection{Type: "postgres", Host: "localhost", Database: "noexist"},
		},
		// No connectionsLoader → no candidates.
	})

	// Simulate the connect failure.
	next, cmd := model.Update(pickerConnectFailedMsg{err: connectErr})
	_ = next
	if cmd == nil {
		t.Fatal("Update(pickerConnectFailedMsg) cmd = nil, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("cmd() type = %T, want tea.QuitMsg (should quit when candidates empty)", cmd())
	}
}

func TestAutoConnectNamedArgFailureReturnsToPicker(t *testing.T) {
	// A named arg that fails still returns to the picker (the named connection IS a candidate).
	connectErr := errors.New("connection refused")
	model := newModelWithDependencies(Session{}, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, connectErr
		},
		autoConnectTarget: config.ResolvedConnection{
			Name:       "petworks-local",
			Raw:        "petworks-local",
			Connection: config.Connection{Type: "sqlite", Database: ":memory:"},
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"petworks-local": {Type: "sqlite", Database: ":memory:"},
			}}, nil
		},
	})
	// A named arg that fails drops into the startup Picker Modal (the named
	// connection IS a candidate). Constructing the model auto-connects, so no
	// startup modal is pushed; the failure handler pushes it.
	next, cmd := model.Update(pickerConnectFailedMsg{err: connectErr})
	model = next.(Model)

	// Should return to the Picker (not quit).
	if model.state.App.Current != StateSelectConnection {
		t.Fatalf("state = %q, want %q", model.state.App.Current, StateSelectConnection)
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("named arg failure should NOT quit when picker has candidates")
		}
	}

	// The startup Picker Modal is now open with the failed connection marked.
	pm, ok := model.currentModal().(*connectionPickerModal)
	if !ok {
		t.Fatalf("currentModal() = %T, want *connectionPickerModal", model.currentModal())
	}
	if !pm.startup {
		t.Fatal("pushed Picker Modal should be in startup mode")
	}
	if pm.lastFailedConnection != "petworks-local" {
		t.Fatalf("lastFailedConnection = %q, want %q", pm.lastFailedConnection, "petworks-local")
	}
	if !strings.Contains(model.state.Notification.Text, "Connection failed") {
		t.Fatalf("notification = %q, want it to contain %q", model.state.Notification.Text, "Connection failed")
	}
}

// ---- Helpers ----

// newReadyModel creates a Model in StateReady with the given adapter, connection
// name, and connections available.
func newReadyModel(t *testing.T, adapter *db.SQLAdapter, connName string, connections config.Connections) Model {
	t.Helper()
	model := newModelWithDependencies(Session{
		ConnectionName: connName,
		DatabaseType:   "sqlite",
		Adapter:        adapter,
	}, modelDependencies{
		open: func(_ context.Context, conn config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
		newHistory:        func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil },
	})
	// Force StateReady (the model starts in StateStartup when adapter is given).
	model.state.SetReady("", NotificationNone)
	return model
}

// typeCommandAndSubmit types text into the command pane and submits.
func typeCommandAndSubmit(t *testing.T, model Model, text string) Model {
	t.Helper()
	// Set the command value directly and dispatch a submit.
	model.command.SetEditorValue(text)
	model.syncCurrentSQL()
	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	if cmd != nil {
		// The submit intent might produce further messages (e.g. opening the modal).
		msgs := collectCommandMessagesForTest(t, cmd)
		for _, msg := range msgs {
			next2, _ := model.Update(msg)
			model = next2.(Model)
		}
	}
	return model
}
