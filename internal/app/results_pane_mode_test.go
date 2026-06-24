package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/adwinying/sqlcery/internal/tui"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestResultsPaneModeViewAcceptsResultsPaneViewContext(t *testing.T) {
	mode := newResultsPaneModeModel()
	mode.SetSize(80, 8)

	interaction := InteractionState{
		LatestResult: &LatestResultContext{
			Statement: "select id, name from widgets",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}}},
				},
			},
		},
	}

	ctx := mode.buildViewContext(interaction)
	view := ansi.Strip(mode.View(ctx))

	for _, want := range []string{"id", "name", "1", "Ada"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View(ctx) = %q, want to contain %q", view, want)
		}
	}
}

func TestResultsPaneModeViewRendersFullResultSet(t *testing.T) {
	mode := newResultsPaneModeModel()
	mode.SetSize(80, 8)

	interaction := InteractionState{
		LatestResult: &LatestResultContext{
			Statement: "select id, name, created_at from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}, {Name: "created_at"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 7, 11, 30, 0, 0, time.UTC)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "Grace"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 8, 9, 0, 0, 0, time.UTC)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(3)}, {Kind: db.ValueKindString, Value: "Linus"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 9, 15, 45, 0, 0, time.UTC)}}},
				},
			},
		},
	}
	view := mode.View(mode.buildViewContext(interaction))

	plainView := ansi.Strip(view)

	for _, want := range []string{
		"id | name  | created_at",
		"1  | Ada   | 2026-04-07 11:30:00",
		"2  | Grace | 2026-04-08 09:00:00",
		"3  | Linus | 2026-04-09 15:45:00",
	} {
		if !strings.Contains(plainView, want) {
			t.Fatalf("View() = %q, want to contain %q", plainView, want)
		}
	}

}

func TestResultsPaneModeViewRendersSelectedPage(t *testing.T) {
	mode := newResultsPaneModeModel()
	mode.SetSize(80, 12)

	rows := make([]db.ResultRow, 0, 305)
	for i := 1; i <= 305; i++ {
		rows = append(rows, db.ResultRow{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(i)}}})
	}

	interaction := InteractionState{
		ResultsPanePage: 1,
		LatestResult: &LatestResultContext{
			Statement: "select id from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}},
				Rows:    rows,
			},
		},
	}
	view := mode.View(mode.buildViewContext(interaction))

	for _, want := range []string{
		"301",
		"305",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}

	for _, unwanted := range []string{"300", "299"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("View() = %q, want page-local rows only and not %q", view, unwanted)
		}
	}
}

func TestResultsPaneModeViewClipsRowsToVisibleViewport(t *testing.T) {
	mode := newResultsPaneModeModel()
	mode.SetSize(80, 14)
	mode.selectedRow = 15
	mode.viewportStart = 9 // scrolloffViewport(15, 0, visibleRows=12, totalRows=30, scrolloff=5) = 9
	mode.selectionActive = true

	rows := make([]db.ResultRow, 0, 30)
	for i := 1; i <= 30; i++ {
		rows = append(rows, db.ResultRow{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: fmt.Sprintf("row-%03d", i)}}})
	}

	interaction := InteractionState{
		LatestResult: &LatestResultContext{
			Statement: "select label from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "label"}},
				Rows:    rows,
			},
		},
	}
	view := ansi.Strip(mode.View(mode.buildViewContext(interaction)))

	for _, want := range []string{"row-014", "row-018"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}

	for _, unwanted := range []string{"row-001", "row-030"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("View() = %q, want viewport clipping to exclude %q", view, unwanted)
		}
	}
}

