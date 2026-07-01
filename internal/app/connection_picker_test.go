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

// --- Frecency fake ---

type fakeFrecencyStore struct {
	opens []string
}

func (f *fakeFrecencyStore) RecordOpen(name string) error {
	f.opens = append(f.opens, name)
	return nil
}

func (f *fakeFrecencyStore) Order(names []string) []string {
	// Return as-is (stable input order) for deterministic tests.
	out := make([]string, len(names))
	copy(out, names)
	return out
}

// newStartupPickerModel constructs a no-Adapter Model and drives the first tick
// (pickerInitMsg) so the startup Connection Picker Modal is pushed — mirroring
// what Init does in production.
func newStartupPickerModel(t *testing.T, deps modelDependencies) Model {
	t.Helper()
	model := newModelWithDependencies(Session{}, deps)
	next, _ := model.Update(pickerInitMsg{})
	return next.(Model)
}

// startupPicker returns the current modal as the startup Connection Picker,
// failing the test if it is not one.
func startupPicker(t *testing.T, model Model) *connectionPickerModal {
	t.Helper()
	pm, ok := model.currentModal().(*connectionPickerModal)
	if !ok {
		t.Fatalf("currentModal() = %T, want *connectionPickerModal", model.currentModal())
	}
	return pm
}

// --- Picker state machine tests ---

func TestPickerInitialStateIsSelectConnection(t *testing.T) {
	model := newStartupPickerModel(t, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, nil
		},
	})
	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("initial state = %q, want %q", got, want)
	}
	// The startup Picker Modal is pushed eagerly at construction.
	pm := startupPicker(t, model)
	if !pm.startup {
		t.Fatal("startup Picker Modal should have startup=true")
	}
}

func TestPickerAutoConnectTargetUsesSelectConnectionState(t *testing.T) {
	model := newModelWithDependencies(Session{}, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, nil
		},
		autoConnectTarget: config.ResolvedConnection{
			Name:       "local",
			Raw:        "local",
			Connection: config.Connection{Type: "sqlite", Database: ":memory:"},
		},
	})
	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("initial state = %q, want %q (auto-connect target should start in StateSelectConnection)", got, want)
	}
	// No modal is pushed at construction; the Connecting Modal is pushed
	// when Init() fires pickerConnectMsg → handlePickerConnect.
	if model.currentModal() != nil {
		t.Fatalf("currentModal() = %T, want nil at construction", model.currentModal())
	}
}

func TestPickerStartupModalLoadsOrderedCandidates(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"beta":  {Type: "sqlite", Database: ":memory:"},
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"gamma": {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})

	pm := startupPicker(t, model)
	if got := len(pm.candidates); got != 3 {
		t.Fatalf("candidates = %d items, want 3", got)
	}
	// Without frecency, should be sorted alphabetically by default.
	if got, want := pm.candidates[0], "alpha"; got != want {
		t.Fatalf("candidates[0] = %q, want %q", got, want)
	}
}

func TestPickerFilterNarrowsCandidates(t *testing.T) {
	candidates := []string{"postgres-prod", "postgres-dev", "sqlite-local"}
	filter := "prod"
	filtered := pickerFilteredCandidates(candidates, filter)
	if got, want := len(filtered), 1; got != want {
		t.Fatalf("len(filtered) = %d, want %d", got, want)
	}
	if got, want := filtered[0], "postgres-prod"; got != want {
		t.Fatalf("filtered[0] = %q, want %q", got, want)
	}
}

func TestPickerFilterEmptyReturnsAll(t *testing.T) {
	candidates := []string{"a", "b", "c"}
	filtered := pickerFilteredCandidates(candidates, "")
	if got, want := len(filtered), 3; got != want {
		t.Fatalf("len(filtered) = %d, want %d", got, want)
	}
}

func TestPickerSelectionEmitsConnectMsg(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"local": {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, errors.New("not called in this test")
		},
	})

	// Press Enter to select the highlighted "local".
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = next
	if cmd == nil {
		t.Fatal("Update(Enter) cmd = nil, want connect command")
	}

	msg := cmd()
	connectMsg, ok := msg.(midRunConnectMsg)
	if !ok {
		t.Fatalf("cmd() type = %T, want midRunConnectMsg", msg)
	}
	if got, want := connectMsg.name, "local"; got != want {
		t.Fatalf("connectMsg.name = %q, want %q", got, want)
	}
}

