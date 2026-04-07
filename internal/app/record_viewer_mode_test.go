package app

import (
	"strings"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

func TestRecordViewerModeViewRendersFullResultSet(t *testing.T) {
	mode := newRecordViewerModeModel()
	mode.SetSize(80, 8)

	view := mode.View(QueryContext{
		LatestResult: &LatestResultContext{
			Query: "select id, name, created_at from widgets order by id",
			PreservedResult: &db.ResultSet{
				Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}, {Name: "created_at"}},
				Rows: []db.ResultRow{
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 7, 11, 30, 0, 0, time.UTC)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "Grace"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 8, 9, 0, 0, 0, time.UTC)}}},
					{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(3)}, {Kind: db.ValueKindString, Value: "Linus"}, {Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 9, 15, 45, 0, 0, time.UTC)}}},
				},
			},
		},
	})

	for _, want := range []string{
		"Record viewer",
		"Rows: 3  Columns: 3",
		"id | name  | created_at",
		"1  | Ada   | 2026-04-07T11:30:00Z",
		"2  | Grace | 2026-04-08T09:00:00Z",
		"3  | Linus | 2026-04-09T15:45:00Z",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, want to contain %q", view, want)
		}
	}

	if strings.Contains(view, "Showing ") {
		t.Fatalf("View() = %q, want all rows without truncation notice", view)
	}
}

func TestRecordViewerModeFooterIncludesModeDetails(t *testing.T) {
	mode := newRecordViewerModeModel()
	footer := mode.Footer("local", "sqlite", QueryContext{
		LatestResult: &LatestResultContext{PreservedResult: &db.ResultSet{Rows: []db.ResultRow{{}, {}}}},
	})

	for _, want := range []string{"Record viewer", "connection local", "dialect sqlite", "2 rows", "ctrl+x mode", "ctrl+c quit"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("Footer() = %q, want to contain %q", footer, want)
		}
	}
}