func TestScrolloffViewportKeepsGuardZone(t *testing.T) {
	// scrolloffViewport(pageRow, vp, visibleRows, totalRows, scrolloff)
	// With 30 rows, visibleRows=12, scrolloff=5:
	// moving down from vp=0 should not scroll until cursor enters the bottom guard zone.

	// Cursor at row 6 (0-indexed): within free zone [5, 7), vp stays 0.
	if got := scrolloffViewport(6, 0, 12, 30, 5); got != 0 {
		t.Fatalf("scrolloffViewport(cursor=6, vp=0, ...) = %d, want 0 (cursor in free zone)", got)
	}

	// Cursor at row 7 (= vp+visibleRows-scrolloff = 0+12-5): enters bottom guard, vp must advance.
	if got := scrolloffViewport(7, 0, 12, 30, 5); got != 1 {
		t.Fatalf("scrolloffViewport(cursor=7, vp=0, ...) = %d, want 1 (bottom guard entered)", got)
	}

	// Cursor at row 4 (< vp+scrolloff = 10+5): enters top guard from vp=10.
	if got := scrolloffViewport(4, 10, 12, 30, 5); got != 4-5 {
		// clamped to 0 since 4-5 = -1 < 0
		want := max(0, 4-5)
		if got != want {
			t.Fatalf("scrolloffViewport(cursor=4, vp=10, ...) = %d, want %d (top guard, clamped)", got, want)
		}
	}

	// At page boundary: cursor at last row (29), vp must be totalRows-visibleRows=18.
	if got := scrolloffViewport(29, 0, 12, 30, 5); got != 18 {
		t.Fatalf("scrolloffViewport(cursor=29, vp=0, ...) = %d, want 18 (clamped at bottom)", got)
	}
}