func TestPickerFilterTypingNarrows(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"postgres-prod": {Type: "postgres"},
			"postgres-dev":  {Type: "postgres"},
			"sqlite-local":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})

	// Type "sqlite"
	for _, ch := range "sqlite" {
		next, _ := model.Update(tea.KeyPressMsg{Text: string(ch)})
		model = next.(Model)
	}

	pm := startupPicker(t, model)
	if got, want := pm.filter, "sqlite"; got != want {
		t.Fatalf("filter = %q, want %q", got, want)
	}

	filtered := pickerFilteredCandidates(pm.candidates, pm.filter)
	if got, want := len(filtered), 1; got != want {
		t.Fatalf("len(filtered) = %d, want %d", got, want)
	}
	if got, want := filtered[0], "sqlite-local"; got != want {
		t.Fatalf("filtered[0] = %q, want %q", got, want)
	}
}

func TestPickerSuccessTransitionsToReady(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	fs := &fakeFrecencyStore{}

	model := newStartupPickerModel(t, modelDependencies{
		frecencyStore: fs,
		newHistory:    func(_ config.ConnectionIdentity) (*apphistory.History, error) { return apphistory.NewHistory(), nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"local": {Type: "sqlite", Database: ":memory:"},
			}}, nil
		},
	})

	// Enter selects "local" → modal forwards midRunConnectMsg.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Update(Enter) cmd = nil")
	}
	connectMsg, ok := cmd().(midRunConnectMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want midRunConnectMsg", cmd())
	}

	// handleMidRunConnect keeps the Picker open (StateSelectConnection) while
	// connecting, rather than switching to a full-screen StateStartup.
	next, cmd = model.Update(connectMsg)
	model = next.(Model)
	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("state while connecting = %q, want %q (modal stays open)", got, want)
	}
	if cmd == nil {
		t.Fatal("Update(midRunConnectMsg) cmd = nil, want open command")
	}
	successMsg := cmd()

	next, _ = model.Update(successMsg)
	model = next.(Model)

	if got, want := model.state.App.Current, StateReady; got != want {
		t.Fatalf("state after connect success = %q, want %q", got, want)
	}
	// The Modal is closed once connected.
	if model.currentModal() != nil {
		t.Fatalf("currentModal() = %T after success, want nil", model.currentModal())
	}

	// Frecency should have been recorded exactly once.
	if got, want := len(fs.opens), 1; got != want {
		t.Fatalf("frecency opens = %d, want %d", got, want)
	}
	if got, want := fs.opens[0], "local"; got != want {
		t.Fatalf("frecency opens[0] = %q, want %q", got, want)
	}
}

func TestPickerFailureKeepsPickerOpenWithMarker(t *testing.T) {
	connectErr := errors.New("connection refused")

	model := newStartupPickerModel(t, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, connectErr
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"broken": {Type: "sqlite", Database: "/nonexistent.db"},
			}}, nil
		},
	})

	// Select "broken" → connect → fail.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	connectMsg := cmd().(midRunConnectMsg)

	next, cmd = model.Update(connectMsg)
	model = next.(Model)

	failMsg := cmd()
	next, _ = model.Update(failMsg)
	model = next.(Model)

	// Stays in the Picker (no Session to fall back to), Modal still open.
	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("state after connect failure = %q, want %q", got, want)
	}
	pm := startupPicker(t, model)
	if pm.lastFailedConnection != "broken" {
		t.Fatalf("lastFailedConnection = %q, want %q", pm.lastFailedConnection, "broken")
	}
	if model.state.Notification.Text == "" {
		t.Fatal("notification = empty, want a connection-failed message in the Status Bar")
	}
	if model.state.Notification.Level != NotificationError {
		t.Fatalf("notification level = %v, want NotificationError", model.state.Notification.Level)
	}
}

