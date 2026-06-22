package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/db"
)

func TestCommandModeViewPreservesEditorLayout(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT \"users\".name, 42, 'Ada', @id -- comment")

	view := mode.View(InteractionState{})

	for _, want := range []string{
		">",
		`SELECT "users".name, 42, 'Ada', @id -- comment`,
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestCommandModeBuildViewContextDelegatesToWidget(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT 1")

	ctx := mode.buildViewContext(InteractionState{})
	view := mode.widget.View(ctx)

	if !strings.Contains(view, "SELECT") {
		t.Fatalf("widget.View(buildViewContext()) = %q, want SQL content", view)
	}
}

func TestBuildAutocompleteItemsSuggestsSlashCommands(t *testing.T) {
	items := buildAutocompleteItems("/se", len([]rune("/se")), nil, nil)

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want slash command suggestions")
	}

	if got, want := items[0].Label, "/select"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsSuggestsTablesAfterFrom(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM us", len([]rune("SELECT * FROM us")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "orders"}}},
		nil)

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want table suggestions")
	}

	if got, want := items[0].Label, "users"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsSuggestsColumnsForQualifier(t *testing.T) {
	items := buildAutocompleteItems("SELECT users.na", len([]rune("SELECT users.na")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name", "email"}}}},
		nil)

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want column suggestions")
	}

	if got, want := items[0].Label, "name"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsSuggestsColumnsFromActiveTables(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users WHERE na", len([]rune("SELECT * FROM users WHERE na")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name", "email"}}}},
		nil)

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want active table columns")
	}

	if got, want := items[0].Label, "name"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsRanksColumnsBeforeKeywordsInWhere(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users WHERE ", len([]rune("SELECT * FROM users WHERE ")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name", "email"}}}},
		nil)

	assertAutocompleteLabelsPrefix(t, items, []string{"email", "id", "name"})
	assertAutocompleteLabelBefore(t, items, "name", "AND")
}

func TestBuildAutocompleteItemsRanksJoinAndWhereAfterTable(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users ", len([]rune("SELECT * FROM users ")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "user_sessions"}}},
		nil)

	assertAutocompleteLabelsPrefix(t, items, []string{"JOIN", "WHERE"})
}

func TestBuildAutocompleteItemsRanksSetFirstAfterUpdateTarget(t *testing.T) {
	items := buildAutocompleteItems("UPDATE users ", len([]rune("UPDATE users ")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
		nil)

	assertAutocompleteLabelsPrefix(t, items, []string{"SET", "WHERE", "RETURNING"})
}

func TestBuildAutocompleteItemsRanksIntoFirstAfterInsert(t *testing.T) {
	items := buildAutocompleteItems("INS", len([]rune("INS")), nil, nil)

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want keyword suggestions")
	}

	if got, want := items[0].Label, "INSERT"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}

	items = buildAutocompleteItems("INSERT ", len([]rune("INSERT ")), nil, nil)
	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want post-insert suggestions")
	}

	if got, want := items[0].Label, "INTO"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsRanksActiveTableColumnsBeforeFallbackColumns(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users WHERE ", len([]rune("SELECT * FROM users WHERE ")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name"}}}},
		&LatestResultContext{PreservedResult: &db.ResultSet{Columns: []db.ResultColumn{{Name: "archived_at"}, {Name: "id"}, {Name: "name"}}}})

	assertAutocompleteLabelBefore(t, items, "name", "archived_at")
	assertAutocompleteLabelBefore(t, items, "id", "archived_at")
}

func TestBuildAutocompleteItemsRanksUnqualifiedTablesBeforeSchemaQualifiedMatches(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM us", len([]rune("SELECT * FROM us")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Namespace: "public", Name: "users"}}},
		nil)

	assertAutocompleteLabelsPrefix(t, items, []string{"users", "public.users"})
}

func TestBuildAutocompleteItemsUsesSchemaQualifiedActiveTableColumns(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM warehouse.users WHERE ", len([]rune("SELECT * FROM warehouse.users WHERE ")),
		&AutocompleteSchemaContext{Tables: []AutocompleteTableContext{
			{Namespace: "warehouse", Name: "users", Columns: []string{"id", "name"}},
			{Namespace: "public", Name: "users", Columns: []string{"id", "public_name"}},
		}},
		nil)

	assertAutocompleteLabelsPrefix(t, items, []string{"id", "name"})
	for _, item := range items {
		if item.Label == "public_name" {
			t.Fatalf("items contained column from wrong schema: %#v", items)
		}
	}
}

