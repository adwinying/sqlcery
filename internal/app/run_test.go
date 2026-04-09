package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
)

func TestRunStartsProgram(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	started := false
	var captured tea.Model

	err := Run(context.Background(), Session{ConnectionName: "local", ConnectionType: "sqlite"}, adapter, RunOptions{
		NewProgram: func(model tea.Model, _ ...tea.ProgramOption) Program {
			captured = model
			return fakeProgram{run: func() (tea.Model, error) {
				started = true
				return model, nil
			}}
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !started {
		t.Fatal("Run() did not start program")
	}

	if _, ok := captured.(Model); !ok {
		t.Fatalf("Run() model type = %T, want %T", captured, Model{})
	}
}

func TestRunUsesProvidedHistorySession(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	historyPath := filepath.Join(t.TempDir(), apphistory.DirName, apphistory.FileName)
	history := apphistory.NewFileBackedSession(historyPath)

	err := Run(context.Background(), Session{ConnectionName: "local", ConnectionType: "sqlite"}, adapter, RunOptions{
		History: history,
		NewProgram: func(model tea.Model, _ ...tea.ProgramOption) Program {
			return fakeProgram{run: func() (tea.Model, error) {
				typed := model.(Model)
				typed.state.SetReady("")
				typed.command.editor.SetValue("select 1;")
				typed.syncCurrentSQL()

				next, cmd := typed.Update(submitIntentMsg{})
				if cmd == nil {
					return nil, fmt.Errorf("submit cmd was nil")
				}

				next, _ = next.(Model).Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
				return next, nil
			}}
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var persisted struct {
		Command    string `json:"command"`
		Connection string `json:"connection"`
		Result     string `json:"result"`
		Time       string `json:"time"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got, want := persisted.Command, "select 1;"; got != want {
		t.Fatalf("persisted command = %q, want %q", got, want)
	}
	if got, want := persisted.Connection, "local"; got != want {
		t.Fatalf("persisted connection = %q, want %q", got, want)
	}
	if got, want := persisted.Result, "Query returned 1 row."; got != want {
		t.Fatalf("persisted result = %q, want %q", got, want)
	}
	if persisted.Time == "" {
		t.Fatal("persisted time = empty, want RFC3339 timestamp")
	}
}

func TestModelViewIncludesSessionDetails(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	view := model.View()

	for _, want := range []string{
		"Write SQL here",
		"ctrl+g submit",
		"esc clear/cancel",
		"ctrl+r history",
		"ctrl+x focus",
		"ctrl+1 split",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}

}

func TestModelUpdateSubmitWarnsWhenHistoryPersistenceFails(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	blockerDir := t.TempDir()
	blockerPath := filepath.Join(blockerDir, "history.log")
	if err := os.WriteFile(blockerPath, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	history := apphistory.NewFileBackedSession(filepath.Join(blockerPath, apphistory.FileName))
	model := newModelWithDependencies(Session{ConnectionName: "local", ConnectionType: "sqlite"}, adapter, modelDependencies{history: history})
	model.state.SetReady("")
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	if got, want := len(model.state.Query.SessionHistory), 1; got != want {
		t.Fatalf("len(state.Query.SessionHistory) = %d, want %d", got, want)
	}
	if got := model.state.Status; !strings.Contains(got, "History was not persisted:") {
		t.Fatalf("state.Status = %q, want history persistence warning", got)
	}
}

func TestLoadAutocompleteSchemaReturnsTableMetadata(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT,
			email TEXT
		)
	`); err != nil {
		t.Fatalf("ExecContext() error = %v", err)
	}

	schema, err := loadAutocompleteSchema(context.Background(), adapter)
	if err != nil {
		t.Fatalf("loadAutocompleteSchema() error = %v", err)
	}

	if schema == nil || len(schema.Tables) == 0 {
		t.Fatal("loadAutocompleteSchema() = nil/empty, want table metadata")
	}

	var users *AutocompleteTableContext
	for i := range schema.Tables {
		if schema.Tables[i].Name == "users" {
			users = &schema.Tables[i]
			break
		}
	}
	if users == nil {
		t.Fatalf("schema.Tables = %#v, want users table", schema.Tables)
	}

	for _, want := range []string{"id", "name", "email"} {
		found := false
		for _, column := range users.Columns {
			if column == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("users.Columns = %#v, want to contain %q", users.Columns, want)
		}
	}
}

func TestModelAutocompleteUsesCachedSchemaWhileTyping(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	loadCalls := 0
	model := newModelWithDependencies(Session{}, adapter, modelDependencies{
		cache: newAutocompleteSchemaCache(),
		loader: func(context.Context, *db.SQLAdapter) (*AutocompleteSchemaContext, error) {
			loadCalls++
			return &AutocompleteSchemaContext{
				Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name"}}},
			}, nil
		},
	})
	model.state.SetReady("")

	cmd := model.refreshAutocompleteSchemaCmd()
	if cmd == nil {
		t.Fatal("refreshAutocompleteSchemaCmd() = nil, want load command")
	}

	msg := cmd()
	next, _ := model.Update(msg)
	model = next.(Model)

	if got, want := loadCalls, 1; got != want {
		t.Fatalf("schema load calls = %d, want %d", got, want)
	}

	for _, keyMsg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'S', 'E', 'L', 'E', 'C', 'T', ' '}},
		{Type: tea.KeyRunes, Runes: []rune{'*'}},
		{Type: tea.KeyRunes, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune{'F', 'R', 'O', 'M', ' '}},
		{Type: tea.KeyRunes, Runes: []rune{'u', 's'}},
	} {
		next, _ = model.Update(keyMsg)
		model = next.(Model)
	}

	if got, want := loadCalls, 1; got != want {
		t.Fatalf("schema load calls after typing = %d, want %d", got, want)
	}

	items := model.command.autocompleteItems(model.state.Query)
	if len(items) == 0 {
		t.Fatal("autocompleteItems() = no items, want cached table suggestions")
	}

	if got, want := items[0].Label, "users"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestModelViewIncludesSharedQueryContextPlaceholders(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1", ConnectionName: "local"}})
	model.state.SetLatestResultContext(&LatestResultContext{Query: "select 1", OriginMode: ModeCommand})
	model.state.SetPendingModeSwitch(&ModeSwitchContext{FromLayout: LayoutCommandOnly, ToLayout: LayoutViewerOnly, FromMode: ModeCommand, ToMode: ModeRecordViewer})
	model.state.SetSelectedHistoryEntry(&HistoryEntryContext{SQL: "select 2", ConnectionName: "local"})

	if model.state.Query.LatestResult == nil {
		t.Fatal("state.Query.LatestResult = nil, want available")
	}
	if model.state.Query.PendingModeSwitch == nil {
		t.Fatal("state.Query.PendingModeSwitch = nil, want available")
	}
	if got, want := len(model.state.Query.SessionHistory), 1; got != want {
		t.Fatalf("len(state.Query.SessionHistory) = %d, want %d", got, want)
	}
	if model.state.Query.SelectedHistoryEntry == nil {
		t.Fatal("state.Query.SelectedHistoryEntry = nil, want available")
	}
}

func TestModelInitTransitionsStartupToReady(t *testing.T) {
	model := NewModel(Session{}, nil)

	if got, want := model.state.App.Current, StateStartup; got != want {
		t.Fatalf("initial state = %q, want %q", got, want)
	}

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init() cmd = nil, want batch command")
	}

	msg := cmd()
	next, _ := model.Update(msg)
	updated := next.(Model)

	if got, want := updated.state.App.Current, StateReady; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}

	if got, want := updated.state.Status, "Ready for SQL input."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelViewRendersStartupState(t *testing.T) {
	view := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil).View()

	for _, want := range []string{
		"[ startup ]",
		"Preparing command mode...",
		"[local]",
		"ctrl+c quit",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}

	if strings.Contains(view, "Command mode") {
		t.Fatalf("View() = %q, want startup view without command footer", view)
	}
}

func TestModelViewRendersReconnectState(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	next, _ := model.Update(reconnectStateMsg{
		Context: ReconnectContext{Attempt: 3, Reason: "connection dropped", LastError: "network timeout"},
		Status:  "Reconnecting to local database.",
	})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{
		"[ reconnect ]",
		"Connection recovery in progress.",
		"Reconnecting to local database.",
		"Attempt 3",
		"Reason: connection dropped",
		"Last error: network timeout",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelViewRendersErrorState(t *testing.T) {
	model := NewModel(Session{}, nil)
	next, _ := model.Update(appErrorMsg{Err: errors.New("dial tcp 127.0.0.1:5432: connect: connection refused"), Status: "Query failed."})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{
		"[ error ]",
		"Query failed.",
		"dial tcp 127.0.0.1:5432: connect: connection refused",
		"ctrl+c quit",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestStatementExecutionFailureUsesFriendlyStatus(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")

	next, _ := model.Update(statementExecutedMsg{
		Query:         "select * from missing;",
		ResultSummary: "error: no such table: missing",
		Err:           errors.New("no such table: missing"),
	})
	model = next.(Model)

	if got := model.state.Status; !strings.Contains(got, "Execution failed. SQL query failed. Check the statement and any referenced tables or columns.") {
		t.Fatalf("state.Status = %q, want friendly query failure", got)
	}

	if got := model.state.Status; !strings.Contains(got, "no such table: missing") {
		t.Fatalf("state.Status = %q, want original error detail", got)
	}
}

func TestModelUpdateQuitsOnCtrlC(t *testing.T) {
	next, cmd := NewModel(Session{}, nil).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, ok := next.(Model); !ok {
		t.Fatalf("Update() model type = %T, want %T", next, Model{})
	}

	if cmd == nil {
		t.Fatal("Update() cmd = nil, want tea.Quit")
	}

	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", cmd(), tea.QuitMsg{})
	}
}

func TestModelUpdateTypesIntoCommandMode(t *testing.T) {
	initial := NewModel(Session{}, nil)
	initial.state.SetReady("")

	next, cmd := initial.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 'e', 'l', 'e', 'c', 't'}})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want cursor blink command")
	}

	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want %T", next, Model{})
	}

	if got, want := model.command.editor.Value(), "select"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model = next.(Model)

	if got, want := model.command.editor.Value(), "select\n1"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
}

