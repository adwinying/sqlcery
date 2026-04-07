package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	mode.syncScroll()

	view := mode.View(QueryContext{})

	for _, want := range []string{
		"sql>",
		"1",
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
	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
	}

	updated, _ := mode.Update(tea.KeyMsg{Type: tea.KeyCtrlY}, query)

	if got, want := updated.Value(), "SELECT * FROM users"; got != want {
		t.Fatalf("Value() = %q, want %q", got, want)
	}
}

func TestCommandModeSuggestionNavigationCyclesSelection(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM ")
	mode.editor.CursorEnd()
	query := QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}, {Name: "orders"}}},
	}

	updated, _ := mode.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}, Alt: true}, query)
	if got, want := updated.selectedSuggestion, 1; got != want {
		t.Fatalf("selectedSuggestion = %d, want %d", got, want)
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}, query)
	if got, want := updated.selectedSuggestion, 0; got != want {
		t.Fatalf("selectedSuggestion = %d, want %d", got, want)
	}
}

func TestCommandModeViewRendersAutocompletePanel(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	mode.editor.SetValue("SELECT * FROM us")
	mode.editor.CursorEnd()
	view := mode.View(QueryContext{
		AutocompleteSchema: &AutocompleteSchemaContext{Tables: []AutocompleteTableContext{{Name: "users"}}},
	})

	for _, want := range []string{"Suggestions:", "> [tbl] users - table"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestCommandModeViewRendersSlashWizard(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	view := mode.View(QueryContext{
		SlashWizard: &SlashCommandWizardContext{
			Step: SlashCommandWizardStepCommand,
			Commands: []SlashCommandWizardCommand{{
				Name:        "tables",
				DisplayName: "/tables",
				Summary:     "list tables in the current database",
				Usage:       "/tables",
			}},
		},
	})

	for _, want := range []string{"Command wizard:", "Step 1/2: choose a slash command", "> /tables - list tables in the current database", "ctrl+g confirm | alt+n next | alt+p prev | esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestCommandModeViewRendersInlineSelectResult(t *testing.T) {
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	view := mode.View(QueryContext{
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
	})

	for _, want := range []string{"Results:", "id | name | created_at", "1  | Ada  | 2026-04-07T11:30:00Z", "1 row."} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestCommandModeViewRendersInlineExecResult(t *testing.T) {
	rowsAffected := int64(2)
	lastInsertID := int64(9)
	mode := newCommandModeModel()
	mode.SetSize(80, 20)
	view := mode.View(QueryContext{
		LatestResult: &LatestResultContext{
			OriginMode:    ModeCommand,
			StatementKind: db.StatementResultKindExec,
			RowsAffected:  &rowsAffected,
			LastInsertID:  &lastInsertID,
		},
	})

	for _, want := range []string{"Results:", "2 rows affected", "last insert id 9"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
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