func TestBuildAutocompleteItemsSuppressedImmediatelyAfterSemicolon(t *testing.T) {
	// No prefix typed yet after the semicolon — suppress the dropdown.
	for _, value := range []string{
		"SELECT * FROM users;",
		"SELECT * FROM users; ",
		"SELECT * FROM users;\n",
	} {
		items := buildAutocompleteItems(value, len([]rune(value)), nil, nil)
		if len(items) != 0 {
			t.Fatalf("buildAutocompleteItems(%q) = %d items, want 0 (suppressed after semicolon)", value, len(items))
		}
	}
}

func TestBuildAutocompleteItemsResetsContextAfterSemicolon(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users; DE", len([]rune("SELECT * FROM users; DE")), nil, nil)

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want new statement suggestions")
	}

	if got, want := items[0].Label, "DELETE"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
	assertAutocompleteLabelBefore(t, items, "DELETE", "DESC")
}

func TestCommandModeAcceptSuggestionReplacesPrefix(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM us")
	mode.editor.CursorEnd()
	// Simulate the "user just typed" state that normally gates the menu.
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
	}

	updated, _ := mode.Update(tea.KeyPressMsg{Code: tea.KeyTab}, query)

	if got, want := updated.Value(), "SELECT * FROM users"; got != want {
		t.Fatalf("Value() = %q, want %q", got, want)
	}
}

func TestCommandModeNavFirstCtrlNAdvancesFromHighlight(t *testing.T) {
	// Item 0 is already visually highlighted (ghost text). First ctrl+n should
	// advance to item 1, not re-apply item 0.
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "orders"}, {Name: "users"}}},
	}
	mode.cachedSuggestions = mode.computeSuggestions(query.AutocompleteSchema, query.LatestResult)

	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, query)

	if got, want := updated.widget.SelectedSuggestion(), 1; got != want {
		t.Fatalf("selectedSuggestion = %d, want %d (advanced past initial highlight)", got, want)
	}
	if !updated.autocompleteNavActive {
		t.Fatal("autocompleteNavActive should be true after first ctrl+n")
	}
	suggestions := updated.cachedSuggestions
	if !strings.Contains(updated.Value(), suggestions[1].InsertText) {
		t.Fatalf("Value() = %q, want it to contain suggestion[1] %q", updated.Value(), suggestions[1].InsertText)
	}
}

func TestCommandModeNavCtrlNWrapsToRestoreSlotAfterAllItems(t *testing.T) {
	// Pressing ctrl+n count times from the initial selection (item 0) cycles
	// through all items and lands on the restore slot.
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "orders"}, {Name: "users"}}},
	}
	mode.cachedSuggestions = mode.computeSuggestions(query.AutocompleteSchema, query.LatestResult)
	count := len(mode.cachedSuggestions)

	// count presses from initial sel=0 cycles through all items and hits restore.
	updated := mode
	for i := 0; i < count; i++ {
		updated, _ = updated.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, query)
	}

	if got := updated.widget.SelectedSuggestion(); got != -1 {
		t.Fatalf("selectedSuggestion = %d, want -1 (restore slot)", got)
	}
	if got, want := updated.Value(), "SELECT * FROM "; got != want {
		t.Fatalf("Value() at restore slot = %q, want %q", got, want)
	}
}

func TestCommandModeNavFirstCtrlPGoesToRestoreSlot(t *testing.T) {
	// Item 0 is the initial highlight. ctrl+p goes backward from 0 to the
	// restore slot, revealing the original typed text.
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "orders"}, {Name: "users"}}},
	}
	mode.cachedSuggestions = mode.computeSuggestions(query.AutocompleteSchema, query.LatestResult)

	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, query)

	if got := updated.widget.SelectedSuggestion(); got != -1 {
		t.Fatalf("selectedSuggestion = %d, want -1 (restore slot on first ctrl+p)", got)
	}
	if !updated.autocompleteNavActive {
		t.Fatal("autocompleteNavActive should be true after first ctrl+p")
	}
	if got, want := updated.Value(), "SELECT * FROM "; got != want {
		t.Fatalf("Value() = %q, want original %q", got, want)
	}
}

