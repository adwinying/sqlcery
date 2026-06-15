package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
	tea "charm.land/bubbletea/v2"
)

func TestParseSlashCommandParsesQuotedArgs(t *testing.T) {
	parsed, err := parseSlashCommand(`/columns "main.users"`)
	if err != nil {
		t.Fatalf("parseSlashCommand() error = %v", err)
	}
	if parsed == nil {
		t.Fatal("parseSlashCommand() = nil, want command")
	}

	if got, want := parsed.DisplayName, "/columns"; got != want {
		t.Fatalf("DisplayName = %q, want %q", got, want)
	}
	if got, want := len(parsed.Args), 1; got != want {
		t.Fatalf("len(Args) = %d, want %d", got, want)
	}
	if got, want := parsed.Args[0], "main.users"; got != want {
		t.Fatalf("Args[0] = %q, want %q", got, want)
	}
}

func TestParseSlashCommandRejectsUnterminatedQuote(t *testing.T) {
	parsed, err := parseSlashCommand(`/columns "users`)
	if err == nil {
		t.Fatal("parseSlashCommand() error = nil, want parse error")
	}
	if parsed != nil {
		t.Fatalf("parseSlashCommand() = %#v, want nil", parsed)
	}
}

func TestDispatchSlashCommandCommandsReturnsWizard(t *testing.T) {
	result, err := dispatchSlashCommand(context.Background(), slashCommandContext{}, slashCommand{Name: "commands", DisplayName: "/commands", RawInput: "/commands"})
	if err != nil {
		t.Fatalf("dispatchSlashCommand() error = %v", err)
	}
	if result.Wizard == nil {
		t.Fatal("result.Wizard = nil, want wizard context")
	}
	if got, want := result.Wizard.Step, SlashCommandWizardStepCommand; got != want {
		t.Fatalf("result.Wizard.Step = %q, want %q", got, want)
	}
	if len(result.Wizard.Commands) != 9 {
		t.Fatalf("len(result.Wizard.Commands) = %d, want 9", len(result.Wizard.Commands))
	}
	if got, want := result.Wizard.Commands[0].DisplayName, "/tables"; got != want {
		t.Fatalf("commands[0].DisplayName = %q, want %q", got, want)
	}
}

func TestDispatchSlashCommandSelectBuildsTemplate(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table users (id integer primary key, name text, email text)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	result, err := dispatchSlashCommand(context.Background(), slashCommandContext{Session: Session{Adapter: adapter}, Dialect: adapter.Dialect()}, slashCommand{Name: "select", DisplayName: "/select", RawInput: "/select users", Args: []string{"users"}})
	if err != nil {
		t.Fatalf("dispatchSlashCommand() error = %v", err)
	}
	if !result.ShouldReplace {
		t.Fatal("result.ShouldReplace = false, want true")
	}
	for _, want := range []string{"SELECT", `FROM "users";`, `"id"`, `"name"`, `"email"`} {
		if got := result.ReplaceEditor; !containsLine(got, want) {
			t.Fatalf("ReplaceEditor = %q, want to contain %q", got, want)
		}
	}
}

