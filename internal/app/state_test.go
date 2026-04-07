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

	if got, want := state.Query.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}

	if got, want := state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}

	if got, want := state.Status, "Starting SQLcery."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestSharedAppStateSnapshotClonesQueryContext(t *testing.T) {
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
	state.SetRunningQueryContext(&RunningQueryContext{Label: "SQL", StartedAt: stamp, Elapsed: 1500 * time.Millisecond, SpinnerFrame: 2})
	state.SetLayout(LayoutSplit)
	state.SetActiveMode(ModeRecordViewer)
	state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1", ConnectionName: "local", ExecutedAt: stamp}})
	state.SetHistorySearchContext(&HistorySearchContext{Query: "sel", SelectedIndex: 1})
	state.SetAutocompleteSchema(&AutocompleteSchemaContext{
		Tables: []AutocompleteTableContext{{Schema: "main", Name: "widgets", Columns: []string{"id", "payload"}}},
	})
	state.SetLatestResultContext(&LatestResultContext{
		Query:               "select * from widgets",
		OriginMode:          ModeCommand,
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
		ToLayout:      LayoutViewerOnly,
		FromMode:      ModeCommand,
		ToMode:        ModeRecordViewer,
		ResultContext: state.Query.LatestResult,
	})
	state.SetSelectedHistoryEntry(&HistoryEntryContext{
		SQL:            "select * from widgets",
		ConnectionName: "local",
		ExecutedAt:     stamp,
	})

	snapshot := state.Snapshot()

	state.Query.CurrentSQL = "mutated"
	state.Query.LastSubmittedSQL = "mutated"
	state.Query.LastAction = "mutated"
	state.Query.Running.Label = "mutated"
	state.Query.Layout = LayoutViewerOnly
	state.Query.Running.Elapsed = 9 * time.Second
	state.Query.Running.SpinnerFrame = 1
	state.Query.SessionHistory[0].SQL = "mutated history"
	state.Query.HistorySearch.Query = "mutated search"
	state.App.Current = StateError
	state.App.Reconnect.Attempt = 99
	state.App.Reconnect.Reason = "changed"
	state.Query.AutocompleteSchema.Tables[0].Name = "changed"
	state.Query.AutocompleteSchema.Tables[0].Columns[0] = "changed"
	state.Query.LatestResult.Query = "mutated"
	state.Query.SlashWizard.Commands[0].DisplayName = "/mutated"
	state.Query.SlashWizard.Targets[0].Display = "changed"
	state.Query.PendingModeSwitch.ToMode = ModeCommand
	state.Query.PendingModeSwitch.ResultContext.Query = "mutated switch"
	*state.Query.LatestResult.RowsAffected = 99
	*state.Query.LatestResult.LastInsertID = 42
	state.Query.LatestResult.InlineRowsTruncated = false
	state.Query.LatestResult.PreservedResult.Columns[0].Name = "changed"
	state.Query.LatestResult.PreservedResult.Rows[0].Values[0].Value.([]byte)[0] = 'z'
	state.Query.LatestResult.InlineResult.Rows[0].Values[0].Value.([]byte)[0] = 'q'
	state.Query.SelectedHistoryEntry.SQL = "changed"

	if got, want := snapshot.App.Current, StateReconnect; got != want {
		t.Fatalf("snapshot.App.Current = %q, want %q", got, want)
	}

	if got, want := snapshot.App.Reconnect.Attempt, 2; got != want {
		t.Fatalf("snapshot.App.Reconnect.Attempt = %d, want %d", got, want)
	}

	if got, want := snapshot.App.Reconnect.Reason, "connection dropped"; got != want {
		t.Fatalf("snapshot.App.Reconnect.Reason = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.CurrentSQL, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Query.CurrentSQL = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.LastSubmittedSQL, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Query.LastSubmittedSQL = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.LastAction, "submit"; got != want {
		t.Fatalf("snapshot.Query.LastAction = %q, want %q", got, want)
	}

	if snapshot.Query.Running == nil {
		t.Fatal("snapshot.Query.Running = nil, want running context")
	}

	if got, want := snapshot.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("snapshot.Query.Layout = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.ViewerPage, 1; got != want {
		t.Fatalf("snapshot.Query.ViewerPage = %d, want %d", got, want)
	}

	if got, want := snapshot.Query.Running.Label, "SQL"; got != want {
		t.Fatalf("snapshot.Query.Running.Label = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.Running.Elapsed, 1500*time.Millisecond; got != want {
		t.Fatalf("snapshot.Query.Running.Elapsed = %v, want %v", got, want)
	}

	if got, want := snapshot.Query.Running.SpinnerFrame, 2; got != want {
		t.Fatalf("snapshot.Query.Running.SpinnerFrame = %d, want %d", got, want)
	}

	if got, want := snapshot.Query.SessionHistory[0].SQL, "select 1"; got != want {
		t.Fatalf("snapshot.Query.SessionHistory[0].SQL = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.HistorySearch.Query, "sel"; got != want {
		t.Fatalf("snapshot.Query.HistorySearch.Query = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.AutocompleteSchema.Tables[0].Name, "widgets"; got != want {
		t.Fatalf("snapshot.Query.AutocompleteSchema.Tables[0].Name = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.AutocompleteSchema.Tables[0].Columns[0], "id"; got != want {
		t.Fatalf("snapshot.Query.AutocompleteSchema.Tables[0].Columns[0] = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.LatestResult.Query, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Query.LatestResult.Query = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.SlashWizard.Commands[0].DisplayName, "/select"; got != want {
		t.Fatalf("snapshot.Query.SlashWizard.Commands[0].DisplayName = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.SlashWizard.Targets[0].Display, "widgets"; got != want {
		t.Fatalf("snapshot.Query.SlashWizard.Targets[0].Display = %q, want %q", got, want)
	}

	if snapshot.Query.LatestResult.RowsAffected == nil || *snapshot.Query.LatestResult.RowsAffected != 3 {
		t.Fatalf("snapshot.Query.LatestResult.RowsAffected = %#v, want 3", snapshot.Query.LatestResult.RowsAffected)
	}

	if snapshot.Query.LatestResult.LastInsertID == nil || *snapshot.Query.LatestResult.LastInsertID != 9 {
		t.Fatalf("snapshot.Query.LatestResult.LastInsertID = %#v, want 9", snapshot.Query.LatestResult.LastInsertID)
	}

	if !snapshot.Query.LatestResult.InlineRowsTruncated {
		t.Fatal("snapshot.Query.LatestResult.InlineRowsTruncated = false, want true")
	}

	if got, want := snapshot.Query.LatestResult.PreservedResult.Columns[0].Name, "payload"; got != want {
		t.Fatalf("snapshot.Query.LatestResult.PreservedResult.Columns[0].Name = %q, want %q", got, want)
	}

	if got, want := string(snapshot.Query.LatestResult.PreservedResult.Rows[0].Values[0].Value.([]byte)), "abc"; got != want {
		t.Fatalf("snapshot.Query.LatestResult.PreservedResult.Rows[0].Values[0] = %q, want %q", got, want)
	}

	if got, want := string(snapshot.Query.LatestResult.InlineResult.Rows[0].Values[0].Value.([]byte)), "xyz"; got != want {
		t.Fatalf("snapshot.Query.LatestResult.InlineResult.Rows[0].Values[0] = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.PendingModeSwitch.ToMode, ModeRecordViewer; got != want {
		t.Fatalf("snapshot.Query.PendingModeSwitch.ToMode = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.PendingModeSwitch.ResultContext.Query, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Query.PendingModeSwitch.ResultContext.Query = %q, want %q", got, want)
	}

	if got, want := string(snapshot.Query.PendingModeSwitch.ResultContext.PreservedResult.Rows[0].Values[0].Value.([]byte)), "abc"; got != want {
		t.Fatalf("snapshot.Query.PendingModeSwitch.ResultContext.PreservedResult.Rows[0].Values[0] = %q, want %q", got, want)
	}

	if got, want := snapshot.Query.SelectedHistoryEntry.SQL, "select * from widgets"; got != want {
		t.Fatalf("snapshot.Query.SelectedHistoryEntry.SQL = %q, want %q", got, want)
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

	state.SetError("dial tcp failed", "Connection failed.")
	if got, want := state.App.Current, StateError; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}

	if got, want := state.App.Error, "dial tcp failed"; got != want {
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

	if got, want := state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
}

func TestSharedAppStateSetViewerPageClampsToAvailableRows(t *testing.T) {
	state := NewSharedAppState()
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: make([]db.ResultRow, 305)}})
	state.SetViewerPage(9)

	if got, want := state.Query.ViewerPage, 1; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	state.SetViewerPage(-2)
	if got, want := state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
}

func TestSharedAppStateChangeViewerPageClampsToAvailableRows(t *testing.T) {
	state := NewSharedAppState()
	state.SetLatestResultContext(&LatestResultContext{PreservedResult: &db.ResultSet{Rows: make([]db.ResultRow, 605)}})

	state.ChangeViewerPage(1)
	if got, want := state.Query.ViewerPage, 1; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	state.ChangeViewerPage(10)
	if got, want := state.Query.ViewerPage, 2; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	state.ChangeViewerPage(-10)
	if got, want := state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
}