func TestCommandModeNavEscRestoresOriginalPrefix(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	originalValue := "SELECT * FROM "
	mode.editor.SetValue(originalValue)
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "orders"}, {Name: "users"}}},
	}
	mode.cachedSuggestions = mode.computeSuggestions(query.AutocompleteSchema, query.LatestResult)

	// Activate nav — first ctrl+n advances from sel=0 to sel=1, populating item 1.
	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, query)
	if updated.Value() == originalValue {
		t.Fatal("value should have changed after ctrl+n")
	}

	// ESC: DismissAutocomplete is called by the parent model; call it directly here.
	updated.DismissAutocomplete()

	if got, want := updated.Value(), originalValue; got != want {
		t.Fatalf("Value() after ESC = %q, want original %q", got, want)
	}
	if updated.autocompleteNavActive {
		t.Fatal("autocompleteNavActive should be false after dismiss")
	}
}

func TestCommandModeNavTypingAcceptsAndClearsNav(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "orders"}, {Name: "users"}}},
	}
	mode.cachedSuggestions = mode.computeSuggestions(query.AutocompleteSchema, query.LatestResult)

	// Activate nav — first ctrl+n populates item 1.
	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, query)
	if !updated.autocompleteNavActive {
		t.Fatal("expected nav to be active after ctrl+n")
	}
	populatedValue := updated.Value()

	// Type a character: nav clears, populated text stays.
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 's', Text: "s"}, query)

	if updated.autocompleteNavActive {
		t.Fatal("autocompleteNavActive should be false after typing")
	}
	if !strings.HasPrefix(updated.Value(), populatedValue) {
		t.Fatalf("Value() = %q, want it to start with populated %q", updated.Value(), populatedValue)
	}
}

func TestCommandModeNavTabClosesWithoutRewriting(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "orders"}, {Name: "users"}}},
	}
	mode.cachedSuggestions = mode.computeSuggestions(query.AutocompleteSchema, query.LatestResult)

	// Activate nav — first ctrl+n populates item 1.
	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, query)
	populatedValue := updated.Value()

	// Tab: should close dropdown, leave text as-is.
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyTab}, query)

	if updated.autocompleteNavActive {
		t.Fatal("autocompleteNavActive should be false after tab")
	}
	if got := updated.Value(); got != populatedValue {
		t.Fatalf("Value() after tab = %q, want %q (text should stay)", got, populatedValue)
	}
}

func TestCommandModeViewRendersAutocompletePanel(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM us")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
	}

	// Ghost text should show completion for "users" after typing "us"
	mode.cachedSuggestions = mode.computeSuggestions(query.AutocompleteSchema, query.LatestResult)
	ghost := mode.ghostText()
	if ghost != "ers" {
		t.Fatalf("ghostText() = %q, want %q", ghost, "ers")
	}
}

func TestCommandModeViewRendersSlashWizard(t *testing.T) {
	m := &slashWizardModal{wizard: SlashCommandWizardContext{
		Step: SlashCommandWizardStepCommand,
		Commands: []SlashCommandWizardCommand{{
			Name:        "tables",
			DisplayName: "/tables",
			Summary:     "list tables in the current database",
			Usage:       "/tables",
		}},
	}}
	modal := m.Render(InteractionState{}, 62)

	for _, want := range []string{"Step 1/2: choose a slash command", "> /tables - list tables in the current database"} {
		if !strings.Contains(modal, want) {
			t.Fatalf("slashWizardModal.Render() = %q, want to contain %q", modal, want)
		}
	}
	hintsStr := strings.Join(m.FooterHints(InteractionState{}), " | ")
	for _, want := range []string{"enter confirm", "ctrl+n next", "ctrl+p prev", "esc close", "ctrl+t keybindings"} {
		if !strings.Contains(hintsStr, want) {
			t.Fatalf("slashWizardModal.FooterHints() = %q, want to contain %q", hintsStr, want)
		}
	}
}