func TestComposeResultsPaneInsertSQLUsesVisibleColumns(t *testing.T) {
	result, err := composeResultsPaneInsertSQL(db.PostgresDialect(), &LatestResultContext{
		Statement: "select id, name, active from public.users order by id;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Namespace: "public", Name: "users"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "name"},
				{Name: "active"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindBool, Value: true}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeResultsPaneInsertSQL() error = %v", err)
	}
	if got, want := result.Action, resultsPaneComposeActionInsert; got != want {
		t.Fatalf("Action = %q, want %q", got, want)
	}
	for _, want := range []string{
		"INSERT INTO \"public\".\"users\"",
		"\"id\"",
		"\"name\"",
		"\"active\"",
		"7",
		"'Ada'",
		"TRUE",
	} {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
}

func TestComposeResultsPaneInsertSQLFallsBackToVisibleColumnsWithQuotedValues(t *testing.T) {
	result, err := composeResultsPaneInsertSQL(db.SQLiteDialect(), &LatestResultContext{
		Statement: "select name, note, payload from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}, {Name: "note"}, {Name: "payload"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "Ada's"}, {Kind: db.ValueKindNull}, {Kind: db.ValueKindBytes, Value: []byte{0xde, 0xad}}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeResultsPaneInsertSQL() error = %v", err)
	}
	for _, want := range []string{
		"INSERT INTO \"widgets\"",
		"\"name\"",
		"\"note\"",
		"\"payload\"",
		"'Ada''s'",
		"NULL",
		"X'dead'",
	} {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
}

func TestComposeResultsPaneDeleteSQLUsesPrimaryKeys(t *testing.T) {
	result, err := composeResultsPaneDeleteSQL(db.PostgresDialect(), &LatestResultContext{
		Statement: "select id, name from public.users order by id;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Namespace: "public", Name: "users"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "name"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeResultsPaneDeleteSQL() error = %v", err)
	}
	if !result.UsedPrimaryKeys {
		t.Fatal("UsedPrimaryKeys = false, want true")
	}
	if got, want := result.Action, resultsPaneComposeActionDelete; got != want {
		t.Fatalf("Action = %q, want %q", got, want)
	}
	for _, want := range []string{
		"DELETE FROM \"public\".\"users\"",
		"\"id\" = 7",
	} {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
	if containsLine(result.SQL, "\"name\" = 'Ada'") {
		t.Fatalf("SQL = %q, want DELETE to omit non-predicate assignments", result.SQL)
	}
}

func TestComposeResultsPaneDeleteSQLFallsBackToVisibleColumnPredicate(t *testing.T) {
	result, err := composeResultsPaneDeleteSQL(db.SQLiteDialect(), &LatestResultContext{
		Statement: "select name, note from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}, {Name: "note"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "Ada's"}, {Kind: db.ValueKindNull}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeResultsPaneDeleteSQL() error = %v", err)
	}
	if result.UsedPrimaryKeys {
		t.Fatal("UsedPrimaryKeys = true, want false")
	}
	for _, want := range []string{
		"DELETE FROM \"widgets\"",
		"\"name\" = 'Ada''s'",
		"\"note\" IS NULL",
	} {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
}

func TestComposeResultsPaneUpdateSQLUsesPrimaryKeys(t *testing.T) {
	result, err := composeResultsPaneUpdateSQL(db.PostgresDialect(), &LatestResultContext{
		Statement: "select id, name, active from public.users order by id;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Namespace: "public", Name: "users"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "name"},
				{Name: "active"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindBool, Value: true}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeResultsPaneUpdateSQL() error = %v", err)
	}
	if !result.UsedPrimaryKeys {
		t.Fatal("UsedPrimaryKeys = false, want true")
	}
	for _, want := range []string{
		"UPDATE \"public\".\"users\"",
		"\"name\" = 'Ada'",
		"\"active\" = TRUE",
		"\"id\" = 7",
	} {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
	if containsLine(result.SQL, "\"id\" = 7,") {
		t.Fatalf("SQL = %q, want primary key omitted from SET list", result.SQL)
	}
}

func TestComposeResultsPaneUpdateSQLRejectsNoPrimaryKeys(t *testing.T) {
	_, err := composeResultsPaneUpdateSQL(db.SQLiteDialect(), &LatestResultContext{
		Statement: "select name, note, payload from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}, {Name: "note"}, {Name: "payload"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "Ada's"}, {Kind: db.ValueKindNull}, {Kind: db.ValueKindBytes, Value: []byte{0xde, 0xad}}}}},
		},
	}, 0)
	if err == nil {
		t.Fatal("composeResultsPaneUpdateSQL() error = nil, want error when no primary key columns")
	}
}

func TestInferQuerySourceTableReturnsNilForJoins(t *testing.T) {
	if got := inferQuerySourceTable("select widgets.id from widgets join owners on owners.id = widgets.owner_id;"); got != nil {
		t.Fatalf("inferQuerySourceTable(join) = %#v, want nil", got)
	}
}

func TestRenderResultsPaneTableOnlyStylesPrimaryKeyColumns(t *testing.T) {
	table := renderResultsPaneTable(&db.ResultSet{
		Columns: []db.ResultColumn{
			{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
			{Name: "name"},
		},
		Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
	}, 0, 80, 10, tui.ResultsPaneRenderState{})

	plain := ansi.Strip(table)
	if !strings.Contains(plain, "id | name") {
		t.Fatalf("renderResultsPaneTable() = %q, want id and name columns in header", plain)
	}
	if !strings.Contains(plain, "7  | Ada") {
		t.Fatalf("renderResultsPaneTable() = %q, want row values aligned", plain)
	}
}

func TestClampResultsPanePage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		page      int
		totalRows int
		want      int
	}{
		{name: "empty rows", page: 4, totalRows: 0, want: 0},
		{name: "negative page", page: -1, totalRows: 305, want: 0},
		{name: "within bounds", page: 1, totalRows: 305, want: 1},
		{name: "beyond end", page: 9, totalRows: 305, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tui.ClampResultsPanePage(tc.page, tc.totalRows); got != tc.want {
				t.Fatalf("ClampResultsPanePage(%d, %d) = %d, want %d", tc.page, tc.totalRows, got, tc.want)
			}
		})
	}
}

func TestResultsPaneModeNavigateSupportsArrowsAndHJKL(t *testing.T) {
	mode := newResultsPaneModeModel()
	query := InteractionState{
		LatestResult: &LatestResultContext{
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "Grace"}}},
				},
			},
		},
	}

	if page, handled := mode.Navigate(tea.KeyPressMsg{Code: tea.KeyRight}, query); !handled || page != 0 {
		t.Fatalf("Navigate(right) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedColumn, 1; got != want {
		t.Fatalf("selectedColumn = %d, want %d", got, want)
	}

	if page, handled := mode.Navigate(tea.KeyPressMsg{Code: tea.KeyDown}, query); !handled || page != 0 {
		t.Fatalf("Navigate(down) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedRow, 1; got != want {
		t.Fatalf("selectedRow = %d, want %d", got, want)
	}

	if page, handled := mode.Navigate(tea.KeyPressMsg{Text: "h"}, query); !handled || page != 0 {
		t.Fatalf("Navigate(h) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedColumn, 0; got != want {
		t.Fatalf("selectedColumn = %d, want %d", got, want)
	}

	if page, handled := mode.Navigate(tea.KeyPressMsg{Text: "k"}, query); !handled || page != 0 {
		t.Fatalf("Navigate(k) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedRow, 0; got != want {
		t.Fatalf("selectedRow = %d, want %d", got, want)
	}
}

func TestRenderResultsPaneTableHighlightsSelectedCell(t *testing.T) {
	table := renderResultsPaneTable(&db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
		Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
	}, 0, 80, 10, tui.ResultsPaneRenderState{Active: tui.ResultsPaneSelection{Row: 0, Column: 1, Active: true}})

	plain := ansi.Strip(table)
	if !strings.Contains(plain, "7  | Ada") && !strings.Contains(plain, "7 | Ada") {
		t.Fatalf("renderResultsPaneTable() = %q, want selected cell text preserved", plain)
	}
}

func TestResultsPaneBorderCounterShowsSelectedRowCount(t *testing.T) {
	interaction := InteractionState{
		MarkedRows: []int{0, 2},
		LatestResult: &LatestResultContext{
			Statement: "select id from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(3)}}},
				},
			},
		},
	}
	counter := resultsPaneBorderCounter(interaction)

	if !strings.Contains(counter, "2 selected") {
		t.Fatalf("resultsPaneBorderCounter() = %q, want to contain %q", counter, "2 selected")
	}
	if !strings.Contains(counter, "Rows 1-3 of 3") {
		t.Fatalf("resultsPaneBorderCounter() = %q, want to contain %q", counter, "Rows 1-3 of 3")
	}
}

func TestResultsPaneModeToggleSelectedRowTracksMultipleRows(t *testing.T) {
	mode := newResultsPaneModeModel()
	query := InteractionState{
		LatestResult: &LatestResultContext{
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}}},
				},
			},
		},
	}

	row, newMarked, selected, handled := mode.ToggleSelectedRow(query)
	if !handled || !selected || row != 0 {
		t.Fatalf("ToggleSelectedRow(first) = (%d, %t, %t), want (0, true, true)", row, selected, handled)
	}
	if got, want := newMarked, []int{0}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("newMarked = %#v, want %#v", got, want)
	}
	query.MarkedRows = newMarked

	mode.selectedRow = 1
	row, newMarked, selected, handled = mode.ToggleSelectedRow(query)
	if !handled || !selected || row != 1 {
		t.Fatalf("ToggleSelectedRow(second) = (%d, %t, %t), want (1, true, true)", row, selected, handled)
	}
	if got, want := newMarked, []int{0, 1}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("newMarked = %#v, want %#v", got, want)
	}
	query.MarkedRows = newMarked

	mode.selectedRow = 0
	row, newMarked, selected, handled = mode.ToggleSelectedRow(query)
	if !handled || selected || row != 0 {
		t.Fatalf("ToggleSelectedRow(toggle-off) = (%d, %t, %t), want (0, false, true)", row, selected, handled)
	}
	if got, want := newMarked, []int{1}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("newMarked = %#v, want %#v", got, want)
	}
}

