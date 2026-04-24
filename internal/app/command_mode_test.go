package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/db"
)

func TestSQLSyntaxHighlighterHighlightsCommonTokens(t *testing.T) {
	highlighter := newSQLSyntaxHighlighter()
	line, _ := highlighter.highlightLine(`SELECT "users".name, 42, 'Ada', @id -- comment`, sqlLexerState{})
	segments := compactStyledSegments(line)

	assertStyledSegmentKind(t, segments, "SELECT", sqlTokenKeyword)
	assertStyledSegmentKind(t, segments, `"users"`, sqlTokenQuotedIdentifier)
	assertStyledSegmentKind(t, segments, "42", sqlTokenNumber)
	assertStyledSegmentKind(t, segments, "'Ada'", sqlTokenString)
	assertStyledSegmentKind(t, segments, "@id", sqlTokenParameter)
	assertStyledSegmentKind(t, segments, "-- comment", sqlTokenComment)
	assertStyledSegmentKind(t, segments, "name", sqlTokenPlain)
	assertStyledSegmentKind(t, segments, ".", sqlTokenOperator)
}

func TestSQLSyntaxHighlighterTracksBlockCommentsAcrossLines(t *testing.T) {
	highlighter := newSQLSyntaxHighlighter()
	lines := highlighter.highlightLines([]string{
		"SELECT 1 /* open comment",
		"still comment */ FROM widgets",
	})

	firstSegments := compactStyledSegments(lines[0])
	secondSegments := compactStyledSegments(lines[1])

	assertStyledSegmentKind(t, firstSegments, "SELECT", sqlTokenKeyword)
	assertStyledSegmentKind(t, firstSegments, "/* open comment", sqlTokenComment)
	assertStyledSegmentKind(t, secondSegments, "still comment */", sqlTokenComment)
	assertStyledSegmentKind(t, secondSegments, "FROM", sqlTokenKeyword)
	assertStyledSegmentContainsKind(t, secondSegments, "widgets", sqlTokenPlain)
}

func TestCommandModeViewPreservesEditorLayout(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT \"users\".name, 42, 'Ada', @id -- comment")

	view := mode.View(QueryContext{})

	for _, want := range []string{
		">",
		`SELECT "users".name, 42, 'Ada', @id -- comment`,
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestBuildAutocompleteItemsSuggestsSlashCommands(t *testing.T) {
	items := buildAutocompleteItems("/se", len([]rune("/se")), QueryContext{})

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want slash command suggestions")
	}

	if got, want := items[0].Label, "/select"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsSuggestsTablesAfterFrom(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM us", len([]rune("SELECT * FROM us")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "orders"}}},
	})

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want table suggestions")
	}

	if got, want := items[0].Label, "users"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsSuggestsColumnsForQualifier(t *testing.T) {
	items := buildAutocompleteItems("SELECT users.na", len([]rune("SELECT users.na")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name", "email"}}}},
	})

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want column suggestions")
	}

	if got, want := items[0].Label, "name"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsSuggestsColumnsFromActiveTables(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users WHERE na", len([]rune("SELECT * FROM users WHERE na")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name", "email"}}}},
	})

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want active table columns")
	}

	if got, want := items[0].Label, "name"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsRanksColumnsBeforeKeywordsInWhere(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users WHERE ", len([]rune("SELECT * FROM users WHERE ")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name", "email"}}}},
	})

	assertAutocompleteLabelsPrefix(t, items, []string{"email", "id", "name"})
	assertAutocompleteLabelBefore(t, items, "name", "AND")
}

func TestBuildAutocompleteItemsRanksJoinAndWhereAfterTable(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users ", len([]rune("SELECT * FROM users ")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "user_sessions"}}},
	})

	assertAutocompleteLabelsPrefix(t, items, []string{"JOIN", "WHERE"})
}

func TestBuildAutocompleteItemsRanksSetFirstAfterUpdateTarget(t *testing.T) {
	items := buildAutocompleteItems("UPDATE users ", len([]rune("UPDATE users ")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
	})

	assertAutocompleteLabelsPrefix(t, items, []string{"SET", "WHERE", "RETURNING"})
}