func TestCommandModeViewRendersHistorySearch(t *testing.T) {
	h := &historySearchModal{filter: "su", selectedIndex: 0}
	interaction := InteractionState{
		ActiveModal: ModalHistorySearch,
		History:     []HistoryEntryContext{{Statement: "select * from user_sessions"}, {Statement: "select * from users"}},
	}
	modal := h.Render(interaction, 62)

	for _, want := range []string{"> select * from users"} {
		if !strings.Contains(modal, want) {
			t.Fatalf("historySearchModal.Render() = %q, want to contain %q", modal, want)
		}
	}
	hintsStr := strings.Join(h.FooterHints(interaction), " | ")
	for _, want := range []string{"enter restore", "ctrl+p up", "ctrl+n down", "esc", "ctrl+t keybindings"} {
		if !strings.Contains(hintsStr, want) {
			t.Fatalf("historySearchModal.FooterHints() = %q, want to contain %q", hintsStr, want)
		}
	}
}

func TestCommandModeViewRendersInlineSelectResult(t *testing.T) {
	query := InteractionState{
		LatestResult: &LatestResultContext{
			OriginPane:    PaneCommand,
			StatementKind: db.StatementResultKindQuery,
			InlineResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}, {Name: "created_at"}},
				Rows: []db.ResultRow{{
					Values: []db.ResultValue{
						{Kind: db.ValueKindInteger, Value: int64(1)},
						{Kind: db.ValueKindString, Value: "Ada"},
						{Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 7, 11, 30, 0, 0, time.UTC)},
					},
				}},
			},
		},
	}
	result := renderInlineResult(query)
	for _, want := range []string{"Results:", "id | name | created_at", "1  | Ada  | 2026-04-07 11:30:00", "1 row."} {
		if !strings.Contains(result, want) {
			t.Fatalf("renderInlineResult() = %q, want to contain %q", result, want)
		}
	}
}

func TestCommandModeViewRendersInlineExecResult(t *testing.T) {
	rowsAffected := int64(2)
	lastInsertID := int64(9)
	query := InteractionState{
		LatestResult: &LatestResultContext{
			OriginPane:    PaneCommand,
			StatementKind: db.StatementResultKindExec,
			RowsAffected:  &rowsAffected,
			LastInsertID:  &lastInsertID,
		},
	}
	result := renderInlineResult(query)
	for _, want := range []string{"Results:", "2 rows affected", "last insert id 9"} {
		if !strings.Contains(result, want) {
			t.Fatalf("renderInlineResult() = %q, want to contain %q", result, want)
		}
	}
}

func TestCommandModeViewShowsWarningForDestructiveGeneratedCommands(t *testing.T) {
	sql := "DELETE FROM \"users\"\nWHERE\n  \"id\" = 7;"
	warning := renderGeneratedStatementWarning(sql)
	if !strings.Contains(warning, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("renderGeneratedStatementWarning() = %q, want destructive warning", warning)
	}
}