// TestPrepareResultsPanePageCJKColumnWidths verifies that CJK characters are
// measured at display width 2 each when computing column widths.
func TestPrepareResultsPanePageCJKColumnWidths(t *testing.T) {
	// "名前" = 2 CJK chars → display width 4
	// "中文テスト" = 5 CJK chars → display width 10
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "名前"}, {Name: "id"}},
		Rows: []db.ResultRow{
			{Values: []db.ResultValue{
				{Kind: db.ValueKindString, Value: "中文"},   // display width 4
				{Kind: db.ValueKindInteger, Value: int64(1)},
			}},
			{Values: []db.ResultValue{
				{Kind: db.ValueKindString, Value: "テスト長い値"}, // display width 12
				{Kind: db.ValueKindInteger, Value: int64(2)},
			}},
		},
	}

	prepared := tui.PrepareResultsPanePage(result, 0)

	// "名前" header has display width 4; cell "テスト長い値" has display width 12 → column 0 width = 12
	if got, want := prepared.Widths[0], 12; got != want {
		t.Fatalf("Widths[0] = %d, want %d (CJK display widths must be counted as 2 each)", got, want)
	}
	// "id" header has display width 2; cell "1" has display width 1 → column 1 width = 2
	if got, want := prepared.Widths[1], 2; got != want {
		t.Fatalf("Widths[1] = %d, want %d", got, want)
	}
}

