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

	if got, want := state.Interaction.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Interaction.ActiveMode = %q, want %q", got, want)
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
	state.SetActiveMode(ModeResultsPane)
	state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1", ConnectionName: "local", ExecutedAt: stamp}})
	state.SetHistorySearchContext(&HistorySearchContext{Filter: "sel", SelectedIndex: 1})
	state.SetAutocompleteSchema(&AutocompleteSchemaContext{
		Tables: []AutocompleteTableContext{{Namespace: "main", Name: "widgets", Columns: []string{"id", "payload"}}},
	})
	state.SetLatestResultContext(&LatestResultContext{
		Statement:           "select * from widgets",
		OriginMode:          ModeCommand,
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
	state.SetViewerPage(1)
	state.SetSlashWizardContext(&SlashCommandWizardContext{
		Step: SlashCommandWizardStepTarget,
		Commands: []SlashCommandWizardCommand{{
			Name:        "select",
			DisplayName: "/select",
			Summary:     "compose a SELECT statement",
			Usage:       "/select <table>",
			NeedsTarget: true,
		}},
		Targets: []SlashCommandWizardTarget{{Value: "widgets", Display: "widgets"}},
	})
	state.SetPendingModeSwitch(&ModeSwitchContext{
		FromLayout:    LayoutSplit,
		ToLayout:      LayoutResultsPaneOnly,
		FromMode:      ModeCommand,
		ToMode:        ModeResultsPane,
		ResultContext: state.Interaction.LatestResult,
	})
	state.SetSelectedHistoryEntry(&HistoryEntryContext{
		SQL:            "select * from widgets",
		ConnectionName: "local",
		ExecutedAt:     stamp,
	})

	snapshot := state.Snapshot()

	state.Interaction.CurrentSQL = "mutated"
	state.Interaction.LastSubmittedSQL = "mutated"
	state.Interaction.LastAction = "mutated"
	state.Interaction.Running.Label = "mutated"
	state.Interaction.Layout = LayoutResultsPaneOnly
	state.Interaction.Running.Elapsed = 9 * time.Second
	state.Interaction.Running.SpinnerFrame = 1
	state.Interaction.SessionHistory[0].SQL = "mutated history"
	state.Interaction.HistorySearch.Filter = "mutated search"
	state.App.Current = StateError
	state.App.Reconnect.Attempt = 99
	state.App.Reconnect.Reason = "changed"
	state.Interaction.AutocompleteSchema.Tables[0].Name = "changed"
	state.Interaction.AutocompleteSchema.Tables[0].Columns[0] = "changed"
	state.Interaction.LatestResult.Statement = "mutated"
	state.Interaction.LatestResult.SelectedRows[0] = 999
	state.Interaction.SlashWizard.Commands[0].DisplayName = "/mutated"
	state.Interaction.SlashWizard.Targets[0].Display = "changed"
	state.Interaction.PendingModeSwitch.ToMode = ModeCommand
	state.Interaction.PendingModeSwitch.ResultContext.Statement = "mutated switch"
	*state.Interaction.LatestResult.RowsAffected = 99
	*state.Interaction.LatestResult.LastInsertID = 42
	state.Interaction.LatestResult.InlineRowsTruncated = false
	state.Interaction.LatestResult.PreservedResult.Columns[0].Name = "changed"
	state.Interaction.LatestResult.PreservedResult.Rows[0].Values[0].Value.([]byte)[0] = 'z'
	state.Interaction.LatestResult.InlineResult.Rows[0].Values[0].Value.([]byte)[0] = 'q'
	state.Interaction.SelectedHistoryEntry.SQL = "changed"

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

	if got, want := snapshot.Interaction.ViewerPage, 1; got != want {
		t.Fatalf("snapshot.Interaction.ViewerPage = %d, want %d", got, want)
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

	if got, want := snapshot.Interaction.SessionHistory[0].SQL, "select 1"; got != want {
		t.Fatalf("snapshot.Interaction.SessionHistory[0].SQL = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.HistorySearch.Filter, "sel"; got != want {
		t.Fatalf("snapshot.Interaction.HistorySearch.Filter = %q, want %q", got, want)
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

	if got, want := snapshot.Interaction.SlashWizard.Commands[0].DisplayName, "/select"; got != want {
		t.Fatalf("snapshot.Interaction.SlashWizard.Commands[0].DisplayName = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.SlashWizard.Targets[0].Display, "widgets"; got != want {
		t.Fatalf("snapshot.Interaction.SlashWizard.Targets[0].Display = %q, want %q", got, want)
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

	if got, want := snapshot.Interaction.PendingModeSwitch.ToMode, ModeResultsPane; got != want {
		t.Fatalf("snapshot.Interaction.PendingModeSwitch.ToMode = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.PendingModeSwitch.ResultContext.Statement, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Interaction.PendingModeSwitch.ResultContext.Statement = %q, want %q", got, want)
	}

	if got, want := string(snapshot.Interaction.PendingModeSwitch.ResultContext.PreservedResult.Rows[0].Values[0].Value.([]byte)), "abc"; got != want {
		t.Fatalf("snapshot.Interaction.PendingModeSwitch.ResultContext.PreservedResult.Rows[0].Values[0] = %q, want %q", got, want)
	}

	if got, want := snapshot.Interaction.SelectedHistoryEntry.SQL, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Interaction.SelectedHistoryEntry.SQL = %q, want %q", got, want)
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

func TestSharedAppStateSetLatestResultContextResetsViewerPage(t *testing.T) {
	state := NewSharedAppState()
	state.SetViewerPage(3)
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: []db.ResultRow{{}}}})

	if got, want := state.Interaction.ViewerPage, 0; got != want {
		t.Fatalf("state.Interaction.ViewerPage = %d, want %d", got, want)
	}
}

func TestSharedAppStateSetViewerPageClampsToAvailableRows(t *testing.T) {
	state := NewSharedAppState()
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: make([]db.ResultRow, 305)}})
	state.SetViewerPage(9)

	if got, want := state.Interaction.ViewerPage, 1; got != want {
		t.Fatalf("state.Interaction.ViewerPage = %d, want %d", got, want)
	}

	state.SetViewerPage(-2)
	if got, want := state.Interaction.ViewerPage, 0; got != want {
		t.Fatalf("state.Interaction.ViewerPage = %d, want %d", got, want)
	}
}

func TestSharedAppStateChangeViewerPageClampsToAvailableRows(t *testing.T) {
	state := NewSharedAppState()
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: make([]db.ResultRow, 605)}})

	state.ChangeViewerPage(1)
	if got, want := state.Interaction.ViewerPage, 1; got != want {
		t.Fatalf("state.Interaction.ViewerPage = %d, want %d", got, want)
	}

	state.ChangeViewerPage(10)
	if got, want := state.Interaction.ViewerPage, 2; got != want {
		t.Fatalf("state.Interaction.ViewerPage = %d, want %d", got, want)
	}

	state.ChangeViewerPage(-10)
	if got, want := state.Interaction.ViewerPage, 0; got != want {
		t.Fatalf("state.Interaction.ViewerPage = %d, want %d", got, want)
	}
}