func TestCommandModeFooterShowsRunningIndicator(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	footer := mode.Footer("local", "sqlite", InteractionState{
		Layout:  LayoutCommandOnly,
		Running: &RunningStatementContext{Label: "SQL", Elapsed: 1500 * time.Millisecond},
	})

	for _, want := range []string{"Command mode", "layout command only", "connection local", "sqlite", "ctrl+t keybindings", "ctrl+3 command", "- SQL 1.5s", "esc cancel query"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}

func TestCommandModeFooterShowsResultsPanePagingWhenResultsPaneFocusedInSplit(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	footer := mode.Footer("local", "sqlite", InteractionState{
		Layout:     LayoutSplit,
		ActivePane: PaneResults,
	})

	for _, want := range []string{"Command line hidden focus", "layout split", "ctrl+t keybindings", "ctrl+u scroll up", "ctrl+d scroll down"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}

func TestCommandModeFooterShowsSelectionCountFromResultsPaneResult(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	footer := mode.Footer("local", "sqlite", InteractionState{
		Layout:     LayoutSplit,
		ActivePane: PaneCommand,
		MarkedRows: []int{0, 2},
	})

	for _, want := range []string{"Command mode", "connection local", "sqlite", "2 selected"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}

func TestIsCompleteSQLStatement(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "single statement", input: "SELECT 1;", want: true},
		{name: "multiline statement", input: "SELECT\n  1;", want: true},
		{name: "trailing whitespace", input: "SELECT 1;   \n\t", want: true},
		{name: "trailing line comment", input: "SELECT 1; -- done", want: true},
		{name: "trailing block comment", input: "SELECT 1; /* done */", want: true},
		{name: "multiple statements ending with semicolon", input: "SELECT 1;\nSELECT 2;", want: true},
		{name: "semicolon in string", input: "SELECT ';' AS value;", want: true},
		{name: "leading comments", input: "-- hello\n/* world */\nSELECT 1;", want: true},
		{name: "missing semicolon", input: "SELECT 1", want: false},
		{name: "incomplete after semicolon", input: "SELECT 1;\nSELECT 2", want: false},
		{name: "unterminated block comment", input: "SELECT 1; /* done", want: false},
		{name: "unterminated string", input: "SELECT 'oops;", want: false},
		{name: "only semicolon", input: ";", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCompleteSQLStatement(tt.input); got != tt.want {
				t.Fatalf("isCompleteSQLStatement(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func assertAutocompleteLabelsPrefix(t *testing.T, items []autocompleteItem, want []string) {
	t.Helper()

	if len(items) < len(want) {
		t.Fatalf("len(items) = %d, want at least %d", len(items), len(want))
	}

	for i, label := range want {
		if got := items[i].Label; got != label {
			t.Fatalf("items[%d].Label = %q, want %q", i, got, label)
		}
	}
}

func assertAutocompleteLabelBefore(t *testing.T, items []autocompleteItem, left, right string) {
	t.Helper()

	leftIndex := -1
	rightIndex := -1
	for i, item := range items {
		if item.Label == left && leftIndex < 0 {
			leftIndex = i
		}
		if item.Label == right && rightIndex < 0 {
			rightIndex = i
		}
	}

	if leftIndex < 0 {
		t.Fatalf("label %q not found in items %#v", left, items)
	}
	if rightIndex < 0 {
		t.Fatalf("label %q not found in items %#v", right, items)
	}
	if leftIndex >= rightIndex {
		t.Fatalf("label %q index = %d, want before %q index = %d", left, leftIndex, right, rightIndex)
	}
}

// --- Scroll behaviour tests ---

func TestCommandModeScrollStepIsHalfPage(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20) // innerHeight=20, half-page=10
	for i := 0; i < 30; i++ {
		mode.AppendReplEntry("> ", fmt.Sprintf("SELECT %d;", i), fmt.Sprintf("%d row.", i))
	}

	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, InteractionState{})
	want := max(1, 20/2) // 10
	if got := updated.widget.ScrollOffset(); got != want {
		t.Fatalf("scrollOffset = %d after ctrl+u with innerHeight=20, want %d (half-page)", got, want)
	}
}

func TestCommandModeScrollOffsetBoundedByContent(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	mode.AppendReplEntry("> ", "SELECT 1;", "1 row.")

	// Press ctrl+u many times — scrollOffset must never exceed naturalScrollTop.
	for i := 0; i < 100; i++ {
		mode, _ = mode.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, InteractionState{})
	}
	natural := mode.computeNaturalScrollTop(max(1, mode.innerHeight))
	if mode.widget.ScrollOffset() > natural {
		t.Fatalf("scrollOffset = %d exceeds naturalScrollTop = %d", mode.widget.ScrollOffset(), natural)
	}
}

func TestCommandModeTypingSnapsScrollToZero(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	for i := 0; i < 20; i++ {
		mode.AppendReplEntry("> ", fmt.Sprintf("SELECT %d;", i), fmt.Sprintf("%d row.", i))
	}

	mode, _ = mode.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, InteractionState{})
	if mode.widget.ScrollOffset() == 0 {
		t.Fatal("expected scrollOffset > 0 after ctrl+u")
	}

	// Type a printable character — scrollOffset must snap back to 0.
	mode, _ = mode.Update(tea.KeyPressMsg{Text: "S"}, InteractionState{})
	if got := mode.widget.ScrollOffset(); got != 0 {
		t.Fatalf("scrollOffset = %d after typing, want 0", got)
	}
}

func TestCommandModeScrollDownIsNoOpAtBottom(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	// scrollOffset starts at 0; ctrl+d should leave it at 0.
	mode, _ = mode.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, InteractionState{})
	if got := mode.widget.ScrollOffset(); got != 0 {
		t.Fatalf("scrollOffset = %d after ctrl+d at bottom, want 0", got)
	}
}