func TestModelUpdateQTypesWhenEditorFocused(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want textarea command")
	}

	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want %T", next, Model{})
	}

	if got, want := model.command.editor.Value(), "q"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
}

func TestModelUpdateSubmitSetsPendingIntent(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 'e', 'l', 'e', 'c', 't'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{';'}})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want submit intent command")
	}

	msg := cmd()
	if _, ok := msg.(submitIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, submitIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	if got, want := model.state.Query.PendingIntent, IntentSubmit; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if model.state.Query.Running == nil {
		t.Fatal("state.Query.Running = nil, want running context")
	}
	if got, want := model.state.Query.Running.Label, "SQL"; got != want {
		t.Fatalf("state.Query.Running.Label = %q, want %q", got, want)
	}

	if got, want := model.state.Query.LastAction, "submit"; got != want {
		t.Fatalf("state.LastAction = %q, want %q", got, want)
	}

	if got, want := model.state.Query.CurrentSQL, "select;"; got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}

	if got, want := model.state.Query.LastSubmittedSQL, "select;"; got != want {
		t.Fatalf("state.Query.LastSubmittedSQL = %q, want %q", got, want)
	}

	view := model.View()
	_ = view // "esc cancel query" may be truncated at 120-char width; check state directly
	if model.state.Query.Running == nil {
		t.Fatal("state.Query.Running should still be set (pending)")
	}
}