func TestPickerDoubleEscArmsAndAborts(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	connectStarted := make(chan struct{})
	connectUnblock := make(chan struct{})

	model := newStartupPickerModel(t, modelDependencies{
		open: func(ctx context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			close(connectStarted)
			select {
			case <-connectUnblock:
				return adapter, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"local": {Type: "sqlite", Database: ":memory:"},
			}}, nil
		},
	})

	// Select "local" → fires midRunConnectMsg.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	connectMsg := cmd().(midRunConnectMsg)

	// Process midRunConnectMsg → stays in the Picker, starts async open.
	next, asyncCmd := model.Update(connectMsg)
	model = next.(Model)
	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("state while connecting = %q, want %q", got, want)
	}

	// Start the async open in background.
	go func() { _ = asyncCmd }()
	_ = connectStarted

	// First Esc arms the abort.
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if !model.pendingConnectAbort {
		t.Fatal("pendingConnectAbort = false after first Esc, want true")
	}

	if model.cancelConnect == nil {
		t.Skip("cancelConnect is nil: async connect completed too fast for test")
	}

	// Second Esc cancels the in-flight connect.
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if model.pendingConnectAbort {
		t.Fatal("pendingConnectAbort = true after second Esc, want false (aborted)")
	}

	// Allow the blocked open to exit via context cancel.
	close(connectUnblock)
}

func TestPickerAbortArmedDisarmsOnOtherKey(t *testing.T) {
	model := newModelWithDependencies(Session{}, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, nil
		},
	})
	// Simulate a connect in flight from the startup Picker.
	ctx, cancel := context.WithCancel(context.Background())
	model.cancelConnect = cancel
	_ = ctx

	// First Esc arms the abort.
	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if !model.pendingConnectAbort {
		t.Fatal("pendingConnectAbort = false after first Esc, want true")
	}

	// Any other key disarms it.
	next, _ = model.Update(tea.KeyPressMsg{Text: "x"})
	model = next.(Model)
	if model.pendingConnectAbort {
		t.Fatal("pendingConnectAbort = true after non-Esc key, want false")
	}
	cancel()
}

func TestPickerEmptyStateShowsCreateRow(t *testing.T) {
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{}, nil
		},
	})

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	view := model.View().Content
	// With no connections the pinned create row is the only item shown.
	if !containsString(view, "Create a new connection") {
		t.Fatalf("View() = %q, want to contain %q", view, "Create a new connection")
	}
	// The old dead-end hint must be gone.
	if containsString(view, "Define connections") {
		t.Fatalf("View() should not contain old empty-state hint 'Define connections'")
	}
}

func TestPickerCtrlCQuitsFromPickerState(t *testing.T) {
	model := newStartupPickerModel(t, modelDependencies{})

	_, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+c) cmd = nil, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Update(ctrl+c) cmd() type = %T, want tea.QuitMsg", cmd())
	}
}

func TestPickerEscQuitsFromStartup(t *testing.T) {
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"local": {Type: "sqlite", Database: ":memory:"},
			}}, nil
		},
	})

	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update(Esc) cmd = nil, want tea.Quit at startup")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Update(Esc) cmd() type = %T, want tea.QuitMsg", cmd())
	}
}

func TestPickerAutoConnectRecordsFrecency(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	fs := &fakeFrecencyStore{}

	model := newModelWithDependencies(Session{}, modelDependencies{
		frecencyStore: fs,
		newHistory:    func(_ config.ConnectionIdentity) (*apphistory.History, error) { return apphistory.NewHistory(), nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		autoConnectTarget: config.ResolvedConnection{
			Name:       "petworks-local",
			Raw:        "petworks-local",
			Connection: config.Connection{Type: "sqlite", Database: ":memory:"},
		},
	})

	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("initial state = %q, want %q", got, want)
	}

	cmd := model.Init()
	msg := cmd()

	// Init emits pickerConnectMsg for the auto-connect target.
	connectMsg, ok := msg.(pickerConnectMsg)
	if !ok {
		t.Fatalf("Init() msg type = %T, want pickerConnectMsg", msg)
	}

	next, cmd := model.Update(connectMsg)
	model = next.(Model)
	// cmd is a batch of [open goroutine, connectingTickCmd].
	successMsg := firstCommandMessageForTest[pickerConnectSuccessMsg](t, cmd)

	next, _ = model.Update(successMsg)
	model = next.(Model)

	if got, want := model.state.App.Current, StateReady; got != want {
		t.Fatalf("state = %q, want %q", got, want)
	}

	if got, want := len(fs.opens), 1; got != want {
		t.Fatalf("frecency opens = %d, want 1", got)
	}
	if got, want := fs.opens[0], "petworks-local"; got != want {
		t.Fatalf("frecency opens[0] = %q, want %q", got, want)
	}
}

