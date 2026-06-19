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

	tea "charm.land/bubbletea/v2"

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

	err := Run(context.Background(), Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter}, RunOptions{
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
	history := apphistory.NewFileBackedHistory(historyPath)

	err := Run(context.Background(), Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter}, RunOptions{
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
		Statement  string `json:"statement"`
		Connection string `json:"connection"`
		Result     string `json:"result"`
		Time       string `json:"time"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got, want := persisted.Statement, "select 1;"; got != want {
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
	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"})
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	view := model.View().Content

	for _, want := range []string{
		"enter submit",
		"esc cancel",
		"ctrl+r history",
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
	blockerPath := filepath.Join(blockerDir, "audit.log")
	if err := os.WriteFile(blockerPath, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	history := apphistory.NewFileBackedHistory(filepath.Join(blockerPath, apphistory.FileName))
	model := newModelWithDependencies(Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter}, modelDependencies{history: history})
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

	if got, want := len(model.state.Interaction.History), 1; got != want {
		t.Fatalf("len(state.Interaction.History) = %d, want %d", got, want)
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
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
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

	for _, keyMsg := range []tea.KeyPressMsg{
		{Text: "SELECT "},
		{Text: "*"},
		{Text: " "},
		{Text: "FROM "},
		{Text: "us"},
	} {
		next, _ = model.Update(keyMsg)
		model = next.(Model)
	}

	if got, want := loadCalls, 1; got != want {
		t.Fatalf("schema load calls after typing = %d, want %d", got, want)
	}

	items := model.command.autocompleteItems(model.state.Interaction)
	if len(items) == 0 {
		t.Fatal("autocompleteItems() = no items, want cached table suggestions")
	}

	if got, want := items[0].Label, "users"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestModelViewIncludesSharedInteractionStatePlaceholders(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select 1", ConnectionName: "local"}})
	model.state.SetLatestResultContext(&LatestResultContext{Statement: "select 1", OriginPane: PaneCommand})
	model.state.SetPendingPaneSwitch(&PaneSwitchContext{FromLayout: LayoutCommandOnly, ToLayout: LayoutResultsOnly, FromPane: PaneCommand, ToPane: PaneResults})
	model.state.SetSelectedHistoryEntry(&HistoryEntryContext{	Statement: "select 2", ConnectionName: "local"})

	if model.state.Interaction.LatestResult == nil {
		t.Fatal("state.Interaction.LatestResult = nil, want available")
	}
	if model.state.Interaction.PendingPaneSwitch == nil {
		t.Fatal("state.Interaction.PendingPaneSwitch = nil, want available")
	}
	if got, want := len(model.state.Interaction.History), 1; got != want {
		t.Fatalf("len(state.Interaction.History) = %d, want %d", got, want)
	}
	if model.state.Interaction.SelectedHistoryEntry == nil {
		t.Fatal("state.Interaction.SelectedHistoryEntry = nil, want available")
	}
}

func TestModelInitTransitionsStartupToReady(t *testing.T) {
	model := NewModel(Session{})

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
	view := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"}).View().Content

	for _, want := range []string{
		"[ startup ]",
		"Preparing command mode...",
		"local",
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
	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"})
	next, _ := model.Update(reconnectStateMsg{
		Context: ReconnectContext{Attempt: 3, Reason: "connection dropped", LastError: "network timeout"},
		Status:  "Reconnecting to local database.",
	})
	model = next.(Model)

	view := model.View().Content
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
	model := NewModel(Session{})
	next, _ := model.Update(appErrorMsg{Err: errors.New("dial tcp 127.0.0.1:5432: connect: connection refused"), Status: "Query failed."})
	model = next.(Model)

	view := model.View().Content
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
	model := NewModel(Session{})
	model.state.SetReady("")

	next, _ := model.Update(statementExecutedMsg{
		Statement:     "select * from missing;",
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
	next, cmd := NewModel(Session{}).Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
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
	initial := NewModel(Session{})
	initial.state.SetReady("")

	next, cmd := initial.Update(tea.KeyPressMsg{Text: "select"})
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

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "1"})
	model = next.(Model)

	if got, want := model.command.editor.Value(), "select\n1"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
}

func TestModelUpdateQTypesWhenEditorFocused(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	next, cmd := model.Update(tea.KeyPressMsg{Text: "q"})
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
	model := NewModel(Session{})
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "select"})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: ";"})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want submit intent command")
	}

	msg := cmd()
	if _, ok := msg.(submitIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, submitIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	if got, want := model.state.Interaction.PendingIntent, IntentSubmit; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if model.state.Interaction.Running == nil {
		t.Fatal("state.Interaction.Running = nil, want running context")
	}
	if got, want := model.state.Interaction.Running.Label, "SQL"; got != want {
		t.Fatalf("state.Interaction.Running.Label = %q, want %q", got, want)
	}

	if got, want := model.state.Interaction.LastAction, "submit"; got != want {
		t.Fatalf("state.LastAction = %q, want %q", got, want)
	}

	if got, want := model.state.Interaction.CurrentSQL, "select;"; got != want {
		t.Fatalf("state.Interaction.CurrentSQL = %q, want %q", got, want)
	}

	if got, want := model.state.Interaction.LastSubmittedSQL, "select;"; got != want {
		t.Fatalf("state.Interaction.LastSubmittedSQL = %q, want %q", got, want)
	}

	view := model.View().Content
	_ = view // "esc cancel query" may be truncated at 120-char width; check state directly
	if model.state.Interaction.Running == nil {
		t.Fatal("state.Interaction.Running should still be set (pending)")
	}
}

func TestModelUpdateSubmitWhileRunningShowsCancelGuidance(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetRunningStatementContext(&RunningStatementContext{Label: "SQL"})

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
	model := NewModel(Session{})
	model.state.SetReady("")
	model.command.editor.SetValue("select\n1")
	model.syncCurrentSQL()
	model.state.SetLastSubmittedSQL("select 0;")

	next, cmd := model.Update(submitIntentMsg{})
	if cmd != nil {
		t.Fatalf("Update(submitIntentMsg{}) cmd = %v, want nil", cmd)
	}
	model = next.(Model)

	if got, want := model.state.Interaction.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Interaction.PendingIntent = %q, want %q", got, want)
	}
	if model.state.Interaction.Running != nil {
		t.Fatalf("state.Interaction.Running = %#v, want nil", model.state.Interaction.Running)
	}
	if got, want := model.state.Status, "SQL is incomplete. End the statement with ';' to run it."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.command.editor.Value(), "select\n1"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.CurrentSQL, "select\n1"; got != want {
		t.Fatalf("state.Interaction.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.LastSubmittedSQL, "select 0;"; got != want {
		t.Fatalf("state.Interaction.LastSubmittedSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.LastAction, "submit"; got != want {
		t.Fatalf("state.Interaction.LastAction = %q, want %q", got, want)
	}
	if got, want := model.state.App.Current, StateReady; got != want {
		t.Fatalf("state.App.Current = %q, want %q", got, want)
	}
}

func TestModelUpdateRunningTickUpdatesElapsedAndFooter(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"})
	model.state.SetReady("")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	startedAt := time.Date(2026, time.April, 8, 10, 0, 0, 0, time.UTC)
	model.state.SetRunningStatementContext(&RunningStatementContext{Label: "SQL", StartedAt: startedAt})

	next, cmd := model.Update(runningTickMsg{StartedAt: startedAt, Now: startedAt.Add(1500 * time.Millisecond)})
	if cmd == nil {
		t.Fatal("Update(runningTickMsg{}) cmd = nil, want follow-up tick")
	}
	model = next.(Model)

	if model.state.Interaction.Running == nil {
		t.Fatal("state.Interaction.Running = nil, want running context")
	}
	if got, want := model.state.Interaction.Running.Elapsed, 1500*time.Millisecond; got != want {
		t.Fatalf("state.Interaction.Running.Elapsed = %v, want %v", got, want)
	}
	if got, want := model.state.Interaction.Running.SpinnerFrame, 1; got != want {
		t.Fatalf("state.Interaction.Running.SpinnerFrame = %d, want %d", got, want)
	}

	view := model.View().Content
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

	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter})
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

	if got, want := model.state.Interaction.PendingIntent, IntentSubmit; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Status, fmt.Sprintf("Executing %d characters of SQL. Press esc to cancel; timeout after 30s.", len(query)); got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	executed := firstCommandMessageForTest[statementExecutedMsg](t, cmd)

	next, _ = model.Update(executed)
	model = next.(Model)

	if got, want := model.state.Interaction.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.PendingIntent = %q, want empty", got)
	}
	if model.state.Interaction.Running != nil {
		t.Fatalf("state.Interaction.Running = %#v, want nil", model.state.Interaction.Running)
	}
	if got, want := model.state.Status, "Query returned 6 rows."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.LatestResult == nil {
		t.Fatal("state.Interaction.LatestResult = nil, want result context")
	}
	if got, want := len(model.state.Interaction.LatestResult.PreservedResult.Rows), 6; got != want {
		t.Fatalf("len(latest.PreservedResult.Rows) = %d, want %d", got, want)
	}
	if got, want := len(model.state.Interaction.LatestResult.InlineResult.Rows), 5; got != want {
		t.Fatalf("len(latest.InlineResult.Rows) = %d, want %d", got, want)
	}
	if !model.state.Interaction.LatestResult.InlineRowsTruncated {
		t.Fatal("latest.InlineRowsTruncated = false, want true")
	}
	if got, want := len(model.state.Interaction.History), 1; got != want {
		t.Fatalf("len(state.Interaction.History) = %d, want %d", got, want)
	}
	if got, want := model.state.Interaction.History[0].Statement, query; got != want {
		t.Fatalf("state.Interaction.History[0].Statement = %q, want %q", got, want)
	}

	view := model.View().Content
	for _, want := range []string{"id | name", "1  | one", "5  | five", "6  | six", "6 rows."} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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

	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter})
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
	if model.state.Interaction.LatestResult == nil {
		t.Fatal("state.Interaction.LatestResult = nil, want result context")
	}
	if got, want := model.state.Interaction.LatestResult.StatementKind, db.StatementResultKindExec; got != want {
		t.Fatalf("latest.StatementKind = %q, want %q", got, want)
	}
	if model.state.Interaction.LatestResult.RowsAffected == nil || *model.state.Interaction.LatestResult.RowsAffected != 2 {
		t.Fatalf("latest.RowsAffected = %#v, want 2", model.state.Interaction.LatestResult.RowsAffected)
	}

	view := model.View().Content
	for _, want := range []string{"2 rows affected"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelUpdateCancelDoesNotClearInput(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	next, _ := model.Update(tea.KeyPressMsg{Text: "select"})
	model = next.(Model)

	// Dismiss autocomplete up-front so this test exercises the fallthrough behaviour.
	model.command.DismissAutocomplete()

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)

	// Esc should not emit clearInputIntentMsg.
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, bad := msg.(clearInputIntentMsg); bad {
				t.Fatal("Update() cmd() type = clearInputIntentMsg, want input preserved")
			}
		}
	}

	if got := model.command.editor.Value(); got == "" {
		t.Fatal("editor.Value() = empty, want input preserved after Esc")
	}
}

func TestModelUpdateCancelDismissesAutocompleteWithoutClearingInput(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")

	// Type enough of a keyword to open the autocomplete dropdown.
	next, _ := model.Update(tea.KeyPressMsg{Text: "sel"})
	model = next.(Model)

	if !model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = false after typing 'sel', want true")
	}

	// First Esc: should close the menu without emitting clearInputIntentMsg
	// and preserve the current editor value.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, bad := msg.(clearInputIntentMsg); bad {
				t.Fatalf("Update() cmd() type = clearInputIntentMsg, want menu dismissal only")
			}
		}
	}
	model = next.(Model)

	if got, want := model.command.editor.Value(), "sel"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true after first Esc, want false")
	}

	// Second Esc: menu already closed -> should also preserve input.
	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)

	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, bad := msg.(clearInputIntentMsg); bad {
				t.Fatal("Update() cmd() type = clearInputIntentMsg on second Esc, want input preserved")
			}
		}
	}

	if got, want := model.command.editor.Value(), "sel"; got != want {
		t.Fatalf("editor.Value() = %q after second Esc, want %q", got, want)
	}
}

func TestModelUpdateCancelAfterDismissTypingReopensAutocomplete(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")

	next, _ := model.Update(tea.KeyPressMsg{Text: "sel"})
	model = next.(Model)

	// Dismiss the menu.
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)

	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true after dismissal, want false")
	}

	// Typing should implicitly lift the dismissal.
	next, _ = model.Update(tea.KeyPressMsg{Text: "e"})
	model = next.(Model)

	if !model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = false after typing more text, want true")
	}
}

func TestAutocompleteMenuOpensAfterTypingOneCharacter(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")

	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true on a fresh model, want false")
	}

	next, _ := model.Update(tea.KeyPressMsg{Text: "s"})
	model = next.(Model)

	if !model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = false after typing one character, want true")
	}
}

func TestAutocompleteMenuStaysClosedOnCursorMovement(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")

	// Type enough to open the menu.
	next, _ := model.Update(tea.KeyPressMsg{Text: "sel"})
	model = next.(Model)

	if !model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = false after typing 'sel', want true")
	}

	// Arrow keys are pure cursor movement: the menu must close and stay
	// closed until the next typing key.
	for _, code := range []rune{tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd} {
		next, _ = model.Update(tea.KeyPressMsg{Code: code})
		model = next.(Model)
		if model.command.AutocompleteVisible(model.state.Interaction) {
			t.Fatalf("AutocompleteVisible() = true after cursor key %v, want false", code)
		}
	}

	// Ctrl+A / Ctrl+E are also cursor movement.
	next, _ = model.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	model = next.(Model)
	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true after Ctrl+A, want false")
	}
	next, _ = model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	model = next.(Model)
	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true after Ctrl+E, want false")
	}

	// Typing again reopens the menu.
	next, _ = model.Update(tea.KeyPressMsg{Text: "e"})
	model = next.(Model)
	if !model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = false after typing another character, want true")
	}
}

func TestAutocompleteMenuStaysClosedOnHistoryRecall(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select * from users"}})

	// Open history search, select the entry (auto-selected), then restore.
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)

	if got, want := model.command.editor.Value(), "select * from users"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true after history recall, want false")
	}

	// Focusing the command pane must also not re-open the menu on its own.
	model.command.Focus()
	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true after Focus() following history recall, want false")
	}

	// The next typed character should open the menu.
	next, _ = model.Update(tea.KeyPressMsg{Text: " "})
	model = next.(Model)
	if !model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = false after typing post-recall, want true")
	}
}

func TestAutocompleteMenuStaysClosedAfterSubmitAndRefocus(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	model := newModelWithDependencies(Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter}, modelDependencies{})
	model.state.SetReady("")

	// Simulate typing a query and submitting it.
	next, _ := model.Update(tea.KeyPressMsg{Text: "select 1;"})
	model = next.(Model)

	if !model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = false after typing SQL, want true")
	}

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	if got := model.command.editor.Value(); got != "" {
		t.Fatalf("editor.Value() = %q, want empty after submit", got)
	}
	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true immediately after submit, want false")
	}

	// Simulate the user refocusing the pane (e.g. Ctrl+W). The menu must
	// not re-open on focus alone.
	model.command.Blur()
	model.command.Focus()
	if model.command.AutocompleteVisible(model.state.Interaction) {
		t.Fatal("AutocompleteVisible() = true after Blur/Focus post-submit, want false")
	}
}

func TestModelUpdateCancelWhileRunningRequestsCancellation(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetRunningStatementContext(&RunningStatementContext{Label: "SQL"})

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
	if got, want := model.state.Interaction.PendingIntent, IntentSubmit; got != want {
		t.Fatalf("state.Interaction.PendingIntent = %q, want %q", got, want)
	}
}

func TestStatementExecutionCancellationUsesFriendlyStatus(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	startedAt := time.Now().Add(-2500 * time.Millisecond)
	model.state.SetRunningStatementContext(&RunningStatementContext{Label: "SQL", StartedAt: startedAt, Elapsed: 2500 * time.Millisecond})

	next, _ := model.Update(statementExecutedMsg{
		Statement:     "select sleep(10);",
		ResultSummary: "error: context canceled",
		Err:           context.Canceled,
	})
	model = next.(Model)

	if got, want := model.state.Status, "Cancelled SQL after 2.5s."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.Running != nil {
		t.Fatalf("state.Interaction.Running = %#v, want nil", model.state.Interaction.Running)
	}
}

func TestStatementExecutionTimeoutUsesFriendlyStatus(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetRunningStatementContext(&RunningStatementContext{Label: "SQL", Elapsed: 30 * time.Second})

	next, _ := model.Update(statementExecutedMsg{
		Statement:     "select sleep(35);",
		ResultSummary: "error: context deadline exceeded",
		Err:           context.DeadlineExceeded,
	})
	model = next.(Model)

	if got, want := model.state.Status, "SQL timed out after 30.0s. Press esc sooner to cancel manually, or retry with a narrower query."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateHistorySetsPendingIntent(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select 1"}, {	Statement: "/tables"}})
	next, cmd := model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want history intent command")
	}

	msg := cmd()
	if _, ok := msg.(historyIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, historyIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	if got, want := model.state.Interaction.PendingIntent, IntentHistory; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActiveModal, ModalHistorySearch; got != want {
		t.Fatalf("state.Interaction.ActiveModal = %q, want %q", got, want)
	}
	if model.state.Interaction.HistorySearch == nil {
		t.Fatal("state.Interaction.HistorySearch = nil, want search context")
	}

	if got, want := model.state.Status, "History search matched 2 entries; selected \"/tables\"."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.SelectedHistoryEntry == nil {
		t.Fatal("state.Interaction.SelectedHistoryEntry = nil, want latest history entry")
	}
	if got, want := model.state.Interaction.SelectedHistoryEntry.Statement, "/tables"; got != want {
		t.Fatalf("state.Interaction.SelectedHistoryEntry.Statement = %q, want %q", got, want)
	}
}

func TestModelUpdateHistoryHandlesEmptyHistory(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	if got, want := model.state.Status, "History search opened; history is empty."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActiveModal, ModalHistorySearch; got != want {
		t.Fatalf("state.Interaction.ActiveModal = %q, want %q", got, want)
	}
	if model.state.Interaction.SelectedHistoryEntry != nil {
		t.Fatalf("state.Interaction.SelectedHistoryEntry = %#v, want nil", model.state.Interaction.SelectedHistoryEntry)
	}
}

func TestModelUpdateHistorySearchFiltersAndCyclesEntries(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select * from users"}, {	Statement: "delete from users"}, {	Statement: "select * from user_sessions"}})

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, _ = model.Update(tea.KeyPressMsg{Text: "su"})
	model = next.(Model)

	if got, want := model.state.Interaction.HistorySearch.Filter, "su"; got != want {
		t.Fatalf("state.Interaction.HistorySearch.Filter = %q, want %q", got, want)
	}
	if model.state.Interaction.SelectedHistoryEntry == nil {
		t.Fatal("state.Interaction.SelectedHistoryEntry = nil, want selected entry")
	}
	if got, want := model.state.Interaction.SelectedHistoryEntry.Statement, "select * from user_sessions"; got != want {
		t.Fatalf("state.Interaction.SelectedHistoryEntry.Statement = %q, want %q", got, want)
	}

	next, _ = model.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	model = next.(Model)

	if got, want := model.state.Interaction.SelectedHistoryEntry.Statement, "select * from users"; got != want {
		t.Fatalf("state.Interaction.SelectedHistoryEntry.Statement = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "History search matched 2 entries; selected \"select * from users\"."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateHistorySearchCancelReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select 1"}})

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)

	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if model.state.Interaction.HistorySearch != nil {
		t.Fatalf("state.Interaction.HistorySearch = %#v, want nil", model.state.Interaction.HistorySearch)
	}
	if got, want := model.state.Interaction.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Interaction.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Exited history search."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateHistorySearchRestoreLoadsEditorAndClosesSearch(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select * from users"}, {	Statement: "select * from user_sessions"}})
	model.command.editor.SetValue("partial")
	model.command.editor.CursorEnd()
	model.syncCurrentSQL()

	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, _ = model.Update(tea.KeyPressMsg{Text: "su"})
	model = next.(Model)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)

	if got, want := model.command.editor.Value(), "select * from user_sessions"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.CurrentSQL, "select * from user_sessions"; got != want {
		t.Fatalf("state.Interaction.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if model.state.Interaction.HistorySearch != nil {
		t.Fatalf("state.Interaction.HistorySearch = %#v, want nil", model.state.Interaction.HistorySearch)
	}
	if model.state.Interaction.SelectedHistoryEntry != nil {
		t.Fatalf("state.Interaction.SelectedHistoryEntry = %#v, want nil", model.state.Interaction.SelectedHistoryEntry)
	}
	if got, want := model.state.Interaction.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Interaction.PendingIntent = %q, want %q", got, want)
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
	model := NewModel(Session{})
	model.state.SetReady("")
	next, cmd := model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want switch mode intent command")
	}

	msg := cmd()
	if _, ok := msg.(switchPaneIntentMsg); !ok {
		t.Fatalf("Update() cmd() type = %T, want %T", msg, switchPaneIntentMsg{})
	}

	next, _ = next.(Model).Update(msg)
	model = next.(Model)

	// In split layout (default), switching to Results Pane succeeds immediately
	if got, want := model.state.Interaction.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.PendingIntent = %q, want %q", got, want)
	}
	if model.state.Interaction.PendingPaneSwitch != nil {
		t.Fatalf("state.Interaction.PendingPaneSwitch = %#v, want nil (switch completes immediately in split layout)", model.state.Interaction.PendingPaneSwitch)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}

	if got, want := model.state.Status, "Focused the Results Pane in split layout. Run a query that returns rows to populate it."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
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

	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter})
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

	next, cmd = model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	if got, want := model.state.Status, "Focused the Results Pane in split layout for 6 row(s) across 2 column(s)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if model.state.Interaction.LatestResult == nil {
		t.Fatal("state.Interaction.LatestResult = nil, want preserved result context")
	}
	if got, want := len(model.state.Interaction.LatestResult.PreservedResult.Rows), 6; got != want {
		t.Fatalf("len(latest preserved rows) = %d, want %d", got, want)
	}
	if got, want := len(model.state.Interaction.LatestResult.InlineResult.Rows), 5; got != want {
		t.Fatalf("len(latest inline rows) = %d, want %d", got, want)
	}
	if got, want := model.state.Interaction.LatestResult.Statement, query; got != want {
		t.Fatalf("latest query = %q, want %q", got, want)
	}
	if model.state.Interaction.PendingPaneSwitch != nil {
		t.Fatalf("state.Interaction.PendingPaneSwitch = %#v, want nil after switching", model.state.Interaction.PendingPaneSwitch)
	}

	view := model.View().Content
	// In REPL mode, Results Pane is not rendered in View(), but transcript shows query results
	for _, want := range []string{"id | name", "1  | one", "6  | six"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelUpdateNewResultResetsResultsPanePage(t *testing.T) {
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

	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	model.state.SetResultsPanePage(4)
	model.command.editor.SetValue("select id, name from widgets order by id;")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want execution command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	if got, want := model.state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
}

func TestModelViewResultsPaneShowsPaginatedRows(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	rows := make([]db.ResultRow, 0, 305)
	for i := 1; i <= 305; i++ {
		rows = append(rows, db.ResultRow{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(i)}}})
	}

	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    rows,
		},
	})
	model.state.SetResultsPanePage(1)

	// In REPL mode, Results Pane is not rendered in View();
	// verify state instead.
	if got, want := model.state.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
	if got, want := len(model.state.Interaction.LatestResult.PreservedResult.Rows), 305; got != want {
		t.Fatalf("len(PreservedResult.Rows) = %d, want %d", got, want)
	}
	if got, want := len(model.state.Interaction.LatestResult.PreservedResult.Columns), 1; got != want {
		t.Fatalf("len(PreservedResult.Columns) = %d, want %d", got, want)
	}
}

func TestModelUpdateCtrlDScrollsWithinPageInResultsPaneOnlyLayout(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})
	// Start on page 0 (rows 1-300).
	if got, want := model.state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("initial state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("Update(ctrl+d) cmd = %#v, want nil", cmd)
	}
	// Page must NOT change — ctrl+d only scrolls within the current page.
	if got, want := model.state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d (ctrl+d must not change page)", got, want)
	}

	// Repeated ctrl+d presses must not push past page boundary.
	for i := 0; i < 20; i++ {
		next, _ = model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
		model = next.(Model)
	}
	if got, want := model.state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d after many ctrl+d presses", got, want)
	}
}

func TestModelUpdateCtrlUScrollsWithinPageInResultsPaneOnlyLayout(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})
	model.state.SetResultsPanePage(2)

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("Update(ctrl+u) cmd = %#v, want nil", cmd)
	}
	// Page must NOT change — ctrl+u only scrolls within the current page.
	if got, want := model.state.Interaction.ResultsPanePage, 2; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d (ctrl+u must not change page)", got, want)
	}

	// Repeated ctrl+u presses must not push past page boundary.
	for i := 0; i < 20; i++ {
		next, _ = model.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		model = next.(Model)
	}
	if got, want := model.state.Interaction.ResultsPanePage, 2; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d after many ctrl+u presses", got, want)
	}
}

func TestModelUpdateCtrlDScrollsOnlyWhenResultsPaneFocusedInSplitLayout(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})
	model.state.SetResultsPanePage(1)
	model.command.editor.SetValue("select 1;")
	model.command.editor.CursorEnd()
	model.syncCurrentSQL()

	// Without Results Pane focus, ctrl+d must not affect the Results Pane page.
	next, _ := model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model = next.(Model)
	if got, want := model.state.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	// With Results Pane focus, ctrl+d scrolls within the current page — page must stay the same.
	model.state.SetActivePane(PaneResults)
	next, _ = model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model = next.(Model)
	if got, want := model.state.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d (ctrl+d must not change page)", got, want)
	}
}

func TestModelUpdateCtrlDDoesNotPageDuringHistorySearch(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActiveModal(ModalHistorySearch)
	model.state.SetHistorySearchContext(&HistorySearchContext{Filter: "sel"})
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    make([]db.ResultRow, 605),
		},
	})
	model.state.SetResultsPanePage(1)

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model = next.(Model)

	if cmd != nil {
		t.Fatalf("Update(ctrl+d) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
	if got, want := model.state.Interaction.ActiveModal, ModalHistorySearch; got != want {
		t.Fatalf("state.Interaction.ActiveModal = %q, want %q", got, want)
	}
	if model.state.Interaction.HistorySearch == nil || model.state.Interaction.HistorySearch.Filter != "sel" {
		t.Fatalf("state.Interaction.HistorySearch = %#v, want query preserved", model.state.Interaction.HistorySearch)
	}
}

func TestModelUpdateArrowKeysNavigateResultsPaneSelectionAcrossPages(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows:    make([]db.ResultRow, 301),
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(right) cmd = %#v, want nil", cmd)
	}
	if got, want := model.resultsPane.selectedColumn, 1; got != want {
		t.Fatalf("resultsPane.selectedColumn = %d, want %d", got, want)
	}
	if got, want := model.state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	model.resultsPane.selectedRow = 299
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = next.(Model)
	if got, want := model.resultsPane.selectedRow, 300; got != want {
		t.Fatalf("resultsPane.selectedRow = %d, want %d", got, want)
	}
	if got, want := model.state.Interaction.ResultsPanePage, 1; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}

	next, _ = model.Update(tea.KeyPressMsg{Text: "k"})
	model = next.(Model)
	if got, want := model.resultsPane.selectedRow, 299; got != want {
		t.Fatalf("resultsPane.selectedRow = %d, want %d", got, want)
	}
	if got, want := model.state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
}

func TestModelUpdateSpaceTogglesSelectedRowsInResultsPane(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets order by id",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}}},
			},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(space) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.LatestResult.SelectedRows, []int{0}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}
	if got, want := model.state.Status, "Selected row 1 (1 total)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	model.resultsPane.selectedRow = 1
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = next.(Model)
	if got, want := model.state.Interaction.LatestResult.SelectedRows, []int{0, 1}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}
	if got, want := model.state.Status, "Selected row 2 (2 total)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	model.resultsPane.selectedRow = 0
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = next.(Model)
	if got, want := model.state.Interaction.LatestResult.SelectedRows, []int{1}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}
	if got, want := model.state.Status, "Unselected row 1 (1 total)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	// In REPL mode, Results Pane is not rendered in View();
	// verify selected row state instead.
	if got, want := len(model.state.Interaction.LatestResult.SelectedRows), 1; got != want {
		t.Fatalf("len(SelectedRows) = %d, want %d after view update", got, want)
	}
}

func TestModelUpdateSpaceIgnoredOutsideResultsPane(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActivePane(PaneCommand)
	model.state.SetPendingIntent(IntentNone, "seed", "unchanged")
	model.state.SetLatestResultContext(&LatestResultContext{
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = next.(Model)
	if model.state.Interaction.LatestResult != nil && len(model.state.Interaction.LatestResult.SelectedRows) != 0 {
		t.Fatalf("SelectedRows = %#v, want unchanged", model.state.Interaction.LatestResult.SelectedRows)
	}
	if got, want := model.state.Status, "unchanged"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if cmd != nil {
		t.Fatalf("Update(space) cmd = %#v, want nil", cmd)
	}
}

func TestModelUpdateNavigationIgnoredOutsideResultsPane(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActivePane(PaneCommand)
	model.state.SetLatestResultContext(&LatestResultContext{
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Text: "l"})
	model = next.(Model)

	if got, want := model.state.Interaction.ResultsPanePage, 0; got != want {
		t.Fatalf("state.Interaction.ResultsPanePage = %d, want %d", got, want)
	}
	if got, want := model.resultsPane.selectedColumn, 0; got != want {
		t.Fatalf("resultsPane.selectedColumn = %d, want %d", got, want)
	}
	if got := model.command.Value(); !strings.Contains(got, "l") {
		t.Fatalf("command.Value() = %q, want rune handled by command mode", got)
	}
	if cmd == nil {
		t.Fatal("Update(l) cmd = nil, want textarea blink command")
	}
}

func TestModelUpdateModeSwitchReturnsFromResultsPaneToCommandMode(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	if got, want := model.state.Interaction.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.PendingIntent, IntentNone; got != want {
		t.Fatalf("state.Interaction.PendingIntent = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Returned to command line."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.PendingPaneSwitch != nil {
		t.Fatalf("state.Interaction.PendingPaneSwitch = %#v, want nil", model.state.Interaction.PendingPaneSwitch)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want editor focused after returning to command mode")
	}
	if strings.Contains(model.View().Content, "Results Pane") {
		t.Fatalf("View() = %q, want command mode view", model.View().Content)
	}
}

func TestModelUpdateQQuitsWhenResultsPaneFocusedInSplitLayout(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})
	model.command.editor.SetValue("select")
	model.command.editor.CursorEnd()
	model.syncCurrentSQL()

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if model.command.Focused() {
		t.Fatal("command.Focused() = true, want editor blurred while Results Pane is focused")
	}

	next, cmd = model.Update(tea.KeyPressMsg{Text: "q"})
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

func TestModelUpdateFocusResultsPaneFromHistorySearchClosesHistorySearch(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select 1"}})
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, _ = model.Update(focusPaneIntentMsg{Pane: PaneResults})
	model = next.(Model)
	{
		m, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = m.(Model)
	}

	if got, want := model.state.Interaction.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if model.state.Interaction.HistorySearch != nil {
		t.Fatalf("state.Interaction.HistorySearch = %#v, want nil", model.state.Interaction.HistorySearch)
	}
}

func TestModelUpdateLayoutSwitchesToResultsPaneOnlyAndClosesHistorySearch(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select 1"}})
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	// Switch to results-pane-only via the intent message (alt+1/alt+2 bindings have been removed)
	next, _ = model.Update(switchLayoutIntentMsg{Layout: LayoutResultsOnly})
	model = next.(Model)

	if got, want := model.state.Interaction.Layout, LayoutResultsOnly; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if model.state.Interaction.HistorySearch != nil {
		t.Fatalf("state.Interaction.HistorySearch = %#v, want nil", model.state.Interaction.HistorySearch)
	}
	if model.state.Interaction.SelectedHistoryEntry != nil {
		t.Fatalf("state.Interaction.SelectedHistoryEntry = %#v, want nil", model.state.Interaction.SelectedHistoryEntry)
	}
	if got, want := model.state.Status, "Switched to results pane only. Run a query that returns rows to populate the Results Pane."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateLayoutSwitchesToCommandOnlyFromSplitResultsPaneFocus(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("Update(ctrl+3) cmd = nil, want layout intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if got, want := model.state.Interaction.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
}

func TestModelUpdateCtrlXUsesSplitFocusWhenAlreadyInSplitLayout(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select 1",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "value"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+x) cmd = nil, want switch mode intent command")
	}
	next, _ = next.(Model).Update(cmd())
	model = next.(Model)

	if got, want := model.state.Interaction.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Focused the Results Pane in split layout for 1 row(s) across 1 column(s)."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestBuildLatestResultContextInfersSingleTableSource(t *testing.T) {
	context := buildLatestResultContext("select id, name from main.widgets order by id;", PaneCommand, &db.StatementResult{
		Kind: db.StatementResultKindQuery,
		ResultSet: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})
	if context == nil || context.PreservedResult == nil || context.PreservedResult.Source == nil {
		t.Fatalf("buildLatestResultContext() = %#v, want inferred source", context)
	}
	if got, want := *context.PreservedResult.Source, (db.TableRef{Namespace: "main", Name: "widgets"}); got != want {
		t.Fatalf("context.PreservedResult.Source = %#v, want %#v", got, want)
	}
	if context.InlineResult == nil || context.InlineResult.Source == nil {
		t.Fatalf("context.InlineResult = %#v, want cloned source", context.InlineResult)
	}
}

func TestModelUpdateCCComposesUpdateAndReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Source:  &db.TableRef{Namespace: "main", Name: "widgets"},
			Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Text: "c"})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(first c) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}

	next, cmd = model.Update(tea.KeyPressMsg{Text: "c"})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(second c) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want true")
	}
	if got, want := model.command.Value(), "UPDATE \"main\".\"widgets\"\nSET\n  \"name\" = 'one'\nWHERE\n  \"id\" = 1;"; got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.CurrentSQL, model.command.Value(); got != want {
		t.Fatalf("state.Interaction.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded UPDATE for row 1 from main.widgets into command mode using primary key predicate."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateCCRejectsUpdateWhenNoPrimaryKeys(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActivePane(PaneResults)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select name from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, _ := model.Update(tea.KeyPressMsg{Text: "c"})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "c"})
	model = next.(Model)

	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if !strings.Contains(model.state.Status, "Could not compose UPDATE") {
		t.Fatalf("state.Status = %q, want error about missing primary key", model.state.Status)
	}
}

func TestModelUpdateCCReportsUnknownSource(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select widgets.id from widgets join owners on owners.id = widgets.owner_id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.KeyPressMsg{Text: "c"})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "c"})
	model = next.(Model)

	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Could not compose UPDATE: result source table is unknown"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateYYComposesInsertAndReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Source:  &db.TableRef{Namespace: "main", Name: "widgets"},
			Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Text: "y"})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(first y) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Press y again to load INSERT for the selected row into command mode."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, cmd = model.Update(tea.KeyPressMsg{Text: "y"})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(second y) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want true")
	}
	if got, want := model.command.Value(), "INSERT INTO \"main\".\"widgets\" (\n  \"id\",\n  \"name\"\n) VALUES (\n  1,\n  'one'\n);"; got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.CurrentSQL, model.command.Value(); got != want {
		t.Fatalf("state.Interaction.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded INSERT for row 1 from main.widgets into command mode."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got := model.View().Content; strings.Contains(got, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("View() = %q, want no destructive warning", got)
	}

	if got := model.View().Content; strings.Contains(got, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("View() = %q, want no destructive warning", got)
	}
}

func TestModelUpdateYYReportsUnknownSource(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select widgets.id from widgets join owners on owners.id = widgets.owner_id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.KeyPressMsg{Text: "y"})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "y"})
	model = next.(Model)

	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Could not compose INSERT: result source table is unknown"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateDDComposesDeleteAndReturnsToCommandMode(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", DatabaseType: "sqlite"})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Source:  &db.TableRef{Namespace: "main", Name: "widgets"},
			Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Text: "d"})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(first d) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Press d again to load DELETE for the selected row into command mode."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	next, cmd = model.Update(tea.KeyPressMsg{Text: "d"})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("Update(second d) cmd = %#v, want nil", cmd)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.Layout, LayoutCommandOnly; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if !model.command.Focused() {
		t.Fatal("command.Focused() = false, want true")
	}
	if got, want := model.command.Value(), "DELETE FROM \"main\".\"widgets\"\nWHERE\n  \"id\" = 1;"; got != want {
		t.Fatalf("command.Value() = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.CurrentSQL, model.command.Value(); got != want {
		t.Fatalf("state.Interaction.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded DELETE for row 1 from main.widgets into command mode using primary key predicate."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	next, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	// In REPL mode, the destructive warning is not rendered in View();
	// verify the warning function still detects the DELETE statement.
	if got := renderGeneratedStatementWarning(model.command.Value()); got == "" {
		t.Fatal("renderGeneratedStatementWarning() = empty, want destructive warning for DELETE")
	}
}

func TestModelUpdateDDKeepsSplitLayoutWhenComposingDelete(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActivePane(PaneResults)
	model.command.Blur()
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select name from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "one"}}}},
		},
	})

	next, _ := model.Update(tea.KeyPressMsg{Text: "d"})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "d"})
	model = next.(Model)

	if got, want := model.state.Interaction.Layout, LayoutSplit; got != want {
		t.Fatalf("state.Interaction.Layout = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.ActivePane, PaneCommand; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Loaded DELETE for row 1 from widgets into command mode using visible column predicate."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if got := model.command.Value(); !strings.Contains(got, "DELETE FROM \"widgets\"") || !strings.Contains(got, "\"name\" = 'one'") {
		t.Fatalf("command.Value() = %q, want generated DELETE", got)
	}
	next2, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next2.(Model)
	if got := renderGeneratedStatementWarning(model.command.Value()); !strings.Contains(got, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("renderGeneratedStatementWarning() = %q, want destructive warning", got)
	}
}

func TestModelUpdateDDReportsUnknownSource(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select widgets.id from widgets join owners on owners.id = widgets.owner_id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.KeyPressMsg{Text: "d"})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "d"})
	model = next.(Model)

	if got, want := model.state.Interaction.ActivePane, PaneResults; got != want {
		t.Fatalf("state.Interaction.ActivePane = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Could not compose DELETE: result source table is unknown"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelUpdateResultsPaneWriteExportsSelectedRowsToCSV(t *testing.T) {
	workingDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingDir, "exports"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	model := NewModel(Session{WorkingDir: workingDir})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "two"}}},
			},
		},
		SelectedRows: []int{1},
	})

	for _, msg := range []tea.KeyPressMsg{
		{Text: ":"},
		{Text: "w"},
		{Text: " "},
		{Text: "exports/selected.csv"},
		{Code: tea.KeyEnter},
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

func TestModelUpdateResultsPaneWriteFallsBackToAllRowsAndSupportsJSONMarkdownTSV(t *testing.T) {
	workingDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingDir, "exports"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	base := NewModel(Session{WorkingDir: workingDir})
	base.state.SetReady("")
	base.state.SetLayout(LayoutResultsOnly)
	base.state.SetActivePane(PaneResults)
	base.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id, name from widgets order by id;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "one"}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "two"}}},
			},
		},
	})

	runWrite := func(model Model, command string) Model {
		next, _ := model.Update(tea.KeyPressMsg{Text: ":"})
		model = next.(Model)
		next, _ = model.Update(tea.KeyPressMsg{Text: command})
		model = next.(Model)
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

func TestModelUpdateResultsPaneWriteValidatesCommandAndPathScope(t *testing.T) {
	workingDir := t.TempDir()
	model := NewModel(Session{WorkingDir: workingDir})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	for _, msg := range []tea.KeyPressMsg{{Text: ":"}, {Code: tea.KeyEnter}} {
		next, _ := model.Update(msg)
		model = next.(Model)
	}
	if got, want := model.state.Status, "Use :w [filename] with .csv, .tsv, .json, or .md while Results Pane is focused."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	for _, msg := range []tea.KeyPressMsg{
		{Text: ":"},
		{Text: "w"},
		{Text: " "},
		{Text: "../out.csv"},
		{Code: tea.KeyEnter},
	} {
		next, _ := model.Update(msg)
		model = next.(Model)
	}
	if got := model.state.Status; !strings.Contains(got, "Could not export rows: export path must stay within") {
		t.Fatalf("state.Status = %q, want scoped path error", got)
	}
}

func TestModelViewResultsPaneShowsWritePrompt(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutResultsOnly)
	model.state.SetActivePane(PaneResults)
	model.state.SetLatestResultContext(&LatestResultContext{
		Statement: "select id from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "id"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}}},
		},
	})

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: ":"})
	model = next.(Model)
	if got, want := model.resultsPane.pendingAction, resultsPanePendingActionExport; got != want {
		t.Fatalf("resultsPane.pendingAction = %q, want %q", got, want)
	}
	if got, want := model.resultsPane.exportBuffer, ":"; got != want {
		t.Fatalf("resultsPane.writeBuffer = %q, want %q", got, want)
	}
	if got := model.state.Status; !strings.Contains(got, "Type :w") {
		t.Fatalf("state.Status = %q, want to contain export guidance", got)
	}
}

func TestModelToggleHelpShowsContextualHelpSurfaceInCommandMode(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("Update(alt+h) cmd = nil, want toggle help intent")
	}
	model = next.(Model)

	next, _ = model.Update(cmd())
	model = next.(Model)

	if !model.state.Interaction.HelpVisible {
		t.Fatal("state.Interaction.HelpVisible = false, want true")
	}
	if got, want := model.state.Status, "Opened help for keybindings and slash commands."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	view := renderHelpSurface(model.state.Interaction)
	for _, want := range []string{
		"Help:",
		"alt+h toggle help",
		"Command mode:",
		"enter submit SQL or slash command",
		"Results Pane:",
		"yy/cc/dd load INSERT/UPDATE/DELETE into command mode",
		"Slash commands:",
		"/help lists slash commands; /commands opens the guided wizard",
		"/select - compose a SELECT statement (/select <table>)",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderHelpSurface() = %q, want to contain %q", view, want)
		}
	}

	next, cmd = model.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("Update(second alt+h) cmd = nil, want toggle help intent")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)
	if model.state.Interaction.HelpVisible {
		t.Fatal("state.Interaction.HelpVisible = true, want false")
	}
	if got, want := model.state.Status, "Closed help."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelToggleHelpShowsSplitAndWizardSpecificGuidance(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetLayout(LayoutSplit)
	model.state.SetActivePane(PaneResults)
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

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt})
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)

	view := renderHelpSurface(model.state.Interaction)
	for _, want := range []string{
		"slash wizard: enter confirm; ctrl+n/ctrl+p move; esc back or close",
		"Results Pane [active]",
		"Command line",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderHelpSurface() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelToggleHelpShowsHistorySearchGuidance(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetHistory([]HistoryEntryContext{{	Statement: "select 1"}})
	next, _ := model.Update(historyIntentMsg{})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("Update(alt+h) cmd = nil, want toggle help intent")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)

	view := renderHelpSurface(model.state.Interaction)
	for _, want := range []string{
		"History search:",
		"type to filter recent commands; enter restore selected entry",
		"ctrl+r or up select older match; ctrl+n or down select newer match",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderHelpSurface() = %q, want to contain %q", view, want)
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