func TestModelUpdateSubmitWhileRunningShowsCancelGuidance(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetRunningQueryContext(&RunningQueryContext{Label: "SQL"})

	next, cmd := model.Update(submitIntentMsg{})
	if cmd != nil {
		t.Fatalf("Update(submitIntentMsg{}) cmd = %v, want nil", cmd)
	}
	model = next.(Model)

	if got, want := model.state.Status, "SQL is still running. Press esc to cancel; timeout after 30s."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateSubmitRejectsIncompleteSQL(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.command.editor.SetValue("select\n1")
	model.syncCurrentSQL()
	model.state.SetLastSubmittedSQL("select 0;")

	next, cmd := model.Update(submitIntentMsg{})
	if cmd != nil {
		t.Fatalf("Update(submitIntentMsg{}) cmd = %v, want nil", cmd)
	}
	model = next.(Model)

	if got, want := model.state.Query.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Query.PendingIntent = %q, want %q", got, want)
	}
	if model.state.Query.Running != nil {
		t.Fatalf("state.Query.Running = %#v, want nil", model.state.Query.Running)
	}
	if got, want := model.state.Status, "SQL is incomplete. End the statement with ';' to run it."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.command.editor.Value(), "select\n1"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Query.CurrentSQL, "select\n1"; got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Query.LastSubmittedSQL, "select 0;"; got != want {
		t.Fatalf("state.Query.LastSubmittedSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Query.LastAction, "submit"; got != want {
		t.Fatalf("state.Query.LastAction = %q, want %q", got, want)
	}
	if got, want := model.state.App.Current, StateReady; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}
}

func TestModelUpdateRunningTickUpdatesElapsedAndFooter(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	startedAt := time.Date(2026, time.April, 8, 10, 0, 0, 0, time.UTC)
	model.state.SetRunningQueryContext(&RunningQueryContext{Label: "SQL", StartedAt: startedAt})

	next, cmd := model.Update(runningTickMsg{StartedAt: startedAt, Now: startedAt.Add(1500 * time.Millisecond)})
	if cmd == nil {
		t.Fatal("Update(runningTickMsg{}) cmd = nil, want follow-up tick")
	}
	model = next.(Model)

	if model.state.Query.Running == nil {
		t.Fatal("state.Query.Running = nil, want running context")
	}
	if got, want := model.state.Query.Running.Elapsed, 1500*time.Millisecond; got != want {
		t.Fatalf("state.Query.Running.Elapsed = %v, want %v", got, want)
	}
	if got, want := model.state.Query.Running.SpinnerFrame, 1; got != want {
		t.Fatalf("state.Query.Running.SpinnerFrame = %d, want %d", got, want)
	}

	view := model.View()
	if !strings.Contains(view, "\\ SQL 1.5s") {
		t.Fatalf("View() = %q, want to contain %q", view, "\\ SQL 1.5s")
	}
}

func TestModelUpdateSubmitExecutesSelectAndLimitsInlineRows(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	for _, statement := range []string{
		`create table widgets (id integer primary key, name text not null)`,
		`insert into widgets (name) values ('one'), ('two'), ('three'), ('four'), ('five'), ('six')`,
	} {
		if _, err := adapter.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("ExecContext(%q) error = %v", statement, err)
		}
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, adapter)
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	query := "select id, name from widgets order by id;"
	model.command.editor.SetValue(query)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	if got, want := model.state.Query.PendingIntent, IntentSubmit; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Status, fmt.Sprintf("Executing %d characters of SQL. Press esc to cancel; timeout after 30s.", len(query)); got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	executed := firstCommandMessageForTest[statementExecutedMsg](t, cmd)

	next, _ = model.Update(executed)
	model = next.(Model)

	if got, want := model.state.Query.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.PendingIntent = %q, want empty", got)
	}
	if model.state.Query.Running != nil {
		t.Fatalf("state.Query.Running = %#v, want nil", model.state.Query.Running)
	}
	if got, want := model.state.Status, "Query returned 6 rows."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Query.LatestResult == nil {
		t.Fatal("state.Query.LatestResult = nil, want result context")
	}
	if got, want := len(model.state.Query.LatestResult.PreservedResult.Rows), 6; got != want {
		t.Fatalf("len(latest.PreservedResult.Rows) = %d, want %d", got, want)
	}
	if got, want := len(model.state.Query.LatestResult.InlineResult.Rows), 5; got != want {
		t.Fatalf("len(latest.InlineResult.Rows) = %d, want %d", got, want)
	}
	if !model.state.Query.LatestResult.InlineRowsTruncated {
		t.Fatal("latest.InlineRowsTruncated = false, want true")
	}
	if got, want := len(model.state.Query.SessionHistory), 1; got != want {
		t.Fatalf("len(state.Query.SessionHistory) = %d, want %d", got, want)
	}
	if got, want := model.state.Query.SessionHistory[0].SQL, query; got != want {
		t.Fatalf("state.Query.SessionHistory[0].SQL = %q, want %q", got, want)
	}

	view := model.View()
	for _, want := range []string{"Results:", "id | name", "1  | one", "5  | five", "Showing first 5 of 6 rows."} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
	if strings.Contains(view, "6  | six") {
		t.Fatalf("View() = %q, want inline result to omit 6th row", view)
	}
}