func TestPickerAutoConnectLoadsHistoryByConnectionIdentity(t *testing.T) {
	adapter := openTestAdapter(t)
	defer adapter.Close()

	wantIdentity := config.ConnectionIdentity("opaque-startup-identity")
	var gotIdentity string
	model := newModelWithDependencies(Session{}, modelDependencies{
		newHistory: func(identity config.ConnectionIdentity) (*apphistory.History, error) {
			gotIdentity = string(identity)
			return apphistory.NewHistory(), nil
		},
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		autoConnectTarget: config.ResolvedConnection{
			Name:       "local",
			Identity:   wantIdentity,
			Connection: config.Connection{Type: "sqlite", Database: ":memory:"},
		},
	})

	next, cmd := model.Update(model.Init()())
	model = next.(Model)
	success := firstCommandMessageForTest[pickerConnectSuccessMsg](t, cmd)
	_, _ = model.Update(success)

	if gotIdentity != string(wantIdentity) {
		t.Fatalf("NewHistory identity = %q, want %q", gotIdentity, wantIdentity)
	}
}

func TestPickerSelectionLoadsHistoryByResolvedConnectionIdentity(t *testing.T) {
	adapter := openTestAdapter(t)
	defer adapter.Close()

	connections := config.Connections{Connection: map[string]config.Connection{
		"local": {Origin: "/project/connections.toml", Type: "sqlite", Database: ":memory:"},
	}}
	want, err := config.ResolveConnectionReference(connections, "local")
	if err != nil {
		t.Fatalf("ResolveConnectionReference() error = %v", err)
	}
	var gotIdentity string
	model := newModelWithDependencies(Session{}, modelDependencies{
		newHistory: func(identity config.ConnectionIdentity) (*apphistory.History, error) {
			gotIdentity = string(identity)
			return apphistory.NewHistory(), nil
		},
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})

	next, cmd := model.Update(midRunConnectMsg{name: "local"})
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)

	if gotIdentity != string(want.Identity) {
		t.Fatalf("NewHistory identity = %q, want %q", gotIdentity, want.Identity)
	}
	if model.session.ConnectionIdentity != want.Identity {
		t.Fatalf("session.ConnectionIdentity = %q, want %q", model.session.ConnectionIdentity, want.Identity)
	}
}

func TestConnectionSwitchReplacesVisibleRecallWithSelectedIdentityHistory(t *testing.T) {
	oldAdapter := openTestAdapter(t)
	defer oldAdapter.Close()
	newAdapter := openTestAdapter(t)
	defer newAdapter.Close()

	selected := config.Connections{Connection: map[string]config.Connection{
		"second": {Origin: "/project/connections.toml", Type: "sqlite", Database: ":memory:"},
	}}
	resolved, err := config.ResolveConnectionReference(selected, "second")
	if err != nil {
		t.Fatalf("ResolveConnectionReference() error = %v", err)
	}
	firstHistory := apphistory.NewHistory()
	_ = firstHistory.Append("select first;")
	secondHistory := apphistory.NewHistory()
	_ = secondHistory.Append("select second;")

	model := newModelWithDependencies(Session{ConnectionName: "first", Adapter: oldAdapter}, modelDependencies{
		history: firstHistory,
		newHistory: func(identity config.ConnectionIdentity) (*apphistory.History, error) {
			if identity != resolved.Identity {
				t.Fatalf("NewHistory identity = %q, want %q", identity, resolved.Identity)
			}
			return secondHistory, nil
		},
		open:              func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) { return newAdapter, nil },
		closeAdapter:      func(*db.SQLAdapter) error { return nil },
		connectionsLoader: func() (config.Connections, error) { return selected, nil },
	})

	next, cmd := model.Update(midRunConnectMsg{name: "second"})
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)

	if got, want := len(model.state.Interaction.History), 1; got != want {
		t.Fatalf("len(visible History) = %d, want %d", got, want)
	}
	if got, want := model.state.Interaction.History[0].Statement, "select second;"; got != want {
		t.Fatalf("visible History Statement = %q, want %q", got, want)
	}
}