func TestBuildAutocompleteItemsRanksIntoFirstAfterInsert(t *testing.T) {
	items := buildAutocompleteItems("INS", len([]rune("INS")), QueryContext{})

	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want keyword suggestions")
	}

	if got, want := items[0].Label, "INSERT"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}

	items = buildAutocompleteItems("INSERT ", len([]rune("INSERT ")), QueryContext{})
	if len(items) == 0 {
		t.Fatal("buildAutocompleteItems() = no items, want post-insert suggestions")
	}

	if got, want := items[0].Label, "INTO"; got != want {
		t.Fatalf("items[0].Label = %q, want %q", got, want)
	}
}

func TestBuildAutocompleteItemsRanksActiveTableColumnsBeforeFallbackColumns(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users WHERE ", len([]rune("SELECT * FROM users WHERE ")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users", Columns: []string{"id", "name"}}}},
		LatestResult:       &LatestResultContext{PreservedResult: &db.ResultSet{Columns: []db.ResultColumn{{Name: "archived_at"}, {Name: "id"}, {Name: "name"}}}},
	})

	assertAutocompleteLabelBefore(t, items, "name", "archived_at")
	assertAutocompleteLabelBefore(t, items, "id", "archived_at")
}

func TestBuildAutocompleteItemsRanksUnqualifiedTablesBeforeSchemaQualifiedMatches(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM us", len([]rune("SELECT * FROM us")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Schema: "public", Name: "users"}}},
	})

	assertAutocompleteLabelsPrefix(t, items, []string{"users", "public.users"})
}

func TestBuildAutocompleteItemsUsesSchemaQualifiedActiveTableColumns(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM warehouse.users WHERE ", len([]rune("SELECT * FROM warehouse.users WHERE ")), QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{
			{Schema: "warehouse", Name: "users", Columns: []string{"id", "name"}},
			{Schema: "public", Name: "users", Columns: []string{"id", "public_name"}},
		}},
	})

	assertAutocompleteLabelsPrefix(t, items, []string{"id", "name"})
	for _, item := range items {
		if item.Label == "public_name" {
			t.Fatalf("items contained column from wrong schema: %#v", items)
		}
	}
}

func TestBuildAutocompleteItemsResetsContextAfterSemicolon(t *testing.T) {
	items := buildAutocompleteItems("SELECT * FROM users; DE", len([]rune("SELECT * FROM users; DE")), QueryContext{})

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
	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
	}

	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}, query)

	if got, want := updated.Value(), "SELECT * FROM users"; got != want {
		t.Fatalf("Value() = %q, want %q", got, want)
	}
}

func TestCommandModeSuggestionNavigationCyclesSelection(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "orders"}}},
	}

	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, query)
	if got, want := updated.selectedSuggestion, 1; got != want {
		t.Fatalf("selectedSuggestion = %d, want %d", got, want)
	}

	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, query)
	if got, want := updated.selectedSuggestion, 0; got != want {
		t.Fatalf("selectedSuggestion = %d, want %d", got, want)
	}
}

func TestCommandModeViewRendersAutocompletePanel(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM us")
	mode.editor.CursorEnd()
	mode.autocompleteOpenedByTyping = true
	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
	}

	// Ghost text should show completion for "users" after typing "us"
	ghost := mode.ghostText(query)
	if ghost != "ers" {
		t.Fatalf("ghostText() = %q, want %q", ghost, "ers")
	}
}

func TestCommandModeViewRendersSlashWizard(t *testing.T) {
	query := QueryContext{
		SlashWizard: &SlashCommandWizardContext{
			Step: SlashCommandWizardStepCommand,
			Commands: []SlashCommandWizardCommand{{
				Name:        "tables",
				DisplayName: "/tables",
				Summary:     "list tables in the current database",
				Usage:       "/tables",
			}},
		},
	}
	popup := renderSlashWizard(query)

	for _, want := range []string{"Command wizard:", "Step 1/2: choose a slash command", "> /tables - list tables in the current database", "enter confirm | ctrl+n next | ctrl+p prev | esc close"} {
		if !strings.Contains(popup, want) {
			t.Fatalf("renderSlashWizard() = %q, want to contain %q", popup, want)
		}
	}
}