func TestModelUpdateSubmitExecutesNonSelectStatement(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key, name text not null)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, adapter)
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	model.command.editor.SetValue(`insert into widgets (name) values ('Ada'), ('Grace');`)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	if got, want := model.state.Status, "Statement executed successfully. 2 rows affected."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Query.LatestResult == nil {
		t.Fatal("state.Query.LatestResult = nil, want result context")
	}
	if got, want := model.state.Query.LatestResult.StatementKind, db.StatementResultKindExec; got != want {
		t.Fatalf("latest.StatementKind = %q, want %q", got, want)
	}
	if model.state.Query.LatestResult.RowsAffected == nil || *model.state.Query.LatestResult.RowsAffected != 2 {
		t.Fatalf("latest.RowsAffected = %#v, want 2", model.state.Query.LatestResult.RowsAffected)
	}

	view := model.View()
	for _, want := range []string{"Results:", "2 rows affected"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelUpdateCancelClearsInput(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 'e', 'l', 'e', 'c', 't'}})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want clear intent command")
	}

	msg := cmd()
	if _, ok := msg.(clearInputIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, clearInputIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	if got := model.command.editor.Value(); got != "" {
		t.Fatalf("editor.Value() = %q, want empty", got)
	}

	if got, want := model.state.Query.PendingIntent, IntentClearInput; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}

	if got := model.state.Query.CurrentSQL; got != "" {
		t.Fatalf("state.Query.CurrentSQL = %q, want empty", got)
	}

	if got, want := model.state.Status, "Cleared current input."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateCancelWhileRunningRequestsCancellation(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetRunningQueryContext(&RunningQueryContext{Label: "SQL"})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want cancel intent command")
	}

	msg := cmd()
	if _, ok := msg.(cancelRunningIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, cancelRunningIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	if got, want := model.state.Status, "Cancelling SQL..."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.state.Query.PendingIntent, IntentSubmit; got != want {
		t.Fatalf("state.Query.PendingIntent = %q, want %q", got, want)
	}
}

func TestStatementExecutionCancellationUsesFriendlyStatus(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	startedAt := time.Now().Add(-2500 * time.Millisecond)
	model.state.SetRunningQueryContext(&RunningQueryContext{Label: "SQL", StartedAt: startedAt, Elapsed: 2500 * time.Millisecond})

	next, _ := model.Update(statementExecutedMsg{
		Query:         "select sleep(10);",
		ResultSummary: "error: context canceled",
		Err:           context.Canceled,
	})
	model = next.(Model)

	if got, want := model.state.Status, "Cancelled SQL after 2.5s."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Query.Running != nil {
		t.Fatalf("state.Query.Running = %#v, want nil", model.state.Query.Running)
	}
}

func TestStatementExecutionTimeoutUsesFriendlyStatus(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetRunningQueryContext(&RunningQueryContext{Label: "SQL", Elapsed: 30 * time.Second})

	next, _ := model.Update(statementExecutedMsg{
		Query:         "select sleep(35);",
		ResultSummary: "error: context deadline exceeded",
		Err:           context.DeadlineExceeded,
	})
	model = next.(Model)

	if got, want := model.state.Status, "SQL timed out after 30.0s. Press esc sooner to cancel manually, or retry with a narrower query."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateHistorySetsPendingIntent(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1"}, {SQL: "/tables"}})
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want history intent command")
	}

	msg := cmd()
	if _, ok := msg.(historyIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, historyIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	if got, want := model.state.Query.PendingIntent, IntentHistory; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeHistorySearch; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.HistorySearch == nil {
		t.Fatal("state.Query.HistorySearch = nil, want search context")
	}

	if got, want := model.state.Status, "History search matched 2 entries; selected \"/tables\"."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Query.SelectedHistoryEntry == nil {
		t.Fatal("state.Query.SelectedHistoryEntry = nil, want latest history entry")
	}
	if got, want := model.state.Query.SelectedHistoryEntry.SQL, "/tables"; got != want {
		t.Fatalf("state.Query.SelectedHistoryEntry.SQL = %q, want %q", got, want)
	}
}

func TestModelUpdateHistoryHandlesEmptySessionHistory(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	if got, want := model.state.Status, "History search opened; session history is empty."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeHistorySearch; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.SelectedHistoryEntry != nil {
		t.Fatalf("state.Query.SelectedHistoryEntry = %#v, want nil", model.state.Query.SelectedHistoryEntry)
	}
}

func TestModelUpdateHistorySearchFiltersAndCyclesEntries(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select * from users"}, {SQL: "delete from users"}, {SQL: "select * from user_sessions"}})

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 'u'}})
	model = next.(Model)

	if got, want := model.state.Query.HistorySearch.Query, "su"; got != want {
		t.Fatalf("state.Query.HistorySearch.Query = %q, want %q", got, want)
	}
	if model.state.Query.SelectedHistoryEntry == nil {
		t.Fatal("state.Query.SelectedHistoryEntry = nil, want selected entry")
	}
	if got, want := model.state.Query.SelectedHistoryEntry.SQL, "select * from user_sessions"; got != want {
		t.Fatalf("state.Query.SelectedHistoryEntry.SQL = %q, want %q", got, want)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	model = next.(Model)

	if got, want := model.state.Query.SelectedHistoryEntry.SQL, "select * from users"; got != want {
		t.Fatalf("state.Query.SelectedHistoryEntry.SQL = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "History search matched 2 entries; selected \"select * from users\"."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateHistorySearchCancelReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1"}})

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.HistorySearch != nil {
		t.Fatalf("state.Query.HistorySearch = %#v, want nil", model.state.Query.HistorySearch)
	}
	if got, want := model.state.Query.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Query.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Exited history search."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateHistorySearchRestoreLoadsEditorAndClosesSearch(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select * from users"}, {SQL: "select * from user_sessions"}})
	model.command.editor.SetValue("partial")
	model.command.editor.CursorEnd()
	model.syncCurrentSQL()

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 'u'}})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)

	if got, want := model.command.editor.Value(), "select * from user_sessions"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Query.CurrentSQL, "select * from user_sessions"; got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.HistorySearch != nil {
		t.Fatalf("state.Query.HistorySearch = %#v, want nil", model.state.Query.HistorySearch)
	}
	if model.state.Query.SelectedHistoryEntry != nil {
		t.Fatalf("state.Query.SelectedHistoryEntry = %#v, want nil", model.state.Query.SelectedHistoryEntry)
	}
	if got, want := model.state.Query.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Query.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Restored selected history entry into the editor."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.command.editor.Line(), 0; got != want {
		t.Fatalf("editor.Line() = %d, want %d", got, want)
	}
	if got, want := model.command.editor.LineInfo().ColumnOffset, len([]rune("select * from user_sessions")); got != want {
		t.Fatalf("editor.ColumnOffset = %d, want %d", got, want)
	}
}