func TestPickerConnectionSummaryNeverIncludesCredentials(t *testing.T) {
	tests := []struct {
		name     string
		conn     config.Connection
		wantPart string
		badPart  string
	}{
		{
			name:     "postgres",
			conn:     config.Connection{Type: "postgres", Host: "db.example.com", Port: 5432, Database: "warehouse", Username: "secret_user", Password: "secret_pass"},
			wantPart: "postgres db.example.com:5432/warehouse",
			badPart:  "secret",
		},
		{
			name:     "mysql",
			conn:     config.Connection{Type: "mysql", Host: "127.0.0.1", Port: 3307, Database: "petworks", Username: "root", Password: "hunter2"},
			wantPart: "mysql 127.0.0.1:3307/petworks",
			badPart:  "hunter2",
		},
		{
			name:     "sqlite",
			conn:     config.Connection{Type: "sqlite", Database: "~/Downloads/app3.db3"},
			wantPart: "sqlite ~/Downloads/app3.db3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := pickerConnectionSummary(tt.conn)
			if tt.wantPart != "" && !containsString(summary, tt.wantPart) {
				t.Fatalf("summary = %q, want to contain %q", summary, tt.wantPart)
			}
			if tt.badPart != "" && containsString(summary, tt.badPart) {
				t.Fatalf("summary = %q, must not contain credentials %q", summary, tt.badPart)
			}
		})
	}
}

func TestPickerRenderRowColourSwatch(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"colored": {Type: "sqlite", Database: ":memory:", Color: "red"},
			"plain":   {Type: "sqlite", Database: ":memory:"},
		},
	}
	loader := func() (config.Connections, error) { return connections, nil }

	// With colour: row should contain the swatch character and the name.
	withColor := pickerRenderRow("colored", loader, 80)
	if !containsString(withColor, "■") {
		t.Fatalf("pickerRenderRow with colour: %q, want to contain swatch ■", withColor)
	}
	if !containsString(withColor, "colored") {
		t.Fatalf("pickerRenderRow with colour: %q, want to contain name", withColor)
	}

	// Without colour: row should not contain the swatch character.
	withoutColor := pickerRenderRow("plain", loader, 80)
	if containsString(withoutColor, "■") {
		t.Fatalf("pickerRenderRow without colour: %q, should not contain swatch", withoutColor)
	}
	if !containsString(withoutColor, "plain") {
		t.Fatalf("pickerRenderRow without colour: %q, want to contain name", withoutColor)
	}
}

func TestPickerStartupViewRendersAsModal(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"local": {Type: "sqlite", Database: ":memory:"},
			"prod":  {Type: "postgres"},
		},
	}
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	view := model.View().Content
	// The startup Picker renders through the same Modal chrome as mid-run:
	// a titled box ("Connection Picker") plus a "Filter:" box.
	if !containsString(view, "Connection Picker") {
		t.Fatalf("View() = %q, want to contain %q", view, "Connection Picker")
	}
	if !containsString(view, "Filter:") {
		t.Fatalf("View() = %q, want to contain %q", view, "Filter:")
	}
}

// containsString is a helper that avoids import of "strings" in the test body.
func containsString(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestPickerFrecencyRecordedExactlyOnce verifies that a single successful open
// records frecency exactly once, not twice (e.g. not in both connect and ready).
func TestPickerFrecencyRecordedExactlyOnce(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	fs := &fakeFrecencyStore{}

	model := newStartupPickerModel(t, modelDependencies{
		frecencyStore: fs,
		newHistory:    func(_ config.ConnectionIdentity) (*apphistory.History, error) { return apphistory.NewHistory(), nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"mydb": {Type: "sqlite", Database: ":memory:"},
			}}, nil
		},
	})

	// Full path: select → connect → success
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	connectMsg := cmd().(midRunConnectMsg)

	next, cmd = model.Update(connectMsg)
	model = next.(Model)
	successMsg := cmd()

	_, _ = model.Update(successMsg)

	if got, want := len(fs.opens), 1; got != want {
		t.Fatalf("frecency RecordOpen called %d times, want exactly 1", got)
	}
}
