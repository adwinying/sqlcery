package app

import (
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

func TestNewSharedAppStateDefaultsToCommandMode(t *testing.T) {
	state := NewSharedAppState()

	if got, want := state.App.Current, StateStartup; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}

	if got, want := state.Interaction.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}

	if got, want := state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}

	if got, want := state.Status, "Starting SQLcery."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestSharedAppStateSnapshotClonesInteractionState(t *testing.T) {
	stamp := time.Date(2026, time.April, 7, 11, 30, 0, 0, time.UTC)
	state := NewSharedAppState()
	state.SetReconnect("Reconnecting to database.", &ReconnectContext{
		Attempt:   2,
		Reason:    "connection dropped",
		LastError: "network timeout",
	})
	state.SetCurrentSQL("select * from widgets")
	state.SetLastSubmittedSQL("select * from widgets")
	state.SetPendingIntent(IntentSubmit, "submit", "ready")
	state.SetRunningStatementContext(&RunningStatementContext{Label: "SQL", StartedAt: stamp, Elapsed: 1500 * time.Millisecond, SpinnerFrame: 2})
	state.SetLayout(LayoutSplit)
	state.SetActivePane(PaneResults)
	state.SetHistory([]HistoryEntryContext{{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp}})
	state.SetAutocompleteSchema(&AutocompleteSchemaContext{
		Tables: []AutocompleteTableContext{{Namespace: "main", Name: "widgets", Columns: []string{"id", "payload"}}},
	})
	state.SetLatestResultContext(&LatestResultContext{
		Statement:           "select * from widgets",
		OriginPane:          PaneCommand,
		SelectedRows:        []int{1, 301},
		StatementKind:       db.StatementResultKindQuery,
		RowsAffected:        cloneInt64Pointer(int64PointerForTest(3)),
		LastInsertID:        cloneInt64Pointer(int64PointerForTest(9)),
		InlineRowsTruncated: true,
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "payload"}},
			Rows: append([]db.ResultRow{{
				Position: 1,
				Values:   []db.ResultValue{{Kind: db.ValueKindBytes, Value: []byte("abc")}},
			}}, make([]db.ResultRow, 300)...),
		},
		InlineResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "payload"}},
			Rows: []db.ResultRow{{
				Position: 1,
				Values:   []db.ResultValue{{Kind: db.ValueKindBytes, Value: []byte("xyz")}},
			}},
		},
	})
	state.SetResultsPanePage(1)
	state.SetPendingPaneSwitch(&PaneSwitchContext{
		FromLayout:    LayoutSplit,
		ToLayout:      LayoutResultsOnly,
		FromPane:      PaneCommand,
		ToPane:        PaneResults,
		ResultContext: state.Interaction.LatestResult,
	})
	snapshot := state.Snapshot()

	state.Interaction.CurrentSQL = "mutated"
	state.Interaction.LastSubmittedSQL = "mutated"
	state.Interaction.LastAction = "mutated"
	state.Interaction.Running.Label = "mutated"
	state.Interaction.Layout = LayoutResultsOnly
	state.Interaction.Running.Elapsed = 9 * time.Second
	state.Interaction.Running.SpinnerFrame = 1
	state.Interaction.History[0].Statement = "mutated history"
	state.App.Current = StateError
	state.App.Reconnect.Attempt = 99
	state.App.Reconnect.Reason = "changed"
	state.Interaction.AutocompleteSchema.Tables[0].Name = "changed"
	state.Interaction.AutocompleteSchema.Tables[0].Columns[0] = "changed"
	state.Interaction.LatestResult.Statement = "mutated"
	state.Interaction.LatestResult.SelectedRows[0] = 999
	state.Interaction.PendingPaneSwitch.ToPane = PaneCommand
	state.Interaction.PendingPaneSwitch.ResultContext.Statement = "mutated switch"
	*state.Interaction.LatestResult.RowsAffected = 99
	*state.Interaction.LatestResult.LastInsertID = 42
	state.Interaction.LatestResult.InlineRowsTruncated = false
	state.Interaction.LatestResult.PreservedResult.Columns[0].Name = "changed"
	state.Interaction.LatestResult.PreservedResult.Rows[0].Values[0].Value.([]byte)[0] = 'z'
	state.Interaction.LatestResult.InlineResult.Rows[0].Values[0].Value.([]byte)[0] = 'q'

	if got, want := snapshot.App.Current, StateReconnect; got != want {
		t.Fatalf("snapshot.App.Current = %q, want %q", got, want)
	}

	if got, want := snapshot.App.Reconnect.Attempt, 2; got != want {
		t.Fatalf("snapshot.App.Reconnect.Attempt = %d, want %d", got, want)
	}

	if got, want := snapshot.App.Reconnect.Reason, "connection dropped"; got != want {
		t.Fatalf("snapshot.App.Reconnect.Reason = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.CurrentSQL, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Interaction.CurrentSQL = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.LastSubmittedSQL, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Interaction.LastSubmittedSQL = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.LastAction, "submit"; got != want {
		t.Fatalf("snapshot.Interaction.LastAction = %q, want %q", got, want)
	}

	if snapshot.Interaction.Running == nil {
		t.Fatal("snapshot.Interaction.Running = nil, want running context")
	}

	if got, want := snapshot.Interaction.Layout, LayoutSplit; got != want {
		t.Fatalf("snapshot.Interaction.Layout = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("snapshot.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	if got, want := snapshot.Interaction.Running.Label, "SQL"; got != want {
		t.Fatalf("snapshot.Interaction.Running.Label = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.Running.Elapsed, 1500*time.Millisecond; got != want {
		t.Fatalf("snapshot.Interaction.Running.Elapsed = %v, want %v", got, want)
	}

	if got, want := snapshot.Interaction.Running.SpinnerFrame, 2; got != want {
		t.Fatalf("snapshot.Interaction.Running.SpinnerFrame = %d, want %d", got, want)
	}

	if got, want := snapshot.Interaction.History[0].Statement, "select 1"; got != want {
		t.Fatalf("snapshot.Interaction.History[0].Statement = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.AutocompleteSchema.Tables[0].Name, "widgets"; got != want {
		t.Fatalf("snapshot.Interaction.AutocompleteSchema.Tables[0].Name = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.AutocompleteSchema.Tables[0].Columns[0], "id"; got != want {
		t.Fatalf("snapshot.Interaction.AutocompleteSchema.Tables[0].Columns[0] = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.LatestResult.Statement, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Interaction.LatestResult.Statement = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.LatestResult.SelectedRows, []int{1, 301}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("snapshot.Interaction.LatestResult.SelectedRows = %#v, want %#v", got, want)
	}

	if snapshot.Interaction.LatestResult.RowsAffected == nil || *snapshot.Interaction.LatestResult.RowsAffected != 3 {
		t.Fatalf("snapshot.Interaction.LatestResult.RowsAffected = %#v, want 3", snapshot.Interaction.LatestResult.RowsAffected)
	}

	if snapshot.Interaction.LatestResult.LastInsertID == nil || *snapshot.Interaction.LatestResult.LastInsertID != 9 {
		t.Fatalf("snapshot.Interaction.LatestResult.LastInsertID = %#v, want 9", snapshot.Interaction.LatestResult.LastInsertID)
	}

	if !snapshot.Interaction.LatestResult.InlineRowsTruncated {
		t.Fatal("snapshot.Interaction.LatestResult.InlineRowsTruncated = false, want true")
	}

	if got, want := snapshot.Interaction.LatestResult.PreservedResult.Columns[0].Name, "payload"; got != want {
		t.Fatalf("snapshot.Interaction.LatestResult.PreservedResult.Columns[0].Name = %q, want %q", got, want)
	}

	if got, want := string(snapshot.Interaction.LatestResult.PreservedResult.Rows[0].Values[0].Value.([]byte)), "abc"; got != want {
		t.Fatalf("snapshot.Interaction.LatestResult.PreservedResult.Rows[0].Values[0] = %q, want %q", got, want)
	}

	if got, want := string(snapshot.Interaction.LatestResult.InlineResult.Rows[0].Values[0].Value.([]byte)), "xyz"; got != want {
		t.Fatalf("snapshot.Interaction.LatestResult.InlineResult.Rows[0].Values[0] = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.PendingPaneSwitch.ToPane, PaneResults; got != want {
		t.Fatalf("snapshot.Interaction.PendingPaneSwitch.ToPane = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.PendingPaneSwitch.ResultContext.Statement, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Interaction.PendingPaneSwitch.ResultContext.Statement = %q, want %q", got, want)
	}

	if got, want := string(snapshot.Interaction.PendingPaneSwitch.ResultContext.PreservedResult.Rows[0].Values[0].Value.([]byte)), "abc"; got != want {
		t.Fatalf("snapshot.Interaction.PendingPaneSwitch.ResultContext.PreservedResult.Rows[0].Values[0] = %q, want %q", got, want)
	}
}

func int64PointerForTest(value int64) *int64 {
	copy := value
	return &copy
}

func TestSharedAppStateTransitions(t *testing.T) {
	state := NewSharedAppState()
	state.SetReady("")

	if got, want := state.App.Current, StateReady; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}

	if got, want := state.Status, "Ready for SQL input."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	state.SetReconnect("", &ReconnectContext{Attempt: 1, Reason: "network lost", LastError: "io eof"})
	if got, want := state.App.Current, StateReconnect; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}

	if got, want := state.Status, "Reconnect requested; retry flow not implemented yet."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	state.SetError("Network error while reaching the database. Check the host, port, SSH tunnel, or VPN. Details: dial tcp failed", "Connection failed.")
	if got, want := state.App.Current, StateError; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}

	if got, want := state.App.Error, "Network error while reaching the database. Check the host, port, SSH tunnel, or VPN. Details: dial tcp failed"; got != want {
		t.Fatalf("state.App.Error = %q, want %q", got, want)
	}

	if got, want := state.Status, "Connection failed."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	state.SetReady("Recovered.")
	if got, want := state.App.Current, StateReady; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}

	if got := state.App.Error; got != "" {
		t.Fatalf("state.App.Error = %q, want empty", got)
	}

	if got := state.App.Reconnect; got != nil {
		t.Fatalf("state.App.Reconnect = %#v, want nil", got)
	}
}

func TestSharedAppStateSetLatestResultContextResetsResultsPanePage(t *testing.T) {
	state := NewSharedAppState()
	state.SetResultsPanePage(3)
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: []db.ResultRow{{}}}})

	if got, want := state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
}

func TestSharedAppStateSetResultsPanePageClampsToAvailableRows(t *testing.T) {
	state := NewSharedAppState()
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: make([]db.ResultRow, 305)}})
	state.SetResultsPanePage(9)

	if got, want := state.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	state.SetResultsPanePage(-2)
	if got, want := state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
}

func TestSharedAppStateChangeResultsPanePageClampsToAvailableRows(t *testing.T) {
	state := NewSharedAppState()
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: make([]db.ResultRow, 605)}})

	state.ChangeResultsPanePage(1)
	if got, want := state.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	state.ChangeResultsPanePage(10)
	if got, want := state.Interaction.ResultsPanePage, 2; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	state.ChangeResultsPanePage(-10)
	if got, want := state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
}