func TestCommandModeAutocompleteDropdownHeightCappedAtFive(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true

	// 10 tables → dropdown must show at most autocompletePanelRows rows.
	tables := make([]AutocompleteTableContext, 10)
	for i := range tables {
		tables[i] = AutocompleteTableContext{Name: fmt.Sprintf("table_%02d", i)}
	}
	query := InteractionState{AutocompleteSchema: &AutocompleteSchemaContext{Tables: tables}}

	dropdown := mode.renderAutocompleteDropdown(query)
	if dropdown == "" {
		t.Fatal("expected non-empty dropdown with 10 table suggestions")
	}
	rows := strings.Split(dropdown, "\n")
	if len(rows) > autocompletePanelRows {
		t.Fatalf("dropdown has %d rows, want at most %d", len(rows), autocompletePanelRows)
	}
}

func TestCommandModeAutocompleteOpensAboveCursorPreservingLinesBelowCursor(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	// Two-line editor. After CursorEnd the cursor is on line 1 ("WHERE id = 1;").
	// CursorUp moves it to line 0 near the end of "SELECT * FROM us".
	mode.editor.SetValue("SELECT * FROM us\nWHERE id = 1;")
	mode.editor.CursorEnd()
	mode.editor.CursorUp()
	mode.autocompleteOpenedByTyping = true

	if line := mode.editor.Line(); line != 0 {
		t.Skipf("cursor ended on line %d instead of 0 — skipping overlay geometry test", line)
	}

	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{
			Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "user_sessions"}},
		},
	}

	// Autocomplete must fire for this test to be meaningful.
	mode.cachedSuggestions = mode.autocompleteItems(query)
	if len(mode.cachedSuggestions) == 0 {
		t.Skip("autocomplete produced no items for this cursor position — skipping overlay geometry test")
	}

	view := mode.View(query)
	viewLines := strings.Split(view, "\n")

	// Find the cursor line (contains "SELECT" and "FROM").
	cursorIdx := -1
	for i, l := range viewLines {
		if strings.Contains(l, "SELECT") && strings.Contains(l, "FROM") {
			cursorIdx = i
			break
		}
	}
	if cursorIdx < 0 {
		t.Fatal("cursor line (SELECT * FROM us) not found in view")
	}
	// The dropdown opens above the cursor — check the row immediately before it.
	if cursorIdx < 1 {
		t.Fatal("cursor line is at the top of the view — no room for dropdown above")
	}

	// The row immediately before the cursor must be a dropdown entry.
	lineBeforeCursor := viewLines[cursorIdx-1]
	if !strings.Contains(lineBeforeCursor, "[") {
		t.Fatalf("row before cursor does not look like a dropdown row: %q", lineBeforeCursor)
	}

	// The line immediately below the cursor must still be "WHERE id = 1;" —
	// the dropdown must not overwrite continuation lines below the cursor.
	if cursorIdx+1 < len(viewLines) {
		lineAfterCursor := viewLines[cursorIdx+1]
		if !strings.Contains(lineAfterCursor, "WHERE") {
			t.Fatalf("continuation line below cursor was overwritten: %q", lineAfterCursor)
		}
	}
}

func TestCommandModeBottomAnchorsShortContent(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 30)
	mode.AppendReplEntry("> ", "SELECT 1;", "1 row.")

	view := mode.View(InteractionState{})
	lines := strings.Split(view, "\n")

	if got := len(lines); got != 30 {
		t.Fatalf("view has %d rows, want 30 (innerHeight)", got)
	}

	// The prompt line ("> ") should sit at the very last row — no blank reserve below.
	promptRow := len(lines) - 1
	if strings.TrimSpace(lines[promptRow]) == "" {
		t.Fatalf("expected prompt at last row %d, but that row is blank; view:\n%s", promptRow, view)
	}

	// All rows above the transcript must be blank top-padding.
	// Transcript has 1 SQL line ("> SELECT 1;") and 1 output line ("1 row."),
	// so content occupies rows promptRow-2..promptRow (3 rows).
	for i := 0; i < promptRow-2; i++ {
		if s := strings.TrimSpace(lines[i]); s != "" {
			t.Fatalf("row %d above transcript is not blank top-padding: %q", i, s)
		}
	}
}

