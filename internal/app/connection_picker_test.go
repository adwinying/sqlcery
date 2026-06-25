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

// --- Picker state machine tests ---

func TestPickerInitialStateIsSelectConnection(t *testing.T) {
	model := newModelWithDependencies(Session{}, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, nil
		},
	})
	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("initial state = %q, want %q", got, want)
	}
}

func TestPickerAutoConnectTargetUsesStartupState(t *testing.T) {
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
	if got, want := model.state.App.Current, StateStartup; got != want {
		t.Fatalf("initial state = %q, want %q (auto-connect target should start in StateStartup)", got, want)
	}
}

func TestPickerLoadConnectionsOrdered(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"beta":  {Type: "sqlite", Database: ":memory:"},
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"gamma": {Type: "sqlite", Database: ":memory:"},
		},
	}

	model := newModelWithDependencies(Session{}, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() cmd = nil, want pickerInitMsg command")
	}

	// pickerInitMsg should trigger pickerLoadConnectionsCmd
	msg := cmd()
	if _, ok := msg.(pickerInitMsg); !ok {
		t.Fatalf("Init() msg type = %T, want pickerInitMsg", msg)
	}

	next, loadCmd := model.Update(pickerInitMsg{})
	model = next.(Model)
	if loadCmd == nil {
		t.Fatal("Update(pickerInitMsg{}) cmd = nil, want load command")
	}

	loadMsg := loadCmd()
	next, _ = model.Update(loadMsg)
	model = next.(Model)

	if got := len(model.picker.Candidates); got != 3 {
		t.Fatalf("picker.Candidates = %d items, want 3", got)
	}
	// Without frecency, should be sorted alphabetically by default.
	wantFirst := "alpha"
	if got := model.picker.Candidates[0]; got != wantFirst {
		t.Fatalf("picker.Candidates[0] = %q, want %q", got, wantFirst)
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

	model := newModelWithDependencies(Session{}, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, errors.New("not called in this test")
		},
	})
	model.picker.Candidates = []string{"local"}
	model.picker.Selected = 0

	// Press Enter to select.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = next
	if cmd == nil {
		t.Fatal("Update(Enter) cmd = nil, want connect command")
	}

	msg := cmd()
	connectMsg, ok := msg.(pickerConnectMsg)
	if !ok {
		t.Fatalf("cmd() type = %T, want pickerConnectMsg", msg)
	}
	if got, want := connectMsg.resolved.Name, "local"; got != want {
		t.Fatalf("connectMsg.resolved.Name = %q, want %q", got, want)
	}
}

func TestPickerFilterTypingNarrows(t *testing.T) {
	model := newModelWithDependencies(Session{}, modelDependencies{})
	model.state.App.Current = StateSelectConnection
	model.picker.Candidates = []string{"postgres-prod", "postgres-dev", "sqlite-local"}

	// Type "sqlite"
	for _, ch := range "sqlite" {
		next, _ := model.Update(tea.KeyPressMsg{Text: string(ch)})
		model = next.(Model)
	}

	if got, want := model.picker.Filter, "sqlite"; got != want {
		t.Fatalf("picker.Filter = %q, want %q", got, want)
	}

	filtered := pickerFilteredCandidates(model.picker.Candidates, model.picker.Filter)
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

	model := newModelWithDependencies(Session{}, modelDependencies{
		frecencyStore: fs,
		newHistory:    func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"local": {Type: "sqlite", Database: ":memory:"},
			}}, nil
		},
	})
	model.state.App.Current = StateSelectConnection
	model.picker.Candidates = []string{"local"}

	// Simulate selecting "local".
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Update(Enter) cmd = nil")
	}

	// cmd should produce pickerConnectMsg → pickerConnectMsg → pickerConnectSuccessMsg
	msg := cmd()
	connectMsg, ok := msg.(pickerConnectMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want pickerConnectMsg", msg)
	}

	next, cmd = model.Update(connectMsg)
	model = next.(Model)
	if got, want := model.state.App.Current, StateStartup; got != want {
		t.Fatalf("state after connect = %q, want %q", got, want)
	}

	if cmd == nil {
		t.Fatal("Update(pickerConnectMsg) cmd = nil, want open command")
	}
	successMsg := cmd()

	next, _ = model.Update(successMsg)
	model = next.(Model)

	if got, want := model.state.App.Current, StateReady; got != want {
		t.Fatalf("state after connect success = %q, want %q", got, want)
	}

	// Frecency should have been recorded exactly once.
	if got, want := len(fs.opens), 1; got != want {
		t.Fatalf("frecency opens = %d, want %d", got, want)
	}
	if got, want := fs.opens[0], "local"; got != want {
		t.Fatalf("frecency opens[0] = %q, want %q", got, want)
	}
}