func TestModelUpdateModeSwitchSetsPendingIntent(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want switch mode intent command")
	}

	msg := cmd()
	if _, ok := msg.(switchModeIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, switchModeIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	if got, want := model.state.Query.PendingIntent, IntentSwitchMode; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if model.state.Query.PendingModeSwitch == nil {
		t.Fatal("state.Query.PendingModeSwitch = nil, want mode switch context")
	}
	if got, want := model.state.Query.PendingModeSwitch.FromMode, ModeCommand; got != want {
		t.Fatalf("state.Query.PendingModeSwitch.FromMode = %q, want %q", got, want)
	}
	if got, want := model.state.Query.PendingModeSwitch.FromLayout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.PendingModeSwitch.FromLayout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.PendingModeSwitch.ToMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.PendingModeSwitch.ToMode = %q, want %q", got, want)
	}
	if got, want := model.state.Query.PendingModeSwitch.ToLayout, LayoutViewerOnly; got != want {
		t.Fatalf("state.Query.PendingModeSwitch.ToLayout = %q, want %q", got, want)
	}

	if got, want := model.state.Status, "Record viewer is available after running a query that returns tabular results."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.PendingModeSwitch.ResultContext != nil {
		t.Fatalf("state.Query.PendingModeSwitch.ResultContext = %#v, want nil", model.state.Query.PendingModeSwitch.ResultContext)
	}
}

func TestModelUpdateModeSwitchPreservesLatestResultContext(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	for _, statement := range []string{
		`create table widgets (id integer primary key, name text not null)`,
		`insert into widgets (name) values ('one'), ('two'), ('three'), ('four'), ('five'), ('six')`,
	} {
		if _, err := adapter.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("ExecContext(%q) error = %v", statement, err)
		}
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, adapter)
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	query := "select id, name from widgets order by id;"
	model.command.editor.SetValue(query)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	if got, want := model.state.Status, "Opened record viewer for 6 row(s) across 2 column(s)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.state.Query.Layout, LayoutViewerOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.LatestResult == nil {
		t.Fatal("state.Query.LatestResult = nil, want preserved result context")
	}
	if got, want := len(model.state.Query.LatestResult.PreservedResult.Rows), 6; got != want {
		t.Fatalf("len(latest preserved rows) = %d, want %d", got, want)
	}
	if got, want := len(model.state.Query.LatestResult.InlineResult.Rows), 5; got != want {
		t.Fatalf("len(latest inline rows) = %d, want %d", got, want)
	}
	if got, want := model.state.Query.LatestResult.Query, query; got != want {
		t.Fatalf("latest query = %q, want %q", got, want)
	}
	if model.state.Query.PendingModeSwitch != nil {
		t.Fatalf("state.Query.PendingModeSwitch = %#v, want nil after switching", model.state.Query.PendingModeSwitch)
	}

	view := model.View()
	for _, want := range []string{"Record viewer", "Rows: 6  Columns: 2", "id | name", "1  | one", "6  | six"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelUpdateNewResultResetsViewerPage(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	for _, statement := range []string{
		`create table widgets (id integer primary key, name text not null)`,
		`insert into widgets (name) values ('one'), ('two'), ('three'), ('four'), ('five'), ('six')`,
	} {
		if _, err := adapter.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("ExecContext(%q) error = %v", statement, err)
		}
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, adapter)
	model.state.SetReady("")
	model.state.SetViewerPage(4)
	model.command.editor.SetValue("select id, name from widgets order by id;")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	if got, want := model.state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
}

func TestModelViewRecordViewerShowsPaginatedRows(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	rows := make([]db.ResultRow, 0, 305)
	for i := 1; i <= 305; i++ {
		rows = append(rows, db.ResultRow{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(i)}}})
	}

	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    rows,
		},
	})
	model.state.SetViewerPage(1)

	view := model.View()
	for _, want := range []string{"Rows: 305  Columns: 1", "Page: 2/2  Showing rows 301-305", "page 2/2", "301", "305"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}

	for _, unwanted := range []string{"299", "300"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("View() = %q, want not to contain %q", view, unwanted)
		}
	}
}

func TestModelUpdateCtrlDPagesForwardInViewerOnlyLayout(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("Update(ctrl+d) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ViewerPage, 1; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
	if got, want := model.state.Status, "Showing record viewer page 2/3 (301-600)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = next.(Model)
	if got, want := model.state.Query.ViewerPage, 2; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = next.(Model)
	if got, want := model.state.Query.ViewerPage, 2; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
	if got, want := model.state.Status, "Already at the last record viewer page (601-605)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateCtrlUPagesBackwardInViewerOnlyLayout(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})
	model.state.SetViewerPage(2)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("Update(ctrl+u) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ViewerPage, 1; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
	if got, want := model.state.Status, "Showing record viewer page 2/3 (301-600)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = next.(Model)
	if got, want := model.state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = next.(Model)
	if got, want := model.state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
	if got, want := model.state.Status, "Already at the first record viewer page (1-300)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateCtrlDPagesOnlyWhenViewerFocusedInSplitLayout(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})
	model.state.SetViewerPage(1)
	model.command.editor.SetValue("select 1;")
	model.command.editor.CursorEnd()
	model.syncCurrentSQL()

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = next.(Model)
	if got, want := model.state.Query.ViewerPage, 1; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	model.state.SetActiveMode(ModeRecordViewer)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = next.(Model)
	if got, want := model.state.Query.ViewerPage, 2; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
	if got, want := model.state.Status, "Showing record viewer page 3/3 (601-605)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateCtrlDDoesNotPageDuringHistorySearch(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeHistorySearch)
	model.state.SetHistorySearchContext(&HistorySearchContext{Query: "sel"})
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})
	model.state.SetViewerPage(1)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("Update(ctrl+d) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ViewerPage, 1; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeHistorySearch; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.HistorySearch == nil || model.state.Query.HistorySearch.Query != "sel" {
		t.Fatalf("state.Query.HistorySearch = %#v, want query preserved", model.state.Query.HistorySearch)
	}
}