// TestRenderInlineResultLineCJKPadding verifies that CJK values are padded
// correctly so columns align despite multi-byte characters.
func TestRenderInlineResultLineCJKPadding(t *testing.T) {
	// Column widths: 4, 10
	widths := []int{4, 10}

	// Row 1: "ab" (width 2) padded to 4, "hello" (width 5) padded to 10
	line1 := tui.RenderInlineResultLine([]string{"ab", "hello"}, widths)
	// Row 2: "中文" (width 4) needs no extra padding, "テスト長い値" (width 12) > 10 but no truncation here
	line2 := tui.RenderInlineResultLine([]string{"中文", "テスト長い値"}, widths)

	// line1 col0: "ab  " (2 spaces of padding)
	if !strings.Contains(line1, "ab  ") {
		t.Fatalf("RenderInlineResultLine() line1 = %q, want col0 padded to width 4", line1)
	}
	// line1 col1: "hello     " (5 spaces)
	if !strings.Contains(line1, "hello     ") {
		t.Fatalf("RenderInlineResultLine() line1 = %q, want col1 padded to width 10", line1)
	}

	// line2 col0: "中文" display-width 4, column width 4 → padding=0.
	// The join separator is " | " so the rendered output is "中文" + "" + " | " = "中文 | ".
	if !strings.Contains(line2, "中文 | ") {
		t.Fatalf("RenderInlineResultLine() line2 = %q, want CJK value followed by \" | \" (no extra padding when display width equals column width)", line2)
	}
}

// TestResultsPaneModeViewCJKCharacters is an end-to-end test ensuring the
// Results Pane renders CJK column headers and values with correct alignment.
func TestResultsPaneModeViewCJKCharacters(t *testing.T) {
	mode := newResultsPaneModeModel()
	mode.SetSize(80, 12)

	cjkInteraction := InteractionState{
		LatestResult: &LatestResultContext{
			Statement: "select 名前, score from users",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "名前"}, {Name: "score"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{
						{Kind: db.ValueKindString, Value: "中文"},
						{Kind: db.ValueKindInteger, Value: int64(100)},
					}},
					{Values: []db.ResultValue{
						{Kind: db.ValueKindString, Value: "Alice"},
						{Kind: db.ValueKindInteger, Value: int64(99)},
					}},
				},
			},
		},
	}
	view := ansi.Strip(mode.View(mode.buildViewContext(cjkInteraction)))

	// Header must contain "名前" and "score"
	if !strings.Contains(view, "名前") {
		t.Fatalf("View() = %q, want CJK column header 名前", view)
	}
	// Data values must appear
	if !strings.Contains(view, "中文") {
		t.Fatalf("View() = %q, want CJK cell value 中文", view)
	}
	// "Alice" must also appear
	if !strings.Contains(view, "Alice") {
		t.Fatalf("View() = %q, want ASCII cell value Alice", view)
	}

	// The header separator must be at least as wide as the CJK column header (display width 4).
	// "名前" header → column width ≥ 4, separator uses max(3, width) dashes → at least 4 dashes.
	if !strings.Contains(view, "----") {
		t.Fatalf("View() = %q, want separator of at least 4 dashes for CJK header column", view)
	}

	// The separator is " | " and column 0 has width=5 (max of "名前"=4, "中文"=4, "Alice"=5).
	// "中文" (display-width 4) gets 1 space of padding before " | " → "中文  | "
	if !strings.Contains(view, "中文  | ") {
		t.Fatalf("View() = %q, want '中文  | ' (1 space padding + separator space for CJK value)", view)
	}

	// "Alice" has display-width 5 = column width → 0 padding before " | " → "Alice | "
	if !strings.Contains(view, "Alice | ") {
		t.Fatalf("View() = %q, want 'Alice | ' (no extra padding for Alice)", view)
	}
}

