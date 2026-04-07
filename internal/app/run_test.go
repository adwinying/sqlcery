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
	view := model.View()

	for _, want := range []string{
		"SQLcery",
		"Connection: local",
		"Dialect: sqlite",
		"App state: ready",
		"Layout: command only",
		"Mode: command",
		"Status: Ready for SQL input.",
		"Write SQL here",
		"Command mode",
		"ctrl+g submit",
		"esc clear",
		"ctrl+r history",
		"ctrl+x focus",
		"ctrl+1 split",
		"ctrl+2 command",
		"ctrl+3 viewer",
		"ctrl+c quit",
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

	view := model.View()

	for _, want := range []string{
		"Latest result: available",
		"Pending mode switch: available",
		"Session history: 1 entries",
		"Selected history entry: available",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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
		"App state: startup",
		"Status: Starting SQLcery.",
		"[ startup ]",
		"Preparing command mode...",
		"App state startup | connection local | ctrl+c quit",
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
		"App state: reconnect",
		"Status: Reconnecting to local database.",
		"Reconnect attempt: 3",
		"Reconnect reason: connection dropped",
		"Reconnect error: network timeout",
		"[ reconnect ]",
		"Connection recovery placeholder is active.",
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
	next, _ := model.Update(appErrorMsg{Err: errTestBoom, Status: "Query failed."})
	model = next.(Model)

	view := model.View()
	for _, want := range []string{
		"App state: error",
		"Status: Query failed.",
		"Error: boom",
		"[ error ]",
		"boom",
		"App state error | ctrl+c quit",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 'e', 'l', 'e', 'c', 't'}})
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
	for _, want := range []string{"Pending: submit", "Running: - SQL 0.0s", "Last action: submit", "Current SQL: 7 characters", "Last submitted SQL: 7 characters", "Status: Executing 7 characters of SQL."} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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
	if got := model.View(); !strings.Contains(got, "Status: SQL is incomplete. End the statement with ';' to run it.") {
		t.Fatalf("View() = %q, want incomplete SQL status", got)
	}
}

func TestModelUpdateRunningTickUpdatesElapsedAndFooter(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
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
	for _, want := range []string{"Running: \\ SQL 1.5s", "\\ SQL 1.5s"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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
	if got, want := model.state.Status, fmt.Sprintf("Executing %d characters of SQL.", len(query)); got != want {
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
	for _, want := range []string{"Results:", "id | name", "1  | one", "5  | five", "Showing first 5 of 6 rows.", "Session history: 1 entries"} {
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
	for _, want := range []string{"Results:", "2 rows affected", "Status: Statement executed successfully. 2 rows affected."} {
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
	for _, want := range []string{"Layout: viewer only", "Record viewer", "Rows: 6  Columns: 2", "id | name", "1  | one", "6  | six", "ctrl+3 viewer"} {
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
	for _, want := range []string{"Layout: viewer only", "Rows: 305  Columns: 1", "Page: 2/2  Showing rows 301-305", "page 2/2", "301", "305"} {
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

func TestModelUpdateModeSwitchReturnsFromRecordViewerToCommandMode(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetLayout(LayoutViewerOnly)
	model.state.SetActiveMode(ModeRecordViewer)
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
	if strings.Contains(model.View(), "Record viewer") {
		t.Fatalf("View() = %q, want command mode view", model.View())
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

	if got, want := model.state.Query.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeHistorySearch; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	view := model.View()
	for _, want := range []string{"Layout: split", "Record viewer", "Command line [active]", "Reverse search:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelUpdateLayoutSwitchesToViewerOnlyAndClosesHistorySearch(t *testing.T) {
	model := NewModel(Session{}, nil)
	model.state.SetReady("")
	model.state.SetSessionHistory([]HistoryEntryContext{{SQL: "select 1"}})
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(ctrl+3) cmd = nil, want layout intent command")
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
	view := model.View()
	for _, want := range []string{"Layout: viewer only", "Run a query that returns rows, then press ctrl+x or ctrl+3."} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true})
	if cmd == nil {
		t.Fatal("Update(ctrl+2) cmd = nil, want layout intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if got, want := model.state.Query.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Query.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Query.ActiveMode, ModeCommand; got != want {
		t.Fatalf("state.Query.ActiveMode = %q, want %q", got, want)
	}
	view := model.View()
	if strings.Contains(view, "Command line [active]") || strings.Contains(view, "----------------------------------------") {
		t.Fatalf("View() = %q, want command-only layout without split sections", view)
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
	view := model.View()
	for _, want := range []string{"Layout: split", "Record viewer [active]", "Command line"} {
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
		Type: "sqlite",
		SQLite: config.SQLiteConnectionOptions{
			Database: ":memory:",
		},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return adapter
}
