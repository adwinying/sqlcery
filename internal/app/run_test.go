package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
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

func TestModelViewIncludesSessionDetails(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"}, nil)
	model.state.SetReady("")
	view := model.View()

	for _, want := range []string{
		"SQLcery",
		"Connection: local",
		"Dialect: sqlite",
		"App state: ready",
		"Mode: command",
		"Status: Ready for SQL input.",
		"Write SQL here",
		"Command mode",
		"ctrl+g submit",
		"esc clear",
		"ctrl+r history",
		"ctrl+x mode",
		"ctrl+c quit",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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
	model.state.SetLatestResultContext(&LatestResultContext{Query: "select 1", OriginMode: ModeCommand})
	model.state.SetPendingModeSwitch(&ModeSwitchContext{FromMode: ModeCommand, ToMode: ModeRecordViewer})
	model.state.SetSelectedHistoryEntry(&HistoryEntryContext{SQL: "select 2", ConnectionName: "local"})

	view := model.View()

	for _, want := range []string{
		"Latest result: available",
		"Pending mode switch: available",
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

	if got, want := model.state.Query.LastAction, "submit"; got != want {
		t.Fatalf("state.LastAction = %q, want %q", got, want)
	}

	if got, want := model.state.Query.CurrentSQL, "select"; got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}

	if got, want := model.state.Query.LastSubmittedSQL, "select"; got != want {
		t.Fatalf("state.Query.LastSubmittedSQL = %q, want %q", got, want)
	}

	view := model.View()
	for _, want := range []string{"Pending: submit", "Last action: submit", "Current SQL: 6 characters", "Last submitted SQL: 6 characters", "Status: Executing 6 characters of SQL."} {
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
	query := "select id, name from widgets order by id"
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

	msg := cmd()
	executed, ok := msg.(statementExecutedMsg)
	if !ok {
		t.Fatalf("cmd() type = %T, want %T", msg, statementExecutedMsg{})
	}

	next, _ = model.Update(executed)
	model = next.(Model)

	if got, want := model.state.Query.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.PendingIntent = %q, want empty", got)
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
	model.command.editor.SetValue(`insert into widgets (name) values ('Ada'), ('Grace')`)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
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

	if got, want := model.state.Status, "History requested; search UI not implemented yet."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
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
	if got, want := model.state.Query.PendingModeSwitch.ToMode, ModeRecordViewer; got != want {
		t.Fatalf("state.Query.PendingModeSwitch.ToMode = %q, want %q", got, want)
	}

	if got, want := model.state.Status, "Mode switch requested to record-viewer without preserved result context; record viewer UI not implemented yet."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
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
	query := "select id, name from widgets order by id"
	model.command.editor.SetValue(query)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	if model.state.Query.PendingModeSwitch == nil {
		t.Fatal("state.Query.PendingModeSwitch = nil, want preserved mode switch context")
	}
	if model.state.Query.PendingModeSwitch.ResultContext == nil {
		t.Fatal("state.Query.PendingModeSwitch.ResultContext = nil, want latest result context")
	}
	if got, want := len(model.state.Query.PendingModeSwitch.ResultContext.PreservedResult.Rows), 6; got != want {
		t.Fatalf("len(pending switch preserved rows) = %d, want %d", got, want)
	}
	if got, want := len(model.state.Query.PendingModeSwitch.ResultContext.InlineResult.Rows), 5; got != want {
		t.Fatalf("len(pending switch inline rows) = %d, want %d", got, want)
	}
	if got, want := model.state.Query.PendingModeSwitch.ResultContext.Query, query; got != want {
		t.Fatalf("pending switch query = %q, want %q", got, want)
	}
	if got, want := model.state.Query.PendingModeSwitch.ToMode, ModeRecordViewer; got != want {
		t.Fatalf("pending switch to mode = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Mode switch requested to record-viewer with preserved result context; record viewer UI not implemented yet."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
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