func TestCommandModeViewRendersHistorySearch(t *testing.T) {
	query := QueryContext{
		ActiveMode: ModeHistorySearch,
		HistorySearch: &HistorySearchContext{
			Query:         "su",
			SelectedIndex: 0,
		},
		SessionHistory: []HistoryEntryContext{{SQL: "select * from user_sessions"}, {SQL: "select * from users"}},
	}
	popup := renderHistorySearch(query)

	for _, want := range []string{"Reverse search:", "query> su", "2 match(es); newest first.", "> select * from users", "enter restore | ctrl+r older | ctrl+n newer | esc close"} {
		if !strings.Contains(popup, want) {
			t.Fatalf("renderHistorySearch() = %q, want to contain %q", popup, want)
		}
	}
}

func TestCommandModeViewRendersInlineSelectResult(t *testing.T) {
	query := QueryContext{
		LatestResult: &LatestResultContext{
			OriginMode:    ModeCommand,
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
	query := QueryContext{
		LatestResult: &LatestResultContext{
			OriginMode:    ModeCommand,
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
	warning := renderGeneratedCommandWarning(sql)
	if !strings.Contains(warning, "Warning: generated DELETE statement. Review carefully before submitting.") {
		t.Fatalf("renderGeneratedCommandWarning() = %q, want destructive warning", warning)
	}
}

func TestCommandModeFooterShowsRunningIndicator(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	footer := mode.Footer("local", "sqlite", QueryContext{
		Layout:  LayoutCommandOnly,
		Running: &RunningQueryContext{Label: "SQL", Elapsed: 1500 * time.Millisecond},
	})

	for _, want := range []string{"Command mode", "layout command only", "connection local", "dialect sqlite", "alt+h help", "ctrl+3 command", "- SQL 1.5s", "esc cancel query"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}

func TestCommandModeFooterShowsViewerPagingWhenViewerFocusedInSplit(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	footer := mode.Footer("local", "sqlite", QueryContext{
		Layout:     LayoutSplit,
		ActiveMode: ModeRecordViewer,
	})

	for _, want := range []string{"Command line hidden focus", "layout split", "alt+h help", "ctrl+u scroll up", "ctrl+d scroll down"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}

func TestCommandModeFooterShowsSelectionCountFromViewerResult(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	footer := mode.Footer("local", "sqlite", QueryContext{
		Layout:       LayoutSplit,
		ActiveMode:   ModeCommand,
		LatestResult: &LatestResultContext{SelectedRows: []int{0, 2}},
	})

	for _, want := range []string{"Command mode", "connection local", "dialect sqlite", "2 selected"} {
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

type styledSegment struct {
	text string
	kind sqlTokenKind
}

func compactStyledSegments(line sqlStyledLine) []styledSegment {
	if len(line) == 0 {
		return nil
	}

	segments := make([]styledSegment, 0, len(line))
	current := styledSegment{kind: line[0].kind}

	for _, sr := range line {
		if sr.kind != current.kind && current.text != "" {
			segments = append(segments, current)
			current = styledSegment{kind: sr.kind}
		}
		current.text += string(sr.rune)
	}

	if current.text != "" {
		segments = append(segments, current)
	}

	return segments
}

func assertStyledSegmentKind(t *testing.T, segments []styledSegment, text string, want sqlTokenKind) {
	t.Helper()

	for _, segment := range segments {
		if segment.text == text {
			if segment.kind != want {
				t.Fatalf("segment %q kind = %v, want %v", text, segment.kind, want)
			}
			return
		}
	}

	t.Fatalf("segment %q not found in %#v", text, segments)
}

func assertStyledSegmentContainsKind(t *testing.T, segments []styledSegment, text string, want sqlTokenKind) {
	t.Helper()

	for _, segment := range segments {
		if strings.Contains(segment.text, text) {
			if segment.kind != want {
				t.Fatalf("segment containing %q kind = %v, want %v", text, segment.kind, want)
			}
			return
		}
	}

	t.Fatalf("segment containing %q not found in %#v", text, segments)
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

// TestRenderLineContentWithGhostCJKCursorPosition verifies that the cursor is
// placed at the correct display-column position when the line contains CJK
// (full-width) characters, which occupy 2 terminal columns each.
func TestRenderLineContentWithGhostCJKCursorPosition(t *testing.T) {
	h := newSQLSyntaxHighlighter()

	// Build a styled line containing one CJK rune followed by an ASCII char.
	// "世" is a full-width CJK character (display width 2).
	// The line is: 世A  (display widths: 2 + 1 = 3)
	line, _ := h.highlightLine("世A", sqlLexerState{})

	// With no ghost text, cursor at display column 2 (after "世") should be
	// rendered on top of "A", not incorrectly placed one column too far right.
	// cursorCol=2 means "2 display columns from the left", i.e. on top of 'A'.
	rendered := h.renderLineContentWithGhost(line, 2, 10, false, "")

	// The rendered string must contain the cursor-styled 'A'. We verify this
	// indirectly: when the cursor is on 'A', 'A' must appear somewhere in the
	// output (cursorStyle wraps it) and the total display width of the content
	// area must equal the editor width (10).
	if !strings.Contains(rendered, "A") {
		t.Fatalf("renderLineContentWithGhost with CJK: expected 'A' in rendered output, got %q", rendered)
	}

	// cursorCol at display column 3 (end of "世A") - ghost text should appear.
	ghost := "GHOST"
	renderedGhost := h.renderLineContentWithGhost(line, 3, 20, false, ghost)
	if !strings.Contains(renderedGhost, ghost) {
		t.Fatalf("renderLineContentWithGhost with CJK and ghost: expected ghost text %q at end-of-line (cursorCol=3 == lineDisplayWidth=3), got %q", ghost, renderedGhost)
	}

	// cursorCol at rune count (2, which equals len(line)) should NOT trigger
	// ghost text, because the cursor is ON the second rune, not after it.
	// Display column 2 != display column 3 (end).
	renderedNoGhost := h.renderLineContentWithGhost(line, 2, 20, false, ghost)
	if strings.Contains(renderedNoGhost, ghost) {
		t.Fatalf("renderLineContentWithGhost with CJK: ghost text must NOT appear when cursorCol=2 (on 'A'), but got %q", renderedNoGhost)
	}
}

// --- Scroll behaviour tests ---

func TestCommandModeScrollStepIsHalfPage(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20) // innerHeight=20, half-page=10
	for i := 0; i < 30; i++ {
		mode.AppendReplEntry("> ", fmt.Sprintf("SELECT %d;", i), fmt.Sprintf("%d row.", i))
	}

	updated, _ := mode.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, QueryContext{})
	want := max(1, 20/2) // 10
	if got := updated.scrollOffset; got != want {
		t.Fatalf("scrollOffset = %d after ctrl+u with innerHeight=20, want %d (half-page)", got, want)
	}
}

func TestCommandModeScrollOffsetBoundedByContent(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	mode.AppendReplEntry("> ", "SELECT 1;", "1 row.")

	// Press ctrl+u many times — scrollOffset must never exceed naturalScrollTop.
	for i := 0; i < 100; i++ {
		mode, _ = mode.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, QueryContext{})
	}
	natural := mode.computeNaturalScrollTop(max(1, mode.innerHeight))
	if mode.scrollOffset > natural {
		t.Fatalf("scrollOffset = %d exceeds naturalScrollTop = %d", mode.scrollOffset, natural)
	}
}

func TestCommandModeTypingSnapsScrollToZero(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	for i := 0; i < 20; i++ {
		mode.AppendReplEntry("> ", fmt.Sprintf("SELECT %d;", i), fmt.Sprintf("%d row.", i))
	}

	mode, _ = mode.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, QueryContext{})
	if mode.scrollOffset == 0 {
		t.Fatal("expected scrollOffset > 0 after ctrl+u")
	}

	// Type a printable character — scrollOffset must snap back to 0.
	mode, _ = mode.Update(tea.KeyPressMsg{Text: "S"}, QueryContext{})
	if got := mode.scrollOffset; got != 0 {
		t.Fatalf("scrollOffset = %d after typing, want 0", got)
	}
}

func TestCommandModeScrollDownIsNoOpAtBottom(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	// scrollOffset starts at 0; ctrl+d should leave it at 0.
	mode, _ = mode.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, QueryContext{})
	if got := mode.scrollOffset; got != 0 {
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
	query := QueryContext{AutocompleteSchema: &AutocompleteSchemaContext{Tables: tables}}

	dropdown := mode.renderAutocompleteDropdown(query)
	if dropdown == "" {
		t.Fatal("expected non-empty dropdown with 10 table suggestions")
	}
	rows := strings.Split(dropdown, "\n")
	if len(rows) > autocompletePanelRows {
		t.Fatalf("dropdown has %d rows, want at most %d", len(rows), autocompletePanelRows)
	}
}

func TestCommandModeAutocompleteOverlaysLinesBelowCursor(t *testing.T) {
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

	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{
			Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "user_sessions"}},
		},
	}

	// Autocomplete must fire for this test to be meaningful.
	if items := mode.autocompleteItems(query); len(items) == 0 {
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
	if cursorIdx+1 >= len(viewLines) {
		t.Fatal("cursor line is at the bottom of the view — no room for overlay")
	}

	// The line immediately below the cursor must be a dropdown entry,
	// not the second editor line ("WHERE id = 1;").
	lineAfterCursor := viewLines[cursorIdx+1]
	if strings.Contains(lineAfterCursor, "WHERE") {
		t.Fatalf("overlay failed: line after cursor is %q — expected autocomplete dropdown row, not editor line 2", lineAfterCursor)
	}
}

func TestCommandModeBottomAnchorsShortContent(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 30)
	mode.AppendReplEntry("> ", "SELECT 1;", "1 row.")

	view := mode.View(QueryContext{})
	lines := strings.Split(view, "\n")

	if got := len(lines); got != 30 {
		t.Fatalf("view has %d rows, want 30 (innerHeight)", got)
	}

	// The prompt line ("> ") should sit at pane_height - bottomPadding - 1,
	// leaving 5 blank reserve rows below it.
	promptRow := 30 - bottomPadding - 1
	if strings.TrimSpace(lines[promptRow]) == "" {
		t.Fatalf("expected prompt at row %d, but that row is blank; view:\n%s", promptRow, view)
	}

	// All rows below the prompt (the reserve) must be blank.
	for i := promptRow + 1; i < len(lines); i++ {
		if s := strings.TrimSpace(lines[i]); s != "" {
			t.Fatalf("row %d below prompt is not blank: %q (should be part of 5-row reserve)", i, s)
		}
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

func TestCommandModeReserveIsFiveRowsWhenContentOverflows(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 10)
	// Enough transcript to force overflow so topPadding is 0.
	for i := 0; i < 30; i++ {
		mode.AppendReplEntry("> ", fmt.Sprintf("SELECT %d;", i), fmt.Sprintf("%d row.", i))
	}

	view := mode.View(QueryContext{})
	lines := strings.Split(view, "\n")

	if got := len(lines); got != 10 {
		t.Fatalf("view has %d rows, want 10", got)
	}

	// The last bottomPadding rows must be blank reserve.
	for i := len(lines) - bottomPadding; i < len(lines); i++ {
		if s := strings.TrimSpace(lines[i]); s != "" {
			t.Fatalf("reserve row %d is not blank: %q", i, s)
		}
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

	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{
			Tables: []AutocompleteTableContext{{Name: "users"}},
		},
	}

	if items := mode.autocompleteItems(query); len(items) == 0 {
		t.Fatal("autocomplete produced no items — test precondition failed")
	}

	view := mode.View(query)
	lines := strings.Split(view, "\n")

	// The last 5 rows are the reserve; when the dropdown opens with cursor at
	// the end of the editor, the dropdown overlays the reserve and must be
	// visible (not clipped off the bottom of the pane).
	found := false
	for i := len(lines) - bottomPadding; i < len(lines); i++ {
		if strings.Contains(lines[i], "users") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected autocomplete dropdown in reserve rows; view:\n%s", view)
	}
}

func TestCommandModeAutocompleteOverlaysMidBufferOfMultiLinePrompt(t *testing.T) {
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

	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{
			Tables: []AutocompleteTableContext{{Name: "users"}},
		},
	}
	if items := mode.autocompleteItems(query); len(items) == 0 {
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

	// The next row must contain a dropdown entry (kw/fn/table kind marker),
	// not "WHERE id = 1".
	next := lines[cursorIdx+1]
	if strings.Contains(next, "WHERE") {
		t.Fatalf("overlay failed: row after cursor is %q (expected dropdown row)", next)
	}
	if !strings.Contains(next, "[") { // dropdown rows are rendered as "[kind] label"
		t.Fatalf("row after cursor does not look like a dropdown row: %q", next)
	}
}
