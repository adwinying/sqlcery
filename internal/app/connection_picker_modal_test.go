package app

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
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
	defer betaAdapter.Close()

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
	defer alphaAdapter.Close()

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
	defer betaAdapter.Close()

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
	// Pre-load candidates so picker has them.
	model.picker.Candidates = []string{"petworks-local"}

	next, cmd := model.Update(pickerConnectFailedMsg{err: connectErr})
	model = next.(Model)

	// Should return to picker (not quit).
	if model.state.App.Current != StateSelectConnection {
		t.Fatalf("state = %q, want %q", model.state.App.Current, StateSelectConnection)
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("named arg failure should NOT quit when picker has candidates")
		}
	}
	if model.picker.ConnectError == "" {
		t.Fatal("picker.ConnectError should be set after named arg failure")
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