func TestPrepareResultsPanePageTruncatesNewlineValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value db.ResultValue
		want  string
	}{
		{
			name:  "LF newline truncated",
			value: db.ResultValue{Kind: db.ValueKindString, Value: "line1\nline2\nline3"},
			want:  "line1...",
		},
		{
			name:  "CRLF newline truncated",
			value: db.ResultValue{Kind: db.ValueKindString, Value: "line1\r\nline2"},
			want:  "line1...",
		},
		{
			name:  "no newline unchanged",
			value: db.ResultValue{Kind: db.ValueKindString, Value: "just a plain value"},
			want:  "just a plain value",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "note"}},
				Rows:    []db.ResultRow{{Values: []db.ResultValue{tc.value}}},
			}
			prepared := tui.PrepareResultsPanePage(result, 0)
			if got := prepared.Rows[0][0]; got != tc.want {
				t.Fatalf("PrepareResultsPanePage row value = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResultsPaneTableTruncatesMultilineValues(t *testing.T) {
	mode := newResultsPaneModeModel()
	mode.SetSize(80, 8)

	truncInteraction := InteractionState{
		LatestResult: &LatestResultContext{
			Statement: "select id, note from widgets",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}, {Name: "note"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{
						{Kind: db.ValueKindInteger, Value: int64(1)},
						{Kind: db.ValueKindString, Value: "first line\nsecond line"},
					}},
					{Values: []db.ResultValue{
						{Kind: db.ValueKindInteger, Value: int64(2)},
						{Kind: db.ValueKindString, Value: "only line"},
					}},
				},
			},
		},
	}
	view := ansi.Strip(mode.View(mode.buildViewContext(truncInteraction)))

	if !strings.Contains(view, "first line...") {
		t.Fatalf("View() = %q, want multiline value truncated to 'first line...'", view)
	}
	if strings.Contains(view, "second line") {
		t.Fatalf("View() = %q, want second line of multiline value not shown", view)
	}
	if !strings.Contains(view, "only line") {
		t.Fatalf("View() = %q, want single-line value unchanged", view)
	}
}

func TestResultsPaneModeNavigateEdgeLockScrollsOnEveryHorizontalPress(t *testing.T) {
	mode := newResultsPaneModeModel()
	mode.SetSize(20, 8)

	query := InteractionState{
		LatestResult: &LatestResultContext{
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}, {Name: "email"}, {Name: "age"}},
				Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindString, Value: "ada@example.com"}, {Kind: db.ValueKindInteger, Value: int64(30)}}}},
			},
		},
	}

	// First right press from col 0 must immediately advance the viewport.
	mode.Navigate(tea.KeyPressMsg{Code: tea.KeyRight}, query)
	if got, want := mode.selectedColumn, 1; got != want {
		t.Fatalf("selectedColumn after 1st right = %d, want %d", got, want)
	}
	if got, want := mode.colScrollOffset, 1; got != want {
		t.Fatalf("colScrollOffset after 1st right = %d, want %d (edge-lock: no dead zone)", got, want)
	}

	// Second right press advances again.
	mode.Navigate(tea.KeyPressMsg{Code: tea.KeyRight}, query)
	if got, want := mode.colScrollOffset, 2; got != want {
		t.Fatalf("colScrollOffset after 2nd right = %d, want %d", got, want)
	}

	// Left press immediately scrolls back.
	mode.Navigate(tea.KeyPressMsg{Text: "h"}, query)
	if got, want := mode.selectedColumn, 1; got != want {
		t.Fatalf("selectedColumn after left = %d, want %d", got, want)
	}
	if got, want := mode.colScrollOffset, 1; got != want {
		t.Fatalf("colScrollOffset after left = %d, want %d (edge-lock: immediate scroll back)", got, want)
	}

	// Pressing left at col 0 clamps cursor and does not underflow offset.
	mode.Navigate(tea.KeyPressMsg{Text: "h"}, query) // back to col 0
	mode.Navigate(tea.KeyPressMsg{Text: "h"}, query) // attempt to go left of col 0
	if got, want := mode.selectedColumn, 0; got != want {
		t.Fatalf("selectedColumn after clamped left = %d, want %d", got, want)
	}
	if got, want := mode.colScrollOffset, 0; got != want {
		t.Fatalf("colScrollOffset after clamped left = %d, want %d", got, want)
	}
}