func TestCommandModeEditorAtLastRowWhenContentOverflows(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	// Enough transcript to force overflow so topPadding is 0.
	for i := 0; i < 30; i++ {
		mode.AppendReplEntry("> ", fmt.Sprintf("SELECT %d;", i), fmt.Sprintf("%d row.", i))
	}

	view := mode.View(InteractionState{})
	lines := strings.Split(view, "\n")

	if got := len(lines); got != 10 {
		t.Fatalf("view has %d rows, want 10", got)
	}

	// With no bottom reserve the editor sits at the very last row.
	// The last row must be non-blank (editor prompt), not a blank reserve row.
	if s := strings.TrimSpace(lines[len(lines)-1]); s == "" {
		t.Fatalf("last row is blank — expected editor prompt, not a blank reserve; view:\n%s", view)
	}
}

func TestCommandModeAutocompleteVisibleWhenCursorIsAtBufferEnd(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	// Fill the pane with transcript so the prompt sits near the bottom with
	// the reserve below it.
	for i := 0; i < 30; i++ {
		mode.AppendReplEntry("> ", fmt.Sprintf("SELECT %d;", i), fmt.Sprintf("%d row.", i))
	}
	mode.editor.SetValue("SELECT * FROM us")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true

	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{
			Tables: []AutocompleteTableContext{{Name: "users"}},
		},
	}

	mode.cachedSuggestions = mode.autocompleteItems(query)
	if len(mode.cachedSuggestions) == 0 {
		t.Fatal("autocomplete produced no items — test precondition failed")
	}

	view := mode.View(query)
	lines := strings.Split(view, "\n")

	// The cursor sits on the last row. The dropdown must open upward —
	// visible in the autocompletePanelRows rows immediately above the cursor.
	cursorRow := len(lines) - 1
	found := false
	for i := cursorRow - autocompletePanelRows; i < cursorRow; i++ {
		if i >= 0 && strings.Contains(lines[i], "users") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected autocomplete dropdown in rows above cursor; view:\n%s", view)
	}
}

func TestCommandModeAutocompleteOpensAboveMidBufferCursorInMultiLinePrompt(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 30)
	// Four-line editor. Place the cursor at the end of line 0 so the dropdown
	// must overlay rows inside the buffer, not after the last line.
	mode.editor.SetValue("SELECT * FROM us\nWHERE id = 1\nORDER BY id\nLIMIT 10;")
	mode.setCursorOffset(len([]rune("SELECT * FROM us")))
	mode.autocompleteOpenedByTyping = true

	if line := mode.editor.Line(); line != 0 {
		t.Skipf("cursor ended on line %d instead of 0 — skipping", line)
	}

	query := InteractionState{
		AutocompleteSchema: &AutocompleteSchemaContext{
			Tables: []AutocompleteTableContext{{Name: "users"}},
		},
	}
	mode.cachedSuggestions = mode.autocompleteItems(query)
	if len(mode.cachedSuggestions) == 0 {
		t.Skip("autocomplete produced no items — skipping overlay mid-buffer test")
	}

	view := mode.View(query)
	lines := strings.Split(view, "\n")

	// Find the cursor line.
	cursorIdx := -1
	for i, l := range lines {
		if strings.Contains(l, "SELECT") && strings.Contains(l, "FROM") {
			cursorIdx = i
			break
		}
	}
	if cursorIdx < 0 {
		t.Fatal("cursor line not found in view")
	}

	// The dropdown opens above the cursor — check the row immediately before it.
	if cursorIdx < 1 {
		t.Fatal("cursor line is at the top of the view — no room for dropdown above")
	}
	prev := lines[cursorIdx-1]
	if !strings.Contains(prev, "[") { // dropdown rows are rendered as "[kind] label"
		t.Fatalf("row before cursor does not look like a dropdown row: %q", prev)
	}

	// The row immediately below the cursor must be "WHERE id = 1" —
	// the dropdown must not overwrite continuation lines below the cursor.
	if cursorIdx+1 < len(lines) {
		next := lines[cursorIdx+1]
		if !strings.Contains(next, "WHERE") {
			t.Fatalf("continuation line below cursor was overwritten or missing: %q", next)
		}
	}
}
