package app

import (
	"strings"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
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
	mode.SetSize(80, 8)

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

func TestRecordViewerModeFooterIncludesModeDetails(t *testing.T) {
	mode := newRecordViewerModeModel()
	footer := mode.Footer("local", "sqlite", QueryContext{
		Layout:       LayoutViewerOnly,
		LatestResult: &LatestResultContext{PreservedResult: &db.ResultSet{Rows: []db.ResultRow{{}, {}}}},
		Running:      &RunningQueryContext{Label: "/tables", Elapsed: 2*time.Second + 300*time.Millisecond},
	})

	for _, want := range []string{"Record viewer", "layout viewer only", "connection local", "dialect sqlite", "2 rows", "page 1/1", "- /tables 2.3s", "ctrl+u prev page", "ctrl+d next page", "ctrl+x focus", "ctrl+1 split", "ctrl+2 command", "ctrl+3 viewer", "ctrl+c quit"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}

func TestRenderRecordViewerTableOnlyStylesPrimaryKeyColumns(t *testing.T) {
	table := renderRecordViewerTable(&db.ResultSet{
		Columns: []db.ResultColumn{
			{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
			{Name: "name"},
		},
		Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindString, Value: "Ada"}}}},
	}, 0, 80, 10)

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