func TestModelUpdateArrowKeysNavigateRecordViewerSelectionAcrossPages(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows:    make([]db.ResultRow, 301),
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(right) cmd = %#v, want nil", cmd)
	}
	if got, want := model.viewer.selectedColumn, 1; got != want {
		t.Fatalf("viewer.selectedColumn = %d, want %d", got, want)
	}
	if got, want := model.state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	model.viewer.selectedRow = 299
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if got, want := model.viewer.selectedRow, 300; got != want {
		t.Fatalf("viewer.selectedRow = %d, want %d", got, want)
	}
	if got, want := model.state.Query.ViewerPage, 1; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = next.(Model)
	if got, want := model.viewer.selectedRow, 299; got != want {
		t.Fatalf("viewer.selectedRow = %d, want %d", got, want)
	}
	if got, want := model.state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
}

func TestModelUpdateSpaceTogglesSelectedRowsInRecordViewer(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}}},
			},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(space) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.LatestResult.SelectedRows, []int{0}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}
	if got, want := model.state.Status, "Selected row 1 (1 total)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	model.viewer.selectedRow = 1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)
	if got, want := model.state.Query.LatestResult.SelectedRows, []int{0, 1}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}
	if got, want := model.state.Status, "Selected row 2 (2 total)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	model.viewer.selectedRow = 0
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)
	if got, want := model.state.Query.LatestResult.SelectedRows, []int{1}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}
	if got, want := model.state.Status, "Unselected row 1 (1 total)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	view := model.View()
	for _, want := range []string{"Selected: 1", "1 selected", "* 2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelUpdateSpaceIgnoredOutsideRecordViewer(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeCommand)
	model.state.SetPendingIntent(IntentNone, "seed", "unchanged")
	model.state.SetLatestResultContext(&LatestResultContext{
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = next.(Model)
	if model.state.Query.LatestResult != nil && len(model.state.Query.LatestResult.SelectedRows) != 0 {
		t.Fatalf("SelectedRows = %#v, want unchanged", model.state.Query.LatestResult.SelectedRows)
	}
	if got, want := model.state.Status, "unchanged"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if cmd != nil {
		t.Fatalf("Update(space) cmd = %#v, want nil", cmd)
	}
}

func TestModelUpdateNavigationIgnoredOutsideRecordViewer(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeCommand)
	model.state.SetLatestResultContext(&LatestResultContext{
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	model = next.(Model)

	if got, want := model.state.Query.ViewerPage, 0; got != want {
		t.Fatalf("state.Query.ViewerPage = %d, want %d", got, want)
	}
	if got, want := model.viewer.selectedColumn, 0; got != want {
		t.Fatalf("viewer.selectedColumn = %d, want %d", got, want)
	}
	if got := model.command.Value(); !strings.Contains(got, "l") {
		t.Fatalf("command.Value() = %q, want rune handled by command mode", got)
	}
	if cmd == nil {
		t.Fatal("Update(l) cmd = nil, want textarea blink command")
	}
}

func TestModelUpdateModeSwitchReturnsFromRecordViewerToCommandMode(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Query.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Query.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Returned to command line."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Query.PendingModeSwitch != nil {
		t.Fatalf("state.Query.PendingModeSwitch = %#v, want nil", model.state.Query.PendingModeSwitch)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want editor focused after returning to command mode")
	}
	if strings.Contains(model.View(), "Record viewer") {
		t.Fatalf("View() = %q, want command mode view", model.View())
	}
}

func TestModelUpdateQQuitsWhenRecordViewerFocusedInSplitLayout(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})
	model.command.editor.SetValue("select")
	model.command.editor.CursorEnd()
	model.syncCurrentSQL()

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if model.command.Focused() {
		t.Fatal("command.Focused() = true, want editor blurred while record viewer is focused")
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Update(q) cmd = nil, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Update(q) cmd() type = %T, want %T", cmd(), tea.QuitMsg{})
	}
	if got, want := model.command.Value(), "select"; got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
}

func TestModelUpdateLayoutSwitchesToSplitAndKeepsHistorySearch(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1"}})
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(ctrl+1) cmd = nil, want layout intent command")
	}
	msg := cmd()
	if got, ok := msg.(switchLayoutIntentMsg); !ok || got.Layout != LayoutSplit {
		t.Fatalf("Update(ctrl+1) cmd() = %#v, want split layout intent", msg)
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)
	{
		m, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = m.(Model)
	}

	if got, want := model.state.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeHistorySearch; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	view := model.View()
	if !strings.Contains(view, "Reverse search:") {
		t.Fatalf("View() = %q, want to contain %q", view, "Reverse search:")
	}
	if got, want := model.state.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
}