func TestResultsPaneModeNavigateClampsAtPageBoundary(t *testing.T) {
	rows := make([]db.ResultRow, tui.ResultsPanePageSize+1)
	for i := range rows {
		rows[i] = db.ResultRow{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(i)}}}
	}
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}},
		Rows:    rows,
	}

	// On page 0, pressing down on the last row of that page must not advance to page 1.
	mode := newResultsPaneModeModel()
	mode.selectedRow = tui.ResultsPanePageSize - 1
	query := InteractionState{ResultsPanePage: 0, LatestResult: &LatestResultContext{PreservedResult: result}}
	page, handled := mode.Navigate(tea.KeyPressMsg{Code: tea.KeyDown}, query)
	if !handled {
		t.Fatal("Navigate(down at page boundary) was not handled")
	}
	if page != 0 {
		t.Fatalf("Navigate(down at page boundary) returned page %d, want 0", page)
	}
	if mode.selectedRow != tui.ResultsPanePageSize-1 {
		t.Fatalf("selectedRow = %d, want %d (should not cross page boundary)", mode.selectedRow, tui.ResultsPanePageSize-1)
	}

	// On page 1, pressing up on its first row must not retreat to page 0.
	mode2 := newResultsPaneModeModel()
	mode2.selectedRow = tui.ResultsPanePageSize
	query2 := InteractionState{ResultsPanePage: 1, LatestResult: &LatestResultContext{PreservedResult: result}}
	page2, handled2 := mode2.Navigate(tea.KeyPressMsg{Code: tea.KeyUp}, query2)
	if !handled2 {
		t.Fatal("Navigate(up at page boundary) was not handled")
	}
	if page2 != 1 {
		t.Fatalf("Navigate(up at page boundary) returned page %d, want 1", page2)
	}
	if mode2.selectedRow != tui.ResultsPanePageSize {
		t.Fatalf("selectedRow = %d, want %d (should not cross page boundary)", mode2.selectedRow, tui.ResultsPanePageSize)
	}
}

func BenchmarkResultsPaneModeViewLargePage(b *testing.B) {
	mode := newResultsPaneModeModel()
	mode.SetSize(140, 24)
	mode.selectedRow = 150
	mode.selectedColumn = 3
	mode.selectionActive = true

	columns := make([]db.ResultColumn, 0, 8)
	for i := 1; i <= 8; i++ {
		columns = append(columns, db.ResultColumn{Name: fmt.Sprintf("col_%d", i)})
	}

	rows := make([]db.ResultRow, 0, 5000)
	for row := 1; row <= 5000; row++ {
		values := make([]db.ResultValue, 0, len(columns))
		for col := range columns {
			values = append(values, db.ResultValue{Kind: db.ValueKindString, Value: fmt.Sprintf("row-%04d-col-%d", row, col+1)})
		}
		rows = append(rows, db.ResultRow{Values: values})
	}

	query := InteractionState{
		ResultsPanePage: 0,
		LatestResult: &LatestResultContext{
			Statement: "select * from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: columns,
				Rows:    rows,
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mode.View(mode.buildViewContext(query))
	}
}