func TestDispatchSlashCommandInsertBuildsDialectAwareTemplate(t *testing.T) {
	tests := []struct {
		name    string
		dialect db.Dialect
		want    []string
	}{
		{
			name:    "sqlite",
			dialect: db.SQLiteDialect(),
			want: []string{
				"INSERT INTO \"users\"",
				"\"column_1\"",
				"?",
			},
		},
		{
			name:    "postgres",
			dialect: db.PostgresDialect(),
			want: []string{
				"INSERT INTO \"users\"",
				"\"column_1\"",
				"$1",
			},
		},
		{
			name:    "mysql",
			dialect: db.MySQLDialect(),
			want: []string{
				"INSERT INTO `users`",
				"`column_1`",
				"?",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dispatchSlashCommand(context.Background(), slashCommandContext{Dialect: tt.dialect}, slashCommand{Name: "insert", DisplayName: "/insert", RawInput: "/insert users", Args: []string{"users"}})
			if err != nil {
				t.Fatalf("dispatchSlashCommand() error = %v", err)
			}
			if !result.ShouldReplace {
				t.Fatal("result.ShouldReplace = false, want true")
			}
			for _, want := range tt.want {
				if got := result.ReplaceEditor; !containsLine(got, want) {
					t.Fatalf("ReplaceEditor = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestDispatchSlashCommandSelectBuildsDialectAwareTemplateWithoutMetadata(t *testing.T) {
	tests := []struct {
		name    string
		dialect db.Dialect
		want    []string
	}{
		{
			name:    "sqlite",
			dialect: db.SQLiteDialect(),
			want:    []string{"SELECT", `FROM "users";`, "  *"},
		},
		{
			name:    "postgres",
			dialect: db.PostgresDialect(),
			want:    []string{"SELECT", `FROM "users";`, "  *"},
		},
		{
			name:    "mysql",
			dialect: db.MySQLDialect(),
			want:    []string{"SELECT", "FROM `users`;", "  *"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dispatchSlashCommand(context.Background(), slashCommandContext{Dialect: tt.dialect}, slashCommand{Name: "select", DisplayName: "/select", RawInput: "/select users", Args: []string{"users"}})
			if err != nil {
				t.Fatalf("dispatchSlashCommand() error = %v", err)
			}
			if !result.ShouldReplace {
				t.Fatal("result.ShouldReplace = false, want true")
			}
			for _, want := range tt.want {
				if got := result.ReplaceEditor; !containsLine(got, want) {
					t.Fatalf("ReplaceEditor = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestDispatchSlashCommandUpdateBuildsDialectAwareTemplate(t *testing.T) {
	tests := []struct {
		name    string
		dialect db.Dialect
		want    []string
	}{
		{
			name:    "sqlite",
			dialect: db.SQLiteDialect(),
			want: []string{
				"UPDATE \"users\"",
				"\"column_1\" = ?",
			},
		},
		{
			name:    "postgres",
			dialect: db.PostgresDialect(),
			want: []string{
				"UPDATE \"users\"",
				"\"column_1\" = $1",
			},
		},
		{
			name:    "mysql",
			dialect: db.MySQLDialect(),
			want: []string{
				"UPDATE `users`",
				"`column_1` = ?",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dispatchSlashCommand(context.Background(), slashCommandContext{Dialect: tt.dialect}, slashCommand{Name: "update", DisplayName: "/update", RawInput: "/update users", Args: []string{"users"}})
			if err != nil {
				t.Fatalf("dispatchSlashCommand() error = %v", err)
			}
			if !result.ShouldReplace {
				t.Fatal("result.ShouldReplace = false, want true")
			}
			for _, want := range tt.want {
				if got := result.ReplaceEditor; !containsLine(got, want) {
					t.Fatalf("ReplaceEditor = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestDispatchSlashCommandCreateBuildsDialectAwareTemplate(t *testing.T) {
	tests := []struct {
		name    string
		dialect db.Dialect
		want    []string
	}{
		{
			name:    "sqlite",
			dialect: db.SQLiteDialect(),
			want: []string{
				"CREATE TABLE \"users\"",
				"id INTEGER PRIMARY KEY",
				"name TEXT",
			},
		},
		{
			name:    "postgres",
			dialect: db.PostgresDialect(),
			want: []string{
				"CREATE TABLE \"users\"",
				"id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY",
				"name TEXT",
			},
		},
		{
			name:    "mysql",
			dialect: db.MySQLDialect(),
			want: []string{
				"CREATE TABLE `users`",
				"id BIGINT AUTO_INCREMENT PRIMARY KEY",
				"name VARCHAR(255)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dispatchSlashCommand(context.Background(), slashCommandContext{Dialect: tt.dialect}, slashCommand{Name: "create", DisplayName: "/create", RawInput: "/create users", Args: []string{"users"}})
			if err != nil {
				t.Fatalf("dispatchSlashCommand() error = %v", err)
			}
			if !result.ShouldReplace {
				t.Fatal("result.ShouldReplace = false, want true")
			}
			for _, want := range tt.want {
				if got := result.ReplaceEditor; !containsLine(got, want) {
					t.Fatalf("ReplaceEditor = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestDispatchSlashCommandDeleteAndDropBuildDialectAwareIdentifiers(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		dialect    db.Dialect
		wantClause string
	}{
		{name: "delete sqlite", command: "delete", dialect: db.SQLiteDialect(), wantClause: `DELETE FROM "users"`},
		{name: "delete postgres", command: "delete", dialect: db.PostgresDialect(), wantClause: `DELETE FROM "users"`},
		{name: "delete mysql", command: "delete", dialect: db.MySQLDialect(), wantClause: "DELETE FROM `users`"},
		{name: "drop sqlite", command: "drop", dialect: db.SQLiteDialect(), wantClause: `DROP TABLE "users";`},
		{name: "drop postgres", command: "drop", dialect: db.PostgresDialect(), wantClause: `DROP TABLE "users";`},
		{name: "drop mysql", command: "drop", dialect: db.MySQLDialect(), wantClause: "DROP TABLE `users`;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dispatchSlashCommand(context.Background(), slashCommandContext{Dialect: tt.dialect}, slashCommand{Name: tt.command, DisplayName: "/" + tt.command, RawInput: "/" + tt.command + " users", Args: []string{"users"}})
			if err != nil {
				t.Fatalf("dispatchSlashCommand() error = %v", err)
			}
			if !containsLine(result.ReplaceEditor, tt.wantClause) {
				t.Fatalf("ReplaceEditor = %q, want to contain %q", result.ReplaceEditor, tt.wantClause)
			}
		})
	}
}

func TestModelSubmitDispatchesSlashTablesExpandsToSQL(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key, name text not null)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	{
		m, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = m.(Model)
	}
	model.command.editor.SetValue("/tables")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want slash dispatch command")
	}
	model = next.(Model)

	if got, want := model.state.Status, "Dispatching /tables. Press esc to cancel; timeout after 30s."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.Running == nil {
		t.Fatal("state.Query.Running = nil, want running context")
	}
	if got, want := model.state.Interaction.Running.Label, "/tables"; got != want {
		t.Fatalf("state.Query.Running.Label = %q, want %q", got, want)
	}

	executed := firstCommandMessageForTest[slashCommandExecutedMsg](t, cmd)

	next, _ = model.Update(executed)
	model = next.(Model)

	// /tables now expands to SQL in the editor instead of executing immediately
	if got, want := model.state.Interaction.LastSubmittedSQL, ""; got != want {
		t.Fatalf("state.Query.LastSubmittedSQL = %q, want %q", got, want)
	}
	if model.state.Interaction.Running != nil {
		t.Fatalf("state.Query.Running = %#v, want nil", model.state.Interaction.Running)
	}
	if got, want := len(model.state.Interaction.SessionHistory), 0; got != want {
		t.Fatalf("len(state.Query.SessionHistory) = %d, want %d", got, want)
	}
	if model.state.Interaction.LatestResult != nil {
		t.Fatalf("state.Query.LatestResult = %#v, want nil", model.state.Interaction.LatestResult)
	}
	if got, want := model.state.Interaction.LastAction, "slash:/tables"; got != want {
		t.Fatalf("state.Query.LastAction = %q, want %q", got, want)
	}
	wantStatus := "Expanded /tables for current database into command mode. Review it, then press enter to run."
	if got := model.state.Status; got != wantStatus {
		t.Fatalf("state.Status = %q, want %q", got, wantStatus)
	}
	// Editor should now contain the SQL to list tables
	for _, want := range []string{"sqlite_master", "IN ('table', 'view')"} {
		if got := model.command.editor.Value(); !containsLine(got, want) {
			t.Fatalf("editor.Value() = %q, want to contain %q", got, want)
		}
	}
}

func TestModelSubmitSlashCommandExpandToEditorDoesNotPersistHistory(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key, name text not null)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	historyPath := filepath.Join(t.TempDir(), apphistory.DirName, apphistory.FileName)
	history := apphistory.NewFileBackedHistory(historyPath)
	model := newModelWithDependencies(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter}, modelDependencies{history: history})
	model.state.SetReady("")
	model.command.editor.SetValue("/tables")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want slash dispatch command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[slashCommandExecutedMsg](t, cmd))
	model = next.(Model)

	// /tables expands to SQL — it should NOT be persisted to history (same as /select, /insert, etc.)
	if _, err := os.ReadFile(historyPath); err == nil {
		t.Fatal("ReadFile() succeeded, want history file to not exist for expand-to-editor commands")
	}
	if got, want := len(model.state.Interaction.SessionHistory), 0; got != want {
		t.Fatalf("len(state.Query.SessionHistory) = %d, want %d", got, want)
	}
}

func TestModelSubmitDispatchesSlashSelectIntoEditor(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key, name text not null)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	model.command.editor.SetValue("/select widgets")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want slash dispatch command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[slashCommandExecutedMsg](t, cmd))
	model = next.(Model)

	if got, want := model.state.Interaction.LastSubmittedSQL, ""; got != want {
		t.Fatalf("state.Query.LastSubmittedSQL = %q, want %q", got, want)
	}
	if got, want := len(model.state.Interaction.SessionHistory), 0; got != want {
		t.Fatalf("len(state.Query.SessionHistory) = %d, want %d", got, want)
	}
	if model.state.Interaction.LatestResult != nil {
		t.Fatalf("state.Query.LatestResult = %#v, want nil", model.state.Interaction.LatestResult)
	}
	if got, want := model.state.Status, "Expanded /select for widgets into command mode. Review it, then press enter to run."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	for _, want := range []string{"SELECT", `FROM "widgets";`, `"id"`, `"name"`} {
		if got := model.command.editor.Value(); !containsLine(got, want) {
			t.Fatalf("editor.Value() = %q, want to contain %q", got, want)
		}
	}
	if got, want := model.state.Interaction.CurrentSQL, model.command.editor.Value(); got != want {
		t.Fatalf("state.Query.CurrentSQL = %q, want %q", got, want)
	}
	if got, want := model.state.Interaction.LastAction, "slash:/select"; got != want {
		t.Fatalf("state.Query.LastAction = %q, want %q", got, want)
	}
}

func TestModelSubmitCommandsOpensWizard(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"})
	model.state.SetReady("")
	{
		m, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = m.(Model)
	}
	model.command.editor.SetValue("/commands")
	model.syncCurrentSQL()

	// /commands opens the wizard synchronously without an async dispatch
	next, cmd := model.Update(submitIntentMsg{})
	if cmd != nil {
		t.Fatalf("Update(submitIntentMsg{}) cmd = %v, want nil (wizard opened synchronously)", cmd)
	}
	model = next.(Model)

	if model.state.Interaction.SlashWizard == nil {
		t.Fatal("state.Query.SlashWizard = nil, want wizard context")
	}
	if got, want := model.state.Status, "Choose a slash command and press enter."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	view := model.View().Content
	for _, want := range []string{"Command wizard:", "Step 1/2: choose a slash command", "> /tables - list tables in the current database", "enter confirm | ctrl+n next | ctrl+p prev | esc close"} {
		if !containsLine(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelSubmitCommandsWizardDispatchesResultCommand(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key, name text not null)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	model.state.SetSlashWizardContext(&SlashCommandWizardContext{
		Step: SlashCommandWizardStepCommand,
		Commands: []SlashCommandWizardCommand{{
			Name:        "tables",
			DisplayName: "/tables",
			Summary:     "list tables in the current database",
			Usage:       "/tables",
		}},
	})

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want slash dispatch command")
	}
	model = next.(Model)
	if got, want := model.state.Status, "Dispatching /tables from wizard. Press esc to cancel; timeout after 30s."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.Running == nil {
		t.Fatal("state.Query.Running = nil, want running context")
	}
	if got, want := model.state.Interaction.Running.Label, "/tables"; got != want {
		t.Fatalf("state.Query.Running.Label = %q, want %q", got, want)
	}

	next, _ = model.Update(firstCommandMessageForTest[slashCommandExecutedMsg](t, cmd))
	model = next.(Model)
	if model.state.Interaction.Running != nil {
		t.Fatalf("state.Query.Running = %#v, want nil", model.state.Interaction.Running)
	}
	if model.state.Interaction.SlashWizard != nil {
		t.Fatalf("state.Query.SlashWizard = %#v, want nil", model.state.Interaction.SlashWizard)
	}
	if got, want := model.state.Status, "Expanded /tables for current database into command mode. Review it, then press enter to run."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelSubmitCommandsWizardLoadsTargetedTemplate(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"})
	model.state.SetReady("")
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

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want slash dispatch command")
	}
	model = next.(Model)
	if got, want := model.state.Status, "Dispatching /select from wizard. Press esc to cancel; timeout after 30s."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.Running == nil {
		t.Fatal("state.Query.Running = nil, want running context")
	}
	if got, want := model.state.Interaction.Running.Label, "/select"; got != want {
		t.Fatalf("state.Query.Running.Label = %q, want %q", got, want)
	}

	next, _ = model.Update(firstCommandMessageForTest[slashCommandExecutedMsg](t, cmd))
	model = next.(Model)
	if model.state.Interaction.Running != nil {
		t.Fatalf("state.Query.Running = %#v, want nil", model.state.Interaction.Running)
	}
	if got, want := model.command.editor.Value(), "SELECT\n  *\nFROM \"widgets\";"; got != want {
		t.Fatalf("editor.Value() = %q, want %q", got, want)
	}
	if model.state.Interaction.SlashWizard != nil {
		t.Fatalf("state.Query.SlashWizard = %#v, want nil", model.state.Interaction.SlashWizard)
	}
}

func TestModelSubmitCommandsWizardAdvancesToTargetSelection(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	{
		m, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = m.(Model)
	}
	model.state.SetSlashWizardContext(&SlashCommandWizardContext{
		Step: SlashCommandWizardStepCommand,
		Commands: []SlashCommandWizardCommand{{
			Name:        "select",
			DisplayName: "/select",
			Summary:     "compose a SELECT statement",
			Usage:       "/select <table>",
			NeedsTarget: true,
		}},
	})

	next, cmd := model.Update(submitIntentMsg{})
	if cmd != nil {
		t.Fatalf("Update(submitIntentMsg{}) cmd = %v, want nil while advancing wizard", cmd)
	}
	model = next.(Model)
	if model.state.Interaction.SlashWizard == nil || model.state.Interaction.SlashWizard.Step != SlashCommandWizardStepTarget {
		t.Fatalf("state.Query.SlashWizard = %#v, want target step", model.state.Interaction.SlashWizard)
	}
	if got, want := model.state.Status, "Choose a table for /select and press enter."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	view := model.View().Content
	for _, want := range []string{"Step 1/2 complete: /select", "Step 2/2: choose a table for /select", "> main.widgets", "esc back"} {
		if !containsLine(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestModelSlashWizardNavigationKeysMoveBackAndClose(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"})
	model.state.SetReady("")
	model.state.SetSlashWizardContext(&SlashCommandWizardContext{
		Step: SlashCommandWizardStepTarget,
		Commands: []SlashCommandWizardCommand{{
			Name:        "select",
			DisplayName: "/select",
			Summary:     "compose a SELECT statement",
			Usage:       "/select <table>",
			NeedsTarget: true,
		}},
		Targets: []SlashCommandWizardTarget{{Value: "users", Display: "users"}, {Value: "widgets", Display: "widgets"}},
	})

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Update(ctrl+n) cmd = nil, want move command")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)
	if got, want := model.state.Interaction.SlashWizard.SelectedTarget, 1; got != want {
		t.Fatalf("SelectedTarget = %d, want %d", got, want)
	}

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update(esc) cmd = nil, want back command")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)
	if model.state.Interaction.SlashWizard == nil || model.state.Interaction.SlashWizard.Step != SlashCommandWizardStepCommand {
		t.Fatalf("state.Query.SlashWizard = %#v, want command step", model.state.Interaction.SlashWizard)
	}

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update(esc) cmd = nil, want close command")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)
	if model.state.Interaction.SlashWizard != nil {
		t.Fatalf("state.Query.SlashWizard = %#v, want nil", model.state.Interaction.SlashWizard)
	}
	if got, want := model.state.Status, "Closed the slash command wizard."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func TestModelSubmitUnknownSlashCommandShowsErrorAndSkipsSQLExecution(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	model.command.editor.SetValue("/wat")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil, want slash dispatch command")
	}
	model = next.(Model)

	next, _ = model.Update(firstCommandMessageForTest[slashCommandExecutedMsg](t, cmd))
	model = next.(Model)

	if got, want := model.state.Status, "/wat failed: unknown slash command /wat"; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
	if model.state.Interaction.LatestResult != nil {
		t.Fatalf("state.Query.LatestResult = %#v, want nil", model.state.Interaction.LatestResult)
	}

	var count int
	if err := adapter.QueryRowContext(context.Background(), `select count(*) from sqlite_master where type = 'table' and name = '/wat'`).Scan(&count); err != nil {
		t.Fatalf("QueryRowContext() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("table named /wat exists; slash command was executed as SQL")
	}
}

func TestSlashCommandCancellationUsesFriendlyStatus(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("")
	model.state.SetRunningStatementContext(&RunningStatementContext{Label: "/tables", Elapsed: 1200 * time.Millisecond})

	next, _ := model.Update(slashCommandExecutedMsg{
		Command:       slashCommand{RawInput: "/tables", DisplayName: "/tables", Name: "tables"},
		ResultSummary: "error: context canceled",
		Err:           context.Canceled,
	})
	model = next.(Model)

	if got, want := model.state.Status, "Cancelled /tables after 1.2s."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}

func containsLine(value, want string) bool {
	return strings.Contains(value, want)
}

func TestModelSubmitNeedsTargetCommandWithoutArgOpensTableSelection(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	{
		m, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = m.(Model)
	}
	model.command.editor.SetValue("/select")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	if cmd != nil {
		t.Fatalf("Update(submitIntentMsg{}) cmd = %v, want nil (wizard opened synchronously)", cmd)
	}
	model = next.(Model)

	wizard := model.state.Interaction.SlashWizard
	if wizard == nil {
		t.Fatal("state.Query.SlashWizard = nil, want table selection wizard")
	}
	if got, want := wizard.Step, SlashCommandWizardStepTarget; got != want {
		t.Fatalf("wizard.Step = %q, want %q", got, want)
	}
	if !wizard.DirectInvocation {
		t.Fatal("wizard.DirectInvocation = false, want true")
	}
	if len(wizard.Targets) == 0 {
		t.Fatal("wizard.Targets = empty, want at least one table")
	}
	selectedCommand, ok := slashWizardCommandByIndex(wizard)
	if !ok {
		t.Fatal("slashWizardCommandByIndex() = false, want selected command")
	}
	if got, want := selectedCommand.Name, "select"; got != want {
		t.Fatalf("selectedCommand.Name = %q, want %q", got, want)
	}
	if got, want := model.state.Status, "Choose a table for /select and press enter."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}

	// The modal should render with a simpler header (no step labels) and "esc close".
	view := model.View().Content
	for _, want := range []string{"Choose a table for /select:", "> main.widgets", "esc close"} {
		if !containsLine(view, want) {
			t.Fatalf("View() does not contain %q\nFull view:\n%s", want, view)
		}
	}
	for _, notWant := range []string{"Step 1/2", "Step 2/2", "esc back"} {
		if containsLine(view, notWant) {
			t.Fatalf("View() contains unexpected %q", notWant)
		}
	}
}

func TestModelSubmitNeedsTargetCommandWithoutArgConfirmDispatchesCommand(t *testing.T) {
	adapter := openTestAdapter(t)
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer primary key)`); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite", Adapter: adapter})
	model.state.SetReady("")
	model.command.editor.SetValue("/select")
	model.syncCurrentSQL()

	// Open the table picker
	next, _ := model.Update(submitIntentMsg{})
	model = next.(Model)

	if model.state.Interaction.SlashWizard == nil {
		t.Fatal("wizard not opened after /select without args")
	}

	// Confirm with the pre-selected table
	next, cmd := model.Update(submitIntentMsg{})
	if cmd == nil {
		t.Fatal("Update(submitIntentMsg{}) cmd = nil after confirming table, want dispatch command")
	}
	model = next.(Model)
	if got, want := model.state.Interaction.Running.Label, "/select"; got != want {
		t.Fatalf("Running.Label = %q, want %q", got, want)
	}

	next, _ = model.Update(firstCommandMessageForTest[slashCommandExecutedMsg](t, cmd))
	model = next.(Model)

	if model.state.Interaction.SlashWizard != nil {
		t.Fatalf("wizard still open after dispatch; want nil")
	}
	for _, want := range []string{"SELECT", `FROM "main"."widgets";`} {
		if got := model.command.editor.Value(); !containsLine(got, want) {
			t.Fatalf("editor.Value() = %q, want to contain %q", got, want)
		}
	}
}

func TestModelSubmitNeedsTargetCommandWithoutArgEscClosesWizard(t *testing.T) {
	model := NewModel(Session{ConnectionName: "local", ConnectionType: "sqlite"})
	model.state.SetReady("")
	model.state.SetSlashWizardContext(&SlashCommandWizardContext{
		Step: SlashCommandWizardStepTarget,
		Commands: []SlashCommandWizardCommand{{
			Name:        "select",
			DisplayName: "/select",
			Summary:     "compose a SELECT statement",
			Usage:       "/select <table>",
			NeedsTarget: true,
		}},
		Targets:          []SlashCommandWizardTarget{{Value: "users", Display: "users"}},
		DirectInvocation: true,
	})

	// ESC on a direct-invocation wizard at target step should close, not go back.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update(esc) cmd = nil, want close command")
	}
	model = next.(Model)
	next, _ = model.Update(cmd())
	model = next.(Model)

	if model.state.Interaction.SlashWizard != nil {
		t.Fatalf("state.Query.SlashWizard = %#v, want nil (closed)", model.state.Interaction.SlashWizard)
	}
	if got, want := model.state.Status, "Closed the slash command wizard."; got != want {
		t.Fatalf("state.Status = %q, want %q", got, want)
	}
}
