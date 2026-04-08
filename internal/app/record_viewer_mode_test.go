package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRecordViewerModeViewRendersFullResultSet(t *testing.T) {
	mode := newRecordViewerModeModel()
	mode.SetSize(80, 8)

	view := mode.View(QueryContext{
		LatestResult: &LatestResultContext{
			Query: "select id, name, created_at from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}}, {Name: "name"}, {Name: "created_at"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 7, 11, 30, 0, 0, time.UTC)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "Grace"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 8, 9, 0, 0, 0, time.UTC)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(3)}, {Kind: db.ValueKindString, Value: "Linus"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 9, 15, 45, 0, 0, time.UTC)}}},
				},
			},
		},
	})

	plainView := ansi.Strip(view)

	for _, want := range []string{
		"Record viewer",
		"Rows: 3  Columns: 3",
		"Page: 1/1  Showing rows 1-3",
		"[pk] id | name  | created_at",
		"1       | Ada   | 2026-04-07T11:30:00Z",
		"2       | Grace | 2026-04-08T09:00:00Z",
		"3       | Linus | 2026-04-09T15:45:00Z",
	} {
		if !strings.Contains(plainView, want) {
			t.Fatalf("View() = %q, want to contain %q", plainView, want)
		}
	}

}

func TestRecordViewerModeViewRendersSelectedPage(t *testing.T) {
	mode := newRecordViewerModeModel()
	mode.SetSize(80, 12)

	rows := make([]db.ResultRow, 0, 305)
	for i := 1; i <= 305; i++ {
		rows = append(rows, db.ResultRow{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(i)}}})
	}

	view := mode.View(QueryContext{
		ViewerPage: 1,
		LatestResult: &LatestResultContext{
			Query: "select id from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}},
				Rows:    rows,
			},
		},
	})

	for _, want := range []string{
		"Rows: 305  Columns: 1",
		"Page: 2/2  Showing rows 301-305",
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

func TestRecordViewerModeViewClipsRowsToVisibleViewport(t *testing.T) {
	mode := newRecordViewerModeModel()
	mode.SetSize(80, 14)
	mode.selectedRow = 15
	mode.selectionActive = true

	rows := make([]db.ResultRow, 0, 30)
	for i := 1; i <= 30; i++ {
		rows = append(rows, db.ResultRow{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: fmt.Sprintf("row-%03d", i)}}})
	}

	view := ansi.Strip(mode.View(QueryContext{
		LatestResult: &LatestResultContext{
			Query: "select label from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "label"}},
				Rows:    rows,
			},
		},
	}))

	for _, want := range []string{"row-014", "row-018", "Viewport rows 14-18 of 30 on this page."} {
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

func TestRecordViewerModeFooterIncludesModeDetails(t *testing.T) {
	mode := newRecordViewerModeModel()
	footer := mode.Footer("local", "sqlite", QueryContext{
		Layout:       LayoutViewerOnly,
		LatestResult: &LatestResultContext{PreservedResult: &db.ResultSet{Rows: []db.ResultRow{{}, {}}}, SelectedRows: []int{0}},
		Running:      &RunningQueryContext{Label: "/tables", Elapsed: 2*time.Second + 300*time.Millisecond},
	})

	for _, want := range []string{"Record viewer", "layout viewer only", "connection local", "dialect sqlite", "2 rows", "page 1/1", "1 selected", "- /tables 2.3s", "alt+h help", "arrows/hjkl navigate", "space toggle row", "yy compose insert", "cc compose update", "dd compose delete", "ctrl+u prev page", "ctrl+d next page", "ctrl+x focus", "ctrl+1 split", "ctrl+2 command", "ctrl+3 viewer", "ctrl+c quit"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}

func TestComposeRecordViewerInsertSQLUsesVisibleColumns(t *testing.T) {
	result, err := composeRecordViewerInsertSQL(db.PostgresDialect(), &LatestResultContext{
		Query: "select id, name, active from public.users order by id;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Schema: "public", Name: "users"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "name"},
				{Name: "active"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindBool, Value: true}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeRecordViewerInsertSQL() error = %v", err)
	}
	if got, want := result.Action, recordViewerComposeActionInsert; got != want {
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

func TestComposeRecordViewerInsertSQLFallsBackToVisibleColumnsWithQuotedValues(t *testing.T) {
	result, err := composeRecordViewerInsertSQL(db.SQLiteDialect(), &LatestResultContext{
		Query: "select name, note, payload from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}, {Name: "note"}, {Name: "payload"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "Ada's"}, {Kind: db.ValueKindNull}, {Kind: db.ValueKindBytes, Value: []byte{0xde, 0xad}}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeRecordViewerInsertSQL() error = %v", err)
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

func TestComposeRecordViewerDeleteSQLUsesPrimaryKeys(t *testing.T) {
	result, err := composeRecordViewerDeleteSQL(db.PostgresDialect(), &LatestResultContext{
		Query: "select id, name from public.users order by id;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Schema: "public", Name: "users"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "name"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeRecordViewerDeleteSQL() error = %v", err)
	}
	if !result.UsedPrimaryKeys {
		t.Fatal("UsedPrimaryKeys = false, want true")
	}
	if got, want := result.Action, recordViewerComposeActionDelete; got != want {
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

func TestComposeRecordViewerDeleteSQLFallsBackToVisibleColumnPredicate(t *testing.T) {
	result, err := composeRecordViewerDeleteSQL(db.SQLiteDialect(), &LatestResultContext{
		Query: "select name, note from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}, {Name: "note"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "Ada's"}, {Kind: db.ValueKindNull}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeRecordViewerDeleteSQL() error = %v", err)
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

func TestComposeRecordViewerUpdateSQLUsesPrimaryKeys(t *testing.T) {
	result, err := composeRecordViewerUpdateSQL(db.PostgresDialect(), &LatestResultContext{
		Query: "select id, name, active from public.users order by id;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Schema: "public", Name: "users"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "name"},
				{Name: "active"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindBool, Value: true}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeRecordViewerUpdateSQL() error = %v", err)
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

func TestComposeRecordViewerUpdateSQLFallsBackToVisibleColumnPredicate(t *testing.T) {
	result, err := composeRecordViewerUpdateSQL(db.SQLiteDialect(), &LatestResultContext{
		Query: "select name, note, payload from widgets;",
		PreservedResult: &db.ResultSet{
			Columns: []db.ResultColumn{{Name: "name"}, {Name: "note"}, {Name: "payload"}},
			Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindString, Value: "Ada's"}, {Kind: db.ValueKindNull}, {Kind: db.ValueKindBytes, Value: []byte{0xde, 0xad}}}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeRecordViewerUpdateSQL() error = %v", err)
	}
	if result.UsedPrimaryKeys {
		t.Fatal("UsedPrimaryKeys = true, want false")
	}
	for _, want := range []string{
		"UPDATE \"widgets\"",
		"\"name\" = 'Ada''s'",
		"\"note\" = NULL",
		"\"payload\" = X'dead'",
		"\"note\" IS NULL",
	} {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
}

func TestInferQuerySourceTableReturnsNilForJoins(t *testing.T) {
	if got := inferQuerySourceTable("select widgets.id from widgets join owners on owners.id = widgets.owner_id;"); got != nil {
		t.Fatalf("inferQuerySourceTable(join) = %#v, want nil", got)
	}
}

func TestRenderRecordViewerTableOnlyStylesPrimaryKeyColumns(t *testing.T) {
	table := renderRecordViewerTable(&db.ResultSet{
		Columns: []db.ResultColumn{
			{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
			{Name: "name"},
		},
		Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
	}, 0, 80, 10, recordViewerRenderState{})

	plain := ansi.Strip(table)
	if !strings.Contains(plain, "[pk] id | name") {
		t.Fatalf("renderRecordViewerTable() = %q, want primary key marker in header", plain)
	}
	if !strings.Contains(plain, "7       | Ada") {
		t.Fatalf("renderRecordViewerTable() = %q, want primary key values aligned", plain)
	}
	if strings.Contains(plain, "[pk] name") {
		t.Fatalf("renderRecordViewerTable() = %q, want non-primary key columns unchanged", plain)
	}
}

func TestClampRecordViewerPage(t *testing.T) {
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
			if got := clampRecordViewerPage(tc.page, tc.totalRows); got != tc.want {
				t.Fatalf("clampRecordViewerPage(%d, %d) = %d, want %d", tc.page, tc.totalRows, got, tc.want)
			}
		})
	}
}

func TestRecordViewerModeNavigateSupportsArrowsAndHJKL(t *testing.T) {
	mode := newRecordViewerModeModel()
	query := QueryContext{
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

	if page, handled := mode.Navigate(tea.KeyMsg{Type: tea.KeyRight}, query); !handled || page != 0 {
		t.Fatalf("Navigate(right) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedColumn, 1; got != want {
		t.Fatalf("selectedColumn = %d, want %d", got, want)
	}

	if page, handled := mode.Navigate(tea.KeyMsg{Type: tea.KeyDown}, query); !handled || page != 0 {
		t.Fatalf("Navigate(down) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedRow, 1; got != want {
		t.Fatalf("selectedRow = %d, want %d", got, want)
	}

	if page, handled := mode.Navigate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}, query); !handled || page != 0 {
		t.Fatalf("Navigate(h) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedColumn, 0; got != want {
		t.Fatalf("selectedColumn = %d, want %d", got, want)
	}

	if page, handled := mode.Navigate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, query); !handled || page != 0 {
		t.Fatalf("Navigate(k) = (%d, %t), want (0, true)", page, handled)
	}
	if got, want := mode.selectedRow, 0; got != want {
		t.Fatalf("selectedRow = %d, want %d", got, want)
	}
}

func TestRenderRecordViewerTableHighlightsSelectedCell(t *testing.T) {
	table := renderRecordViewerTable(&db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
		Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
	}, 0, 80, 10, recordViewerRenderState{Active: recordViewerSelection{Row: 0, Column: 1, Active: true}})

	if !strings.Contains(table, "\x1b[") {
		t.Fatalf("renderRecordViewerTable() = %q, want ANSI styling for selected cell", table)
	}
	plain := ansi.Strip(table)
	if !strings.Contains(plain, "7  | Ada") && !strings.Contains(plain, "7 | Ada") {
		t.Fatalf("renderRecordViewerTable() = %q, want selected cell text preserved", plain)
	}
}

func TestRecordViewerModeViewShowsSelectedRowCount(t *testing.T) {
	mode := newRecordViewerModeModel()
	mode.SetSize(80, 8)

	view := ansi.Strip(mode.View(QueryContext{
		LatestResult: &LatestResultContext{
			Query:        "select id from widgets order by id",
			SelectedRows: []int{0, 2},
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(3)}}},
				},
			},
		},
	}))

	for _, want := range []string{"Selected: 2", "* 1", "* 3"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}
}

func TestRecordViewerModeToggleSelectedRowTracksMultipleRows(t *testing.T) {
	mode := newRecordViewerModeModel()
	query := QueryContext{
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

	row, selected, handled := mode.ToggleSelectedRow(&query)
	if !handled || !selected || row != 0 {
		t.Fatalf("ToggleSelectedRow(first) = (%d, %t, %t), want (0, true, true)", row, selected, handled)
	}
	if got, want := query.LatestResult.SelectedRows, []int{0}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}

	mode.selectedRow = 1
	row, selected, handled = mode.ToggleSelectedRow(&query)
	if !handled || !selected || row != 1 {
		t.Fatalf("ToggleSelectedRow(second) = (%d, %t, %t), want (1, true, true)", row, selected, handled)
	}
	if got, want := query.LatestResult.SelectedRows, []int{0, 1}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}

	mode.selectedRow = 0
	row, selected, handled = mode.ToggleSelectedRow(&query)
	if !handled || selected || row != 0 {
		t.Fatalf("ToggleSelectedRow(toggle-off) = (%d, %t, %t), want (0, false, true)", row, selected, handled)
	}
	if got, want := query.LatestResult.SelectedRows, []int{1}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("SelectedRows = %#v, want %#v", got, want)
	}
}

func BenchmarkRecordViewerModeViewLargePage(b *testing.B) {
	mode := newRecordViewerModeModel()
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

	query := QueryContext{
		ViewerPage: 0,
		LatestResult: &LatestResultContext{
			Query: "select * from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: columns,
				Rows:    rows,
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mode.View(query)
	}
}