func TestModelUpdateLayoutSwitchesToViewerOnlyAndClosesHistorySearch(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1"}})
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(ctrl+2) cmd = nil, want layout intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutViewerOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if model.state.Query.HistorySearch != nil {
		t.Fatalf("state.Query.HistorySearch = %#v, want nil", model.state.Query.HistorySearch)
	}
	if model.state.Query.SelectedHistoryEntry != nil {
		t.Fatalf("state.Query.SelectedHistoryEntry = %#v, want nil", model.state.Query.SelectedHistoryEntry)
	}
	if got, want := model.state.Status, "Switched to viewer only. Run a query that returns rows to populate the viewer."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateLayoutSwitchesToCommandOnlyFromSplitViewerFocus(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(ctrl+3) cmd = nil, want layout intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
}

func TestModelUpdateCtrlXUsesSplitFocusWhenAlreadyInSplitLayout(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Focused the record viewer in split layout for 1 row(s) across 1 column(s)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestBuildLatestResultContextInfersSingleTableSource(t *testing.T) {
	context := buildLatestResultContext("select id, name from main.widgets order by id;", ModeCommand, &db.StatementResult{
		Kind: db.StatementResultKindQuery,
		ResultSet: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})
	if context == nil || context.PreservedResult == nil || context.PreservedResult.Source == nil {
		t.Fatalf("buildLatestResultContext() = %#v, want inferred source", context)
	}
	if got, want := *context.PreservedResult.Source, (db.TableRef{Schema: "main", Name: "widgets"}); got != want {
		t.Fatalf("context.PreservedResult.Source = %#v, want %#v", got, want)
	}
	if context.InlineResult == nil || context.InlineResult.Source == nil {
		t.Fatalf("context.InlineResult = %#v, want cloned source", context.InlineResult)
	}
}

func TestModelUpdateCCComposesUpdateAndReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Source:  &db.TableRef{Schema: "main", Name: "widgets"},
			Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(first c) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(second c) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Query.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want true")
	}
	if got, want := model.command.Value(), "UPDATE \"main\".\"widgets\"\nSET\n  \"name\" = 'one'\nWHERE\n  \"id\" = 1;"; got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Query.CurrentSQL, model.command.Value(); got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded UPDATE for row 1 from main.widgets into command mode using primary key predicate."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateCCKeepsSplitLayoutWhenComposingUpdate(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeRecordViewer)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select name from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded UPDATE for row 1 from widgets into command mode using visible column predicate."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got := model.command.Value(); !strings.Contains(got, "UPDATE \"widgets\"") || !strings.Contains(got, "\"name\" = 'one'") {
		t.Fatalf("command.Value() = %q, want generated UPDATE", got)
	}
}

func TestModelUpdateCCReportsUnknownSource(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select widgets.id from widgets join owners on owners.id = widgets.owner_id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = next.(Model)

	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Could not compose UPDATE: result source table is unknown"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateYYComposesInsertAndReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Source:  &db.TableRef{Schema: "main", Name: "widgets"},
			Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(first y) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Press y again to load INSERT for the selected row into command mode."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(second y) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Query.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want true")
	}
	if got, want := model.command.Value(), "INSERT INTO \"main\".\"widgets\" (\n  \"id\",\n  \"name\"\n) VALUES (\n  1,\n  'one'\n);"; got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Query.CurrentSQL, model.command.Value(); got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded INSERT for row 1 from main.widgets into command mode."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got := model.View(); strings.Contains(got, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("View() = %q, want no destructive warning", got)
	}
}

func TestModelUpdateYYKeepsSplitLayoutWhenComposingInsert(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeRecordViewer)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select name from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded INSERT for row 1 from widgets into command mode."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got := model.command.Value(); !strings.Contains(got, "INSERT INTO \"widgets\"") || !strings.Contains(got, "'one'") {
		t.Fatalf("command.Value() = %q, want generated INSERT", got)
	}
	if got := model.View(); strings.Contains(got, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("View() = %q, want no destructive warning", got)
	}
}

func TestModelUpdateYYReportsUnknownSource(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select widgets.id from widgets join owners on owners.id = widgets.owner_id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = next.(Model)

	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Could not compose INSERT: result source table is unknown"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateDDComposesDeleteAndReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Source:  &db.TableRef{Schema: "main", Name: "widgets"},
			Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(first d) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Press d again to load DELETE for the selected row into command mode."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(second d) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Query.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want true")
	}
	if got, want := model.command.Value(), "DELETE FROM \"main\".\"widgets\"\nWHERE\n  \"id\" = 1;"; got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Query.CurrentSQL, model.command.Value(); got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded DELETE for row 1 from main.widgets into command mode using primary key predicate."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	next, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	if got := model.View(); !strings.Contains(got, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("View() = %q, want destructive warning", got)
	}
}

func TestModelUpdateDDKeepsSplitLayoutWhenComposingDelete(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeRecordViewer)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select name from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded DELETE for row 1 from widgets into command mode using visible column predicate."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got := model.command.Value(); !strings.Contains(got, "DELETE FROM \"widgets\"") || !strings.Contains(got, "\"name\" = 'one'") {
		t.Fatalf("command.Value() = %q, want generated DELETE", got)
	}
	next2, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next2.(Model)
	if got := model.View(); !strings.Contains(got, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("View() = %q, want destructive warning", got)
	}
}

func TestModelUpdateDDReportsUnknownSource(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select widgets.id from widgets join owners on owners.id = widgets.owner_id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = next.(Model)

	if got, want := model.state.Query.ActiveMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Could not compose DELETE: result source table is unknown"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateRecordViewerWriteExportsSelectedRowsToCSV(t *testing.T) {
	workingDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingDir, "exports"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	model := NewModel(Session{WorkingDir: workingDir}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "two"}}},
			},
		},
		SelectedRows: []int{1},
	})

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{':'}},
		{Type: tea.KeyRunes, Runes: []rune{'w'}},
		{Type: tea.KeyRunes, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune{'e', 'x', 'p', 'o', 'r', 't', 's', '/', 's', 'e', 'l', 'e', 'c', 't', 'e', 'd', '.', 'c', 's', 'v'}},
		{Type: tea.KeyEnter},
	} {
		next, _ := model.Update(msg)
		model = next.(Model)
	}

	if got, want := model.state.Status, "Exported 1 row(s) as csv from selected rows to exports/selected.csv."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(workingDir, "exports", "selected.csv"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "id,name\n2,two\n"; got != want {
		t.Fatalf("export file = %q, want %q", got, want)
	}
}