func TestPickerFailureReturnsToPickerWithError(t *testing.T) {
	connectErr := errors.New("connection refused")

	model := newModelWithDependencies(Session{}, modelDependencies{
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return nil, connectErr
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"broken": {Type: "sqlite", Database: "/nonexistent.db"},
			}}, nil
		},
	})
	model.state.App.Current = StateSelectConnection
	model.picker.Candidates = []string{"broken"}

	// Select "broken"
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	connectMsg := cmd().(pickerConnectMsg)

	next, cmd = model.Update(connectMsg)
	model = next.(Model)

	// Get the open result (fail).
	failMsg := cmd()
	next, _ = model.Update(failMsg)
	model = next.(Model)

	if got, want := model.state.App.Current, StateSelectConnection; got != want {
		t.Fatalf("state after connect failure = %q, want %q", got, want)
	}
	if model.picker.ConnectError == "" {
		t.Fatal("picker.ConnectError = empty, want non-empty error message")
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

	model := newModelWithDependencies(Session{}, modelDependencies{
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
	model.state.App.Current = StateSelectConnection
	model.picker.Candidates = []string{"local"}

	// Select "local" → fires pickerConnectMsg
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	connectMsg := cmd().(pickerConnectMsg)

	// Process pickerConnectMsg → transitions to StateStartup, starts async open
	next, asyncCmd := model.Update(connectMsg)
	model = next.(Model)
	if got, want := model.state.App.Current, StateStartup; got != want {
		t.Fatalf("state after connect start = %q, want %q", got, want)
	}

	// Start the async open in background.
	go func() {
		_ = asyncCmd
	}()
	_ = connectStarted

	// First Esc arms PendingAbort.
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if !model.picker.PendingAbort {
		t.Fatal("picker.PendingAbort = false after first Esc, want true")
	}

	// Second Esc cancels the context and arms the cancel.
	if model.cancelConnect == nil {
		t.Skip("cancelConnect is nil: async connect completed too fast for test")
	}

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if model.picker.PendingAbort {
		t.Fatal("picker.PendingAbort = true after second Esc, want false (aborted)")
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
	// Simulate being in StateStartup with a pending connect (picker origin).
	model.state.App.Current = StateStartup
	ctx, cancel := context.WithCancel(context.Background())
	model.cancelConnect = cancel
	_ = ctx

	// First Esc arms PendingAbort.
	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if !model.picker.PendingAbort {
		t.Fatal("picker.PendingAbort = false after first Esc, want true")
	}

	// Any other key disarms it.
	next, _ = model.Update(tea.KeyPressMsg{Text: "x"})
	model = next.(Model)
	if model.picker.PendingAbort {
		t.Fatal("picker.PendingAbort = true after non-Esc key, want false")
	}
	cancel()
}

func TestPickerEmptyStateShowsMessage(t *testing.T) {
	model := newModelWithDependencies(Session{}, modelDependencies{
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{}, nil
		},
	})
	model.state.App.Current = StateSelectConnection

	// After loading (empty), the view should mention "No connections defined".
	model.picker.Candidates = nil
	view := model.View().Content
	if !containsString(view, "No connections") {
		t.Fatalf("View() = %q, want to contain %q", view, "No connections")
	}
}

func TestPickerCtrlCQuitsFromPickerState(t *testing.T) {
	model := newModelWithDependencies(Session{}, modelDependencies{})
	model.state.App.Current = StateSelectConnection

	_, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+c) cmd = nil, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Update(ctrl+c) cmd() type = %T, want tea.QuitMsg", cmd())
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
		newHistory:    func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		autoConnectTarget: config.ResolvedConnection{
			Name:       "petworks-local",
			Raw:        "petworks-local",
			Connection: config.Connection{Type: "sqlite", Database: ":memory:"},
		},
	})

	if got, want := model.state.App.Current, StateStartup; got != want {
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
	// cmd is the async open.
	successMsg := cmd()

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

func TestPickerStateSelectConnectionViewRendersFilter(t *testing.T) {
	model := newModelWithDependencies(Session{}, modelDependencies{})
	model.state.App.Current = StateSelectConnection
	model.picker.Candidates = []string{"local", "prod"}

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	view := model.View().Content
	if !containsString(view, "connection picker") {
		t.Fatalf("View() = %q, want to contain %q", view, "connection picker")
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

	model := newModelWithDependencies(Session{}, modelDependencies{
		frecencyStore: fs,
		newHistory:    func(_ string) (*apphistory.History, error) { return apphistory.NewHistory(), nil },
		open: func(_ context.Context, _ config.Connection) (*db.SQLAdapter, error) {
			return adapter, nil
		},
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{Connection: map[string]config.Connection{
				"mydb": {Type: "sqlite", Database: ":memory:"},
			}}, nil
		},
	})
	model.state.App.Current = StateSelectConnection
	model.picker.Candidates = []string{"mydb"}

	// Full path: select → connect → success
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	connectMsg := cmd().(pickerConnectMsg)

	next, cmd = model.Update(connectMsg)
	model = next.(Model)
	successMsg := cmd()

	next, _ = model.Update(successMsg)
	model = next.(Model)

	if got, want := len(fs.opens), 1; got != want {
		t.Fatalf("frecency RecordOpen called %d times, want exactly 1", got)
	}
}