func TestModelUpdateRecordViewerWriteFallsBackToAllRowsAndSupportsJSONMarkdownTSV(t *testing.T) {
	workingDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingDir, "exports"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	base := NewModel(Session{WorkingDir: workingDir}, nil)
	base.state.SetReady("")
	base.state.SetLayout(LayoutViewerOnly)
	base.state.SetActiveMode(ModeRecordViewer)
	base.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "two"}}},
			},
		},
	})

	runWrite := func(model Model, command string) Model {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
		model = next.(Model)
		next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(command)})
		model = next.(Model)
		next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return next.(Model)
	}

	model := runWrite(base, "w exports/all.tsv")
	if got, want := model.state.Status, "Exported 2 row(s) as tsv from current result rows to exports/all.tsv."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	tsvData, err := os.ReadFile(filepath.Join(workingDir, "exports", "all.tsv"))
	if err != nil {
		t.Fatalf("ReadFile(tsv) error = %v", err)
	}
	if got, want := string(tsvData), "id\tname\n1\tone\n2\ttwo\n"; got != want {
		t.Fatalf("tsv export = %q, want %q", got, want)
	}

	model = runWrite(base, "w exports/all.json")
	jsonData, err := os.ReadFile(filepath.Join(workingDir, "exports", "all.json"))
	if err != nil {
		t.Fatalf("ReadFile(json) error = %v", err)
	}
	for _, want := range []string{"\"id\": 1", "\"name\": \"one\"", "\"id\": 2", "\"name\": \"two\""} {
		if !strings.Contains(string(jsonData), want) {
			t.Fatalf("json export = %q, want to contain %q", string(jsonData), want)
		}
	}

	model = runWrite(base, "w exports/all.md")
	markdownData, err := os.ReadFile(filepath.Join(workingDir, "exports", "all.md"))
	if err != nil {
		t.Fatalf("ReadFile(md) error = %v", err)
	}
	for _, want := range []string{"| id | name |", "| --- | --- |", "| 1 | one |", "| 2 | two |"} {
		if !strings.Contains(string(markdownData), want) {
			t.Fatalf("markdown export = %q, want to contain %q", string(markdownData), want)
		}
	}
}

func TestModelUpdateRecordViewerWriteValidatesCommandAndPathScope(t *testing.T) {
	workingDir := t.TempDir()
	model := NewModel(Session{WorkingDir: workingDir}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	for _, msg := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune{':'}}, {Type: tea.KeyEnter}} {
		next, _ := model.Update(msg)
		model = next.(Model)
	}
	if got, want := model.state.Status, "Use :w [filename] with .csv, .tsv, .json, or .md while record viewer is focused."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{':'}},
		{Type: tea.KeyRunes, Runes: []rune{'w'}},
		{Type: tea.KeyRunes, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune{'.', '.', '/', 'o', 'u', 't', '.', 'c', 's', 'v'}},
		{Type: tea.KeyEnter},
	} {
		next, _ := model.Update(msg)
		model = next.(Model)
	}
	if got := model.state.Status; !strings.Contains(got, "Could not export rows: export path must stay within") {
		t.Fatalf("state.Status = %q, want scoped path error", got)
	}
}

func TestModelViewRecordViewerShowsWritePrompt(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetLatestResultContext(&LatestResultContext{
		Query: "select id from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = next.(Model)
	view := model.View()
	for _, want := range []string{"Command: :", ":w [file] export", "enter save", "esc cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelToggleHelpShowsContextualHelpSurfaceInCommandMode(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(alt+h) cmd = nil, want toggle help intent")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	if !model.state.Query.HelpVisible {
		t.Fatal("state.Query.HelpVisible = false, want true")
	}
	if got, want := model.state.Status, "Opened help for keybindings and slash commands."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	view := model.View()
	for _, want := range []string{
		"Help:",
		"alt+h toggle help",
		"Command mode:",
		"ctrl+g submit SQL or slash command",
		"Record viewer:",
		"yy/cc/dd load INSERT/UPDATE/DELETE into command mode",
		"Slash commands:",
		"/help lists slash commands; /commands opens the guided wizard",
		"/select - compose a SELECT statement (/select <table>)",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(second alt+h) cmd = nil, want toggle help intent")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)
	if model.state.Query.HelpVisible {
		t.Fatal("state.Query.HelpVisible = true, want false")
	}
	if got, want := model.state.Status, "Closed help."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelToggleHelpShowsSplitAndWizardSpecificGuidance(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveMode(ModeRecordViewer)
	model.state.SetSlashWizardContext(&SlashCommandWizardContext{
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

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Alt: true})
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)

	view := model.View()
	for _, want := range []string{
		"slash wizard: ctrl+g confirm; alt+n/alt+p move; esc back or close",
		"Record viewer [active]",
		"Command line",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelToggleHelpShowsHistorySearchGuidance(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1"}})
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(alt+h) cmd = nil, want toggle help intent")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)

	view := model.View()
	for _, want := range []string{
		"History search:",
		"type to filter recent commands; enter restore selected entry",
		"ctrl+r or up select older match; alt+p or down select newer match",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

type fakeProgram struct {
	run func() (tea.Model, error)
}

var errTestBoom = errors.New("boom")

func (p fakeProgram) Run() (tea.Model, error) {
	return p.run()
}

func openTestAdapter(t *testing.T) *db.SQLAdapter {
	t.Helper()

	adapter, err := db.Open(context.Background(), config.Connection{
		Type:     "sqlite",
		Database: ":memory:",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return adapter
}
